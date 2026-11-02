// The COOKED FORM (docs/SPEC-TABLES.md §7): the Dart read half and the write
// half (§7.6, cookwrite.go): a fixed root's writer and a variable graph's.
//
// A cook is a region another build wrote, opened by a header match and then
// POINTED AT — nothing per node, ever, which is what makes Open O(1) in the
// file's size. Emitted on the side, in <Base>Cook.dart, which a consumer
// imports only if it reads a cook.
//
// WHERE DART DIFFERS FROM THE REFERENCE, and why:
//
//   - A REFERENCE RESOLVES TO AN OFFSET, not to a pointer: the slot is eight
//     bytes, SIGNED, self-relative from the SLOT'S OWN position, and Dart's
//     currency for "where" is an index into the region. Null is a delta of
//     zero, exactly as §6.3 states it.
//   - AND THE DEREF IS BOUNDED. C++ needs no bound because a pointer past the
//     region is the caller's problem the moment it is dereferenced; in Dart the
//     read after it would be an escaping RangeError, and a reader that throws
//     on hostile bytes is not a reader that REFUSES them. So a delta that
//     leaves the region answers TableCookRef.outside, and the fuzzer's oracle
//     is what holds that.
//   - THE LAYOUT MODEL IS THE GENERATED OFFSETS, the block form's rule for the
//     same reason: Dart has no struct to disagree with them, and the header's
//     BUILD VERSION is what refuses a producer that does.
//
// A closure carrying a UNION is a NAMED ABSENCE, as it is in the C# backend:
// the cook descriptors have no union column — §7.2's storage kinds are scalar,
// record, string, bytes and reference — so a walker could not describe one, and
// a form the reflective half cannot describe is a form this tier does not
// claim to read.
package darttable

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookHeaderBytes is §7.1's header: eight u64 words.
const cookHeaderBytes = int64(64)

// cookMaxAlign is the ceiling on the header's `alignment` word (§7).
const cookMaxAlign = int64(64)

