// THE COOKED FORM in JavaScript (docs/SPEC-TABLES.md §7): the READ side, emitted
// ON THE SIDE into <Base>Cook.js.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format, not a wire. Tooling writes a
// region for one build version and that build points at it: the header is
// matched, the root is the record at the data part's base, and nothing else
// happens. No walk, no fix-up and no allocation beyond one handle — which is
// what makes Open O(1) in the file's size, the bar the scale §7 is built for
// asks for.
//
// NOTHING DECLARES IT, exactly as nothing declares the block form. Every table
// gets an Open, a consumer imports this module only if it opens a cook, and
// <Base>Table.js carries not one symbol of it.
//
// A COOKED RECORD IS THE BLITTABLE ROW. The region is laid out by §20.3's C ABI
// model, which is the same model the block form's records are read from — so
// the two accelerators share one set of `<Name>Row` accessor objects rather
// than growing a second reader. A record the block form already emits is
// emitted THERE and not again here.
//
// A REFERENCE IS EIGHT BYTES, SIGNED, SELF-RELATIVE from the slot's own
// position, and NULL IS ZERO (§6.3). JavaScript reads it as a BigInt because
// that is the only exact sixty-four-bit integer it has, and converts once, at
// the resolution, after bounding the target inside the region: a delta that
// leaves the region is a REFUSAL, where C++ simply trusts the bytes. That is
// not a contract this port invented — it is what the contract "a read never
// escapes the buffer" costs in a language where reading past a view throws.
//
// ALLOCATION: one handle per Open, one BigInt per reference resolved, nothing
// per field. The bytes belong to the consumer.
package jstable

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

// cookMaxAlign is the ceiling on the header's `alignment` word (§7).
const cookMaxAlign = int64(64)

// generateCookFiles emits <Base>Cook.js for every file of a unit that declares
// a table. A table this backend cannot open still gets a line saying which and
// why, never a silent absence.
func generateCookFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(u.Tables) == 0 {
		return out, nil
	}
	ck := cookUnitOf(u)
	if len(ck.tables) == 0 && len(ck.skipped) == 0 {
		return out, nil
	}
	// THE COOK HOME is <Package>Cook.js — one home per unit, named by the
	// package, independent of file order (runtimeHome, jstable.go). The shared
	// cook runtime and every record object the BLOCK form does not already emit
	// land there, once per unit.
	home := runtimeHome(u)
	runtimeWritten := false
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &cookGen{unit: u, file: f, base: f.Base, cook: ck, blocks: blocks, home: f.Base == home,
			homeBase: home, imports: map[string]map[string]bool{}}
		g.emit()
		if g.home {
			runtimeWritten = true
		}
		out[f.Base+"Cook.js"] = g.assemble()
	}
	// No file of the unit is named for the package, so the home is emitted for
	// the unit rather than for a file.
	if !runtimeWritten {
		g := &cookGen{unit: u, base: home, cook: ck, blocks: blocks, home: true,
			homeBase: home, imports: map[string]map[string]bool{}}
		g.emit()
		out[home+"Cook.js"] = g.assemble()
	}
	return out, nil
}

// ---- the unit's cook surface ----

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
// gets one — with one absence this backend states rather than hides: §7.2's
// cook descriptors carry no UNION column, so a closure with a union has no
// reflective read here. Inventing a column would be inventing a contract, and
// a port does not; it is a named follow-on, exactly as it is in C#.
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

