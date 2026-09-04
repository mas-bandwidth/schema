// The NODE TABLE's framing on the wire (docs/SPEC-TABLES.md §3.1): the records a
// pointered save writes, under the reserved field id `0xFFFF`, in one or more
// kind-`12` fields that CONCATENATE.
//
// A reader that cannot name the id skips each field by its `L` and counts it
// unknown — no new skip rule, and no ceiling, because the field REPEATS. That
// is the one exception to §3's last-occurrence-wins, and it belongs to this
// reserved id alone.
package tablewire

import (
	"encoding/binary"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nodeRecordHeader is a record's own framing: `type id (u64), length (u32)`.
// Twelve bytes a node, which is the price §6.3 states.
const nodeRecordHeader = 8 + 4

// nodeFieldMax is the largest payload one node-table field may carry: a `L` is
// a u32, so a field's bytes cannot exceed `0xFFFFFFFF` (§3).
const nodeFieldMax = int(0xFFFFFFFF)

// NodeBodyMax is the ceiling on ONE node's body, and exceeding it is a
// SAVE-TIME REFUSAL naming the node (docs/SPEC-TABLES.md §3.1): the record's length
// is a `u32`, and the repair is more nodes — which is the shape the flat
// encoding wants anyway — rather than four more bytes on every node in every
// save. The AGGREGATE ceiling is the one that had to go, and the repeating
// field removed it.
const NodeBodyMax = int(0xFFFFFFFF)

// appendNodeTable writes the numbering's records into the root body, in the
// place §3.1 puts them: LAST, after the root's own declared fields and before
// the u16 terminator, so a reader that gives up a gigabyte into the table still
// holds the root's real values. Field order is not part of the contract (§3), so
// a reader finds them by id.
func (e *encoder) appendNodeTable(body []byte, g *NodeGraph) ([]byte, error) {
	records := g.Records()
	// the root body arrives terminated; the node table's fields are fields of
	// it, so the terminator is lifted and written again after them
	if len(body) < 2 {
		return nil, fmt.Errorf("internal: a table body is never shorter than its terminator")
	}
	out := body[:len(body)-2]

	// the FIRST field's payload opens with the count; every field's payload
	// then carries WHOLE RECORDS, and the fields concatenate in order
	field := &buf{}
	field.u64(uint64(len(records)))
	flush := func() {
		out = binary.LittleEndian.AppendUint16(out, ir.NodeTableFieldId)
		out = append(out, uint8(ir.TableKindString))
		out = binary.LittleEndian.AppendUint32(out, uint32(len(field.b)))
		out = append(out, field.b...)
		field = &buf{}
	}
	for _, node := range records {
		rec, err := encodeBody(e, node)
		if err != nil {
			return nil, err
		}
		if len(rec) > NodeBodyMax {
			return nil, fmt.Errorf("node %s: its body is %d bytes and a record's length is a u32 — save refuses it rather than truncating, and the repair is more nodes (docs/SPEC-TABLES.md §3.1)", node.Def.Name, len(rec))
		}
		// A RECORD NEVER STRADDLES A FIELD: the next field opens when the
		// record about to be written would not fit in this one, so every
		// multi-byte read a reader makes lies inside one contiguous payload and
		// the generated body decoder never learns that chunking exists.
		if len(field.b) > 0 && len(field.b)+nodeRecordHeader+len(rec) > nodeFieldMax {
			flush()
		}
		field.u64(ir.TableTypeId(node.Def.Name))
		field.u32(uint32(len(rec)))
		field.raw(rec)
	}
	flush()
	return binary.LittleEndian.AppendUint16(out, 0), nil
}

// nodeTableBytes gathers the node table out of a ROOT body: every top-level
// field under the reserved id, in order, concatenated.
//
// **The node table is whole or it is nothing**: numbering is positional across
// the concatenation, so a field that cannot be read cannot be dropped without
// renumbering every record after it. A node-table field arriving under a kind
// other than `12`, or with a length past the root body, makes the whole table
// malformed, every pointer in the save reads null, and one event is counted.
// Damage to the root body ELSEWHERE — a field of another id the scan cannot
// skip, a missing terminator — is §4's framing damage on the root body, not
// the table's: the scan stops there and what it has found is the table
// (docs/SPEC-TABLES.md §3.1), which `node_count` then has to match. The root
// body still reads on past the fields, so the root's own values survive.
func nodeTableBytes(data []byte, report *tabletext.Report) (payload []byte, present bool, ok bool) {
	r := &wireReader{buf: data, report: &tabletext.Report{}}
	for {
		if !r.has(2) {
			return payload, present, true
		}
		id := r.u16()
		if id == 0 {
			return payload, present, true
		}
		if !r.has(1) {
			return payload, present, true
		}
		kind := r.u8()
		if id == ir.NodeTableFieldId {
			present = true
			if kind != ir.TableKindString {
				report.Malformed = true
				return nil, present, false
			}
			if !r.has(4) {
				return nil, present, false
			}
			n := int(r.u32())
			if !r.has(n) {
				return nil, present, false
			}
			payload = append(payload, r.buf[r.off:r.off+n]...)
			r.off += n
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
func scanNodeRecords(payload []byte) ([]nodeRecord, bool) {
	if len(payload) < 8 {
		return nil, false
	}
	declared := binary.LittleEndian.Uint64(payload)
	off := 8
	var records []nodeRecord
	for off < len(payload) {
		if off+nodeRecordHeader > len(payload) {
			return nil, false // a record whose header runs past its field
		}
		typeId := binary.LittleEndian.Uint64(payload[off:])
		length := int(binary.LittleEndian.Uint32(payload[off+8:]))
		off += nodeRecordHeader
		if length < 0 || off+length > len(payload) {
			return nil, false // a record whose length runs past its field
		}
		records = append(records, nodeRecord{TypeId: typeId, Body: payload[off : off+length]})
		off += length
	}
	if off != len(payload) {
		return nil, false // bytes left over inside a field
	}
	if declared != uint64(len(records)) {
		return nil, false // a node_count the scan does not match
	}
	return records, true
}