type cookUnit struct {
	tables  []*ir.Struct
	members map[string]*ir.MemberLayout
	order   []string
	skipped map[string]string
	align   int64
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
// gets one, with the union absence stated above.
func cookUnitOf(u *ir.Unit) *cookUnit {
	c := &cookUnit{members: map[string]*ir.MemberLayout{}, skipped: map[string]string{}}
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		// A map's generated ENTRY (docs/SPEC-TABLES.md §2.8) is never a root or
		// a row: it is an extent inside its holder's node, reached through the
		// holder's map slot, exactly as the block form treats it (§19). A cook
		// for it would name a value class that lives only in the variable form's
		// <Base>Wire.dart, which a cook library does not import.
		if st.MapEntryOf != "" {
			continue
		}
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
	// THE ENTRY CURSOR of every map a member holds (§2.8, §7.2): an entry is no
	// root, but the holder's <field>At and <field>Find move an <Entry>Row over
	// the entry array, so the entry record, and every record it reaches, is a
	// member whose cursor this unit emits. Repeated until no map adds one, since
	// an entry's value may hold a map of its own.
	for grew := true; grew; {
		grew = false
		for _, name := range sortedKeys(c.members) {
			st := cookMember(u, name)
			if st == nil {
				continue
			}
			for _, f := range st.Fields {
				if !f.IsMap() || c.members[f.MapEntry.Name] != nil {
					continue
				}
				cookWalk(u, f.MapEntry.Name, func(n string, ml *ir.MemberLayout) {
					c.members[n] = ml
				})
				grew = true
			}
		}
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
	var walkUnion func(*ir.Union)
	seenUnion := map[*ir.Union]bool{}
	// a union's set arm rides in its holder's node: the arm's by-value table
	// body and its pointer target are members of the same region (§3.1, §7.6),
	// and so is whatever an arm that is itself a union holds, however deep
	walkUnion = func(un *ir.Union) {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		for _, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			if t := tableRef(arm.F); t != nil {
				walk(t.Name)
			} else if inner := unionRef(arm.F); inner != nil {
				walkUnion(inner)
			}
		}
	}
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
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walk(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	walk(name)
}

// cookableClosure answers whether the cook descriptors can describe one
// table's whole region, and why not when they cannot.
func cookableClosure(u *ir.Unit, st *ir.Struct) string {
	seen := map[string]bool{}
	var walk func(string) string
	var walkUnion func(*ir.Union) string
	walkUnion = func(un *ir.Union) string {
		for _, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			switch ref := arm.F.Type.Ref.(type) {
			case *ir.Union:
				if why := walkUnion(ref); why != "" {
					return why
				}
			case *ir.Struct:
				if why := walk(ref.Name); why != "" {
					return why
				}
			}
		}
		return ""
	}
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
				if why := walkUnion(ref); why != "" {
					return why
				}
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

type cookGen struct {
	unit      *ir.Unit
	file      *ir.File
	cook      *cookUnit
	blocks    *ir.BlockUnit
	home      bool
	homeBase  string
	body      strings.Builder
	imports   map[string]map[string]bool
	needData  bool
	needHome  bool
	needBlock bool
	// needConvert asks for dart:convert: the variable cook writer encodes a
	// Dart String as the UTF-8 bytes §7.2's storage holds
	needConvert bool
	// prefixed names a library imported under a prefix: the variable cook
	// writer's value classes live in <Home>Wire.dart and share their names
	// with the fixed class's storage in <Home>Fixed.dart (§7.6)
	prefixed map[string]string
	// form1 is set while a fixed root's FORM-1 cook writer is emitted
	// (cookwrite_form1.go): the variable walk over the wire library's class
	form1 bool
	// form1Unions queues the unions the form-1 cook writes as ARRAY ELEMENTS
	// (§2.6, §3): each gets one element writer, `_cook<Union>`, in the
	// namespace the record writers share with every declared type name
	form1Unions     []*ir.Union
	form1UnionsSeen map[*ir.Union]bool
	// the records some field column names, so only those need the edge method
	cookNamed map[string]bool
	// depth numbers the extent walk's nested locals (l1, n1, i1, ...) so an
	// inner holder never shadows an outer one
	depth int
	// mapOrders queues the key order of each map entry type a writer sorts
	// (docs/SPEC-TABLES.md §2.8), emitted once per root writer
	mapOrders map[string]*ir.Field
}

func (g *cookGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

func (g *cookGen) need(library, symbol string) {
	if g.imports[library] == nil {
		g.imports[library] = map[string]bool{}
	}
	g.imports[library][symbol] = true
}

// generateCookFiles emits <Base>Cook.dart for every file of a unit that
// declares a table, plus <Package>Cook.dart — the unit's one cook runtime home.
func generateCookFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(u.Tables) == 0 {
		return out, nil
	}
	cook := cookUnitOf(u)
	home := runtimeHome(u)
	written := false
	for _, f := range u.Files {
		g := &cookGen{unit: u, file: f, cook: cook, blocks: blocks,
			home: f.Base == home, homeBase: home, imports: map[string]map[string]bool{}}
		if g.home {
			g.emitRuntime()
			written = true
		}
		// the RECORD CURSORS this file declares that the BLOCK form has not
		// already written: one record, one cursor, whichever accelerator
		// reaches it first
		for _, record := range cook.order {
			if cookDeclFile(u, record) != f.Base || blocks.Layout(record) != nil {
				continue
			}
			g.emitRecordCursor(record, cook.members[record])
		}
		for _, t := range f.Tables {
			if cook.opens(t.Name) {
				g.emitCook(t, cook.members[t.Name])
				continue
			}
			if why := cook.skipped[t.Name]; why != "" {
				g.pf("// table %s has NO cook reader here: %s.\n", t.Name, why)
				g.pf("// Its wire (§3) is unaffected, and schema cook-check still reads the\n")
				g.pf("// file — only this reader is absent, and it is absent by construction.\n\n")
				// the WRITE half has no descriptors to describe, so a union
				// closure does not stop it (§7.6, cookwrite_form1.go)
				g.emitCookWriteForm1(t)
			}
		}
		if g.body.Len() > 0 {
			out[f.Base+"Cook.dart"] = g.assemble()
		}
	}
	if !written {
		g := &cookGen{unit: u, cook: cook, blocks: blocks, home: true, homeBase: home,
			imports: map[string]map[string]bool{}}
		g.emitRuntime()
		out[home+"Cook.dart"] = g.assemble()
	}
	return out, nil
}

func (g *cookGen) assemble() []byte {
	var h strings.Builder
	if g.file == nil {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.unit.Package)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the COOKED FORM (docs/SPEC-TABLES.md §7): read and carried write surfaces.\n", g.unit.Package)
	h.WriteString("//\n")
	h.WriteString("// Open checks the header and POINTS — nothing per node, ever, which is what\n")
	h.WriteString("// makes it O(1) in the file's size. Import this library if you read or write a\n")
	h.WriteString("// cook; the unit's <Base>Table.dart carries not one symbol of it.\n")
	h.WriteString("//\n")
	h.WriteString("// A reference resolves to an OFFSET in the region and the deref is BOUNDED:\n")
	h.WriteString("// in Dart the read after an escaping delta would be a RangeError, and a\n")
	h.WriteString("// reader that throws on hostile bytes is not one that refuses them.\n")
	h.WriteString("\n")
	if g.needConvert {
		h.WriteString("import 'dart:convert';\n")
	}
	if g.needData || g.home || g.needConvert {
		h.WriteString("import 'dart:typed_data';\n\n")
	}
	if !g.home && g.needHome {
		g.imports[g.homeBase+"Cook.dart"] = nil
	}
	if g.needBlock {
		g.imports[g.homeBase+"Block.dart"] = nil
	}
	for base := range g.prefixed {
		if _, ok := g.imports[base]; !ok {
			g.imports[base] = nil
		}
	}
	for _, base := range sortedKeys(g.imports) {
		syms := g.imports[base]
		if prefix := g.prefixed[base]; prefix != "" {
			fmt.Fprintf(&h, "import '%s' as %s;\n", base, prefix)
			if syms == nil {
				continue
			}
		}
		if syms == nil {
			fmt.Fprintf(&h, "import '%s';\n", base)
			continue
		}
		names := sortedKeys(syms)
		one := fmt.Sprintf("import '%s' show %s;", base, strings.Join(names, ", "))
		if len(one) <= 80 {
			h.WriteString(one + "\n")
			continue
		}
		two := fmt.Sprintf("    show %s;", strings.Join(names, ", "))
		if len(two) <= 80 {
			fmt.Fprintf(&h, "import '%s'\n%s\n", base, two)
			continue
		}
		fmt.Fprintf(&h, "import '%s'\n    show\n", base)
		for i, n := range names {
			sep := ","
			if i == len(names)-1 {
				sep = ";"
			}
			fmt.Fprintf(&h, "        %s%s\n", n, sep)
		}
	}
	if len(g.imports) > 0 {
		h.WriteString("\n")
	}
	h.WriteString(strings.Trim(g.body.String(), "\n"))
	h.WriteString("\n")
	return []byte(h.String())
}

func (g *cookGen) emitRuntime() {
	// the BUILD VERSION rides in the BLOCK runtime home; a unit whose tables
	// have no block form still needs it, so it is emitted here only then
	g.pf("%s", cookRuntimeSource(ir.BuildVersion(g.unit), len(g.blocks.Tables) == 0))
}

// emitRecordCursor writes a cooked record's view: a ByteData, an offset, and
// one getter per field where it lies. It is the block form's cursor exactly —
// one record, one layout, one spelling — and it is emitted here only when the
// block form has not already written it.
func (g *cookGen) emitRecordCursor(record string, ml *ir.MemberLayout) {
	if ml == nil {
		return
	}
	g.needData = true
	bg := &blockGen{unit: g.unit, file: g.file, blocks: g.blocks, imports: g.imports, inCook: true}
	g.pf("// %s as a cooked RECORD: a view and an offset, and every field read where\n", record)
	g.pf("// it lies. the offset is settable so a walk reuses one cursor (§7.2). A POINTER\n")
	g.pf("// field is its slot's offset: resolve it through <Root>Cook.at.\n")
	g.pf("final class %sRow {\n", record)
	g.pf("  final ByteData view;\n")
	g.pf("  int at;\n\n")
	g.pf("  %sRow(this.view, this.at);\n\n", record)
	g.pf("  static const int rowSize = %d;\n", ml.Size)
	g.pf("  static const int rowAlign = %d;\n\n", ml.Align)
	bg.body.Reset()
	bg.emitInlineAccessors(ml, nil, "  ", "at")
	body := strings.TrimRight(bg.body.String(), "\n")
	g.pf("%s\n", body)
	g.pf("}\n\n")
}

// emitCook writes one root's cook reader: Open, the region, the deref and the
// descriptors.
func (g *cookGen) emitCook(st *ir.Struct, ml *ir.MemberLayout) {
	name := st.Name
	g.needData = true
	g.needHome = true
	// the BUILD VERSION lives in the accelerator that owns it (§20.7): the
	// block runtime home when this unit has a block form, and the cook's own
	// otherwise
	if len(g.blocks.Tables) > 0 {
		g.needBlock = true
	}
	g.pf("// %s's cook: a region and its length, and then records in place (§7).\n", name)
	g.pf("//\n")
	g.pf("// The bytes belong to the CONSUMER — a file it read, or memory a producer\n")
	g.pf("// handed across. Nothing here copies them.\n")
	g.pf("final class %sCook {\n", name)
	g.pf("  final Uint8List bytes;\n")
	g.pf("  final ByteData view;\n")
	g.pf("  // the REGION's first byte within bytes, and the data part's length\n")
	g.pf("  final int region;\n")
	g.pf("  final int length;\n\n")
	g.pf("  const %sCook(this.bytes, this.view, this.region, this.length);\n\n", name)
	g.pf("  /// A cursor over this cook's records, to lend to [root] and to the row\n")
	g.pf("  /// accessors that follow a reference.\n")
	g.pf("  %sRow cursor() => %sRow(view, region);\n\n", name, name)
	g.pf("  /// The ROOT, which sits at offset 0 of the region (§7.1), moved onto the\n")
	g.pf("  /// cursor the caller lends. There is no allocating twin: a walk that\n")
	g.pf("  /// follows references moves one cursor per record type and allocates\n")
	g.pf("  /// nothing per node, and [cursor] makes the one you lend.\n")
	g.pf("  %sRow root(%sRow into) {\n", name, name)
	g.pf("    into.at = region;\n    return into;\n  }\n\n")
	g.needRow(name)
	g.emitCookOpen(st, ml)
	g.emitCookAt(st)
	g.emitCookDescriptors(st)
	g.emitCookWriteFixed(st)
	g.pf("}\n\n")
	g.emitCookWriteForm1(st)
}

func (g *cookGen) needRow(record string) {
	base := cookDeclFile(g.unit, record)
	if base == "" {
		return
	}
	if g.blocks.Layout(record) != nil {
		g.need(base+"Block.dart", record+"Row")
		return
	}
	if g.file != nil && base == g.file.Base {
		return
	}
	g.need(base+"Cook.dart", record+"Row")
}

// dartCookMagic is the cook's magic (docs/SPEC-TABLES.md §7.1), the value the
// runtime's tableCookMagic spells.
const dartCookMagic = uint64(0x4B4F4F434D484353)

func (g *cookGen) emitCookOpen(st *ir.Struct, ml *ir.MemberLayout) {
	name := st.Name
	g.pf("  // open checks the header and POINTS, and this is the WHOLE check (§7): the\n")
	g.pf("  // magic read bytewise, the byte order it establishes, the build version,\n")
	g.pf("  // every RESERVED word zero, the region ALIGNMENT the header names, the two\n")
	g.pf("  // part lengths against the length the caller passed — a truncated file\n")
	g.pf("  // refuses — the ROOT's own storage inside the data part, and the alignment\n")
	g.pf("  // of the base. Nothing per node, ever: that is what makes this O(1) in the\n")
	g.pf("  // file's size.\n")
	g.pf("  //\n")
	g.pf("  // On a match the bytes ARE what this build wrote, in this build's layout and\n")
	g.pf("  // this build's byte order, so there is nothing to validate and nothing to\n")
	g.pf("  // fix up. On any failure it returns null and points at nothing, and the\n")
	g.pf("  // caller falls back to a wire load — the path that carries every version.\n")
	g.pf("  //\n")
	g.pf("  // EVERY NUMBER BELOW COMES OUT OF THE FILE. Dart's int is signed and 64 bits\n")
	g.pf("  // wide, so a forged word near 2^63 arrives NEGATIVE — and every term is\n")
	g.pf("  // bounded BEFORE it is added, so nothing here wraps past the top of the\n")
	g.pf("  // type and lands back inside the buffer.\n")
	g.pf("  //\n")
	g.pf("  // open is openWithReason without the reason: the same checks in the same\n")
	g.pf("  // order, so the verdict and the reason never disagree.\n")
	g.pf("  static %sCook? open(Uint8List? bytes, int at, int length) {\n", name)
	g.pf("    return openWithReason(bytes, at, length).$1;\n  }\n\n")
	g.pf("  // openWithReason answers the cook and the FIRST failing clause beside the\n")
	g.pf("  // null, as the reference leg names it (docs/SPEC-TABLES.md §7, the clauses in\n")
	g.pf("  // the order internal/codegen/gotable/cook.go's Open checks them): no buffer\n")
	g.pf("  // is an unaligned base, a claim under the header or past the caller's bytes\n")
	g.pf("  // is truncated, the magic is not_a_cook or, read byte-reversed,\n")
	g.pf("  // foreign_order, then the order word, the build version, the reserved words,\n")
	g.pf("  // the alignment word, the two part lengths and the root inside the data\n")
	g.pf("  // part, and last the base's own alignment.\n")
	g.pf("  static (%sCook?, TableRefuseReason) openWithReason(\n", name)
	g.pf("    Uint8List? bytes,\n    int at,\n    int length,\n  ) {\n")
	g.pf("    if (bytes == null || at < 0) {\n      return (null, TableRefuseReason.unalignedBase);\n    }\n")
	g.pf("    if (length < %d) {\n      return (null, TableRefuseReason.truncated);\n    }\n", cookHeaderBytes)
	g.pf("    if (length > bytes.length - at) {\n")
	g.pf("      // the claim runs past the bytes the caller owns\n")
	g.pf("      return (null, TableRefuseReason.truncated);\n    }\n")
	g.pf("    final view = ByteData.view(\n")
	g.pf("      bytes.buffer,\n      bytes.offsetInBytes,\n      bytes.lengthInBytes,\n    );\n\n")
	g.pf("    // THE MAGIC, read BYTEWISE before anything else: it is what establishes\n")
	g.pf("    // the byte order every other header word is written in. A cook of the\n")
	g.pf("    // other order reads back this constant byte-reversed and refuses HERE,\n")
	g.pf("    // rather than reaching a fix-up pass this design does not have.\n")
	g.pf("    final magic = tableCookRead64(view, at);\n")
	g.pf("    if (magic != tableCookMagic) {\n")
	// the magic as a cook of the OTHER byte order reads back through the same
	// bytewise read: what tells foreign_order from not_a_cook (§7.1)
	g.pf("      return (\n        null,\n        magic == 0x%016x\n", bits.ReverseBytes64(dartCookMagic))
	g.pf("            ? TableRefuseReason.foreignOrder\n            : TableRefuseReason.notACook,\n      );\n    }\n")
	g.pf("    // and the ORDER WORD does the other job: it RECORDS which order wrote the\n")
	g.pf("    // file, so a refusal names the order rather than inferring it.\n")
	g.pf("    if (tableCookRead64(view, at + 16) != tableCookByteOrder) {\n      return (null, TableRefuseReason.notACook);\n    }\n")
	g.pf("    // THE BUILD VERSION: under the match-and-point rule a matching id means\n")
	g.pf("    // open checks nothing further, so it is the sole guard between this\n")
	g.pf("    // runtime and a foreign region (§20).\n")
	g.pf("    if (tableCookRead64(view, at + 8) != tableBuildVersion) {\n      return (null, TableRefuseReason.wrongBuildVersion);\n    }\n")
	g.pf("    // THE RESERVED WORDS: a non-zero one means a writer used a form this\n")
	g.pf("    // build does not understand, and open refuses rather than ignoring it.\n")
	g.pf("    if (tableCookRead64(view, at + 48) != 0 ||\n        tableCookRead64(view, at + 56) != 0) {\n")
	g.pf("      return (null, TableRefuseReason.reservedNotZero);\n    }\n\n")
	g.pf("    // THE ALIGNMENT WORD is the one field the check COMPUTES WITH rather than\n")
	g.pf("    // only compares against — the data part begins at align_up(64, alignment)\n")
	g.pf("    // — so a word that is not an alignment rounds nothing and aligns nothing:\n")
	g.pf("    // at least the floor, at most the ceiling, a power of two, and a multiple\n")
	g.pf("    // of the ROOT's own alignof.\n")
	g.pf("    final alignment = tableCookRead64(view, at + 40);\n")
	g.pf("    if (alignment < %d ||\n        alignment > %d ||\n        alignment & (alignment - 1) != 0 ||\n        alignment %% %d != 0) {\n",
		ir.RegionAlignFloor, cookMaxAlign, ml.Align)
	g.pf("      return (null, TableRefuseReason.badAlignment);\n    }\n\n")
	g.pf("    // THE DATA OFFSET IS DERIVED, never a header field: a fact a reader\n")
	g.pf("    // computes is a fact two writers cannot disagree about.\n")
	g.pf("    final dataOffset = (%d + alignment - 1) & ~(alignment - 1);\n\n", cookHeaderBytes)
	g.pf("    // THE TWO PART LENGTHS against the length the caller passed. The whole\n")
	g.pf("    // file is dataOffset + data + attribution, and a size that is not exactly\n")
	g.pf("    // that refuses: a truncated file and a file with trailing bytes are the\n")
	g.pf("    // same refusal. A length past 2^63 arrives negative and is not a length.\n")
	g.pf("    //\n")
	g.pf("    // THE DATA PART MUST HOLD THE ROOT. The part lengths frame the FILE; they\n")
	g.pf("    // do not say the region is at least sizeof(root). Without this a forged\n")
	g.pf("    // short data part describes a root partly outside the file, and a\n")
	g.pf("    // match-and-point reader would hand back storage the caller never gave\n")
	g.pf("    // it — the one way this design could read past the length it was passed.\n")
	g.pf("    final dataLength = tableCookRead64(view, at + 24);\n")
	g.pf("    final attribution = tableCookRead64(view, at + 32);\n")
	g.pf("    if (dataLength < 0 ||\n        attribution < 0 ||\n")
	g.pf("        dataOffset > length ||\n        dataLength > length - dataOffset ||\n")
	g.pf("        attribution != length - dataOffset - dataLength ||\n        dataLength < %d) {\n", ml.Size)
	g.pf("      return (null, TableRefuseReason.truncated);\n    }\n\n")
	g.pf("    // THE ALIGNMENT OF THE BASE, on the offset the caller placed the file at —\n")
	g.pf("    // the only base a Dart consumer holds (§19.1's rule, in this language's\n")
	g.pf("    // currency).\n")
	g.pf("    if (at %% alignment != 0) {\n      return (null, TableRefuseReason.unalignedBase);\n    }\n\n")
	// split unless it fits the formatter's 80 columns (tables-dart-clean)
	ret := fmt.Sprintf("    return (%sCook(bytes, view, at + dataOffset, dataLength), TableRefuseReason.ok);", name)
	if len(ret) > 80 {
		ret = fmt.Sprintf("    return (\n      %sCook(bytes, view, at + dataOffset, dataLength),\n      TableRefuseReason.ok,\n    );", name)
	}
	g.pf("%s\n  }\n\n", ret)
}

func (g *cookGen) emitCookAt(st *ir.Struct) {
	g.pf("  // A REFERENCE IS DEREFERENCED THROUGH at, and it is the same encoding in a\n")
	g.pf("  // locked region and an opened cook (§6.3): the slot is eight bytes, SIGNED,\n")
	g.pf("  // self-relative from the SLOT'S OWN position, so a deref is one add and\n")
	g.pf("  // NULL IS A DELTA OF ZERO.\n")
	g.pf("  //\n")
	g.pf("  // It takes the SLOT's offset and not its value, because a self-relative\n")
	g.pf("  // delta means nothing without the position it is relative to. The size is\n")
	g.pf("  // the pointee record's full extent, bounded before any target addition.\n")
	g.pf("  // The slot itself must hold eight bytes inside the region. It answers\n")
	g.pf("  // the TARGET's offset, TableCookRef.none for a null, and\n")
	g.pf("  // TableCookRef.outside for a slot or target record outside the region — C++\n")
	g.pf("  // need not check and Dart does, because the read after it would be an\n")
	g.pf("  // escaping RangeError rather than a refusal.\n")
	g.pf("  int at(int slot, int size) {\n")
	g.pf("    if (size < 0 || length < 8 || slot < region || slot - region > length - 8) {\n")
	g.pf("      return TableCookRef.outside;\n    }\n")
	g.pf("    final delta = view.getInt64(slot, Endian.little);\n")
	g.pf("    if (delta == 0) {\n      return TableCookRef.none;\n    }\n")
	g.pf("    final relative = slot - region;\n")
	g.pf("    // Bound the delta before addition: the entire record must fit (§7).\n")
	g.pf("    if (delta < -relative || delta > length - size - relative) {\n")
	g.pf("      return TableCookRef.outside;\n    }\n")
	g.pf("    return slot + delta;\n  }\n\n")
}

// emitCookDescriptors is the cook's reflective half: one record's layout as
// DATA, so a consumer — or a gate — walks a cooked region without a
// hand-written record per table and with nothing to maintain when a field is
// added. It is the same mechanism §8 gives the wire and §19.2 gives the block,
// over the facts a cooked region actually has: an offset, a size, a pointer
// edge, a count companion, and the record a field names.
func (g *cookGen) emitCookDescriptors(st *ir.Struct) {
	g.cookNamed = map[string]bool{}
	cookWalk(g.unit, st.Name, func(n string, ml *ir.MemberLayout) {
		for _, fl := range ml.Fields {
			if fl.Field.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
				g.cookNamed[ref.Name] = true
			}
		}
	})
	g.pf("  // this root's cook descriptors: CONSTANT data, so a reflective walk costs a\n")
	g.pf("  // lookup and not a parse. Every record the region can hold hangs off the\n")
	g.pf("  // field column, so a walker reaches the whole graph from the root — and\n")
	g.pf("  // they are MEMBERS, so the graph claims one name and that name is the\n")
	g.pf("  // root's (docs/SPEC-TABLES.md §11).\n")
	var records []string
	cookWalk(g.unit, st.Name, func(n string, _ *ir.MemberLayout) { records = append(records, n) })
	sort.Strings(records)
	for _, r := range records {
		g.emitCookRecordDescriptor(st.Name, r)
	}
	// the EDGE that keeps the graph const: the DESCRIPTOR graph can be cyclic —
	// a record's field column can name its own record — so a field names its
	// record through a static method rather than by value. (A cooked REGION is
	// never cyclic: `schema cook` refuses one by name, §3.1.) Only
	// the records something POINTS AT or NESTS need one — a root nothing names
	// is reached as `type` and no field column carries it.
	for _, r := range records {
		if !g.cookNamed[r] {
			continue
		}
		g.pf("  static TableCookInfo _%s() => %s;\n",
			dartCookInfoSymbol(st.Name, r), dartCookInfoSymbol(st.Name, r))
	}
	g.trimBlank()
}

func (g *cookGen) emitCookRecordDescriptor(owner, record string) {
	ml := g.cook.members[record]
	if ml == nil {
		return
	}
	g.pf("  static const TableCookInfo %s = TableCookInfo(\n", dartCookInfoSymbol(owner, record))
	g.pf("    name: '%s',\n", record)
	g.pf("    size: %d,\n", ml.Size)
	g.pf("    align: %d,\n", ml.Align)
	g.pf("    fields: <TableCookFieldInfo>[\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		names := "null"
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				names = "_" + dartCookInfoSymbol(owner, ref.Name)
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
		// A `*string`/`*bytes` (docs/SPEC-TABLES.md §2.5) is a POINTER, and a
		// pointer's cooked slot is one eight-byte reference — it has no inline
		// length companion and no inline buffer, so the pieces it would index
		// are not there. Only the INLINE string/bytes takes a count companion.
		if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			countOffset = pieces[1].Offset
		}
		isArray := f.KeyEnum != "" || f.Array != ir.ArrayNone
		bound := int64(1)
		elemSize := fl.Size
		if isArray {
			bound = f.ArrayBound
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			if bound > 0 {
				elemSize = pieces[0].Size / bound
			} else {
				// AN UNBOUNDED ARRAY (docs/SPEC-TABLES.md §2.9) carries no
				// inline slot count: the field is the sixteen-byte TableList
				// reference and there is no per-element stride to divide out,
				// so the slot is the reference and it is not an inline array.
				// This is the same row ir.BlockFieldOf states for the case,
				// and it keeps an unbounded []T from taking the descriptor
				// down with a divide by its absent bound.
				elemSize = pieces[0].Size
				isArray = false
			}
		}
		if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer {
			// the COUNT COMPANION of a string or a `bytes` bounds a walker just
			// as an array's does (§7.4), and the bound is the declared length;
			// a POINTER string carries no inline buffer and takes the
			// reference row above instead
			bound = f.Type.Size
			isArray = false
		}
		presentOffset := int64(-1)
		if f.Type.Optional && !f.Type.Pointer {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			presentOffset = pieces[len(pieces)-1].Offset
		}
		g.pf("      TableCookFieldInfo(\n")
		g.pf("        name: '%s',\n", f.Name)
		g.pf("        offset: %d,\n", fl.Offset)
		g.pf("        size: %d,\n", fl.Size)
		g.pf("        elemSize: %d,\n", elemSize)
		g.pf("        isArray: %v,\n", isArray)
		g.pf("        arrayBound: %d,\n", bound)
		g.pf("        isPointer: %v,\n", f.Type.Pointer)
		g.pf("        countOffset: %d,\n", countOffset)
		g.pf("        presentOffset: %d,\n", presentOffset)
		g.pf("        storage: TableCookStorage.%s,\n", dartCookStorageKind(f))
		g.pf("        record: %s,\n", names)
		g.pf("      ),\n")
	}
	g.pf("    ],\n  );\n\n")
}

