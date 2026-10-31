package mootable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainStores is how many retention buffers a batch probe hands the load: a
// batch takes ONE REGION AND ONE RETENTION BUFFER A BODY (docs/SPEC-TABLES.md
// §3.3, §6.6), and no manifest batch carries more bodies than this.
const retainStores = 8

// messageOut is one retaining batch load and the file-form save of each body.
type messageOut struct {
	n                       int
	retained, lost, unknown int
	malformed               bool
	used                    []int
	saves                   [][]byte
	saveLost                int
	nonFinite               string
	refusedReason           string
}

func (o messageOut) counters() string {
	return fmt.Sprintf("%d,%d,%d", o.retained, o.lost, o.unknown)
}

// mooRetainMessages loads a form-2 batch against the connection's vocabulary
// with a store a body, and saves every body as a FILE.
func mooRetainMessages(t *testing.T, g *generatedUnit, rootName string, conn, batch []byte, capacity, ids int) messageOut {
	t.Helper()
	idArg := "nil"
	if ids >= 0 {
		idArg = strconv.Itoa(ids)
	}
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + mooHex +
		fmt.Sprintf(`local report: any = {}
local vocabulary = MS.NewVocabulary()
if not MS.AnnounceRead(vocabulary, inputs.conn, report) then print("ANNOUNCE " .. tostring(report.reason)) return end
local stores = {}
for i = 1, %[4]d do
	stores[i] = R.newStore(%[1]d, %[2]s)
end
local n, values = R.LoadRetainMessages(vocabulary, %[3]q, inputs.message, 256, stores, report)
print(string.format("load %%d %%d,%%d,%%d %%s %%s", n, report.retained or 0, report.retainLost or 0, report.unknown, tostring(report.malformed), tostring(report.reason)))
local used = {}
for i = 1, math.max(n, 0) do
	used[i] = R.used(stores[i])
end
print("used " .. table.concat(used, ","))
for i = 1, math.max(n, 0) do
	local before = report.retainLost
	local out, why = R.SaveRetain(%[3]q, values[i], stores[i], report)
	if out == nil then print("SAVENIL " .. tostring(why and why.name)) else print("save " .. hex(out) .. " " .. (report.retainLost - before)) end
end
`, capacity, idArg, rootName, retainStores)
	out := g.run(t, main, map[string][]byte{"conn": conn, "message": batch})
	var o messageOut
	for line := range strings.SplitSeq(out, "\n") {
		k, v, _ := strings.Cut(line, " ")
		switch k {
		case "ANNOUNCE":
			t.Fatalf("moo refuses the announcement: %s", line)
		case "load":
			var rest string
			f := strings.Fields(v)
			if len(f) < 4 {
				t.Fatalf("moo printed %q", out)
			}
			o.n, _ = strconv.Atoi(f[0])
			if _, err := fmt.Sscanf(f[1], "%d,%d,%d", &o.retained, &o.lost, &o.unknown); err != nil {
				t.Fatalf("moo printed %q", out)
			}
			o.malformed = f[2] == "true"
			rest = strings.Join(f[3:], " ")
			o.refusedReason = rest
		case "used":
			for s := range strings.SplitSeq(v, ",") {
				if s != "" {
					n, _ := strconv.Atoi(s)
					o.used = append(o.used, n)
				}
			}
		case "save":
			hx, lost, _ := strings.Cut(v, " ")
			b, err := hex.DecodeString(hx)
			if err != nil {
				t.Fatalf("moo printed %q", out)
			}
			o.saves = append(o.saves, b)
			n, _ := strconv.Atoi(lost)
			o.saveLost += n
		case "SAVENIL":
			o.saves = append(o.saves, nil)
		}
	}
	return o
}

// goRetainMessages is the Go reference engine's answer: tablewire.DecodeRetainMessages
// and EncodeRetain per body, at the same capacities.
func goRetainMessages(t *testing.T, u *ir.Unit, rootName string, conn, batch []byte, capacity, ids int) messageOut {
	t.Helper()
	if ids < 0 {
		ids = capacity / 8
	}
	m := tabletext.NewModel(u)
	var vocab tablewire.Vocabulary
	var ann tabletext.Report
	if err := vocab.AnnounceRead(conn, &ann); err != nil {
		t.Fatalf("the Go engine refuses the announcement: %v", err)
	}
	insts := make([]*tabletext.Instance, retainStores)
	keeps := make([]*tablewire.Retain, retainStores)
	for i := range insts {
		insts[i] = m.New(u.Tables[rootName])
		keeps[i] = &tablewire.Retain{Capacity: capacity, IdCapacity: ids}
	}
	var rep tabletext.Report
	n, _, err := tablewire.DecodeRetainMessages(m, insts, batch, &vocab, keeps, &rep)
	var o messageOut
	o.n = n
	if err != nil || rep.Refused || rep.NonFiniteFloat != "" {
		o.n = -1
		o.nonFinite = rep.NonFiniteFloat
	}
	o.retained, o.lost, o.unknown, o.malformed = rep.Retained, rep.RetainLost, rep.Unknown, rep.Malformed
	for i := 0; i < max(o.n, 0); i++ {
		o.used = append(o.used, keeps[i].Used())
	}
	for i := 0; i < max(o.n, 0); i++ {
		before := rep.RetainLost
		save, err := tablewire.EncodeRetain(m, insts[i], keeps[i], &rep)
		if err != nil {
			o.saves = append(o.saves, nil)
			continue
		}
		o.saves = append(o.saves, save)
		o.saveLost += rep.RetainLost - before
	}
	return o
}

