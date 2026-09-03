// The BLOCK FORM in Go (docs/SPEC-TABLES.md §19): the READ half, emitted ON
// THE SIDE into <Base>Block.go.
//
// NOTHING DECLARES IT. Every fixed table has a block form; a consumer compiles
// this file only if it uses one, and <Base>Table.go carries not one symbol of
// it. The C++ side is the producer (§19.1's builder) and this side is the
// consumer: it POINTS at bytes another language wrote and reads rows in place,
// with no marshalling and no copy at the boundary.
//
// Two ways to read one block, and both come from one declaration (§19.2): the
// DESCRIPTORS, which carry the projection's own layout and retire a hand-kept
// mirror, and the generated BLITTABLE STRUCT beside them, which is the typed
// fast path a per-frame job uses.
//
// THE LAYOUT MODEL IS NAMED (§19.3): every size and offset asserted here is
// Go's own — unsafe.Sizeof, unsafe.Offsetof and unsafe.Alignof, which are what
// a pointer conversion actually indexes with. A bool in a row is ONE byte, one
// in C++ and one here.
//
// The blittable records are a SECOND set of structs beside the table wire's
// storage, taking §11's claimed <Name>Row suffix, for the reason the C# port's
// do: the wire's storage is spelled the way the Go PACKET emitter spells it —
// `string(N)` as [N]byte, for one — and the C ABI record is `char[N+1]`. One
// declaration cannot be two types, so the accelerator's record takes the
// claimed suffix.
//
// ALLOCATION: none of it. The bytes belong to the consumer — a []byte it owns,
// an mmap, or memory a producer handed across — and this side takes a pointer
// and a length and points.
package gotable

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateBlockFiles emits <Base>Block.go for every file of a unit that
// declares a table. A file whose tables all lack a block form still gets one,
// saying which table and why.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if blocks == nil {
		return out, nil
	}
	// THE BLOCK HOME is the first file, by declaration order, that declares a
	// table WITH a block form — never the protocol id's home, which may
	// declare no table at all. The runtime and every blittable record land
	// there, once per unit: a Go package is one namespace across its files, so
	// "emitted once, anywhere" is the whole requirement. C++ takes the other
	// road — its primitives ride in EVERY <Base>Block.h behind a `#ifndef` —
	// because a C++ consumer may include one header alone.
	home := blockHome(u, blocks)
	if home == "" {
		return out, nil
	}
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &blockGen{unit: u, file: f, blocks: blocks, home: f.Base == home}
		g.emit()
		src, err := g.assemble()
		if err != nil {
			return nil, err
		}
		out[f.Base+"Block.go"] = src
	}
	return out, nil
}

// blockHome is the file the unit's shared block runtime and every blittable
// record are emitted into: the first, by declaration order, that declares a
// table with a block form. Empty when the unit has no block form at all, in
// which case no file needs the runtime.
func blockHome(u *ir.Unit, blocks *ir.BlockUnit) string {
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if blocks.Block(st.Name) != nil {
				return f.Base
			}
		}
	}
	return ""
}

type blockGen struct {
	unit        *ir.Unit
	file        *ir.File
	blocks      *ir.BlockUnit
	home        bool
	runtime     strings.Builder // the shared runtime, home file only
	structs     strings.Builder // the blittable records
	handles     strings.Builder // the block handles, their Open and their descriptors
	needsFmt    bool
	needsUnsafe bool
}

func (g *blockGen) rf(format string, args ...any) { fmt.Fprintf(&g.runtime, format, args...) }
func (g *blockGen) sf(format string, args ...any) { fmt.Fprintf(&g.structs, format, args...) }
func (g *blockGen) hf(format string, args ...any) { fmt.Fprintf(&g.handles, format, args...) }

func (g *blockGen) emit() {
	if g.home {
		g.needsUnsafe = true
		g.rf("%s", blockRuntime(ir.BuildVersion(g.unit)))
		// EVERY blittable record of the unit, here and nowhere else. Not the
		// file that DECLARES the type: a record a block form reaches is often
		// declared in a file of `type`s alone, which gets no Block.go of its
		// own, and a consumer would then reference a struct nothing emitted.
		// One definition per unit, in a file that exists.
		for _, name := range g.blocks.Order {
			g.emitBlittable(name)
		}
	}
	// every marked table's PROJECTION is a record too, and it belongs to the
	// file that declares the table
	for _, st := range g.file.Tables {
		if bl := g.blocks.Block(st.Name); bl != nil {
			g.emitProjection(bl)
		}
	}
	if g.home {
		g.emitLayoutCheck()
	}
	for _, st := range g.file.Tables {
		if bl := g.blocks.Block(st.Name); bl != nil {
			g.emitBlockHandle(bl)
			continue
		}
		g.hf("// table %s has NO block form: %s (docs/SPEC-TABLES.md §19).\n", st.Name, g.blocks.SkippedReason(st.Name))
		g.hf("// Its wire (§3) is unaffected — only this projection is absent, and it is\n")
		g.hf("// absent by construction rather than by refusal.\n\n")
	}
}

