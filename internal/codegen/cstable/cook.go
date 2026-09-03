// THE COOKED FORM in C# (docs/SPEC-TABLES.md §7): the READ side, emitted ON THE
// SIDE into <Base>Cook.cs.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format, not a wire. Tooling writes
// a region for one build version and that build points at it: the header is
// matched, the root is the record at the data part's base, and nothing else
// happens. No walk, no fix-up, no allocation — which is what makes Open O(1)
// in the file's size, the bar the scale §7 is built for asks for.
//
// NOTHING DECLARES IT, exactly as nothing declares the block form. Every table
// gets an Open, a consumer compiles this file only if it opens a cook, and
// <Base>Table.cs carries not one symbol of it.
//
// A COOKED RECORD IS THE BLITTABLE ROW. The region is laid out by §20.3's C ABI
// model, which is the same model the block form's <Name>Row structs are spelled
// from — so the two accelerators share one set of records rather than growing a
// second ABI. A record the block form already emits is emitted THERE and not
// again here; this file emits the rest of the unit's cook closure.
//
// THE LAYOUT MODEL IS NAMED (§19.3): every size and offset here is the MANAGED
// unmanaged-struct model — what Span and pointer arithmetic index with — never
// the interop marshalling model. A `bool` in a cooked record is ONE byte.
//
// ALLOCATION: none, on open or on read. The bytes belong to the consumer — an
// mmap, a NativeArray, a pinned array — and this side takes a pointer and a
// length and points.
package cstable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookMagic identifies a cooked file and carries the byte-order check with it
// (docs/SPEC-TABLES.md §7.1). It is "SCHMCOOK" read as ASCII in the byte order a
// little-endian store produces, the same shape the block's SCHMABLK takes, and
// it is stored in the PRODUCER's order: a consumer reads back this constant, or
// that constant byte-reversed, which is a cook of the other order.
const cookMagic = uint64(0x4B4F4F434D484353)

// cookHeaderBytes is §7.1's header: eight u64 words.
const cookHeaderBytes = int64(64)

// cookMaxAlign is the ceiling on the header's `alignment` word (§7): the same
// sixty-four a block's base takes, and past which the derived data offset would
// no longer be the 64 every unit this language can declare produces.
const cookMaxAlign = int64(64)

// generateCookFiles emits <Base>Cook.cs for every file of a unit that declares
// a table, plus <Package>Cook.cs — the unit's one runtime home — when no file is
// named for the package. A table whose closure C# cannot spell blittably still
// gets a line saying which and why, never a silent absence.
func generateCookFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(u.Tables) == 0 {
		return out, nil
	}
	ck := cookUnitOf(u)
	if len(ck.tables) == 0 && len(ck.skipped) == 0 {
		return out, nil
	}
	// THE COOK HOME is <Package>Cook.cs — one home per unit, named by the
	// package, independent of file order (runtimeHome, cstable.go). The shared
	// cook runtime and every blittable record the BLOCK form does not already
	// emit land there, once per unit.
	//
	// C# has no include guard and no per-file visibility: one assembly sees
	// every file, so "emitted once, anywhere" is the whole requirement.
	home := runtimeHome(u)
	runtimeWritten := false
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &cookGen{unit: u, file: f, cook: ck, blocks: blocks, home: f.Base == home}
		g.emit()
		if g.home {
			runtimeWritten = true
		}
		out[f.Base+"Cook.cs"] = g.assemble()
	}
	// No file of the unit is named for the package, so the home is emitted for
	// the unit rather than for a file.
	if !runtimeWritten {
		g := &cookGen{unit: u, cook: ck, blocks: blocks, home: true}
		g.emit()
		out[home+"Cook.cs"] = g.assemble()
	}
	return out, nil
}

// ---- the unit's cook surface ----

// cookUnit is one unit's whole cook read surface: every table that gets an
// Open, the layout of every record their closures reach, and — for the tables
// that get none — the reason.
type cookUnit struct {
	tables  []*ir.Struct                // every table with a C# Open, sorted by name
	members map[string]*ir.MemberLayout // every record the cook closure reaches
	order   []string                    // those record names, sorted
	skipped map[string]string           // table -> why it has no C# Open
	align   int64                       // the unit's region alignment (§7.1)
}

func (c *cookUnit) opens(name string) bool {
	for _, st := range c.tables {
		if st.Name == name {
			return true
		}
	}
	return false
}

// cookUnitOf computes the surface. A ROOT IS ANY TABLE (§7) and every table
// gets one — with one absence this backend states rather than hides: a closure
// carrying a UNION has no blittable C# spelling under §19.3's Sequential rule,
// which is the same reason a union keeps a table out of the block form, and it
// is a named follow-on rather than a refusal.
func cookUnitOf(u *ir.Unit) *cookUnit {
	c := &cookUnit{members: map[string]*ir.MemberLayout{}, skipped: map[string]string{}}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if why := cookableClosure(u, st); why != "" {
			c.skipped[name] = why
			continue
		}
		c.tables = append(c.tables, st)
	}
	for _, st := range c.tables {
		cookWalk(u, st.Name, func(name string, ml *ir.MemberLayout) {
			c.members[name] = ml
		})
	}
	aligns := make([]int64, 0, len(c.members))
	for name := range c.members {
		c.order = append(c.order, name)
		aligns = append(aligns, c.members[name].Align)
	}
	sort.Strings(c.order)
	c.align = ir.RegionAlignOf(aligns...)
	return c
}