func sameMessageOut(a, b messageOut) bool {
	if a.n != b.n || a.counters() != b.counters() || a.malformed != b.malformed || a.saveLost != b.saveLost ||
		len(a.saves) != len(b.saves) || len(a.used) != len(b.used) {
		return false
	}
	for i := range a.saves {
		if string(a.saves[i]) != string(b.saves[i]) {
			return false
		}
	}
	for i := range a.used {
		if a.used[i] != b.used[i] {
			return false
		}
	}
	return true
}

func (o messageOut) String() string {
	var saves []string
	for _, s := range o.saves {
		saves = append(saves, hex.EncodeToString(s))
	}
	return fmt.Sprintf("n=%d %s malformed=%v used=%v savelost=%d saves=%v", o.n, o.counters(), o.malformed, o.used, o.saveLost, saves)
}

// retainMessageRow is a `retain-message` row joined to its `connection`.
type retainMessageRow struct {
	caseName, unit, root, connWire, wire, capacity, ids, counters, saveLost string
	saves                                                                   []string
}

func retainMessageRows(t *testing.T) []retainMessageRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	conns := map[string]string{}
	var raw [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) == 0 || strings.HasPrefix(x[0], "#") {
			continue
		}
		switch x[0] {
		case "connection":
			conns[x[1]] = x[4]
		case "retain-message":
			raw = append(raw, x)
		}
	}
	var rows []retainMessageRow
	for _, x := range raw {
		if len(x) < 11 {
			continue
		}
		wire, ok := conns[x[2]]
		if !ok {
			t.Fatalf("retain-message %s names no connection %s", x[1], x[2])
		}
		rows = append(rows, retainMessageRow{caseName: x[1], connWire: wire, unit: x[3], root: x[4], wire: x[5], capacity: x[6], ids: x[7], counters: x[8], saveLost: x[9], saves: strings.Split(x[10], ",")})
	}
	return rows
}

// TestMooRetainMessageAnswersTheManifest answers the `retain-message` rows
// (docs/SPEC-TABLES.md §3.3, §6.6): a form-2 batch loaded with retention
// against the connection's announced vocabulary, a retention buffer a body,
// and the bodies saved as FILES, which is the one direction retention crosses
// the forms. The three counters, the retain_lost the saves add and each pinned
// save byte for byte; and the Go engine's answer at the same stores.
func TestMooRetainMessageAnswersTheManifest(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := retainMessageRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no retain-message row")
	}
	for _, row := range rows {
		g := generateUnit(t, units, row.unit)
		if g.err != nil {
			t.Fatalf("unit %s: %v", row.unit, g.err)
		}
		conn := readRoot(t, row.connWire)
		batch := readRoot(t, row.wire)
		got := mooRetainMessages(t, g, row.root, conn, batch, retainFull, -1)
		if got.counters() != row.counters {
			t.Errorf("%s: load counters %s, want %s", row.caseName, got.counters(), row.counters)
		}
		if strconv.Itoa(got.saveLost) != row.saveLost {
			t.Errorf("%s: the saves lost %d, want %s", row.caseName, got.saveLost, row.saveLost)
		}
		if len(got.saves) != len(row.saves) {
			t.Fatalf("%s: %d saves, want %d", row.caseName, len(got.saves), len(row.saves))
		}
		for i, rel := range row.saves {
			if rel == "-" {
				continue
			}
			if want := readRoot(t, rel); string(got.saves[i]) != string(want) {
				t.Errorf("%s: save %d is not the pinned file %s: %s", row.caseName, i, rel, firstDiff(got.saves[i], want))
			}
		}
		want := goRetainMessages(t, g.unit, row.root, conn, batch, retainFull, -1)
		if !sameMessageOut(got, want) {
			t.Errorf("%s: moo and the Go engine disagree:\nmoo: %s\ngo:  %s", row.caseName, got, want)
		}
		// one byte under what the first body used: that body's last record is lost
		// in both engines, and the rest of the batch stands
		if len(got.used) > 0 && got.used[0] > 0 {
			short := mooRetainMessages(t, g, row.root, conn, batch, got.used[0]-1, -1)
			wantShort := goRetainMessages(t, g.unit, row.root, conn, batch, got.used[0]-1, -1)
			if !sameMessageOut(short, wantShort) {
				t.Errorf("%s at capacity %d: moo and the Go engine disagree:\nmoo: %s\ngo:  %s", row.caseName, got.used[0]-1, short, wantShort)
			}
		}
	}
}

