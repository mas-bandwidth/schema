// The NODE TABLE's framing on the wire (docs/SPEC-TABLES.md §3.1): the records
// a pointered save writes, under the reserved field id
// `0xFFFFFFFFFFFFFFFF`, in ONE kind-`12` field.
//
// A reader that cannot name the id skips the field by its `L` and counts it
// unknown, once — no new skip rule, and no ceiling, because an `L` with
// sixty-four bits of capability frames a numbering of any size, so the whole
// numbering is one contiguous payload and a save's node bodies have no
// aggregate ceiling.
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// appendNodeTable writes the numbering's records into the root body, in the
// place §3.1 puts them: LAST, after the root's own declared fields and before
// the zero reference, so a reader that gives up a gigabyte into the table still
// holds the root's real values. Field order is not part of the contract (§3), so
// a reader finds it by id.
//
// `fields` is the root's own fields WITHOUT the terminator; the caller writes
// that after this returns.
func (e *encoder) appendNodeTable(fields []byte, g *NodeGraph) ([]byte, error) {
	records := g.Records()
	w := &buf{b: fields}
	w.leb(e.ids.ref(ir.TableNodeWireId))
	w.u8(uint8(ir.TableKindString)) // kind 12 is §3's opaque byte payload

	// the payload opens with the count and then carries the records back to
	// back: `type id reference, length, body`
	payload := &buf{}
	payload.leb(uint64(len(records)))
	for _, node := range records {
		// a BYTE BUFFER's record is the bytes themselves under a reserved
		// type id — no fields, no terminator (docs/SPEC-TABLES.md §2.5, §3.1)
		payload.leb(e.ids.ref(node.TypeId()))
		if node.Blob != nil {
			payload.leb(uint64(len(node.Blob.Data)))
			payload.raw(node.Blob.Data)
			continue
		}
		body, err := encodeBody(e, node.Inst)
		if err != nil {
			return nil, err
		}
		payload.leb(uint64(len(body)))
		payload.raw(body)
	}
	w.leb(uint64(len(payload.b)))
	w.raw(payload.b)
	return w.b, nil
}

// nodeTableBytes gathers the node table out of a ROOT body: the one top-level
// field under the reserved id.
//
// **The node table is whole or it is nothing**: numbering is positional, so a
// record that cannot be read cannot be dropped without renumbering every record
// after it. A node-table field arriving under a kind other than `12`, or with a
// length past the root body, makes the whole table malformed, every pointer in
// the save reads null, and one event is counted. **The reserved id inside a
// NESTED body is malformed** on the numbering's own rule, and that is the
// caller's check (§3.1). Damage to the root body ELSEWHERE — a field of another
// id the scan cannot skip, a missing terminator — is §4's framing damage on the
// root body, not the table's: the scan stops there and what it has found is the
// table, which `node_count` then has to match.
func nodeTableBytes(data []byte, ids []uint64, report *tabletext.Report) (payload []byte, present bool, ok bool) {
	r := &wireReader{buf: data, report: &tabletext.Report{}, ids: ids}
	for {
		ref, next, good := readLeb(r.buf, r.off)
		if !good {
			return payload, present, true
		}
		r.off = next
		if ref == 0 {
			return payload, present, true
		}
		id, named := r.id(ref)
		if !named {
			return payload, present, true
		}
		if !r.has(1) {
			return payload, present, true
		}
		kind := r.u8()
		if id == ir.TableNodeWireId {
			present = true
			if kind != ir.TableKindString {
				report.Malformed = true
				return nil, present, false
			}
			n, after, good := readLeb(r.buf, r.off)
			if !good {
				return nil, present, false
			}
			r.off = after
			if !r.has(int(n)) || n > uint64(len(r.buf)) {
				return nil, present, false
			}
			payload = r.buf[r.off : r.off+int(n)]
			r.off += int(n)
			continue
		}
		if !r.skip(kind) {
			return payload, present, true
		}
	}
}

// nodeRecord is one record as the scan found it: its wire type id and its body.
type nodeRecord struct {
	TypeId uint64
	Body   []byte
}

// scanNodeRecords is the AUTHORITATIVE record scan (docs/SPEC-TABLES.md §3.1).
// `node_count` is data from the wire: the scan reads records until the payload
// is consumed and takes what it finds, and a count that disagrees with the scan
// is malformed. Nothing — no directory, no region, no allocation — is sized
// from `node_count` before the scan has confirmed it.
//
// A type id REFERENCE of `0` is malformed, because `0` names no id and a record
// must say what it is (§3).
func scanNodeRecords(payload []byte, ids []uint64) ([]nodeRecord, bool) {
	declared, off, ok := readLeb(payload, 0)
	if !ok {
		return nil, false
	}
	var records []nodeRecord
	for off < len(payload) {
		ref, next, good := readLeb(payload, off)
		if !good || ref == 0 || ref > uint64(len(ids)) {
			return nil, false
		}
		off = next
		length, after, good := readLeb(payload, off)
		if !good {
			return nil, false
		}
		off = after
		if length > uint64(len(payload)-off) {
			return nil, false // a record whose length runs past its field
		}
		records = append(records, nodeRecord{TypeId: ids[ref-1], Body: payload[off : off+int(length)]})
		off += int(length)
	}
	if off != len(payload) {
		return nil, false // bytes left over inside a field
	}
	if declared != uint64(len(records)) {
		return nil, false // a node_count the scan does not match
	}
	return records, true
}
