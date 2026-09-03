// The BLOCK FORM in C# (SPEC-TABLES.md §19): the READ half, emitted ON THE
// SIDE into <Base>Block.cs.
//
// NOTHING DECLARES IT. Every fixed table has a block form; a consumer compiles
// this file only if it uses one, and <Base>Table.cs carries not one symbol of
// it. The C++ side is the producer (§19.1's builder) and this side is the
// consumer: it POINTS at bytes another language wrote and reads rows in place,
// with no marshalling and no copy at the boundary.
//
// Two ways to read one block, and both come from one declaration (§19.2):
// the DESCRIPTORS, which carry the projection's own layout and retire a
// hand-kept mirror, and the generated BLITTABLE STRUCT beside them, which is
// the typed fast path a per-frame job uses. A consumer picks by what it is
// doing — reflection to walk anything, the struct to read one thing fast.
//
// THE LAYOUT MODEL IS NAMED (§19.3): every size and offset asserted here is
// the MANAGED unmanaged-struct model — what Span and pointer arithmetic
// actually index with — never the interop marshalling model. The consequence
// stated plainly: a `bool` in a row is ONE byte, one in C++ and one here, four
// under default marshalling. Pack and Size set the managed layout too, despite
// reading as interop attributes, which is exactly why they are the mechanism.
//
// ALLOCATION: none of it. The bytes belong to the consumer — a managed array,
// a NativeArray, or a pointer the producer handed across — and this side takes
// a pointer and a length and points.
package cstable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateBlockFiles emits <Base>Block.cs for every file of a unit that
// declares a table. A file whose tables all lack a block form still gets one,
// saying which table and why.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if blocks == nil {
		return out, nil
	}
	// THE BLOCK HOME is the first file, by basename, that declares a table
	// WITH a block form — never the protocol id's home, which may declare no
	// table at all (a unit whose constants live in their own file is the
	// ordinary case, and the dogfood is one). The runtime and every blittable
	// record land there, once per unit.
	//
	// C# has no include guard and no per-file visibility: one assembly sees
	// every file, so "emitted once, anywhere" is the whole requirement. C++
	// takes the other road — its primitives ride in EVERY <Base>Block.h behind
	// a `#ifndef` — because a C++ consumer may include one header alone.
	home := blockHome(u, blocks)
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &blockGen{unit: u, file: f, blocks: blocks, home: f.Base == home}
		g.emit()
		out[f.Base+"Block.cs"] = g.assemble()
	}
	return out, nil
}

// blockHome is the file the unit's shared block runtime and every blittable
// record are emitted into: the first, by basename, that declares a table with
// a block form. Empty when the unit has no block form at all, in which case
// no file needs the runtime.
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
	unit    *ir.Unit
	file    *ir.File
	blocks  *ir.BlockUnit
	home    bool
	runtime strings.Builder // the shared runtime, home file only
	handles strings.Builder // namespace <Pkg>: the block handles
	structs strings.Builder // the blittable records, in the package namespace
}

func (g *blockGen) rf(format string, args ...any) { fmt.Fprintf(&g.runtime, format, args...) }
func (g *blockGen) hf(format string, args ...any) { fmt.Fprintf(&g.handles, format, args...) }
func (g *blockGen) sf(format string, args ...any) { fmt.Fprintf(&g.structs, format, args...) }