func (g *blockGen) assemble() ([]byte, error) {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the BLOCK FORM (docs/SPEC-TABLES.md §19): the READ half.\n", g.unit.Package)
	h.WriteString("//\n")
	h.WriteString("// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on\n")
	h.WriteString("// the side: compile this file only if you read a block. The unit's\n")
	h.WriteString("// <Base>Table.go carries not one symbol of it.\n")
	h.WriteString("//\n")
	h.WriteString("// It is UNSAFE by nature, not by taste: a block is memory another language\n")
	h.WriteString("// wrote, and pointing at it without a copy is the whole point (§12.1).\n")
	h.WriteString("//\n")
	h.WriteString("// Every size and offset below is Go's own layout model — what unsafe.Sizeof,\n")
	h.WriteString("// unsafe.Offsetof and a pointer conversion index with (§19.3). A bool in a row\n")
	h.WriteString("// is ONE byte. The generated init REFUSES rather than guesses: a runtime whose\n")
	h.WriteString("// model disagrees with the one the compiler derived stops the program where it\n")
	h.WriteString("// starts, naming the record, the field and both numbers.\n\n")
	fmt.Fprintf(&h, "package %s\n\n", g.unit.Package)
	var imports []string
	if g.needsFmt {
		imports = append(imports, `"fmt"`)
	}
	if g.needsUnsafe {
		imports = append(imports, `"unsafe"`)
	}
	h.WriteString(goImports(imports))
	h.WriteString(g.runtime.String())
	if g.structs.Len() > 0 {
		h.WriteString("// The BLITTABLE records: one per record the block form touches, laid out to\n")
		h.WriteString("// the C ABI with GENERATED PADDING FIELDS wherever the layout has interior\n")
		h.WriteString("// padding (docs/SPEC-TABLES.md §19.3). Explicit padding is chosen over relying\n")
		h.WriteString("// on Go's own inter-field padding matching C's, because that is an\n")
		h.WriteString("// assumption, and an assumption is what this form exists to delete: the\n")
		h.WriteString("// padding puts every field where the model says, and the init below proves\n")
		h.WriteString("// this runtime agrees.\n")
		h.WriteString("//\n")
		h.WriteString("// They take a CLAIMED SUFFIX — <Name>Row for a row and <Table>BlockProjection\n")
		h.WriteString("// for a projection (§11) — because the package already holds a struct of each\n")
		h.WriteString("// declaration's name, which is the table wire's storage, and one declaration\n")
		h.WriteString("// cannot be two types.\n")
		h.WriteString(g.structs.String())
		h.WriteString("\n")
	}
	h.WriteString(g.handles.String())
	src, err := format.Source([]byte(h.String()))
	if err != nil {
		return nil, fmt.Errorf("generated Go for %sBlock.go does not parse — a compiler bug, not a schema error: %w", g.file.Base, err)
	}
	return src, nil
}

// ---- the shared runtime ----

