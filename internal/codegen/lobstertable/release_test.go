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
let rec = schema_rt.load_tolerant("ListNode", data, rep)
print("malformed={rep.malformed} answered=" + (if rec: "record" else: "nil"))
if rec:
    let out, why = schema_rt.save_reasoned(rec)
    print("save=" + (if why == "": "ok" else: why))
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
	cycleRefused = "malformed=true answered=nil\n"
)

// TestLobsterTableLoadUnlinksWhatTheRootDoesNotReach: an orphan cycle (B and
// C naming each other, the root naming neither) is damage, and the refused
// load ends it itself, so a caller that is handed nothing leaks nothing.
func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReach(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 2, true), false, nil)
	if !strings.Contains(out, cycleRefused) || strings.Contains(out, leakReport) {
		t.Fatalf("an orphan cycle was not refused, or outlived the load:\n%s", out)
	}
}

// TestLobsterTableLoadUnlinksWhatTheRootDoesNotReachNegativeControl takes the
// load's unlink pass out and requires the refused orphan cycle to leak.
func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReachNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "\n        unlink_unreached(st, root)\n"
	out := runRelease(t, cyclicListWire(t, 2, true), false, func(src string) string {
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("the unlink anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, anchor, "\n        if length(st.nodes) < 0:\n            unlink_unreached(st, root)\n", 1)
	})
	if !strings.Contains(out, leakReport) {
		t.Fatalf("NEGATIVE CONTROL FAILED: without the unlink pass the orphan cycle still did not leak:\n%s", out)
	}
}

// TestLobsterTableReleaseEndsAReachedCycle: root -> B -> C -> root is damage,
// and the refused load releases what it placed before it answers nil.
func TestLobsterTableReleaseEndsAReachedCycle(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 1, false), true, nil)
	if !strings.Contains(out, cycleRefused) || strings.Contains(out, leakReport) {
		t.Fatalf("a reached cycle was not refused, or outlived the load:\n%s", out)
	}
}

// TestLobsterTableReleaseEndsAReachedCycleNegativeControl drops the release
// the tolerant read does on damage and requires the refused reached cycle to
// leak: the witness that the test above loads a real cycle.
func TestLobsterTableReleaseEndsAReachedCycleNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "    if report.malformed:\n        release(rec)\n        return nil\n"
	out := runRelease(t, cyclicListWire(t, 1, false), false, func(src string) string {
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("the damage release anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, anchor, "    if report.malformed:\n        return nil\n", 1)
	})
	if !strings.Contains(out, leakReport) {
		t.Fatalf("NEGATIVE CONTROL FAILED: without the release on damage the refused reached cycle did not leak:\n%s", out)
	}
}

// TestLobsterTableCyclicWireIsDamageNegativeControl takes the cycle check out
// and requires a reached cycle to load as a record, unrefused: the witness
// that the refusal above is the check's and no other rule's.
func TestLobsterTableCyclicWireIsDamageNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "and load_cyclic(st, root):"
	out := runRelease(t, cyclicListWire(t, 1, false), true, func(src string) string {
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("the cycle check anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, anchor, "and false and load_cyclic(st, root):", 1)
	})
	if !strings.Contains(out, "malformed=false answered=record\n") {
		t.Fatalf("NEGATIVE CONTROL FAILED: without the cycle check the reached cycle was still refused:\n%s", out)
	}
}
