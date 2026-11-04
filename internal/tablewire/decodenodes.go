// LOAD IS A SCAN, and that is the whole of its bound (docs/SPEC-TABLES.md §3.1).
// Reading follows no reference: the records are walked once to learn what each
// node IS, and once to decode each body into its own storage, so a forward
// index resolves without scratch. The pointer graph's own rules (an index past
// the table, a cycle, an unreachable record, a shared blob) ran in the wire
// check before this decode began (wirecheck.go).
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// decodeState is the save's numbering, resident: the node every index names.
// It is shared by every sub-reader of one decode, because an index resolves
// against ONE numbering however deep the field that carries it sits.
type decodeState struct {
	root  *tabletext.Instance
	nodes []Node // index i names node index i + 2 — the records; a zero Node is one this reader cannot name
}

// The three marks of the pointer graph's walk, one per node (wirecheck.go).
const (
	markUnvisited uint8 = iota
	markOpen            // entered, its descent not finished
	markClosed          // entered and finished: reaching it again is a share
)

// resolveCell resolves one pointer SLOT — a field's cell, an element of an
// array of pointers (§2.1) or an arm — against the numbering. A pointer to a
// record of a type this reader CANNOT name reads null (evolution, §3.1, §6.5);
// one to a record of a type it names that is not the type the slot requires
// refuses, and so does a blob under a `*T` slot, a table under a `*bytes`
// slot, or a `*string` blob under a `*bytes` slot (§2.5).
func (r *wireReader) resolveCell(cell *tabletext.Cell, f *ir.Field, index uint64) bool {
	cell.Node = nil
	cell.Blob = nil
	st := r.st
	if index == ir.NodeWireIndexNull {
		return true
	}
	if st == nil {
		// a value-only root places no node: every index the wire check
		// admitted reads null here, because no record has a type this root
		// can name
		return true
	}
	blob := f.Type.Blob()
	target := f.Type.Name
	wrong := func() bool {
		return r.refuse(&Refusal{Reason: ReasonWireDamaged, Detail: "a pointer whose target is not the type its slot requires: " + FloatOwner(r.m, f)})
	}
	switch {
	case index == ir.NodeWireIndexRoot:
		// the wire check refuses every pointer to the root, because the root's
		// descent is open for the whole walk: a pointer to it is a cycle
		return r.damage("a pointer to the root")
	case index-2 < uint64(len(st.nodes)):
		node := st.nodes[index-2]
		if node.Inst == nil && node.Blob == nil {
			return true // a record of a type this reader cannot name reads null
		}
		if blob {
			if node.Blob == nil || node.Kind != f.Type.Kind {
				return wrong()
			}
			cell.Blob = node.Blob
			return true
		}
		if node.Inst == nil || node.Inst.Def.Name != target {
			return wrong()
		}
		cell.Node = node.Inst
		return true
	}
	return r.damage("a pointer index past the node table")
}

// decodeValue reads the root body and, for a pointered root, the node table:
// the numbering first, then every record, then the root. EVERY RECORD OF A
// TYPE THIS READER NAMES IS DECODED and held to the declaration, whether or
// not a slot the reader knows reaches it (§6.5).
func decodeValue(m *tabletext.Model, inst *tabletext.Instance, data []byte, ids []uint64, report *tabletext.Report, rt *retainState) error {
	fail := &readFail{}
	if !ir.VariableTables(m.Unit)[inst.Def.Name] {
		// a value-only table places no node, and the node table (if a writer
		// left one, which the wire check has judged) is stepped over by id
		r := &wireReader{buf: data, report: report, m: m, ids: ids, rt: rt, fail: fail}
		r.body(inst)
		return fail.err
	}
	st := &decodeState{root: inst}

	var records []nodeRecord
	if payload, present, framed := nodeTableBytes(data, ids); present {
		var scanned bool
		if framed {
			records, scanned = scanNodeRecords(payload, ids)
		}
		if !framed || !scanned {
			return &Refusal{Reason: ReasonWireDamaged, Detail: "the node table cannot be read whole"}
		}
	}

	// THE TYPE IDS THIS ROOT CAN PLACE, which is the set §3.1 means by "a type
	// id this reader can name": a node record is a POINTER's pointee, so a
	// table no pointer below this root targets is a node nothing here can
	// name. The two reserved blob ids sit beside them (§2.5), each placeable
	// only under a root a pointer of its kind sits below.
	byTypeId := map[uint64]*ir.Struct{}
	for _, sd := range ir.PointerReachable(inst.Def) {
		if def := m.Lookup(sd.Name); def != nil {
			byTypeId[ir.TableWireId(def.WireName())] = def
		}
	}
	blobKind := map[uint64]ir.FieldTypeKind{}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(inst.Def)
	if bytesEdge {
		blobKind[ir.BytesWireTypeId] = ir.TBytes
	}
	if stringEdge {
		blobKind[ir.StringWireTypeId] = ir.TString
	}

	// PASS ONE: fill the numbering from the FRAMING, so that an index resolves
	// whichever way it points. A blob's record IS its bytes, so its node is
	// complete here.
	st.nodes = make([]Node, len(records))
	for i, rec := range records {
		if kind, ok := blobKind[rec.TypeId]; ok {
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: append([]byte(nil), rec.Body...)}, Kind: kind}
			continue
		}
		sd := byTypeId[rec.TypeId]
		if sd == nil {
			report.Unknown++ // a record this reader cannot name keeps its index
			if rt != nil {
				// A NODE RECORD WHOSE TYPE ID THIS READER CANNOT NAME IS AN
				// EXCLUDED CLASS (docs/SPEC-TABLES.md §6.6)
				rt.lost(report)
			}
			continue
		}
		st.nodes[i] = Node{Inst: m.New(sd)}
	}

	// PASS TWO: decode each body into its own storage.
	for i, rec := range records {
		if st.nodes[i].Inst == nil {
			continue
		}
		sub := &wireReader{buf: rec.Body, report: report, m: m, ids: ids, st: st, rt: rt, fail: fail}
		if !sub.bodyAt(st.nodes[i].Inst, true) {
			return fail.err
		}
	}

	r := &wireReader{buf: data, report: report, m: m, ids: ids, st: st, rt: rt, fail: fail}
	r.body(inst)
	return fail.err
}
