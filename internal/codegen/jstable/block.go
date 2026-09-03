// The BLOCK FORM in JavaScript (docs/SPEC-TABLES.md §19): the READ half, and only
// the read half, emitted ON THE SIDE into <Base>Block.js.
//
// NOTHING DECLARES IT. Every fixed table has a block form; a consumer imports
// this module only if it reads one, and <Base>Table.js carries not one symbol
// of it. The C++ side is the producer (§19.1's builder) and this side is a
// consumer: it POINTS at bytes another language wrote and reads rows in place,
// with no marshalling and no copy at the boundary.
//
// THE READING TIER HAS NO BUILDER, and the reason is the language's: §19.1's
// builder places rows at a pitch it controls, and JavaScript controls no
// layout at all. What it can do — and what the form is for — is read a frame
// another language filled, at the pitch that frame carries, without copying a
// byte of it.
//
// Two ways to read one block, and both come from one declaration (§19.2): the
// DESCRIPTORS, which carry the projection's own layout and retire a hand-kept
// mirror, and the GENERATED ACCESSORS beside them, which are the typed fast
// path a per-frame consumer uses. A consumer picks by what it is doing —
// reflection to walk anything, the accessors to read one thing fast.
//
// THE LAYOUT CONTRACT (§19.3), in a language with no layout. C++ static_asserts
// each size and offset against its own compiler's model and C# asserts against
// the managed model; JavaScript has no second model to check the first against,
// so there is nothing to compare and saying otherwise would be a check that
// cannot fail. What IS checked, once, before any Open returns a handle, is
// everything the generated constants can still disagree about: every record's
// size against the extent of its own fields, every alignment against its size,
// and — the one that is two independent derivations — each array's PITCH
// CONSTANT against the size of the row type it names. That last is §19.5's
// named negative control, and perturbing either side reds it. A failure is
// this BUILD's defect, not the bytes', so the first Open throws and names the
// block rather than refusing bytes that may be perfectly good.
//
// WHAT OPEN ANSWERS. Null means one thing: these bytes are not a block of this
// build. A caller's error is a throw with the fix in it — no Uint8Array is a
// TypeError, and a view starting at a byteOffset that is not a multiple of
// sixty-four is a RangeError, because a Node Buffer under 4 KiB is carved out
// of a shared pool at an arbitrary offset and the same bytes copied into a
// fresh Uint8Array open.
//
// ALLOCATION: one small handle per Open, and nothing per row. The bytes belong
// to the consumer — a Uint8Array over an ArrayBuffer, a slice of a
// SharedArrayBuffer, or a view onto memory the producer handed across — and
// this side takes that view and points.
package jstable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateBlockFiles emits <Base>Block.js for every file of a unit that
// declares a table. A file whose tables all lack a block form still gets one,
// saying which table and why.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if blocks == nil {
		return out, nil
	}
	// THE BLOCK HOME is <Package>Block.js — one home per unit, named by the
	// package, independent of file order (runtimeHome, jstable.go). The shared
	// runtime and every record's accessor object land there, once per unit, and
	// every other module imports them.
	//
	// A unit with NO block form at all needs no runtime and names no home:
	// every Block.js it gets says which table has no block form and why, and
	// none of them imports a runtime symbol.
	home := blockHome(u, blocks)
	runtimeWritten := false
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &blockGen{unit: u, file: f, base: f.Base, blocks: blocks, home: f.Base == home, homeBase: home,
			imports: map[string]map[string]bool{}}
		g.emit()
		if g.home {
			runtimeWritten = true
		}
		out[f.Base+"Block.js"] = g.assemble()
	}
	// No file of the unit is named for the package, so the home is emitted for
	// the unit rather than for a file.
	if home != "" && !runtimeWritten {
		g := &blockGen{unit: u, base: home, blocks: blocks, home: true, homeBase: home,
			imports: map[string]map[string]bool{}}
		g.emit()
		out[home+"Block.js"] = g.assemble()
	}
	return out, nil
}

// blockHome is the module the unit's shared block runtime and every record
// accessor object are emitted into: <Package>Block.js. Empty when the unit has
// no block form at all, in which case no module needs the runtime.
func blockHome(u *ir.Unit, blocks *ir.BlockUnit) string {
	if !anyBlockForm(u, blocks) {
		return ""
	}
	return runtimeHome(u)
}

