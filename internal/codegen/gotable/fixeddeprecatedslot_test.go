package gotable

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §3.4,
// §21.2, §22.6; docs/rulings.json fixed-deprecated-field-is-carried): it reads
// and writes like any other field, whatever the field's supported fixed shape.
// The declared default applies only where the record does not supply the field.

import "testing"

// TestFixedDeprecatedFieldIsCarried covers every fixed shape whose deprecated
// slot the writer used to fill from the declared default: scalar, array,
// optional, text, bytes, nested record, and a ranged scalar. Every shape holds
// a non-default value and the read lands it, the write stores it, and a value
// the field's kind refuses is refused.
func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

fixed table Child {
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
	"testing"
	"unsafe"
)

func assertCarried(t *testing.T, got T) {
	t.Helper()
	if got.B != 99 || got.C != 9 || got.Arr != ([2]uint16{0x1111, 0x2222}) {
		t.Fatalf("carried scalar/array fields: %+v", got)
	}
	if !got.OptPresent || got.Opt != 0xbeef {
		t.Fatalf("carried optional: present=%v value=%d", got.OptPresent, got.Opt)
	}
	if got.CountedCount != 2 || got.Counted[0] != 0x33 || got.Counted[1] != 0x44 {
		t.Fatalf("carried counted array: count=%d values=%v", got.CountedCount, got.Counted)
	}
	if got.TextLength != 1 || got.Text[0] != 'q' {
		t.Fatalf("carried string: length=%d bytes=%v", got.TextLength, got.Text)
	}
	if got.BlobLength != 1 || got.Blob[0] != 'r' {
		t.Fatalf("carried bytes: length=%d bytes=%v", got.BlobLength, got.Blob)
	}
	if got.Child.Retired != 0x88 || got.Child.Value != 0x4567 {
		t.Fatalf("carried nested record: %+v", got.Child)
	}
	if got.RetiredRange != 5 {
		t.Fatalf("carried ranged field: %d", got.RetiredRange)
	}
	if got.A != 1 || got.D != 4 || got.LiveBounded != 6 {
		t.Fatalf("live fields changed: %+v", got)
	}
}

func held() T {
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
	v.RetiredRange = 5
	v.LiveBounded = 6
	return v
}

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	v := held()
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	var def T
	TReset(&def)
	dbuf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{def}, dbuf); n != int64(len(dbuf)) {
		t.Fatalf("default save %d, want %d", n, len(dbuf))
	}
	if bytes.Equal(buf, dbuf) {
		t.Fatal("the writer stored the declared defaults instead of the held values")
	}

	back := make([]T, 1)
	plan := make([]TableFixedEntry, TFixedPlanMax)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d, report %+v", n, r)
	}
	assertCarried(t, back[0])

	again := make([]byte, TFixedMeasure(1))
	if n := TFixedSave(back, again); n != int64(len(again)) {
		t.Fatalf("re-save %d, want %d", n, len(again))
	}
	if !bytes.Equal(buf, again) {
		t.Fatalf("the carried fields did not write back byte for byte:\nfirst  %x\nsecond %x", buf, again)
	}

	// THE COMPILED LINEAGE PLAN carries them too: the identity-shaped plan is
	// compiled from the layout's own bytes and its uncovered destinations are
	// the reset image, so a deprecated destination the plan leaves uncovered
	// would land the declared default here.
	layout, why := tableFixedParseLayout(TFixedLayout)
	if why != "" {
		t.Fatalf("parse layout: %s", why)
	}
	compiled := make([]TableFixedEntry, 2*len(TFixedDst))
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
	if why := tableFixedRun(compiled, count, buf[len(buf)-int(TFixedRecordBytes)+8:], dst, &compiledReport); why != "" {
		t.Fatalf("compiled plan run: %s", why)
	}
	assertCarried(t, *lineage)
}

