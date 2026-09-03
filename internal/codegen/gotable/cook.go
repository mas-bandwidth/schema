// The COOKED FORM in Go (docs/SPEC-TABLES.md §7): the READ half, emitted ON
// THE SIDE into <Base>Cook.go.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format and not a wire. Tooling
// writes a region for one BUILD VERSION and that build points at it: Open
// matches the header and returns the root where it lies. There is no walk, no
// fix-up and no allocation, which is what makes Open O(1) in the file's size.
//
// It reaches FURTHER than the wire half does, and that is the design rather
// than an exception to it: a cook is POINTED AT, not parsed, so it needs not
// one line of the codec the variable class is missing. A pointered unit gets no
// <Base>Table.go and its cooked assets still open in full (§11).
//
// THE MEMORY IS THE CONSUMER'S, and it must stay put and stay aligned for as
// long as the handle lives: an mmap, or a []byte the consumer keeps. This side
// takes a pointer and a length and points.
package gotable

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The cook's MAGIC (docs/SPEC-TABLES.md §7.1). The value is "SCHMCOOK" read as
// ASCII in the byte order a little-endian store produces.
const cookMagic = uint64(0x4B4F4F434D484353)

// §7.1's header is 64 bytes of u64 words, and the DATA part begins at
// align_up(64, alignment) — DERIVED and not a header field.
const cookHeaderBytes = int64(64)

// The ceiling on the header's alignment word: the same sixty-four a block's
// base takes (§19.1).
const cookMaxAlign = int64(64)

// generateCookFiles emits <Base>Cook.go for every file of a unit that declares
// a table.
func generateCookFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(u.Tables) == 0 {
		return out, nil
	}
	ck := cookUnitOf(u)
	if len(ck.tables) == 0 && len(ck.skipped) == 0 {
		return out, nil
	}
	// THE COOK HOME is the first file, by declaration order, that declares a
	// table this backend can open — never the protocol id's home, which may
	// declare no table at all. The shared cook runtime and every blittable
	// record the BLOCK form does not already emit land there, once per unit: a
	// Go package is one namespace across its files, so "emitted once,
	// anywhere" is the whole requirement.
	home := ck.home(u)
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &cookGen{unit: u, file: f, cook: ck, blocks: blocks, home: f.Base == home,
			wroteDescriptor: map[string]bool{}}
		g.emit()
		src, err := g.assemble()
		if err != nil {
			return nil, err
		}
		out[f.Base+"Cook.go"] = src
	}
	return out, nil
}

// ---- the unit's cook surface ----

// cookUnit is one unit's whole cook read surface: every table that gets an
// Open, the layout of every record their closures reach, and — for the tables
// that get none — the reason.
type cookUnit struct {
	tables  []*ir.Struct                // every table with a Go Open, sorted by name
	members map[string]*ir.MemberLayout // every record the cook closure reaches
	order   []string                    // those record names, sorted
	skipped map[string]string           // table -> why it has no Go Open
	align   int64                       // the unit's region alignment (§7.1)
}

func (c *cookUnit) home(u *ir.Unit) string {
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if c.opens(st.Name) {
				return f.Base
			}
		}
	}
	for _, f := range u.Files {
		if len(f.Tables) > 0 {
			return f.Base
		}
	}
	return ""
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
// carrying a UNION has no blittable spelling under the C ABI record rule the
// accelerators are pinned to (§19.3), which is the same reason a union keeps a
// table out of the block form, and it is a named follow-on rather than a
// refusal.
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

// cookableClosure answers whether Go can spell one table's whole cooked region
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
				return name + "." + f.Name + " is a union, and a cooked record's blittable form is a C ABI record with generated padding, which cannot overlay arms"
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

	// a unit's roots share records, and one package cannot declare a var twice
	wroteDescriptor map[string]bool
	// the Record columns, linked in init() because a cooked graph is cyclic
	links []string

	needsFmt    bool
	needsUnsafe bool
}

