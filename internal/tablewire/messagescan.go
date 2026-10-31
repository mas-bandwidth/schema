// THE MESSAGE FORM's SCAN (docs/SPEC-TABLES.md §3.3): what the wire fuzzer
// needs to know about a batch's bits.
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageSpots is every REFERENCE a batch carries, at its bit position and
// width, found by reading the batch against the vocabulary
// and recording each one as it is met. The read stops where a real read
// would, so the spots of a damaged batch are the ones before the damage.
//
// It is the fuzzer's reference pass on this form (§3.3, §4.2): every
// reference set to `E`, to `E + 1` and to the largest the width can spell.
func MessageSpots(m *tabletext.Model, root *ir.Struct, data []byte, v *Vocabulary) []BitSpot {
	var spots []BitSpot
	if len(data) < 2 || data[0] != ir.TableWireMessageForm || v == nil || !v.announced || messageRoot(m, root) != nil {
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
		d := &bitDecoder{m: m, v: v, report: &report, r: r, refBits: v.RefBits(), spots: &spots}
		if !d.body(m.New(root)) {
			break
		}
	}
	return spots
}
