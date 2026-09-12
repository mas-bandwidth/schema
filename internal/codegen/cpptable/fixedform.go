// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the reference
// writer, the reference reader and the LAYOUT, for C++.
//
// A fixed-table record is an eight-byte hash of the writer's LAYOUT
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. So:
//
//	THE WRITER is the type's constant bytes memcpy'd — the hash and zeros —
//	and then value stores at constant offsets. MeasureBody is a constexpr.
//
//	THE READER IS ONE PLAN-DRIVEN PATH. A plan is a flat array of entries and
//	a read is one loop over it. For a record whose hash equals the reader's
//	own the plan is the IDENTITY PLAN, built at COMPILE TIME by a constexpr
//	walk of this type's leaves with adjacent runs coalesced; for any other
//	hash the SAME LOOP runs over a plan compiled once from the writer's layout
//	and cached by hash, so the compile is paid once per peer. There is no
//	second reader and no fast/slow cliff — the owner's own ruling, which §3.4
//	quotes him on.
//
// Nothing here touches form 1: a fixed-table type's reader accepts both, by
// the form byte.
package cpptable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SIZES, THE WALK, THE LAYOUT AND THE HASH ARE NOT HERE. They produce the
// BYTES a fixed record and its LAYOUT are made of, and a second copy of them in
// a second backend is a second answer waiting to happen, so they live in ir
// (ir/fixedform.go) and every port renders the same walk. The ROOT SELECTION
// lives there too (ir.TableFixedEmitted): which types carry this form is not a
// question a port gets to answer for itself. What is left in this file is C++:
// how an offset is spelled, what an element's storage type is called, and how a
// plan is laid down in the source.

// offOf is C++'s offsetof, in the spelling that is a constant expression in
// every compiler this repo builds under.
func offOf(owner, member string) string {
	return fmt.Sprintf("(uint32_t) __builtin_offsetof( %s, %s )", owner, member)
}

// dstTerms renders a destination — a sum of offsetofs, and nothing at all is
// the record's own base.
func dstTerms(terms []ir.TableFixedTerm) string {
	if len(terms) == 0 {
		return "0"
	}
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		parts = append(parts, offOf(t.Type, t.Member))
	}
	return strings.Join(parts, " + ")
}

// dstRow is one TableFixedDst: MY side of a layout entry, in C++.
func (g *tableGen) dstRow(e ir.TableFixedLayoutEntry) string {
	d := e.Dst
	stride := "0"
	switch {
	case d.Stride1:
		stride = "1"
	case d.Stride != nil:
		stride = "(uint32_t) sizeof( " + g.fixedElemStorage(d.Stride) + " )"
	}
	aux := dstTerms(d.Aux)
	if d.AuxExtendsDst && len(d.Aux) == 1 {
		// a union's tag sits INSIDE the union storage the destination named,
		// so the aux is that destination and then the tag's own offset
		aux = dstTerms(d.Dst) + " + " + offOf(d.Aux[0].Type, d.Aux[0].Member)
	}
	return fmt.Sprintf("{ %s, %s, %s, %d, %d }", dstTerms(d.Dst), stride, aux, d.Counted, d.Meta)
}

// ---------------------------------------------------------------------------
// THE EMISSION
// ---------------------------------------------------------------------------

// fixedRoots is every table of this file the fixed form is emitted for, and
// the ANSWER IS ir's (ir.TableFixedEmitted): the closure this form lays out,
// §3.4's record ceiling and the plan's leaf cap are one rule for every port,
// not one per backend.
func (g *tableGen) fixedRoots(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if g.isVar(st.Name) || !ir.TableFixedEmitted(g.unit, st) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// THE DECLARING FILE OWNS A TYPE'S FIXED-FORM MATERIAL, which is what
// unitFixedOrder is for — the same rule the C backend states at length. A unit
// is MANY FILES (SPEC §3.2) and this form's walk is the ROOT'S CLOSURE, so a
// root in one file reaches types declared in another, and the two headers land
// in one translation unit because the first includes the second to see the
// struct at all. So the closure is taken over the WHOLE UNIT, once, and each
// file emits the members it DECLARES: the `inline` write body is defined
// exactly once, and so is each `static_assert`.
func (g *tableGen) unitFixedOrder() []*ir.Struct {
	closure := ir.TableClosure(g.unit)
	seen := map[string]bool{}
	var order []*ir.Struct
	add := func(st *ir.Struct) {
		if g.isVar(st.Name) || !ir.TableFixedEmitted(g.unit, st) {
			return
		}
		fixedCollectTypes(st, seen, &order)
	}
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			add(st)
		}
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				add(st)
			}
		}
	}
	return order
}

// fixedDeclaredHere is every type name THIS FILE declares — structs, tables and
// unions alike, because all three carry a layout assert.
func (g *tableGen) fixedDeclaredHere() map[string]bool {
	here := map[string]bool{}
	for _, st := range g.file.Tables {
		here[st.Name] = true
	}
	for _, un := range g.file.TableUnions {
		here[un.Name] = true
	}
	for _, d := range g.file.Decls {
		switch t := d.(type) {
		case *ir.Struct:
			here[t.Name] = true
		case *ir.Union:
			here[t.Name] = true
		}
	}
	return here
}

func (g *tableGen) emitFixedForm(members []*ir.Struct) {
	roots := g.fixedRoots(members)
	all := g.unitFixedOrder()
	here := g.fixedDeclaredHere()
	own := make([]*ir.Struct, 0, len(all))
	for _, st := range all {
		if here[st.Name] {
			own = append(own, st)
		}
	}
	if len(roots) == 0 && len(own) == 0 {
		return
	}
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's LAYOUT and then\n")
	g.pf("// the values in declared order, every field at its declared storage width.\n")
	g.pf("// The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("// ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("// the writer's own layout for anybody else. There is no second reader.\n\n")
	g.emitFixedLayoutAsserts(own, all, here)
	for _, st := range own {
		g.emitFixedWriteBody(st)
	}
	// THE READ-SIDE BOUNDS, in the same closure order the write bodies take —
	// post-order, so a nested type's body stands before the call to it.
	for _, st := range own {
		g.emitFixedClampBodyFn(st)
	}
	for _, st := range roots {
		g.emitFixedClamp(st)
		g.emitFixedRoot(st)
	}
}

func fixedCollectTypes(st *ir.Struct, seen map[string]bool, order *[]*ir.Struct) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedCollectTypes(r, seen, order)
		case *ir.Union:
			for _, v := range r.Variants {
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					fixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}

// fixedElemStorage is the C++ storage type of ONE element, which is what the
// loop above strides by.
func (g *tableGen) fixedElemStorage(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		return f.Type.Name
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32_t"
		}
		return "uint64_t"
	}
	if f.Type.Width == 128 {
		if f.Type.Signed {
			return "serialize::int128_t"
		}
		return "serialize::uint128_t"
	}
	if f.Type.Signed {
		return fmt.Sprintf("int%d_t", f.Type.Width)
	}
	return fmt.Sprintf("uint%d_t", f.Type.Width)
}

