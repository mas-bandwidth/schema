package main

// THE ANNOUNCEMENT'S ID TABLE IS A FILE'S (docs/SPEC-TABLES.md §3.3: the
// announcement is a form 1 file; §3, THE DISTINCTNESS CHECK IS BOUNDED WORK).
// The `announce-grown` rows read a message row again over its connection's
// announcement with the id table grown to 4096 entries, to 4097, and to 4096
// with one id named twice. These tests hold the rows' answers to the
// compiler's engine, the grown bytes to their shape, and the rows to the
// derived manifest a driver reads and the expectations it is scored against.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

var defaultGrownRows = []AnnounceGrown{
	{Name: "login_full_ids4096", Message: "login_full", Entries: 4096, Repeat: false, Refused: false},
	{Name: "login_full_ids4097", Message: "login_full", Entries: 4097, Repeat: false, Refused: true},
	{Name: "login_full_ids4096_repeat", Message: "login_full", Entries: 4096, Repeat: true, Refused: true},
	{Name: "vocab9_wide_ids4096", Message: "vocab9_wide", Entries: 4096, Repeat: false, Refused: false},
	{Name: "vocab9_wide_ids4097", Message: "vocab9_wide", Entries: 4097, Repeat: false, Refused: true},
	{Name: "vocab9_wide_ids4096_repeat", Message: "vocab9_wide", Entries: 4096, Repeat: true, Refused: true},
}

func ensureGrownRows(m *Manifest) {
	if len(m.Grown) == 0 {
		m.Grown = slices.Clone(defaultGrownRows)
	}
}

// grownAnnouncement is a row's grown announcement and the connection it grew
// from.
func grownAnnouncement(t *testing.T, m *Manifest, g AnnounceGrown) ([]byte, Connection) {
	t.Helper()
	msg, err := m.LookupMessage(g.Message)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.LookupConnection(msg.Connection)
	if err != nil {
		t.Fatal(err)
	}
	grown, err := GrowIdTable(wireBytes(t, c.Wire), g.Entries, g.Repeat)
	if err != nil {
		t.Fatal(err)
	}
	return grown, c
}

// TestLimitAnnounceGrownIsTheEnginesAnswer: every row's answer is the one
// the engine gives its grown announcement. A `message` row is read clean and
// announces exactly the connection's own vocabulary, so its message golden
// decodes against it; a `malformed` row is damage, which sets no vocabulary
// and is refused. The corpus carries all three shapes: 4096 distinct accepted,
// 4097 and a repeat at 4096 refused.
func TestLimitAnnounceGrownIsTheEnginesAnswer(t *testing.T) {
	m, _, u := corpus(t)
	ensureGrownRows(m)
	shapes := map[string]bool{}
	for _, g := range m.Grown {
		grown, c := grownAnnouncement(t, m, g)
		var v tablewire.Vocabulary
		var report tabletext.Report
		err := v.AnnounceRead(grown, &report)
		if g.Refused {
			if err == nil || !report.Refused || v.Announced() {
				t.Errorf("%s: the row says malformed and the engine answered err=%v refused=%v announced=%v", g.Name, err, report.Refused, v.Announced())
			}
		} else {
			_, own := announced(t, u, c)
			if err != nil || report.Refused || !v.Announced() || !report.Silent() {
				t.Errorf("%s: the row says message and the engine answered err=%v report=%+v announced=%v", g.Name, err, report, v.Announced())
				continue
			}
			same := len(v.Entries()) == len(own.Entries())
			for i := 0; same && i < len(own.Entries()); i++ {
				same = v.Entries()[i].Key() == own.Entries()[i].Key()
			}
			if !same || v.RefBits() != own.RefBits() || v.BuildVersion() != own.BuildVersion() {
				t.Errorf("%s: the grown announcement does not announce the connection's own vocabulary", g.Name)
			}
		}
		shapes[g.answerShape()] = true
	}
	for _, want := range []string{"4096 distinct message", "4097 distinct malformed", "4096 repeat malformed"} {
		if !shapes[want] {
			t.Errorf("the manifest carries no announce-grown row of shape %q", want)
		}
	}
}

func TestAGrownAnnouncementIsTheEnginesAnswer(t *testing.T) {
	TestLimitAnnounceGrownIsTheEnginesAnswer(t)
}

// answerShape is the row's entries, shape and answer as the manifest spells
// them.
func (g AnnounceGrown) answerShape() string {
	shape, answer := "distinct", "message"
	if g.Repeat {
		shape = "repeat"
	}
	if g.Refused {
		answer = "malformed"
	}
	return strings.Join([]string{strconv.Itoa(g.Entries), shape, answer}, " ")
}

