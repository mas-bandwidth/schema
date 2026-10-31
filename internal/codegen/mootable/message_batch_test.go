package mootable_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE MESSAGE FORM'S BATCH, held to the Go engine's reader over DAMAGE
// (docs/SPEC-TABLES.md §3.3): the pinned batches — a fixed root's, a
// pointered root's, a batch of three, a root that reaches no node — are
// mutated (every truncation, the form byte, the count byte, bytes after the
// pad, every byte flipped three ways), and moo's LoadMessages must answer each
// mutation as tablewire.DecodeMessages does: the same refusal by name, or the
// same number of bodies delivered with the same counters and the same
// malformed verdict. The rows are the manifest's message rows and the two
// pointered batches the harness pins (TestAPointeredBatch).

type batchCase struct {
	name, conn, root, wire string
}

var batchCases = []batchCase{
	{"login_full", "backend_conn", "LoginRequest", "testdata/wire/tables/login_full_message.bin"},
	{"match_full", "backend_conn", "MatchResult", "testdata/wire/tables/match_full_message.bin"},
	{"store_full", "backend_conn", "StorePurchase", "testdata/wire/tables/store_full_message.bin"},
	{"graph_tree", "graph_conn", "Scene", "testdata/wire/tables/graph_tree_message.bin"},
	{"graph_batch", "graph_conn", "Scene", "testdata/wire/tables/graph_batch_message.bin"},
	{"graph_empty", "graph_conn", "Scene", "testdata/wire/tables/graph_empty_message.bin"},
}

// mutations is the damage swept over one wire, each distinct, in a fixed order.
func mutations(wire []byte) [][]byte {
	seen := map[string]bool{}
	var out [][]byte
	add := func(b []byte) {
		if !seen[string(b)] {
			seen[string(b)] = true
			out = append(out, b)
		}
	}
	add(wire)
	for n := range wire {
		add(append([]byte(nil), wire[:n]...))
	}
	for _, tail := range [][]byte{{0}, {0xFF}, {0, 0}} {
		add(append(append([]byte(nil), wire...), tail...))
	}
	for _, form := range []byte{0, 1, 3, 4, 0xFF} {
		b := append([]byte(nil), wire...)
		b[0] = form
		add(b)
	}
	for _, count := range []byte{0, 1, 2, 3, 0x7F, 0xFF} {
		b := append([]byte(nil), wire...)
		b[1] = count
		add(b)
	}
	for i := 1; i < len(wire); i++ {
		for _, f := range []func(byte) byte{func(x byte) byte { return x ^ 0xFF }, func(x byte) byte { return x ^ 1 }, func(byte) byte { return 0 }} {
			b := append([]byte(nil), wire...)
			b[i] = f(b[i])
			add(b)
		}
	}
	return out
}

func packCases(cases [][]byte) []byte {
	var out []byte
	for _, c := range cases {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c)))
		out = append(out, c...)
	}
	return out
}

func damageProbe(g *generatedUnit, root string, capacity int) string {
	return fmt.Sprintf(`--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
%slocal cases = inputs.cases
local at = 0
local i = 0
while at < buffer.len(cases) do
	local len = buffer.readu32(cases, at)
	local data = buffer.create(len)
	buffer.copy(data, 0, cases, at + 4, len)
	at += 4 + len
	i += 1
	local v = MS.NewVocabulary()
	local rep: any = {}
	if not MS.AnnounceRead(v, inputs.conn, rep) then print("ANNOUNCE " .. tostring(rep.reason)) return end
	local n = MS.LoadMessages(v, %q, data, %d, rep)
	print(string.format("%%d %%d %%d,%%d,%%d,%%d,%%d,%%s,%%d %%s", i, n, rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, tostring(rep.malformed), rep.blobShared or 0, tostring(rep.reason)))
end
`, g.requireAll(), root, capacity)
}