func blockRuntime(buildVersion uint64) string {
	return fmt.Sprintf(`// BuildVersion is the unit's build version (docs/SPEC-TABLES.md §20): the one
// digest a block carries and BlockOpen compares. It moves when anything a
// reader points at moves.
const BuildVersion uint64 = 0x%016x

// TableBlockMagic identifies a schema block and carries the byte-order check
// with it (docs/SPEC-TABLES.md §19.1). It is stored in the producer's NATIVE
// order; a consumer that reads back the byte-swapped value has found a foreign
// byte order, and one that reads back anything else has not found a block at
// all.
const TableBlockMagic uint64 = 0x%016x

// TableBlockByteOrder is THIS BUILD's byte order, as the prologue carries it
// (docs/SPEC-TABLES.md §20.3): 1 little, 2 big. A block written by a build of
// the other order is REFUSED by BlockOpen — a fix-up path is a named
// obligation, not something a consumer improvises row by row.
//
// Go has no portable compile-time byte-order constant, so it is read off the
// machine once, at package initialisation, and never on a per-block path.
var TableBlockByteOrder = tableBlockNativeOrder()

func tableBlockNativeOrder() uint64 {
	probe := uint16(1)
	if *(*byte)(unsafe.Pointer(&probe)) == 1 {
		return 1 // little
	}
	return 2 // big
}

// TableBlockTriple is what a table knows about ONE of its out-of-line arrays:
// where the rows start, how many there are, and how far apart they sit.
// Sixteen bytes with no interior padding, sitting at the array field's own
// position in the projection (§2.7). A consumer reads all three FROM THE
// INSTANCE, never from its own constants — that is the difference between a
// generated pair of structs and an ABI (§19.2).
type TableBlockTriple struct {
	OffsetOf uint64 // block-relative: the block relocates by plain copy
	Count    uint32 // rows the producer filled; rows past it are not part of the block
	Stride   uint32 // the pitch the consumer indexes with, from the data
}

// TableBlockRows is one array's rows, ITERATED at the pitch the INSTANCE gives
// (§19.2). A call site never spells the pitch arithmetic itself, for the same
// reason a keyed array's call sites should not re-derive their own slot rule.
type TableBlockRows[T any] struct {
	Base   unsafe.Pointer
	Count  int32
	Stride int32
}

// Len is the row count the instance carries.
func (r TableBlockRows[T]) Len() int32 { return r.Count }

// At points at one row where it lies, at the pitch the instance gives. It
// copies nothing.
func (r TableBlockRows[T]) At(i int32) *T {
	return (*T)(unsafe.Add(r.Base, uintptr(i)*uintptr(r.Stride)))
}

// ---- reflection over a block (docs/SPEC-TABLES.md §8, §19.2) ----
//
// The descriptors are the mechanism, and they are what retires a hand-kept
// mirror: a consumer holding them reads the triples out of an instance and
// points at rows, with no hand-written struct per table and no knowledge of
// the spelling that produced any of it. They are constant data, so this costs
// a lookup, not a parse — and they are immutable, so any goroutine may read
// them.

// TableBlockFieldInfo is one field's position in the record this descriptor
// describes.
type TableBlockFieldInfo struct {
	Name           string
	Offset         uint32 // the field's offset in the record this descriptor describes
	Size           uint32 // its size there
	Kind           uint8  // the table-wire kind, as TableFieldInfo carries it
	OutOfLine      bool   // an out-of-line array: the three members below are live
	OffsetOfOffset uint32 // the triple's OffsetOf member, or 0xffffffff
	CountOffset    uint32 // its Count member, or 0xffffffff
	StrideOffset   uint32 // its Stride member, or 0xffffffff
	Stride         uint32 // THIS BUILD's pitch, to assert against — never to index with (§19.2)
	// Element is the ELEMENT's or the nested record's own layout, behind a
	// function so a descriptor graph needs no initialisation order. nil when
	// the field is a scalar. Following it is how a walker DESCENDS: an
	// out-of-line array's rows, and a nested record's fields, are both reached
	// through this one column.
	Element func() *TableBlockInfo
}

// TableBlockInfo is one record's layout as DATA — the whole mechanism behind
// the block form's read side. A block-form table's own descriptor describes
// its PROJECTION; the element descriptor of each out-of-line array describes
// that array's ROW, and so on down.
type TableBlockInfo struct {
	Name         string
	BuildVersion uint64 // the unit's (docs/SPEC-TABLES.md §20)
	Size         uint32 // the record's own size: a projection's, or a row's
	Align        uint32
	NumFields    int32
	Fields       []TableBlockFieldInfo
}

`, buildVersion, ir.BlockMagic)
}

// ---- the blittable records ----

func (g *blockGen) emitBlittable(name string) {
	ml := g.blocks.Layout(name)
	if ml == nil {
		return
	}
	g.sf("\n// %sRow — a block row, or a record one nests by value. `Row` is a CLAIMED\n", name)
	g.sf("// suffix (docs/SPEC-TABLES.md §11), so no declaration in the unit can take it.\n")
	g.sf("type %sRow struct {\n", name)
	g.emitBlittableFields(ml, 0, false)
	g.sf("}\n")
}

func (g *blockGen) emitProjection(bl *ir.BlockLayout) {
	g.needsUnsafe = true
	name := bl.Table.Name
	g.sf("\n// %sBlockProjection — the block PROJECTION: the table's own instance as it\n", name)
	g.sf("// sits at the front of a block, opening with the generated PROLOGUE and\n")
	g.sf("// carrying, per out-of-line array, the triple that says where its rows are.\n")
	g.sf("// It is a record like any other and follows the same C ABI rule (§19.3).\n")
	g.sf("//\n")
	g.sf("// It is a SEPARATE record from %sRow: a table can be both a block root and\n", name)
	g.sf("// another block's row, and the two differ by the prologue.\n")
	g.sf("type %sBlockProjection struct {\n", name)
	g.sf("\tMagic        uint64 // generated: identifies a schema block\n")
	g.sf("\tBuildVersion uint64 // generated: the unit's build version (docs/SPEC-TABLES.md §20)\n")
	g.sf("\tByteOrder    uint64 // generated: 1 little, 2 big\n")
	g.emitBlittableFields(&bl.Projection, ir.BlockPrologueBytes, true)
	g.sf("}\n")
}