func (g *cookGen) rf(format string, args ...any) { fmt.Fprintf(&g.runtime, format, args...) }
func (g *cookGen) sf(format string, args ...any) { fmt.Fprintf(&g.structs, format, args...) }
func (g *cookGen) hf(format string, args ...any) { fmt.Fprintf(&g.handles, format, args...) }

// blockHasRecord reports whether the BLOCK form already emits <Name>Row for
// this unit, in which case the cook uses that one rather than emitting a
// second: one declaration cannot be two types, and a cooked record IS the
// blittable row.
func (g *cookGen) blockHasRecord(name string) bool {
	return g.blocks != nil && blockHome(g.unit, g.blocks) != "" && g.blocks.Layout(name) != nil
}

// blockOwnsRuntime reports whether the unit's BLOCK form already emits the
// shared constants — BuildVersion above all. The two accelerators share one
// package, so exactly one of them defines each name.
func (g *cookGen) blockOwnsRuntime() bool {
	return g.blocks != nil && blockHome(g.unit, g.blocks) != ""
}

func (g *cookGen) emit() {
	if g.home {
		g.needsUnsafe = true
		g.rf("%s", cookRuntime(ir.BuildVersion(g.unit), !g.blockOwnsRuntime()))
		for _, name := range g.cook.order {
			if g.blockHasRecord(name) {
				continue
			}
			g.emitBlittable(name)
		}
		g.emitLayoutCheck()
		// EVERY cooked record's descriptor, here and nowhere else: a unit's
		// roots share records, and one package cannot declare a var twice.
		g.emitAllDescriptors()
	}
	for _, st := range g.file.Tables {
		if g.cook.opens(st.Name) {
			g.emitCookHandle(st)
			continue
		}
		g.hf("// table %s has NO Go cook Open: %s (docs/SPEC-TABLES.md §7, §19.3).\n", st.Name, g.cook.skipped[st.Name])
		g.hf("// Its wire (§3) and its cook are unaffected — only this backend's reader is\n")
		g.hf("// absent, and it is absent by construction rather than by refusal.\n\n")
	}
}

func (g *cookGen) assemble() ([]byte, error) {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the COOKED FORM (docs/SPEC-TABLES.md §7): the READ half.\n", g.unit.Package)
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
	h.WriteString("// as the handle lives: an mmap, or a []byte the consumer keeps. This side takes\n")
	h.WriteString("// a pointer and a length and points — the block form's contract (§19.2), for the\n")
	h.WriteString("// same reason.\n")
	h.WriteString("//\n")
	h.WriteString("// THIS FILE AND <Base>Block.go ARE A PAIR: they share one set of blittable\n")
	h.WriteString("// <Name>Row records, because a cooked record IS the blittable row. Compile both\n")
	h.WriteString("// or neither — one without the other leaves those records undefined.\n\n")
	fmt.Fprintf(&h, "package %s\n\n", g.unit.Package)
	if g.needsFmt || g.needsUnsafe {
		h.WriteString("import (\n")
		if g.needsFmt {
			h.WriteString("\t\"fmt\"\n")
		}
		if g.needsUnsafe {
			h.WriteString("\t\"unsafe\"\n")
		}
		h.WriteString(")\n\n")
	}
	h.WriteString(g.runtime.String())
	if g.structs.Len() > 0 {
		h.WriteString("// The BLITTABLE records a cooked region is laid out from: the C ABI layout\n")
		h.WriteString("// §20.3 commits the compiler to, with GENERATED PADDING FIELDS wherever it has\n")
		h.WriteString("// interior padding (docs/SPEC-TABLES.md §19.3). A cooked record IS the\n")
		h.WriteString("// blittable row, so these are the same <Name>Row structs the block form spells,\n")
		h.WriteString("// from the same model; a record the block form already emits is emitted THERE\n")
		h.WriteString("// and not again here.\n")
		h.WriteString(g.structs.String())
		h.WriteString("\n")
	}
	h.WriteString(g.handles.String())
	src, err := format.Source([]byte(h.String()))
	if err != nil {
		return nil, fmt.Errorf("generated Go for %sCook.go does not parse — a compiler bug, not a schema error: %w", g.file.Base, err)
	}
	return src, nil
}