// goAnswer is the Go engine's answer to one mutation in moo's line format.
func goAnswer(m *tabletext.Model, vocab *tablewire.Vocabulary, root string, capacity int, data []byte) string {
	insts := make([]*tabletext.Instance, capacity)
	for i := range insts {
		insts[i] = m.New(m.Lookup(root))
	}
	var rep tabletext.Report
	n, _, err := tablewire.DecodeMessages(m, insts, data, vocab, &rep)
	if err != nil {
		var reason string
		if r, ok := errors.AsType[*tablewire.MessageRefusal](err); ok {
			reason = r.Reason.String()
			if r.Reason != tablewire.ReasonBatchTooLarge {
				n = -1
			}
		} else {
			reason = "non_finite_float"
			n = 0
		}
		return fmt.Sprintf("%d %s", n, reason)
	}
	return fmt.Sprintf("%d %d,%d,%d,%d,%d,%v,%d", n, rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed, rep.BlobShared)
}

// mooAnswer reduces a moo line (without its index) to goAnswer's format.
func mooAnswer(line string) string {
	f := strings.SplitN(line, " ", 3)
	if len(f) < 3 {
		return line
	}
	if f[2] != "nil" {
		reason, _, _ := strings.Cut(f[2], ":")
		return fmt.Sprintf("%s %s", f[0], reason)
	}
	return fmt.Sprintf("%s %s", f[0], f[1])
}

func runDamage(t *testing.T, capacity int, mutate func([]byte) [][]byte) {
	t.Helper()
	units, _ := readManifest(t)
	graph := "graphdemo"
	cache := map[string]*generatedUnit{}
	total := 0
	for _, c := range batchCases {
		unit := graph
		if c.conn != "graph_conn" {
			unit = "backenddemo"
		}
		g, ok := cache[unit]
		if !ok {
			g = generateUnit(t, units, unit)
			cache[unit] = g
		}
		if g.err != nil {
			t.Fatalf("%s: unit %s is refused: %v", c.name, unit, g.err)
		}
		conn := readRoot(t, "testdata/wire/tables/"+c.conn+".bin")
		wire := readRoot(t, c.wire)
		cases := mutate(wire)
		m := tabletext.NewModel(g.unit)
		var vocab tablewire.Vocabulary
		var arep tabletext.Report
		if err := vocab.AnnounceRead(conn, &arep); err != nil {
			t.Fatal(err)
		}
		out := g.run(t, damageProbe(g, c.root, capacity), map[string][]byte{"conn": conn, "cases": packCases(cases)})
		lines := strings.Split(out, "\n")
		if len(lines) != len(cases) {
			t.Fatalf("%s: moo answered %d of %d cases:\n%s", c.name, len(lines), len(cases), out)
		}
		bad := 0
		for i, line := range lines {
			_, rest, _ := strings.Cut(line, " ")
			want := goAnswer(m, &vocab, c.root, capacity, cases[i])
			if got := mooAnswer(rest); got != want {
				if bad++; bad <= 5 {
					t.Errorf("%s: case %d (%d bytes: %x): moo %q (%s), the Go engine %q", c.name, i, len(cases[i]), cases[i], got, rest, want)
				}
			}
		}
		total += len(cases)
	}
	t.Logf("%d damaged batches answered as the Go engine answers them", total)
}

func TestMooMessageBatchDamageMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	runDamage(t, 256, mutations)
}

// TestMooMessageBatchCapacityMatchesTheGoEngine: a batch above the caller's
// capacity is batch_too_large with the wire's count and nothing decoded, at
// every capacity from one body up.
func TestMooMessageBatchCapacityMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	for _, capacity := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			t.Parallel()
			runDamage(t, capacity, func(w []byte) [][]byte { return [][]byte{w} })
		})
	}
}

// TestMooMessageBatchWriteBounds: SaveMessages and MeasureMessages refuse a
// batch of none or of 257 by name, batch_too_large, and write 256.
func TestMooMessageBatchWriteBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local v = load.ReadStrict("LoginRequest", inputs.file, load.newReport())
assert(v ~= nil)
for _, n in { 0, 1, 255, 256, 257, 300 } do
	local values = {}
	for i = 1, n do values[i] = v end
	local rep: any = {}
	local size = MS.MeasureMessages("LoginRequest", values, rep)
	local out, saved = MS.SaveMessages("LoginRequest", values, rep)
	print(string.format("%d %d %s %s %s", n, size, tostring(out ~= nil), tostring(saved), tostring(rep.reason)))