// ---- the writer ------------------------------------------------------------

func (g *tableGen) emitFixedWriteBody(st *ir.Struct) {
	g.fixedOwner = st
	g.pf("// %s's stores. The prefill — the hash, then zeros — is memcpy'd first,\n", st.Name)
	g.pf("// which is also what zero-fills every byte of declared slack.\n")
	g.pf("inline void %sFixedWriteBody( uint8_t * b, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    (void) b; (void) value;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedWriteField(f, off, "b", "value", 4)
		off += ir.TableFixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, indent int) { //nolint:gocyclo
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not it
		// is present, and when the flag is 0 what rides is zero. It is ONE `if`
		// here rather than a rule anywhere else, because the template already
		// put the zeros there — so an absent optional costs the writer the
		// branch and not one store, and a caller's untouched payload storage
		// never reaches the wire.
		ind := strings.Repeat(" ", indent)
		g.pf("%sTableFixedPut8( %s + %d, %s.%s_present ? 1 : 0 );\n", ind, buf, off, val, f.Name)
		g.pf("%sif ( %s.%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedWritePayload(f, off+ir.TableFixedPresentBytes, buf, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedWritePayload(f, off, buf, val, indent)
}

func (g *tableGen) emitFixedWritePayload(f *ir.Field, off int64, buf, val string, indent int) { //nolint:gocyclo
	ind := strings.Repeat(" ", indent)
	base := off
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4): there is no count and no
		// slack, so the bound is the loop.
		g.emitFixedWriteLoop(f, base, strconv.FormatInt(f.KeyEnumRef.Max, 10), buf, val+"."+ir.TableFixedKeyedSlots(g.fixedOwner, f), indent)
	case f.Array == ir.ArrayFixed:
		// AND EVERY ELEMENT OF `[N]T` IS LIVE: min equals max, so no count rides.
		g.emitFixedWriteLoop(f, base, strconv.FormatInt(f.ArrayBound, 10), buf, val+"."+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS THE LOOP AND THE SLACK STAYS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4). Writing all Max elements put the unused
		// slots' STORAGE on the wire, which for an array of a type with
		// declared defaults is the element's default image and not zero — a
		// value nobody wrote, riding as if somebody had.
		g.fixedWriteBound(fmt.Sprintf("%s.%s_count", val, f.Name), f.ArrayBound, "count", ind)
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_count );\n", ind, buf, base, val, f.Name)
		g.emitFixedWriteLoop(f, base+ir.TableFixedCountBytes, fmt.Sprintf("%s.%s_count", val, f.Name), buf, val+"."+f.Name, indent)
	case f.Type.Kind == ir.TString:
		g.emitFixedWriteText(f, base, buf, val, ind, 1)
	case f.Type.Kind == ir.TWString:
		g.emitFixedWriteText(f, base, buf, val, ind, 2)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedWriteText(f, base, buf, val, ind, 1)
	default:
		g.emitFixedWriteElement(f, base, buf, val+"."+f.Name, indent)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base int64, count string, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := ir.TableFixedElementBytes(f)
	g.pf("%sfor ( int64_t i = 0; i < (int64_t) %s; ++i )\n%s{\n", ind, count, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s + %d + i * %d", buf, base, elem), expr+"[i]", indent+4)
	g.pf("%s}\n", ind)
}

// emitFixedWriteText is a text field's store: the used LENGTH, then that many
// UNITS onto the template's zeros (docs/SPEC-TABLES.md §3.4, "the slack is
// zero"). Copying the whole span instead put whatever the storage held past
// the used length on the wire — stale bytes, and a `string(N)` written twice
// with two different values would not compare equal on the second write.
func (g *tableGen) emitFixedWriteText(f *ir.Field, base int64, buf, val, ind string, unit int64) {
	g.fixedWriteBound(fmt.Sprintf("%s.%s_length", val, f.Name), f.Type.Size, "length", ind)
	g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
	scale := ""
	if unit != 1 {
		scale = fmt.Sprintf("%d * ", unit)
	}
	g.pf("%smemcpy( %s + %d, %s.%s, (size_t) ( %s%s.%s_length ) );\n", ind, buf, base+4, val, f.Name, scale, val, f.Name)
}

// fixedWriteBound is the WRITE-SIDE CHECK, and it is DEBUG-ONLY BY RULE: NDEBUG
// removes schema_assert exactly as it removes assert, so a release build pays
// nothing for it. The read side checks the same number unconditionally and
// counts the clamp — a write-side check catches the caller's own bug at the
// call site, and it is never the thing that keeps the wire safe.
func (g *tableGen) fixedWriteBound(expr string, bound int64, what, ind string) {
	g.pf("%sschema_assert( %s >= 0 && %s <= %d ); // the declared %s is the bound (§3.4)\n", ind, expr, expr, bound, what)
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, off int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedWriteBody( %s + %d, %s );\n", ind, r.Name, buf, off, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s.type );\n", ind, tag*8, buf, off, tag*8, expr)
			g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
			for _, v := range r.Variants {
				g.pf("%s    case %sType::%s:\n%s    {\n", ind, f.Type.Name, ir.GoExportName(v.Name), ind)
				g.emitFixedWriteElement(v.F, off+tag, buf, fmt.Sprintf("%s.%s", expr, v.Name), indent+8)
				g.pf("%s        break;\n%s    }\n", ind, ind)
			}
			g.pf("%s    default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
			return
		case *ir.Flags:
			g.pf("%sTableFixedPut64( %s + %d, (uint64_t) %s );\n", ind, buf, off, expr)
			return
		}
	}
	w := ir.TableFixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%sTableFixedPut8( %s + %d, %s ? 1 : 0 );\n", ind, buf, off, expr)
	case ir.TFloat32:
		g.pf("%sTableFixedPutF32( %s + %d, %s );\n", ind, buf, off, expr)
	case ir.TFloat64:
		g.pf("%sTableFixedPutF64( %s + %d, %s );\n", ind, buf, off, expr)
	default:
		if w == 16 {
			g.pf("%sTableFixedPut128( %s + %d, %s );\n", ind, buf, off, expr)
			return
		}
		g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
	}
}