// ---- the blittable records ----

func (g *cookGen) emitBlittable(name string) {
	ml := g.cook.members[name]
	if ml == nil {
		return
	}
	g.sf("\n// %sRow — a cooked record. `Row` is a CLAIMED suffix (docs/SPEC-TABLES.md\n", name)
	g.sf("// §11), so no declaration in the unit can take it.\n")
	g.sf("type %sRow struct {\n", name)
	w := &cookWriter{g: g}
	for _, fl := range ml.Fields {
		w.pieces = ir.FieldPieces(g.unit, fl.Field, fl.Offset)
		w.piece = 0
		g.emitBlittableField(fl.Field, w)
	}
	w.pad(ml.Size)
	g.sf("}\n")
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
	if w.at >= to {
		return
	}
	w.g.sf("\t_ [%d]byte // generated padding\n", to-w.at)
	w.at = to
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
		// and NULL IS ZERO. It is not a Go pointer and never becomes one:
		// <T>At is the one add that resolves it.
		next()
		g.sf("\t%s int64 // *%s: signed self-relative delta, zero is null (§6.3)\n", name, f.Type.Name)
	case f.Type.Kind == ir.TString:
		next()
		g.sf("\t%s [%d]byte // string(%d): buffer, used length beside it\n", name, f.Type.Size+1, f.Type.Size)
		next()
		g.sf("\t%sLength int32\n", name)
	case f.Type.Kind == ir.TBytes:
		next()
		g.sf("\t%s [%d]byte // bytes(%d): buffer, used length beside it\n", name, f.Type.Size, f.Type.Size)
		next()
		g.sf("\t%sLength int32\n", name)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
		// every array in a cooked record is INLINE, because a cook writes the
		// by-value form verbatim (§7). A COUNTED array writes all N slots
		// (§7.2), so the storage is N wide whatever the count.
		next()
		g.sf("\t%s [%d]%s\n", name, f.ArrayBound, goCookBlittableType(g.unit, f.Type))
		if f.Array == ir.ArrayCounted {
			next()
			g.sf("\t%sCount int32\n", name)
		}
	default:
		next()
		g.sf("\t%s %s\n", name, goCookBlittableType(g.unit, f.Type))
	}
	if f.Type.Optional {
		next()
		g.sf("\t%sPresent bool // ?%s: one byte, in Go's model as in C++\n", name, f.Type.Name)
	}
}

// goCookBlittableType is goBlittableType with one difference: a pointer
// field's storage is the delta slot, never the target's record.
func goCookBlittableType(u *ir.Unit, t ir.FieldType) string {
	if t.Pointer {
		return "int64"
	}
	return goBlittableType(u, t)
}

// ---- the layout check (docs/SPEC-TABLES.md §19.3, §20.3) ----

// emitLayoutCheck is §20.3's Go half for the COOK closure: the compiler's
// layout model committed to every cookable record, asserted against THIS
// runtime's own, run once at package initialisation. C++ says it with
// static_assert at compile time; Go could say most of it as a constant
// expression but not with a message a person can act on, so it says it where
// the program starts and REFUSES.
func (g *cookGen) emitLayoutCheck() {
	g.needsFmt = true
	g.hf("// THE LAYOUT CONTRACT for the cook closure (docs/SPEC-TABLES.md §20.3), run\n")
	g.hf("// ONCE at package initialisation: a cooked region is laid out by the\n")
	g.hf("// compiler's C ABI model, so a runtime that lays one of these records out\n")
	g.hf("// differently would read a cook at the wrong offsets and never know.\n")
	g.hf("func init() {\n")
	for _, name := range g.cook.order {
		g.emitRecordCheck(name)
	}
	g.hf("}\n\n")
	g.hf("// tableCookLayoutSize refuses a record this runtime lays out at a different\n")
	g.hf("// size, or with a different alignment, from the layout model the cook's bytes\n")
	g.hf("// come from.\n")
	g.hf("func tableCookLayoutSize(what string, got, want uintptr) {\n")
	g.hf("\tif got != want {\n")
	g.hf("\t\tpanic(fmt.Sprintf(\"schema cook layout: %%s is %%d bytes in this runtime and %%d in the layout \"+\n")
	g.hf("\t\t\t\"model the cook's bytes come from — the two disagree about the bytes (docs/SPEC-TABLES.md §20.3)\", what, got, want))\n")
	g.hf("\t}\n}\n\n")
	g.hf("// tableCookLayoutOffset refuses a field this runtime puts somewhere else.\n")
	g.hf("func tableCookLayoutOffset(what string, got, want uintptr) {\n")
	g.hf("\tif got != want {\n")
	g.hf("\t\tpanic(fmt.Sprintf(\"schema cook layout: %%s sits at %%d in this runtime and %%d in the layout \"+\n")
	g.hf("\t\t\t\"model the cook's bytes come from — the two disagree about the bytes (docs/SPEC-TABLES.md §20.3)\", what, got, want))\n")
	g.hf("\t}\n}\n\n")
}

