// The FLAT NODE TABLE's numbering (SPEC-TABLES.md §3.1), which is also the
// order a region is packed in (§6.3) and therefore the order a COOK lays its
// nodes out in (§7).
//
// The numbering is DETERMINISTIC AND RE-DERIVED, NEVER CARRIED: measure derives
// it from the graph and save derives the same one from the same graph, and
// nothing passes between them. That is what makes `measure == save` hold across
// a pointer graph, and it is why a cook can reproduce a wire's node order from
// the graph alone.
package tablewire

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// NodeGraph is one save's numbering: the nodes in index order, with the root
// first. Node index `1` is the root and record `k` (1-based) is index `k + 1`,
// so Nodes[i] is node index `i + 1` and Nodes[0] is the root (§3.1).
type NodeGraph struct {
	Nodes []*tabletext.Instance
	index map[*tabletext.Instance]uint32
}

// Index is the node index of one instance, or [ir.NodeIndexNull] for a nil
// referent — absence and null are one value (§3.1).
func (g *NodeGraph) Index(inst *tabletext.Instance) uint32 {
	if inst == nil {
		return ir.NodeIndexNull
	}
	return g.index[inst]
}

// Records is the numbering's node-table records: every node but the root, in
// index order. The root carries no record — it IS the body that hosts the table
// — which is why an index of `1` is checked against the reader's own root type
// rather than against a wire type id (§3.1).
func (g *NodeGraph) Records() []*tabletext.Instance { return g.Nodes[1:] }

// Number derives the numbering of one root instance: the FIRST-VISIT order of a
// depth-first pre-order walk from the root over POINTER EDGES ONLY — fields in
// declaration order, array elements in index order, and descending through
// every by-value edge there is (a nested table, an element of a bounded or
// enum-keyed array, a member of a true `if` group, a present optional's value,
// a union's set arm) to reach the pointer fields inside them. A node takes its
// index the first time it is reached and never again.
//
// A DATA CYCLE IS REFUSED HERE, and the refusal is free: the walk carries one
// entry per reachable node — that map IS identity, since a node must know its
// index to be named twice — so colouring each entry while its descent is open
// costs one bit. A reference to an entry still open is a cycle, named, and
// measure, save, Cook and Lock all return failure. Nothing recurses away.
func Number(m *tabletext.Model, root *tabletext.Instance) (*NodeGraph, error) {
	g := &NodeGraph{index: map[*tabletext.Instance]uint32{}}
	open := map[*tabletext.Instance]bool{}
	g.Nodes = append(g.Nodes, root)
	g.index[root] = ir.NodeIndexRoot
	open[root] = true
	if err := g.descend(m, root, open); err != nil {
		return nil, err
	}
	open[root] = false
	return g, nil
}

// descend walks one node's BY-VALUE closure looking for pointer edges. It is
// the walk, and [visitEdges] is the shape of the closure it walks.
func (g *NodeGraph) descend(m *tabletext.Model, inst *tabletext.Instance, open map[*tabletext.Instance]bool) error {
	var err error
	visitEdges(m, inst, func(target *tabletext.Instance, field string) bool {
		if err != nil {
			return false
		}
		if open[target] {
			err = fmt.Errorf("data cycle: %s.%s reaches %s, which is still open — a cycle is refused at save and at Lock, and nothing recurses away (SPEC-TABLES.md §3.1)", inst.Def.Name, field, target.Def.Name)
			return false
		}
		if _, seen := g.index[target]; seen {
			return true // one index, one node: a shared node is reached again and numbered once
		}
		g.index[target] = uint32(len(g.Nodes)) + 1
		g.Nodes = append(g.Nodes, target)
		open[target] = true
		if e := g.descend(m, target, open); e != nil {
			err = e
			return false
		}
		open[target] = false
		return true
	})
	return err
}

// visitEdges calls visit for every POINTER EDGE this instance's by-value
// closure carries, in the walk's order, and descends through every by-value
// edge to reach them. It stops when visit returns false.
//
// **A field the writer does not write is not an edge** (§3.1): a pointer under
// a false guard, or inside an absent optional, or in an array slot past the
// live count, or in a union arm that is not set, is not visited and its target
// takes no index — so a save never writes a record that no written field names.
//
// The one place this walk is deliberately LOOSER than the writer is a by-value
// nesting that would ELIDE: the writer drops an all-default nested table, an
// all-default keyed slot and an all-default fixed array, and this walk descends
// into them anyway. It costs nothing and cannot diverge, because a by-value
// body holding a NON-NULL pointer is never all-default — a kind `17` field is
// seven bytes whatever index it carries — so every body this walk reaches an
// edge through is a body the writer writes.
func visitEdges(m *tabletext.Model, inst *tabletext.Instance, visit func(target *tabletext.Instance, field string) bool) {
	guards := tabletext.Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if terms, guarded := guards[f.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if f.Type.Optional && !fv.Present {
			continue
		}
		if f.Type.Pointer {
			if fv.Cell.Node != nil && !visit(fv.Cell.Node, f.Name) {
				return
			}
			continue
		}
		if un := tabletext.UnionOf(f); un != nil {
			if fv.Cell.U != 0 && fv.Cell.Tab != nil {
				visitEdges(m, fv.Cell.Tab, visit)
			}
			continue
		}
		st := tabletext.StructOf(f)
		if st == nil {
			continue
		}
		switch {
		case f.KeyEnum != "":
			// slot 0 is None's and no walk ever reaches it (§2.4)
			for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
				if sub := fv.Elems[slot].Tab; sub != nil {
					visitEdges(m, sub, visit)
				}
			}
		case f.Array == ir.ArrayFixed:
			for k := range fv.Elems {
				if sub := fv.Elems[k].Tab; sub != nil {
					visitEdges(m, sub, visit)
				}
			}
		case f.Array == ir.ArrayCounted:
			// only the LIVE elements ride, so only they carry edges
			for k := 0; k < fv.Count && k < len(fv.Elems); k++ {
				if sub := fv.Elems[k].Tab; sub != nil {
					visitEdges(m, sub, visit)
				}
			}
		default:
			if fv.Cell.Tab != nil {
				visitEdges(m, fv.Cell.Tab, visit)
			}
		}
	}
}
