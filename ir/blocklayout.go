// The BLOCK FORM's layout model (docs/SPEC-TABLES.md §2.7, §19).
//
// The compiler computes the layout and both backends assert it: this file is
// the one place the numbers come from, so C++'s static_asserts, C#'s generated
// size/offset checks, the baseline's layout lines and the layout digest can
// never disagree about what the layout IS. A backend that derived its own
// would be a second model, and a two-language contract held by two models is
// held by neither (§19.3).
//
// The rule is the C ABI's natural one: each field at its own alignment, the
// record's alignment the greatest of its fields', the size rounded up to it.
// That covers the PROJECTION as much as a row — the table at the front of a
// block is a record too.
package ir

import "sort"

// BlockPrologueBytes is the projection's generated prologue: three uint64s —
// `magic`, which identifies a schema block; `build_version`, the digest §20
// defines; and `byte_order`, which a producer stamps with its own. They are
// generated exactly as an optional's presence companion is, and a field may
// not be named after any of the three (§11).
const BlockPrologueBytes = 24

// Byte-order values a block's prologue carries (docs/SPEC-TABLES.md §20.3's byte).
// A block written by a build of the other order is REFUSED: the fix-up path is
// a named obligation, not something a consumer improvises.
const (
	BlockByteOrderLittle = 1
	BlockByteOrderBig    = 2
)

// BlockAlign is the alignment every block base and every out-of-line array
// start takes: a cache line, so two workers filling different arrays never
// share one (docs/SPEC-TABLES.md §19.1).
const BlockAlign = 64

// BlockMagic identifies a schema block and carries the byte-order check with
// it (docs/SPEC-TABLES.md §19.1). It is stored in the producer's NATIVE order; a
// consumer that reads back the byte-swapped value has found a foreign byte
// order and refuses, and one that reads back anything else has not found a
// block at all.
//
// The value is "SCHMABLK" read as ASCII in the low-to-high byte order a
// little-endian store produces, which makes a hex dump of a block legible.
const BlockMagic = uint64(0x4B4C42414D484353)

// MemberLayout is one C-ABI record's computed layout: its size, its alignment
// and the offset, size and alignment of every field of it.
type MemberLayout struct {
	Name   string
	Size   int64
	Align  int64
	Fields []FieldLayout
}

// FieldLayout is one field's position inside a record. Offset and Size cover
// the field's WHOLE storage — an array's elements together, a string's buffer
// AND its length companion, an optional's value AND its presence bool — so
// "the byte offset and size that field has" is one pair, exactly as the
// baseline records it (§18.1).
type FieldLayout struct {
	Field  *Field
	Offset int64
	Size   int64
	Align  int64
}

// FieldByName returns a record's layout entry for one field name.
func (m *MemberLayout) FieldByName(name string) *FieldLayout {
	for i := range m.Fields {
		if m.Fields[i].Field.Name == name {
			return &m.Fields[i]
		}
	}
	return nil
}

// BlockArray is one out-of-line array of a block-form table: the field, its
// element, the derived pitch and the declared maximum, plus where its
// (offset_of, count, stride) triple sits in the projection.
type BlockArray struct {
	Field    *Field
	Elem     *Struct
	ElemName string
	Stride   int64 // the pitch: the element's sizeof (docs/SPEC-TABLES.md §2.7)
	Max      int64 // the declared [..N] maximum — sizes the storage, NOT a digest fact
	// the triple's position in the projection: sixteen bytes with no interior
	// padding, at the field's own position (docs/SPEC-TABLES.md §2.7)
	TripleOffset   int64
	OffsetOfOffset int64
	CountOffset    int64
	StrideOffset   int64
}

// BlockLayout is one block-form table's whole computed layout: its projection
// record, its out-of-line arrays in declaration order, the storage size the
// declared maxima sum to, and the digest both sides assert.
type BlockLayout struct {
	Table      *Struct
	Projection MemberLayout // the projection record, prologue included
	Arrays     []BlockArray
	MaxBytes   int64 // <Table>BlockMaxBytes: the projection plus every array at its maximum
}