func (g *cookGen) emitRecordCheck(name string) {
	ml := g.cook.members[name]
	if ml == nil {
		return
	}
	spelled := name + "Row"
	g.hf("\ttableCookLayoutSize(%q, unsafe.Sizeof(%s{}), %d)\n", spelled, spelled, ml.Size)
	g.hf("\ttableCookLayoutSize(%q, unsafe.Alignof(%s{}), %d)\n", spelled+" alignment", spelled, ml.Align)
	for _, fl := range ml.Fields {
		g.hf("\ttableCookLayoutOffset(%q, unsafe.Offsetof(%s{}.%s), %d)\n",
			spelled+"."+ir.GoExportName(fl.Field.Name), spelled, ir.GoExportName(fl.Field.Name), fl.Offset)
	}
}

// ---- the cook handle ----

func (g *cookGen) emitCookHandle(st *ir.Struct) {
	g.needsUnsafe = true
	name := st.Name
	ml := g.cook.members[name]
	align := g.cook.align
	g.hf("// %sCook is %s's cook: a pointer and a length, and then the root where it\n", name, name)
	g.hf("// lies. Opening one is a HEADER MATCH and no copy; a reference is one add\n")
	g.hf("// (docs/SPEC-TABLES.md §7).\n")
	g.hf("//\n")
	g.hf("// `Cook` is a CLAIMED suffix (§11). C++ spells the same claimed verbs as free\n")
	g.hf("// functions — %sOpen, %sAt — and so does Go, because Go has free functions;\n", name, name)
	g.hf("// what is a MEMBER here is only what §11 leaves a language free to make one.\n")
	g.hf("//\n")
	g.hf("// THE MEMORY IS THE CONSUMER'S. Nothing here allocates, nothing here copies and\n")
	g.hf("// nothing here pins: the region must stay put and stay aligned for as long as\n")
	g.hf("// this handle or anything reached through it is used.\n")
	g.hf("type %sCook struct {\n", name)
	g.hf("\tRegion       unsafe.Pointer // the DATA part's base: the root sits at offset zero\n")
	g.hf("\tRegionLength int64          // data_length, as the header framed it\n")
	g.hf("}\n\n")

	g.hf("// §7.1's constants, so a consumer reading this file has the facts and not a\n")
	g.hf("// description of them. They are METHODS because §11 leaves a language whose\n")
	g.hf("// accessors are members free to spell them that way, and a package constant\n")
	g.hf("// per table per fact would claim names §11 does not.\n")
	g.hf("func (c %sCook) RegionAlignment() int64 { return %d } // the greatest alignof in the region, floor eight\n", name, align)
	g.hf("func (c %sCook) RootSize() int64        { return %d }\n", name, ml.Size)
	g.hf("func (c %sCook) RootAlign() int64       { return %d }\n\n", name, ml.Align)

	g.hf("// Root is the root record at the region's base. It is a POINTER and not a\n")
	g.hf("// copy: a cooked graph is walked by adding deltas to slot addresses.\n")
	g.hf("func (c %sCook) Root() *%sRow { return (*%sRow)(c.Region) }\n\n", name, name, name)

	g.emitOpen(st, ml)
	g.emitAt(st)
	g.hf("// Type is %s's cooked-record descriptor, the head of the graph a reflective\n", name)
	g.hf("// walk follows (docs/SPEC-TABLES.md §8).\n")
	g.hf("func (c %sCook) Type() *TableCookInfo { return &%s }\n\n", name, cookInfoSymbol(name))
}

