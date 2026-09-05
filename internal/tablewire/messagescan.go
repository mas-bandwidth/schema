// THE MESSAGE FORM's SCANS (docs/SPEC-TABLES.md §3.3): what the wire fuzzer
// and a sizing pass need to know about a batch's bits without decoding a
// value into anything.
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageSpots is every REFERENCE and every NODE INDEX a batch carries, at its
// bit position and width, found by reading the batch against the vocabulary
// and recording each one as it is met. The read stops where a real read
// would, so the spots of a damaged batch are the ones before the damage.
//
// It is the fuzzer's reference pass on this form (§3.3, §4.2): every
// reference set to `E`, to `E + 1` and to the largest the width can spell.
func MessageSpots(m *tabletext.Model, root *ir.Struct, data []byte, v *Vocabulary) []BitSpot {
	var spots []BitSpot
	if len(data) < 2 || data[0] != ir.TableWireMessageForm || v == nil || !v.announced {
		return nil
	}
	r := newBitReader(data[1:])
	raw, ok := r.get(8)
	if !ok {
		return nil
	}
	count := int(raw) + 1
	var report tabletext.Report
	for range count {
		d := &bitDecoder{m: m, v: v, report: &report, r: r, refBits: v.RefBits(), indexBits: ir.TableMessageBitsRequired(0, 1), spots: &spots}
		if !d.root(m.New(root)) {
			break
		}
	}
	return spots
}

// MessageRecord is one node record of one body's node table, as a sizing
// pass sees it: its type id, and for a blob its length.
type MessageRecord struct {
	TypeId uint64
	Blob   bool
	Length int64
}

// MessageNodeRecords walks a batch's bodies by the announced shapes alone,
// reading no value, and answers each body's node records in order. `whole`
// is false where the framing gave out, which is where a reader's LoadMeasure
// answers -1: a type id reference of 0, one past E, or one naming anything
// but a kind-0 entry, or a body that runs out of bits (§3.1, §3.3). The pad
// and what follows it are the READ's to refuse, not the sizing's.
func MessageNodeRecords(data []byte, v *Vocabulary) (bodies [][]MessageRecord, whole bool) {
	if len(data) < 2 || data[0] != ir.TableWireMessageForm || v == nil || !v.announced {
		return nil, false
	}
	r := newBitReader(data[1:])
	raw, ok := r.get(8)
	if !ok {
		return nil, false
	}
	count := int(raw) + 1
	var report tabletext.Report
	d := &bitDecoder{v: v, report: &report, r: r, refBits: v.RefBits()}
	for range count {
		d.indexBits = ir.TableMessageBitsRequired(0, 1)
		var records []MessageRecord
		at := r.off
		ref, ok := r.get(d.refBits)
		if !ok {
			return nil, false
		}
		entry, named := d.entry(ref)
		if named && entry.Id == ir.TableNodeWireId {
			n, ok := r.get(32)
			if !ok {
				return nil, false
			}
			d.indexBits = ir.TableMessageBitsRequired(0, int64(n)+1)
			for range n {
				typeRef, ok := r.get(d.refBits)
				if !ok {
					return nil, false
				}
				typeEntry, named := d.entry(typeRef)
				if !named || reserved(typeEntry.Id) || typeEntry.Kind != 0 {
					return nil, false
				}
				if typeEntry.Id == ir.BytesWireTypeId || typeEntry.Id == ir.StringWireTypeId {
					length, ok := r.get(32)
					if !ok || !r.align() || !r.skip(int(length)*8) {
						return nil, false
					}
					records = append(records, MessageRecord{TypeId: typeEntry.Id, Blob: true, Length: int64(length)})
					continue
				}
				if !d.skipBody() {
					return nil, false
				}
				records = append(records, MessageRecord{TypeId: typeEntry.Id})
			}
		} else {
			r.off = at
		}
		if !d.skipBody() {
			// THE ROOT BODY'S OWN FRAMING GAVE OUT: the numbering was whole, so
			// the batch is sized through this body and no further, which is
			// the body the load meets damage inside after delivering the ones
			// before it (§3.3)
			return append(bodies, records), true
		}
		bodies = append(bodies, records)
	}
	return bodies, true
}