// ---- the root's surface ----------------------------------------------------

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := ir.TableFixedTypeBytes(st)
	known := g.lineageEntries(st)
	floor := g.lineageFloor(st)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("// MeasureBody IS A CONSTEXPR on this form: the body is the same size for\n")
	g.pf("// every value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("constexpr int64_t %sFixedBodyBytes = %d;\n", st.Name, body)
	g.pf("constexpr int64_t %sFixedRecordBytes = 8 + %sFixedBodyBytes; // the hash and the body\n", st.Name, st.Name)
	g.pf("constexpr uint64_t %sFixedHash = 0x%016xull; // fnv1a64 over the layout and the definitions digest (bill §13)\n\n", st.Name, hash)

	g.pf("// THE LAYOUT (form 1 calls this the vocabulary block): %d entries, a\n", len(entries))
	g.pf("// PRE-ORDER walk of the closure in the writer's declared order.\n")
	g.pf("// Every byte is settled by the compiler.\n")
	g.pf("constexpr uint8_t %sFixedLayout[] = {\n", st.Name)
	g.emitByteArray(layout)
	g.pf("};\n")
	g.pf("constexpr int64_t %sFixedLayoutBytes = (int64_t) sizeof( %sFixedLayout );\n\n", st.Name, st.Name)

	g.pf("// MY SIDE of the layout, one row per entry: the storage facts a layout\n")
	g.pf("// entry cannot carry, which is what a plan compiled from another writer's\n")
	g.pf("// layout lands values through.\n")
	g.pf("constexpr TableFixedDst %sFixedDst[] = {\n", st.Name)
	for _, e := range entries {
		g.pf("    %s, // %s\n", g.dstRow(e), e.Note)
	}
	g.pf("};\n\n")

	plan, guarded := ir.TableFixedBuildPlan(g.unit, st)
	g.pf("// THE IDENTITY PLAN, coalesced out of %d leaves by the schema compiler —\n", ir.TableFixedLeafCount(g.unit, st))
	g.pf("// the one walk every backend lays down (ir/fixedform.go), so no two ports\n")
	g.pf("// can disagree about what the coalescer did; the asserts above tie every\n")
	g.pf("// destination in it to this compiler's own ABI. UNGUARDED ENTRIES FIRST,\n")
	g.pf("// then the arms: the entries that are nearly all of a plan never test a\n")
	g.pf("// guard at all.\n")
	g.pf("constexpr TableFixedEntry %sFixedPlan[] = {\n", st.Name)
	for _, e := range plan {
		guard := "kTableFixedNoGuard"
		if e.Guard != ir.TableFixedNoGuard {
			guard = fmt.Sprintf("%du", e.Guard)
		}
		argw := e.ArgW
		if argw == 0 {
			argw = 1
		}
		argw2 := e.ArgW2
		if argw2 == 0 {
			argw2 = 1
		}
		// THE SECOND GUARD LANE IS NAMED, never left to a zero: an entry whose
		// guard2 reads 0 is an entry conditional on the byte at offset 0 (§5.8 row 6).
		guard2 := "kTableFixedNoGuard"
		if e.Guard2 != ir.TableFixedNoGuard {
			guard2 = fmt.Sprintf("%du", e.Guard2)
		}
		g.pf("    { %du, %du, %du, %du, %s, %s, %d, %d, 0, 0, %d, %s, %d, %d }, // %s\n",
			e.Src, e.Dst, e.Size, e.Aux, guard, fixedOpName(e.Op), e.Arg, e.Meta, argw, guard2, e.Arg2, argw2, e.Note)
	}
	g.pf("};\n")
	g.pf("constexpr int32_t %sFixedPlanCount = %d;\n", st.Name, len(plan))
	g.pf("constexpr int32_t %sFixedPlanGuarded = %d;\n\n", st.Name, guarded)

	cover := ir.TableFixedIdentityCoverage(g.unit, st)
	g.pf("// THE TYPE'S VALUE BYTES: every byte of this build's own storage that holds\n")
	g.pf("// a DECLARED VALUE, sorted and merged. Padding is not in it — a byte between\n")
	g.pf("// two fields holds nothing, so nothing defaults it — and neither is anything\n")
	g.pf("// else the identity plan above does not land, because this IS that plan's\n")
	g.pf("// destinations. THE PREFILL IS THIS SET MINUS WHAT A PLAN LANDS (§3.4), so\n")
	g.pf("// against the identity plan it is empty and the identity read writes no byte\n")
	g.pf("// twice; a plan compiled from a stranger's layout subtracts itself from it\n")
	g.pf("// once, when the plan is compiled, and prefills exactly the rest.\n")
	g.pf("constexpr TableFixedFill %sFixedCover[] = {\n", st.Name)
	for _, r := range cover {
		g.pf("    { %du, %du },\n", r.Dst, r.Size)
	}
	g.pf("};\n")
	g.pf("constexpr int32_t %sFixedCoverCount = %d;\n\n", st.Name, len(cover))

	g.emitFixedLineage(st, known, floor)

	g.pf("// A FILE: THE HEADER (docs/SPEC-TABLES.md §3, one rule for all five forms)\n")
	g.pf("// — form byte, seven reserved zero bytes, the LAYOUT HASH at 8, body at 16 —\n")
	g.pf("// then the layout behind its u32 length, then the records to the end of it.\n")
	g.pf("constexpr int64_t %sFixedMeasure( int64_t count )\n{\n", st.Name)
	g.pf("    return kTableFixedHeaderBytes + 4 + %sFixedLayoutBytes + count * %sFixedRecordBytes;\n}\n\n", st.Name, st.Name)

	g.pf("inline int64_t %sFixedSave( const %s * values, int64_t count, uint8_t * buffer, int64_t capacity )\n{\n", st.Name, st.Name)
	g.pf("    const int64_t need = %sFixedMeasure( count );\n", st.Name)
	g.pf("    if ( count < 0 || buffer == NULL || capacity < need ) { return -1; }\n")
	g.pf("    memset( buffer, 0, (size_t) kTableFixedHeaderBytes ); // the seven reserved bytes, and the rest of the header\n")
	g.pf("    buffer[0] = kTableFixedForm;\n")
	g.pf("    TableFixedPut64( buffer + kTableFixedHashAt, %sFixedHash );\n", st.Name)
	g.pf("    TableFixedPut32( buffer + kTableFixedHeaderBytes, (uint32_t) %sFixedLayoutBytes );\n", st.Name)
	g.pf("    memcpy( buffer + kTableFixedHeaderBytes + 4, %sFixedLayout, (size_t) %sFixedLayoutBytes );\n", st.Name, st.Name)
	g.pf("    uint8_t * at = buffer + kTableFixedHeaderBytes + 4 + %sFixedLayoutBytes;\n", st.Name)
	g.pf("    for ( int64_t k = 0; k < count; ++k )\n    {\n")
	g.pf("        TableFixedPut64( at, %sFixedHash );\n", st.Name)
	g.pf("        memset( at + 8, 0, (size_t) %sFixedBodyBytes ); // the prefill's zeros\n", st.Name)
	g.pf("        %sFixedWriteBody( at + 8, values[k] );\n", st.Name)
	g.pf("        at += %sFixedRecordBytes;\n    }\n    return need;\n}\n\n", st.Name)

	g.pf("// THE READ: a prefill and ONE loop over ONE plan. Hash chooses the plan\n")
	g.pf("// from the known set COMPILE laid down (algorithm §5.3). A stranger's\n")
	g.pf("// layout is never parsed: unknown hash is layout_newer, known hash with\n")
	g.pf("// different bytes is layout_malformed, a hash below the floor is\n")
	g.pf("// layout_unsupported. Identity is a PLAN walked by the same loop.\n")
	g.pf("inline int64_t %sFixedLoad( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n", st.Name, st.Name)
	g.pf("                            TableFixedEntry * plan, int32_t plan_capacity, TableFixedPlanCache * cache, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n    if ( report == NULL ) { report = &local; }\n")
	g.pf("    if ( data == NULL || bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = true; return -1; }\n")
	g.pf("    if ( data[0] != kTableFixedForm )\n    {\n")
	g.pf("        report->refused = true;\n")
	g.pf("        report->reason = data[0] == kTableWireForm ? previous_form\n")
	g.pf("                       : data[0] == kTableWireMessageForm ? message_form_as_file\n")
	g.pf("                       : newer_form;\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    const uint32_t layout_bytes = TableFixedGet32( data + kTableFixedHeaderBytes );\n")
	g.pf("    if ( (int64_t) layout_bytes + kTableFixedHeaderBytes + 4 > bytes ) { report->refused = true; report->reason = layout_malformed; return -1; }\n")
	g.pf("    const uint8_t * layout = data + kTableFixedHeaderBytes + 4;\n")
	g.pf("    const uint64_t hash = TableFixedGet64( data + kTableFixedHashAt ); // the header's hash; digest is not on the wire\n")
	g.pf("    const uint8_t * at = layout + layout_bytes;\n")
	g.pf("    const int64_t rest = bytes - kTableFixedHeaderBytes - 4 - (int64_t) layout_bytes;\n")
	g.pf("    int32_t lineage_at = -1;\n")
	g.pf("    for ( int32_t i = 0; i < %sFixedLineageCount; ++i )\n", st.Name)
	g.pf("    {\n        if ( %sFixedLineage[i] == hash ) { lineage_at = i; break; }\n    }\n", st.Name)
	g.pf("    if ( lineage_at < 0 )\n    {\n")
	g.pf("        report->refused = true; report->reason = layout_newer; report->layout_hash = hash; return -1;\n")
	g.pf("    }\n")
	g.pf("    int32_t floor_at = %sFixedFloor;\n", st.Name)
	g.pf("#ifdef SCHEMA_FIXED_FLOOR_TEST_HOOKS\n")
	g.pf("    floor_at = %sFixedFloorLive;\n", st.Name)
	g.pf("#endif\n")
	g.pf("    if ( lineage_at < floor_at )\n    {\n")
	g.pf("        report->refused = true; report->reason = layout_unsupported; report->layout_hash = hash; return -1;\n")
	g.pf("    }\n")
	g.pf("    const TableFixedKnownLayout & known = %sFixedKnown[lineage_at];\n", st.Name)
	g.pf("    if ( (int64_t) layout_bytes != known.layout_bytes || memcmp( layout, known.layout, (size_t) layout_bytes ) != 0 )\n")
	g.pf("    {\n        report->refused = true; report->reason = layout_malformed; return -1;\n    }\n")
	g.pf("    const TableFixedEntry * entries = %sFixedPlan;\n", st.Name)
	g.pf("    int32_t entry_count = %sFixedPlanCount;\n", st.Name)
	g.pf("    int32_t entry_guarded = %sFixedPlanGuarded;\n", st.Name)
	g.pf("    int64_t record_bytes = %sFixedRecordBytes;\n", st.Name)
	g.pf("    const TableFixedFill * fill = NULL;\n")
	g.pf("    int32_t fill_count = 0;\n")
	g.pf("    if ( hash != %sFixedHash )\n    {\n", st.Name)
	g.pf("        // AN OLDER WRITER: compile the plan from the TRUSTED known layout,\n")
	g.pf("        // never from the file's bytes (those were only compared). Cached by hash.\n")
	g.pf("        const TableFixedPlanCacheSlot * hit = NULL;\n")
	g.pf("        if ( cache != NULL )\n        {\n")
	g.pf("            for ( int32_t i = 0; i < cache->used; ++i )\n            {\n")
	g.pf("                if ( cache->slots[i].hash == hash ) { hit = &cache->slots[i]; break; }\n")
	g.pf("            }\n        }\n")
	g.pf("        if ( hit != NULL )\n        {\n")
	g.pf("            entries = hit->plan;\n            entry_count = hit->count;\n            entry_guarded = hit->guarded;\n")
	g.pf("            record_bytes = hit->record_bytes;\n")
	g.pf("            fill = hit->fill;\n            fill_count = hit->fill_count;\n")
	g.pf("        }\n        else\n        {\n")
	g.pf("            TableFixedLayoutView parsed;\n")
	g.pf("            TableMessageReason why = layout_malformed;\n")
	g.pf("            if ( !TableFixedParseLayout( known.layout, known.layout_bytes, parsed, why ) ) { report->refused = true; report->reason = layout_malformed; return -1; }\n")
	g.pf("            TableFixedEntry * dest = plan;\n            int32_t dest_capacity = plan_capacity;\n            int storing = 0;\n")
	g.pf("            if ( cache != NULL && cache->storage != NULL && cache->used < kTableFixedPlanCacheCapacity && cache->stride > 0 )\n")
	g.pf("            {\n                dest = cache->storage + cache->used * cache->stride;\n                dest_capacity = cache->stride;\n                storing = 1;\n            }\n")
	g.pf("            int32_t compiled_guarded = 0;\n")
	g.pf("            uint32_t fill_at = 0;\n")
	g.pf("            const int32_t made = TableFixedCompile( parsed, %sFixedLayout, (int32_t) %sFixedLayoutBytes, %sFixedDst,\n", st.Name, st.Name, st.Name)
	g.pf("                                                   %sFixedCover, %sFixedCoverCount,\n", st.Name, st.Name)
	g.pf("                                                   dest, dest_capacity, &compiled_guarded, &fill_at, &fill_count, report );\n")
	g.pf("            if ( made < 0 ) { report->refused = true; report->reason = ( made == -2 ) ? layout_record_too_large : plan_too_large; return -1; }\n")
	g.pf("            record_bytes = known.record_bytes;\n")
	g.pf("            fill = ( fill_count > 0 ) ? (const TableFixedFill *) (const void *) ( (const uint8_t *) dest + fill_at ) : NULL;\n")
	g.pf("            if ( cache != NULL ) { cache->compiles++; }\n")
	g.pf("            if ( storing )\n            {\n")
	g.pf("                cache->slots[cache->used].hash = hash;\n")
	g.pf("                cache->slots[cache->used].plan = dest;\n")
	g.pf("                cache->slots[cache->used].count = made;\n")
	g.pf("                cache->slots[cache->used].guarded = compiled_guarded;\n")
	g.pf("                cache->slots[cache->used].record_bytes = record_bytes;\n")
	g.pf("                cache->slots[cache->used].fill = fill;\n")
	g.pf("                cache->slots[cache->used].fill_count = fill_count;\n")
	g.pf("                cache->slots[cache->used].made = 1;\n")
	g.pf("                cache->used++;\n")
	g.pf("            }\n")
	g.pf("            entries = dest;\n            entry_count = made;\n            entry_guarded = compiled_guarded;\n")
	g.pf("        }\n")
	g.pf("    }\n")
	g.pf("    if ( record_bytes <= 8 || rest %% record_bytes != 0 ) { report->malformed = true; return -1; }\n")
	g.pf("    const int64_t n = rest / record_bytes;\n")
	g.pf("    if ( n > capacity ) { report->refused = true; report->reason = batch_too_large; return -1; }\n")
	g.pf("    %s defaults;\n", st.Name)
	g.pf("    if ( fill_count > 0 )\n    {\n")
	g.pf("        memset( (void *) &defaults, 0, sizeof( defaults ) );\n")
	g.pf("        %sReset( defaults );\n    }\n", st.Name)
	g.pf("    for ( int64_t k = 0; k < n; ++k )\n    {\n")
	g.pf("        if ( TableFixedGet64( at ) != hash ) { report->refused = true; report->reason = no_layout; return -1; }\n")
	g.pf("        TableFixedFillRun( fill, fill_count, (const uint8_t *) &defaults, (uint8_t *) &values[k] );\n")
	g.pf("        TableFixedRun( entries, entry_count, entry_guarded, at + 8, (uint8_t *) &values[k], report );\n")
	g.pf("        TableFixedClampKnownRanges( %sFixedKnownRanges[lineage_at], %sFixedKnownRangeCounts[lineage_at],\n", st.Name, st.Name)
	g.pf("                                    (uint8_t *) &values[k], report );\n")
	if ir.TableFixedClampNeeded(st) {
		g.pf("        %sFixedClamp( values[k], report );\n", st.Name)
	}
	g.pf("        at += record_bytes;\n    }\n    return n;\n}\n\n")
}