// anyBlockForm reports whether the unit has a block form at all — the condition
// under which its shared block runtime and record objects exist and need a home.
func anyBlockForm(u *ir.Unit, blocks *ir.BlockUnit) bool {
	if blocks == nil {
		return false
	}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if blocks.Block(st.Name) != nil {
				return true
			}
		}
	}
	return false
}

type blockGen struct {
	unit     *ir.Unit
	file     *ir.File // nil for a runtime home the unit has no file for
	base     string   // this module's own basename
	blocks   *ir.BlockUnit
	home     bool
	homeBase string
	body     strings.Builder
	imports  map[string]map[string]bool
}

func (g *blockGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

func (g *blockGen) need(base string, symbols ...string) {
	if base == "" || base == g.base+"Block" {
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

func (g *blockGen) needHome(symbols ...string) { g.need(g.homeBase+"Block", symbols...) }

func (g *blockGen) emit() {
	if g.home {
		g.pf("%s", blockRuntime(ir.BuildVersion(g.unit)))
		// EVERY record's accessor object of the unit, here and nowhere else.
		// Not the file that DECLARES the type: a record a block form reaches
		// is often declared in a file of `type`s alone, which gets no Block.js
		// of its own, and a consumer would then import an object nothing
		// emitted. One definition per unit, in a module that exists.
		refs := recordRefs{}
		for _, name := range g.blocks.Order {
			if ml := g.blocks.Layout(name); ml != nil {
				emitRecordObject(&g.body, g.unit, name, ml, refs)
			}
		}
	}
	if g.file == nil {
		return // the runtime home the unit has no file for: nothing declares here
	}
	for _, st := range g.file.Tables {
		if bl := g.blocks.Block(st.Name); bl != nil {
			g.emitBlockHandle(bl)
			continue
		}
		g.pf("// table %s has NO block form: %s (docs/SPEC-TABLES.md §19).\n", st.Name, g.blocks.SkippedReason(st.Name))
		g.pf("// Its wire (§3) is unaffected — only this projection is absent, and it is\n")
		g.pf("// absent by construction rather than by refusal.\n\n")
	}
}

func (g *blockGen) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file, g.unit))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the BLOCK FORM (docs/SPEC-TABLES.md §19): the READ half.\n", g.unit.Package)
	h.WriteString("//\n")
	if g.home {
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Block.js, one home per unit, named by\n")
		h.WriteString("// the package and independent of file order (docs/SPEC-TABLES.md §19.2).\n")
		h.WriteString("//\n")
	}
	h.WriteString("// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on\n")
	h.WriteString("// the side: import this module only if you read a block. The unit's\n")
	h.WriteString("// <Base>Table.js carries not one symbol of it.\n")
	h.WriteString("//\n")
	h.WriteString("// Open takes the bytes the CONSUMER holds — a Uint8Array — and points at them.\n")
	h.WriteString("// Nothing here copies, and nothing per row allocates. Bytes that are not a\n")
	h.WriteString("// block of this build are refused with a null handle, whatever they carry; a\n")
	h.WriteString("// CALLER's error — no Uint8Array, or a view that starts at an unaligned\n")
	h.WriteString("// byteOffset — throws, naming the fix.\n")
	h.WriteString("//\n")
	h.WriteString("// THE BASE'S ALIGNMENT IS THE VIEW'S byteOffset. JavaScript has no addresses,\n")
	h.WriteString("// so the one alignment fact a consumer can state is where its view starts\n")
	h.WriteString("// inside its buffer, and that is what Open measures against §19.1's sixty-four.\n\n")
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
	if !g.home && g.homeBase != "" {
		// THE HOME's WHOLE surface is RE-EXPORTED from every other module of
		// the unit. It is DEFINED once — an ES module is file-scoped, so a
		// second copy would be a second, unequal object — but a consumer that
		// imports a table's handle needs every record object a row can hand an
		// offset into: the rows an array holds AND the `type` rows those rows
		// nest, which is what `PositionAt` returns an offset for. Making it
		// import a second module to read one would be a papercut with no
		// property behind it. One module, one whole surface — and the whole of
		// it, not the part this module's own code happened to reference.
		h.WriteString("// The unit's shared block surface, defined once in its runtime home and\n")
		h.WriteString("// re-exported here whole: one module, one whole surface.\n")
		fmt.Fprintf(&h, "export { %s } from \"./%sBlock.js\";\n\n", strings.Join(blockHomeSurface(g.blocks), ", "), g.homeBase)
	}
	h.WriteString(g.body.String())
	return []byte(h.String())
}