end
`
	got := g.run(t, main, map[string][]byte{"file": readRoot(t, "testdata/wire/tables/login_full.bin")})
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("unexpected answer:\n%s", got)
	}
	for i, n := range []int{0, 1, 255, 256, 257, 300} {
		f := strings.Fields(lines[i])
		refused := n < 1 || n > 256
		if refused && (f[1] != "-1" || f[2] != "false" || f[4] != "batch_too_large") {
			t.Errorf("a batch of %d is not refused batch_too_large: %s", n, lines[i])
		}
		if !refused && (f[2] != "true" || f[1] != f[3] || f[1] == "-1") {
			t.Errorf("a batch of %d does not save at its measure: %s", n, lines[i])
		}
	}
}

// TestMooMessageBatchDamageNegativeControl: the sweep goes red when the pad
// rule is dropped — a reader that no longer verifies the trailing pad and the
// bytes after it answers a clean batch where the Go engine answers malformed.
func TestMooMessageBatchDamageNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	src := string(g.files["table_message.moo"])
	const anchor = "if got == count and (not ralign(r) or r.off ~= r.n) then"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the pad rule's anchor is not in table_message.moo exactly once")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(strings.Replace(src, anchor, "if false then", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := readRoot(t, "testdata/wire/tables/backend_conn.bin")
	wire := readRoot(t, "testdata/wire/tables/login_full_message.bin")
	cases := [][]byte{append(append([]byte(nil), wire...), 0)}
	out := g.run(t, damageProbe(g, "LoginRequest", 256), map[string][]byte{"conn": conn, "cases": packCases(cases)})
	_, rest, _ := strings.Cut(out, " ")
	if !strings.Contains(rest, ",false,") {
		t.Fatalf("the patched reader still answers the byte after the pad as malformed: %s", out)
	}
}

// TestMooMessageOverAPointeredRootMatchesTheGoEngine: the message form over a
// plain, pointered `table` (docs/SPEC-TABLES.md §3.3, "The message form over
// a variable root"), the way TestMooMessageFormMatchesTheGoEngine holds a
// fixed one. For each graph instance of the manifest (a tree, a shared node, a
// root that reaches nothing, a deep chain) the Go engine writes the file wire
// and its message form; moo reads the file wire and saves the same message
// bytes, and loads the Go engine's message into the file wire the Go engine
// itself reads back.
func TestMooMessageOverAPointeredRootMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	m := tabletext.NewModel(g.unit)
	var vocab tablewire.Vocabulary
	var arep tabletext.Report
	if err := vocab.AnnounceRead(readRoot(t, "testdata/wire/tables/graph_conn.bin"), &arep); err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, row := range manifestRows(t) {
		if row.kind != "instance" || row.unit != "graphdemo" {
			continue
		}
		file := readRoot(t, row.wire)
		inst := m.New(m.Lookup(row.root))
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, inst, file, &rep); !ok || err != nil {
			t.Fatalf("%s: the Go engine does not read its own wire: %v", row.caseName, err)
		}
		msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("%s: the Go engine writes no message for a pointered root: %v", row.caseName, err)
		}
		back := m.New(m.Lookup(row.root))
		var r3 tabletext.Report
		if n, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{back}, msg, &vocab, &r3); err != nil || n != 1 {
			t.Fatalf("%s: the Go engine does not read its own batch: %v", row.caseName, err)
		}
		backFile, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatal(err)
		}
		out := parseProbe(g.run(t, messageDiffProbe(g, tableModuleOf(g, row.root), row.root), map[string][]byte{"file": file, "message": msg}))
		if rep.BlobShared > 0 {
			// a blob named from two slots: the strict read refuses it and no writer carries it
			t.Logf("%s: shares a blob (the strict read refuses); moo: %v", row.caseName, out)
			continue
		}
		if out["one"] != fmt.Sprintf("%x", msg) {
			t.Errorf("%s: moo's message is not the Go engine's:\nmoo: %v\ngo:  %x", row.caseName, out, msg)
			continue
		}
		if out["load"] != "1 0,0,0,0,0,false" || out["file"] != fmt.Sprintf("%x", backFile) {
			t.Errorf("%s: moo does not load the Go engine's message into the Go engine's file form:\nmoo:  %v\nfile: %x", row.caseName, out, backFile)
			continue
		}
		compared++
	}
	if compared < 3 {
		t.Fatalf("only %d graph instances compared", compared)
	}
	t.Logf("%d graph instances compared both ways", compared)
}
