// THE MESSAGE FORM'S VOCABULARY (docs/SPEC-TABLES.md §3.3): the entries an
// announcement carries, and the shapes that make a bitpacked body readable.
//
// The message form is the table wire optimized for BANDWIDTH. A body's
// references are bits rather than bytes, there is no kind byte and no length
// on any field, and a value rides at the width its declaration states, which
// is the width the packet wire writes it at. What a reader needs to skip a
// field it cannot name, and to decode one whose declaration has MOVED, is
// therefore not on the body at all: it is the announcement's ENTRY, an id
// beside a kind beside a SHAPE.
//
// One name may take TWO ENTRIES. A unit declaring `count uint8` in one table
// and `count uint32` in another announces both, at their own kinds and shapes,
// and a body names the one it means. That is what the vocabulary buys by being
// a field of the announcement's body rather than its trailer.
package ir

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/bits"
)

// The PACKINGS a numeric shape can carry (docs/SPEC-TABLES.md §3.3).
const (
	// TableMessageRaw is the kind's own storage width, two's complement for
	// the signed kinds.
	TableMessageRaw = 0
	// TableMessageRanged is `base` plus the offset `bits` spell, which is what
	// the packet wire writes for a ranged integer, a `bits(N)`, a `flags` mask
	// and a fixed-point value.
	TableMessageRanged = 1
	// TableMessageQuantized is `min + index * step`, the compressed float's
	// own encoding (SPEC.md §4.3).
	TableMessageQuantized = 2
)

// TableMessageShape is the width and range facts a reader needs to SKIP a
// field exactly and to DECODE it when its own declaration has moved
// (docs/SPEC-TABLES.md §3.3). Which members carry meaning is decided by the
// entry's KIND, and every member a kind does not name is zero.
type TableMessageShape struct {
	Packing uint8    // integers, fixed-point and f32
	Bits    int64    // the value width under a ranged or quantized packing
	Base    *big.Int // the ranged base: the wire carries value - Base
	QMin    float32  // the quantized triple, SPEC.md §4.3's compressed-float declaration
	QMax    float32
	QRes    float32
	Min     int64 // an array's minimum count
	Max     int64 // an array's maximum count, a string's capacity, a keyed array's slots
	Elem    uint8 // an array's or a keyed array's element kind
	Inner   *TableMessageShape
}

// TableVocabularyEntry is ONE announced entry: an id, a kind, and a shape.
// Two entries that agree on all three parts are one entry.
type TableVocabularyEntry struct {
	Id    uint64
	Kind  uint8
	Shape TableMessageShape
}

// TableMessageRefBits is the width of a REFERENCE on a bitpacked body: the
// bits that spell every slot of a vocabulary of `entries` entries, and the
// zero reference beside them — `bits_required(0, entries)`, the same function
// the packet wire uses for a ranged integer.
func TableMessageRefBits(entries int) int {
	if entries <= 0 {
		return 1 // a vocabulary of no entries still has to spell the terminator
	}
	return bits.Len64(uint64(entries))
}

// TableMessageBitsRequired is `bits_required(min, max)`: the bit length of
// `max - min`, and zero where the two are equal, which is a value that spends
// no bit at all.
func TableMessageBitsRequired(min, max int64) int {
	if max <= min {
		return 0
	}
	return bits.Len64(uint64(max - min))
}

// TableMessageListMax is the maximum count an UNBOUNDED array announces
// (§2.9). The declaration states no bound, and the announcement has to state
// one because the count's width is a fact of the wire, so the widest count a
// batch could carry is what rides: a `[]T` spends 32 bits on its count where a
// bounded array spends `bits_required(min, max)`.
const TableMessageListMax = int64(0xFFFFFFFF)

// ---- the entry's bytes ----

// Encode writes one entry into the announcement's vocabulary array: the id, a
// fixed little-endian u64, then the kind, then the shape the kind names.
func (e TableVocabularyEntry) Encode(out []byte) []byte {
	out = binary.LittleEndian.AppendUint64(out, e.Id)
	out = append(out, e.Kind)
	return appendShape(out, e.Kind, e.Shape)
}

// Key is one entry's identity for placement: two entries that agree on the id,
// the kind and every fact of the shape are one entry and take one slot.
func (e TableVocabularyEntry) Key() string { return string(e.Encode(nil)) }