// ArrayByName returns one out-of-line array by field name.
func (b *BlockLayout) ArrayByName(name string) *BlockArray {
	for i := range b.Arrays {
		if b.Arrays[i].Field.Name == name {
			return &b.Arrays[i]
		}
	}
	return nil
}

// BlockUnit is a unit's whole block-form surface: every FIXED table that has
// one, the layout of every record the form touches, and — for the tables that
// do not have one — the reason, so the generated file can say it rather than
// leave a reader to guess.
//
// NOTHING DECLARES THE BLOCK FORM. Every fixed table has one and it is emitted
// ON THE SIDE, in <Base>Block.h / <Base>Block.cpp / <Base>Block.cs, which a
// consumer includes and links only if it uses the form. The ordinary
// <Base>Table.h carries not one symbol of it, which is what the zero-cost gate
// (§2.2) asks.
type BlockUnit struct {
	Tables  []*BlockLayout           // every table with a block form, sorted by name
	Members map[string]*MemberLayout // every record in the block closure, by name
	Order   []string                 // the closure's member names, sorted
	// Skipped names each table that has NO block form, and why in one clause.
	Skipped map[string]string
}

// Layout returns one closure member's computed layout, or nil.
func (b *BlockUnit) Layout(name string) *MemberLayout {
	if b == nil {
		return nil
	}
	return b.Members[name]
}

// Block returns one marked table's block layout, or nil.
func (b *BlockUnit) Block(name string) *BlockLayout {
	if b == nil {
		return nil
	}
	for _, bl := range b.Tables {
		if bl.Table.Name == name {
			return bl
		}
	}
	return nil
}

// IsBlock reports whether a name is a marked block-form table.
func (b *BlockUnit) IsBlock(name string) bool { return b.Block(name) != nil }

// InClosure reports whether a name is a record the block form touches — a
// marked table, or anything one of its out-of-line arrays reaches by value.
func (b *BlockUnit) InClosure(name string) bool {
	if b == nil {
		return false
	}
	_, ok := b.Members[name]
	return ok
}

// SkippedReason is why one table has no block form, or "" when it has one.
func (b *BlockUnit) SkippedReason(name string) string {
	if b == nil {
		return ""
	}
	return b.Skipped[name]
}

// blockFormable answers whether one table has a block form, and why not when
// it does not. Two answers, and both are properties of the DECLARATION rather
// than of anything an author writes to ask for the form:
//
//   - a VARIABLE-LENGTH table has none: a pointer anywhere in its by-value
//     closure means no fixed pitch anywhere in it (docs/SPEC-TABLES.md §19).
//   - a table whose closure carries a UNION has none: §19.3 pins the C# side
//     to Sequential with generated padding, and Sequential cannot overlay
//     arms. Emitting the form on one side only would break the two-language
//     contract the form exists to be, so neither side emits it and both say
//     so. A union in a block closure is a named follow-on (§15).
func blockFormable(u *Unit, st *Struct, variable map[string]bool) (bool, string) {
	if variable[st.Name] {
		return false, "it is VARIABLE-LENGTH: a pointer in its by-value closure means no fixed pitch anywhere in it"
	}
	seen := map[string]bool{}
	var walk func(name string) string
	walk = func(name string) string {
		if seen[name] {
			return ""
		}
		seen[name] = true
		member := memberStruct(u, name)
		if member == nil {
			return ""
		}
		for _, f := range member.Fields {
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Union:
				return name + "." + f.Name + " is a union, and a block's blittable C# form is Sequential with generated padding, which cannot overlay arms"
			case *Struct:
				if why := walk(ref.Name); why != "" {
					return why
				}
			}
		}
		return ""
	}
	if why := walk(st.Name); why != "" {
		return false, why
	}
	return true, ""
}

