package tablewire

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const messageRepeatUnit = `package messagerepeat

enum Mode { Idle, Run, Fly }

flags Cap { Trace, Verbose }

fixed table Child
{
    n int32 | min = -50, max = 50
    s string(3)
}

union Arm
{
    c   Child
    k   uint8
    t   string(4)
}

fixed table Root
{
    flag   bool
    mode   Mode
    cap    Cap
    ratio  float32 | min = 0, max = 1, resolution = 0.01
    f64    float64
    counts [..4]uint8
    fixedc [3]int16
    by     [Mode]int32
    arm    Arm
    arms   [..2]Arm
    wtext  string(16)
    bi     bits(9)
    wide   int128 | min = -1000000000000000000000, max = 1000000000000000000000
    q      fixed(8, 8) | min = -100, max = 100
    at     timestamp
    kid    Child
    okid   ?Child
    opt    ?int64
    raw    bytes(5)
}
`

// messageBodyEnd is the bit offset, within data[1:], just past a batch-of-one's
// body, its terminating reference included.
func messageBodyEnd(t *testing.T, m *tabletext.Model, voc *Vocabulary, wire []byte) int {
	r := newBitReader(wire[1:])
	r.get(8)
	v := *voc
	d := &bitDecoder{m: m, v: &v, report: &tabletext.Report{}, r: r, refBits: v.RefBits(), fail: &readFail{}}
	if !d.body(m.New(m.Lookup("Root"))) {
		t.Fatal("body")
	}
	return r.off
}

// messageSplice copies the bits [from, to) of src into w.
func messageSplice(src []byte, from, to int, w *bitWriter) {
	r := newBitReader(src)
	r.skip(from)
	for i := from; i < to; i++ {
		b, _ := r.get(1)
		w.put(b, 1)
	}
}

// TestMessageRefusesRepeatedFieldId holds docs/SPEC-TABLES.md §3.3: a field id
// twice in one message body (the first body's fields, then a second body's,
// under one terminator) is damage, and the batch refuses wire_damaged.
func TestMessageRefusesRepeatedFieldId(t *testing.T) {
	m := covModel(t, messageRepeatUnit)
	root := m.Lookup("Root")
	var voc Vocabulary
	var ann tabletext.Report
	voc.AnnounceRead(Announce(m.Unit), &ann)
	for _, pair := range [][2]string{{`{"bi":5}`, `{"bi":300}`}, {`{"f64":1.5}`, `{"f64":-2}`}, {`{"flag":true}`, `{"flag":true}`}, {`{"opt":1}`, `{"opt":2}`}} {
		x, _ := EncodeMessage(m, covPlace(t, m, "Root", pair[0]))
		y, _ := EncodeMessage(m, covPlace(t, m, "Root", pair[1]))
		ex := messageBodyEnd(t, m, &voc, x)
		ey := messageBodyEnd(t, m, &voc, y)
		rb := voc.RefBits()
		w := &bitWriter{}
		w.put(2, 8) // the message form's byte
		w.put(0, 8) // one body
		messageSplice(x[1:], 8, ex-rb, w)
		messageSplice(y[1:], 8, ey, w)
		w.align()
		data := w.b
		inst := m.New(root)
		v := voc
		var rep tabletext.Report
		ok, err := DecodeMessage(m, inst, data, &v, &rep)
		if ok || RefusalReason(err) != ReasonWireDamaged {
			t.Errorf("%s then %s: a repeated field id answered ok=%v err=%v: %s", pair[0], pair[1], ok, err, hex.EncodeToString(data))
		}
	}
}

// TestMessageRefusesRepeatedKeyedSlot holds docs/SPEC-TABLES.md §3.2 and §3.3:
// the same keyed slot twice in one enum-keyed array (the second key's bits
// overwritten with the first's) is damage.
func TestMessageRefusesRepeatedKeyedSlot(t *testing.T) {
	m := covModel(t, messageRepeatUnit)
	root := m.Lookup("Root")
	var voc Vocabulary
	var ann tabletext.Report
	voc.AnnounceRead(Announce(m.Unit), &ann)
	data, _ := EncodeMessage(m, covPlace(t, m, "Root", `{"by":{"Run":1,"Fly":2}}`))
	v := voc
	if ok, err := DecodeMessage(m, m.New(root), data, &v, &tabletext.Report{}); !ok {
		t.Fatalf("the clean batch was refused: %v", err)
	}
	v = voc
	spots := MessageSpots(m, root, data, &v)
	// spots: [field ref, key Run, key Fly, terminator]
	if len(spots) < 3 {
		t.Fatal("spots")
	}
	b := append([]byte(nil), data...)
	run, fly := spots[1], spots[2]
	for i := 0; i < fly.Width; i++ {
		bit := (run.Value >> uint(i)) & 1
		pos := 8 + fly.Off + i // spots are relative to data[1:]
		if bit == 1 {
			b[pos/8] |= 1 << uint(pos%8)
		} else {
			b[pos/8] &^= 1 << uint(pos%8)
		}
	}
	inst := m.New(root)
	v2 := voc
	var rep tabletext.Report
	ok, err := DecodeMessage(m, inst, b, &v2, &rep)
	if ok || RefusalReason(err) != ReasonWireDamaged {
		t.Errorf("a repeated keyed slot answered ok=%v err=%v: %s", ok, err, hex.EncodeToString(b))
	}
}

// TestMessageRepeatNegativeControl removes each repeat rule from the message
// decoder and requires its own test red.
func TestMessageRepeatNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		from, to, run, want string
	}{
		{"\t\tif seen[entry.Id] {", "\t\tif false && seen[entry.Id] {", "^TestMessageRefusesRepeatedFieldId$", "a repeated field id answered ok=true"},
		{"\t\tif seen[key.Id] {", "\t\tif false && seen[key.Id] {", "^TestMessageRefusesRepeatedKeyedSlot$", "a repeated keyed slot answered ok=true"},
	} {
		out, failed := slowtest.RunSabotaged(t, "../..", "./internal/tablewire", c.run,
			slowtest.Sabotage{File: "internal/tablewire/messagedecode.go", From: c.from, To: c.to})
		if !failed || !strings.Contains(out, c.want) {
			t.Errorf("negative control %s: the rule is gone and the test did not go red on its own words\n%s", c.run, out)
		}
	}
}