func (g *cookGen) emitOpen(st *ir.Struct, ml *ir.MemberLayout) {
	name := st.Name
	g.hf("// %sOpen checks the header and POINTS, and this is the WHOLE check\n", name)
	g.hf("// (docs/SPEC-TABLES.md §7): the magic read in the machine's own order, the byte\n")
	g.hf("// order it establishes, the build version, every RESERVED word zero, the region\n")
	g.hf("// ALIGNMENT the header names, the two part lengths against the length the\n")
	g.hf("// caller passed — a truncated file refuses — the ROOT's own storage inside the\n")
	g.hf("// data part, and the alignment of the base. Nothing per node, ever: that is\n")
	g.hf("// what makes this O(1) in the file's size.\n")
	g.hf("//\n")
	g.hf("// On a match the bytes ARE what this build wrote, in this build's layout and\n")
	g.hf("// this build's byte order, so there is nothing to validate and nothing to fix\n")
	g.hf("// up. On any failure it returns false and points at nothing, and the caller\n")
	g.hf("// falls back to a wire load — the path that carries every version.\n")
	g.hf("//\n")
	g.hf("// EVERY NUMBER BELOW COMES OUT OF THE FILE, so all of the arithmetic is\n")
	g.hf("// UNSIGNED and each term is BOUNDED BEFORE IT IS ADDED. A signed length would\n")
	g.hf("// put one signed value into that arithmetic and one negative case into every\n")
	g.hf("// comparison; a caller holding a length from a stat casts once, at the call\n")
	g.hf("// site, where the sign is still its own business.\n")
	g.hf("func %sOpen(cook *%sCook, base unsafe.Pointer, length int64) bool {\n", name, name)
	g.hf("\t*cook = %sCook{}\n", name)
	g.hf("\tif base == nil || length < %d {\n\t\treturn false\n\t}\n", cookHeaderBytes)
	g.hf("\tbytes := uint64(length)\n\n")
	g.hf("\t// THE MAGIC, read before anything else: it is what establishes the byte\n")
	g.hf("\t// order every other header word is written in. A cook of the other order\n")
	g.hf("\t// reads back this constant byte-reversed and refuses HERE, rather than\n")
	g.hf("\t// reaching a fix-up pass this design does not have.\n")
	g.hf("\tif *(*uint64)(base) != TableCookMagic {\n\t\treturn false\n\t}\n")
	g.hf("\t// and the ORDER WORD does the other job: it RECORDS which order wrote the\n")
	g.hf("\t// file, so a refusal names the order rather than inferring it.\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 16)) != TableCookByteOrder {\n\t\treturn false\n\t}\n")
	g.hf("\t// THE BUILD VERSION: under the match-and-point rule a matching id means Open\n")
	g.hf("\t// checks nothing further, so it is the sole guard between this runtime and a\n")
	g.hf("\t// foreign region (§20).\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 8)) != BuildVersion {\n\t\treturn false\n\t}\n")
	g.hf("\t// THE RESERVED WORDS: a non-zero one means a writer used a form this build\n")
	g.hf("\t// does not understand, and Open refuses rather than ignoring it.\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 48)) != 0 {\n\t\treturn false\n\t}\n")
	g.hf("\tif *(*uint64)(unsafe.Add(base, 56)) != 0 {\n\t\treturn false\n\t}\n\n")
	g.hf("\t// THE ALIGNMENT WORD is the one field the check COMPUTES WITH rather than\n")
	g.hf("\t// only compares against — the data part begins at align_up(64, alignment)\n")
	g.hf("\t// and the base is measured against it — so a word that is not an alignment\n")
	g.hf("\t// rounds nothing and aligns nothing. A zero there is a division by zero\n")
	g.hf("\t// inside the check, which is the defect the check prevents.\n")
	g.hf("\talignment := *(*uint64)(unsafe.Add(base, 40))\n")
	g.hf("\tif alignment < %d || alignment > %d {\n\t\treturn false\n\t}\n", ir.RegionAlignFloor, cookMaxAlign)
	g.hf("\tif alignment&(alignment-1) != 0 {\n\t\treturn false // a power of two\n\t}\n")
	g.hf("\tif alignment%%%d != 0 {\n\t\treturn false // and a multiple of the ROOT's own alignof\n\t}\n\n", ml.Align)
	g.hf("\t// THE DATA OFFSET IS DERIVED, never a header field: a fact a reader computes\n")
	g.hf("\t// is a fact two writers cannot disagree about, and it is 64 for every unit\n")
	g.hf("\t// this language can declare.\n")
	g.hf("\tdataOffset := (uint64(%d) + alignment - 1) &^ (alignment - 1)\n\n", cookHeaderBytes)
	g.hf("\t// THE TWO PART LENGTHS against the length the caller passed. The whole file\n")
	g.hf("\t// is dataOffset + data + attribution, and a size that is not exactly that\n")
	g.hf("\t// refuses: a truncated file and a file with trailing bytes are the same\n")
	g.hf("\t// refusal. Each term is bounded before it is added, so nothing here can wrap\n")
	g.hf("\t// past the top of the type and land back inside the buffer.\n")
	g.hf("\tdataLength := *(*uint64)(unsafe.Add(base, 24))\n")
	g.hf("\tattribution := *(*uint64)(unsafe.Add(base, 32))\n")
	g.hf("\tif dataLength > bytes || attribution > bytes-dataLength {\n\t\treturn false\n\t}\n")
	g.hf("\tif dataOffset > bytes-dataLength-attribution {\n\t\treturn false\n\t}\n")
	g.hf("\tif dataOffset+dataLength+attribution != bytes {\n\t\treturn false\n\t}\n\n")
	g.hf("\t// THE DATA PART MUST HOLD THE ROOT. The part lengths frame the FILE; they do\n")
	g.hf("\t// not say the region is at least sizeof(root). Without this a forged short\n")
	g.hf("\t// data part describes a root partly outside the file, and a match-and-point\n")
	g.hf("\t// reader would hand back storage the caller never gave it — the one way this\n")
	g.hf("\t// design could read past the length it was passed.\n")
	g.hf("\tif dataLength < %d {\n\t\treturn false\n\t}\n\n", ml.Size)
	g.hf("\t// THE ALIGNMENT OF THE BASE. The header pads the data part to the region's\n")
	g.hf("\t// alignment, so a base an allocator or mmap gave you is already aligned; one\n")
	g.hf("\t// that is not is a caller's buffer this form cannot be read out of. The\n")
	g.hf("\t// alignment divides 64, so the derived data offset carries the property from\n")
	g.hf("\t// the file's base to the region's.\n")
	g.hf("\tif uint64(uintptr(base))%%alignment != 0 {\n\t\treturn false\n\t}\n\n")
	g.hf("\tcook.Region = unsafe.Add(base, uintptr(dataOffset))\n")
	g.hf("\tcook.RegionLength = int64(dataLength)\n")
	g.hf("\treturn true\n}\n\n")
}