func (g *tableGen) emitFixedLineage(st *ir.Struct, known []fixedKnown, floor int32) {
	if len(known) == 0 {
		return
	}
	g.pf("// THE LINEAGE, oldest first, the reader's own last (bill §6b). LOAD looks\n")
	g.pf("// the file's header hash up here and never parses a stranger's layout.\n")
	g.pf("constexpr uint64_t %sFixedLineage[] = {\n", st.Name)
	for _, k := range known {
		g.pf("    0x%016xull, // %s\n", k.hash, k.note)
	}
	g.pf("};\n")
	g.pf("constexpr int32_t %sFixedLineageCount = %d;\n", st.Name, len(known))
	g.pf("constexpr int32_t %sFixedFloor = %d;\n", st.Name, floor)
	g.pf("#ifdef SCHEMA_FIXED_FLOOR_TEST_HOOKS\n")
	g.pf("inline int32_t %sFixedFloorLive = %sFixedFloor;\n", st.Name, st.Name)
	g.pf("inline void %sFixedSetFloorForTest( int32_t index ) { %sFixedFloorLive = index; }\n", st.Name, st.Name)
	g.pf("#endif\n\n")
	for i, k := range known {
		g.pf("constexpr uint8_t %sFixedKnown%dLayout[] = {\n", st.Name, i)
		g.emitByteArray(k.layout)
		g.pf("};\n")
	}
	g.pf("constexpr TableFixedKnownLayout %sFixedKnown[] = {\n", st.Name)
	for i, k := range known {
		g.pf("    { 0x%016xull, %sFixedKnown%dLayout, %d, %d }, // %s\n",
			k.hash, st.Name, i, len(k.layout), k.recordBytes, k.note)
	}
	g.pf("};\n\n")
	for i, k := range known {
		if len(k.ranges) == 0 {
			continue
		}
		g.pf("constexpr TableFixedKnownRange %sFixedKnown%dRanges[] = {\n", st.Name, i)
		for _, r := range k.ranges {
			g.pf("    { %du, %du, %du, %d, %d },\n", r.dst, r.width, r.signed, r.lo, r.hi)
		}
		g.pf("};\n")
	}
	g.pf("constexpr const TableFixedKnownRange * %sFixedKnownRanges[] = {\n", st.Name)
	for i, k := range known {
		if len(k.ranges) == 0 {
			g.pf("    NULL,\n")
		} else {
			g.pf("    %sFixedKnown%dRanges,\n", st.Name, i)
		}
	}
	g.pf("};\n")
	g.pf("constexpr int32_t %sFixedKnownRangeCounts[] = {", st.Name)
	for i, k := range known {
		if i > 0 {
			g.pf(",")
		}
		g.pf(" %d", len(k.ranges))
	}
	g.pf(" };\n\n")
}

