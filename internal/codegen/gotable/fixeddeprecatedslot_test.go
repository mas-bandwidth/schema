package gotable

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §2.10,
// §3.4, §21.2, §22.2, §22.6; docs/rulings.json
// fixed-deprecated-field-is-carried). The writer stores what the value holds
// and the reader lands the stored value, for every supported fixed shape:
// scalar, array, optional, text/bytes, or nested record. The declared default
// applies only where the record does not supply the field.

import "testing"

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
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
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"
)

func assertCarried(t *testing.T, got T) {
	t.Helper()
	if got.A != 1 || got.D != 4 {
		t.Fatalf("live fields changed: a=%d d=%d", got.A, got.D)
	}
	if got.B != 99 || got.C != 9 {
		t.Fatalf("deprecated scalar values: b=%d c=%d", got.B, got.C)
	}
	if got.Arr != ([2]uint16{0x1111, 0x2222}) {
		t.Fatalf("deprecated array value: %v", got.Arr)
	}
	if !got.OptPresent || got.Opt != 0xbeef {
		t.Fatalf("deprecated optional: present=%v value=%d", got.OptPresent, got.Opt)
	}
	if got.CountedCount != 2 || got.Counted[0] != 0x33 || got.Counted[1] != 0x44 {
		t.Fatalf("deprecated counted array: count=%d values=%v", got.CountedCount, got.Counted)
	}
	if got.TextLength != 1 || got.Text[0] != 'q' {
		t.Fatalf("deprecated string: length=%d bytes=%v", got.TextLength, got.Text)
	}
	if got.BlobLength != 1 || got.Blob[0] != 'r' {
		t.Fatalf("deprecated bytes: length=%d bytes=%v", got.BlobLength, got.Blob)
	}
	if got.Child.Retired != 0x88 || got.Child.Value != 0x4567 {
		t.Fatalf("deprecated nested record: %+v", got.Child)
	}
	if got.RetiredRange != 3 || got.LiveBounded != 6 {
		t.Fatalf("ranged values: retired=%d live=%d", got.RetiredRange, got.LiveBounded)
	}
}

func carriedValue() T {
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
	v.RetiredRange = 3
	v.LiveBounded = 6
	return v
}

func TestWriterStoresCarriedValues(t *testing.T) {
	t.Parallel()
	v := carriedValue()
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	body := buf[len(buf)-int(TFixedRecordBytes)+8:]
	if got := int32(binary.LittleEndian.Uint32(body[0:])); got != 1 {
		t.Fatalf("writer changed live field a: %d, want 1", got)
	}
	if got := int64(binary.LittleEndian.Uint64(body[4:])); got != 99 {
		t.Fatalf("writer wrote %d in the deprecated scalar slot, want 99", got)
	}
	if body[12] != 9 {
		t.Fatalf("writer wrote %d in the deprecated uint8 slot, want 9", body[12])
	}
	if binary.LittleEndian.Uint16(body[13:]) != 0x1111 || binary.LittleEndian.Uint16(body[15:]) != 0x2222 {
		t.Fatalf("writer did not store the deprecated array: %v", body[13:17])
	}
	if body[17] != 1 || binary.LittleEndian.Uint16(body[18:]) != 0xbeef {
		t.Fatalf("writer did not store the deprecated optional: %v", body[17:20])
	}
	if binary.LittleEndian.Uint32(body[20:]) != 2 || body[24] != 0x33 || body[25] != 0x44 {
		t.Fatalf("writer did not store the deprecated counted array: %v", body[20:27])
	}
	if binary.LittleEndian.Uint32(body[27:]) != 1 || string(body[31:35]) != "q\x00\x00\x00" {
		t.Fatalf("writer did not store the deprecated string: %v", body[27:35])
	}
	if binary.LittleEndian.Uint32(body[35:]) != 1 || string(body[39:43]) != "r\x00\x00\x00" {
		t.Fatalf("writer did not store the deprecated bytes: %v", body[35:43])
	}
	if body[43] != 0x88 || binary.LittleEndian.Uint16(body[44:]) != 0x4567 {
		t.Fatalf("writer did not store the deprecated nested record: %v", body[43:46])
	}
	if got := int32(binary.LittleEndian.Uint32(body[46:])); got != 4 {
		t.Fatalf("writer changed live field d: %d, want 4", got)
	}
	if body[50] != 3 || body[51] != 6 {
		t.Fatalf("writer changed retired/live ranged values: %v %v", body[50], body[51])
	}
}

