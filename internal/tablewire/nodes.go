// The FLAT NODE TABLE's numbering (docs/SPEC-TABLES.md §3.1), which is also the
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

// Node is one numbered node: a table instance, or a BYTE BUFFER's blob
// (docs/SPEC-TABLES.md §2.5). Exactly one of Inst and Blob is set; a blob
// carries the field kind that named it, because a `*bytes` blob and a
// `*string` blob ride under two reserved type ids (§3.1) and the node itself
// does not know which slot reached it.
type Node struct {
	Inst *tabletext.Instance
	Blob *tabletext.Blob
	Kind ir.FieldTypeKind // a blob's: TBytes or TString
}

// Key is the node's identity for the numbering's map: the instance or the blob.
func (n Node) Key() any {
	if n.Blob != nil {
		return n.Blob
	}
	return n.Inst
}

// TypeId is the record's wire type id (docs/SPEC-TABLES.md §3.1): the table's
// name hash, or a blob's reserved id.
func (n Node) TypeId() uint64 {
	if n.Blob != nil {
		if n.Kind == ir.TString {
			return ir.StringWireTypeId
		}
		return ir.BytesWireTypeId
	}
	return ir.TableWireId(n.Inst.Def.Name)
}

// Name spells the node for a diagnostic.
func (n Node) Name() string {
	if n.Blob != nil {
		if n.Kind == ir.TString {
			return "*string"
		}
		return "*bytes"
	}
	return n.Inst.Def.Name
}

// NodeGraph is one save's numbering: the nodes in index order, with the root
// first. Node index `1` is the root and record `k` (1-based) is index `k + 1`,
// so Nodes[i] is node index `i + 1` and Nodes[0] is the root (§3.1).
type NodeGraph struct {
	Nodes []Node
	index map[any]uint64
}

// Index is the node index of one instance, or [ir.NodeWireIndexNull] for a nil
// referent — absence and null are one value (§3.1).
func (g *NodeGraph) Index(inst *tabletext.Instance) uint64 {
	if inst == nil {
		return ir.NodeWireIndexNull
	}
	return g.index[inst]
}

// BlobIndex is the node index of one blob, or [ir.NodeWireIndexNull] for nil.
func (g *NodeGraph) BlobIndex(blob *tabletext.Blob) uint64 {
	if blob == nil {
		return ir.NodeWireIndexNull
	}
	return g.index[blob]
}

// Records is the numbering's node-table records: every node but the root, in
// index order. The root carries no record — it IS the body that hosts the table
// — which is why an index of `1` is checked against the reader's own root type
// rather than against a wire type id (§3.1).
func (g *NodeGraph) Records() []Node { return g.Nodes[1:] }

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
	g := &NodeGraph{index: map[any]uint64{}}
	open := map[any]bool{}
	g.Nodes = append(g.Nodes, Node{Inst: root})
	g.index[root] = ir.NodeWireIndexRoot
	open[root] = true
	if err := g.descend(m, root, open); err != nil {
		return nil, err
	}
	open[root] = false
	return g, nil
}