// Blocks computes the whole block surface of a unit, or nil when the unit
// declares no table at all. Every FIXED table gets one; a table that cannot
// carry the form is recorded in Skipped with the reason, never silently
// dropped.
func Blocks(u *Unit) *BlockUnit {
	if len(u.Tables) == 0 {
		return nil
	}
	variable := VariableTables(u)
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	b := &BlockUnit{Members: map[string]*MemberLayout{}, Skipped: map[string]string{}}
	var marked []*Struct
	for _, name := range names {
		st := u.Tables[name]
		ok, why := blockFormable(u, st, variable)
		if !ok {
			b.Skipped[name] = why
			continue
		}
		marked = append(marked, st)
	}
	if len(marked) == 0 {
		return b
	}
	// every record the form touches: each marked table's BY-VALUE layout is
	// not needed (the projection replaces it), but the ROW types are, and so
	// is everything they nest by value.
	add := func(name string) {
		var walk func(name string)
		walk = func(name string) {
			if _, done := b.Members[name]; done {
				return
			}
			st := memberStruct(u, name)
			if st == nil {
				return
			}
			ml := layoutRecord(u, st)
			b.Members[name] = ml
			for _, f := range st.Fields {
				if f.Type.Kind == TNamed {
					if ref, ok := f.Type.Ref.(*Struct); ok {
						walk(ref.Name)
					}
				}
			}
		}
		walk(name)
	}
	for _, st := range marked {
		bl := &BlockLayout{Table: st}
		bl.Projection = layoutProjection(u, st)
		for _, fl := range bl.Projection.Fields {
			f := fl.Field
			if !BlockOutOfLine(f) {
				continue
			}
			elem, _ := f.Type.Ref.(*Struct)
			add(elem.Name)
			el := b.Members[elem.Name]
			bl.Arrays = append(bl.Arrays, BlockArray{
				Field:          f,
				Elem:           elem,
				ElemName:       elem.Name,
				Stride:         el.Size,
				Max:            f.ArrayBound,
				TripleOffset:   fl.Offset,
				OffsetOfOffset: fl.Offset,
				CountOffset:    fl.Offset + 8,
				StrideOffset:   fl.Offset + 12,
			})
		}
		// every field of the projection that is not a triple still names a
		// record whose layout the other side asserts
		for _, fl := range bl.Projection.Fields {
			if fl.Field.Type.Kind == TNamed {
				if ref, ok := fl.Field.Type.Ref.(*Struct); ok && !BlockOutOfLine(fl.Field) {
					add(ref.Name)
				}
			}
		}
		bl.MaxBytes = blockMaxBytes(bl)
		b.Tables = append(b.Tables, bl)
	}
	for name := range b.Members {
		b.Order = append(b.Order, name)
	}
	sort.Strings(b.Order)
	return b
}

// BlockOutOfLine reports whether a field of a block-form table is one of the
// arrays that moves out of line: DEPTH ONE, BOUNDED ONLY (docs/SPEC-TABLES.md
// §2.7) — the marked table's own `[..N]T` fields whose element is a struct.
// A fixed `[N]T`, an enum-keyed `[E]T`, and every array at any depth inside an
// element stay exactly where they are.
func BlockOutOfLine(f *Field) bool {
	if f.Array != ArrayCounted || f.KeyEnum != "" {
		return false
	}
	if f.Type.Kind != TNamed || f.Type.Pointer {
		return false
	}
	_, isStruct := f.Type.Ref.(*Struct)
	return isStruct
}

// RecordLayout is one closure record's C ABI layout — the model §20.3 commits
// the compiler to for EVERY record in a unit's table closure, not only the ones
// a block form reaches: each field at its own alignment, the record's alignment
// the greatest of its fields', the size rounded up to it.
//
// It is what a COOK's region is laid out by (§7): a cooked node is this record
// at these offsets, so the one model that C++'s static_asserts, C#'s generated
// checks and the build version's `record` lines all come from is the one model
// the cook's bytes come from too. A second walk here would be a second ABI.
func RecordLayout(u *Unit, st *Struct) *MemberLayout { return layoutRecord(u, st) }

// RegionAlignFloor is the floor on a COOK's region alignment (docs/SPEC-TABLES.md
// §7): the alignment a region actually needs is the greatest alignment of any
// record in it, which for a region of byte-only records would be 1, and the
// floor holds it at eight so the attribution part that follows the data is
// aligned for its own `u64` pairs without a second padding rule.
const RegionAlignFloor = int64(8)