func appendShape(out []byte, kind uint8, s TableMessageShape) []byte {
	switch {
	case tableMessageIntegerKind(kind):
		out = append(out, s.Packing)
		if s.Packing == TableMessageRanged {
			out = appendLebBytes(out, uint64(s.Bits))
			// A RANGED BASE IS ENCODED BY ITS KIND'S SIGNEDNESS (§3.3): a
			// signed kind's is a zigzag LEB128, an unsigned kind's an
			// unsigned LEB128, and a 128-bit kind's the sixteen-byte pattern
			switch {
			case kind == TableKindI128 || kind == TableKindU128:
				out = appendWideBase(out, s.Base)
			case TableKindSigned(int(kind)):
				out = appendLebBytes(out, tableMessageZigzag(s.Base))
			default:
				out = appendLebBytes(out, tableMessageUnsignedBase(s.Base))
			}
		}
	case kind == TableKindF32:
		out = append(out, s.Packing)
		if s.Packing == TableMessageQuantized {
			// the three facts SPEC.md §4.3's rule takes, as float32: the step
			// count, the width and the value all derive from them
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(s.QMin))
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(s.QMax))
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(s.QRes))
		}
	case tableMessageFixedKind(kind):
		out = append(out, s.Packing)
		if s.Packing == TableMessageRanged {
			out = appendLebBytes(out, uint64(s.Bits))
			out = appendWideBase(out, s.Base)
		}
	case kind == TableKindString, kind == TableKindWstring:
		out = appendLebBytes(out, uint64(s.Max))
	case kind == TableKindArray:
		out = appendLebBytes(out, uint64(s.Min))
		out = appendLebBytes(out, uint64(s.Max))
		out = append(out, s.Elem)
		out = appendShape(out, s.Elem, tableMessageInner(s))
	case kind == TableKindKeyed:
		out = appendLebBytes(out, uint64(s.Max))
		out = append(out, s.Elem)
		out = appendShape(out, s.Elem, tableMessageInner(s))
	}
	return out
}

func tableMessageInner(s TableMessageShape) TableMessageShape {
	if s.Inner == nil {
		return TableMessageShape{}
	}
	return *s.Inner
}

