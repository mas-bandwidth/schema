package lobstertable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// RELEASE (docs/PORT-LOBSTER.md, THE LOAD): Lobster frees by reference
// counting, so a cycle of Records outlives the caller, and the VM reports such
// records at exit ("LEAKS FOUND"). A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md
// §3.1, §4; Glenn, 2026-10-31): the load refuses it as malformed and answers
// nil, and the refusal itself must leak nothing. These tests load a cyclic
// ListNode wire of tables/pointers, hold the refusal and an empty exit report,
// and carry a reversed witness for the cycle check and for each of the two
// rules that end what the refused load placed.

// cyclicListWire writes a four-node ListNode chain root -> B -> C -> D through
// the engine and rewrites C's pointer (node index 4, D) to name `to`, which
// closes a cycle the engine's writer would refuse to write: to 2 makes B and
// C an orphan cycle once the root's own pointer is nulled (orphan), to 1 makes
// root -> B -> C -> root, reached from the root.
func cyclicListWire(t *testing.T, to byte, orphan bool) []byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "pointers")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("ListNode")
	if st == nil {
		t.Fatal("tables/pointers declares no ListNode")
	}
	nodes := make([]*tabletext.Instance, 4)
	for i := range nodes {
		nodes[i] = m.New(st)
		v, ok := nodes[i].FieldByKey("value")
		if !ok {
			t.Fatal("ListNode has no value")
		}
		v.Cell.I = int64(0x41 + i)
	}
	for i := range 3 {
		next, ok := nodes[i].FieldByKey("next")
		if !ok {
			t.Fatal("ListNode has no next")
		}
		next.Cell.Node = nodes[i+1]
	}
	wire, err := tablewire.Encode(m, nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	patch := func(from, to byte) {
		pat := []byte{0x11, from}
		if n := bytes.Count(wire, pat); n != 1 {
			t.Fatalf("pointer %d is on the wire %d times, want 1: %x", from, n, wire)
		}
		at := bytes.Index(wire, pat)
		wire[at+1] = to
	}
	// the root's own pointer first, while index 2 is named once
	if orphan {
		patch(2, 0)
	}
	patch(4, to)
	return wire
}

// runRelease loads the wire at ListNode, tolerantly, and prints the report's
// malformed flag and whether the load answered a record; a record it does
// answer is saved (so the graph is walked as a caller walks it) and released
// when release is set. It returns the whole of stdout, which carries the VM's
// exit-time leak report. patch, when set, rewrites the emitted load.lobster.
func runRelease(t *testing.T, wire []byte, release bool, patch func(string) string) string {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix([]string{"tables/pointers"}))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "load.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(`let data = read_file("w0")
assert data
let rep = schema_rt.LoadReport {}
let rec = schema_rt.load("ListNode", data, rep)
print("answered=" + (if rec: "record" else: "nil"))
if rec:
    let out, why = schema_rt.save_reasoned(rec)
    print("save=" + (if why == "": "accepted" else: "refused") + " bytes={length(out)}")
`)
	if release {
		b.WriteString("    schema_rt.release(rec)\n")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"w0": wire}))
}

const (
	leakReport   = "LEAKS FOUND"
	cycleRefused = "answered=nil\n"
)

// Wire cycles refuse before allocation. Test cleanup against deliberately
// constructed in-memory cycles as a separate boundary.
func runMemoryCycle(t *testing.T, orphan, cleanup bool) string {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix([]string{"tables/pointers"}))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`import .load
def link(source:schema_rt.Record, target:schema_rt.Record):
    for(source.fields) sl:
        if sl.fd.name == "next":
            sl.cell.node = target
            return
    assert false
def exercise():
    let root = schema_rt.new_record(schema_rt.table_of("ListNode"))
    let b = schema_rt.new_record(schema_rt.table_of("ListNode"))
    let c = schema_rt.new_record(schema_rt.table_of("ListNode"))
    link(b, c)
`)
	if orphan {
		b.WriteString(`    link(c, b)
    let state = schema_rt.LoadState { data: "", ids: [], report: schema_rt.LoadReport {}, variable: true, good: true, root: root, nodes: [schema_rt.Node { kind: 1, rec: b, blob: nil }, schema_rt.Node { kind: 1, rec: c, blob: nil }], named: [], count_refused: false }
`)
		if cleanup {
			b.WriteString("    schema_rt.unlink_unreached(state, root)\n")
		} else {
			b.WriteString("    assert length(state.nodes) == 2\n")
		}
	} else {
		b.WriteString("    link(root, b)\n    link(c, root)\n")
		if cleanup {
			b.WriteString("    schema_rt.release(root)\n")
		}
	}
	b.WriteString("exercise()\nprint(\"cycle exercised\")\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return string(lobsterrun.Run(t, g.dir, "main.lobster", nil))
}

func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReach(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 2, true), false, nil)
	if !strings.Contains(out, cycleRefused) || strings.Contains(out, leakReport) {
		t.Fatalf("orphan wire was accepted or leaked:\n%s", out)
	}
	out = runMemoryCycle(t, true, true)
	if !strings.Contains(out, "cycle exercised") || strings.Contains(out, leakReport) {
		t.Fatalf("orphan cleanup failed:\n%s", out)
	}
}

func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReachNegativeControl(t *testing.T) {
	t.Parallel()
	if out := runMemoryCycle(t, true, false); !strings.Contains(out, leakReport) {
		t.Fatalf("omitting orphan cleanup did not expose the leak:\n%s", out)
	}
}

func TestLobsterTableReleaseEndsAReachedCycle(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 1, false), true, nil)
	if !strings.Contains(out, cycleRefused) || strings.Contains(out, leakReport) {
		t.Fatalf("cyclic wire was accepted or leaked:\n%s", out)
	}
	out = runMemoryCycle(t, false, true)
	if !strings.Contains(out, "cycle exercised") || strings.Contains(out, leakReport) {
		t.Fatalf("reached cycle cleanup failed:\n%s", out)
	}
}

func TestLobsterTableReleaseEndsAReachedCycleNegativeControl(t *testing.T) {
	t.Parallel()
	if out := runMemoryCycle(t, false, false); !strings.Contains(out, leakReport) {
		t.Fatalf("omitting release did not expose the leak:\n%s", out)
	}
}

// Both generic wire validation and the declared graph check reject cycles.
// Disabling both defenses proves the fixture carries a reached cycle.
func TestLobsterTableCyclicWireIsDamageNegativeControl(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 1, false), true, func(src string) string {
		const generic = "    if not wire_check(st, first):"
		const declared = "and load_cyclic(st, root):"
		if strings.Count(src, generic) != 1 || strings.Count(src, declared) != 1 {
			t.Fatal("cycle guards not unique")
		}
		src = strings.Replace(src, generic, "    if false:", 1)
		return strings.Replace(src, declared, "and false and load_cyclic(st, root):", 1)
	})
	if !strings.Contains(out, "answered=record\n") || strings.Contains(out, leakReport) {
		t.Fatalf("cycle-check mutation did not expose a releasable cycle:\n%s", out)
	}
}