// RegionAlignOf is a region's alignment given the alignments of the records in
// it: the greatest of them, never below the floor.
func RegionAlignOf(aligns ...int64) int64 {
	a := RegionAlignFloor
	for _, v := range aligns {
		if v > a {
			a = v
		}
	}
	return a
}

// FieldPieces is one field's contiguous storage members in order, as the
// generated record declares them: a `string(N)` is a `char[N+1]` buffer AND an
// int32 used length, a counted array is its elements AND an int32 count, an
// optional adds a presence bool. A cook writes a region PIECE BY PIECE rather
// than copying a struct, because the byte order is settled at cook time (§7)
// and a swap has to know where every scalar begins.
func FieldPieces(u *Unit, f *Field, fieldOffset int64) []BlockFieldPiece {
	return BlockFieldPieceOffsets(u, f, fieldOffset, false)
}

// UnionLayout is a generated union's own layout: the tag at offset 0 at its own
// alignment, the arms overlaid at the greatest arm alignment, the whole rounded
// up. It is the layout [RecordLayout] already folds into a union-typed field's
// size, exposed so a cook can write the SET arm at the arm offset and zero the
// rest of the extent (§7).
func UnionLayout(u *Unit, un *Union) (size, align, tag, armOffset int64) {
	tag = int64(StorageBitsFor(un.Max)) / 8
	align = tag
	var armAlign, armSize int64 = 1, 0
	for _, v := range un.Variants {
		arm := memberStruct(u, v.Type)
		if arm == nil {
			continue
		}
		ml := layoutRecord(u, arm)
		if ml.Align > armAlign {
			armAlign = ml.Align
		}
		if ml.Size > armSize {
			armSize = ml.Size
		}
	}
	if armAlign > align {
		align = armAlign
	}
	armOffset = alignUp(tag, armAlign)
	size = alignUp(armOffset+armSize, align)
	return size, align, tag, armOffset
}

func memberStruct(u *Unit, name string) *Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// ---- the C ABI walk ----

// storagePiece is one contiguous member of a generated record: the pieces a
// field's storage is spelled as. A string is a buffer piece plus a length
// piece; a counted array is an elements piece plus a count piece; an optional
// adds a presence piece. Laying the pieces out in order IS the C ABI walk.
type storagePiece struct {
	size  int64
	align int64
}

func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

// layoutRecord walks one generated record's storage the way a C compiler does.
func layoutRecord(u *Unit, st *Struct) *MemberLayout {
	ml := &MemberLayout{Name: st.Name}
	offset := int64(0)
	maxAlign := int64(1)
	for _, f := range st.Fields {
		pieces := fieldPieces(u, f, false)
		start := int64(-1)
		var fieldAlign int64 = 1
		for _, p := range pieces {
			if p.align > maxAlign {
				maxAlign = p.align
			}
			if p.align > fieldAlign {
				fieldAlign = p.align
			}
			offset = alignUp(offset, p.align)
			if start < 0 {
				start = offset
			}
			offset += p.size
		}
		if start < 0 {
			continue
		}
		ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start, Size: offset - start, Align: fieldAlign})
	}
	ml.Align = maxAlign
	ml.Size = alignUp(offset, maxAlign)
	if ml.Size == 0 {
		// an empty record is one byte in both languages
		ml.Size = 1
	}
	return ml
}

// layoutProjection walks a block-form table's PROJECTION: the generated
// prologue, then the table's own fields at their natural offsets, with every
// out-of-line array's storage replaced IN PLACE by its sixteen-byte
// (offset_of u64, count u32, stride u32) triple (docs/SPEC-TABLES.md §2.7).
func layoutProjection(u *Unit, st *Struct) MemberLayout {
	ml := MemberLayout{Name: st.Name}
	// the prologue: three uint64s (magic, build_version, byte_order)
	offset := int64(BlockPrologueBytes)
	maxAlign := int64(8)
	for _, f := range st.Fields {
		pieces := fieldPieces(u, f, true)
		start := int64(-1)
		var fieldAlign int64 = 1
		for _, p := range pieces {
			if p.align > maxAlign {
				maxAlign = p.align
			}
			if p.align > fieldAlign {
				fieldAlign = p.align
			}
			offset = alignUp(offset, p.align)
			if start < 0 {
				start = offset
			}
			offset += p.size
		}
		if start < 0 {
			continue
		}
		ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start, Size: offset - start, Align: fieldAlign})
	}
	ml.Align = maxAlign
	ml.Size = alignUp(offset, maxAlign)
	return ml
}