// descend walks one node's BY-VALUE closure looking for pointer edges. It is
// the walk, and [visitEdges] is the shape of the closure it walks. A BYTE
// BUFFER's blob is numbered as every node is and reaches nothing, so its
// descent closes at once (§2.5, §3.1).
func (g *NodeGraph) descend(m *tabletext.Model, inst *tabletext.Instance, open map[any]bool) error {
	var err error
	visitEdges(m, inst, func(target Node, field string) bool {
		if err != nil {
			return false
		}
		key := target.Key()
		if open[key] {
			err = fmt.Errorf("data cycle: %s.%s reaches %s, which is still open — a cycle is refused at save and at Lock, and nothing recurses away (docs/SPEC-TABLES.md §3.1)", inst.Def.Name, field, target.Name())
			return false
		}
		if _, seen := g.index[key]; seen {
			return true // one index, one node: a shared node is reached again and numbered once
		}
		g.index[key] = uint64(len(g.Nodes)) + 1
		g.Nodes = append(g.Nodes, target)
		if target.Blob != nil {
			return true
		}
		open[key] = true
		if e := g.descend(m, target.Inst, open); e != nil {
			err = e
			return false
		}
		open[key] = false
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
func visitEdges(m *tabletext.Model, inst *tabletext.Instance, visit func(target Node, field string) bool) {
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
		if f.Type.Blob() {
			if fv.Cell.Blob != nil && !visit(Node{Blob: fv.Cell.Blob, Kind: f.Type.Kind}, f.Name) {
				return
			}
			continue
		}
		if f.Type.Pointer {
			// a pointer field, or an ARRAY of them (§2.1): every live slot is an
			// edge, in index order, and a null slot is not
			switch f.Array {
			case ir.ArrayNone:
				if fv.Cell.Node != nil && !visit(Node{Inst: fv.Cell.Node}, f.Name) {
					return
				}
			case ir.ArrayFixed:
				for k := range fv.Elems {
					if fv.Elems[k].Node != nil && !visit(Node{Inst: fv.Elems[k].Node}, f.Name) {
						return
					}
				}
			case ir.ArrayCounted, ir.ArrayList:
				for k := 0; k < fv.Count && k < len(fv.Elems); k++ {
					if fv.Elems[k].Node != nil && !visit(Node{Inst: fv.Elems[k].Node}, f.Name) {
						return
					}
				}
			}
			continue
		}
		if un := tabletext.UnionOf(f); un != nil {
			// a union's set arm is an edge, and so is every element's of an
			// array of unions — the live elements of a counted one only (§3.1)
			switch f.Array {
			case ir.ArrayNone:
				visitArmEdges(m, &fv.Cell, un, f.Name, visit)
			case ir.ArrayFixed:
				for k := range fv.Elems {
					visitArmEdges(m, &fv.Elems[k], un, f.Name, visit)
				}
			case ir.ArrayCounted, ir.ArrayList:
				for k := 0; k < fv.Count && k < len(fv.Elems); k++ {
					visitArmEdges(m, &fv.Elems[k], un, f.Name, visit)
				}
			}
			continue
		}
		st := tabletext.StructOf(f)
		if st == nil {
			continue
		}
		switch {
		case f.KeyEnum != "":
			// every stored slot is a named variant's: the storage has no None
			// slot to skip (§2.4)
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
		case f.CountedOnWire():
			// only the LIVE elements ride, so only they carry edges. An
			// UNBOUNDED ARRAY is a by-value edge at its FIELD'S POSITION,
			// its elements visited in index order and each descended before
			// the next is reached (§2.9, §3.1)
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

// visitArmEdges walks ONE union cell's set arm (docs/SPEC-TABLES.md §2.6,
// §3.1). AN ARM IS AN EDGE OF ITS OWN KIND: a declared type or table arm is
// descended by value, a POINTER arm is itself a pointer edge — so a node named
// from an arm and from a field beside it is one node under one index — a byte
// buffer arm is that node, and an arm that is another union asks the same
// question one level in.
func visitArmEdges(m *tabletext.Model, cell *tabletext.Cell, un *ir.Union, field string, visit func(target Node, field string) bool) {
	if cell.U == 0 || int(cell.U) > len(un.Variants) {
		return
	}
	arm := un.Variants[cell.U-1]
	if arm.Body() {
		if cell.Tab != nil {
			visitEdges(m, cell.Tab, visit)
		}
		return
	}
	fv := cell.Arm
	if fv == nil {
		return
	}
	f := fv.Def
	switch {
	case f.Type.Blob():
		if fv.Cell.Blob != nil {
			visit(Node{Blob: fv.Cell.Blob, Kind: f.Type.Kind}, field)
		}
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		if fv.Cell.Node != nil {
			visit(Node{Inst: fv.Cell.Node}, field)
		}
	case f.Type.Pointer:
		live := len(fv.Elems)
		if f.CountedOnWire() && fv.Count < live {
			live = fv.Count
		}
		for k := 0; k < live; k++ {
			if fv.Elems[k].Node != nil {
				visit(Node{Inst: fv.Elems[k].Node}, field)
			}
		}
	case tabletext.UnionOf(f) != nil:
		visitArmEdges(m, &fv.Cell, tabletext.UnionOf(f), field, visit)
	}
}