// emitBlittableFields walks one record's computed layout and emits its fields
// with generated padding between them.
//
// `projection` is the whole of what decides whether a bounded array becomes a
// TRIPLE or stays INLINE, and it is load-bearing (docs/SPEC-TABLES.md §2.7):
// DEPTH ONE, BOUNDED ONLY — only the block-form TABLE'S OWN bounded arrays of
// structs are laid out of line, and every array at any depth inside a row or
// inside a record a row nests is inline storage exactly where it always was.
func (g *blockGen) emitBlittableFields(ml *ir.MemberLayout, at int64, projection bool) {
	w := &blockWriter{g: g, at: at}
	for _, fl := range ml.Fields {
		// Pad to each PIECE of the field, not only to the field: a field's own
		// storage can carry interior padding — a `string(N)` buffer followed
		// by its int32 length is the ordinary case — and padding only between
		// fields slides every field after it.
		w.pieces = ir.BlockFieldPieceOffsets(g.unit, fl.Field, fl.Offset, projection)
		w.piece = 0
		g.emitBlittableField(fl.Field, projection, w)
	}
	w.pad(ml.Size) // the trailing padding is part of the record, and stating it costs nothing
}

// blockWriter lays a record out piece by piece, padding to the offset the
// compiler's model gives each one and advancing past the bytes it takes. It
// exists because a field is not always one piece (§19.3).
type blockWriter struct {
	g      *blockGen
	at     int64
	pieces []ir.BlockFieldPiece
	piece  int
}

// pad fills to an offset with a BLANK field, which Go gives no name and no
// accessor — the bytes are the record's, not a member's.
func (w *blockWriter) pad(to int64) {
	if w.at >= to {
		return
	}
	w.g.sf("\t_ [%d]byte // generated padding\n", to-w.at)
	w.at = to
}

// next pads to the next piece's offset and accounts for the bytes it takes;
// the caller emits the piece's own spelling between the two.
func (w *blockWriter) next() {
	if w.piece >= len(w.pieces) {
		return
	}
	p := w.pieces[w.piece]
	w.pad(p.Offset)
	w.at = p.Offset + p.Size
	w.piece++
}

func (g *blockGen) emitBlittableField(f *ir.Field, projection bool, w *blockWriter) {
	name := ir.GoExportName(f.Name)
	next := w.next
	if projection && ir.BlockOutOfLine(f) {
		next()
		g.sf("\t%s TableBlockTriple // [..%d]%s, laid out of line\n", name, f.ArrayBound, f.Type.Name)
		return
	}
	switch {
	case f.Type.Kind == ir.TString:
		next()
		g.sf("\t%s [%d]byte // string(%d): max length, used length beside it\n", name, f.Type.Size+1, f.Type.Size)
		next()
		g.sf("\t%sLength int32\n", name)
	case f.Type.Kind == ir.TBytes:
		next()
		g.sf("\t%s [%d]byte // bytes(%d): fixed buffer, used length beside it\n", name, f.Type.Size, f.Type.Size)
		next()
		g.sf("\t%sLength int32\n", name)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
		next()
		g.sf("\t%s [%d]%s\n", name, f.ArrayBound, goBlittableType(g.unit, f.Type))
		if f.Array == ir.ArrayCounted {
			next()
			g.sf("\t%sCount int32\n", name)
		}
	default:
		next()
		g.sf("\t%s %s\n", name, goBlittableType(g.unit, f.Type))
	}
	if f.Type.Optional {
		next()
		g.sf("\t%sPresent bool // ?%s: one byte, in Go's model as in C++\n", name, f.Type.Name)
	}
}

// goBlittableType maps a field's declared type to its Go blittable spelling,
// for both accelerators: a cooked record and a block row are one set of structs
// from one layout model, so they are spelled by one function (§7).
//
// A bool is ONE byte, which is what this form asserts (§19.3). An enum takes
// its own generated named type, whose width the layout check proves against
// the model's; a flags field is a uint64 in every target.
func goBlittableType(u *ir.Unit, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TInt:
		if t.Signed {
			return fmt.Sprintf("int%d", t.Width)
		}
		return fmt.Sprintf("uint%d", t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32"
		}
		return "uint64"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			return t.Name
		case *ir.Flags:
			return "uint64"
		case *ir.Struct:
			return t.Name + "Row"
		}
	}
	return "byte"
}