// blockHomeSurface is every name the unit's block home exports: the shared
// runtime, and one accessor object per record the block form lays out.
func blockHomeSurface(blocks *ir.BlockUnit) []string {
	names := []string{"BuildVersion", "TableBlockByteOrder", "TableBlockLayout", "TableBlockMagic"}
	for _, name := range blocks.Order {
		if blocks.Layout(name) != nil {
			names = append(names, recordName(name))
		}
	}
	sort.Strings(names)
	return names
}

// ---- the block handle ----

func (g *blockGen) emitBlockHandle(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.needHome("BuildVersion", "TableBlockLayout", "TableBlockMagic", "TableBlockByteOrder")
	g.pf("// %s's block: a view and a length, and then rows in place. Opening one is\n", name)
	g.pf("// ONE check and no copy; reading a row is one add (docs/SPEC-TABLES.md §19.2).\n")
	g.pf("//\n")
	g.pf("// The bytes belong to the CONSUMER. Nothing here allocates but the handle.\n")
	g.pf("export const %sBlock = (() => {\n", name)
	// the descriptors, closure-local: constant data, so a reflective read costs
	// a lookup and not a parse
	records := blockDescriptorRecords(g.blocks, bl)
	for _, r := range records {
		g.emitBlockRecordDescriptor(r, g.blocks.Layout(r), nil)
	}
	g.emitBlockRecordDescriptor("", &bl.Projection, bl)
	g.emitStrideChecks(bl)
	g.emitBlockOpen(bl)
	g.emitBlockAccessors(bl)
	g.pf("  return Object.freeze({\n")
	g.pf("    // The storage a PRODUCER of this block allocates, sized from the declared\n")
	g.pf("    // maxima (docs/SPEC-TABLES.md §19.1). A JavaScript consumer does not allocate\n")
	g.pf("    // a block — the bytes are handed to it — but it caps by this: a playback\n")
	g.pf("    // buffer, a recording, a scratch copy all size from the generated constant\n")
	g.pf("    // rather than from a number a person wrote down beside it.\n")
	g.pf("    MaxBytes: %d,\n", bl.MaxBytes)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("    // %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		g.pf("    // what it read from the instance, never with these (docs/SPEC-TABLES.md §19.2).\n")
		g.pf("    %sStride: %d, %sMax: %d, %sProjectionOffset: %d,\n",
			field, a.Stride, field, a.Max, field, a.TripleOffset)
	}
	members := []string{"Open", "Layout", "Type: projection"}
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		members = append(members, field+"Count", field+"Pitch", field+"At")
	}
	members = append(members, g.blockInlineMembers(bl)...)
	g.pf("    %s,\n", strings.Join(members, ", "))
	g.pf("  });\n})();\n\n")
}

// emitStrideChecks emits the ONE pair of independent derivations the layout
// contract has in this language: each array's generated PITCH constant, and
// the size of the row object it names. Perturbing either goes red (§19.5).
func (g *blockGen) emitStrideChecks(bl *ir.BlockLayout) {
	g.pf("  // the layout contract's one independent pair (§19.3, §19.5): the array's\n")
	g.pf("  // generated pitch, and the row object's own size.\n")
	g.pf("  const strides = [\n")
	for _, a := range bl.Arrays {
		g.need(g.homeBase+"Block", recordName(a.ElemName))
		g.pf("    { What: %q, Stride: %d, SizeOf: () => %s.Size },\n",
			bl.Table.Name+"Block."+ir.GoExportName(a.Field.Name)+"Stride", a.Stride, recordName(a.ElemName))
	}
	g.pf("  ];\n")
	g.pf("  // the check runs ONCE, before the first Open points at anything, and a\n")
	g.pf("  // failure is this BUILD's defect rather than the bytes': the generated\n")
	g.pf("  // constants disagree with each other, so it throws and names the block.\n")
	g.pf("  let layoutChecked = false;\n")
	g.pf("  function Layout() {\n")
	g.pf("    if (!layoutChecked) {\n")
	g.pf("      if (!TableBlockLayout.verify(projection, strides)) {\n")
	g.pf("        throw new Error(\"%sBlock: this build's generated layout constants disagree with each other (docs/SPEC-TABLES.md §19.3) — regenerate\");\n", bl.Table.Name)
	g.pf("      }\n")
	g.pf("      layoutChecked = true;\n")
	g.pf("    }\n")
	g.pf("    return true;\n")
	g.pf("  }\n\n")
}

