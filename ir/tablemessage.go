// THE MESSAGE FORM'S BITPACKED BODY (docs/SPEC-TABLES.md §3.3): the wire law
// the C++ reference, the compiler's engine and the tool all read from one
// place.
//
// The message form is the table wire optimized for BANDWIDTH. A body's
// references are bits rather than bytes, there is no kind byte at all, and a
// value rides at its DECLARED width, which is the width the packet wire
// writes it at. What a reader needs to skip a field it cannot name is
// therefore not on the body at all: it is the ANNOUNCEMENT's per-entry
// record, one per vocabulary slot, carrying the kind, the widths and the
// range base the sender declared.
package ir

import (
	"encoding/binary"
	"math/big"
	"math/bits"
)

// TableMessageRefBits is the width of a REFERENCE on a bitpacked body: the
// bits that spell every slot of a table of `entries` entries, and the zero
// reference beside them (docs/SPEC-TABLES.md §3.3).
//
// A reference names a slot in `[0, entries]`, so it is `bits_required(0,
// entries)` — the same function the packet wire uses for a ranged integer,
// against the one bound the announcement already carries. A table of 29
// entries spends five bits a reference where the file form spent eight.
func TableMessageRefBits(entries int) int {
	if entries <= 0 {
		return 1 // a table of no entries still has to spell the terminator
	}
	return bits.Len64(uint64(entries))
}

// TableMessageBitsRequired is `bits_required(0, n)` over a non-negative bound:
// the width of a length, a count or an index whose maximum the declaration
// states.
func TableMessageBitsRequired(n int64) int {
	if n <= 0 {
		return 0
	}
	return bits.Len64(uint64(n))
}

// The per-entry record's FLAGS (docs/SPEC-TABLES.md §3.3).
const (
	// TableMessageAmbiguous marks an id the unit's closure gives MORE THAN
	// ONE shape — the same field name declared at two types in two records.
	// The record carries the first shape in projection order, and a reader
	// that cannot NAME the id cannot skip it: the body stops at that field,
	// one `malformed` counts, and the fields decoded before it stand, which
	// is §3's framing damage at the level it occurs. A reader that CAN name
	// the id never consults the record at all, so an ambiguous entry costs a
	// reader that knows the field nothing.
	TableMessageAmbiguous = 1 << 0
	// TableMessageLebLength marks a length whose bound the declaration does
	// not state — an unbounded array (§2.9) and the node table's payload —
	// so it rides as a bit LEB128 rather than at a fixed width.
	TableMessageLebLength = 1 << 1
	// TableMessageBitLength marks a length that counts BITS rather than
	// elements. The node table's payload is the one field that carries one,
	// because its records are a bit stream of their own and a byte count
	// could not frame them.
	TableMessageBitLength = 1 << 2
)

// TableMessageDescriptor is ONE vocabulary entry's record: what a field header
// under this id carries, in enough detail that a reader which cannot name the
// id skips it AT BIT GRANULARITY (docs/SPEC-TABLES.md §3.3).
//
// An entry that names no field shape — an enum variant's name, a table's own
// name id, a blob type id — carries kind 0 and nothing else, because no field
// header ever rides under it.
type TableMessageDescriptor struct {
	Kind       uint8 // the §3 kind a field header under this id carries, 0 for a name that is never a field
	ElemKind   uint8 // an array's or a keyed array's element kind, 0 otherwise
	LengthBits uint8 // the width of the length or count this kind frames itself with, 0 where it frames none
	ValueBits  uint8 // the width of one value at its declared range, 0 where the kind carries no fixed-width value
	Flags      uint8
	Min        int64 // the range base a ranged value is written against: the wire carries `value - Min`
}

// TableMessageRecordBytes is one record's size in the announcement, and the
// stride a reader indexes the array by: record `k` of a table of `E` entries
// begins at `TableMessageRecordBytes * (k-1)`, so a lookup is an index and
// never a search.
const TableMessageRecordBytes = 13

// Encode writes one record in the announcement's own byte order.
func (d TableMessageDescriptor) Encode(out []byte) []byte {
	out = append(out, d.Kind, d.ElemKind, d.LengthBits, d.ValueBits, d.Flags)
	return binary.LittleEndian.AppendUint64(out, uint64(d.Min))
}

