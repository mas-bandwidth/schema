// LOAD IS A SCAN, and that is the whole of its bound (docs/SPEC-TABLES.md §3.1).
// Reading follows no reference: the records are walked once to learn what each
// node IS, and once to decode each body into its own storage, so a forward
// index resolves without scratch. Every record is visited a fixed number of
// times, in index order, and each is consumed in full before the next begins.
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// decodeState is the save's numbering, resident: the node every index names,
// and whether the table could be read whole at all. It is shared by every
// sub-reader of one decode, because an index resolves against ONE numbering
// however deep the field that carries it sits.
type decodeState struct {
	root  *tabletext.Instance
	nodes []Node // index i names node index i + 2 — the records; a zero Node is one this reader cannot name
	// good is false when the node table could not be read whole. Numbering is
	// positional across the concatenation, so a field that cannot be read
	// cannot be dropped without renumbering every record after it: the whole
	// table is malformed and EVERY POINTER IN THE SAVE READS NULL.
	good bool
}

// resolve places one node index in a pointer slot. Every failure is one of §4's
// events and none is new; in every one of them the pointer stays null. A BYTE
// BUFFER's slot resolves the same way, against its reserved type id (§2.5): a
// table's record under a `*bytes` slot, a blob under a `*T` slot, or a
// `*string` blob under a `*bytes` slot is a kind mismatch.
func (r *wireReader) resolve(fv *tabletext.Field, index uint32) {
	r.resolveCell(&fv.Cell, fv.Def, index)
}

// resolveCell resolves one pointer SLOT — a field's cell, or an element of an
// array of pointers (§2.1) — against the numbering.
func (r *wireReader) resolveCell(cell *tabletext.Cell, f *ir.Field, index uint32) {
	cell.Node = nil
	cell.Blob = nil
	st := r.st
	if st == nil || !st.good {
		return // a numbering that failed resolves nothing
	}
	blob := f.Type.Blob()
	target := f.Type.Name
	switch {
	case index == ir.NodeIndexNull:
		return
	case index == ir.NodeIndexRoot:
		// the root carries no record and therefore no wire type id, so the
		// READER'S OWN root type is what the claim is checked against — and it
		// is checked
		if blob || st.root.Def.Name != target {
			r.report.KindMismatch++
			return
		}
		cell.Node = st.root
	case int(index)-2 < len(st.nodes):
		node := st.nodes[index-2]
		if node.Inst == nil && node.Blob == nil {
			// a node whose type id this reader cannot name KEEPS ITS INDEX, and
			// every pointer naming it reads null. The unknown was counted once,
			// at the node, not once per pointer.
			return
		}
		if blob {
			if node.Blob == nil || node.Kind != f.Type.Kind {
				r.report.KindMismatch++
				return
			}
			cell.Blob = node.Blob
			return
		}
		if node.Inst == nil || node.Inst.Def.Name != target {
			r.report.KindMismatch++
			return
		}
		cell.Node = node.Inst
	default:
		// an index above node_count + 1
		r.report.Malformed = true
	}
}

// decodeVariable reads a pointered root: the node table first, because a reader
// has already read `head = 2` before it learns whether the table can be read at
// all, then the records, then every body.
func decodeVariable(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	st := &decodeState{root: inst}

	payload, present, framed := nodeTableBytes(data, report)
	records, scanned := []nodeRecord(nil), true
	if framed && present {
		records, scanned = scanNodeRecords(payload)
	}
	switch {
	case !framed && present:
		report.Malformed = true
	case !scanned:
		report.Malformed = true
	default:
		st.good = true
	}

	// the type ids the unit can name. A table name is scoped to a WHOLE unit
	// closure, which is why the id is 64 bits and why this map is the only
	// lookup a scan needs. The two reserved blob ids sit beside them (§2.5).
	byTypeId := map[uint64]*ir.Struct{}
	for name := range ir.TableClosure(m.Unit) {
		if sd := m.Lookup(name); sd != nil {
			byTypeId[ir.TableTypeId(name)] = sd
		}
	}

	// PASS ONE: fill the numbering from the FRAMING, so that an index resolves
	// whichever way it points. It reads no body — a blob's record IS its
	// bytes, so its node is complete here, and the tolerant wire load copies
	// them as it copies every node (§2.5).
	st.nodes = make([]Node, len(records))
	for i, rec := range records {
		switch rec.TypeId {
		case ir.BytesTypeId:
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: append([]byte(nil), rec.Body...)}, Kind: ir.TBytes}
			continue
		case ir.StringTypeId:
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: append([]byte(nil), rec.Body...)}, Kind: ir.TString}
			continue
		}
		sd := byTypeId[rec.TypeId]
		if sd == nil {
			report.Unknown++ // skipped by its length, and it keeps its index
			continue
		}
		st.nodes[i] = Node{Inst: m.New(sd)}
	}

	// PASS TWO: decode each body into its own storage.
	for i, rec := range records {
		if st.nodes[i].Inst == nil {
			continue
		}
		sub := &wireReader{buf: rec.Body, report: report, m: m, st: st}
		sub.body(st.nodes[i].Inst)
	}

	r := &wireReader{buf: data, report: report, m: m, st: st}
	ok := r.body(inst)
	return ok, nil
}
