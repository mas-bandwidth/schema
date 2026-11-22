package tabledemo_test

import (
	"encoding/binary"
	"testing"
	"unsafe"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// heldKindWire is one Patrol form-1 file whose body carries one field of the
// given kind under id, its payload framed by a one-byte L, and an id table
// naming only id.
func heldKindWire(id uint64, kind byte, payload []byte) []byte {
	w := []byte{byte(ir.TableWireForm), 1, kind, byte(len(payload))}
	w = append(w, payload...)
	w = append(w, 0)
	w = binary.LittleEndian.AppendUint64(w, id)
	return binary.LittleEndian.AppendUint64(w, 1)
}

// patrolHeldLoads reads wire through the generated region load and the
// retaining load, each into a region sized by PatrolLoadMeasure and aligned to
// 64 bytes as a caller sizes it, and answers whether each read returned.
func patrolHeldLoads(wire []byte) (plain, kept bool, plainReport, keptReport tabledemo.TableReport) {
	size := tabledemo.PatrolLoadMeasure(wire)
	if size < 0 {
		return false, false, plainReport, keptReport
	}
	region := func() []byte {
		raw := make([]byte, size+63)
		off := (-uintptr(unsafe.Pointer(unsafe.SliceData(raw)))) & 63
		return raw[off : off+uintptr(size)]
	}
	plain = tabledemo.PatrolLoad(region(), wire, &plainReport) != nil
	retain := tabledemo.TableRetain{Bytes: make([]byte, 1<<16), Ids: make([]tabledemo.TableRetainID, 64)}
	kept = tabledemo.PatrolLoadRetain(region(), wire, &retain, &keptReport) != nil
	return plain, kept, plainReport, keptReport
}

// TestGeneratedKindHeldEscapeRefuses: kind 31 is held by name and never
// written (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never
// the other way). The generated reader meets it only as damage, at a field it
// cannot name as at one it declares, in the plain load and the retaining one,
// exactly as it meets 33 and 34. Red on the old escape, which skipped an
// unnamed 31 by its L and counted it unknown. The control is the same field
// under the payload-free kind 32, which an unnamed field skips and counts.
func TestGeneratedKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	unknown, active := ir.TableWireId("zzz_held"), ir.TableWireId("active")
	for _, c := range []struct {
		name string
		wire []byte
	}{
		{"unnamed field", heldKindWire(unknown, ir.TableKindHeldEscape, []byte{0xAA, 0xBB})},
		{"unnamed field, L 0", heldKindWire(unknown, ir.TableKindHeldEscape, nil)},
		{"named field", heldKindWire(active, ir.TableKindHeldEscape, []byte{0})},
		{"retired 33", heldKindWire(unknown, ir.TableKindRetiredWstring, nil)},
		{"reserved 34", heldKindWire(unknown, ir.TableKindReservedFloat16, nil)},
	} {
		plain, kept, pr, kr := patrolHeldLoads(c.wire)
		if plain || kept {
			t.Errorf("%s: the load returned (plain=%t unknown=%d, retain=%t unknown=%d), want the whole load refused", c.name, plain, pr.Unknown, kept, kr.Unknown)
		}
	}
	plain, kept, pr, kr := patrolHeldLoads(heldKindWire(unknown, ir.TableKindNoPayload, nil))
	if !plain || !kept || pr.Unknown != 1 || kr.Unknown != 1 {
		t.Fatalf("control: an unnamed kind 32 read plain=%t unknown=%d retain=%t unknown=%d, want both read and one unknown", plain, pr.Unknown, kept, kr.Unknown)
	}
}