// TableMessageDecodeDescriptor reads one record back.
func TableMessageDecodeDescriptor(in []byte) TableMessageDescriptor {
	return TableMessageDescriptor{
		Kind:       in[0],
		ElemKind:   in[1],
		LengthBits: in[2],
		ValueBits:  in[3],
		Flags:      in[4],
		Min:        int64(binary.LittleEndian.Uint64(in[5:13])),
	}
}

// TableMessageFieldDescriptor is one FIELD's record: the shape a header under
// its id carries on a bitpacked body.
//
// THE WIDTHS ARE THE PACKET WIRE'S. A ranged integer rides as `value - min` in
// `bits_required(min, max)` bits, a fixed-point value rides as its raw scaled
// integer over the shifted range, a `bits(N)` rides in N, a bool in one — and
// the announcement carries the range base beside the width, because the
// receiver's own range is its own and a message from another build is the
// ordinary case this wire exists for.
//
// FLOATS RIDE UNCOMPRESSED, at 32 or 64 bits. The packet wire quantizes a
// `f32 | min, max, res` onto its step count, which is lossy, and a message and
// its file form would then be two different values under one instance. The
// resolution is also a third parameter the record would have to carry for a
// receiver at another build to read it at all.
func TableMessageFieldDescriptor(f *Field) TableMessageDescriptor {
	d := TableMessageDescriptor{
		Kind:     uint8(TableWireFieldKind(f)),
		ElemKind: uint8(TableWireElemKind(f)),
	}
	switch {
	case f.Type.Pointer && f.Array == ArrayNone:
		// a POINTER is its node index and never its referent (§3.1)
		d.Kind, d.ElemKind, d.ValueBits = TableKindPointer, 0, PointerWireBits
		return d
	case f.IsMap():
		// A MAP RIDES AS AN ARRAY OF TABLES (§2.8) whose entries are
		// self-delimiting bodies, so its length is a count of entries and its
		// bound is the map's own.
		d.LengthBits = uint8(TableMessageBitsRequired(f.ArrayBound))
		if f.IsList() {
			d.Flags |= TableMessageLebLength
		}
		return d
	case f.KeyEnum != "":
		// an ENUM-KEYED array frames the number of PRESENT slots (§3.2)
		if f.KeyEnumRef != nil {
			d.LengthBits = uint8(TableMessageBitsRequired(f.KeyEnumRef.Max))
		}
		d.ValueBits = uint8(tableMessageValueBits(f))
		d.Min = tableMessageMin(f)
		return d
	case f.Type.Kind == TString && !f.Type.Blob():
		d.LengthBits, d.ValueBits = uint8(TableMessageBitsRequired(f.Type.Size)), 8
		return d
	case f.Type.Kind == TBytes && !f.Type.Blob():
		d.LengthBits, d.ValueBits = uint8(TableMessageBitsRequired(f.Type.Size)), 8
		return d
	case f.Array != ArrayNone:
		d.LengthBits = uint8(TableMessageBitsRequired(f.ArrayBound))
		if f.IsList() {
			d.Flags |= TableMessageLebLength
		}
		d.ValueBits = uint8(tableMessageValueBits(f))
		d.Min = tableMessageMin(f)
		return d
	}
	d.ValueBits = uint8(tableMessageValueBits(f))
	d.Min = tableMessageMin(f)
	return d
}

// tableMessageValueBits is ONE value's declared width in bits: a scalar's, or
// an array element's, which are the same width because the range is the
// FIELD's and every element of it rides under that one range.
//
// It answers 0 for every kind that frames itself — a table body, a union, an
// enum reference — because those carry no fixed-width value at all.
func tableMessageValueBits(f *Field) int {
	if f.Type.Pointer {
		return PointerWireBits
	}
	switch f.Type.Kind {
	case TBool:
		return 1
	case TBits:
		return f.Type.Width
	case TFloat32:
		return 32
	case TFloat64:
		return 64
	case TString, TBytes:
		return 8
	case TInt:
		if f.Type.Width > 64 {
			return f.Type.Width // a 128-bit integer rides at its storage width
		}
		if f.HasIntRange {
			return int(BitsRequired(f.IntMin, f.IntMax))
		}
		return f.Type.Width
	case TFixed:
		if f.Type.Width > 64 {
			return f.Type.Width
		}
		if f.IntMin != nil && f.IntMax != nil {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				return 0 // the degenerate range costs no bit at all
			}
			return int(BitsRequired(f.IntMin, f.IntMax)) + f.Type.FracBits
		}
		return f.Type.Width
	case TNamed:
		if _, isFlags := f.Type.Ref.(*Flags); isFlags {
			return TableKindWidth(TableScalarKind(f)) * 8
		}
	}
	return 0
}