func TestFixedDeprecatedFieldIsCarriedRefusesLikeAnyField(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		spoil func(v *T)
	}{
		{"ranged out of range", func(v *T) { v.RetiredRange = 9 }},
		{"counted count past its bound", func(v *T) { v.CountedCount = 4 }},
		{"string length past its capacity", func(v *T) { v.TextLength = 5 }},
		{"bytes length past its capacity", func(v *T) { v.BlobLength = 5 }},
	} {
		v := held()
		c.spoil(&v)
		buf := make([]byte, TFixedMeasure(1))
		if n := TFixedSave([]T{v}, buf); n != -1 {
			t.Errorf("%s: save returned %d, want -1", c.name, n)
		}
	}
}

// THE READ CHECKS A DEPRECATED FIELD LIKE ANY FIELD: a stored value outside
// the field's declared range refuses the whole load (the verdict only). The
// field's byte is found as the one byte two in-range saves differ in.
func TestFixedDeprecatedFieldIsCarriedReadRefusesLikeAnyField(t *testing.T) {
	t.Parallel()
	v := held()
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d, want %d", n, len(buf))
	}
	v.RetiredRange = 4
	other := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, other); n != int64(len(other)) {
		t.Fatalf("save %d, want %d", n, len(other))
	}
	at := -1
	for i := range buf {
		if buf[i] != other[i] {
			if at >= 0 {
				t.Fatalf("the two saves differ at %d and %d; want one byte", at, i)
			}
			at = i
		}
	}
	if at < 0 || buf[at] != 5 {
		t.Fatalf("no single byte holds the deprecated ranged field (at=%d)", at)
	}
	buf[at] = 9
	back := make([]T, 1)
	plan := make([]TableFixedEntry, TFixedPlanMax)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n >= 0 {
		t.Fatalf("a stored deprecated value outside its declared range was read (load %d); it must be refused like any field", n)
	}
}
`)
}

// A DEPRECATED NAMED-SHAPE FIELD (enum, flags, keyed array) is carried by the
// same rule (§3.4, §22.6): FixedSave stores the held value, FixedLoad lands it,
// and a deprecated enum ordinal past its set is refused like any ordinal.
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

func TestFixedDeprecatedFieldIsCarriedEnumFlagsKeyed(t *testing.T) {
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
	if bytes.Equal(bp[uEPos:uEPos+uESize], bq[uEPos:uEPos+uESize]) {
		t.Fatalf("writer stored the default enum instead of the held value")
	}
	if bytes.Equal(bp[uFPos:uFPos+uFSize], bq[uFPos:uFPos+uFSize]) {
		t.Fatalf("writer stored the default flags instead of the held value")
	}
	if bytes.Equal(bp[uKeysPos:uKeysPos+uKeysSize], bq[uKeysPos:uKeysPos+uKeysSize]) {
		t.Fatalf("writer stored the default keyed array instead of the held value")
	}
	if bp[uHPos] != 0x77 || bq[uHPos] != 0x99 {
		t.Fatalf("writer stored the default uint8 instead of the held value: %d %d", bp[uHPos], bq[uHPos])
	}
	if int32(binary.LittleEndian.Uint32(bp[uGPos:])) != 3 || int32(binary.LittleEndian.Uint32(bq[uGPos:])) != 5 {
		t.Fatalf("live field corrupted")
	}

	back := make([]U, 1)
	plan := make([]TableFixedEntry, UFixedPlanMax)
	var r TableReport
	if n := UFixedLoad(back, bufP, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d, report %+v", n, r)
	}
	got := back[0]
	if got.E != ColorRed || got.F != BitsOne || got.H != 0x77 {
		t.Fatalf("carried named shapes: e=%v f=%v h=%d", got.E, got.F, got.H)
	}
	if got.Keys[0] != 0xaaaa {
		t.Fatalf("carried keyed array: %v", got.Keys)
	}
	if got.G != 3 {
		t.Fatalf("live field changed: %d", got.G)
	}

	var bad U
	UReset(&bad)
	bad.E = Color(9)
	if n := UFixedSave([]U{bad}, bufP); n != -1 {
		t.Fatalf("a deprecated enum ordinal past its set saved: %d", n)
	}
}
`)
}
