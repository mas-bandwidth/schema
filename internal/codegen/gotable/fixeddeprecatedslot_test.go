package gotable

// A DEPRECATED FIELD'S SLOT CARRIES ITS DECLARED DEFAULT ON THE FIXED FORM
// (docs/SPEC-TABLES.md §3.4, DEPRECATED SLOT), whatever the field's supported
// fixed shape: scalar, array, optional, text/bytes, or nested record. The
// writer must not leak live values, and both the identity and compiled reader
// plans must ignore peer bytes and restore the same default image.

import "testing"

func TestFixedDeprecatedSlotIsTheDeclaredDefault(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

type Child { value uint16 = 9 }
fixed table T
{
    a int32
    b int64 = 4294967303 | deprecated
    c uint8 | deprecated
    arr [2]uint16 | deprecated
    opt ?uint16 | deprecated
    counted [..3]uint8 | deprecated
    text string(4) = "xy" | deprecated
    blob bytes(4) = "ab" | deprecated
    child Child | deprecated
    d int32
}
`, `package probe

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

const wantB int64 = 4294967303 // 0x1_00000007: both four-byte lanes carry bits

func setPeerBytes(body []byte) {
	binary.LittleEndian.PutUint64(body[4:], 0x0000000900003039)
	body[12] = 77
	binary.LittleEndian.PutUint16(body[13:], 0x1111)
	binary.LittleEndian.PutUint16(body[15:], 0x2222)
	body[17] = 1
	binary.LittleEndian.PutUint16(body[18:], 0xbeef)
	binary.LittleEndian.PutUint32(body[20:], 2)
	body[24], body[25] = 0x33, 0x44
	binary.LittleEndian.PutUint32(body[27:], 1)
	body[31], body[32], body[33], body[34] = 'z', 0xff, 0xff, 0xff
	binary.LittleEndian.PutUint32(body[35:], 1)
	body[39], body[40], body[41], body[42] = 'q', 0xff, 0xff, 0xff
	binary.LittleEndian.PutUint16(body[43:], 0x4567)
}

func assertDeprecatedDefaults(t *testing.T, got T) {
	t.Helper()
	if got.B != wantB || got.C != 0 || got.Arr != ([2]uint16{}) {
		t.Fatalf("scalar/array defaults: %+v", got)
	}
	if got.OptPresent || got.Opt != 0 {
		t.Fatalf("optional default: present=%v value=%d", got.OptPresent, got.Opt)
	}
	if got.CountedCount != 0 || got.Counted != ([3]uint8{}) {
		t.Fatalf("counted-array default: count=%d values=%v", got.CountedCount, got.Counted)
	}
	if got.TextLength != 2 || string(got.Text[:2]) != "xy" || got.Text[2] != 0 || got.Text[3] != 0 {
		t.Fatalf("string default: length=%d bytes=%v", got.TextLength, got.Text)
	}
	if got.BlobLength != 2 || string(got.Blob[:2]) != "ab" || got.Blob[2] != 0 || got.Blob[3] != 0 {
		t.Fatalf("bytes default: length=%d bytes=%v", got.BlobLength, got.Blob)
	}
	if got.Child.Value != 9 {
		t.Fatalf("nested-record default: %+v", got.Child)
	}
}

func TestFixedDeprecatedSlotIsTheDeclaredDefault(t *testing.T) {
	t.Parallel()
	var v T
	TReset(&v)
	v.A = 1
	v.B = 99
	v.C = 9
	v.Arr = [2]uint16{0x1111, 0x2222}
	v.OptPresent, v.Opt = true, 0xbeef
	v.CountedCount = 2
	v.Counted[0], v.Counted[1] = 0x33, 0x44
	v.TextLength, v.Text[0] = 1, 'q'
	v.BlobLength, v.Blob[0] = 1, 'r'
	v.Child.Value = 0x4567
	v.D = 4
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	body := buf[len(buf)-int(TFixedRecordBytes)+8:]
	if got := int64(binary.LittleEndian.Uint64(body[4:])); got != wantB {
		t.Fatalf("writer wrote %d in declared-default scalar slot, want %d", got, wantB)
	}
	if body[12] != 0 || string(body[13:17]) != "\x00\x00\x00\x00" || string(body[17:20]) != "\x00\x00\x00" || string(body[20:27]) != "\x00\x00\x00\x00\x00\x00\x00" {
		t.Fatalf("writer leaked deprecated scalar/array/optional/count values: %v", body[12:27])
	}
	if binary.LittleEndian.Uint32(body[27:]) != 2 || string(body[31:35]) != "xy\x00\x00" {
		t.Fatalf("writer did not write deprecated string default: %v", body[27:35])
	}
	if binary.LittleEndian.Uint32(body[35:]) != 2 || string(body[39:43]) != "ab\x00\x00" {
		t.Fatalf("writer did not write deprecated bytes default: %v", body[35:43])
	}
	if binary.LittleEndian.Uint16(body[43:]) != 9 {
		t.Fatalf("writer did not write nested-record default: %v", body[43:45])
	}
	if got := int32(binary.LittleEndian.Uint32(body[0:])); got != 1 {
		t.Fatalf("writer changed live field a: %d, want 1", got)
	}
	if got := int32(binary.LittleEndian.Uint32(body[45:])); got != 4 {
		t.Fatalf("writer changed live field d: %d, want 4", got)
	}

	setPeerBytes(body)
	back := make([]T, 1)
	plan := make([]TableFixedEntry, TFixedPlanMax)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("identity load %d, report %+v", n, r)
	}
	assertDeprecatedDefaults(t, back[0])
	if back[0].A != 1 || back[0].D != 4 {
		t.Fatalf("identity plan changed live fields: %+v", back[0])
	}

	layout, why := tableFixedParseLayout(TFixedLayout)
	if why != "" {
		t.Fatalf("parse layout: %s", why)
	}
	compiled := make([]TableFixedEntry, TFixedPlanMax)
	var compiledReport TableReport
	count := tableFixedCompile(layout, TFixedLayout, TFixedDst, compiled, &compiledReport)
	if count < 0 {
		t.Fatalf("compile identity-shaped lineage plan: %d", count)
	}
	lineage := new(T)
	dst := tableFixedOverlay(unsafe.Pointer(lineage), unsafe.Sizeof(*lineage))
	for _, h := range tableFixedHoles(compiled, count, uint32(len(TFixedResetImage))) {
		copy(dst[h.Off:h.Off+h.Size], TFixedResetImage[h.Off:h.Off+h.Size])
	}
	if why := tableFixedRun(compiled, count, body, dst, &compiledReport); why != "" {
		t.Fatalf("compiled plan run: %s", why)
	}
	assertDeprecatedDefaults(t, *lineage)
	if lineage.A != 1 || lineage.D != 4 {
		t.Fatalf("compiled plan changed live fields: %+v", lineage)
	}
}
`)
}