func (g *tableGen) emitByteArray(b []byte) {
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		var sb strings.Builder
		sb.WriteString("   ")
		for _, v := range b[i:end] {
			fmt.Fprintf(&sb, " 0x%02x,", v)
		}
		g.pf("%s\n", sb.String())
	}
}

// emitFixedLayoutAsserts is what makes a generator-computed offset safe: every
// number the plans and the destination rows below are built from, checked
// against the compiler's own offsetof and sizeof.
// order is what THIS FILE owns; all is the unit's whole fixed closure and here
// is what this file declares, which together place a UNION's asserts in the file
// that declares the union rather than in every file that holds one by value.
func (g *tableGen) emitFixedLayoutAsserts(order, all []*ir.Struct, here map[string]bool) {
	g.pf("// THE ABI AGREES WITH THE PLANS. The offsets below were computed by the\n")
	g.pf("// schema compiler rather than folded by this one, because they are the same\n")
	g.pf("// offsets every other port needs; these are that arithmetic checked against\n")
	g.pf("// this compiler's own, at build time, one line per fact the plans rest on.\n")
	for _, st := range order {
		for _, m := range ir.TableFixedMembers(g.unit, st) {
			if m.Member == "" {
				g.pf("static_assert( sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );\n", m.Owner, m.Value, m.Owner)
				continue
			}
			g.pf("static_assert( %s == %d, \"%s.%s: the fixed form's plan lands here\" );\n",
				offOf(m.Owner, g.fixedMemberSpelling(st, m.Member)), m.Value, m.Owner, m.Member)
		}
	}
	unions := map[string]bool{}
	for _, st := range all {
		for _, f := range st.Fields {
			un, ok := f.Type.Ref.(*ir.Union)
			if !ok || f.Type.Kind != ir.TNamed || unions[f.Type.Name] || !here[f.Type.Name] {
				continue
			}
			unions[f.Type.Name] = true
			size, _, _, arms := ir.UnionLayout(g.unit, un)
			g.pf("static_assert( sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );\n", f.Type.Name, size, f.Type.Name)
			g.pf("static_assert( %s == 0, \"%s: the tag leads the union storage\" );\n", offOf(f.Type.Name, "type"), f.Type.Name)
			for _, v := range un.Variants {
				g.pf("static_assert( %s == %d, \"%s.%s: every arm is overlaid here\" );\n", offOf(f.Type.Name, v.Name), arms, f.Type.Name, v.Name)
			}
		}
	}
	g.pf("\n")
}