// BlockFieldPieceOffsets is the absolute offset of each contiguous PIECE of
// one field's storage, given where the field starts.
//
// A field is not always one piece: a `string(N)` is a buffer AND an int32 used
// length, a counted array is its elements AND an int32 count, an optional adds
// a presence bool. The C ABI aligns each piece on its own, so a field can carry
// INTERIOR padding — `char[49]` followed by an `int32_t` puts two bytes between
// them — and a port that lays a record out field by field, padding only BETWEEN
// fields, silently slides everything after it.
//
// The C# blittable emitter walks this rather than deriving it, because the two
// derivations disagreeing is exactly the defect it exists to prevent (§19.3).
func BlockFieldPieceOffsets(u *Unit, f *Field, fieldOffset int64, projection bool) []BlockFieldPiece {
	pieces := fieldPieces(u, f, projection)
	out := make([]BlockFieldPiece, 0, len(pieces))
	at := fieldOffset
	for _, p := range pieces {
		at = alignUp(at, p.align)
		out = append(out, BlockFieldPiece{Offset: at, Size: p.size})
		at += p.size
	}
	return out
}

// BlockFieldPiece is one contiguous member of a field's generated storage:
// where it starts inside the record, and how many bytes it takes.
type BlockFieldPiece struct {
	Offset int64
	Size   int64
}

// fieldPieces spells one field's storage as the contiguous pieces the
// generated record declares, in order. `projection` selects the block form's
// projection spelling, in which an out-of-line array is its triple.
func fieldPieces(u *Unit, f *Field, projection bool) []storagePiece {
	if projection && BlockOutOfLine(f) {
		// (offset_of u64, count u32, stride u32) — ONE sixteen-byte piece with
		// no interior padding, at the field's own position. It is one piece and
		// not three because both backends spell it as one member of a triple
		// TYPE, and a port that walked three would account for eight bytes
		// where sixteen were written.
		return []storagePiece{{size: 16, align: 8}}
	}
	var pieces []storagePiece
	switch {
	case f.Type.Pointer:
		// TableRef: EIGHT bytes at eight (docs/SPEC-TABLES.md §6.3). The slot holds
		// an arena offset in one form and a self-relative region delta in the
		// other, and the region delta is what sizes it: at eight bytes one
		// region reaches everything, which is the scale §7 is built for.
		pieces = append(pieces, storagePiece{size: 8, align: 8})
	case f.Type.Kind == TString:
		pieces = append(pieces, storagePiece{size: f.Type.Size + 1, align: 1}) // char[N+1]
		pieces = append(pieces, storagePiece{size: 4, align: 4})               // int32 length
	case f.Type.Kind == TBytes:
		pieces = append(pieces, storagePiece{size: f.Type.Size, align: 1}) // uint8[N]
		pieces = append(pieces, storagePiece{size: 4, align: 4})           // int32 length
	case f.KeyEnum != "":
		e := elementPiece(u, f)
		pieces = append(pieces, storagePiece{size: e.size * f.ArrayBound, align: e.align})
	case f.Array == ArrayFixed:
		e := elementPiece(u, f)
		pieces = append(pieces, storagePiece{size: e.size * f.ArrayBound, align: e.align})
	case f.Array == ArrayCounted:
		e := elementPiece(u, f)
		pieces = append(pieces, storagePiece{size: e.size * f.ArrayBound, align: e.align})
		pieces = append(pieces, storagePiece{size: 4, align: 4}) // int32 count
	default:
		pieces = append(pieces, elementPiece(u, f))
	}
	if f.Type.Optional {
		pieces = append(pieces, storagePiece{size: 1, align: 1}) // bool present
	}
	return pieces
}