// DecodeShape reads one shape back, and ok is false where the bytes run out or
// a width is one no kind can hold. A HOSTILE SHAPE IS A HOSTILE WIDTH, and
// every width is checked once, at AnnounceRead, and never again.
//
// A `min` or a `max` above what its kind can hold is one of those widths. A
// string and a wide string are bounded by the int32 storage cap the checker
// applies to every `N` (SPEC §4.3, §6.1), and an array and a keyed entry by
// the 32-bit count an unbounded array announces (TableMessageListMax, §2.9),
// which is the widest count this form spells. A larger bound is a shape no
// conforming declaration can produce, and a reader that carried it would do
// its length arithmetic in a range that overflows.
func DecodeShape(in []byte, kind uint8) (TableMessageShape, int, bool) {
	var s TableMessageShape
	at := 0
	leb := func() (uint64, bool) {
		v, n, ok := readLebBytes(in[at:])
		if !ok {
			return 0, false
		}
		at += n
		return v, true
	}
	switch {
	case tableMessageIntegerKind(kind), tableMessageFixedKind(kind), kind == TableKindF32:
		if at >= len(in) {
			return s, 0, false
		}
		s.Packing = in[at]
		at++
		switch {
		case s.Packing == TableMessageRaw:
		case s.Packing == TableMessageRanged && kind != TableKindF32:
			v, ok := leb()
			if !ok || v > uint64(TableMessageKindBits(kind)) {
				return s, 0, false // A WIDTH ABOVE THE KIND'S OWN DOMAIN is a hostile width
			}
			s.Bits = int64(v)
			switch {
			case kind == TableKindI128 || kind == TableKindU128 || tableMessageFixedKind(kind):
				if at+16 > len(in) {
					return s, 0, false
				}
				s.Base = wideBaseFrom(in[at : at+16])
				at += 16
			case TableKindSigned(int(kind)):
				z, ok := leb()
				if !ok {
					return s, 0, false
				}
				s.Base = big.NewInt(tableMessageUnzigzag(z))
			default:
				u, ok := leb()
				if !ok {
					return s, 0, false
				}
				s.Base = new(big.Int).SetUint64(u)
			}
		case s.Packing == TableMessageQuantized && kind == TableKindF32:
			if at+12 > len(in) {
				return s, 0, false
			}
			s.QMin = math.Float32frombits(binary.LittleEndian.Uint32(in[at:]))
			s.QMax = math.Float32frombits(binary.LittleEndian.Uint32(in[at+4:]))
			s.QRes = math.Float32frombits(binary.LittleEndian.Uint32(in[at+8:]))
			at += 12
			// A NON-CONFORMING TRIPLE IS A HOSTILE WIDTH like any other: the
			// announcement is refused and no vocabulary is set
			count, _, ok := TableMessageQuantization(s)
			if !ok {
				return s, 0, false
			}
			s.Bits = int64(bits.Len32(count))
		default:
			return s, 0, false // a packing outside the closed set
		}
	case kind == TableKindString, kind == TableKindWstring:
		v, ok := leb()
		if !ok || v > math.MaxInt32 {
			return s, 0, false // A MAX ABOVE WHAT THE KIND CAN HOLD is a hostile width
		}
		s.Max = int64(v)
	case kind == TableKindArray, kind == TableKindKeyed:
		if kind == TableKindArray {
			v, ok := leb()
			if !ok || v > uint64(TableMessageListMax) {
				return s, 0, false
			}
			s.Min = int64(v)
		}
		v, ok := leb()
		if !ok || v > uint64(TableMessageListMax) {
			return s, 0, false
		}
		if int64(v) < s.Min {
			return s, 0, false // an array whose min exceeds its max
		}
		s.Max = int64(v)
		if at >= len(in) {
			return s, 0, false
		}
		s.Elem = in[at]
		at++
		if !TableMessageKnownKind(s.Elem) {
			return s, 0, false // an element kind outside the closed set
		}
		inner, n, ok := DecodeShape(in[at:], s.Elem)
		if !ok {
			return s, 0, false
		}
		at += n
		s.Inner = &inner
	}
	return s, at, true
}

// TableMessageKnownKind reports whether a kind is one of §3's closed set, plus
// the `0` an entry takes when its framing is not an entry's to give.
func TableMessageKnownKind(kind uint8) bool {
	switch {
	case kind == 0:
		return true
	case kind >= TableKindBool && kind <= TableKindPointer:
		return true
	case kind >= TableKindI128 && kind <= TableKindUFixed128:
		return true
	case kind >= TableKindEnum && kind <= TableKindWstring:
		return true
	}
	return false
}

func tableMessageIntegerKind(kind uint8) bool {
	return (kind >= TableKindI8 && kind <= TableKindU64) || kind == TableKindI128 || kind == TableKindU128
}

func tableMessageFixedKind(kind uint8) bool {
	return kind >= TableKindFixed8 && kind <= TableKindUFixed128
}

// tableMessageZigzag is the signed base's encoding: a small negative base
// costs the bytes a small positive one does.
func tableMessageZigzag(v *big.Int) uint64 {
	if v == nil {
		return 0
	}
	n := v.Int64()
	return uint64(n<<1) ^ uint64(n>>63)
}