// fixedMemberSpelling is how THIS backend names a storage member ir's C ABI
// walk answered for: a keyed array's slots sit behind the accessor wrapper here
// and are the field itself in C, and the wrapper's slots are its first member,
// so the two are the same byte under two spellings.
func (g *tableGen) fixedMemberSpelling(st *ir.Struct, member string) string {
	for _, f := range st.Fields {
		if f.Name == member && f.KeyEnum != "" {
			return ir.TableFixedKeyedSlots(st, f)
		}
	}
	return member
}

func fixedOpName(op int) string {
	switch op {
	case ir.TableFixedOpCount:
		return "kTableFixedCount"
	case ir.TableFixedOpText:
		return "kTableFixedText"
	}
	return "kTableFixedCopy"
}

// ---- the read-side bounds --------------------------------------------------
//
// THE CLAMP IS BRANCHLESS AND THE COUNT IS AN ADD. A bounds pass over a record
// that is nearly always in range is a pass of branches that are nearly always
// not taken, and a predictor that is right every time still spends the slots;
// worse, a branch in the body of an array loop is what stops the loop
// vectorizing at all. Written as a select and an add the whole pass folds into
// straight-line SIMD over an array, which is what a form that trades bytes for
// speed owes its own read path (test/bench/fixedform_measure.cpp).
//
// The two lines re-read the storage rather than naming a temporary, and that is
// deliberate: the C leg has no `auto` and this walk knows every field's DECLARED
// width but not its spelled storage type, so a temporary would mean a second
// type table to keep in step with the first. A compiler folds the reload.
//
// A fixed record is a positional image, so the ONE read loop moves bytes and
// asks nothing about what they mean. What a declaration bounds is held HERE,
// as straight-line code in the generated decode, after the copy — never as plan
// entries, which would be a test per bounded field on every read of every
// record and is the cost the identity plan exists to avoid.
//
// THE PASS RUNS OVER STORAGE, so ONE pass covers both plans: the identity plan
// and a plan compiled from a stranger's layout land values in the same places.
// It walks only what a read can have written — a counted array's LIVE elements
// and never its slack, an optional's payload only when the present byte says
// so — because the prefill's declared defaults are in range by construction
// (internal/check refuses a range that excludes zero without a default in it)
// and clamping storage nobody wrote would count a clamp on every clean read.