func TestReaderLandsCarriedValues(t *testing.T) {
	t.Parallel()
	v := carriedValue()
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	back := make([]T, 1)
	plan := make([]TableFixedEntry, TFixedPlanMax)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("identity load %d, report %+v", n, r)
	}
	assertCarried(t, back[0])

	again := make([]byte, TFixedMeasure(1))
	if n := TFixedSave(back, again); n != int64(len(again)) {
		t.Fatalf("re-save %d, want %d", n, len(again))
	}
	if !bytes.Equal(again, buf) {
		t.Fatalf("the record does not write back byte for byte")
	}

	layout, why := tableFixedParseLayout(TFixedLayout)
	if why != "" {
		t.Fatalf("parse layout: %s", why)
	}
	compiled := make([]TableFixedEntry, 1<<10)
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
	body := buf[len(buf)-int(TFixedRecordBytes)+8:]
	if why := tableFixedRun(compiled, count, body, dst, &compiledReport); why != "" {
		t.Fatalf("compiled plan run: %s", why)
	}
	assertCarried(t, *lineage)
}

func TestWriterRefusesOutOfRangeCarriedValue(t *testing.T) {
	t.Parallel()
	v := carriedValue()
	buf := make([]byte, TFixedMeasure(1))
	v.RetiredRange = 9
	if n := TFixedSave([]T{v}, buf); n != -1 {
		t.Fatalf("save accepted deprecated retiredRange = 9 against max = 5: %d", n)
	}
	v = carriedValue()
	v.CountedCount = 4
	if n := TFixedSave([]T{v}, buf); n != -1 {
		t.Fatalf("save accepted deprecated counted count = 4 against bound 3: %d", n)
	}
}
`)
}

// A DEPRECATED NAMED-SHAPE FIELD (enum, flags, keyed array) is carried too
// (§3.4): FixedSave writes what the value holds, FixedLoad lands it. A
// deprecated ENUM VARIANT (§2.15) is a different rule and is unchanged.
func TestFixedDeprecatedFieldIsCarriedEnumFlagsKeyed(t *testing.T) {
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
	uEPos    = 0
	uESize   = 1
	uFPos    = 1
	uFSize   = 8
	uKeysPos = 9
	uKeysSize = 6
	uGPos    = 15
	uHPos    = 19
)

func uBody(buf []byte) []byte {
	return buf[len(buf)-int(UFixedRecordBytes)+8:]
}

func TestEnumFlagsKeyedWriterStoresCarriedValues(t *testing.T) {
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
	if bp[uEPos] != byte(ColorRed) || bq[uEPos] != byte(ColorBlue) {
		t.Fatalf("writer did not store the deprecated enum: %d %d", bp[uEPos], bq[uEPos])
	}
	if binary.LittleEndian.Uint64(bp[uFPos:]) != uint64(BitsOne) || binary.LittleEndian.Uint64(bq[uFPos:]) != uint64(BitsTwo) {
		t.Fatalf("writer did not store the deprecated flags")
	}
	if binary.LittleEndian.Uint16(bp[uKeysPos:]) != p.Keys[0] || binary.LittleEndian.Uint16(bq[uKeysPos:]) != q.Keys[0] {
		t.Fatalf("writer did not store the deprecated keyed array")
	}
	if bp[uHPos] != 0x77 || bq[uHPos] != 0x99 {
		t.Fatalf("writer did not store the deprecated uint8: %d %d", bp[uHPos], bq[uHPos])
	}
	if int32(binary.LittleEndian.Uint32(bp[uGPos:])) != 3 || int32(binary.LittleEndian.Uint32(bq[uGPos:])) != 5 {
		t.Fatalf("live field corrupted")
	}
}

func TestEnumFlagsKeyedReaderLandsCarriedValues(t *testing.T) {
	t.Parallel()
	var p U
	UReset(&p)
	p.E, p.F, p.H = ColorRed, BitsOne, 0x77
	p.Keys[0] = 0xaaaa
	p.G = 3
	bufP := make([]byte, UFixedMeasure(1))
	if n := UFixedSave([]U{p}, bufP); n != int64(len(bufP)) {
		t.Fatalf("save p: %d", n)
	}
	back := make([]U, 1)
	plan := make([]TableFixedEntry, UFixedPlanMax)
	var r TableReport
	if n := UFixedLoad(back, bufP, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d, report %+v", n, r)
	}
	got := back[0]
	if got.E != ColorRed || got.F != BitsOne || got.H != 0x77 {
		t.Fatalf("named-shape carried values: e=%v f=%v h=%d", got.E, got.F, got.H)
	}
	if got.Keys[0] != 0xaaaa {
		t.Fatalf("keyed-array carried value: %v", got.Keys)
	}
	if got.G != 3 {
		t.Fatalf("live field changed: %d", got.G)
	}
	again := make([]byte, UFixedMeasure(1))
	if n := UFixedSave(back, again); n != int64(len(again)) {
		t.Fatalf("re-save: %d", n)
	}
	if !bytes.Equal(again, bufP) {
		t.Fatalf("the record does not write back byte for byte")
	}
}
`)
}
