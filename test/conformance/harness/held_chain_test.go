package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A held chain vector is a framing vector the law-home keeps out of the
// manifest: its bytes are pinned beside the manifest's vectors, and this test
// holds the engine to the verdict and to the value it carries. The rule is
// docs/SPEC-TABLES.md §3.1: the node table is flat, so a chain is not a depth
// and every pointer is resolved by the same linear scan, whatever the chain's
// length. The owned value is the whole chain along `link`, in bounded stack.
type heldChainVector struct {
	name, unit, root string
	// accepted is the verdict the vector owes (docs/SPEC-TABLES.md §4).
	accepted bool
	// chain is the number of `node` records a read value holds along `link`,
	// when the verdict is accepted.
	chain int
	// node, link and next name the record table, the root's pointer field and
	// the record's own pointer field.
	node, link, next string
}

var heldChainVectors = []heldChainVector{
	{
		name: "node_chain_200000", unit: "graphdemo", root: "Scene",
		accepted: true, chain: 200000,
		node: "ListNode", link: "head", next: "next",
	},
}

// TestHeldChainVectorAnswersTheEngine is the oracle check for the held chain
// vectors: each pinned file is read as TestFramingVectorsReadThroughTheEngine
// reads a manifest row (strictDecode on the named unit and root, the verdict
// alone and never a reason), and an accepted value must hold the whole chain
// along its link field.
func TestHeldChainVectorAnswersTheEngine(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	for _, v := range heldChainVectors {
		v := v
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			unit, err := u.get(v.unit)
			if err != nil {
				t.Fatal(err)
			}
			if st := unit.Tables[v.root]; st != nil && st.FixedDeclared {
				if unit, err = framingVariableUnit(m, v.unit, v.root); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(v.root)
			if def == nil {
				t.Fatalf("%s: the unit declares no root %q", v.name, v.root)
			}
			inst := model.New(def)
			var report tabletext.Report
			accepted, _ := strictDecode(model, inst, data, &report)
			if accepted != v.accepted {
				t.Fatalf("%s: accepted=%t, the vector owes %t", v.name, accepted, v.accepted)
			}
			if !accepted {
				return
			}
			if got := heldChainLength(t, inst, v); got != v.chain {
				t.Errorf("%s: the value holds %d %s records along %s, want %d", v.name, got, v.node, v.link, v.chain)
			}
		})
	}
}

// heldChainLength walks the value's link fields to null, requiring every
// record to be the declared node table, and stops one past the expected length
// so a value that never reaches null cannot stall the test.
func heldChainLength(t *testing.T, inst *tabletext.Instance, v heldChainVector) int {
	t.Helper()
	field, ok := inst.FieldByKey(v.link)
	if !ok {
		t.Fatalf("%s: %s declares no field %q", v.name, v.root, v.link)
	}
	node := field.Cell.Node
	for seen := 0; ; seen++ {
		if node == nil {
			return seen
		}
		if node.Def == nil || node.Def.Name != v.node {
			t.Fatalf("%s: link %d is not a %s record", v.name, seen, v.node)
		}
		if seen >= v.chain {
			t.Fatalf("%s: the chain runs past %d %s records", v.name, v.chain, v.node)
		}
		next, ok := node.FieldByKey(v.next)
		if !ok {
			t.Fatalf("%s: %s declares no field %q", v.name, v.node, v.next)
		}
		node = next.Cell.Node
	}
}