// emitFixedClampBodyFn is ONE type's bounds, the twin of its write body: a
// nested type is a call and not an inlining, so a type that appears twice in a
// closure is spelled once.
func (g *tableGen) emitFixedClampBodyFn(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.fixedOwner = st
	g.pf("// %s's read-side bounds.\n", st.Name)
	g.pf("inline void %sFixedClampBody( %s & value, int32_t & clamped, int32_t & damaged )\n{\n", st.Name, st.Name)
	g.pf("    (void) value; (void) clamped; (void) damaged;\n")
	for _, f := range st.Fields {
		g.emitFixedClampField(f, "value", 4)
	}
	g.pf("}\n\n")
}

// emitFixedClamp is the ROOT's entry point: the one call a read makes, and the
// one place the count reaches the report.
func (g *tableGen) emitFixedClamp(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("// THE READ-SIDE BOUNDS (docs/SPEC-TABLES.md §3.4): a ranged scalar's\n")
	g.pf("// declared min and max, and an ORDINAL's set — a union tag past the arm\n")
	g.pf("// count, an enum ordinal past the enum's top value. Straight-line, after\n")
	g.pf("// the copy, over STORAGE, so the identity plan and a plan compiled from a\n")
	g.pf("// stranger's layout are held to the same numbers by the same pass. Every\n")
	g.pf("// clamp COUNTS.\n")
	g.pf("inline void %sFixedClamp( %s & value, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    int32_t clamped = 0;\n")
	g.pf("    int32_t damaged = 0;\n")
	g.pf("    %sFixedClampBody( value, clamped, damaged );\n", st.Name)
	g.pf("    report->clamped += clamped;\n")
	g.pf("    // ILL-FORMED TEXT IS FRAMING-CLASS DAMAGE (§3, §4), so it lands on the\n")
	g.pf("    // one flag and not on a counter: the field read its declared default\n")
	g.pf("    // and the rest of the record stands.\n")
	g.pf("    if ( damaged != 0 ) { report->malformed = true; }\n}\n\n")
}

func (g *tableGen) emitFixedClampField(f *ir.Field, val string, indent int) {
	if !ir.TableFixedClampNeededField(f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§3.4), so it is not
		// held to a bound either: what rides there is zero from the template
		// and nobody wrote it.
		g.pf("%sif ( %s.%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedClampPayload(f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedClampPayload(f, val, indent)
}

func (g *tableGen) emitFixedClampPayload(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4).
		g.pf("%sfor ( int64_t i = 0; i < %d; ++i )\n%s{\n", ind, f.KeyEnumRef.Max, ind)
		g.emitFixedClampElement(f, val+"."+ir.TableFixedKeyedSlots(g.fixedOwner, f)+"[i]", indent+4)
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayFixed:
		g.pf("%sfor ( int64_t i = 0; i < %d; ++i )\n%s{\n", ind, f.ArrayBound, ind)
		g.emitFixedClampElement(f, val+"."+f.Name+"[i]", indent+4)
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayCounted:
		// THE LIVE COUNT AND NOT THE BOUND: the slack is the template's zeros
		// and a zero nobody wrote is not a value to hold to a range.
		g.pf("%sfor ( int64_t i = 0; i < (int64_t) %s.%s_count; ++i )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedClampElement(f, val+"."+f.Name+"[i]", indent+4)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		g.emitFixedTextContent(f, val, ind)
	default:
		g.emitFixedClampElement(f, val+"."+f.Name, indent)
	}
}

// emitFixedTextContent is THE ONE CONTENT RULE THE WIRE HAS (§3, §4), on this
// form's terms. A `string(N)`'s used bytes are well-formed UTF-8 with no zero
// among them and a `wstring(N)`'s used units are paired UTF-16 with no zero
// unit; the check runs over the USED LENGTH and over nothing else, because the
// slack carries no meaning and reading it would be the cost this form exists to
// avoid.
//
// A PAYLOAD THAT IS NOT TEXT IS DAMAGE AND NOT DATA, and it is the same
// verdict every other form reaches: THE FIELD READS ITS DECLARED DEFAULT, one
// `malformed` counts, and the rest of the record stands. It is what SPEC.md
// §4.7 and §4.12 put on the packet wire, where the whole read refuses — here
// the record is positional, so the damage is one field's and the reader does
// not lose the others to it.
func (g *tableGen) emitFixedTextContent(f *ir.Field, val, ind string) {
	call := fmt.Sprintf("TableUtf8Valid( (const uint8_t *) %s.%s, (uint64_t) %s.%s_length )", val, f.Name, val, f.Name)
	if f.Type.Kind == ir.TWString {
		// THE SAME BYTES THE COPY MOVED. A wstring's units ride little-endian
		// and land as a raw copy, so the validator reads the storage as the
		// wire it came from.
		call = fmt.Sprintf("TableUtf16Valid( (const uint8_t *) %s.%s, (int64_t) %s.%s_length )", val, f.Name, val, f.Name)
	}
	g.pf("%sif ( !%s )\n%s{\n", ind, call, ind)
	g.pf("%s    memset( %s.%s, 0, sizeof( %s.%s ) );\n", ind, val, f.Name, val, f.Name)
	if f.Type.Kind == ir.TString && hasByteDefault(f) {
		g.pf("%s    memcpy( %s.%s, %s, %d ); // the declared default\n", ind, val, f.Name, cStringLit(f.DefBytes), len(f.DefBytes))
		g.pf("%s    %s.%s_length = %d;\n", ind, val, f.Name, len(f.DefBytes))
	} else {
		g.pf("%s    %s.%s_length = 0;\n", ind, val, f.Name)
	}
	g.pf("%s    damaged++;\n%s}\n", ind, ind)
}

// ordinalFillsStorage reports whether an ordinal's extent is the LARGEST value
// its storage width holds — 255 in a byte, 65535 in two. The read-side clamp
// for such an ordinal is `> extent`, a comparison the tag's own type can never
// satisfy, so the emitter drops it: the same "this check cannot fire" test
// tableClampEnds applies to a ranged scalar's end sitting on its width's limit.
func ordinalFillsStorage(max int64, storageBits int) bool {
	if storageBits <= 0 || storageBits >= 64 {
		return false
	}
	return uint64(max) >= uint64(1)<<uint(storageBits)-1
}