func (g *blockGen) emit() {
	if g.home {
		g.rf("%s", blockRuntime(ir.BuildVersion(g.unit)))
		g.emitLayoutCheck()
		// EVERY blittable record of the unit, here and nowhere else. Not the
		// file that DECLARES the type: a record a block form reaches is often
		// declared in a file of `type`s alone, which gets no Block.cs of its
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
	for _, st := range g.file.Tables {
		if bl := g.blocks.Block(st.Name); bl != nil {
			g.emitBlockHandle(bl)
			continue
		}
		g.hf("// table %s has NO block form: %s (SPEC-TABLES.md §19).\n", st.Name, g.blocks.SkippedReason(st.Name))
		g.hf("// Its wire (§3) is unaffected — only this projection is absent, and it is\n")
		g.hf("// absent by construction rather than by refusal.\n\n")
	}
}

func (g *blockGen) assemble() []byte {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the BLOCK FORM (SPEC-TABLES.md §19): the READ half.\n", g.unit.Package)
	h.WriteString("//\n")
	h.WriteString("// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on\n")
	h.WriteString("// the side: compile this file only if you read a block. The unit's\n")
	h.WriteString("// <Base>Table.cs carries not one symbol of it.\n")
	h.WriteString("//\n")
	h.WriteString("// It is UNSAFE by nature, not by taste: a block is memory another language\n")
	h.WriteString("// wrote, and pointing at it without a copy is the whole point (§12.1). A\n")
	h.WriteString("// project that compiles this file sets AllowUnsafeBlocks.\n")
	h.WriteString("//\n")
	h.WriteString("// Every size and offset below is the MANAGED unmanaged-struct model — what\n")
	h.WriteString("// Span and pointer arithmetic index with — never the interop marshalling\n")
	h.WriteString("// model (§19.3). A bool in a row is ONE byte.\n\n")
	h.WriteString("using System;\n")
	h.WriteString("using System.Runtime.CompilerServices;\n")
	h.WriteString("using System.Runtime.InteropServices;\n\n")
	fmt.Fprintf(&h, "namespace %s\n{\n\n", capitalize(g.unit.Package))
	h.WriteString(indent4(g.runtime.String()))
	if g.structs.Len() > 0 {
		var b strings.Builder
		b.WriteString("// The BLITTABLE records: one per record the block form touches, laid out to\n")
		b.WriteString("// the C ABI with GENERATED PADDING FIELDS wherever the layout has interior\n")
		b.WriteString("// padding and a Size that pins the trailing padding — both are needed\n")
		b.WriteString("// (SPEC-TABLES.md §19.3). Explicit padding is chosen over LayoutKind.Explicit\n")
		b.WriteString("// because Sequential is the form every blittable path handles best, and over\n")
		b.WriteString("// relying on a padding-free field order because that is discipline, and\n")
		b.WriteString("// discipline is what this form exists to delete.\n")
		b.WriteString("//\n")
		b.WriteString("// They take a CLAIMED SUFFIX in the unit's own namespace — <Name>Row for a\n")
		b.WriteString("// row and <Table>BlockProjection for a projection — because the namespace\n")
		b.WriteString("// already holds a sealed CLASS of each declaration's name, which is the\n")
		b.WriteString("// table wire's storage, and one declaration cannot be two types.\n")
		h.WriteString(indent4(b.String()))
		h.WriteString(indent4(g.structs.String()))
	}
	if g.handles.Len() > 0 {
		h.WriteString(indent4(g.handles.String()))
	}
	h.WriteString("\n}\n")
	return []byte(h.String())
}

// ---- the blittable records ----

func (g *blockGen) emitBlittable(name string) {
	ml := g.blocks.Layout(name)
	if ml == nil {
		return
	}
	g.sf("// %s — a block row, or a record one nests by value. `Row` is a CLAIMED\n", name)
	g.sf("// suffix (SPEC-TABLES.md §11), so no declaration in the unit can take it.\n")
	g.sf("[StructLayout(LayoutKind.Sequential, Pack = 1, Size = %d)]\n", ml.Size)
	g.sf("public unsafe struct %sRow\n{\n", name)
	g.emitBlittableFields(ml, 0, false)
	g.sf("}\n\n")
}

func (g *blockGen) emitProjection(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.sf("// %s — the block PROJECTION: the table's own instance as it sits at the\n", name)
	g.sf("// front of a block, opening with the generated PROLOGUE and carrying, per\n")
	g.sf("// out-of-line array, the triple that says where its rows are. It is a record\n")
	g.sf("// like any other and follows the same C ABI rule (SPEC-TABLES.md §19.3).\n")
	g.sf("//\n")
	g.sf("// It is a SEPARATE record from <Table>Row: a table can be both a block root\n")
	g.sf("// and another block's row, and the two differ by the prologue.\n")
	g.sf("[StructLayout(LayoutKind.Sequential, Pack = 1, Size = %d)]\n", bl.Projection.Size)
	g.sf("public unsafe struct %sBlockProjection\n{\n", name)
	g.sf("    public ulong Magic;        // generated: identifies a schema block\n")
	g.sf("    public ulong BuildVersion; // generated: the unit's build version (SPEC-TABLES.md §20)\n")
	g.sf("    public ulong ByteOrder;    // generated: 1 little, 2 big\n")
	g.emitBlittableFields(&bl.Projection, ir.BlockPrologueBytes, true)
	g.sf("}\n\n")
}

// emitBlittableFields walks one record's computed layout and emits its fields
// with generated padding between them.
//
// `projection` is the whole of what decides whether a bounded array becomes a
// TRIPLE or stays INLINE, and it is load-bearing (SPEC-TABLES.md §2.7): DEPTH
// ONE, BOUNDED ONLY — only the block-form TABLE'S OWN bounded arrays of
// structs are laid out of line, and every array at any depth inside a row or
// inside a record a row nests is inline storage exactly where it always was.
// Emitting a triple for one of those puts sixteen bytes where the C++ side put
// the whole array, and every field after it lands somewhere else.
func (g *blockGen) emitBlittableFields(ml *ir.MemberLayout, at int64, projection bool) {
	w := &blockWriter{g: g, at: at}
	for _, fl := range ml.Fields {
		// Pad to each PIECE of the field, not only to the field: a field's own
		// storage can carry interior padding — a `string(N)` buffer followed by
		// its int32 length is the ordinary case — and padding only between
		// fields slides every field after it.
		w.pieces = ir.BlockFieldPieceOffsets(g.unit, fl.Field, fl.Offset, projection)
		w.piece = 0
		g.emitBlittableField(fl.Field, projection, w)
	}
	w.pad(ml.Size) // the trailing padding is pinned by Size too, and stating it costs nothing
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

func (w *blockWriter) pad(to int64) {
	for w.at < to {
		w.g.sf("    private byte _pad%d;\n", w.at)
		w.at++
	}
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

// emitBlittableField emits one field's pieces, padding to each piece's own
// offset — the model's answer for where every piece starts (§19.3).
func (g *blockGen) emitBlittableField(f *ir.Field, projection bool, w *blockWriter) {
	name := ir.GoExportName(f.Name)
	next := w.next
	if projection && ir.BlockOutOfLine(f) {
		next()
		g.sf("    public TableBlockTriple %s; // [..%d]%s, laid out of line\n", name, f.ArrayBound, f.Type.Name)
		return
	}
	switch {
	case f.Type.Kind == ir.TString:
		next()
		g.sf("    public fixed byte %s[%d]; // string(%d): max length, used length beside it\n", name, f.Type.Size+1, f.Type.Size)
		next()
		g.sf("    public int %sLength;\n", name)
	case f.Type.Kind == ir.TBytes:
		next()
		g.sf("    public fixed byte %s[%d]; // bytes(%d): fixed buffer, used length beside it\n", name, f.Type.Size, f.Type.Size)
		next()
		g.sf("    public int %sLength;\n", name)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
		next()
		g.emitBlittableArray(f, name)
		if f.Array == ir.ArrayCounted {
			next()
			g.sf("    public int %sCount;\n", name)
		}
	default:
		next()
		g.sf("    public %s %s;\n", g.blittableType(f.Type), name)
	}
	if f.Type.Optional {
		next()
		g.sf("    public bool %sPresent; // ?%s: one byte, in the managed model as in C++\n", name, f.Type.Name)
	}
}

// emitBlittableArray spells an INLINE array. A primitive element takes a C#
// fixed-size buffer, which is exactly the C ABI's `T[N]`; every other element
// — an enum, a nested record — takes N generated fields, because a fixed-size
// buffer accepts primitives only.
func (g *blockGen) emitBlittableArray(f *ir.Field, name string) {
	typ := g.blittableType(f.Type)
	if csFixedBufferPrimitive(typ) {
		g.sf("    public fixed %s %s[%d];\n", typ, name, f.ArrayBound)
		return
	}
	g.sf("    // [%d]%s: a fixed-size buffer takes primitives only, so the elements are\n", f.ArrayBound, typ)
	g.sf("    // generated one per slot — the same bytes at the same offsets.\n")
	for i := int64(0); i < f.ArrayBound; i++ {
		g.sf("    public %s %s%d;\n", typ, name, i)
	}
}

func csFixedBufferPrimitive(typ string) bool {
	switch typ {
	case "bool", "byte", "sbyte", "short", "ushort", "int", "uint", "long", "ulong", "char", "float", "double":
		return true
	}
	return false
}

// blittableType maps a field's declared type to its C# blittable spelling. A
// bool is ONE byte under the managed model, which is what this form asserts
// (SPEC-TABLES.md §19.3).
func (g *blockGen) blittableType(t ir.FieldType) string { return csBlittableType(g.unit, t) }

// csBlittableType maps a field's declared type to its C# blittable spelling,
// for both accelerators: a cooked record and a block row are one set of structs
// from one layout model, so they are spelled by one function (§7).
func csBlittableType(u *ir.Unit, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TInt:
		if t.Signed {
			switch t.Width {
			case 8:
				return "sbyte"
			case 16:
				return "short"
			case 32:
				return "int"
			default:
				return "long"
			}
		}
		switch t.Width {
		case 8:
			return "byte"
		case 16:
			return "ushort"
		case 32:
			return "uint"
		default:
			return "ulong"
		}
	case ir.TBits:
		if t.Width <= 32 {
			return "uint"
		}
		return "ulong"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			// the enum lives in the unit's own namespace, beside the table
			// wire's storage classes; the blittable records live one namespace
			// in, so the reference is qualified
			return capitalize(u.Package) + "." + t.Name
		case *ir.Flags:
			return "ulong"
		case *ir.Struct:
			return t.Name + "Row"
		}
	}
	return "byte"
}

// ---- the layout check (SPEC-TABLES.md §19.3) ----

// emitLayoutCheck emits the generated check, run once, asserting each type's
// size and each field's offset against the same constants the C++ side
// asserts. C# cannot say this at compile time, so it says it at type
// initialization and throws naming the type, the field, the expected offset
// and the one THIS runtime produced.
func (g *blockGen) emitLayoutCheck() {
	g.rf("// The LAYOUT CONTRACT's C# half (SPEC-TABLES.md §19.3), run ONCE: every size\n")
	g.rf("// and offset the C++ side static_asserts, asserted here against the MANAGED\n")
	g.rf("// model — Unsafe.SizeOf and address arithmetic on a stack instance, never\n")
	g.rf("// Marshal.SizeOf or Marshal.OffsetOf. The two models disagree on the field\n")
	g.rf("// kinds this form uses, and a contract that did not say which it asserted\n")
	g.rf("// could pass on one measurement and garble on the other.\n")
	g.rf("//\n")
	g.rf("// Neither side's layout is inferred from the other's: both are checked\n")
	g.rf("// against their own runtime's model, which is the only way a two-language\n")
	g.rf("// contract can be held by a compiler that generates both halves.\n")
	// PUBLIC, not internal: a consumer's own test leg calls Verify() to hold
	// §19.3's contract at start-up rather than waiting for the first Open to
	// throw in a game, and a check nobody outside the assembly can call is a
	// check nobody calls.
	g.rf("public static unsafe class TableBlockLayout\n{\n")
	g.rf("    private static bool checked_;\n\n")
	g.rf("    internal static void Verify()\n    {\n")
	g.rf("        if (checked_) { return; }\n")
	g.rf("        checked_ = true;\n")
	for _, name := range g.blocks.Order {
		g.emitRecordCheck(name, g.blocks.Layout(name), false)
	}
	for _, bl := range g.blocks.Tables {
		g.emitRecordCheck(bl.Table.Name, &bl.Projection, true)
		// and each array's PITCH CONSTANT, against this runtime's own sizeof.
		// Without this the constant is emitted and never read, so perturbing
		// it on one side only — which is §19.5's named negative control —
		// could not turn anything red here.
		for _, a := range bl.Arrays {
			g.rf("        Size(\"%sBlock.%sStride\", (int) %sBlock.%sStride, Unsafe.SizeOf<%sRow>());\n",
				bl.Table.Name, ir.GoExportName(a.Field.Name), bl.Table.Name, ir.GoExportName(a.Field.Name),
				a.ElemName)
		}
	}
	g.rf("    }\n\n")
	g.rf("    private static void Size(string what, int got, int want)\n    {\n")
	g.rf("        if (got != want)\n        {\n")
	g.rf("            throw new InvalidOperationException(\n")
	g.rf("                \"schema block layout: \" + what + \" is \" + got + \" bytes in this runtime and \" + want +\n")
	g.rf("                \" in the schema the C++ side asserts — the two sides disagree about the bytes (SPEC-TABLES.md §19.3)\");\n")
	g.rf("        }\n    }\n\n")
	g.rf("    private static void Offset(string what, long got, long want)\n    {\n")
	g.rf("        if (got != want)\n        {\n")
	g.rf("            throw new InvalidOperationException(\n")
	g.rf("                \"schema block layout: \" + what + \" sits at \" + got + \" in this runtime and \" + want +\n")
	g.rf("                \" in the schema the C++ side asserts — the two sides disagree about the bytes (SPEC-TABLES.md §19.3)\");\n")
	g.rf("        }\n    }\n")
	g.rf("}\n\n")
}

func (g *blockGen) emitRecordCheck(name string, ml *ir.MemberLayout, projection bool) {
	if ml == nil {
		return
	}
	spelled := name + "Row"
	if projection {
		spelled = name + "BlockProjection"
	}
	g.rf("        {\n")
	g.rf("            %s probe = default;\n", spelled)
	g.rf("            byte* at = (byte*) &probe;\n")
	g.rf("            Size(\"%s\", Unsafe.SizeOf<%s>(), %d);\n", spelled, spelled, ml.Size)
	if projection {
		g.rf("            Offset(\"%s.Magic\", (byte*) &probe.Magic - at, 0);\n", spelled)
		g.rf("            Offset(\"%s.BuildVersion\", (byte*) &probe.BuildVersion - at, 8);\n", spelled)
		g.rf("            Offset(\"%s.ByteOrder\", (byte*) &probe.ByteOrder - at, 16);\n", spelled)
	}
	for _, fl := range ml.Fields {
		g.rf("            Offset(\"%s.%s\", (byte*) %s - at, %d);\n", spelled, ir.GoExportName(fl.Field.Name),
			g.blockFieldAddress(fl.Field, projection), fl.Offset)
	}
	g.rf("        }\n")
}

// blockFieldAddress spells the address of one field of the stack probe. A
// fixed-size buffer has no address of its own — its FIRST element does, and
// that is the same byte.
// blockFieldAddress spells the address of one field of the stack probe. It
// takes `projection` for the same reason emitBlittableFields does: a bounded
// array of structs is a TRIPLE in a projection and INLINE everywhere else, and
// the two have different addresses to take (SPEC-TABLES.md §2.7).
func (g *blockGen) blockFieldAddress(f *ir.Field, projection bool) string {
	name := ir.GoExportName(f.Name)
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return "probe." + name // a fixed-size buffer IS a pointer in unsafe context
	}
	inline := !projection || !ir.BlockOutOfLine(f)
	if inline && (f.KeyEnum != "" || f.Array != ir.ArrayNone) {
		if csFixedBufferPrimitive(g.blittableType(f.Type)) {
			return "probe." + name
		}
		return "&probe." + name + "0" // the per-slot generated fields
	}
	return "&probe." + name
}