// elementPiece is one VALUE of a field's declared type — the array element, or
// the scalar itself.
func elementPiece(u *Unit, f *Field) storagePiece {
	t := f.Type
	switch t.Kind {
	case TBool:
		return storagePiece{size: 1, align: 1}
	case TFloat32:
		return storagePiece{size: 4, align: 4}
	case TFloat64:
		return storagePiece{size: 8, align: 8}
	case TInt, TFixed:
		// a 128-bit integer, and a fixed of 128 bits, is SIXTEEN BYTES AT
		// SIXTEEN — the C ABI's natural alignment for a 128-bit integer, and
		// the one the C++ side spells out where its storage is the emulated
		// pair (docs/SPEC-TABLES.md §7.2, §19.3)
		w := int64(t.Width) / 8
		return storagePiece{size: w, align: w}
	case TBits:
		if t.Width <= 32 {
			return storagePiece{size: 4, align: 4}
		}
		return storagePiece{size: 8, align: 8}
	case TNamed:
		switch ref := t.Ref.(type) {
		case *Enum:
			w := int64(StorageBitsFor(ref.Max)) / 8
			return storagePiece{size: w, align: w}
		case *Flags:
			return storagePiece{size: 8, align: 8} // uint64 in every target
		case *Struct:
			ml := layoutRecord(u, ref)
			return storagePiece{size: ml.Size, align: ml.Align}
		case *Union:
			// the generated C++ union is a TAG beside an anonymous union of
			// the arms: the tag at offset 0 at its own alignment, the arms
			// overlaid at the greatest arm alignment, the whole rounded up.
			// A union has no blittable C# spelling under §19.3's Sequential
			// rule, so it keeps a table out of the BLOCK form — but its
			// layout is still a fact the build version folds in.
			tag := int64(StorageBitsFor(ref.Max)) / 8
			size, align := tag, tag
			var armAlign, armSize int64 = 1, 0
			for _, v := range ref.Variants {
				arm := memberStruct(u, v.Type)
				if arm == nil {
					continue
				}
				ml := layoutRecord(u, arm)
				if ml.Align > armAlign {
					armAlign = ml.Align
				}
				if ml.Size > armSize {
					armSize = ml.Size
				}
			}
			if armAlign > align {
				align = armAlign
			}
			size = alignUp(size, armAlign) + armSize
			return storagePiece{size: alignUp(size, align), align: align}
		}
	}
	// TString/TBytes are handled by the caller
	return storagePiece{size: 1, align: 1}
}

// ---- the extent ----

// BlockArrayStart is where one out-of-line array begins, given the array
// starts before it: aligned to max( 64, alignof( element ) ) (§19.1).
func BlockArrayStart(offset, elemAlign int64) int64 {
	return alignUp(offset, max(int64(BlockAlign), elemAlign))
}

// BlockExtent lays a block out from its counts — the same walk the generated
// Begin does, and the one the layout numbers in §19.1 come from. It returns
// each array's start and the used extent.
func BlockExtent(bl *BlockLayout, elemAligns []int64, counts []int64) (starts []int64, used int64) {
	offset := bl.Projection.Size
	starts = make([]int64, len(bl.Arrays))
	for i, a := range bl.Arrays {
		starts[i] = BlockArrayStart(offset, elemAligns[i])
		offset = starts[i] + counts[i]*a.Stride
	}
	used = alignUp(offset, BlockAlign)
	if used < bl.Projection.Size {
		used = alignUp(bl.Projection.Size, BlockAlign)
	}
	return starts, used
}

// blockMaxBytes is <Table>BlockMaxBytes: the projection plus every out-of-line
// array at its declared maximum, laid out by the same rule (§19.1). One
// extent, allocated once, never grown, never pooled.
func blockMaxBytes(bl *BlockLayout) int64 {
	offset := bl.Projection.Size
	for _, a := range bl.Arrays {
		offset = BlockArrayStart(offset, elemAlignOf(a))
		offset += a.Max * a.Stride
	}
	return alignUp(offset, BlockAlign)
}