// dartCookInfoSymbol names one record's descriptor. They are MEMBERS of the
// cook class, so the whole graph claims exactly one name — the root's.
func dartCookInfoSymbol(owner, record string) string {
	if owner == record {
		return "type"
	}
	return "record" + record
}

// dartCookStorageKind is what a cooked SLOT holds, which is not always what the
// wire carries: an ENUM slot holds the ORDINAL at the enum's own derived
// storage width (§7.2), where the wire carries the variant-name hash. So the
// descriptors name the storage rather than reuse a wire kind, and a walker
// reads a slot with the width `elemSize` gives and the signedness this names.
func dartCookStorageKind(f *ir.Field) string {
	if f.Type.Pointer {
		return "reference"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "boolean"
	case ir.TFloat32, ir.TFloat64:
		return "float"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TBits:
		return "unsigned"
	case ir.TInt:
		if f.Type.Signed {
			return "signed"
		}
		return "unsigned"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "unsigned"
		case *ir.Struct:
			return "record"
		}
	}
	return "record"
}

// trimBlank drops a trailing blank line from the body — the emitter's
// per-member spacing meeting a closing brace, which `dart format` writes with
// no blank between.
func (g *cookGen) trimBlank() {
	text := strings.TrimRight(g.body.String(), "\n")
	g.body.Reset()
	g.body.WriteString(text)
	g.body.WriteString("\n")
}