// ---- the layout check (docs/SPEC-TABLES.md §19.3) ----

// emitLayoutCheck emits the generated check, run ONCE at package
// initialisation: every size, alignment and offset the C++ side static_asserts,
// asserted here against Go's own model. Go could state most of it at compile
// time, but not with a message a person can act on, so it states it where the
// program starts and REFUSES — naming the record, the field, the offset the
// compiler derived and the one THIS runtime produced.
//
// Neither side's layout is inferred from the other's: both are checked against
// their own runtime's model, which is the only way a two-language contract can
// be held by a compiler that generates both halves.
func (g *blockGen) emitLayoutCheck() {
	g.needsFmt = true
	g.hf("// The LAYOUT CONTRACT's Go half (docs/SPEC-TABLES.md §19.3), run ONCE at\n")
	g.hf("// package initialisation. It REFUSES rather than guesses: a runtime whose\n")
	g.hf("// model disagrees with the one the compiler derived stops the program where\n")
	g.hf("// it starts, rather than pointing at a row and reading the wrong bytes.\n")
	g.hf("func init() {\n")
	for _, name := range g.blocks.Order {
		g.emitRecordCheck(name, g.blocks.Layout(name), false, nil)
	}
	for _, bl := range g.blocks.Tables {
		g.emitRecordCheck(bl.Table.Name, &bl.Projection, true, bl)
	}
	g.hf("}\n\n")
	g.hf("// tableBlockLayoutSize refuses a record this runtime lays out at a different\n")
	g.hf("// size, or with a different alignment, from the one the compiler derived.\n")
	g.hf("func tableBlockLayoutSize(what string, got, want uintptr) {\n")
	g.hf("\tif got != want {\n")
	g.hf("\t\tpanic(fmt.Sprintf(\"schema block layout: %%s is %%d bytes in this runtime and %%d in the schema \"+\n")
	g.hf("\t\t\t\"the C++ side asserts — the two sides disagree about the bytes (docs/SPEC-TABLES.md §19.3)\", what, got, want))\n")
	g.hf("\t}\n}\n\n")
	g.hf("// tableBlockLayoutOffset refuses a field this runtime puts somewhere else.\n")
	g.hf("func tableBlockLayoutOffset(what string, got, want uintptr) {\n")
	g.hf("\tif got != want {\n")
	g.hf("\t\tpanic(fmt.Sprintf(\"schema block layout: %%s sits at %%d in this runtime and %%d in the schema \"+\n")
	g.hf("\t\t\t\"the C++ side asserts — the two sides disagree about the bytes (docs/SPEC-TABLES.md §19.3)\", what, got, want))\n")
	g.hf("\t}\n}\n\n")
}

func (g *blockGen) emitRecordCheck(name string, ml *ir.MemberLayout, projection bool, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	spelled := name + "Row"
	if projection {
		spelled = name + "BlockProjection"
	}
	g.hf("\ttableBlockLayoutSize(%q, unsafe.Sizeof(%s{}), %d)\n", spelled, spelled, ml.Size)
	g.hf("\ttableBlockLayoutSize(%q, unsafe.Alignof(%s{}), %d)\n", spelled+" alignment", spelled, ml.Align)
	if projection {
		g.hf("\ttableBlockLayoutOffset(%q, unsafe.Offsetof(%s{}.Magic), 0)\n", spelled+".Magic", spelled)
		g.hf("\ttableBlockLayoutOffset(%q, unsafe.Offsetof(%s{}.BuildVersion), 8)\n", spelled+".BuildVersion", spelled)
		g.hf("\ttableBlockLayoutOffset(%q, unsafe.Offsetof(%s{}.ByteOrder), 16)\n", spelled+".ByteOrder", spelled)
	}
	for _, fl := range ml.Fields {
		g.hf("\ttableBlockLayoutOffset(%q, unsafe.Offsetof(%s{}.%s), %d)\n",
			spelled+"."+ir.GoExportName(fl.Field.Name), spelled, ir.GoExportName(fl.Field.Name), fl.Offset)
	}
	if bl != nil {
		// and each array's PITCH CONSTANT, against this runtime's own size.
		// Without this the constant is emitted and never read, so perturbing
		// it on one side only — §19.5's named negative control — could not
		// turn anything red here.
		for _, a := range bl.Arrays {
			g.hf("\ttableBlockLayoutSize(%q, unsafe.Sizeof(%sRow{}), %d)\n",
				bl.Table.Name+"."+ir.GoExportName(a.Field.Name)+" stride", a.ElemName, a.Stride)
		}
	}
}