func (g *cookGen) emitAt(st *ir.Struct) {
	name := st.Name
	g.hf("// %sAt dereferences a reference, and it is the same call in a locked region\n", name)
	g.hf("// and an opened cook because they are the same encoding (§6.3): the slot is\n")
	g.hf("// eight bytes, SIGNED, self-relative from the SLOT'S OWN ADDRESS, so a deref\n")
	g.hf("// needs no base pointer and no bounds test, and NULL IS A DELTA OF ZERO.\n")
	g.hf("// Nothing about this call is the cook's: it is what a region reference is, and\n")
	g.hf("// a cook is a region written verbatim.\n")
	g.hf("//\n")
	g.hf("// It takes the SLOT and not its value, because a self-relative delta means\n")
	g.hf("// nothing without the address it is relative to.\n")
	g.hf("func %sAt(slot *int64) *%sRow {\n", name, name)
	g.hf("\tdelta := *slot\n")
	g.hf("\tif delta == 0 {\n\t\treturn nil\n\t}\n")
	g.hf("\treturn (*%sRow)(unsafe.Add(unsafe.Pointer(slot), uintptr(delta)))\n}\n\n", name)
}

// emitAllDescriptors writes the unit's whole cooked-record descriptor graph
// into the home file: constant data, so a reflective walk costs a lookup and
// not a parse. Every record the region can hold hangs off the field column, so
// a walker reaches the whole graph from any root.
func (g *cookGen) emitAllDescriptors() {
	for _, name := range g.cook.order {
		g.emitRecordDescriptor(name)
	}
	if len(g.links) == 0 {
		return
	}
	g.hf("// THE RECORD COLUMNS are linked here rather than in the literals above, and\n")
	g.hf("// the reason is a language fact rather than a taste: a cooked graph is CYCLIC\n")
	g.hf("// by design — ListNode names ListNode — and Go refuses an initialization\n")
	g.hf("// cycle among package-level variables, a closure in an initializer included.\n")
	g.hf("// The links are written once, before any consumer runs, and nothing mutates\n")
	g.hf("// them after: the descriptor surface is immutable from then on, readable from\n")
	g.hf("// any goroutine at any time with no synchronisation.\n")
	g.hf("func init() {\n")
	for _, l := range g.links {
		g.hf("\t%s\n", l)
	}
	g.hf("}\n\n")
}