// cookableClosure answers whether this backend can read one table's whole
// cooked region, and why not when it cannot.
func cookableClosure(u *ir.Unit, st *ir.Struct) string {
	seen := map[string]bool{}
	var walk func(string) string
	walk = func(name string) string {
		if seen[name] {
			return ""
		}
		seen[name] = true
		m := cookMember(u, name)
		if m == nil {
			return ""
		}
		for _, f := range m.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return name + "." + f.Name + " is a union, and §7.2's cook descriptors carry no union column — a reader that invented one would be inventing a contract"
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
	unit     *ir.Unit
	file     *ir.File // nil for a runtime home the unit has no file for
	base     string   // this module's own basename
	cook     *cookUnit
	blocks   *ir.BlockUnit
	home     bool
	homeBase string
	body     strings.Builder
	imports  map[string]map[string]bool
}

func (g *cookGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

func (g *cookGen) need(base string, symbols ...string) {
	if base == "" || base == g.base+"Cook" {
		return
	}
	set := g.imports[base]
	if set == nil {
		set = map[string]bool{}
		g.imports[base] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
}

func (g *cookGen) needHome(symbols ...string) { g.need(g.homeBase+"Cook", symbols...) }

// blockHasRecord reports whether the BLOCK form already emits <Name>Row for
// this unit, in which case the cook imports that one rather than emitting a
// second: one module-scope name cannot be two objects, and a cooked record IS
// the blittable row.
func (g *cookGen) blockHasRecord(name string) bool {
	return g.blocks != nil && blockHome(g.unit, g.blocks) != "" && g.blocks.Layout(name) != nil
}

// blockOwnsRuntime reports whether the unit's BLOCK form already exports the
// shared constants — BuildVersion above all. Exactly one module of the unit
// defines each.
func (g *cookGen) blockOwnsRuntime() bool {
	return g.blocks != nil && blockHome(g.unit, g.blocks) != ""
}

// recordModule names the module that emits one record's accessor object.
func (g *cookGen) recordModule(name string) string {
	if g.blockHasRecord(name) {
		return blockHome(g.unit, g.blocks) + "Block"
	}
	return g.homeBase + "Cook"
}

func (g *cookGen) emit() {
	if g.home {
		g.pf("%s", cookRuntime(ir.BuildVersion(g.unit), !g.blockOwnsRuntime()))
		if g.blockOwnsRuntime() {
			g.need(blockHome(g.unit, g.blocks)+"Block", "BuildVersion")
		}
		refs := recordRefs{}
		for _, name := range g.cook.order {
			if g.blockHasRecord(name) {
				continue
			}
			if ml := g.cook.members[name]; ml != nil {
				emitRecordObject(&g.body, g.unit, name, ml, refs)
			}
		}
	}
	if g.file == nil {
		return // the runtime home the unit has no file for: nothing declares here
	}
	for _, st := range g.file.Tables {
		if g.cook.opens(st.Name) {
			g.emitCookHandle(st)
			continue
		}
		g.pf("// table %s has NO JavaScript cook Open: %s (docs/SPEC-TABLES.md §7, §7.2).\n", st.Name, g.cook.skipped[st.Name])
		g.pf("// Its wire (§3) and its cook are unaffected — only this backend's reader is\n")
		g.pf("// absent, and it is absent by construction rather than by refusal.\n\n")
	}
}

func (g *cookGen) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file, g.unit))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the COOKED FORM (docs/SPEC-TABLES.md §7): the READ half.\n", g.unit.Package)
	h.WriteString("//\n")
	if g.home {
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Cook.js, one home per unit, named by\n")
		h.WriteString("// the package and independent of file order (docs/SPEC-TABLES.md §19.2).\n")
		h.WriteString("//\n")
	}
	h.WriteString("// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format and not a wire. Tooling writes\n")
	h.WriteString("// a region for one BUILD VERSION and that build points at it: Open matches the\n")
	h.WriteString("// header and returns the root where it lies. There is no walk, no fix-up and no\n")
	h.WriteString("// allocation beyond one handle, which is what makes Open O(1) in the file's size.\n")
	h.WriteString("//\n")
	h.WriteString("// A COOK IS TRUSTED INPUT, LOADED FROM DISK. Open's checks are IDENTITY checks —\n")
	h.WriteString("// is this file for THIS build — and not a trust boundary: there is NO PER-NODE\n")
	h.WriteString("// VALIDATION AT LOAD, ever. A file whose provenance you doubt is `schema\n")
	h.WriteString("// cook-check`'s business, run by a person, once, offline.\n")
	h.WriteString("//\n")
	h.WriteString("// THIS MODULE AND <Base>Block.js ARE A PAIR where the unit has both: they share\n")
	h.WriteString("// one set of <Name>Row accessor objects, because a cooked record IS the\n")
	h.WriteString("// blittable row.\n")
	h.WriteString("//\n")
	h.WriteString("// A refusal is a null handle, and a reference that leaves the region resolves to\n")
	h.WriteString("// a refusal rather than to a read: no exception leaves this module, whatever the\n")
	h.WriteString("// bytes carry.\n\n")
	if len(g.imports) > 0 {
		bases := make([]string, 0, len(g.imports))
		for b := range g.imports {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		for _, b := range bases {
			syms := make([]string, 0, len(g.imports[b]))
			for s := range g.imports[b] {
				syms = append(syms, s)
			}
			sort.Strings(syms)
			fmt.Fprintf(&h, "import { %s } from \"./%s.js\";\n", strings.Join(syms, ", "), b)
		}
		h.WriteString("\n")
	}
	if !g.home && len(g.imports[g.homeBase+"Cook"]) > 0 {
		// THE HOME's surface is RE-EXPORTED from every module of the unit that
		// uses it. It is DEFINED once — an ES module is file-scoped, so a
		// second copy would be a second, unequal object — but a consumer that
		// imports a table's handle needs the record objects it hands offsets
		// into, and making it import a second module to read a row would be a
		// papercut with no property behind it. One module, one whole surface.
		syms := make([]string, 0, len(g.imports[g.homeBase+"Cook"]))
		for s := range g.imports[g.homeBase+"Cook"] {
			syms = append(syms, s)
		}
		sort.Strings(syms)
		h.WriteString("// The unit's shared cook surface, defined once in its runtime home and\n")
		h.WriteString("// re-exported here: one module, one whole surface.\n")
		fmt.Fprintf(&h, "export { %s } from \"./%sCook.js\";\n\n", strings.Join(syms, ", "), g.homeBase)
	}
	h.WriteString(g.body.String())
	return []byte(h.String())
}

// ---- the cook handle ----

func (g *cookGen) emitCookHandle(st *ir.Struct) {
	name := st.Name
	ml := g.cook.members[name]
	align := g.cook.align
	g.needHome("TableCookLayout", "TableCookMagic", "TableCookByteOrder")
	if g.blockOwnsRuntime() {
		g.need(blockHome(g.unit, g.blocks)+"Block", "BuildVersion")
	} else {
		g.needHome("BuildVersion")
	}
	g.pf("// %s's cook: a view and a length, and then the root where it lies. Opening\n", name)
	g.pf("// one is a HEADER MATCH and no copy; a reference is one add (docs/SPEC-TABLES.md §7).\n")
	g.pf("//\n")
	g.pf("// `Cook` is a CLAIMED suffix (§11). C++ spells the same claimed verbs as free\n")
	g.pf("// functions — %sOpen, %sAt — and this port spells them as MEMBERS of this\n", name, name)
	g.pf("// object, which is the rule the block form already follows for its accessors.\n")
	g.pf("//\n")
	g.pf("// THE MEMORY IS THE CONSUMER'S: the view must stay valid for as long as this\n")
	g.pf("// handle or anything reached through it is used.\n")
	g.pf("export const %sCook = (() => {\n", name)
	records := g.descriptorRecords(st)
	for _, r := range records {
		g.emitRecordDescriptor(r)
	}
	g.pf("  const root = %s;\n\n", cookInfoSymbol(st.Name))
	g.pf("  let layoutState = -1;\n")
	g.pf("  function Layout() {\n")
	g.pf("    if (layoutState < 0) { layoutState = TableCookLayout.verify(root) ? 1 : 0; }\n")
	g.pf("    return layoutState === 1;\n")
	g.pf("  }\n\n")
	g.emitOpen(st, ml, align)
	g.emitAt(st)
	g.pf("  return Object.freeze({\n")
	g.pf("    // §7.1's constants, so a consumer reading this module has the facts and\n")
	g.pf("    // not a description of them.\n")
	g.pf("    RegionAlignment: %d, // the greatest alignof in the region, floor eight\n", align)
	g.pf("    RootSize: %d,\n", ml.Size)
	g.pf("    RootAlign: %d,\n", ml.Align)
	g.pf("    Open, At, Layout, Type: root,\n")
	g.pf("  });\n})();\n\n")
}

func (g *cookGen) emitOpen(st *ir.Struct, ml *ir.MemberLayout, align int64) {
	g.pf("  // Open checks the header and POINTS, and this is the WHOLE check\n")
	g.pf("  // (docs/SPEC-TABLES.md §7): the magic, the byte order it establishes, the build\n")
	g.pf("  // version, every RESERVED word zero, the region ALIGNMENT the header names,\n")
	g.pf("  // the two part lengths against the length the caller passed — a truncated\n")
	g.pf("  // file refuses — the ROOT's own storage inside the data part, and the\n")
	g.pf("  // alignment of the base. Nothing per node, ever: that is what makes this\n")
	g.pf("  // O(1) in the file's size.\n")
	g.pf("  //\n")
	g.pf("  // EVERY NUMBER BELOW COMES OUT OF THE FILE, so all of the arithmetic is\n")
	g.pf("  // BIGINT and each term is BOUNDED BEFORE IT IS ADDED. A Number would have\n")
	g.pf("  // lost the low bits of a forged length before the comparison ran, which is\n")
	g.pf("  // exactly what the comparison after it was supposed to catch.\n")
	g.pf("  //\n")
	g.pf("  // THE BASE'S ALIGNMENT IS THE VIEW'S byteOffset: JavaScript has no addresses,\n")
	g.pf("  // so where a view starts inside its buffer is the one alignment fact a\n")
	g.pf("  // consumer can state, and it is what this measures.\n")
	g.pf("  function Open(bytes) {\n")
	g.pf("    if (!Layout()) { return null; }\n")
	g.pf("    if (bytes === null || bytes === undefined || bytes.length < %d) { return null; }\n", cookHeaderBytes)
	g.pf("    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);\n")
	g.pf("    const extent = BigInt(bytes.length);\n\n")
	g.pf("    // THE MAGIC, before anything else: it is what establishes the byte order\n")
	g.pf("    // every other header word is written in. A cook of the other order reads\n")
	g.pf("    // back this constant byte-reversed and refuses HERE, rather than reaching\n")
	g.pf("    // a fix-up pass this design does not have.\n")
	g.pf("    if (view.getBigUint64(0, true) !== TableCookMagic) { return null; }\n")
	g.pf("    // and the ORDER WORD does the other job: it RECORDS which order wrote the\n")
	g.pf("    // file, so a refusal names the order rather than inferring it. A file\n")
	g.pf("    // whose magic matched and whose order word did not is corrupt, and there\n")
	g.pf("    // is no reading that recovers it.\n")
	g.pf("    if (view.getBigUint64(16, true) !== TableCookByteOrder) { return null; }\n")
	g.pf("    // THE BUILD VERSION: under the match-and-point rule a matching id means\n")
	g.pf("    // Open checks nothing further, so it is the sole guard between this\n")
	g.pf("    // runtime and a foreign region (§20).\n")
	g.pf("    if (view.getBigUint64(8, true) !== BuildVersion) { return null; }\n")
	g.pf("    // THE RESERVED WORDS: a non-zero one means a writer used a form this\n")
	g.pf("    // build does not understand, and Open refuses rather than ignoring it.\n")
	g.pf("    if (view.getBigUint64(48, true) !== 0n) { return null; }\n")
	g.pf("    if (view.getBigUint64(56, true) !== 0n) { return null; }\n\n")
	g.pf("    // THE ALIGNMENT WORD is the one field the check COMPUTES WITH rather than\n")
	g.pf("    // only compares against — the data part begins at align_up(64, alignment)\n")
	g.pf("    // and the base is measured against it — so a word that is not an\n")
	g.pf("    // alignment rounds nothing and aligns nothing.\n")
	g.pf("    const alignment = view.getBigUint64(40, true);\n")
	g.pf("    if (alignment < %dn || alignment > %dn) { return null; }\n", ir.RegionAlignFloor, cookMaxAlign)
	g.pf("    if ((alignment & (alignment - 1n)) !== 0n) { return null; } // a power of two\n")
	g.pf("    if ((alignment %% %dn) !== 0n) { return null; }            // and a multiple of the ROOT's own alignof\n\n", ml.Align)
	g.pf("    // THE DATA OFFSET IS DERIVED, never a header field: a fact a reader\n")
	g.pf("    // computes is a fact two writers cannot disagree about.\n")
	g.pf("    const dataOffset = (%dn + alignment - 1n) & ~(alignment - 1n);\n\n", cookHeaderBytes)
	g.pf("    // THE TWO PART LENGTHS against the length the caller passed. The whole\n")
	g.pf("    // file is dataOffset + data + attribution, and a size that is not exactly\n")
	g.pf("    // that refuses: a truncated file and a file with trailing bytes are the\n")
	g.pf("    // same refusal. Each term is bounded before it is added.\n")
	g.pf("    const dataLength = view.getBigUint64(24, true);\n")
	g.pf("    const attribution = view.getBigUint64(32, true);\n")
	g.pf("    if (dataLength > extent || attribution > extent - dataLength) { return null; }\n")
	g.pf("    if (dataOffset > extent - dataLength - attribution) { return null; }\n")
	g.pf("    if (dataOffset + dataLength + attribution !== extent) { return null; }\n\n")
	g.pf("    // THE DATA PART MUST HOLD THE ROOT. The part lengths frame the FILE; they\n")
	g.pf("    // do not say the region is at least sizeof(root). Without this a forged\n")
	g.pf("    // short data part describes a root partly outside the file, and a\n")
	g.pf("    // match-and-point reader would hand back storage the caller never gave it.\n")
	g.pf("    if (dataLength < %dn) { return null; }\n\n", ml.Size)
	g.pf("    // THE ALIGNMENT OF THE BASE. The header pads the data part to the region's\n")
	g.pf("    // alignment, and the alignment divides 64, so the derived data offset\n")
	g.pf("    // carries the property from the file's base to the region's.\n")
	g.pf("    if ((BigInt(bytes.byteOffset) %% alignment) !== 0n) { return null; }\n\n")
	g.pf("    // `region` is the DATA part's offset inside the caller's view: the root\n")
	g.pf("    // sits at region + 0, and every accessor takes an offset in these bytes.\n")
	g.pf("    return { Bytes: bytes, View: view, Region: Number(dataOffset), Length: Number(dataLength) };\n")
	g.pf("  }\n\n")
}

func (g *cookGen) emitAt(st *ir.Struct) {
	g.pf("  // A REFERENCE IS DEREFERENCED THROUGH At, and it is the same call in a\n")
	g.pf("  // locked region and an opened cook because they are the same encoding\n")
	g.pf("  // (§6.3): the slot is eight bytes, SIGNED, self-relative from the SLOT'S\n")
	g.pf("  // OWN position, so a deref needs no base offset, and NULL IS A DELTA OF\n")
	g.pf("  // ZERO.\n")
	g.pf("  //\n")
	g.pf("  // It takes the SLOT's own offset and not its value, because a self-relative\n")
	g.pf("  // delta means nothing without the position it is relative to.\n")
	g.pf("  //\n")
	g.pf("  // -1 IS NULL AND -2 IS A REFUSAL. C++ trusts the delta and adds it; a\n")
	g.pf("  // JavaScript read past a view throws, and an exception escaping a reader is\n")
	g.pf("  // the one thing this form may not do — so the target is bounded inside the\n")
	g.pf("  // region first, and a delta that leaves it is refused rather than followed.\n")
	g.pf("  // A consumer that must tell a null from a forgery has both answers.\n")
	g.pf("  //\n")
	g.pf("  // THE DELTA IS COMPOSED FROM TWO 32-BIT READS, not read as a BigInt.\n")
	g.pf("  // Every BigInt is an object and a deref is the cook's hottest line, so\n")
	g.pf("  // reading one here would allocate per EDGE FOLLOWED. The composition is\n")
	g.pf("  // exact wherever it can matter: a delta whose magnitude fits fifty-three\n")
	g.pf("  // bits composes losslessly, and one that does not is astronomically\n")
	g.pf("  // outside a region — a region is bounded by the length the caller passed —\n")
	g.pf("  // so it lands outside the bound and refuses, which is the same answer the\n")
	g.pf("  // exact arithmetic gives.\n")
	g.pf("  function At(cook, slot) {\n")
	g.pf("    const view = cook.View;\n")
	g.pf("    const low = view.getUint32(slot, true);\n")
	g.pf("    const high = view.getInt32(slot + 4, true); // SIGNED: the delta is\n")
	g.pf("    if (high === 0 && low === 0) { return -1; } // null\n")
	g.pf("    const target = slot + high * 4294967296 + low;\n")
	g.pf("    if (target < cook.Region || target >= cook.Region + cook.Length) { return -2; } // outside the region\n")
	g.pf("    return target;\n")
	g.pf("  }\n\n")
}

// ---- the descriptors ----

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
	g.need(g.recordModule(record), recordName(record))
	g.pf("  const %s = Object.freeze({\n", cookInfoSymbol(record))
	g.pf("    Name: %q, Size: %d, Align: %d, NumFields: %d, Row: %s,\n",
		record, ml.Size, ml.Align, len(ml.Fields), recordName(record))
	g.pf("    Fields: Object.freeze([\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		names := "null"
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				names = "() => " + cookInfoSymbol(ref.Name)
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
		g.pf("      { Name: %q, Offset: %d, Size: %d, ElemSize: %d, IsArray: %t, ArrayBound: %d, IsPointer: %t, CountOffset: %d, PresentOffset: %d, Storage: %q, RecordRef: %s },\n",
			f.Name, fl.Offset, fl.Size, elemSize, isArray, bound, f.Type.Pointer, countOffset, presentOffset,
			cookStorageKind(f), names)
	}
	g.pf("    ]),\n  });\n\n")
}

