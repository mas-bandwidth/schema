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
// §3.1, §4; Glenn, 2026-10-31), so the Lobster load owes the refusal; until it
// carries it, these tests load a cyclic ListNode wire of tables/pointers and
// hold the exit report empty, with a reversed witness for each of the two
// rules. When the Lobster load refuses, they become refusal tests.

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

// runRelease loads the wire at ListNode, saves it (so the graph is walked as
// a caller walks it), releases it when release is set, and returns the whole
// of stdout, which carries the VM's exit-time leak report.
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
print("loaded unknown={rep.unknown} malformed={rep.malformed} rec=" + (if rec: "yes" else: "no"))
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

const leakReport = "LEAKS FOUND"

// TestLobsterTableLoadUnlinksWhatTheRootDoesNotReach: an orphan cycle (B and
// C naming each other, the root naming neither) is damage (§3.1, §4), so the
// load refuses it and leaks nothing.
func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReach(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 2, true), false, nil)
	if strings.Contains(out, leakReport) || !strings.Contains(out, "malformed=true") || !strings.Contains(out, "rec=no") {
		t.Fatalf("an orphan cyclic wire was not refused as damage:\n%s", out)
	}
}

// TestLobsterTableLoadUnlinksWhatTheRootDoesNotReachNegativeControl removes the
// cycle refusal and the unlink pass and requires the orphan cycle to leak: the
// witness that the refusal above is what ends it.
func TestLobsterTableLoadUnlinksWhatTheRootDoesNotReachNegativeControl(t *testing.T) {
	t.Parallel()
	const block = "        if not st.report.malformed and load_cyclic(st, root):\n            st.report.malformed = true\n        unlink_unreached(st, root)\n"
	out := runRelease(t, cyclicListWire(t, 2, true), false, func(src string) string {
		if n := strings.Count(src, block); n != 1 {
			t.Fatalf("the cycle-refusal and unlink block is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, block, "        st.report.stopped = st.report.stopped\n", 1)
	})
	if !strings.Contains(out, leakReport) {
		t.Fatalf("NEGATIVE CONTROL FAILED: without the cycle refusal and unlink the orphan cycle still did not leak:\n%s", out)
	}
}

// TestLobsterTableReleaseEndsAReachedCycle: root -> B -> C -> root is damage
// too, so the load refuses it and leaks nothing.
func TestLobsterTableReleaseEndsAReachedCycle(t *testing.T) {
	t.Parallel()
	out := runRelease(t, cyclicListWire(t, 1, false), true, nil)
	if strings.Contains(out, leakReport) || !strings.Contains(out, "malformed=true") || !strings.Contains(out, "rec=no") {
		t.Fatalf("a cyclic reached wire was not refused as damage:\n%s", out)
	}
}

// TestLobsterTableReleaseEndsAReachedCycleNegativeControl removes the cycle
// refusal and requires the reached cycle to leak: the witness that the refusal
// above is what ends it.
func TestLobsterTableReleaseEndsAReachedCycleNegativeControl(t *testing.T) {
	t.Parallel()
	const cyc = "        if not st.report.malformed and load_cyclic(st, root):\n            st.report.malformed = true\n"
	out := runRelease(t, cyclicListWire(t, 1, false), false, func(src string) string {
		if n := strings.Count(src, cyc); n != 1 {
			t.Fatalf("the cycle-refusal anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, cyc, "", 1)
	})
	if !strings.Contains(out, leakReport) {
		t.Fatalf("NEGATIVE CONTROL FAILED: without the cycle refusal the reached cycle still did not leak:\n%s", out)
	}
}