// emitCookMapAccessors writes a cooked record's MAP field as its ENTRIES, the
// way the Go cook exposes them (docs/SPEC-TABLES.md §2.8, §7.2): the slot is an
// int64 SELF-RELATIVE delta to the entry array and an int32 count, and the
// entries are by-value records in ASCENDING KEY ORDER inside the holder's node
// extent. So the field is three accessors and no scalar:
//
//   - `<f>Count`, the live count — 0 for an ABSENT map (a null delta) and for
//     an EMPTY one, and 0 for a forged slot whose array would leave the view,
//     because a reader that throws on hostile bytes is not one that refuses
//     them (the cook's own rule, above);
//   - `<f>At(index, into)`, entry `index` of [0, count) moved onto the lent
//     `<Entry>Row` cursor, in the order Go's `<F>.At(i)` answers;
//   - `<f>Find(key, into)`, §2.8's `Find`: a binary search over the entries
//     where they lie, `floor(log2 n) + 1` key compares, no allocation, null
//     for a key the map does not hold. Dart has no pointer to hand back to a
//     scalar value, so the handle is the ENTRY cursor in every case and the
//     value is read through it — §7's per-backend spelling (the Dart column).
//
// A KEY is an integer or a bounded string (§2.8; an enum key is refused by
// name). An integer key is the Dart `int`, compared at its signedness — a
// uint64 by flipping the sign bit, since Dart's int is signed; a string key is
// its UTF-8 bytes (`List<int>`), ordered bytewise over the used length with the
// shorter first, which is the order the cook was written in.
func (g *blockGen) emitCookMapAccessors(f *ir.Field, offset int64, ind, origin string) {
	name := accessorName(f, false)
	entry := f.MapEntry.Name + "Row"
	el := ir.RecordLayout(g.unit, f.MapEntry)
	key := ir.MapKeyField(f)
	keyOffset := el.Fields[0].Offset
	g.needRow(f.MapEntry.Name)
	g.pf("%s// a MAP: the slot is a self-relative delta to the ENTRY ARRAY and its\n", ind)
	g.pf("%s// count, the entries in ascending key order (docs/SPEC-TABLES.md §2.8,\n", ind)
	g.pf("%s// §7.2). The count is 0 for an absent or an empty map, and for an array\n", ind)
	g.pf("%s// that would leave the view.\n", ind)
	g.pf("%sint get %sCount {\n", ind, name)
	g.pf("%s  final slot = %s + %d;\n", ind, origin, offset)
	g.pf("%s  final delta = view.getInt64(slot, Endian.little);\n", ind)
	g.pf("%s  final count = view.getInt32(slot + 8, Endian.little);\n", ind)
	g.pf("%s  if (count <= 0 || delta == 0 || slot + delta < 0) {\n", ind)
	g.pf("%s    return 0;\n%s  }\n", ind, ind)
	g.arrowBlock(ind+"  ", fmt.Sprintf("return count > (view.lengthInBytes - slot - delta) ~/ %d", el.Size), "? 0", ": count;")
	g.pf("%s}\n\n", ind)

	g.pf("%sint get _%sEntries {\n", ind, name)
	g.pf("%s  final slot = %s + %d;\n", ind, origin, offset)
	g.pf("%s  return slot + view.getInt64(slot, Endian.little);\n%s}\n\n", ind, ind)

	g.pf("%s/// One entry, moved onto the cursor the caller lends; `index` runs over\n", ind)
	g.pf("%s/// [0, count) in ascending key order.\n", ind)
	g.signature(ind, entry, name+"At", "int index", entry+" into")
	g.pf("%s  into.at = _%sEntries + index * %d;\n", ind, name, el.Size)
	g.pf("%s  return into;\n%s}\n\n", ind, ind)

	keyType := "int"
	if key.Type.Kind == ir.TString {
		keyType = "List<int>"
	}
	g.pf("%s/// `Find` (§2.8): a binary search over the entries where they lie, no\n", ind)
	g.pf("%s/// allocation; the entry moved onto `into`, or null for a key it lacks.\n", ind)
	g.signature(ind, entry+"?", name+"Find", keyType+" key", entry+" into")
	g.pf("%s  final count = %sCount;\n", ind, name)
	g.pf("%s  final first = _%sEntries;\n", ind, name)
	g.pf("%s  var lo = 0;\n%s  var hi = count;\n", ind, ind)
	g.pf("%s  while (lo < hi) {\n", ind)
	g.pf("%s    final mid = lo + ((hi - lo) >> 1);\n", ind)
	g.pf("%s    if (_%sOrder(first + mid * %d, key) < 0) {\n", ind, name, el.Size)
	g.pf("%s      lo = mid + 1;\n%s    } else {\n%s      hi = mid;\n%s    }\n%s  }\n", ind, ind, ind, ind, ind)
	g.pf("%s  if (lo == count || _%sOrder(first + lo * %d, key) != 0) {\n", ind, name, el.Size)
	g.pf("%s    return null;\n%s  }\n", ind, ind)
	g.pf("%s  into.at = first + lo * %d;\n", ind, el.Size)
	g.pf("%s  return into;\n%s}\n\n", ind, ind)

	// the KEY ORDER the cook was written in (§2.8): an entry's key against the
	// probe, negative when the entry's sorts first
	g.pf("%sint _%sOrder(int entry, %s key) {\n", ind, name, keyType)
	if key.Type.Kind == ir.TString {
		pieces := ir.FieldPieces(g.unit, key, keyOffset)
		g.pf("%s  var used = view.getInt32(entry + %d, Endian.little);\n", ind, pieces[1].Offset)
		g.pf("%s  if (used < 0 || used > %d) {\n%s    used = 0;\n%s  }\n", ind, key.Type.Size, ind, ind)
		g.pf("%s  for (var i = 0; i < used && i < key.length; i++) {\n", ind)
		g.pf("%s    final order = view.getUint8(entry + %d + i) - key[i];\n", ind, keyOffset)
		g.pf("%s    if (order != 0) {\n%s      return order;\n%s    }\n%s  }\n", ind, ind, ind, ind)
		g.pf("%s  return used - key.length;\n", ind)
	} else {
		g.pf("%s  final stored = %s;\n", ind, g.readAt(key, fmt.Sprintf("entry + %d", keyOffset)))
		if !key.Type.Signed && key.Type.Width == 64 {
			// Dart's int is signed: a uint64 orders by its bits with the sign flipped
			g.pf("%s  return (stored ^ (1 << 63)).compareTo(key ^ (1 << 63));\n", ind)
		} else {
			g.pf("%s  return stored.compareTo(key);\n", ind)
		}
	}
	g.pf("%s}\n\n", ind)
}