// cookWalk visits every record one table's cooked region can hold: itself,
// everything it nests by value, and everything it POINTS AT — a cook's region
// is the whole graph, so a pointer edge reaches records a by-value walk never
// would.
func cookWalk(u *ir.Unit, name string, visit func(string, *ir.MemberLayout)) {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		if seen[n] {
			return
		}
		st := cookMember(u, n)
		if st == nil {
			return
		}
		seen[n] = true
		visit(n, ir.RecordLayout(u, st))
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	walk(name)
}

// cookableClosure answers whether C# can spell one table's whole cooked region
// blittably, and why not when it cannot.
func cookableClosure(u *ir.Unit, st *ir.Struct) string {
	seen := map[string]bool{}
	var walk func(string) string
	walk = func(name string) string {
		if seen[name] {
			return ""
		}
		seen[name] = true
		member := cookMember(u, name)
		if member == nil {
			return ""
		}
		for _, f := range member.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return name + "." + f.Name + " is a union, and a cooked record's C# form is Sequential with generated padding, which cannot overlay arms"
			case *ir.Struct:
				if why := walk(ref.Name); why != "" {
					return why
				}
			}
		}
		return ""
	}
	return walk(st.Name)
}

func cookMember(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// ---- the emitter ----

type cookGen struct {
	unit    *ir.Unit
	file    *ir.File
	cook    *cookUnit
	blocks  *ir.BlockUnit
	home    bool
	runtime strings.Builder
	structs strings.Builder
	handles strings.Builder
}

func (g *cookGen) rf(format string, args ...any) { fmt.Fprintf(&g.runtime, format, args...) }
func (g *cookGen) sf(format string, args ...any) { fmt.Fprintf(&g.structs, format, args...) }
func (g *cookGen) hf(format string, args ...any) { fmt.Fprintf(&g.handles, format, args...) }

// blockHasRecord reports whether the BLOCK form already emits <Name>Row for
// this unit, in which case the cook uses that one rather than emitting a
// second: one declaration cannot be two types, and a cooked record IS the
// blittable row.
func (g *cookGen) blockHasRecord(name string) bool {
	return anyBlockForm(g.unit, g.blocks) && g.blocks.Layout(name) != nil
}

// blockOwnsRuntime reports whether the unit's BLOCK form already emits the
// shared constants — BuildVersion above all. The two accelerators share one
// `Schema` partial class, so exactly one of them defines each constant.
func (g *cookGen) blockOwnsRuntime() bool {
	return anyBlockForm(g.unit, g.blocks)
}

func (g *cookGen) emit() {
	if g.home {
		g.rf("%s", cookRuntime(ir.BuildVersion(g.unit), !g.blockOwnsRuntime()))
		g.emitLayoutCheck()
		for _, name := range g.cook.order {
			if g.blockHasRecord(name) {
				continue
			}
			g.emitBlittable(name)
		}
	}
	if g.file == nil {
		return // the runtime home the unit has no file for declares nothing
	}
	for _, st := range g.file.Tables {
		if g.cook.opens(st.Name) {
			g.emitCookHandle(st)
			continue
		}
		g.hf("// table %s has NO C# cook Open: %s (docs/SPEC-TABLES.md §7, §19.3).\n", st.Name, g.cook.skipped[st.Name])
		g.hf("// Its wire (§3) and its cook are unaffected — only this backend's reader is\n")
		g.hf("// absent, and it is absent by construction rather than by refusal.\n\n")
	}
}

func (g *cookGen) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file, g.unit))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the COOKED FORM (docs/SPEC-TABLES.md §7): the READ half.\n", g.unit.Package)
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Cook.cs, one home per unit, named by\n")
		h.WriteString("// the package and independent of file order (§19.2).\n")
	}
	h.WriteString("//\n")
	h.WriteString("// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format and not a wire. Tooling writes\n")
	h.WriteString("// a region for one BUILD VERSION and that build points at it: Open matches the\n")
	h.WriteString("// header and returns the root where it lies. There is no walk, no fix-up and no\n")
	h.WriteString("// allocation, which is what makes Open O(1) in the file's size.\n")
	h.WriteString("//\n")
	h.WriteString("// A COOK IS TRUSTED INPUT, LOADED FROM DISK. Open's checks are IDENTITY checks —\n")
	h.WriteString("// is this file for THIS build — and not a trust boundary: there is NO PER-NODE\n")
	h.WriteString("// VALIDATION AT LOAD, ever. A file whose provenance you doubt is `schema\n")
	h.WriteString("// cook-check`'s business, run by a person, once, offline.\n")
	h.WriteString("//\n")
	h.WriteString("// THE MEMORY IS THE CONSUMER'S, and it must stay put and stay aligned for as long\n")
	h.WriteString("// as the handle lives: an mmap, a NativeArray, or an array the consumer pinned.\n")
	h.WriteString("// This side takes a pointer and a length and points — the block form's contract\n")
	h.WriteString("// (§19.2), for the same reason.\n")
	h.WriteString("//\n")
	h.WriteString("// It is UNSAFE by nature, not by taste. A project that compiles this file sets\n")
	h.WriteString("// AllowUnsafeBlocks.\n")
	h.WriteString("//\n")
	h.WriteString("// THIS FILE AND <Base>Block.cs ARE A PAIR: they share one set of blittable\n")
	h.WriteString("// <Name>Row records, because a cooked record IS the blittable row. Compile both\n")
	h.WriteString("// or neither — one without the other leaves those records undefined.\n")
	h.WriteString("//\n")
	h.WriteString("// Every size and offset below is the MANAGED unmanaged-struct model — what Span\n")
	h.WriteString("// and pointer arithmetic index with — never the interop marshalling model\n")
	h.WriteString("// (§19.3). A bool in a cooked record is ONE byte.\n\n")
	h.WriteString("using System;\n")
	h.WriteString("using System.Runtime.CompilerServices;\n")
	h.WriteString("using System.Runtime.InteropServices;\n\n")
	fmt.Fprintf(&h, "namespace %s\n{\n\n", capitalize(g.unit.Package))
	if g.runtime.Len() > 0 {
		h.WriteString(indent4(g.runtime.String()))
	}
	if g.structs.Len() > 0 {
		var b strings.Builder
		b.WriteString("// The BLITTABLE records a cooked region is laid out from: the C ABI layout\n")
		b.WriteString("// §20.3 commits the compiler to, with GENERATED PADDING FIELDS wherever it has\n")
		b.WriteString("// interior padding and a Size that pins the trailing padding — both are needed\n")
		b.WriteString("// (docs/SPEC-TABLES.md §19.3). A cooked record IS the blittable row, so these are\n")
		b.WriteString("// the same <Name>Row structs the block form spells, from the same model; a\n")
		b.WriteString("// record the block form already emits is emitted THERE and not again here.\n")
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

func (g *cookGen) emitBlittable(name string) {
	ml := g.cook.members[name]
	if ml == nil {
		return
	}
	g.sf("// %s — a cooked record. `Row` is a CLAIMED suffix (docs/SPEC-TABLES.md §11), so no\n", name)
	g.sf("// declaration in the unit can take it.\n")
	g.sf("[StructLayout(LayoutKind.Sequential, Pack = 1, Size = %d)]\n", ml.Size)
	g.sf("public unsafe struct %sRow\n{\n", name)
	w := &cookWriter{g: g}
	for _, fl := range ml.Fields {
		w.pieces = ir.FieldPieces(g.unit, fl.Field, fl.Offset)
		w.piece = 0
		g.emitBlittableField(fl.Field, w)
	}
	w.pad(ml.Size)
	g.sf("}\n\n")
}

// cookWriter lays a record out piece by piece, padding to the offset the
// compiler's model gives each one. A field is not always one piece (§7.2's
// storage-piece table), and a port that padded only BETWEEN fields would slide
// every field after a `string(N)`.
type cookWriter struct {
	g      *cookGen
	at     int64
	pieces []ir.BlockFieldPiece
	piece  int
}

func (w *cookWriter) pad(to int64) {
	for w.at < to {
		w.g.sf("    private byte _pad%d;\n", w.at)
		w.at++
	}
}

func (w *cookWriter) next() {
	if w.piece >= len(w.pieces) {
		return
	}
	p := w.pieces[w.piece]
	w.pad(p.Offset)
	w.at = p.Offset + p.Size
	w.piece++
}

func (g *cookGen) emitBlittableField(f *ir.Field, w *cookWriter) {
	name := ir.GoExportName(f.Name)
	next := w.next
	switch {
	case f.Type.Pointer:
		// A *T SLOT IS EIGHT BYTES AT EIGHT (docs/SPEC-TABLES.md §6.3, §7.2),
		// holding the SIGNED SELF-RELATIVE delta from the slot's own address,
		// and NULL IS ZERO. It is not a managed reference and never becomes
		// one: <T>Cook.At is the one add that resolves it.
		next()
		g.sf("    public long %s; // *%s: signed self-relative delta, zero is null (§6.3)\n", name, f.Type.Name)
	case f.Type.Kind == ir.TString:
		next()
		g.sf("    public fixed byte %s[%d]; // string(%d): buffer, used length beside it\n", name, f.Type.Size+1, f.Type.Size)
		next()
		g.sf("    public int %sLength;\n", name)
	case f.Type.Kind == ir.TBytes:
		next()
		g.sf("    public fixed byte %s[%d]; // bytes(%d): buffer, used length beside it\n", name, f.Type.Size, f.Type.Size)
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

// emitBlittableArray spells an INLINE array — every array in a cooked record is
// inline, because a cook writes the by-value form verbatim (§7). A COUNTED
// array writes all N slots (§7.2), so the storage is N wide whatever the count.
func (g *cookGen) emitBlittableArray(f *ir.Field, name string) {
	typ := g.cookBlittableType(f.Type)
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

// cookBlittableType is blittableType with one difference: a pointer field's
// storage is the delta slot, never the target's record.
func (g *cookGen) cookBlittableType(t ir.FieldType) string {
	if t.Pointer {
		return "long"
	}
	return g.blittableType(t)
}

func (g *cookGen) blittableType(t ir.FieldType) string { return csBlittableType(g.unit, t) }

// ---- the layout check (docs/SPEC-TABLES.md §19.3, §20.3) ----

// emitLayoutCheck is §20.3's C# half for the COOK closure: the compiler's
// layout model committed to every cookable record, asserted against THIS
// runtime's own. C# has no static_assert, so the check runs once at type
// initialization and THROWS naming the type, the field, the offset it found
// and the offset the compiler's model says — loud and early, but a first-use
// failure and not a compile-time one.
func (g *cookGen) emitLayoutCheck() {
	g.rf("// THE LAYOUT CONTRACT for the cook closure (docs/SPEC-TABLES.md §20.3), run ONCE:\n")
	g.rf("// a cooked region is laid out by the compiler's C ABI model, so a runtime that\n")
	g.rf("// lays one of these records out differently would read a cook at the wrong\n")
	g.rf("// offsets and never know. C++ says this with static_assert at compile time;\n")
	g.rf("// C# has none, so it says it at type initialization and throws.\n")
	g.rf("//\n")
	g.rf("// Asserted against the MANAGED model — Unsafe.SizeOf and address arithmetic on\n")
	g.rf("// a stack instance, never Marshal.SizeOf or Marshal.OffsetOf (§19.3).\n")
	g.rf("public static unsafe class TableCookLayout\n{\n")
	g.rf("    private static bool checked_;\n\n")
	g.rf("    public static void Verify()\n    {\n")
	g.rf("        if (checked_) { return; }\n")
	g.rf("        checked_ = true;\n")
	if g.blockOwnsRuntime() {
		g.rf("        // the records the BLOCK form emits are checked by its own half, and a\n")
		g.rf("        // cooked region holds both sets\n")
		g.rf("        TableBlockLayout.Verify();\n")
	}
	for _, name := range g.cook.order {
		g.emitRecordCheck(name)
	}
	g.rf("    }\n\n")
	g.rf("    internal static void Size(string what, int got, int want)\n    {\n")
	g.rf("        if (got != want)\n        {\n")
	g.rf("            throw new InvalidOperationException(\n")
	g.rf("                \"schema cook layout: \" + what + \" is \" + got + \" bytes in this runtime and \" + want +\n")
	g.rf("                \" in the layout model the cook's bytes come from — the two disagree about the bytes (docs/SPEC-TABLES.md §20.3)\");\n")
	g.rf("        }\n    }\n\n")
	g.rf("    internal static void Offset(string what, long got, long want)\n    {\n")
	g.rf("        if (got != want)\n        {\n")
	g.rf("            throw new InvalidOperationException(\n")
	g.rf("                \"schema cook layout: \" + what + \" sits at \" + got + \" in this runtime and \" + want +\n")
	g.rf("                \" in the layout model the cook's bytes come from — the two disagree about the bytes (docs/SPEC-TABLES.md §20.3)\");\n")
	g.rf("        }\n    }\n")
	g.rf("}\n\n")
}

func (g *cookGen) emitRecordCheck(name string) {
	ml := g.cook.members[name]
	if ml == nil {
		return
	}
	spelled := name + "Row"
	g.rf("        {\n")
	g.rf("            %s probe = default;\n", spelled)
	g.rf("            byte* at = (byte*) &probe;\n")
	g.rf("            Size(\"%s\", Unsafe.SizeOf<%s>(), %d);\n", spelled, spelled, ml.Size)
	for _, fl := range ml.Fields {
		g.rf("            Offset(\"%s.%s\", (byte*) %s - at, %d);\n", spelled, ir.GoExportName(fl.Field.Name),
			g.cookFieldAddress(fl.Field), fl.Offset)
	}
	g.rf("        }\n")
}

func (g *cookGen) cookFieldAddress(f *ir.Field) string {
	name := ir.GoExportName(f.Name)
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return "probe." + name // a fixed-size buffer IS a pointer in unsafe context
	}
	if !f.Type.Pointer && (f.KeyEnum != "" || f.Array != ir.ArrayNone) {
		if csFixedBufferPrimitive(g.cookBlittableType(f.Type)) {
			return "probe." + name
		}
		return "&probe." + name + "0"
	}
	return "&probe." + name
}

// ---- the cook handle ----

func (g *cookGen) emitCookHandle(st *ir.Struct) {
	name := st.Name
	ml := g.cook.members[name]
	align := g.cook.align
	g.hf("// %s's cook: a pointer and a length, and then the root where it lies. Opening\n", name)
	g.hf("// one is a HEADER MATCH and no copy; a reference is one add (docs/SPEC-TABLES.md §7).\n")
	g.hf("//\n")
	g.hf("// `Cook` is a CLAIMED suffix (§11). C++ spells the same claimed verbs as free\n")
	g.hf("// functions — %sOpen, %sAt — and C# spells them as MEMBERS of this type, which\n", name, name)
	g.hf("// is the rule the block form already follows for its accessors.\n")
	g.hf("//\n")
	g.hf("// THE MEMORY IS THE CONSUMER'S. Nothing here allocates, nothing here copies and\n")
	g.hf("// nothing here pins: the region must stay put and stay aligned for as long as\n")
	g.hf("// this handle or anything reached through it is used.\n")
	g.hf("public unsafe readonly struct %sCook\n{\n", name)
	g.hf("    private readonly byte* region;      // the DATA part's base: the root sits at offset zero\n")
	g.hf("    private readonly long regionLength; // data_length, as the header framed it\n\n")
	g.hf("    private %sCook(byte* region, long regionLength)\n    {\n", name)
	g.hf("        this.region = region;\n        this.regionLength = regionLength;\n    }\n\n")
	g.hf("    static %sCook()\n    {\n", name)
	g.hf("        // the layout contract, run once before any static member of this type\n")
	g.hf("        TableCookLayout.Verify();\n")
	g.hf("    }\n\n")
	g.hf("    // §7.1's constants, so a consumer reading this file has the facts and not a\n")
	g.hf("    // description of them.\n")
	g.hf("    public const long RegionAlignment = %d; // the greatest alignof in the region, floor eight\n", align)
	g.hf("    public const long RootSize = %d;\n", ml.Size)
	g.hf("    public const long RootAlign = %d;\n\n", ml.Align)
	g.hf("    // The region, and the root at its base. Both are POINTERS and not managed\n")
	g.hf("    // references: a cooked graph is walked by adding deltas to slot addresses,\n")
	g.hf("    // which is what a `ref` cannot carry across a field access.\n")
	g.hf("    public byte* Region { get { return region; } }\n")
	g.hf("    public long RegionLength { get { return regionLength; } }\n")
	g.hf("    public %sRow* RootPointer { get { return (%sRow*) region; } }\n", name, name)
	g.hf("    public ref readonly %sRow Root { get { return ref *(%sRow*) region; } }\n\n", name, name)
	g.emitOpen(st, ml, align)
	g.emitAt(st)
	g.emitAccessors(st, ml)
	g.emitDescriptors(st)
	g.hf("}\n\n")
}

func (g *cookGen) emitOpen(st *ir.Struct, ml *ir.MemberLayout, align int64) {
	name := st.Name
	g.hf("    // Open checks the header and POINTS, and this is the WHOLE check\n")
	g.hf("    // (docs/SPEC-TABLES.md §7): the magic read bytewise, the byte order it\n")
	g.hf("    // establishes, the build version, every RESERVED word zero, the region\n")
	g.hf("    // ALIGNMENT the header names, the two part lengths against the length the\n")
	g.hf("    // caller passed — a truncated file refuses — the ROOT's own storage inside\n")
	g.hf("    // the data part, and the alignment of the base. Nothing per node, ever:\n")
	g.hf("    // that is what makes this O(1) in the file's size.\n")
	g.hf("    //\n")
	g.hf("    // On a match the bytes ARE what this build wrote, in this build's layout and\n")
	g.hf("    // this build's byte order, so there is nothing to validate and nothing to fix\n")
	g.hf("    // up. On any failure it returns false and points at nothing, and the caller\n")
	g.hf("    // falls back to a wire load — the path that carries every version.\n")
	g.hf("    //\n")
	g.hf("    // EVERY NUMBER BELOW COMES OUT OF THE FILE, so all of the arithmetic is\n")
	g.hf("    // UNSIGNED and each term is BOUNDED BEFORE IT IS ADDED. A signed length\n")
	g.hf("    // would put one signed value into that arithmetic and one negative case\n")
	g.hf("    // into every comparison; a caller holding a length from a stat casts once,\n")
	g.hf("    // at the call site, where the sign is still its own business.\n")
	g.hf("    public static bool Open(out %sCook cook, IntPtr pointer, long length)\n    {\n", name)
	g.hf("        cook = default;\n")
	g.hf("        TableCookLayout.Verify();\n")
	g.hf("        if (pointer == IntPtr.Zero || length < %d) { return false; }\n", cookHeaderBytes)
	g.hf("        byte* at = (byte*) pointer;\n")
	g.hf("        ulong bytes = (ulong) length;\n\n")
	g.hf("        // THE MAGIC, read BYTEWISE before anything else: it is what establishes\n")
	g.hf("        // the byte order every other header word is written in. A cook of the\n")
	g.hf("        // other order reads back this constant byte-reversed and refuses HERE,\n")
	g.hf("        // rather than reaching a fix-up pass this design does not have.\n")
	g.hf("        if (Schema.TableCookRead64(at + %d) != Schema.TableCookMagic) { return false; }\n", 0)
	g.hf("        // and the ORDER WORD does the other job: it RECORDS which order wrote the\n")
	g.hf("        // file, so a refusal names the order rather than inferring it. A file\n")
	g.hf("        // whose magic matched and whose order word did not is corrupt, and there\n")
	g.hf("        // is no reading that recovers it.\n")
	g.hf("        if (Schema.TableCookRead64(at + %d) != Schema.TableCookByteOrder) { return false; }\n", 16)
	g.hf("        // THE BUILD VERSION: under the match-and-point rule a matching id means\n")
	g.hf("        // Open checks nothing further, so it is the sole guard between this\n")
	g.hf("        // runtime and a foreign region (§20).\n")
	g.hf("        if (Schema.TableCookRead64(at + %d) != Schema.BuildVersion) { return false; }\n", 8)
	g.hf("        // THE RESERVED WORDS: a non-zero one means a writer used a form this\n")
	g.hf("        // build does not understand, and Open refuses rather than ignoring it.\n")
	g.hf("        if (Schema.TableCookRead64(at + %d) != 0) { return false; }\n", 48)
	g.hf("        if (Schema.TableCookRead64(at + %d) != 0) { return false; }\n\n", 56)
	g.hf("        // THE ALIGNMENT WORD is the one field the check COMPUTES WITH rather than\n")
	g.hf("        // only compares against — the data part begins at align_up(64, alignment)\n")
	g.hf("        // and the base is measured against it — so a word that is not an\n")
	g.hf("        // alignment rounds nothing and aligns nothing. A zero there is a division\n")
	g.hf("        // by zero inside the check, which is the defect the check prevents.\n")
	g.hf("        ulong alignment = Schema.TableCookRead64(at + %d);\n", 40)
	g.hf("        if (alignment < %d || alignment > %d) { return false; }\n", ir.RegionAlignFloor, cookMaxAlign)
	g.hf("        if ((alignment & (alignment - 1)) != 0) { return false; } // a power of two\n")
	g.hf("        if ((alignment %% %d) != 0) { return false; }             // and a multiple of the ROOT's own alignof\n\n", ml.Align)
	g.hf("        // THE DATA OFFSET IS DERIVED, never a header field: a fact a reader\n")
	g.hf("        // computes is a fact two writers cannot disagree about, and it is 64 for\n")
	g.hf("        // every unit this language can declare.\n")
	g.hf("        ulong dataOffset = ((ulong) %d + alignment - 1) & ~(alignment - 1);\n\n", cookHeaderBytes)
	g.hf("        // THE TWO PART LENGTHS against the length the caller passed. The whole\n")
	g.hf("        // file is dataOffset + data + attribution, and a size that is not exactly\n")
	g.hf("        // that refuses: a truncated file and a file with trailing bytes are the\n")
	g.hf("        // same refusal. Each term is bounded before it is added, so nothing here\n")
	g.hf("        // can wrap past the top of the type and land back inside the buffer.\n")
	g.hf("        ulong dataLength = Schema.TableCookRead64(at + %d);\n", 24)
	g.hf("        ulong attribution = Schema.TableCookRead64(at + %d);\n", 32)
	g.hf("        if (dataLength > bytes || attribution > bytes - dataLength) { return false; }\n")
	g.hf("        if (dataOffset > bytes - dataLength - attribution) { return false; }\n")
	g.hf("        if (dataOffset + dataLength + attribution != bytes) { return false; }\n\n")
	g.hf("        // THE DATA PART MUST HOLD THE ROOT. The part lengths frame the FILE; they\n")
	g.hf("        // do not say the region is at least sizeof(root). Without this a forged\n")
	g.hf("        // short data part describes a root partly outside the file, and a\n")
	g.hf("        // match-and-point reader would hand back storage the caller never gave\n")
	g.hf("        // it — the one way this design could read past the length it was passed.\n")
	g.hf("        if (dataLength < %d) { return false; }\n\n", ml.Size)
	g.hf("        // THE ALIGNMENT OF THE BASE. The header pads the data part to the\n")
	g.hf("        // region's alignment, so a base an allocator or mmap gave you is already\n")
	g.hf("        // aligned; one that is not is a caller's buffer this form cannot be read\n")
	g.hf("        // out of. The alignment divides 64, so the derived data offset carries\n")
	g.hf("        // the property from the file's base to the region's.\n")
	g.hf("        if ((((ulong) at) %% alignment) != 0) { return false; }\n\n")
	g.hf("        cook = new %sCook(at + dataOffset, (long) dataLength);\n", name)
	g.hf("        return true;\n    }\n\n")
	g.hf("    // The same open over a SPAN, which is how a consumer that already has the\n")
	g.hf("    // bytes in hand spells it. The contract is the pointer form's and is not\n")
	g.hf("    // softened by the spelling: the span must be over memory the CONSUMER keeps\n")
	g.hf("    // fixed — native memory, a pinned array, or a `fixed` block that encloses\n")
	g.hf("    // every use of the handle — because the handle outlives this call and the\n")
	g.hf("    // pin below does not.\n")
	g.hf("    //\n")
	g.hf("    // A span's length is an int, so this overload reaches 2 GiB and the POINTER\n")
	g.hf("    // FORM is the one with the reach the cook is built for (§6.3): a catalogue\n")
	g.hf("    // past that ceiling is opened through the pointer form, never through this.\n")
	g.hf("    public static bool Open(out %sCook cook, ReadOnlySpan<byte> bytes)\n    {\n", name)
	g.hf("        fixed (byte* p = bytes)\n        {\n")
	g.hf("            return Open(out cook, (IntPtr) p, bytes.Length);\n")
	g.hf("        }\n    }\n\n")
}

func (g *cookGen) emitAt(st *ir.Struct) {
	name := st.Name
	g.hf("    // A REFERENCE IS DEREFERENCED THROUGH At, and it is the same call in a locked\n")
	g.hf("    // region and an opened cook because they are the same encoding (§6.3): the\n")
	g.hf("    // slot is eight bytes, SIGNED, self-relative from the SLOT'S OWN ADDRESS, so\n")
	g.hf("    // a deref needs no base pointer and no bounds test, and NULL IS A DELTA OF\n")
	g.hf("    // ZERO. Nothing about this call is the cook's: it is what a region reference\n")
	g.hf("    // is, and a cook is a region written verbatim.\n")
	g.hf("    //\n")
	g.hf("    // It takes the SLOT and not its value, because a self-relative delta means\n")
	g.hf("    // nothing without the address it is relative to.\n")
	g.hf("    public static %sRow* At(long* slot)\n    {\n", name)
	g.hf("        long delta = *slot;\n")
	g.hf("        if (delta == 0) { return null; }\n")
	g.hf("        return (%sRow*) ((byte*) slot + delta);\n", name)
	g.hf("    }\n\n")
}

// emitAccessors spells the pieces a consumer reads THROUGH rather than at: a
// string's used bytes, a `bytes` field's used bytes, and an array's live slots
// — each a span over the region, which costs no copy and no allocation.
//
// They are STATIC and take the row, because a cooked record is shared with the
// block form and neither accelerator adds members to the other's structs.
func (g *cookGen) emitAccessors(st *ir.Struct, ml *ir.MemberLayout) {
	name := st.Name
	wrote := false
	for _, fl := range ml.Fields {
		f := fl.Field
		member := ir.GoExportName(f.Name)
		if !wrote {
			g.hf("    // The pieces a consumer reads THROUGH rather than at: each is a SPAN over\n")
			g.hf("    // the region itself, so reading one copies nothing and allocates nothing.\n")
			g.hf("    // A span's lifetime is the REGION's, which is the consumer's to keep.\n")
			wrote = true
		}
		switch {
		case f.Type.Pointer:
			continue
		case f.Type.Kind == ir.TString:
			g.hf("    // string(%d): the used bytes, without the zero tail (§7.2).\n", f.Type.Size)
			g.hf("    public static ReadOnlySpan<byte> %s(%sRow* row)\n    {\n", member, name)
			g.hf("        int used = row->%sLength;\n", member)
			g.hf("        if (used < 0 || used > %d) { used = 0; } // a companion outside its bound is cook-check's refusal, not a read\n", f.Type.Size)
			g.hf("        return new ReadOnlySpan<byte>(row->%s, used);\n", member)
			g.hf("    }\n\n")
		case f.Type.Kind == ir.TBytes:
			g.hf("    // bytes(%d): the used bytes.\n", f.Type.Size)
			g.hf("    public static ReadOnlySpan<byte> %s(%sRow* row)\n    {\n", member, name)
			g.hf("        int used = row->%sLength;\n", member)
			g.hf("        if (used < 0 || used > %d) { used = 0; }\n", f.Type.Size)
			g.hf("        return new ReadOnlySpan<byte>(row->%s, used);\n", member)
			g.hf("    }\n\n")
		case f.KeyEnum != "" || f.Array != ir.ArrayNone:
			elem := g.cookBlittableType(f.Type)
			g.hf("    public static ReadOnlySpan<%s> %s(%sRow* row)\n    {\n", elem, member, name)
			if f.Array == ir.ArrayCounted {
				g.hf("        int used = row->%sCount;\n", member)
				g.hf("        if (used < 0 || used > %d) { used = 0; }\n", f.ArrayBound)
			} else {
				// a fixed [N]T and an enum-keyed [E]T are complete by
				// construction: every slot is written, slot 0 included (§7.2)
				g.hf("        int used = %d;\n", f.ArrayBound)
			}
			g.hf("        return new ReadOnlySpan<%s>(%s, used);\n", elem, g.arrayBase(f, member))
			g.hf("    }\n\n")
		}
	}
}

func (g *cookGen) arrayBase(f *ir.Field, member string) string {
	if csFixedBufferPrimitive(g.cookBlittableType(f.Type)) {
		return "row->" + member
	}
	return "&row->" + member + "0"
}

// ---- the descriptors ----

// emitDescriptors is the cook's reflective half: one record's layout as DATA,
// so a consumer — or a gate — walks a cooked region without a hand-written
// struct per table and with nothing to maintain when a field is added. It is
// the same mechanism §8 gives the wire and §19.2 gives the block, over the
// facts a cooked region actually has: an offset, a size, a pointer edge, a
// count companion, and the record a field names.
func (g *cookGen) emitDescriptors(st *ir.Struct) {
	name := st.Name
	records := g.descriptorRecords(st)
	g.hf("    // this root's cook descriptors: constant data, so a reflective walk costs a\n")
	g.hf("    // lookup and not a parse. Every record the region can hold hangs off the\n")
	g.hf("    // field column, so a walker reaches the whole graph from the root.\n")
	for _, r := range records {
		g.emitRecordDescriptor(r)
	}
	g.hf("    public static TableCookInfo Type { get { return %s; } }\n", cookInfoSymbol(name))
}

func (g *cookGen) descriptorRecords(st *ir.Struct) []string {
	var out []string
	cookWalk(g.unit, st.Name, func(n string, _ *ir.MemberLayout) { out = append(out, n) })
	sort.Strings(out)
	return out
}

func (g *cookGen) emitRecordDescriptor(record string) {
	ml := g.cook.members[record]
	if ml == nil {
		return
	}
	g.hf("    private static readonly TableCookInfo %s = new TableCookInfo\n    {\n", cookInfoSymbol(record))
	g.hf("        Name = %q, Size = %d, Align = %d, NumFields = %d,\n", record, ml.Size, ml.Align, len(ml.Fields))
	g.hf("        Fields = new TableCookFieldInfo[]\n        {\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		names := "null"
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				names = fmt.Sprintf("delegate { return %s; }", cookInfoSymbol(ref.Name))
			}
		}
		countOffset := int64(-1)
		if f.Array == ir.ArrayCounted {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			countOffset = pieces[len(pieces)-1].Offset
			if f.Type.Optional {
				countOffset = pieces[len(pieces)-2].Offset
			}
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			countOffset = pieces[1].Offset
		}
		isArray := f.KeyEnum != "" || f.Array != ir.ArrayNone
		bound := int64(1)
		elemSize := fl.Size
		if isArray {
			bound = f.ArrayBound
			elemSize = ir.FieldPieces(g.unit, f, fl.Offset)[0].Size / bound
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
			// the COUNT COMPANION of a string or a `bytes` bounds a walker just
			// as an array's does (§7.4), and the bound is the declared length
			bound = f.Type.Size
			isArray = false
		}
		presentOffset := int64(-1)
		if f.Type.Optional {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			presentOffset = pieces[len(pieces)-1].Offset
		}
		g.hf("            new TableCookFieldInfo { Name = %q, Offset = %d, Size = %d, ElemSize = %d, IsArray = %t, ArrayBound = %d, IsPointer = %t, CountOffset = %d, PresentOffset = %d, Storage = TableCookStorage.%s, RecordRef = %s },\n",
			f.Name, fl.Offset, fl.Size, elemSize, isArray, bound, f.Type.Pointer, countOffset, presentOffset,
			cookStorageKind(f), names)
	}
	g.hf("        },\n    };\n\n")
}

func cookInfoSymbol(record string) string { return "cookRecord" + record }

// cookStorageKind is what a cooked SLOT holds, which is not always what the
// wire carries: an ENUM slot holds the ORDINAL at the enum's own derived
// storage width (§7.2), where the wire carries the variant-name hash. So the
// descriptors name the storage rather than reuse a wire kind, and a walker
// reads a slot with the width `ElemSize` gives and the signedness this names.
func cookStorageKind(f *ir.Field) string {
	if f.Type.Pointer {
		return "Reference"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "Bool"
	case ir.TFloat32, ir.TFloat64:
		return "Float"
	case ir.TString:
		return "String"
	case ir.TBytes:
		return "Bytes"
	case ir.TBits:
		return "Unsigned"
	case ir.TInt:
		if f.Type.Signed {
			return "Signed"
		}
		return "Unsigned"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "Unsigned"
		case *ir.Struct:
			return "Record"
		}
	}
	return "Record"
}