func (g *cookGen) emitRecordDescriptor(record string) {
	ml := g.cook.members[record]
	if ml == nil {
		return
	}
	symbol := cookInfoSymbol(record)
	if g.wroteDescriptor[symbol] {
		return
	}
	g.wroteDescriptor[symbol] = true
	g.hf("var %s = TableCookInfo{\n", symbol)
	g.hf("\tName: %q, Size: %d, Align: %d, NumFields: %d,\n", record, ml.Size, ml.Align, len(ml.Fields))
	g.hf("\tFields: []TableCookFieldInfo{\n")
	var fieldIndex []string
	for _, fl := range ml.Fields {
		f := fl.Field
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				g.links = append(g.links, fmt.Sprintf("%s.Fields[%d].Record = func() *TableCookInfo { return &%s }",
					symbol, len(fieldIndex), cookInfoSymbol(ref.Name)))
			}
		}
		fieldIndex = append(fieldIndex, f.Name)
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
		g.hf("\t\t{Name: %q, Offset: %d, Size: %d, ElemSize: %d, IsArray: %t, ArrayBound: %d, IsPointer: %t, CountOffset: %d, PresentOffset: %d, Storage: TableCookStorage%s},\n",
			f.Name, fl.Offset, fl.Size, elemSize, isArray, bound, f.Type.Pointer, countOffset, presentOffset,
			cookStorageKind(f))
	}
	g.hf("\t},\n}\n\n")
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
// home's <Base>Cook.go. `withBuildVersion` is set only when the unit has no
// BLOCK form to carry it: one package, so exactly one accelerator defines each
// name (docs/SPEC-TABLES.md §20.7).
func cookRuntime(buildVersion uint64, withBuildVersion bool) string {
	var b strings.Builder
	if withBuildVersion {
		fmt.Fprintf(&b, `// BuildVersion is the unit's build version (docs/SPEC-TABLES.md §20): one
// digest over every fact the bytes this build produces depend on. It both
// ADDRESSES a cooked artifact — the store is keyed by (asset hash, build
// version) — and REFUSES one, because it is what Open checks out of the header.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
// what everything cooked or blocked is keyed by.
const BuildVersion uint64 = 0x%016x

`, buildVersion)
	}
	fmt.Fprintf(&b, `// TableCookMagic is the cook's magic (docs/SPEC-TABLES.md §7.1), read before
// anything else: it is what establishes the byte order every other header word
// is written in, and it is also what separates a COOK from a BLOCK — the two
// accelerators carry the same build version and different magics, because a
// form's identity belongs in its magic rather than in a second digest.
//
// The value is "SCHMCOOK" read as ASCII in the byte order a little-endian store
// produces, so a hex dump of a little-endian cook is legible.
const TableCookMagic uint64 = 0x%016x

// TableCookByteOrder is THIS BUILD's byte order, as §7.1's order word records
// it. The byte order is settled AT COOK TIME for the target build, so a
// matching build version already means a matching order and Open never fixes
// anything up; a cook whose recorded order is not this build's is simply not
// this build's file.
var TableCookByteOrder = tableCookNativeOrder()

func tableCookNativeOrder() uint64 {
	probe := uint16(1)
	if *(*byte)(unsafe.Pointer(&probe)) == 1 {
		return 1 // little
	}
	return 2 // big
}

// TableCookHeaderBytes is §7.1's header: 64 bytes of u64 words. The DATA part
// begins at align_up(64, alignment) — DERIVED and not a header field, because a
// fact a reader computes is a fact two writers cannot disagree about.
const TableCookHeaderBytes int64 = %d

// TableCookMaxAlign is the ceiling on the header's alignment word: the same
// sixty-four a block's base takes (§19.1), past which the derived data offset
// would no longer be the 64 every unit this language can declare produces.
const TableCookMaxAlign int64 = %d

// TableCookStorage is what a cooked SLOT holds, which is not always what the
// WIRE carries: an ENUM slot holds the ORDINAL at the enum's own derived
// storage width (docs/SPEC-TABLES.md §7.2), where the wire rides the
// variant-name hash. A walker reads a slot with the width ElemSize gives and
// the signedness this names.
//
// Its members are FLAT package-level constants, which is what Go already does
// with every enum a schema declares (SPEC.md §6.1's Go column) — so all eight
// spellings are claimed in internal/tablenames, exactly as a declared enum's
// variants are claimed by the checker. C# spells the same vocabulary as one
// scoped enum and claims one name; the port does not get to have it both ways,
// and the flat form is the one a Go consumer expects to write.
type TableCookStorage uint8

const (
	TableCookStorageRecord    TableCookStorage = iota // a nested record, or an array of them: descend through it
	TableCookStorageReference                         // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
	TableCookStorageBool
	TableCookStorageSigned
	TableCookStorageUnsigned // an unsigned integer, an enum ordinal, a bits(N), a flags mask
	TableCookStorageFloat
	TableCookStorageString // char[N + 1] with an int32 used length beside it
	TableCookStorageBytes  // uint8[N] with an int32 used length beside it
)

// TableCookFieldInfo is one record's field as DATA — the mechanism behind a
// reflective read of a cooked region, and what a gate walks a whole graph with.
// A field carries the facts a region actually has: where it sits, how big it
// is, whether it is a POINTER EDGE, the bound its COUNT COMPANION is checked
// against, and the record it names.
type TableCookFieldInfo struct {
	Name          string
	Offset        int32 // the field's offset in the record this descriptor describes
	Size          int32 // its whole storage there, companions included
	ElemSize      int32 // one element's size, for an array; the field's own otherwise
	IsArray       bool
	ArrayBound    int32 // the DECLARED bound: a counted array's N, a string's or bytes' length
	IsPointer     bool  // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
	CountOffset   int32 // the count companion's offset, or -1 when the field has none
	PresentOffset int32 // an optional's presence bool, or -1
	Storage       TableCookStorage
	// Record is the record this field NAMES, behind a function so the table
	// stays constructible in any order — Go refuses an initialisation cycle
	// among package-level variables, and a cooked graph is cyclic by design.
	// nil when the field is a scalar. Following it is how a walker DESCENDS: a
	// pointer's target, a by-value nesting, and an array's element are all
	// reached through this one column.
	Record func() *TableCookInfo
}

// TableCookInfo is one cooked record's layout as DATA.
type TableCookInfo struct {
	Name      string
	Size      int32
	Align     int32
	NumFields int32
	Fields    []TableCookFieldInfo
}

`, cookMagic, cookHeaderBytes, cookMaxAlign)
	return b.String()
}