// ---- the block handle ----

func (g *blockGen) emitBlockHandle(bl *ir.BlockLayout) {
	name := bl.Table.Name
	pkg := capitalize(g.unit.Package)
	g.hf("// %s's block: a pointer and a length, and then rows in place. Opening one\n", name)
	g.hf("// is ONE check and no copy; reading a row is one add (SPEC-TABLES.md §19.2).\n")
	g.hf("//\n")
	g.hf("// The bytes belong to the CONSUMER — a managed array it pinned, a NativeArray,\n")
	g.hf("// or memory the producer handed across. Nothing here allocates.\n")
	g.hf("public unsafe readonly struct %sBlock\n{\n", name)
	g.hf("    private readonly byte* basePointer;\n")
	g.hf("    private readonly long bytes;\n\n")
	g.hf("    private %sBlock(byte* basePointer, long bytes)\n    {\n", name)
	g.hf("        this.basePointer = basePointer;\n        this.bytes = bytes;\n    }\n\n")
	g.hf("    static %sBlock()\n    {\n", name)
	g.hf("        // the layout contract, run once before any static member of this type\n")
	g.hf("        TableBlockLayout.Verify();\n")
	g.hf("    }\n\n")
	g.hf("    // The storage a PRODUCER of this block allocates, sized from the declared\n")
	g.hf("    // maxima (SPEC-TABLES.md §19.1). A C# consumer does not allocate a block —\n")
	g.hf("    // the bytes are handed to it — but it caps by this: a playback buffer, a\n")
	g.hf("    // recording, a scratch copy all size from the generated constant rather\n")
	g.hf("    // than from a number a person wrote down beside it.\n")
	g.hf("    public const long BlockMaxBytes = %d;\n\n", bl.MaxBytes)
	g.hf("    public byte* Base { get { return basePointer; } }\n")
	g.hf("    public long Bytes { get { return bytes; } }\n\n")
	g.hf("    // the table's own declared fields, read where they lie\n")
	g.hf("    public ref readonly %sBlockProjection Projection { get { return ref *(%sBlockProjection*) basePointer; } }\n\n", name, name)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("    // %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		g.hf("    // what it read from the instance, never with these (SPEC-TABLES.md §19.2).\n")
		g.hf("    public const long %sStride = %d;\n", field, a.Stride)
		g.hf("    public const long %sMax = %d;\n", field, a.Max)
		g.hf("    public const long %sProjectionOffset = %d;\n\n", field, a.TripleOffset)
		g.hf("    // ITERATED, not indexed by hand: the accessor yields a reference to each\n")
		g.hf("    // row where it lies, at the pitch the INSTANCE gives, for count rows.\n")
		g.hf("    public TableBlockRows<%sRow> %s\n    {\n", a.ElemName, field)
		g.hf("        get\n        {\n")
		g.hf("            %sBlockProjection* p = (%sBlockProjection*) basePointer;\n", name, name)
		g.hf("            return new TableBlockRows<%sRow>(basePointer + p->%s.OffsetOf, (int) p->%s.Count, (int) p->%s.Stride);\n",
			a.ElemName, field, field, field)
		g.hf("        }\n    }\n\n")
		g.hf("    // and the CONTIGUOUS view, available because the pitch IS sizeof (§2.7),\n")
		g.hf("    // which is how the per-frame fast path is actually written.\n")
		g.hf("    public ReadOnlySpan<%sRow> %sSpan\n    {\n", a.ElemName, field)
		g.hf("        get\n        {\n")
		g.hf("            %sBlockProjection* p = (%sBlockProjection*) basePointer;\n", name, name)
		g.hf("            return new ReadOnlySpan<%sRow>(basePointer + p->%s.OffsetOf, (int) p->%s.Count);\n",
			a.ElemName, field, field)
		g.hf("        }\n    }\n\n")
	}
	g.emitBlockOpen(bl)
	g.emitBlockDescriptors(bl)
	g.hf("}\n\n")
	_ = pkg
}

