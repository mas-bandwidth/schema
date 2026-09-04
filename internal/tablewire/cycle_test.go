// A POINTER CYCLE ON A FORGED WIRE (docs/SPEC-TABLES.md §3.1, §14 note 9;
// schema#535, filed on #521 as G-19).
//
// One flipped bit in a `ListNode` chain under a `Scene` root turns the first
// node's `next` index from 3 into 2, which is that node's own index: it loads,
// the report is all zero, and the loaded node points at itself. That looks
// like a hole and is the contract, stated in §3.1 in four clauses this file
// pins one by one, because each is a thing a future reader could quietly stop
// doing:
//
//  1. LOAD ACCEPTS IT, SILENTLY. "A pointer field's payload is a NUMBER: it is
//     bounds-checked and stored, never followed. There is no traversal on the
//     load path, and therefore no traversal bound — no depth cap, no visited
//     set, no ordering rule on the indices." The index names a node that
//     exists and whose type matches, so no §4 event happened and no counter
//     may move. A reader that started following references to refuse this
//     would take on state proportional to the graph on the reading path, which
//     §6.5 forbids and §14 note 4 records as rejected.
//  2. THE WALKING CONSUMER CARRIES THE BOUND. "What a cyclic structure costs
//     is paid by whatever WALKS it, and a consumer walking untrusted table
//     data — a reflection dump, a text export (§16) — carries its own visit
//     bound." The text writer refuses this instance by name.
//  3. SO DOES A RE-ENCODE. A save from a builder refuses a data cycle (§3.1),
//     and the loaded instance is a builder's shape.
//  4. AND THE WALK TERMINATES. Both refusals return rather than recursing
//     away, which is the half a depth-limited walker would also satisfy and a
//     naive one would not.
//
// §14 note 9 prices the reader-side acyclicity check this version does not
// spend and records what shape it would take. Landing one is that ruling, not
// a fix, and this file is what would have to change to take it.
package tablewire_test

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const selfCycleVector = "../../testdata/wire/tables/fuzz-vectors/pointer_self_cycle.bin"

func pointerModel(t *testing.T) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// reachesItself walks the loaded graph with its own visit bound — the bound
// §3.1 says every walker owns — and reports whether any node is reachable from
// itself.
func reachesItself(root *tabletext.Instance) bool {
	done := map[*tabletext.Instance]bool{}
	var walk func(n *tabletext.Instance, open map[*tabletext.Instance]bool) bool
	walk = func(n *tabletext.Instance, open map[*tabletext.Instance]bool) bool {
		if n == nil {
			return false
		}
		if open[n] {
			return true
		}
		if done[n] {
			return false
		}
		done[n] = true
		open[n] = true
		for i := range n.Fields {
			f := &n.Fields[i]
			cells := make([]*tabletext.Cell, 0, 1+len(f.Elems))
			cells = append(cells, &f.Cell)
			for j := range f.Elems {
				cells = append(cells, &f.Elems[j])
			}
			for _, c := range cells {
				if walk(c.Node, open) || walk(c.Tab, open) {
					return true
				}
			}
		}
		delete(open, n)
		return false
	}
	return walk(root, map[*tabletext.Instance]bool{})
}

func TestForgedPointerCycleLoadsSilentlyAndRefusesEveryWalk(t *testing.T) {
	wire, err := os.ReadFile(selfCycleVector)
	if err != nil {
		t.Fatal(err)
	}
	m := pointerModel(t)
	inst := m.New(m.Lookup("Scene"))
	var r tabletext.Report

	// CLAUSE 1: the load accepts it and reports nothing.
	ok, derr := tablewire.Decode(m, inst, wire, &r)
	if derr != nil {
		t.Fatalf("the load errored on a wire it is specified to scan: %v", derr)
	}
	if !ok {
		t.Fatal("the load refused a forged cycle; §3.1 specifies a scan that follows nothing")
	}
	if !r.Silent() {
		t.Errorf("a counter moved for a pointer index that names a node of the right type: %+v", r)
	}

	// the vector is still the mutant it was pinned as
	if !reachesItself(inst) {
		t.Fatal("the pinned vector no longer loads a node reachable from itself — it has stopped being the case it pins")
	}

	// CLAUSE 2: the text export carries its own visit bound and refuses by name.
	text, werr := m.Write(inst)
	if werr == nil {
		t.Errorf("the text export walked a cyclic graph and produced %d bytes; §3.1 requires the walking consumer to carry the bound", len(text))
	} else if !containsAll(werr.Error(), "data cycle", "docs/SPEC-TABLES.md §3.1") {
		t.Errorf("the text export's refusal does not name the cycle and its rule: %v", werr)
	}

	// CLAUSE 3: a re-encode refuses it the same way.
	enc, eerr := tablewire.Encode(m, inst)
	if eerr == nil {
		t.Errorf("a save reproduced a data cycle as %d bytes; §3.1 refuses one at save", len(enc))
	} else if !containsAll(eerr.Error(), "data cycle", "docs/SPEC-TABLES.md §3.1") {
		t.Errorf("the save's refusal does not name the cycle and its rule: %v", eerr)
	}

	// CLAUSE 4 is this test returning at all: both refusals return rather than
	// recursing away, and the Go test timeout is the gate on that.
}

func containsAll(s string, wants ...string) bool {
	for _, w := range wants {
		found := false
		for i := 0; i+len(w) <= len(s); i++ {
			if s[i:i+len(w)] == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