func (g *blockGen) emitBlockOpen(bl *ir.BlockLayout) {
	g.pf("  // Open checks once and points, and this is the WHOLE check\n")
	g.pf("  // (docs/SPEC-TABLES.md §19.2): the magic, the BYTE ORDER the prologue carries\n")
	g.pf("  // against this build's own, the BUILD VERSION against this build's own,\n")
	g.pf("  // each array's pitch, its offset_of, its COUNT against the declared\n")
	g.pf("  // maximum and its extent inside the block, the used extent against the\n")
	g.pf("  // bytes the caller passed, and the base's alignment. On a match the bytes\n")
	g.pf("  // are what a build with this layout wrote, so there is nothing to validate\n")
	g.pf("  // and nothing to fix up. On any failure it returns null and points at\n")
	g.pf("  // nothing.\n")
	g.pf("  //\n")
	g.pf("  // There is ONE entry point and no tolerant twin: the block form is\n")
	g.pf("  // same-build by construction — both sides generated from one declaration\n")
	g.pf("  // at one build — so a consumer older than its producer is not a case. A\n")
	g.pf("  // mismatch is a refusal; regenerate both sides. Data that must outlive the\n")
	g.pf("  // build that wrote it takes the wire, which this same table still has.\n")
	g.pf("  //\n")
	g.pf("  // NULL IS ONE ANSWER AND IT MEANS ONE THING: these bytes are not a block of\n")
	g.pf("  // this build — a foreign magic, the other byte order, another build version,\n")
	g.pf("  // or framing the prologue's own numbers refuse. What the CALLER got wrong is\n")
	g.pf("  // not a null, it is a throw with the fix in it: no Uint8Array at all is a\n")
	g.pf("  // TypeError, and a view whose byteOffset is not a multiple of %d — a Node\n", ir.BlockAlign)
	g.pf("  // Buffer under 4 KiB is carved out of a shared pool at an arbitrary offset —\n")
	g.pf("  // is a RangeError, because that is the caller's placement and not the bytes,\n")
	g.pf("  // and the same bytes copied into a fresh Uint8Array open.\n")
	g.pf("  //\n")
	g.pf("  // EVERY NUMBER OUT OF THE INSTANCE IS BIGINT ARITHMETIC, and each term is\n")
	g.pf("  // BOUNDED BEFORE IT IS ADDED. A forged offset_of near 2^63 must refuse, and\n")
	g.pf("  // a Number would have lost the low bits of it before the comparison ran.\n")
	g.pf("  // The C++ and C# sides hold the same shape for the same reason.\n")
	g.pf("  function Open(bytes) {\n")
	g.pf("    if (!(bytes instanceof Uint8Array)) {\n")
	g.pf("      throw new TypeError(\"%sBlock.Open takes the block's bytes as a Uint8Array, not \" + (bytes === null ? \"null\" : typeof bytes));\n", bl.Table.Name)
	g.pf("    }\n")
	g.pf("    if ((bytes.byteOffset %% %d) !== 0) { // the base's alignment\n", ir.BlockAlign)
	g.pf("      throw new RangeError(\"%sBlock.Open: the view starts \" + bytes.byteOffset + \" bytes into its ArrayBuffer, and a block's base \" +\n", bl.Table.Name)
	g.pf("        \"must be a multiple of %d (docs/SPEC-TABLES.md §19.1) — a pooled Node Buffer starts anywhere; copy the bytes \" +\n", ir.BlockAlign)
	g.pf("        \"into a fresh Uint8Array first: new Uint8Array(bytes)\");\n")
	g.pf("    }\n")
	g.pf("    Layout();\n")
	g.pf("    if (bytes.length < %d) { return null; }\n", bl.Projection.Size)
	g.pf("    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);\n")
	g.pf("    if (view.getBigUint64(0, true) !== TableBlockMagic) { return null; }\n")
	g.pf("    if (view.getBigUint64(8, true) !== BuildVersion) { return null; }\n")
	g.pf("    if (view.getBigUint64(16, true) !== TableBlockByteOrder) { return null; }\n")
	g.pf("    const extent = BigInt(bytes.length);\n")
	g.pf("    let used = %dn;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		alignment := int64(ir.BlockAlign)
		if a.ElemAlign() > alignment {
			alignment = a.ElemAlign()
		}
		g.pf("    {\n")
		g.pf("      const offsetOf = view.getBigUint64(%d, true);\n", a.OffsetOfOffset)
		g.pf("      const count = BigInt(view.getUint32(%d, true));\n", a.CountOffset)
		g.pf("      const stride = BigInt(view.getUint32(%d, true));\n", a.StrideOffset)
		g.pf("      if (stride !== %dn) { return null; }\n", a.Stride)
		g.pf("      // past the DECLARED MAXIMUM: Begin refuses this on the producer side\n")
		g.pf("      // and Open refuses it here, because a consumer that sizes anything by\n")
		g.pf("      // the maximum would overflow on a count the maximum does not bound\n")
		g.pf("      if (count > %dn) { return null; }\n", a.Max)
		g.pf("      if (offsetOf < %dn || (offsetOf %% %dn) !== 0n) { return null; }\n", bl.Projection.Size, alignment)
		g.pf("      if (offsetOf > extent) { return null; }\n")
		g.pf("      const rows = count * stride; // both bounded above: this cannot carry\n")
		g.pf("      if (rows > extent - offsetOf) { return null; }\n")
		g.pf("      const end = offsetOf + rows;\n")
		g.pf("      if (end > used) { used = end; }\n")
		g.pf("    }\n")
		_ = field
	}
	g.pf("    // the used extent, rounded to %d WITHOUT the rounding itself wrapping:\n", ir.BlockAlign)
	g.pf("    // used is already inside the extent, and the padding is paid out of the\n")
	g.pf("    // slack that is left rather than added and compared after.\n")
	g.pf("    const padding = (%dn - (used %% %dn)) %% %dn;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.pf("    if (padding > extent - used) { return null; }\n")
	g.pf("    used += padding;\n")
	g.pf("    return { Bytes: bytes, View: view, Used: Number(used) };\n")
	g.pf("  }\n\n")
}

// emitBlockAccessors emits the projection's own reads: the inline fields where
// they lie, and, per out-of-line array, the count and the pitch READ OUT OF THE
// INSTANCE plus the byte offset of one row at that pitch.
func (g *blockGen) emitBlockAccessors(bl *ir.BlockLayout) {
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("  // %s: the rows the producer filled, at the pitch the INSTANCE gives.\n", a.Field.Name)
		g.pf("  // A call site never spells the pitch arithmetic itself, for the same\n")
		g.pf("  // reason a keyed array's call sites should not re-derive their own slot\n")
		g.pf("  // rule: the idiom written at every call site is the one written wrong\n")
		g.pf("  // somewhere (docs/SPEC-TABLES.md §19.2).\n")
		g.pf("  function %sCount(block) { return block.View.getUint32(%d, true); }\n", field, a.CountOffset)
		g.pf("  function %sPitch(block) { return block.View.getUint32(%d, true); }\n", field, a.StrideOffset)
		g.pf("  // THE OFFSET IS COMPOSED FROM TWO 32-BIT READS, not read as a BigInt,\n")
		g.pf("  // and this is the block form's hottest line: every BigInt is an object,\n")
		g.pf("  // so `getBigUint64` here would allocate ONE PER ROW on a path this form\n")
		g.pf("  // exists to make free. Open has already bounded this offset inside the\n")
		g.pf("  // extent the caller passed — in BigInt, once, where a forgery near 2^63\n")
		g.pf("  // must refuse — so by the time a row is addressed the value fits a\n")
		g.pf("  // Number exactly and the composition is lossless.\n")
		g.pf("  function %sAt(block, index) {\n", field)
		g.pf("    const view = block.View;\n")
		g.pf("    const offsetOf = view.getUint32(%d, true) * 4294967296 + view.getUint32(%d, true);\n",
			a.OffsetOfOffset+4, a.OffsetOfOffset)
		g.pf("    return offsetOf + index * view.getUint32(%d, true);\n", a.StrideOffset)
		g.pf("  }\n\n")
	}
	for _, fl := range bl.Projection.Fields {
		f := fl.Field
		if ir.BlockOutOfLine(f) {
			continue
		}
		member := ir.GoExportName(f.Name)
		facts := ir.BlockFieldOf(g.unit, f, fl.Offset, true)
		pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, true)
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		switch {
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			g.pf("  function %sLength(block) { return block.View.getInt32(%d, true); }\n", member, pieces[1].Offset)
			g.pf("  function %s(block) {\n", member)
			g.pf("    let used = %sLength(block);\n", member)
			g.pf("    if (!(used >= 0) || used > %d) { used = 0; }\n", facts.ArrayBound)
			g.pf("    return block.Bytes.subarray(%d, %d + used); // a VIEW: no copy\n", fl.Offset, fl.Offset)
			g.pf("  }\n")
		case isClassRef(f.Type) && f.Type.Kind == ir.TNamed:
			elem := facts.ElemSize
			if elem == 0 {
				elem = fl.Size
			}
			g.pf("  function %sAt(index = 0) { return %d + index * %d; }\n", member, fl.Offset, elem)
		default:
			elem := facts.ElemSize
			if elem == 0 {
				elem = fl.Size
			}
			g.pf("  function %s(block, index = 0) { const view = block.View; return %s; }\n", member,
				jsScalarRead(kind, elem, fmt.Sprintf("%d + index * %d", fl.Offset, elem)))
			if isFlagsField(f) {
				g.pf("  function %sHas(block, bit, index = 0) { const view = block.View; return %s; }\n", member,
					jsFlagBitRead(fmt.Sprintf("%d + index * %d", fl.Offset, elem)))
			}
		}
		if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			g.pf("  function %sCount(block) { return block.View.getInt32(%d, true); }\n", member, facts.CountOffset)
		}
		if facts.Optional {
			g.pf("  function %sPresent(block) { return block.View.getUint8(%d) !== 0; }\n", member, facts.PresentOffset)
		}
	}
	g.pf("\n")
}

