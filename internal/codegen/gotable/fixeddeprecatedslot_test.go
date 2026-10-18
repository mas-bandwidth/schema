package gotable

// A DEPRECATED FIELD'S SLOT CARRIES ITS DECLARED DEFAULT ON THE FIXED FORM
// (docs/SPEC-TABLES.md §3.4, DEPRECATED SLOT): the field keeps its slot, the
// writer emits the field's declared default there — the type's zero when none
// is declared — and the reader yields that default whatever the record carries.
// The probe writes a value whose deprecated fields hold other values, reads the
// bytes at their slots, then overwrites those slots with a peer's bytes and
// reads the record back: the writer's default must be on the wire and the
// reader's value must not be the peer's bytes. The declared default is wider
// than the const op's four-byte Aux lane, so the plan carries both lanes.

import "testing"

func TestFixedDeprecatedSlotIsTheDeclaredDefault(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

fixed table T
{
    a int32
    b int64 = 4294967303 | deprecated
    c uint8 | deprecated
    d int32
}
`, `package probe

import (
	"encoding/binary"
	"testing"
)

const wantB int64 = 4294967303 // 0x1_00000007: both four-byte lanes carry bits

func TestFixedDeprecatedSlotIsTheDeclaredDefault(t *testing.T) {
	var v T
	TReset(&v)
	v.A = 1
	v.B = 99
	v.C = 9
	v.D = 4
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	body := buf[len(buf)-int(TFixedRecordBytes)+8:]
	if got := int64(binary.LittleEndian.Uint64(body[4:])); got != wantB {
		t.Fatalf("writer wrote %d in the declared-default deprecated slot, want %d", got, wantB)
	}
	if got := body[12]; got != 0 {
		t.Fatalf("writer wrote %d in the no-default deprecated slot, want 0", got)
	}
	if got := int32(binary.LittleEndian.Uint32(body[0:])); got != 1 {
		t.Fatalf("writer changed the live field a: %d, want 1", got)
	}
	if got := int32(binary.LittleEndian.Uint32(body[13:])); got != 4 {
		t.Fatalf("writer changed the live field d: %d, want 4", got)
	}

	binary.LittleEndian.PutUint64(body[4:], 0x0000000900003039)
	body[12] = 99
	back := make([]T, 1)
	plan := make([]TableFixedEntry, TFixedPlanMax)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d, report %+v", n, r)
	}
	if back[0].A != 1 || back[0].D != 4 {
		t.Fatalf("the live fields did not round-trip: %+v", back[0])
	}
	if back[0].B != wantB {
		t.Fatalf("reader yielded %d from the declared-default deprecated slot, want the declared default %d", back[0].B, wantB)
	}
	if back[0].C != 0 {
		t.Fatalf("reader yielded %d from the no-default deprecated slot, want the type's zero", back[0].C)
	}
}
`)
}