func tableMessageUnzigzag(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// tableMessageUnsignedBase is an unsigned kind's base, whole: the domain's
// high half has a base a zigzag cannot spell, and an unsigned LEB128 can.
func tableMessageUnsignedBase(v *big.Int) uint64 {
	if v == nil {
		return 0
	}
	return v.Uint64()
}

// TableMessageKindBits is the widest RANGED value a kind can carry: its own
// storage width. A width above it is a hostile width on the announcement
// (§3.3), and bounding it here is what keeps every bit read under the
// primitive that holds it.
func TableMessageKindBits(kind uint8) int64 {
	return int64(TableKindWidth(int(kind))) * 8
}

// ---- the compressed float's rule, in float32 and by nothing else ----
//
// A compressed float quantizes by SPEC.md §4.3's rule (docs/SPEC-TABLES.md
// §3.3): the step count and the width derive from the announced triple at
// AnnounceRead exactly as the packet wire derives them at compile time, and
// the index and the float derive from those with TWO roundings on each side,
// never one, and never a float64 anywhere. The explicit float32 conversions
// on every product are what hold the second rounding: Go permits a compiler
// to fuse a multiply and an add across statements, and a conversion is the
// one thing that stops it.

// TableMessageQuantization is the triple's derivation: the step count and
// `delta`, and ok false for a triple SPEC.md calls non-conforming — `min` not
// below `max`, `res` not above zero, or a `delta` or `delta / res` not finite
// in float32.
func TableMessageQuantization(s TableMessageShape) (count uint32, delta float32, ok bool) {
	if !(s.QMin < s.QMax) || !(s.QRes > 0) {
		return 0, 0, false
	}
	delta = float32(s.QMax - s.QMin)
	values := float32(delta / s.QRes)
	if delta-delta != 0 || values-values != 0 {
		return 0, 0, false // Inf - Inf is NaN, and NaN - NaN is NaN
	}
	if !(values >= 1) {
		values = 1
	} else if values > 4294967040.0 {
		values = 4294967040.0 // the largest float below 2^32
	}
	return uint32(math.Ceil(float64(values))), delta, true
}

// TableMessageQuantize is the writer's half: the index a value takes.
func TableMessageQuantize(s TableMessageShape, value float32) uint32 {
	count, delta, ok := TableMessageQuantization(s)
	if !ok {
		return 0
	}
	normalized := float32((value - s.QMin) / delta)
	if !(normalized >= 0) {
		normalized = 0
	} else if !(normalized <= 1) {
		normalized = 1
	}
	scaled := float32(normalized * float32(count))
	index := uint32(math.Floor(float64(float32(scaled + 0.5))))
	// AND THE INTEGER CLAMPS TO `count`, which is the packet wire's own last
	// step (SPEC.md §4.3): the float clamp above already bounded the ratio, so
	// this catches only the rounding at the top of the range.
	return min(index, count)
}

// TableMessageDequantize is the reader's half: the float an index names.
func TableMessageDequantize(s TableMessageShape, index uint32) float32 {
	count, delta, ok := TableMessageQuantization(s)
	if !ok {
		return s.QMin
	}
	if index > count {
		index = count
	}
	normalized := float32(float32(index) / float32(count))
	scaled := float32(normalized * delta)
	return float32(scaled + s.QMin)
}

func appendWideBase(out []byte, v *big.Int) []byte {
	lo, hi := uint64(0), uint64(0)
	if v != nil {
		raw := new(big.Int).And(v, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)))
		lo = new(big.Int).And(raw, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
		hi = new(big.Int).Rsh(raw, 64).Uint64()
	}
	out = binary.LittleEndian.AppendUint64(out, lo)
	return binary.LittleEndian.AppendUint64(out, hi)
}

