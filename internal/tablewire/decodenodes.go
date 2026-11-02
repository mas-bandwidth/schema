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
	// named records the blob node indices a slot has already named, so a
	// SECOND slot naming one blob is counted as BlobShared (docs/SPEC-TABLES.md
	// §2.5). The tolerant read keeps the one node; only the counter moves.
	named map[uint64]bool
}

// The three marks of the loaded graph's cycle check, one per node.
const (
	markUnvisited uint8 = iota
	markOpen            // entered, its descent not finished
	markClosed          // entered and finished: reaching it again is a share
)

// cyclic reports whether the LOADED graph holds a cycle (docs/SPEC-TABLES.md
// §3.1, §4). No writer produces one, so a cyclic wire is damage and the
// whole load is refused (Glenn, 2026-10-31: "It's perhaps the MOST OBVIOUS
// attack somebody would do on this protocol!").
//
// The vertices are the root and every record this reader placed; the edges
// are every pointer slot a node's by-value closure STORES, a slot under a
// false guard included, because the load stored it. A cycle anywhere refuses,
// one the root never reaches included.
//
// Each node carries one of three marks and is ENTERED ONCE: a pointer to an
// OPEN node is a cycle, and a pointer to a CLOSED node is a legal share. So
// the check is linear in nodes plus slots. A walk that tracked only the open
// path would re-enter every shared node from every path that reaches it,
// which is exponential on a diamond chain.
func (st *decodeState) cyclic(m *tabletext.Model) bool {
	index := make(map[*tabletext.Instance]int, len(st.nodes)+1)
	index[st.root] = 0
	for i, n := range st.nodes {
		if n.Inst != nil {
			index[n.Inst] = i + 1
		}
	}
	instAt := func(v int) *tabletext.Instance {
		if v == 0 {
			return st.root
		}
		return st.nodes[v-1].Inst
	}
	targets := func(v int) []int {
		var out []int
		visitSlots(m, instAt(v), true, func(target Node, _ string) bool {
			if target.Inst != nil {
				if t, ok := index[target.Inst]; ok {
					out = append(out, t)
				}
			}
			return true
		})
		return out
	}
	mark := make([]uint8, len(st.nodes)+1)
	type frame struct {
		node    int
		targets []int
		next    int
	}
	var stack []frame
	for start := range mark {
		if mark[start] != markUnvisited || instAt(start) == nil {
			continue
		}
		mark[start] = markOpen
		stack = append(stack[:0], frame{node: start, targets: targets(start)})
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next == len(top.targets) {
				mark[top.node] = markClosed
				stack = stack[:len(stack)-1]
				continue
			}
			t := top.targets[top.next]
			top.next++
			switch mark[t] {
			case markOpen:
				return true // a pointer to an OPEN node: a cycle
			case markClosed:
				continue // a pointer to a CLOSED node: a share, never re-entered
			}
			mark[t] = markOpen
			stack = append(stack, frame{node: t, targets: targets(t)})
		}
	}
	return false
}

// resolve places one node index in a pointer slot. Every failure is one of §4's
// events and none is new; in every one of them the pointer stays null. A BYTE
// BUFFER's slot resolves the same way, against its reserved type id (§2.5): a
// table's record under a `*bytes` slot, a blob under a `*T` slot, or a
// `*string` blob under a `*bytes` slot is a kind mismatch.
func (r *wireReader) resolve(fv *tabletext.Field, index uint64) {
	r.resolveCell(&fv.Cell, fv.Def, index)
}