// tableMessageMin is the RANGE BASE the wire writes a value against, so that
// the payload is `value - Min` and a reader at another build adds the SENDER's
// base back. It is 0 for every unranged field and for every 128-bit one.
func tableMessageMin(f *Field) int64 {
	if f.Type.Pointer || f.Type.Width > 64 {
		return 0
	}
	switch f.Type.Kind {
	case TInt:
		if f.HasIntRange && f.IntMin != nil {
			return bigToInt64(f.IntMin)
		}
	case TFixed:
		if f.IntMin != nil && f.IntMax != nil && f.IntMin.Cmp(f.IntMax) != 0 {
			// the raw scaled base: the whole-unit minimum shifted by F, which
			// is the integer the storage holds at that minimum
			return bigToInt64(new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits)))
		}
	}
	return 0
}

func bigToInt64(v *big.Int) int64 {
	if v == nil || !v.IsInt64() {
		return 0
	}
	return v.Int64()
}

// TableMessageNodeDescriptor is the reserved NODE-TABLE id's own record. Its
// payload is the numbering's records as a BIT STREAM, framed by a bit LEB128
// of its BIT count, because a byte count could not frame a stream that never
// aligns (§3.1, §3.3).
func TableMessageNodeDescriptor() TableMessageDescriptor {
	return TableMessageDescriptor{
		Kind:  TableKindString,
		Flags: TableMessageLebLength | TableMessageBitLength,
	}
}

// TableVocabularyRecords is the announcement's per-entry array, one CANONICAL
// record per slot of [TableVocabulary], in that order (docs/SPEC-TABLES.md
// §3.3).
//
// THE RECORD IS THE WIRE CONTRACT FOR ITS ID, and both halves write to it: a
// field's header rides at the record's widths and against its range base, not
// at the field's own. One id declared at two BOUNDS in two records — a
// `string(16)` here and a `string(32)` there, an `int32 | min = 0, max = 100`
// beside a bare one — is legal and the file form reads it by the kind byte the
// body carries. A bitpacked body has no kind byte, so the widths a reader
// spends must be a function of the ID ALONE, and the canonical shape is the
// WIDEST of them: the union of the ranges, the larger bound, and an unbounded
// length wherever one occurrence is unbounded. A field then costs the bits its
// id costs, which is a little more than its own declaration would have spent
// and is what makes the id skippable.
//
// An id whose occurrences differ in KIND cannot be reconciled that way: there
// is no shape that spells a `string` and a `u32`. The entry is marked
// AMBIGUOUS, a message-form save of a root that reaches such a field refuses
// by name, and the file form is untouched.
func TableVocabularyRecords(u *Unit) []TableMessageDescriptor {
	vocabulary := TableVocabulary(u)
	slot := make(map[uint64]int, len(vocabulary))
	for i, id := range vocabulary {
		slot[id] = i
	}
	shapes := make([]*tableMessageShape, len(vocabulary))
	merge := func(id uint64, d TableMessageDescriptor, f *Field) {
		at, known := slot[id]
		if !known {
			return
		}
		if shapes[at] == nil {
			shapes[at] = &tableMessageShape{}
		}
		shapes[at].merge(d, f)
	}

	var noteUnion func(un *Union)
	seen := map[*Union]bool{}
	noteField := func(f *Field) {
		merge(TableFieldWireId(f), TableMessageFieldDescriptor(f), f)
		if f.IsMap() {
			for _, sub := range f.MapEntry.Fields {
				merge(TableFieldWireId(sub), TableMessageFieldDescriptor(sub), sub)
			}
		}
		if f.Type.Kind == TNamed {
			if un, isUnion := f.Type.Ref.(*Union); isUnion {
				noteUnion(un)
			}
		}
	}
	noteUnion = func(un *Union) {
		if seen[un] {
			return
		}
		seen[un] = true
		for _, v := range un.Variants {
			// AN ARM HEADER IS A FIELD HEADER (§3): an arm's name id carries
			// the arm's own payload shape, so an arm this reader cannot name
			// is skipped exactly as an unknown field is.
			merge(TableWireId(v.Name), tableMessageArmDescriptor(v), v.F)
			if v.F != nil {
				noteField(v.F)
			}
		}
	}
	for name := range TableClosure(u) {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			noteField(f)
		}
	}
	merge(TableNodeWireId, TableMessageNodeDescriptor(), nil)

	out := make([]TableMessageDescriptor, len(vocabulary))
	for i, sh := range shapes {
		if sh != nil {
			out[i] = sh.record()
		}
	}
	return out
}