// TestMooRetainConstructsMatchTheGoEngine is the retention differential across
// every table construct the variable wire claims (construct_test.go): the
// SENDER is the construct's own unit, the READER is the same root declared with
// one field the sender never wrote, so EVERY field of the wire is an unknown
// the reader must resolve and keep. The Go engine writes the sender's file form
// and its message form, and moo's retaining load and save and the Go engine's
// must agree on the store bytes used, the three counters and the saved bytes,
// on both forms and at a full capacity and one byte under it.
func TestMooRetainConstructsMatchTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	files, messages, retaining, messageRetaining := 0, 0, 0, 0
	for _, c := range constructCases {
		sender := unitFromSource(t, c.schema)
		reader := generateFiles(t, map[string]string{"Probe.schema": fmt.Sprintf("package probe\ntable %s\n{\n    marker uint8\n}\n", c.root)})
		file := goOracleWire(t, sender, c.root, c.json)
		bare := file
		if sender.Tables[c.root].Integrity == "crc32c" {
			bare = file[:len(file)-4]
		}
		full := goRetain(t, reader.unit, c.root, bare, retainFull, -1)
		probes := []int{retainFull}
		if full.retained > 0 {
			retaining++
			probes = append(probes, full.used-1)
		}
		for _, capacity := range probes {
			want := goRetain(t, reader.unit, c.root, bare, capacity, -1)
			got := mooRetain(t, reader, c.root, bare, capacity, -1)
			if got.used != want.used || got.counters() != want.counters() || got.value != want.value ||
				got.saveLost != want.saveLost || string(got.save) != string(want.save) {
				t.Errorf("%s (file form) at capacity %d: moo and the Go engine disagree:\nmoo: used %d %s lost %d %x\ngo:  used %d %s lost %d %x",
					c.name, capacity, got.used, got.counters(), got.saveLost, got.save, want.used, want.counters(), want.saveLost, want.save)
			}
			files++
		}
		// the message form of the same value
		var vocab tablewire.Vocabulary
		var rep tabletext.Report
		if err := vocab.AnnounceRead(ir.TableAnnouncement(sender), &rep); err != nil || !vocab.Announced() {
			continue
		}
		m := tabletext.NewModel(sender)
		inst := m.New(sender.Tables[c.root])
		var r2 tabletext.Report
		if ok, err := tablewire.Decode(m, inst, bare, &r2); !ok || err != nil {
			t.Fatalf("%s: the Go engine does not read its own wire: %v", c.name, err)
		}
		batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			continue
		}
		if sender.Tables[c.root].Integrity == "crc32c" {
			continue // a sealed batch carries the trailer of the emitted writers, not the engine's
		}
		conn := ir.TableAnnouncement(sender)
		fullMsg := goRetainMessages(t, reader.unit, c.root, conn, batch, retainFull, -1)
		if fullMsg.retained > 0 {
			messageRetaining++
		}
		mprobes := []int{retainFull}
		if len(fullMsg.used) > 0 && fullMsg.used[0] > 0 {
			mprobes = append(mprobes, fullMsg.used[0]-1)
		}
		for _, capacity := range mprobes {
			want := goRetainMessages(t, reader.unit, c.root, conn, batch, capacity, -1)
			got := mooRetainMessages(t, reader, c.root, conn, batch, capacity, -1)
			if !sameMessageOut(got, want) {
				t.Errorf("%s (message form) at capacity %d: moo and the Go engine disagree:\nmoo: %s\ngo:  %s", c.name, capacity, got, want)
			}
			messages++
		}
	}
	if retaining < 10 {
		t.Fatalf("only %d constructs retained anything: the differential proves little", retaining)
	}
	if messageRetaining < 10 {
		t.Fatalf("only %d message batches retained anything: the differential proves little", messageRetaining)
	}
	t.Logf("%d file-form and %d message-form retaining loads agree with the Go engine (%d constructs retain, %d batches retain)", files, messages, retaining, messageRetaining)
}

// TestMooRetainMessageNegativeControl plants out the message form's capture
// (the one call table_message.moo makes into the retain hook for an unknown
// entry) and requires the manifest's retain_message_tail row to go red.
func TestMooRetainMessageNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	row := retainMessageRows(t)[0]
	g := generateUnit(t, units, row.unit)
	conn, batch := readRoot(t, row.connWire), readRoot(t, row.wire)
	clean := mooRetainMessages(t, g, row.root, conn, batch, retainFull, -1)
	if clean.counters() != row.counters {
		t.Fatalf("the control is not clean: %s, want %s", clean.counters(), row.counters)
	}
	path := filepath.Join(g.dir, "table_message.moo")
	src := string(g.files["table_message.moo"])
	const anchor = "\t\t\t\t_retain.captureMessage(t, v, d, entry, at, t.r.off)\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the capture call is not in table_message.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "\t\t\t\t_retain.lost(t.report)\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mooRetainMessages(t, g, row.root, conn, batch, retainFull, -1); got.counters() == row.counters {
		t.Fatalf("the counters still match with the message capture planted out: %s", got.counters())
	}
}