func wideBaseFrom(in []byte) *big.Int {
	lo := new(big.Int).SetUint64(binary.LittleEndian.Uint64(in))
	hi := new(big.Int).SetUint64(binary.LittleEndian.Uint64(in[8:]))
	raw := new(big.Int).Or(new(big.Int).Lsh(hi, 64), lo)
	if raw.Bit(127) == 1 {
		raw.Sub(raw, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	return raw
}

func readLebBytes(in []byte) (uint64, int, bool) {
	var v uint64
	for i, shift := 0, uint(0); i < len(in); i, shift = i+1, shift+7 {
		if shift >= 64 {
			return 0, 0, false
		}
		b := in[i]
		v |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			if i > 0 && b == 0 {
				return 0, 0, false // a longer spelling of the same number
			}
			return v, i + 1, true
		}
	}
	return 0, 0, false
}

// ---- one declaration's entry ----

// TableFieldEntry is one FIELD's announced entry: its id, the kind a header
// under it carries, and the shape that says how wide the payload is.
func TableFieldEntry(f *Field) TableVocabularyEntry {
	return TableVocabularyEntry{Id: TableFieldWireId(f), Kind: uint8(TableWireFieldKind(f)), Shape: TableFieldShape(f)}
}

// TableFieldShape is one field's shape at its own kind.
func TableFieldShape(f *Field) TableMessageShape {
	switch {
	case f.Type.Pointer && f.Array == ArrayNone:
		return TableMessageShape{} // a node index, whose width the node count settles
	case f.IsMap():
		inner := TableMessageShape{}
		return TableMessageShape{Min: 0, Max: tableMessageArrayMax(f), Elem: TableKindTable, Inner: &inner}
	case f.KeyEnum != "":
		inner := tableMessageElementShape(f)
		slots := int64(0)
		if f.KeyEnumRef != nil {
			slots = f.KeyEnumRef.Max
		}
		return TableMessageShape{Max: slots, Elem: uint8(TableWireElemKind(f)), Inner: &inner}
	case (f.Type.Kind == TString || f.Type.Kind == TWString) && !f.Type.Blob():
		return TableMessageShape{Max: f.Type.Size}
	case f.Type.Kind == TBytes && !f.Type.Blob():
		inner := TableMessageShape{Packing: TableMessageRaw}
		return TableMessageShape{Min: 0, Max: f.Type.Size, Elem: TableKindU8, Inner: &inner}
	case f.Array != ArrayNone:
		inner := tableMessageElementShape(f)
		min := int64(0)
		switch f.Array {
		case ArrayFixed:
			min = f.ArrayBound
		case ArrayCounted:
			min = f.ArrayMin
		}
		return TableMessageShape{Min: min, Max: tableMessageArrayMax(f), Elem: uint8(TableWireElemKind(f)), Inner: &inner}
	}
	return tableMessageElementShape(f)
}

func tableMessageArrayMax(f *Field) int64 {
	// AN UNBOUNDED ARRAY AND A MAP carry min 0 and max 2^32 - 1 (§3.3): a
	// number the data decides, with no bound to size a width from
	if f.IsList() || f.IsMap() {
		return TableMessageListMax
	}
	return f.ArrayBound
}

// tableMessageElementShape is ONE value's shape: what the packet wire writes
// for that declaration, at that width.
func tableMessageElementShape(f *Field) TableMessageShape {
	if f.Type.Pointer {
		return TableMessageShape{}
	}
	switch f.Type.Kind {
	case TBits:
		// a `bits(N)` rides in N, which is a range of [0, 2^N - 1] against a
		// base of zero
		return TableMessageShape{Packing: TableMessageRanged, Bits: int64(f.Type.Width), Base: big.NewInt(0)}
	case TFloat32:
		if f.HasFloatRange {
			return TableMessageShape{
				Packing: TableMessageQuantized,
				Bits:    CompressedFloatBits(f.FMin, f.FMax, f.Resolution),
				QMin:    float32(f.FMin),
				QMax:    float32(f.FMax),
				QRes:    float32(f.Resolution),
			}
		}
		return TableMessageShape{Packing: TableMessageRaw}
	case TInt:
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			return TableMessageShape{Packing: TableMessageRanged, Bits: BitsRequired(f.IntMin, f.IntMax), Base: f.IntMin}
		}
		return TableMessageShape{Packing: TableMessageRaw}
	case TFixed:
		if f.IntMin != nil && f.IntMax != nil {
			shift := uint(f.Type.FracBits)
			bits := int64(0)
			if f.IntMin.Cmp(f.IntMax) != 0 {
				bits = BitsRequired(f.IntMin, f.IntMax) + int64(f.Type.FracBits)
			}
			return TableMessageShape{Packing: TableMessageRanged, Bits: bits, Base: new(big.Int).Lsh(f.IntMin, shift)}
		}
		return TableMessageShape{Packing: TableMessageRaw}
	case TNamed:
		if fl, isFlags := f.Type.Ref.(*Flags); isFlags {
			// A `flags` MASK RIDES RAW at its wire width, which is a range of
			// [0, 2^bits - 1] against a base of zero
			return TableMessageShape{Packing: TableMessageRanged, Bits: int64(fl.WireBits), Base: big.NewInt(0)}
		}
	}
	return TableMessageShape{}
}

