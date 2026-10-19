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

type Child {
    retired uint8 = 7 | deprecated
    value uint16 = 9
}
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
    retiredRange uint8 = 3 | min = 1, max = 5, deprecated
    liveBounded uint8 = 1 | min = 0, max = 10
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
	body[43] = 0x55
	binary.LittleEndian.PutUint16(body[44:], 0x4567)
	body[50] = 99
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
	if got.Child.Retired != 7 || got.Child.Value != 9 {
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
	v.Child.Retired, v.Child.Value = 0x88, 0x4567
	v.D = 4
	// These retired values violate the old shape's bounds. They must not
	// refuse a save because the writer emits the declared defaults instead.
	v.CountedCount = 4
	v.TextLength = 5
	v.BlobLength = 5
	v.RetiredRange = 9
	v.LiveBounded = 6 // Keeps FixedCheckBody present for this record.
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
	if body[43] != 7 || binary.LittleEndian.Uint16(body[44:]) != 9 {
		t.Fatalf("writer did not write nested-record defaults: %v", body[43:46])
	}
	if got := int32(binary.LittleEndian.Uint32(body[0:])); got != 1 {
		t.Fatalf("writer changed live field a: %d, want 1", got)
	}
	if got := int32(binary.LittleEndian.Uint32(body[46:])); got != 4 {
		t.Fatalf("writer changed live field d: %d, want 4", got)
	}
	if body[50] != 3 || body[51] != 6 {
		t.Fatalf("writer changed retired/live ranged values: %v %v", body[50], body[51])
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

// A DEPRECATED NAMED-SHAPE FIELD (enum, flags, keyed array) is the same rule
// (§3.4 DEPRECATED SLOT): FixedSave writes the declared default, FixedLoad
// ignores peer bytes and yields the declared default. The other scalar and
// container shapes are covered by TestFixedDeprecatedSlotIsTheDeclaredDefault
// above; these are the named-shape leaves the const op cannot land.
func TestFixedDeprecatedSlotEnumFlagsKeyed(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

enum Color { Red, Green, Blue }
flags Bits { One, Two }
fixed table U
{
    e Color = Green | deprecated
    f Bits | deprecated
    keys [Color]uint16 | deprecated
    g int32
    h uint8 | deprecated
}
`, `package probe

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Body offsets follow declaration order; the Go struct's own padding does not
// map to the packed record body, so these are the record's bytes.
const (
	uEPos   = 0
	uESize  = 1
	uFPos   = 1
	uFSize  = 8
	uKeysPos = 9
	uKeysSize = 6
	uGPos   = 15
	uHPos   = 19
)

func uBody(buf []byte) []byte {
	return buf[len(buf)-int(UFixedRecordBytes)+8:]
}

func TestFixedDeprecatedSlotEnumFlagsKeyed(t *testing.T) {
	t.Parallel()
	var p, q U
	UReset(&p)
	UReset(&q)
	p.E, p.F, p.H = ColorRed, BitsOne, 0x77
	q.E, q.F, q.H = ColorBlue, BitsTwo, 0x99
	p.Keys[0], q.Keys[0] = 0xaaaa, 0xbbbb
	p.G, q.G = 3, 5
	bufP := make([]byte, UFixedMeasure(1))
	bufQ := make([]byte, UFixedMeasure(1))
	if n := UFixedSave([]U{p}, bufP); n != int64(len(bufP)) {
		t.Fatalf("save p: %d", n)
	}
	if n := UFixedSave([]U{q}, bufQ); n != int64(len(bufQ)) {
		t.Fatalf("save q: %d", n)
	}
	bp, bq := uBody(bufP), uBody(bufQ)
	if !bytes.Equal(bp[uEPos:uEPos+uESize], bq[uEPos:uEPos+uESize]) {
		t.Fatalf("writer leaked enum live value")
	}
	if !bytes.Equal(bp[uFPos:uFPos+uFSize], bq[uFPos:uFPos+uFSize]) {
		t.Fatalf("writer leaked flags live value")
	}
	if !bytes.Equal(bp[uKeysPos:uKeysPos+uKeysSize], bq[uKeysPos:uKeysPos+uKeysSize]) {
		t.Fatalf("writer leaked keyed-array live value")
	}
	if bp[uHPos] != 0 || bq[uHPos] != 0 {
		t.Fatalf("writer leaked deprecated uint8: %d %d", bp[uHPos], bq[uHPos])
	}
	if int32(binary.LittleEndian.Uint32(bp[uGPos:])) != 3 || int32(binary.LittleEndian.Uint32(bq[uGPos:])) != 5 {
		t.Fatalf("live field corrupted")
	}

	copy(bp[uEPos:uEPos+uESize], []byte{7})
	binary.LittleEndian.PutUint64(bp[uFPos:uFPos+uFSize], 0x5555555555555555)
	binary.LittleEndian.PutUint16(bp[uKeysPos:], 0x1234)
	bp[uHPos] = 0xee
	back := make([]U, 1)
	plan := make([]TableFixedEntry, UFixedPlanMax)
	var r TableReport
	if n := UFixedLoad(back, bufP, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d, report %+v", n, r)
	}
	got := back[0]
	if got.E != ColorGreen || got.F != 0 || got.H != 0 {
		t.Fatalf("named-shape defaults: e=%v f=%v h=%d", got.E, got.F, got.H)
	}
	for i := range got.Keys {
		if got.Keys[i] != 0 {
			t.Fatalf("keyed-array default: %v", got.Keys)
		}
	}
	if got.G != 3 {
		t.Fatalf("live field changed: %d", got.G)
	}
}
`)
}