// emitCookListAccessors writes a cooked record's LIST field (docs/SPEC-TABLES.md
// §2.9, §7.2) as the span its elements are, in place in the holder's node
// extent: `<f>Count`, zero for an empty list and for a span that would leave
// the view, and `<f>At(index)` — a record element moved onto the cursor the
// caller lends, a scalar element read where it lies, and a reference or union
// element answered as its slot's offset, the pointer field's own currency.
func (g *blockGen) emitCookListAccessors(f *ir.Field, offset int64, ind, origin string) {
	name := accessorName(f, false)
	size, _ := ir.ListElementLayout(g.unit, f)
	g.pf("%s// a LIST: the slot is a self-relative delta to the ELEMENT ARRAY and its\n", ind)
	g.pf("%s// count, the elements in place (docs/SPEC-TABLES.md §2.9, §7.2). The\n", ind)
	g.pf("%s// count is 0 for an empty list, and for an array that would leave the view.\n", ind)
	g.pf("%sint get %sCount {\n", ind, name)
	g.pf("%s  final slot = %s + %d;\n", ind, origin, offset)
	g.pf("%s  final delta = view.getInt64(slot, Endian.little);\n", ind)
	g.pf("%s  final count = view.getInt32(slot + 8, Endian.little);\n", ind)
	g.pf("%s  if (count <= 0 || delta == 0 || slot + delta < 0) {\n", ind)
	g.pf("%s    return 0;\n%s  }\n", ind, ind)
	g.arrowBlock(ind+"  ", fmt.Sprintf("return count > (view.lengthInBytes - slot - delta) ~/ %d", size), "? 0", ": count;")
	g.pf("%s}\n\n", ind)

	g.pf("%sint get _%sElements {\n", ind, name)
	g.pf("%s  final slot = %s + %d;\n", ind, origin, offset)
	g.pf("%s  return slot + view.getInt64(slot, Endian.little);\n%s}\n\n", ind, ind)

	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	at := fmt.Sprintf("_%sElements + index * %d", name, size)
	switch {
	case isStructRef(f.Type):
		row := f.Type.Name + "Row"
		g.needRow(f.Type.Name)
		g.pf("%s/// One element, moved onto the cursor the caller lends; `index` runs\n", ind)
		g.pf("%s/// over [0, count).\n", ind)
		g.signature(ind, row, name+"At", "int index", row+" into")
		g.pf("%s  into.at = %s;\n", ind, at)
		g.pf("%s  return into;\n%s}\n\n", ind, ind)
	case f.Type.Pointer || unionRef(f) != nil:
		g.arrow(ind, fmt.Sprintf("int %sAt(int index)", name), at)
	default:
		g.scalarGetter(ind, &elem, fmt.Sprintf("%s %sAt(int index)", dartScalarType(f.Type), name), at)
	}
}