func (g *blockGen) emitBlockOpen(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.hf("    // Open checks once and points, and this is the WHOLE check (SPEC-TABLES.md\n")
	g.hf("    // §19.2): the magic read bytewise, the BYTE ORDER the prologue carries\n")
	g.hf("    // against this build's own, the BUILD VERSION against this build's own,\n")
	g.hf("    // each array's pitch, its offset_of, its COUNT against the declared\n")
	g.hf("    // maximum and its extent inside the block, the used extent\n")
	g.hf("    // against the bytes the caller passed, and the base's alignment. On a\n")
	g.hf("    // match the bytes are what a build with this layout wrote, so there is\n")
	g.hf("    // nothing to validate and nothing to fix up. On any failure it returns\n")
	g.hf("    // false and points at nothing.\n")
	g.hf("    //\n")
	g.hf("    // There is ONE entry point and no tolerant twin: the block form is\n")
	g.hf("    // same-build by construction — both sides generated from one declaration\n")
	g.hf("    // at one build — so a consumer older than its producer is not a case. A\n")
	g.hf("    // mismatch is a refusal; regenerate both sides. Data that must outlive the\n")
	g.hf("    // build that wrote it takes the wire, which this same table still has.\n")
	g.hf("    public static bool Open(out %sBlock block, IntPtr pointer, long bytes)\n    {\n", name)
	g.hf("        block = default;\n")
	g.hf("        TableBlockLayout.Verify();\n")
	g.hf("        if (pointer == IntPtr.Zero || bytes < %d) { return false; }\n", bl.Projection.Size)
	g.hf("        byte* at = (byte*) pointer;\n")
	g.hf("        if ((((ulong) pointer) %% %d) != 0) { return false; } // the base's alignment\n", ir.BlockAlign)
	g.hf("        if (Schema.TableBlockRead64(at) != Schema.TableBlockMagic) { return false; }\n")
	g.hf("        if (Schema.TableBlockRead64(at + 8) != Schema.BuildVersion) { return false; }\n")
	g.hf("        if (Schema.TableBlockRead64(at + 16) != Schema.TableBlockByteOrder) { return false; }\n")
	g.hf("        %sBlockProjection* projection = (%sBlockProjection*) at;\n", name, name)
	g.hf("        long used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		alignment := ir.BlockAlign
		if a.ElemAlign() > int64(alignment) {
			alignment = int(a.ElemAlign())
		}
		g.hf("        {\n")
		g.hf("            // EVERY NUMBER BELOW COMES FROM THE INSTANCE, so the arithmetic is\n")
		g.hf("            // unsigned and each term is BOUNDED BEFORE IT IS ADDED. A forged\n")
		g.hf("            // OffsetOf near 2^63 must refuse, and an addition that wrapped past\n")
		g.hf("            // the top of the type would be what the check after it was supposed\n")
		g.hf("            // to catch. The C++ side holds the same shape for the same reason.\n")
		g.hf("            ulong offsetOf = projection->%s.OffsetOf;\n", field)
		g.hf("            ulong count = projection->%s.Count;\n", field)
		g.hf("            ulong stride = projection->%s.Stride;\n", field)
		g.hf("            if (stride != (ulong) Unsafe.SizeOf<%sRow>()) { return false; }\n", a.ElemName)
		g.hf("            // past the DECLARED MAXIMUM: Begin refuses this on the producer\n")
		g.hf("            // side and Open refuses it here, because a consumer that sizes\n")
		g.hf("            // anything by the maximum would overflow on a count the maximum\n")
		g.hf("            // does not bound\n")
		g.hf("            if (count > (ulong) %sMax) { return false; }\n", field)
		g.hf("            if (offsetOf < %d || (offsetOf %% %d) != 0) { return false; }\n", bl.Projection.Size, alignment)
		g.hf("            if (offsetOf > (ulong) bytes) { return false; }\n")
		g.hf("            ulong rows = count * stride; // both bounded above: this cannot carry\n")
		g.hf("            if (rows > (ulong) bytes - offsetOf) { return false; }\n")
		g.hf("            long end = (long) (offsetOf + rows);\n")
		g.hf("            if (end > used) { used = end; }\n")
		g.hf("        }\n")
	}
	g.hf("        // the used extent, rounded to %d WITHOUT the rounding itself wrapping:\n", ir.BlockAlign)
	g.hf("        // used is already inside bytes, and the padding is paid out of the slack\n")
	g.hf("        // that is left rather than added and compared after.\n")
	g.hf("        long padding = (%d - (used %% %d)) %% %d;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.hf("        if (padding > bytes - used) { return false; }\n")
	g.hf("        used += padding;\n")
	g.hf("        block = new %sBlock(at, used);\n", name)
	g.hf("        return true;\n    }\n\n")
}

// emitBlockDescriptors is the REFLECTIVE half (SPEC-TABLES.md §8, §19.2): the
// projection offset of every field, the offsets of the three members inside
// each triple, and the element's own descriptor beside them. A consumer
// holding these reads the facts out of an instance and points at rows, with no
// hand-written struct per table and nothing to maintain when a field is added.
func (g *blockGen) emitBlockDescriptors(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.hf("    // this table's block descriptors: constant data, so a reflective read\n")
	g.hf("    // costs a lookup and not a parse. The row layouts hang off the element\n")
	g.hf("    // column rather than taking names of their own, so a walker reaches every\n")
	g.hf("    // record through the graph.\n")
	records := blockDescriptorRecords(g.blocks, bl)
	for _, r := range records {
		g.emitBlockRecordDescriptor(name, r, g.blocks.Layout(r), nil)
	}
	g.emitBlockRecordDescriptor(name, "", &bl.Projection, bl)
	g.hf("    public static TableBlockInfo Type { get { return %s; } }\n", blockInfoSymbol(name, ""))
}

func (g *blockGen) emitBlockRecordDescriptor(owner, record string, ml *ir.MemberLayout, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	symbol := blockInfoSymbol(owner, record)
	name := record
	if bl != nil {
		name = bl.Table.Name
	}
	g.hf("    private static readonly TableBlockInfo %s = new TableBlockInfo\n    {\n", symbol)
	g.hf("        Name = %q, BuildVersion = Schema.BuildVersion, Size = %d, Align = %d, NumFields = %d,\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.hf("        Fields = new TableBlockFieldInfo[]\n        {\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkArray
		}
		if bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				g.hf("            new TableBlockFieldInfo { Name = %q, Offset = %d, Size = %d, Kind = %d, OutOfLine = true, OffsetOfOffset = %d, CountOffset = %d, StrideOffset = %d, Stride = %d, ElementRef = delegate { return %s; } },\n",
					f.Name, fl.Offset, fl.Size, kind, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride,
					blockInfoSymbol(owner, a.ElemName))
				continue
			}
		}
		element := "null"
		// A field that NAMES a record carries that record's layout, whether it
		// holds one or an array of them: an INLINE array of records is part of
		// a row, and a walker descending one reaches its element through this
		// same column. Only the pointer class has no layout to name.
		if f.Type.Kind == ir.TNamed && !f.Type.Pointer {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				element = fmt.Sprintf("delegate { return %s; }", blockInfoSymbol(owner, ref.Name))
			}
		}
		g.hf("            new TableBlockFieldInfo { Name = %q, Offset = %d, Size = %d, Kind = %d, OutOfLine = false, OffsetOfOffset = -1, CountOffset = -1, StrideOffset = -1, Stride = 0, ElementRef = %s },\n",
			f.Name, fl.Offset, fl.Size, kind, element)
	}
	g.hf("        },\n    };\n\n")
}