// resolveCell resolves one pointer SLOT — a field's cell, or an element of an
// array of pointers (§2.1) — against the numbering.
func (r *wireReader) resolveCell(cell *tabletext.Cell, f *ir.Field, index uint64) {
	cell.Node = nil
	cell.Blob = nil
	st := r.st
	if st == nil || !st.good {
		return // a numbering that failed resolves nothing
	}
	blob := f.Type.Blob()
	target := f.Type.Name
	switch {
	case index == ir.NodeWireIndexNull:
		return
	case index == ir.NodeWireIndexRoot:
		// the root carries no record and therefore no wire type id, so the
		// READER'S OWN root type is what the claim is checked against — and it
		// is checked
		if blob || st.root.Def.Name != target {
			r.report.KindMismatch++
			return
		}
		cell.Node = st.root
	case index >= 2 && index-2 < uint64(len(st.nodes)):
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
			// TWO SLOTS NAMING ONE BLOB (docs/SPEC-TABLES.md §2.5): the
			// tolerant read keeps the one node and counts each extra naming;
			// the strict read refuses it by name, and no writer emits it.
			if st.named == nil {
				st.named = map[uint64]bool{}
			}
			if st.named[index] {
				r.report.BlobShared++
			} else {
				st.named[index] = true
			}
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
func decodeVariable(m *tabletext.Model, inst *tabletext.Instance, data []byte, ids []uint64, report *tabletext.Report, rt *retainState, region bool) (bool, error) {
	st := &decodeState{root: inst}
	countRefusal := false

	payload, present, framed := nodeTableBytes(data, ids, report)
	records, scanned := []nodeRecord(nil), true
	if framed && present {
		records, scanned = scanNodeRecords(payload, ids)
	}
	switch {
	case !framed && present:
		report.Malformed = true
	case !scanned:
		report.Malformed = true
	default:
		st.good = true
	}

	// THE TYPE IDS THIS ROOT CAN PLACE, which is narrower than the unit's
	// closure and is the set §3.1 means by "a type id this reader cannot
	// name": a node record is a POINTER's pointee, so a table no pointer
	// below this root targets is a node nothing here can name — it is skipped
	// by its length and counted unknown, and it commands no region storage
	// (§6.5). A file never carries one, because a writer writes only the ids
	// its own body used; a MESSAGE can, because a connection's table
	// announces every table's name id whether or not a pointer names it
	// (§3.3). The two reserved blob ids sit beside them (§2.5).
	//
	// The PLACEABLE SET is named on its own line, before the id map is built
	// from it, and messagedecode.go names its own the same way. The message
	// reader's is the seam the node-type negative control replaces with the
	// whole unit closure, because a FILE never carries the record this rule
	// skips and no file vector can go red on it.
	byTypeId := map[uint64]*ir.Struct{}
	placeable := map[string]bool{}
	for _, st := range ir.PointerReachable(inst.Def) {
		placeable[st.Name] = true
	}
	for name := range placeable {
		if sd := m.Lookup(name); sd != nil {
			byTypeId[ir.TableWireId(sd.WireName())] = sd
		}
	}

	// AND THE TWO RESERVED BLOB IDS ARE ON THE SAME FOOTING (§2.5): a blob
	// node is a pointer's pointee too, so a `*bytes` record under a root no
	// `*bytes` edge sits below is a node this reader cannot name either. An id
	// missing from this map falls through to the table lookup, which never
	// holds it, and is counted unknown there.
	blobKind := map[uint64]ir.FieldTypeKind{}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(inst.Def)
	if bytesEdge {
		blobKind[ir.BytesWireTypeId] = ir.TBytes
	}
	if stringEdge {
		blobKind[ir.StringWireTypeId] = ir.TString
	}

	// PASS ONE: fill the numbering from the FRAMING, so that an index resolves
	// whichever way it points. It reads no body — a blob's record IS its
	// bytes, so its node is complete here, and the tolerant wire load copies
	// them as it copies every node (§2.5).
	st.nodes = make([]Node, len(records))
	for i, rec := range records {
		if kind, ok := blobKind[rec.TypeId]; ok {
			if kind == ir.TString && !textValid(rec.Body) {
				// A TEXT BLOB'S CONTENT IS REFUSED ON THE SAME TERMS as a kind
				// 12 payload (§3.1): the record is malformed, and every slot
				// naming it reads null, which is what an empty node resolves to
				report.Malformed = true
				continue
			}
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: append([]byte(nil), rec.Body...)}, Kind: kind}
			continue
		}
		sd := byTypeId[rec.TypeId]
		if sd == nil {
			report.Unknown++ // skipped by its length, and it keeps its index
			if rt != nil {
				// A NODE RECORD WHOSE TYPE ID THIS READER CANNOT NAME IS AN
				// EXCLUDED CLASS (docs/SPEC-TABLES.md §6.6): it is a whole
				// node, and putting one back means renumbering a graph the
				// writer numbers from its own edges (§3.1).
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
		sub := &wireReader{buf: rec.Body, report: report, m: m, ids: ids, st: st, rt: rt, countRefusal: &countRefusal, region: region}
		sub.bodyAt(st.nodes[i].Inst, true)
		if countRefusal {
			return false, &CountRefusal{}
		}
	}

	r := &wireReader{buf: data, report: report, m: m, ids: ids, st: st, rt: rt, countRefusal: &countRefusal, region: region}
	ok := r.body(inst)
	if countRefusal {
		return false, &CountRefusal{}
	}
	if st.good && !report.Malformed && st.cyclic(m) {
		report.Malformed = true // a cyclic wire is damage (§3.1, §4)
	}
	return ok, nil
}