// elemAlignOf is one array element's alignment. The stride is the element's
// sizeof and the size is a multiple of the alignment, so the alignment divides
// the stride — which is what makes every row start aligned with no case.
func elemAlignOf(a BlockArray) int64 {
	// derived from the stride: a standard-layout record's size is a multiple
	// of its alignment, and the alignment is the greatest power of two that
	// divides it, bounded by 16 — the largest scalar alignment this language
	// has. Computing it here rather than carrying it keeps BlockArray a
	// description of the WIRE-visible facts.
	align := int64(1)
	for align < 16 && a.Stride%(align*2) == 0 {
		align *= 2
	}
	return align
}

// ElemAlign is elemAlignOf, exported for the backends and the tests.
func (a BlockArray) ElemAlign() int64 { return elemAlignOf(a) }

// ---- what a GENERIC ROW WALK needs (docs/SPEC-TABLES.md §8.1, §19.2) ----

// BlockFieldFacts is one field of a block record, described in the vocabulary
// §8.1's table descriptors already use, so ONE walker reads a cooked node and a
// block row without learning a second one.
//
// Where the field STARTS is already in the layout; this is everything a walker
// needs after that — how many slots it holds, how wide one is, and where the
// companions live. Without it the descriptors name a `string(15)` as twenty
// bytes at an offset and no reader can tell where the sixteen-byte buffer stops
// and the used length begins.
type BlockFieldFacts struct {
	IsArray       bool  // inline storage of ArrayBound slots at ElemSize (`bytes` included)
	Counted       bool  // CountOffset names an int32 used-length companion
	Optional      bool  // PresentOffset names a bool presence companion
	ArrayBound    int64 // inline slots, or a string's declared maximum; 0 for a plain scalar
	ElemSize      int64 // ONE slot's size; the field's own when it holds one value
	CountOffset   int64 // the used-length companion, or -1
	PresentOffset int64 // the presence companion, or -1
}

// BlockFieldOf derives one field's row-walk facts from the ONE layout model
// (§19.3). `projection` selects the block form's projection spelling, in which
// an out-of-line array is its triple: the count companion is then the triple's
// own `count` member, which is the same column doing the same job.
func BlockFieldOf(u *Unit, f *Field, fieldOffset int64, projection bool) BlockFieldFacts {
	facts := BlockFieldFacts{CountOffset: -1, PresentOffset: -1, Optional: f.Type.Optional}
	pieces := BlockFieldPieceOffsets(u, f, fieldOffset, projection)
	if len(pieces) == 0 {
		return facts
	}
	if facts.Optional {
		facts.PresentOffset = pieces[len(pieces)-1].Offset
		pieces = pieces[:len(pieces)-1]
	}
	if len(pieces) == 0 {
		return facts
	}
	if projection && BlockOutOfLine(f) {
		// the triple: (offset_of u64, count u32, stride u32) at 0/8/12 (§2.7)
		facts.IsArray = true
		facts.Counted = true
		facts.ArrayBound = f.ArrayBound
		facts.CountOffset = pieces[0].Offset + 8
		facts.ElemSize = 0 // the PITCH is the instance's own, and the Stride column carries this build's
		return facts
	}
	switch {
	case f.Type.Pointer:
		facts.ElemSize = pieces[0].Size
	case f.Type.Kind == TString:
		// char[N+1] buffer, then the int32 used length. A string is not an
		// ARRAY — §8.1's descriptors say the same — but it is COUNTED, and
		// ArrayBound is its declared maximum.
		facts.Counted = true
		facts.ArrayBound = f.Type.Size
		facts.ElemSize = 1
		facts.CountOffset = pieces[1].Offset
	case f.Type.Kind == TBytes:
		facts.IsArray = true
		facts.Counted = true
		facts.ArrayBound = f.Type.Size
		facts.ElemSize = 1
		facts.CountOffset = pieces[1].Offset
	case f.KeyEnum != "" || f.Array == ArrayFixed:
		facts.IsArray = true
		facts.ArrayBound = f.ArrayBound
		facts.ElemSize = elementPiece(u, f).size
	case f.Array == ArrayCounted:
		facts.IsArray = true
		facts.Counted = true
		facts.ArrayBound = f.ArrayBound
		facts.ElemSize = elementPiece(u, f).size
		facts.CountOffset = pieces[1].Offset
	default:
		facts.ElemSize = pieces[0].Size
	}
	return facts
}