func (g *blockGen) blockInlineMembers(bl *ir.BlockLayout) []string {
	var out []string
	for _, fl := range bl.Projection.Fields {
		f := fl.Field
		if ir.BlockOutOfLine(f) {
			continue
		}
		member := ir.GoExportName(f.Name)
		facts := ir.BlockFieldOf(g.unit, f, fl.Offset, true)
		switch {
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			out = append(out, member, member+"Length")
		case isClassRef(f.Type) && f.Type.Kind == ir.TNamed:
			out = append(out, member+"At")
		default:
			out = append(out, member)
			if isFlagsField(f) {
				out = append(out, member+"Has")
			}
		}
		if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			out = append(out, member+"Count")
		}
		if facts.Optional {
			out = append(out, member+"Present")
		}
	}
	return out
}

// ---- the descriptors (docs/SPEC-TABLES.md §8, §19.2) ----

func (g *blockGen) emitBlockRecordDescriptor(record string, ml *ir.MemberLayout, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	symbol := blockInfoSymbol(record)
	name := record
	if bl != nil {
		name = bl.Table.Name
	}
	g.pf("  const %s = Object.freeze({\n", symbol)
	g.pf("    Name: %q, BuildVersion: BuildVersion, Size: %d, Align: %d, NumFields: %d,\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.pf("    Fields: Object.freeze([\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			// the ELEMENT kind, exactly as the wire's field descriptor carries
			// it (§8.1): `bytes` is an array of u8, and the two backends'
			// descriptors are byte-compared through the block dump, so they
			// spell it once.
			kind = tkU8
		}
		facts := ir.BlockFieldOf(g.unit, f, fl.Offset, bl != nil)
		if bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				g.pf("      { Name: %q, Offset: %d, Size: %d, Kind: %d, OutOfLine: true, OffsetOfOffset: %d, CountOffset: %d, StrideOffset: %d, Stride: %d, IsArray: true, Counted: true, Optional: false, ArrayBound: %d, ElemSize: 0, PresentOffset: -1, ElementRef: () => %s },\n",
					f.Name, fl.Offset, fl.Size, kind, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride,
					a.Max, blockInfoSymbol(a.ElemName))
				continue
			}
		}
		element := "null"
		// A field that NAMES a record carries that record's layout, whether it
		// holds one or an array of them: an INLINE array of records is part of
		// a row, and a walker descending one reaches its element through this
		// same column.
		if f.Type.Kind == ir.TNamed && !f.Type.Pointer {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				element = "() => " + blockInfoSymbol(ref.Name)
			}
		}
		g.pf("      { Name: %q, Offset: %d, Size: %d, Kind: %d, OutOfLine: false, OffsetOfOffset: -1, CountOffset: %d, StrideOffset: -1, Stride: 0, IsArray: %t, Counted: %t, Optional: %t, ArrayBound: %d, ElemSize: %d, PresentOffset: %d, ElementRef: %s },\n",
			f.Name, fl.Offset, fl.Size, kind, facts.CountOffset,
			facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.ElemSize,
			facts.PresentOffset, element)
	}
	g.pf("    ]),\n  });\n\n")
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