// TestLimitGrowIdTable holds the grown bytes to their shape: the body and the
// original entries unmoved, the count word the new length, every added id new,
// and a repeat that names the first id twice and nothing else.
func TestLimitGrowIdTable(t *testing.T) {
	corpus(t)
	golden := wireBytes(t, "testdata/wire/tables/backend_conn.bin")
	k := int(binary.LittleEndian.Uint64(golden[len(golden)-8:]))
	for _, repeat := range []bool{false, true} {
		for _, n := range []int{k + 1, 4096, 4097} {
			grown, err := GrowIdTable(golden, n, repeat)
			if err != nil {
				t.Fatal(err)
			}
			if len(grown) != len(golden)+8*(n-k) {
				t.Fatalf("n=%d: %d bytes, want %d", n, len(grown), len(golden)+8*(n-k))
			}
			if !bytes.Equal(grown[:len(golden)-8], golden[:len(golden)-8]) {
				t.Fatalf("n=%d: the body or an original entry moved", n)
			}
			if got := binary.LittleEndian.Uint64(grown[len(grown)-8:]); got != uint64(n) {
				t.Fatalf("n=%d: the count word is %d", n, got)
			}
			ids := map[uint64]int{}
			table := grown[len(grown)-8-8*n : len(grown)-8]
			for i := range n {
				ids[binary.LittleEndian.Uint64(table[8*i:])]++
			}
			want := n
			if repeat {
				want = n - 1
				if ids[binary.LittleEndian.Uint64(table)] != 2 {
					t.Fatalf("n=%d: the repeat does not name the first id twice", n)
				}
			}
			if len(ids) != want {
				t.Fatalf("n=%d repeat=%v: %d distinct ids, want %d", n, repeat, len(ids), want)
			}
		}
	}
	if _, err := GrowIdTable(golden, k-1, false); err == nil {
		t.Fatal("a table was grown to fewer entries than it has")
	}
}

func TestGrowIdTable(t *testing.T) {
	TestLimitGrowIdTable(t)
}

// TestLimitTheGrownRowsReachADriver: the derived manifest hands a driver each row
// as an ordinary connection keyed by the row's name, over the materialised
// grown file, and an ordinary message on it; the answer stays withheld. The
// message surface scores a `message` row against the message golden and a
// `malformed` row against a refusal verdict.
func TestLimitTheGrownRowsReachADriver(t *testing.T) {
	m, _, _ := corpus(t)
	ensureGrownRows(m)
	work := t.TempDir()
	if err := materialise(m, work); err != nil {
		t.Fatal(err)
	}
	derived := filepath.Join(work, "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(text), "\n")
	if strings.Contains(string(text), "announce-grown") || strings.Contains(string(text), "malformed announcement") {
		t.Fatal("the derived manifest carries the row's answer")
	}
	want, err := expectations(m, "message", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range m.Grown {
		msg, _ := m.LookupMessage(g.Message)
		grown, c := grownAnnouncement(t, m, g)
		if !bytes.Equal(wireBytes(t, g.File), grown) {
			t.Errorf("%s: the materialised announcement is not the grown one", g.Name)
		}
		conn := "connection " + g.Name + " " + c.Unit + " "
		row := strings.Join([]string{"message", g.Name, g.Name, msg.Root, msg.FileWire, msg.MessageWire}, " ")
		var sawConn, sawRow bool
		for _, l := range lines {
			sawConn = sawConn || strings.HasPrefix(l, conn) && strings.HasSuffix(l, " "+g.File)
			sawRow = sawRow || l == row
		}
		if !sawConn || !sawRow {
			t.Errorf("%s: the derived manifest carries connection=%v message=%v", g.Name, sawConn, sawRow)
		}
		i := slices.IndexFunc(want, func(e expectation) bool { return e.name == g.Name })
		if i < 0 {
			t.Errorf("%s: the message surface does not score the row", g.Name)
			continue
		}
		e := want[i]
		if g.Refused {
			if e.want != nil {
				t.Errorf("%s: scored as %+v, want refusal (nil want)", g.Name, e)
			}
		} else if !bytes.Equal(e.want, wireBytes(t, msg.MessageWire)) {
			t.Errorf("%s: not scored against the message golden", g.Name)
		}
	}
}

func TestTheGrownRowsReachADriver(t *testing.T) {
	TestLimitTheGrownRowsReachADriver(t)
}