// signature writes one method head the way `dart format` does: on one line when
// it fits in 80 columns, otherwise one parameter per line with a trailing comma,
// and the result type on a line of its own when even the head does not fit.
func (g *blockGen) signature(ind, result, name string, params ...string) {
	one := fmt.Sprintf("%s%s %s(%s) {", ind, result, name, strings.Join(params, ", "))
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	if len(ind)+len(result)+len(name)+2 > 80 {
		// the head alone does not fit: the result type takes its own line
		g.pf("%s%s\n%s%s(\n", ind, result, ind, name)
	} else {
		g.pf("%s%s %s(\n", ind, result, name)
	}
	for _, p := range params {
		g.pf("%s  %s,\n", ind, p)
	}
	g.pf("%s) {\n", ind)
}

// arrowBlock writes `head then : else` as one statement, broken where `dart
// format` breaks a conditional that does not fit in 80 columns.
func (g *blockGen) arrowBlock(ind, head, then, otherwise string) {
	one := fmt.Sprintf("%s%s %s %s", ind, head, then, otherwise)
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s\n%s    %s\n%s    %s\n", ind, head, ind, then, ind, otherwise)
}

// cookDeclFile is the file a cooked record's cursor is emitted in. A map's
// generated ENTRY is declared where its holder is (§2.8): the checker records
// no file for the entry of a map that is itself a map's value, so it is read
// off the holder, up the MapEntryOf chain ("Holder.field").
func cookDeclFile(u *ir.Unit, record string) string {
	for range 64 {
		if base := u.DeclFile[record]; base != "" {
			return base
		}
		st := cookMember(u, record)
		if st == nil || st.MapEntryOf == "" {
			return ""
		}
		record, _, _ = strings.Cut(st.MapEntryOf, ".")
	}
	return ""
}