// blockDescriptorRecords is every record one block's descriptors reach, sorted.
func blockDescriptorRecords(b *ir.BlockUnit, bl *ir.BlockLayout) []string {
	seen := map[string]bool{}
	var out []string
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
		out = append(out, name)
		for _, fl := range ml.Fields {
			if fl.Field.Type.Kind == ir.TNamed && !fl.Field.Type.Pointer {
				if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
					walk(ref.Name)
				}
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
		if fl.Field.Type.Kind == ir.TNamed && !fl.Field.Type.Pointer {
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// blockInfoSymbol names one record's descriptor inside its owner's block type.
// The empty record is the owner's own projection.
func blockInfoSymbol(owner, record string) string {
	if record == "" {
		return "blockProjection"
	}
	return "blockRow" + record
}

// blockRuntime is the shared block runtime, emitted once per unit into the
// home file's <Base>Block.cs.
func blockRuntime(buildVersion uint64) string {
	return `// What a table knows about ONE of its out-of-line arrays: where the rows
// start, how many there are, and how far apart they sit. Sixteen bytes with no
// interior padding, sitting at the array field's own position in the
// projection (SPEC-TABLES.md §2.7). A consumer reads all three FROM THE
// INSTANCE, never from its own constants — that is the difference between a
// generated pair of structs and an ABI (§19.2).
[StructLayout(LayoutKind.Sequential, Pack = 1, Size = 16)]
public struct TableBlockTriple
{
    public ulong OffsetOf; // block-relative: the block relocates by plain memcpy
    public uint Count;     // rows the producer filled; rows past it are not part of the block
    public uint Stride;    // the pitch the consumer indexes with, from the data
}

// One array's rows, ITERATED at the pitch the instance gives (SPEC-TABLES.md
// §19.2). A call site never spells the pitch arithmetic itself, for the same
// reason a keyed array's call sites should not re-derive their own slot rule:
// the idiom written at every call site is the one written wrong somewhere.
public unsafe readonly struct TableBlockRows<T> where T : unmanaged
{
    private readonly byte* rows;
    private readonly int count;
    private readonly int stride;

    public TableBlockRows(byte* rows, int count, int stride)
    {
        this.rows = rows;
        this.count = count;
        this.stride = stride;
    }

    public int Length { get { return count; } }
    public int Stride { get { return stride; } }

    public ref readonly T this[int index]
    {
        get { return ref *(T*) (rows + (long) index * stride); }
    }

    public Enumerator GetEnumerator() { return new Enumerator(rows, count, stride); }

    public unsafe struct Enumerator
    {
        private byte* at;
        private int remaining;
        private readonly int stride;

        internal Enumerator(byte* rows, int count, int stride)
        {
            this.at = rows - stride;
            this.remaining = count;
            this.stride = stride;
        }

        public bool MoveNext()
        {
            if (remaining == 0) { return false; }
            remaining--;
            at += stride;
            return true;
        }

        public ref readonly T Current { get { return ref *(T*) at; } }
    }
}

// ---- reflection over a block (SPEC-TABLES.md §8, §19.2) ----
//
// The descriptors are the mechanism, and they are what retires a hand-kept
// mirror: a consumer holding them reads the triples out of an instance and
// points at rows, with no hand-written struct per table and no knowledge of
// the spelling that produced any of it.
public sealed class TableBlockFieldInfo
{
    public string Name;
    public int Offset;      // the field's offset in the record this descriptor describes
    public int Size;        // its size there
    public byte Kind;       // the table-wire kind, as TableFieldInfo carries it
    public bool OutOfLine;  // an out-of-line array: the three members below are live
    public int OffsetOfOffset; // the triple's OffsetOf member, or -1
    public int CountOffset;    // its Count member, or -1
    public int StrideOffset;   // its Stride member, or -1
    public int Stride;         // THIS BUILD's pitch, to assert against — never to index with (§19.2)
    // the ELEMENT's or the nested record's own layout, behind a delegate so
    // the table stays constructible in any order. null when the field is a
    // scalar. Following it is how a walker DESCENDS: an out-of-line array's
    // rows, and a nested record's fields, are both reached through this one
    // column.
    public Func<TableBlockInfo> ElementRef;
    public TableBlockInfo Element
    {
        get { return ElementRef == null ? null : ElementRef(); }
    }
}

// One record's layout as DATA — the whole mechanism behind the block form's
// read side, and what retires a hand-kept mirror. A block-form table's own
// descriptor describes its PROJECTION; the element descriptor of each
// out-of-line array describes that array's ROW, and so on down.
public sealed class TableBlockInfo
{
    public string Name;
    public ulong BuildVersion; // the unit's (SPEC-TABLES.md §20)
    public int Size;           // the record's own size: a projection's, or a row's
    public int Align;
    public int NumFields;
    public TableBlockFieldInfo[] Fields;
}

// Schema carries every generated function and constant of the unit — C# has no
// namespace-level functions — and this is the block form's slice of it.
public static partial class Schema
{
    // THE BUILD VERSION (SPEC-TABLES.md §20): one digest over every fact the
    // bytes this build produces depend on — the type wire's protocol id, every
    // table's layout keyed by wire id, every table's meaning (defaults,
    // ranges, enum and union vocabularies, keyed the same way), and the
    // build's byte order. It is the number a block carries and the number Open
    // compares.
    //
    // There are TWO ids in the design and they are not interchangeable: the
    // PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION
    // is what everything cooked or blocked is keyed by. A table edit moves
    // this and never the protocol id; a type edit moves both.
    public const ulong BuildVersion = ` + fmt.Sprintf("0x%016xUL", buildVersion) + `;

    // The block's magic (SPEC-TABLES.md §19.1), read BYTEWISE: it is the one
    // field read without assuming the order the rest of the block is in.
    public const ulong TableBlockMagic = 0x4b4c42414d484353UL;

    // THIS BUILD's byte order, as the prologue carries it (§20.3). A block
    // written by a build of the other order is REFUSED by Open: a big-endian
    // fix-up path is a named obligation, not something a consumer improvises
    // row by row.
    public static ulong TableBlockByteOrder
    {
        get { return BitConverter.IsLittleEndian ? 1UL : 2UL; }
    }

    public static unsafe ulong TableBlockRead64(byte* p) { return *(ulong*) p; }
}

`
}