// blockInfoSymbol names one record's descriptor inside its owner's closure.
// The empty record is the owner's own projection.
func blockInfoSymbol(record string) string {
	if record == "" {
		return "projection"
	}
	return "blockRow" + record
}

// blockRuntime is the shared block runtime, emitted once per unit into the
// home module's <Base>Block.js.
func blockRuntime(buildVersion uint64) string {
	return `// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the
// bytes this build produces depend on — the type wire's protocol id, every
// table's layout keyed by wire id, every table's meaning (defaults, ranges,
// enum and union vocabularies, keyed the same way), and the build's byte
// order. It is the number a block carries and the number Open compares.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
// what everything cooked or blocked is keyed by. A table edit moves this and
// never the protocol id; a type edit moves both.
export const BuildVersion = ` + fmt.Sprintf("0x%016xn", buildVersion) + `;

// The block's magic (docs/SPEC-TABLES.md §19.1), read as an explicit
// little-endian word: it is what separates a block written by a build of this
// byte order from one written by a build of the other, whose bytes read back
// byte-reversed here and refuse.
export const TableBlockMagic = 0x4b4c42414d484353n;

// THIS SIDE's byte order, as the prologue carries it (§20.3). A JavaScript
// reader reads at explicit little-endian offsets, so its order IS little
// whatever the host is, and a block written by a build of the other order is
// refused twice over: by the magic, and by this word.
export const TableBlockByteOrder = 1n;

// THE LAYOUT CONTRACT (docs/SPEC-TABLES.md §19.3) in a language with no layout.
//
// C++ static_asserts each size and offset against its own compiler's model and
// C# asserts against the managed model. JavaScript has no second model — an
// object has no offsets at all — so there is nothing here to measure the
// compiler's model against, and a check that claimed otherwise could never
// fail. What this checks instead is everything the generated constants can
// still disagree about, and it is checked ONCE, before any Open returns a
// handle:
//
//   - every record's size against the extent of its own last field, its
//     alignment a power of two, and its size a multiple of that alignment;
//   - the PROJECTION's fields all past the generated prologue's twenty-four
//     bytes, which nothing in the descriptors describes;
//   - each array's PITCH CONSTANT against the size of the row object it names
//     — the one pair here that is TWO INDEPENDENT DERIVATIONS, and §19.5's
//     named negative control.
//
// A failure is THIS BUILD's defect — its own constants disagree — and the
// first Open of the block throws, naming it. It is not a null: a null says the
// bytes are not this build's, and these bytes were never looked at.
export const TableBlockLayout = Object.freeze({
  verify(projection, strides) {
    const seen = new Set();
    function record(info, isProjection) {
      if (info === null || seen.has(info)) { return true; }
      seen.add(info);
      if (!(info.Size > 0) || !(info.Align > 0)) { return false; }
      if ((info.Align & (info.Align - 1)) !== 0) { return false; }
      if ((info.Size % info.Align) !== 0) { return false; }
      for (let i = 0; i < info.Fields.length; i++) {
        const f = info.Fields[i];
        if (f.Offset < 0 || f.Size < 0) { return false; }
        if (isProjection && f.Offset < 24) { return false; } // the generated prologue
        if (f.Offset + f.Size > info.Size) { return false; }
        if (f.CountOffset >= 0 && f.CountOffset + 4 > info.Size) { return false; }
        if (f.PresentOffset >= 0 && f.PresentOffset + 1 > info.Size) { return false; }
        if (f.OutOfLine) {
          if (f.OffsetOfOffset < 0 || f.OffsetOfOffset + 16 > info.Size) { return false; }
          if (f.CountOffset !== f.OffsetOfOffset + 8) { return false; }
          if (f.StrideOffset !== f.OffsetOfOffset + 12) { return false; }
        }
        if (f.ElementRef !== null && !record(f.ElementRef(), false)) { return false; }
      }
      return true;
    }
    if (!record(projection, true)) { return false; }
    for (let i = 0; i < strides.length; i++) {
      if (strides[i].Stride !== strides[i].SizeOf()) { return false; }
    }
    return true;
  },
});

`
}