func (g *tableGen) emitFixedClampElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if ir.TableFixedClampNeeded(r) {
				g.pf("%s%sFixedClampBody( %s, clamped, damaged );\n", ind, r.Name, expr)
			}
			return
		case *ir.Union:
			// A UNION TAG PAST THE ARM COUNT NAMES NO ARM. On the compiled path
			// the ordinal op has already remapped it; on the identity path it
			// is a raw copy out of a stranger's bytes, and this is what holds
			// it. It lands None — the same nothing an unset union holds — and
			// counts.
			if !ordinalFillsStorage(r.Max, ir.StorageBitsFor(r.Max)) {
				g.pf("%sif ( (uint64_t) %s.type > %du ) { %s.type = %sType::None; clamped++; }\n",
					ind, expr, r.Max, expr, f.Type.Name)
			}
			if !ir.TableFixedClampNeededUnionArm(r) {
				return
			}
			g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
			for _, v := range r.Variants {
				if v.F == nil || !ir.TableFixedClampNeededField(v.F) {
					continue
				}
				g.pf("%s    case %sType::%s:\n%s    {\n", ind, f.Type.Name, ir.GoExportName(v.Name), ind)
				g.emitFixedClampElement(v.F, fmt.Sprintf("%s.%s", expr, v.Name), indent+8)
				g.pf("%s        break;\n%s    }\n", ind, ind)
			}
			g.pf("%s    default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			// AN ORDINAL PAST THE ENUM'S TOP VALUE is not a variant this
			// generation cannot name — it is a number the enum cannot hold at
			// all. It lands None and counts. A value INSIDE an `| max = K`
			// widening is the other case and is left alone: that is a name a
			// later generation has, not a bound this one broke.
			//
			// An enum whose extent FILLS its storage (255 variants in a byte)
			// is the "this check cannot fire" case tableClampEnds already
			// applies to a ranged scalar: the tag's own type holds no value
			// above the extent, so the comparison is always false and the
			// emitter drops it rather than hand a warning-as-error build a
			// tautology. No semantics move — the elided check never clamped.
			if ordinalFillsStorage(r.Max, r.StorageBits) {
				return
			}
			g.pf("%sif ( (uint64_t) %s > %du ) { %s = %s::None; clamped++; }\n",
				ind, expr, r.Max, expr, f.Type.Name)
			return
		}
		return
	}
	width := int(ir.TableFixedStorageBytes(f.Type))
	switch {
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		g.fixedClampFloat(expr, formatFloat(f.FMin, true), formatFloat(f.FMax, true), ind)
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		g.fixedClampFloat(expr, formatFloat(f.FMin, false), formatFloat(f.FMax, false), ind)
	default:
		signed := ir.TableKindSigned(ir.TableScalarKind(f))
		// A FIXED-POINT FIELD'S BOUNDS ARE IN VALUE UNITS and its storage is
		// raw, so ir.TableRawRange shifts them by F before either end is
		// spelled — the same numbers the variable form's codec clamps at.
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			switch {
			case low && high:
				g.fixedClampBoth(expr, tableIntLit(rlo, signed, width), tableIntLit(rhi, signed, width), ind)
			case low:
				g.fixedClampEnd(expr, "<", tableIntLit(rlo, signed, width), ind)
			case high:
				g.fixedClampEnd(expr, ">", tableIntLit(rhi, signed, width), ind)
			}
		}
		if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s// bits(%d) width clamp\n", ind, f.Type.Width)
			g.fixedClampEnd(expr, ">", fmt.Sprintf("%dull", maxv), ind)
		}
	}
}

// fixedClampBoth is a two-ended clamp: the select, then the count as an add.
func (g *tableGen) fixedClampBoth(expr, lo, hi, ind string) {
	// THE COUNT IS AN OR OF THE TWO ENDS, and the casts are what say the `|` is
	// meant: clang reads a bitwise operator between two comparisons as a
	// mistyped `||` and reds it (-Wbitwise-instead-of-logical), which is a good
	// warning about code that is not this. A sum would silence it too and is
	// measurably slower — the or folds to a mask the compiler keeps in a
	// vector lane, and the add does not (test/bench/fixedform_measure.cpp).
	g.pf("%sclamped += (int) ( %s < %s ) | (int) ( %s > %s );\n", ind, expr, lo, expr, hi)
	g.pf("%s%s = ( %s < %s ) ? %s : ( ( %s > %s ) ? %s : %s );\n", ind, expr, expr, lo, lo, expr, hi, hi, expr)
}

// fixedClampFloat is the BOUNDED FLOAT's clamp, and it is not the integer one:
// IEEE says every ordered comparison against a NaN is false, so `v < lo` and
// `v > hi` are BOTH false for a NaN and the integer shape would let it land
// whole, counting nothing — a value outside the declared range reaching the
// consumer, which is the one thing this pass exists to stop (docs/SPEC-TABLES.md
// §3.4).
//
// THE LOW TEST IS NEGATED INSTEAD: `!( v >= lo )` is true for a NaN and for
// every value below the minimum, and false for everything in range, so a NaN
// lands `min` and counts ONE, exactly as -inf does. Nothing else moves — in
// particular -0.0 against a min of +0.0 is `-0.0 >= 0.0`, which is true, so it
// is IN RANGE and lands as written with its sign bit and counts nothing.
func (g *tableGen) fixedClampFloat(expr, lo, hi, ind string) {
	g.pf("%sclamped += (int) ( !( %s >= %s ) ) | (int) ( %s > %s );\n", ind, expr, lo, expr, hi)
	g.pf("%s%s = ( !( %s >= %s ) ) ? %s : ( ( %s > %s ) ? %s : %s );\n", ind, expr, expr, lo, lo, expr, hi, hi, expr)
}

// fixedClampEnd is the one-ended twin: an end the storage's own width already
// holds is an end the emitter drops (tableClampEnds), so a great many bounded
// fields spend one compare and not two.
func (g *tableGen) fixedClampEnd(expr, op, bound, ind string) {
	g.pf("%sclamped += ( %s %s %s );\n", ind, expr, op, bound)
	g.pf("%s%s = ( %s %s %s ) ? %s : %s;\n", ind, expr, expr, op, bound, bound, expr)
}