// tableMessageShape accumulates every declaration one id carries, and answers
// the CANONICAL record the announcement publishes for it.
type tableMessageShape struct {
	first    TableMessageDescriptor
	conflict bool // the occurrences differ in kind, and no shape spells both

	bound     int64 // the widest declared length or count bound
	leb       bool  // one occurrence's bound is not declared at all
	unranged  bool  // one occurrence rides at its raw storage width
	ranged    bool
	min, max  *big.Int
	valueBits int
}

func (sh *tableMessageShape) merge(d TableMessageDescriptor, f *Field) {
	if sh.valueBits == 0 && !sh.ranged && !sh.unranged && sh.first.Kind == 0 && !sh.conflict {
		sh.first = d
	} else if sh.first.Kind != d.Kind || sh.first.ElemKind != d.ElemKind {
		sh.conflict = true
	}
	if d.Flags&TableMessageLebLength != 0 {
		sh.leb = true
	}
	if b := int64(1)<<uint(d.LengthBits) - 1; b > sh.bound {
		sh.bound = b
	}
	if int(d.ValueBits) > sh.valueBits {
		sh.valueBits = int(d.ValueBits)
	}
	lo, hi, ranged := tableMessageRange(f)
	if !ranged {
		sh.unranged = true
		return
	}
	sh.ranged = true
	if sh.min == nil || lo.Cmp(sh.min) < 0 {
		sh.min = lo
	}
	if sh.max == nil || hi.Cmp(sh.max) > 0 {
		sh.max = hi
	}
}

func (sh *tableMessageShape) record() TableMessageDescriptor {
	d := sh.first
	d.LengthBits = uint8(TableMessageBitsRequired(sh.bound))
	if sh.leb {
		d.Flags |= TableMessageLebLength
	}
	switch {
	case sh.conflict:
		d.Flags |= TableMessageAmbiguous
	case sh.ranged && !sh.unranged:
		// EVERY occurrence is ranged, so the canonical range is their union
		d.ValueBits = uint8(BitsRequired(sh.min, sh.max))
		d.Min = bigToInt64(sh.min)
	default:
		d.ValueBits = uint8(sh.valueBits)
		d.Min = 0
	}
	return d
}

// tableMessageRange is one field's declared value range on the RAW scale a
// bitpacked body writes, and ranged is false for a field that rides at its
// storage width — an unranged integer, a float, a bool, or anything whose
// payload is not a number at all.
func tableMessageRange(f *Field) (lo, hi *big.Int, ranged bool) {
	if f == nil || f.Type.Pointer || f.Type.Width > 64 {
		return nil, nil, false
	}
	switch f.Type.Kind {
	case TInt:
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			return f.IntMin, f.IntMax, true
		}
	case TFixed:
		if f.IntMin != nil && f.IntMax != nil && f.IntMin.Cmp(f.IntMax) != 0 {
			shift := uint(f.Type.FracBits)
			return new(big.Int).Lsh(f.IntMin, shift), new(big.Int).Lsh(f.IntMax, shift), true
		}
	}
	return nil, nil, false
}

// tableMessageArmDescriptor is one ARM's record. A payload-free arm rides
// under kind 32 and carries nothing at all; a declared-type arm is a
// self-delimiting body; every other arm is exactly what a FIELD of that type
// would carry (§2.6).
func tableMessageArmDescriptor(v UnionVariant) TableMessageDescriptor {
	switch {
	case v.Void():
		return TableMessageDescriptor{Kind: TableKindNoPayload}
	case v.Body():
		return TableMessageDescriptor{Kind: TableKindTable}
	}
	return TableMessageFieldDescriptor(v.F)
}