// ---- the block handle ----

func (g *blockGen) emitBlockHandle(bl *ir.BlockLayout) {
	g.needsUnsafe = true
	name := bl.Table.Name
	g.hf("// %sBlockMaxBytes is the storage a PRODUCER of this block allocates, sized\n", name)
	g.hf("// from the declared maxima (docs/SPEC-TABLES.md §19.1). A Go consumer does\n")
	g.hf("// not allocate a block — the bytes are handed to it — but it caps by this: a\n")
	g.hf("// playback buffer, a recording, a scratch copy all size from the generated\n")
	g.hf("// constant rather than from a number a person wrote down beside it.\n")
	g.hf("const %sBlockMaxBytes int64 = %d\n\n", name, bl.MaxBytes)

	g.hf("// %sBlock is %s's block: a pointer and a length, and then rows in place.\n", name, name)
	g.hf("// Opening one is ONE check and no copy; reading a row is one add (§19.2).\n")
	g.hf("//\n")
	g.hf("// The bytes belong to the CONSUMER — a []byte it owns, an mmap, or memory a\n")
	g.hf("// producer handed across. Nothing here allocates.\n")
	g.hf("//\n")
	g.hf("// A block is valid until the next fill over the SAME storage, which\n")
	g.hf("// invalidates every block over it and every row pointer taken from one.\n")
	g.hf("type %sBlock struct {\n", name)
	g.hf("\tBase       unsafe.Pointer // the extent's base, 64-byte aligned\n")
	g.hf("\tProjection *%sBlockProjection // the projection, at offset 0\n", name)
	g.hf("\tBytes      int64 // the extent in use\n")
	g.hf("}\n\n")

	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("// %sStride, %sMax and %sProjectionOffset are the constants this build\n", field, field, field)
		g.hf("// asserts against. A consumer INDEXES with what it read from the instance,\n")
		g.hf("// never with these (docs/SPEC-TABLES.md §19.2). They are METHODS rather than\n")
		g.hf("// package constants because §11 gives a language whose accessors are members\n")
		g.hf("// exactly that latitude: a member claims nothing at file scope.\n")
		g.hf("func (b *%sBlock) %sStride() int64 { return %d }\n", name, field, a.Stride)
		g.hf("func (b *%sBlock) %sMax() int64 { return %d }\n", name, field, a.Max)
		g.hf("func (b *%sBlock) %sProjectionOffset() int64 { return %d }\n\n", name, field, a.TripleOffset)

		g.hf("// %s is ITERATED, not indexed by hand: the accessor points at each row where\n", field)
		g.hf("// it lies, at the pitch the INSTANCE gives, for the count the instance gives.\n")
		g.hf("func (b *%sBlock) %s() TableBlockRows[%sRow] {\n", name, field, a.ElemName)
		g.hf("\treturn TableBlockRows[%sRow]{\n", a.ElemName)
		g.hf("\t\tBase:   unsafe.Add(b.Base, uintptr(b.Projection.%s.OffsetOf)),\n", field)
		g.hf("\t\tCount:  int32(b.Projection.%s.Count),\n", field)
		g.hf("\t\tStride: int32(b.Projection.%s.Stride),\n", field)
		g.hf("\t}\n}\n\n")

		g.hf("// %sSpan is the CONTIGUOUS view, available because the pitch IS the\n", field)
		g.hf("// element's size (§2.7), which is how the per-frame fast path is written.\n")
		g.hf("func (b *%sBlock) %sSpan() []%sRow {\n", name, field, a.ElemName)
		g.hf("\treturn unsafe.Slice((*%sRow)(unsafe.Add(b.Base, uintptr(b.Projection.%s.OffsetOf))), b.Projection.%s.Count)\n",
			a.ElemName, field, field)
		g.hf("}\n\n")
	}

	g.emitBlockOpen(bl)
	g.emitBlockDescriptors(bl)
}