func cookInfoSymbol(record string) string { return "cookRecord" + record }

// cookStorageKind is what a cooked SLOT holds, which is not always what the
// wire carries: an ENUM slot holds the ORDINAL at the enum's own derived
// storage width (§7.2), where the wire carries the variant-name hash. So the
// descriptors name the storage rather than reuse a wire kind, and a walker
// reads a slot with the width `elemSize` gives and the signedness this names.
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
// home's <Base>Cook.js. `buildVersion` rides here only when the unit has no
// BLOCK form to carry it: exactly one module of a unit exports each constant
// (docs/SPEC-TABLES.md §20.7).
func cookRuntime(buildVersion uint64, withBuildVersion bool) string {
	var b strings.Builder
	if withBuildVersion {
		b.WriteString(`// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the
// bytes this build produces depend on. It both ADDRESSES a cooked artifact —
// the store is keyed by (asset hash, build version) — and REFUSES one, because
// it is what Open checks out of the header.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
// what everything cooked or blocked is keyed by.
export const BuildVersion = ` + fmt.Sprintf("0x%016xn", buildVersion) + `;

`)
	}
	b.WriteString(`// The cook's MAGIC (docs/SPEC-TABLES.md §7.1), read before anything else: it is
// what establishes the byte order every other header word is written in, and it
// is also what separates a COOK from a BLOCK — the two accelerators carry the
// same build version and different magics, because a form's identity belongs in
// its magic rather than in a second digest.
//
// The value is "SCHMCOOK" read as ASCII in the byte order a little-endian store
// produces, so a hex dump of a little-endian cook is legible.
export const TableCookMagic = ` + fmt.Sprintf("0x%016xn", cookMagic) + `;

// THIS SIDE's byte order, as §7.1's order word records it. A JavaScript reader
// reads at explicit little-endian offsets, so its order IS little whatever the
// host is, and a cook written by a build of the other order is refused twice
// over: by the magic, and by this word.
export const TableCookByteOrder = 1n;

// §7.1's header is 64 bytes of u64 words, and the DATA part begins at
// align_up(64, alignment) — DERIVED and not a header field, because a fact a
// reader computes is a fact two writers cannot disagree about.
export const TableCookHeaderBytes = ` + fmt.Sprintf("%d", cookHeaderBytes) + `;

// The ceiling on the header's alignment word: the same sixty-four a block's
// base takes (§19.1), past which the derived data offset would no longer be the
// 64 every unit this language can declare produces.
export const TableCookMaxAlign = ` + fmt.Sprintf("%d", cookMaxAlign) + `;

// THE LAYOUT CONTRACT for the cook closure (docs/SPEC-TABLES.md §20.3), in a
// language with no layout.
//
// A cooked region is laid out by the compiler's C ABI model, and C++ says so
// with static_assert while C# says it at type initialization. JavaScript has no
// record layout to measure, so what is checked instead is the generated
// descriptors' own consistency — every field inside its record, every count and
// presence companion inside it, every alignment a power of two dividing the
// size, and every array's element size dividing its extent. It runs ONCE,
// before any Open returns a handle, and a failure is a REFUSAL rather than a
// throw.
export const TableCookLayout = Object.freeze({
  verify(root) {
    const seen = new Set();
    function record(info) {
      if (info === null || seen.has(info)) { return true; }
      seen.add(info);
      if (!(info.Size > 0) || !(info.Align > 0)) { return false; }
      if ((info.Align & (info.Align - 1)) !== 0) { return false; }
      if ((info.Size % info.Align) !== 0) { return false; }
      if (info.Row.Size !== info.Size || info.Row.Align !== info.Align) { return false; }
      for (let i = 0; i < info.Fields.length; i++) {
        const f = info.Fields[i];
        if (f.Offset < 0 || f.Size < 0 || f.Offset + f.Size > info.Size) { return false; }
        if (f.CountOffset >= 0 && f.CountOffset + 4 > info.Size) { return false; }
        if (f.PresentOffset >= 0 && f.PresentOffset + 1 > info.Size) { return false; }
        if (f.IsArray && (f.ArrayBound <= 0 || f.ElemSize <= 0)) { return false; }
        if (f.IsPointer && f.Size !== 8) { return false; }
        if (f.RecordRef !== null && !f.IsPointer && !record(f.RecordRef())) { return false; }
        if (f.RecordRef !== null && f.IsPointer && !record(f.RecordRef())) { return false; }
      }
      return true;
    }
    return record(root);
  },
});

`)
	return b.String()
}