// ---- the shared runtime ----

// cookRuntime is the unit's shared cook runtime, emitted once into the cook
// home's <Base>Cook.cs. `buildVersion` rides here only when the unit has no
// BLOCK form to carry it: `Schema` is one partial class across a unit's files,
// so exactly one accelerator defines each constant (docs/SPEC-TABLES.md §20.7).
func cookRuntime(buildVersion uint64, withBuildVersion bool) string {
	var b strings.Builder
	b.WriteString(`// What a cooked SLOT holds, which is not always what the WIRE carries: an
// ENUM slot holds the ORDINAL at the enum's own derived storage width
// (docs/SPEC-TABLES.md §7.2), where the wire rides the variant-name hash. A walker
// reads a slot with the width ElemSize gives and the signedness this names.
public enum TableCookStorage
{
    Record,    // a nested record, or an array of them: descend through it
    Reference, // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    Bool,
    Signed,
    Unsigned,  // an unsigned integer, an enum ordinal, a bits(N), a flags mask
    Float,
    String,    // char[N + 1] with an int32 used length beside it
    Bytes,     // uint8[N] with an int32 used length beside it
}

// One record's layout as DATA — the mechanism behind a reflective read of a
// cooked region, and what a gate walks a whole graph with. A field carries the
// facts a region actually has: where it sits, how big it is, whether it is a
// POINTER EDGE, the bound its COUNT COMPANION is checked against, and the
// record it names.
public sealed class TableCookFieldInfo
{
    public string Name;
    public int Offset;      // the field's offset in the record this descriptor describes
    public int Size;        // its whole storage there, companions included
    public int ElemSize;    // one element's size, for an array; the field's own otherwise
    public bool IsArray;
    public int ArrayBound;  // the DECLARED bound: a counted array's N, a string's or bytes' length
    public bool IsPointer;  // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    public int CountOffset; // the count companion's offset, or -1 when the field has none
    public int PresentOffset; // an optional's presence bool, or -1
    public TableCookStorage Storage; // what one slot HOLDS, at ElemSize bytes
    // the record this field NAMES, behind a delegate so the table stays
    // constructible in any order. null when the field is a scalar. Following it
    // is how a walker DESCENDS — a pointer's target, a by-value nesting, and an
    // array's element are all reached through this one column.
    public Func<TableCookInfo> RecordRef;
    public TableCookInfo Record
    {
        get { return RecordRef == null ? null : RecordRef(); }
    }
}

// One cooked record's layout as DATA.
public sealed class TableCookInfo
{
    public string Name;
    public int Size;
    public int Align;
    public int NumFields;
    public TableCookFieldInfo[] Fields;
}

// Schema carries every generated function and constant of the unit — C# has no
// namespace-level functions — and this is the cook's slice of it.
public static partial class Schema
{
`)
	if withBuildVersion {
		b.WriteString(`    // THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the
    // bytes this build produces depend on. It both ADDRESSES a cooked artifact
    // — the store is keyed by (asset hash, build version) — and REFUSES one,
    // because it is what Open checks out of the header.
    //
    // There are TWO ids in the design and they are not interchangeable: the
    // PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
    // what everything cooked or blocked is keyed by.
    public const ulong BuildVersion = ` + fmt.Sprintf("0x%016xUL", buildVersion) + `;

`)
	}
	b.WriteString(`    // The cook's MAGIC (docs/SPEC-TABLES.md §7.1), read BYTEWISE before anything
    // else: it is what establishes the byte order every other header word is
    // written in, and it is also what separates a COOK from a BLOCK — the two
    // accelerators carry the same build version and different magics, because a
    // form's identity belongs in its magic rather than in a second digest.
    //
    // The value is "SCHMCOOK" read as ASCII in the byte order a little-endian
    // store produces, so a hex dump of a little-endian cook is legible.
    public const ulong TableCookMagic = ` + fmt.Sprintf("0x%016xUL", cookMagic) + `;

    // THIS BUILD's byte order, as §7.1's order word records it. The byte order
    // is settled AT COOK TIME for the target build, so a matching build version
    // already means a matching order and Open never fixes anything up; a cook
    // whose recorded order is not this build's is simply not this build's file.
    public static ulong TableCookByteOrder
    {
        get { return BitConverter.IsLittleEndian ? 1UL : 2UL; }
    }

    // §7.1's header is 64 bytes of u64 words, and the DATA part begins at
    // align_up(64, alignment) — DERIVED and not a header field, because a fact
    // a reader computes is a fact two writers cannot disagree about.
    public const long TableCookHeaderBytes = ` + fmt.Sprintf("%d", cookHeaderBytes) + `;

    // The ceiling on the header's alignment word: the same sixty-four a block's
    // base takes (§19.1), past which the derived data offset would no longer be
    // the 64 every unit this language can declare produces.
    public const long TableCookMaxAlign = ` + fmt.Sprintf("%d", cookMaxAlign) + `;

    public static unsafe ulong TableCookRead64(byte* p) { return *(ulong*) p; }
}

`)
	return b.String()
}