// TableArmEntry is one union ARM's announced entry, under the ARM NAME's id:
// an arm header is a field header (§2.6), so an arm this reader cannot name is
// skipped exactly as an unknown field is.
func TableArmEntry(v UnionVariant) TableVocabularyEntry {
	// AN ARM RENAMED UNDER `was` RIDES UNDER ITS OLD NAME'S HASH (§5), so the
	// entry's id is the wire name's and a rename moves no slot.
	id := TableWireId(v.WireName())
	switch {
	case v.Void():
		return TableVocabularyEntry{Id: id, Kind: TableKindNoPayload}
	case v.Body():
		return TableVocabularyEntry{Id: id, Kind: TableKindTable}
	}
	e := TableFieldEntry(v.F)
	e.Id = id
	return e
}

// TableMessageValueBits is one value's width in bits under a shape, and -1
// where the kind's payload is not a fixed-width value at all.
func TableMessageValueBits(kind uint8, s TableMessageShape) int64 {
	switch {
	case kind == TableKindBool:
		return 1
	case kind == TableKindF64:
		return 64
	case kind == TableKindF32:
		if s.Packing == TableMessageQuantized {
			return s.Bits
		}
		return 32
	case tableMessageIntegerKind(kind), tableMessageFixedKind(kind):
		if s.Packing == TableMessageRanged {
			return s.Bits
		}
		return int64(TableKindWidth(int(kind))) * 8
	}
	return -1
}

// TableMessageCountBits is an array's or a keyed array's count width, and 0
// where `min` equals `max` and no count rides at all.
func TableMessageCountBits(s TableMessageShape) int {
	return TableMessageBitsRequired(s.Min, s.Max)
}

// TableMessageAligns reports the two payloads that ALIGN to the next byte
// boundary before their bytes: a `string(N)`, and an array whose element kind
// is `6`, which is every `bytes(N)`. The align costs at most seven bits and
// buys a memcpy on the largest payload on the wire.
func TableMessageAligns(kind uint8, s TableMessageShape) bool {
	return kind == TableKindString || (kind == TableKindArray && s.Elem == TableKindU8)
}

// TableVocabularyName is the name this unit's closure gives an announced id,
// or "" where it names none: a field, a variant, an arm, a table, or one of
// the reserved ids by its own word.
func TableVocabularyName(u *Unit, id uint64) string {
	switch id {
	case TableNodeWireId:
		return "(node table)"
	case BytesWireTypeId:
		return "bytes"
	case StringWireTypeId:
		return "string"
	case WstringWireTypeId:
		return "wstring"
	}
	for member := range TableClosure(u) {
		if TableWireId(member) == id {
			return member
		}
		st := memberStruct(u, member)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if TableFieldWireId(f) == id {
				return f.Name
			}
			if f.IsMap() {
				for _, sub := range f.MapEntry.Fields {
					if TableFieldWireId(sub) == id {
						return sub.Name
					}
				}
			}
		}
	}
	for _, e := range u.Enums {
		for _, v := range e.Variants {
			if TableWireId(v) == id {
				return v
			}
		}
	}
	for _, un := range u.Unions {
		for _, v := range un.Variants {
			if TableWireId(v.Name) == id {
				return v.Name
			}
		}
	}
	return ""
}

// TableMessageShapeString spells one entry's shape for a reader: the facts
// its kind gives it, in the page's own words.
func TableMessageShapeString(kind uint8, s TableMessageShape) string {
	switch {
	case kind == 0:
		return "name"
	case tableMessageIntegerKind(kind), tableMessageFixedKind(kind):
		if s.Packing == TableMessageRanged {
			return fmt.Sprintf("ranged %d bits from %s", s.Bits, s.Base)
		}
		return "raw"
	case kind == TableKindF32:
		if s.Packing == TableMessageQuantized {
			return fmt.Sprintf("quantized %d bits over [%g, %g] at %g", s.Bits, s.QMin, s.QMax, s.QRes)
		}
		return "raw"
	case kind == TableKindString, kind == TableKindWstring:
		return fmt.Sprintf("max %d", s.Max)
	case kind == TableKindArray:
		return fmt.Sprintf("count %d..%d of kind %d %s", s.Min, s.Max, s.Elem, TableMessageShapeString(s.Elem, tableMessageInner(s)))
	case kind == TableKindKeyed:
		return fmt.Sprintf("%d slots of kind %d %s", s.Max, s.Elem, TableMessageShapeString(s.Elem, tableMessageInner(s)))
	}
	return ""
}
