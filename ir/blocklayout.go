// The BLOCK FORM's layout model (SPEC-TABLES.md §2.7, §19).
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

import (
	"fmt"
	"sort"
	"strings"
)

// BlockPrologueBytes is the projection's generated prologue: two uint64s,
// `magic` and `layout_id` (SPEC-TABLES.md §19.1). It is generated exactly as
// an optional's presence companion is, and a field may not be named after
// either half (§11).
const BlockPrologueBytes = 16

// BlockAlign is the alignment every block base and every out-of-line array
// start takes: a cache line, so two workers filling different arrays never
// share one (SPEC-TABLES.md §19.1).
const BlockAlign = 64

// BlockMagic identifies a schema block and carries the byte-order check with
// it (SPEC-TABLES.md §19.1). It is stored in the producer's NATIVE order; a
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
	Stride   int64 // the pitch: the element's sizeof (SPEC-TABLES.md §2.7)
	Max      int64 // the declared [..N] maximum — sizes the storage, NOT a digest fact
	// the triple's position in the projection: sixteen bytes with no interior
	// padding, at the field's own position (SPEC-TABLES.md §2.7)
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
	MaxBytes   int64  // <Table>BlockMaxBytes: the projection plus every array at its maximum
	LayoutId   uint64 // the digest (SPEC-TABLES.md §19.3)
	Digest     string // the digest's canonical text, for `schema` output and for review
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

// BlockUnit is a unit's whole block-form surface: the marked tables in
// declaration order, the layout of every record the form touches, and the
// closure a backend emits block machinery for. A unit with no `| block` marker
// produces a nil BlockUnit, which is what makes the zero-cost gate (§2.2)
// answerable by asking one question.
type BlockUnit struct {
	Tables  []*BlockLayout           // the marked tables, sorted by name
	Members map[string]*MemberLayout // every record in the block closure, by name
	Order   []string                 // the closure's member names, sorted
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

// BlockTables is the unit's `| block` tables, sorted by name.
func BlockTables(u *Unit) []*Struct {
	var out []*Struct
	for _, st := range u.Tables {
		if st.Block {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Blocks computes the whole block surface of a unit, or nil when the unit
// marks no table. The checker refuses every declaration this cannot lay out
// (§11), so a unit that reaches here lays out completely.
func Blocks(u *Unit) *BlockUnit {
	marked := BlockTables(u)
	if len(marked) == 0 {
		return nil
	}
	b := &BlockUnit{Members: map[string]*MemberLayout{}}
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
		bl.Digest = blockDigestText(bl, b)
		bl.LayoutId = fnv1a64(bl.Digest)
		b.Tables = append(b.Tables, bl)
	}
	for name := range b.Members {
		b.Order = append(b.Order, name)
	}
	sort.Strings(b.Order)
	return b
}

// BlockOutOfLine reports whether a field of a block-form table is one of the
// arrays that moves out of line: DEPTH ONE, BOUNDED ONLY (SPEC-TABLES.md
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
// (offset_of u64, count u32, stride u32) triple (SPEC-TABLES.md §2.7).
func layoutProjection(u *Unit, st *Struct) MemberLayout {
	ml := MemberLayout{Name: st.Name}
	// the prologue: two uint64s
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

// fieldPieces spells one field's storage as the contiguous pieces the
// generated record declares, in order. `projection` selects the block form's
// projection spelling, in which an out-of-line array is its triple.
func fieldPieces(u *Unit, f *Field, projection bool) []storagePiece {
	if projection && BlockOutOfLine(f) {
		// (offset_of u64, count u32, stride u32) — sixteen bytes, no interior
		// padding, at the field's own position
		return []storagePiece{{size: 8, align: 8}, {size: 4, align: 4}, {size: 4, align: 4}}
	}
	var pieces []storagePiece
	switch {
	case f.Type.Pointer:
		pieces = append(pieces, storagePiece{size: 4, align: 4}) // TableRef
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
	case TInt:
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
		}
	}
	// TString/TBytes are handled by the caller; TFixed and unions never reach
	// a block closure (refused by name, §11)
	return storagePiece{size: 1, align: 1}
}

// ---- the extent ----

// BlockArrayStart is where one out-of-line array begins, given the array
// starts before it: aligned to max( 64, alignof( element ) ) (§19.1).
func BlockArrayStart(offset, elemAlign int64) int64 {
	a := int64(BlockAlign)
	if elemAlign > a {
		a = elemAlign
	}
	return alignUp(offset, a)
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

// ---- the digest (SPEC-TABLES.md §19.3) ----

// blockDigestText is the canonical text the layout id digests: the
// projection's own fields with their offsets and sizes, each out-of-line
// array's element and pitch, and every row field's offset, size and kind.
//
// A declared MAXIMUM is deliberately EXCLUDED — it moves the offset_ofs
// written into an instance, and a consumer takes every offset_of FROM the
// instance, so raising one is absorbed on the default entry point (§19.4). A
// port that folded the maximum in would break that absorption with nothing to
// catch it.
//
// The text is a reviewable artifact for the same reason the wire-shape
// projection is: what an id depends on should be printable.
func blockDigestText(bl *BlockLayout, b *BlockUnit) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "block %s sizeof=%d alignof=%d\n", bl.Table.Name, bl.Projection.Size, bl.Projection.Align)
	for _, fl := range bl.Projection.Fields {
		f := fl.Field
		if BlockOutOfLine(f) {
			fmt.Fprintf(&sb, "  field %s offset=%d size=%d kind=%d elem=%s stride=%d\n",
				f.Name, fl.Offset, fl.Size, TableScalarKind(f), f.Type.Name, strideOf(bl, f.Name))
			continue
		}
		fmt.Fprintf(&sb, "  field %s offset=%d size=%d kind=%d\n", f.Name, fl.Offset, fl.Size, TableScalarKind(f))
	}
	// every row type, in the order its array is declared, and everything a row
	// nests by value beneath it — deduplicated, so one element named twice
	// digests once
	seen := map[string]bool{}
	var emit func(name string)
	emit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		ml := b.Members[name]
		if ml == nil {
			return
		}
		fmt.Fprintf(&sb, "row %s sizeof=%d alignof=%d\n", name, ml.Size, ml.Align)
		for _, fl := range ml.Fields {
			fmt.Fprintf(&sb, "  field %s offset=%d size=%d kind=%d\n", fl.Field.Name, fl.Offset, fl.Size, TableScalarKind(fl.Field))
		}
		for _, fl := range ml.Fields {
			if fl.Field.Type.Kind == TNamed {
				if ref, ok := fl.Field.Type.Ref.(*Struct); ok {
					emit(ref.Name)
				}
			}
		}
	}
	for _, a := range bl.Arrays {
		emit(a.ElemName)
	}
	return sb.String()
}

func strideOf(bl *BlockLayout, field string) int64 {
	for _, a := range bl.Arrays {
		if a.Field.Name == field {
			return a.Stride
		}
	}
	return 0
}

// fnv1a64 is the digest function: 64-bit FNV-1a over the canonical text. A
// digest, not a version counter (§19.3) — and a function both generated sides
// receive as a LITERAL, so neither computes it at runtime.
func fnv1a64(s string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 0x100000001b3
	}
	return h
}