func (g *blockGen) emitBlockOpen(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.hf("// %sBlockOpen checks once and points, and this is the WHOLE check\n", name)
	g.hf("// (docs/SPEC-TABLES.md §19.2): the magic read in the machine's own order, the\n")
	g.hf("// BYTE ORDER the prologue carries against this build's own, the BUILD VERSION\n")
	g.hf("// against this build's own, each array's pitch, its offset, its COUNT against\n")
	g.hf("// the declared maximum and its extent inside the block, the used extent\n")
	g.hf("// against the bytes the caller passed, and the base's alignment. On a match\n")
	g.hf("// the bytes are what a build with this layout wrote, so there is nothing to\n")
	g.hf("// validate and nothing to fix up. On any failure it returns false and points\n")
	g.hf("// at nothing.\n")
	g.hf("//\n")
	g.hf("// There is ONE entry point and no tolerant twin: the block form is same-build\n")
	g.hf("// by construction — both sides generated from one declaration at one build —\n")
	g.hf("// so a consumer older than its producer is not a case. A mismatch is a\n")
	g.hf("// refusal; regenerate both sides. Data that must outlive the build that wrote\n")
	g.hf("// it takes the wire (§3), which this same table still has.\n")
	g.hf("func %sBlockOpen(block *%sBlock, base unsafe.Pointer, bytes int64) bool {\n", name, name)
	g.hf("\t*block = %sBlock{}\n", name)
	g.hf("\tif base == nil || bytes < %d {\n\t\treturn false\n\t}\n", bl.Projection.Size)
	g.hf("\tif uintptr(base)%%%d != 0 {\n\t\treturn false // the base's alignment\n\t}\n", ir.BlockAlign)
	g.hf("\t// the prologue read in the machine's own order: a byte-swapped magic is a\n")
	g.hf("\t// FOREIGN BYTE ORDER, and anything else is not a block at all. Both refuse.\n")
	g.hf("\tif *(*uint64)(base) != TableBlockMagic {\n\t\treturn false\n\t}\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 8)) != BuildVersion {\n\t\treturn false\n\t}\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 16)) != TableBlockByteOrder {\n")
	g.hf("\t\treturn false // a block of the other byte order: the fix-up path is a named obligation\n\t}\n")
	g.hf("\tprojection := (*%sBlockProjection)(base)\n", name)
	g.hf("\tused := int64(%d)\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("\t{\n")
		g.hf("\t\t// EVERY NUMBER BELOW COMES FROM THE INSTANCE, so the arithmetic is\n")
		g.hf("\t\t// unsigned and each term is BOUNDED BEFORE IT IS ADDED. A forged\n")
		g.hf("\t\t// OffsetOf near 2^63 must refuse, and an addition that wrapped past the\n")
		g.hf("\t\t// top of the type would be what the check after it was supposed to\n")
		g.hf("\t\t// catch. The C++ side holds the same shape for the same reason.\n")
		g.hf("\t\toffsetOf := projection.%s.OffsetOf\n", field)
		g.hf("\t\tcount := uint64(projection.%s.Count)\n", field)
		g.hf("\t\tstride := uint64(projection.%s.Stride)\n", field)
		g.hf("\t\tif stride != uint64(unsafe.Sizeof(%sRow{})) {\n\t\t\treturn false\n\t\t}\n", a.ElemName)
		g.hf("\t\t// past the DECLARED MAXIMUM: the producer side refuses this too, because\n")
		g.hf("\t\t// a consumer that sizes anything by the maximum would overflow on a\n")
		g.hf("\t\t// count the maximum does not bound\n")
		g.hf("\t\tif count > %d {\n\t\t\treturn false\n\t\t}\n", a.Max)
		g.hf("\t\tif offsetOf < %d || offsetOf%%%d != 0 {\n\t\t\treturn false\n\t\t}\n", bl.Projection.Size, blockStartAlign(a))
		g.hf("\t\tif offsetOf > uint64(bytes) {\n\t\t\treturn false\n\t\t}\n")
		g.hf("\t\trows := count * stride // both bounded above: this cannot carry\n")
		g.hf("\t\tif rows > uint64(bytes)-offsetOf {\n\t\t\treturn false\n\t\t}\n")
		g.hf("\t\tif end := int64(offsetOf + rows); end > used {\n\t\t\tused = end\n\t\t}\n")
		g.hf("\t}\n")
	}
	g.hf("\t// the used extent, rounded to %d WITHOUT the rounding itself wrapping: used\n", ir.BlockAlign)
	g.hf("\t// is already inside bytes, and the padding is paid out of the slack that is\n")
	g.hf("\t// left rather than added and compared after.\n")
	g.hf("\tpadding := (%d - (used %% %d)) %% %d\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.hf("\tif padding > bytes-used {\n\t\treturn false\n\t}\n")
	g.hf("\tused += padding\n")
	g.hf("\tblock.Base = base\n")
	g.hf("\tblock.Projection = projection\n")
	g.hf("\tblock.Bytes = used\n")
	g.hf("\treturn true\n}\n\n")
}

// blockStartAlign is where one out-of-line array begins: aligned to
// max( 64, alignof( element ) ) (docs/SPEC-TABLES.md §19.1).
func blockStartAlign(a ir.BlockArray) int64 {
	if a.ElemAlign() > int64(ir.BlockAlign) {
		return a.ElemAlign()
	}
	return int64(ir.BlockAlign)
}

// emitBlockDescriptors emits one table's block reflection: the projection
// offset of every field, the offsets of the three members inside each triple,
// and the ELEMENT's own descriptor beside them (docs/SPEC-TABLES.md §8,
// §19.2). A consumer holding these reads the facts out of an instance and
// points at rows, with no hand-written struct per table.
func (g *blockGen) emitBlockDescriptors(bl *ir.BlockLayout) {
	name := bl.Table.Name
	records := blockDescriptorRecords(g.blocks, bl)
	for _, r := range records {
		g.emitBlockRecordDescriptor(name, r, g.blocks.Layout(r), nil)
	}
	g.emitBlockRecordDescriptor(name, "", &bl.Projection, bl)
	g.hf("// Type is this block's descriptors: constant data, so a reflective read costs\n")
	g.hf("// a lookup and not a parse. The row layouts hang off the element column\n")
	g.hf("// rather than taking names of their own, so a walker reaches every record\n")
	g.hf("// through the graph.\n")
	g.hf("func (b *%sBlock) Type() *TableBlockInfo { return &%s }\n\n", name, blockInfoSymbol(name, ""))
}

func (g *blockGen) emitBlockRecordDescriptor(owner, record string, ml *ir.MemberLayout, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	symbol := blockInfoSymbol(owner, record)
	name := record
	projection := bl != nil
	if projection {
		name = bl.Table.Name
	}
	g.hf("var %s = TableBlockInfo{\n", symbol)
	g.hf("\tName: %q, BuildVersion: BuildVersion, Size: %d, Align: %d, NumFields: %d,\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.hf("\tFields: []TableBlockFieldInfo{\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		outOfLine := projection && ir.BlockOutOfLine(f)
		offsetOfOffset, countOffset, strideOffset, stride := "0xffffffff", "0xffffffff", "0xffffffff", "0"
		element := "nil"
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			offsetOfOffset = fmt.Sprintf("%d", a.OffsetOfOffset)
			countOffset = fmt.Sprintf("%d", a.CountOffset)
			strideOffset = fmt.Sprintf("%d", a.StrideOffset)
			stride = fmt.Sprintf("%d", a.Stride)
			element = fmt.Sprintf("func() *TableBlockInfo { return &%s }", blockInfoSymbol(owner, a.ElemName))
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed && g.blocks.Layout(ref.Name) != nil {
			element = fmt.Sprintf("func() *TableBlockInfo { return &%s }", blockInfoSymbol(owner, ref.Name))
		}
		g.hf("\t\t{Name: %q, Offset: %d, Size: %d, Kind: %d, OutOfLine: %v, OffsetOfOffset: %s, CountOffset: %s, StrideOffset: %s, Stride: %s, Element: %s},\n",
			f.Name, fl.Offset, fl.Size, kind, outOfLine, offsetOfOffset, countOffset, strideOffset, stride, element)
	}
	g.hf("\t},\n}\n\n")
}

// blockDescriptorRecords is every record one block's descriptors reach, in a
// stable order: each out-of-line array's element and everything it nests by
// value, sorted so the generated text is deterministic.
func blockDescriptorRecords(b *ir.BlockUnit, bl *ir.BlockLayout) []string {
	seen := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		ml := b.Layout(name)
		if ml == nil {
			return
		}
		seen[name] = true
		for _, fl := range ml.Fields {
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok && fl.Field.Type.Kind == ir.TNamed {
				walk(ref.Name)
			}
		}
	}
	for _, a := range bl.Arrays {
		walk(a.ElemName)
	}
	for _, fl := range bl.Projection.Fields {
		if ir.BlockOutOfLine(fl.Field) {
			continue
		}
		if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok && fl.Field.Type.Kind == ir.TNamed {
			walk(ref.Name)
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// blockInfoSymbol names one record's descriptor. The descriptors are package
// data, so they take an unexported name — a schema declaration always
// generates an exported spelling, so nothing a user writes can collide with
// one, and the block form claims no file-scope name beyond §11's set.
func blockInfoSymbol(owner, record string) string {
	if record == "" {
		return "blockInfo" + owner
	}
	return "blockInfo" + owner + record
}
