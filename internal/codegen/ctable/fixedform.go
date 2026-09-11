// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for C.
//
// The reference is internal/codegen/cpptable and this port matches it BYTE FOR
// BYTE, which is the only thing a fixed record's reader can rely on: the sizes,
// the LAYOUT's pre-order walk, the layout's bytes and its hash are all ir's
// (ir/fixedform.go), the ROOT SELECTION is ir's too (ir.TableFixedEmitted), and
// neither backend computes any of it a second time. What this file spells for
// itself is C.
//
// TWO C-SHAPED DIFFERENCES, and each is the C form of one C++ mechanism rather
// than a new idea:
//
//   - THE IDENTITY PLAN IS DATA, NOT A CONSTEXPR WALK. C++ writes the leaf walk
//     as a constexpr function and lets the compiler run it and coalesce the
//     result. C has no constexpr, so THE GENERATOR runs the same walk and the
//     same coalescer and lays the finished plan down as a static const array.
//     The compile-time work happens either way; the compiler that does it
//     differs. What ties the array's numbers to the C ABI is the block of
//     SCHEMA_TABLE_STATIC_ASSERTs emitted above it: every offset and every
//     size the plan depends on is asserted against offsetof and sizeof, so a
//     layout this generator got wrong is a build error and never a wrong read.
//
//   - A UNION'S ARMS SIT BEHIND `as`. The C++ storage is a tag beside an
//     ANONYMOUS union, so `offsetof( MixedEvent, hit )` names the arm; the C
//     storage names the union member, so the same offset is `as.hit`. It is
//     the same byte either way and the asserts say so.
package ctable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---------------------------------------------------------------------------
// WHICH TABLES CARRY THE FORM
// ---------------------------------------------------------------------------

// fixedRoots is every table of this file the fixed form is emitted for, and the
// ANSWER IS ir's (ir.TableFixedEmitted) — the same call the C++ reference makes,
// so the two legs cannot carry the form for different sets of types.
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

// ---------------------------------------------------------------------------
// THE EMISSION
// ---------------------------------------------------------------------------

// THE DECLARING FILE OWNS A TYPE'S FIXED-FORM MATERIAL, and that is what
// unitFixedOrder is for. A unit is MANY FILES (SPEC §3.2) and the fixed form's
// walk is the ROOT'S CLOSURE, so a root in one file reaches types declared in
// another — and the two headers land in one translation unit, because the first
// includes the second to see the struct at all. Emitting a type's asserts and
// write body in both is a redefinition: in C++ the `static_assert`s are
// harmless but the `inline` body is not, and in C the assert itself is a
// `typedef char name[1]`, whose repetition is a C11 feature this generated code
// does not require. So the closure is taken over the WHOLE UNIT, once, and each
// file emits the members it DECLARES — which is a rule with no ordering hazard,
// since the file that declares a type is the file every user of it includes.
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
	g.pf("/* ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n\n")
	g.pf("   A record is an eight-byte hash of the writer's LAYOUT and then\n")
	g.pf("   the values in declared order, every field at its declared storage width.\n")
	g.pf("   The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("   ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("   the writer's own layout for anybody else. There is no second reader. */\n\n")
	g.emitFixedLayoutAsserts(own, all, here)
	for _, st := range own {
		g.pf("static SCHEMA_UNUSED %s void %s( uint8_t * b, const %s * value );\n",
			tableInlineMacro(g.unit.Package), g.sym(st.Name, "fixed_write_body"), st.Name)
	}
	g.pf("\n")
	for _, st := range own {
		g.emitFixedWriteBody(st)
	}
	/* THE READ-SIDE BOUNDS, in the same closure order the write bodies take —
	   post-order, so a nested type's body stands before the call to it. */
	for _, st := range own {
		g.emitFixedClampBodyFn(st)
	}
	for _, st := range roots {
		g.emitFixedClamp(st)
		g.emitFixedRoot(st)
	}
}

// emitFixedLayoutAsserts is what makes a generator-computed offset safe: every
// number the plan and the destination rows below are built from, checked
// against the compiler's own offsetof and sizeof.
// order is what THIS FILE owns; all is the unit's whole fixed closure and here
// is what this file declares, which together place a UNION's asserts in the file
// that declares the union rather than in every file that holds one by value.
func (g *tableGen) emitFixedLayoutAsserts(order, all []*ir.Struct, here map[string]bool) {
	g.pf("/* THE C ABI AGREES WITH THE PLAN. C has no constexpr, so the offsets in the\n")
	g.pf("   plans below were computed by the schema compiler rather than folded by\n")
	g.pf("   this one; these are that arithmetic checked against the compiler's own,\n")
	g.pf("   at build time, one line per fact the plans rest on. */\n")
	for _, st := range order {
		for _, m := range ir.TableFixedMembers(g.unit, st) {
			if m.Member == "" {
				g.pf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_size, sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );\n",
					ir.RustSnake(m.Owner), m.Owner, m.Value, m.Owner)
				continue
			}
			g.pf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_%s, offsetof( %s, %s ) == %d, \"%s.%s: the fixed form's plan lands here\" );\n",
				ir.RustSnake(m.Owner), ir.RustSnake(m.Member), m.Owner, m.Member, m.Value, m.Owner, m.Member)
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
			g.pf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_size, sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );\n",
				ir.RustSnake(f.Type.Name), f.Type.Name, size, f.Type.Name)
			g.pf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_type, offsetof( %s, type ) == 0, \"%s: the tag leads the union storage\" );\n",
				ir.RustSnake(f.Type.Name), f.Type.Name, f.Type.Name)
			g.pf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_as, offsetof( %s, as ) == %d, \"%s: every arm is overlaid here\" );\n",
				ir.RustSnake(f.Type.Name), f.Type.Name, arms, f.Type.Name)
		}
	}
	g.pf("\n")
}

// ---- the template writer ---------------------------------------------------

func (g *tableGen) emitFixedWriteBody(st *ir.Struct) {
	g.pf("/* %s's stores. The template — the hash, then zeros — is memcpy'd first,\n", st.Name)
	g.pf("   which is also what zero-fills every byte of declared slack. */\n")
	g.pf("static SCHEMA_UNUSED %s void %s( uint8_t * b, const %s * value )\n{\n",
		tableInlineMacro(g.unit.Package), g.sym(st.Name, "fixed_write_body"), st.Name)
	g.pf("    (void) b; (void) value;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedWriteField(f, off, "b", "value->", 4)
		off += ir.TableFixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, indent int) {
	if f.Type.Optional {
		/* AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		   (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not it
		   is present, and when the flag is 0 what rides is zero. One `if`, and
		   the template already put the zeros there. */
		ind := strings.Repeat(" ", indent)
		g.pf("%stable_fixed_put8( %s + %d, %s%s_present ? 1 : 0 );\n", ind, buf, off, val, f.Name)
		g.pf("%sif ( %s%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedWritePayload(f, off+ir.TableFixedPresentBytes, buf, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedWritePayload(f, off, buf, val, indent)
}

func (g *tableGen) emitFixedWritePayload(f *ir.Field, off int64, buf, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	base := off
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4): no count, no slack.
		g.emitFixedWriteLoop(f, base, strconv.FormatInt(f.KeyEnumRef.Max, 10), buf, val+f.Name, indent)
	case f.Array == ir.ArrayFixed:
		// AND EVERY ELEMENT OF `[N]T` IS LIVE: min equals max.
		g.emitFixedWriteLoop(f, base, strconv.FormatInt(f.ArrayBound, 10), buf, val+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS THE LOOP AND THE SLACK STAYS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4), exactly as the reference does it.
		g.fixedWriteBound(fmt.Sprintf("%s%s_count", val, f.Name), f.ArrayBound, "count", ind)
		g.pf("%stable_fixed_put32( %s + %d, (uint32_t) %s%s_count );\n", ind, buf, base, val, f.Name)
		g.emitFixedWriteLoop(f, base+ir.TableFixedCountBytes, fmt.Sprintf("%s%s_count", val, f.Name), buf, val+f.Name, indent)
	case f.Type.Kind == ir.TString:
		g.emitFixedWriteText(f, base, buf, val, ind, 1)
	case f.Type.Kind == ir.TWString:
		g.emitFixedWriteText(f, base, buf, val, ind, 2)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedWriteText(f, base, buf, val, ind, 1)
	default:
		g.emitFixedWriteElement(f, base, buf, val+f.Name, indent)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base int64, count string, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := ir.TableFixedElementBytes(f)
	g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < (int64_t) %s; ++i )\n%s    {\n", ind, ind, ind, count, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s + %d + i * %d", buf, base, elem), expr+"[i]", indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
}

/*
emitFixedWriteText is the C twin of the reference's text store: the used

	LENGTH, then that many UNITS onto the template's zeros (§3.4).
*/
func (g *tableGen) emitFixedWriteText(f *ir.Field, base int64, buf, val, ind string, unit int64) {
	g.fixedWriteBound(fmt.Sprintf("%s%s_length", val, f.Name), f.Type.Size, "length", ind)
	g.pf("%stable_fixed_put32( %s + %d, (uint32_t) %s%s_length );\n", ind, buf, base, val, f.Name)
	scale := ""
	if unit != 1 {
		scale = fmt.Sprintf("%d * ", unit)
	}
	g.pf("%smemcpy( %s + %d, %s%s, (size_t) ( %s%s%s_length ) );\n", ind, buf, base+4, val, f.Name, scale, val, f.Name)
}

/*
fixedWriteBound is the WRITE-SIDE CHECK, DEBUG-ONLY BY RULE: NDEBUG removes

	schema_assert exactly as it removes assert. The read side checks the same
	number in every build and counts the clamp.
*/
func (g *tableGen) fixedWriteBound(expr string, bound int64, what, ind string) {
	g.pf("%sschema_assert( %s >= 0 && %s <= %d ); /* the declared %s is the bound (§3.4) */\n", ind, expr, expr, bound, what)
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, off int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%s( %s + %d, &%s );\n", ind, g.sym(r.Name, "fixed_write_body"), buf, off, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%stable_fixed_put%d( %s + %d, (uint%d_t) %s.type );\n", ind, tag*8, buf, off, tag*8, expr)
			g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
			for _, v := range r.Variants {
				g.pf("%s    case %s:\n%s    {\n", ind, enumConst(f.Type.Name+"Type", v.Name), ind)
				g.emitFixedWriteElement(v.F, off+tag, buf, fmt.Sprintf("%s.as.%s", expr, v.Name), indent+8)
				g.pf("%s        break;\n%s    }\n", ind, ind)
			}
			g.pf("%s    default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			g.pf("%stable_fixed_put%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
			return
		case *ir.Flags:
			g.pf("%stable_fixed_put64( %s + %d, (uint64_t) %s );\n", ind, buf, off, expr)
			return
		}
	}
	w := ir.TableFixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%stable_fixed_put8( %s + %d, %s ? 1 : 0 );\n", ind, buf, off, expr)
	case ir.TFloat32:
		g.pf("%stable_fixed_putf32( %s + %d, %s );\n", ind, buf, off, expr)
	case ir.TFloat64:
		g.pf("%stable_fixed_putf64( %s + %d, %s );\n", ind, buf, off, expr)
	default:
		if w == 16 {
			if f.Type.Signed {
				g.pf("%stable_fixed_put128_i( %s + %d, %s );\n", ind, buf, off, expr)
			} else {
				g.pf("%stable_fixed_put128_u( %s + %d, %s );\n", ind, buf, off, expr)
			}
			return
		}
		g.pf("%stable_fixed_put%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
	}
}

// ---- the root's surface ----------------------------------------------------

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := ir.TableFixedTypeBytes(st)
	n := ir.RustSnake(st.Name)

	g.pf("/* ---- %s, the fixed form ---- */\n\n", st.Name)
	g.pf("/* THE BODY IS ONE CONSTANT on this form: it is the same size for every\n")
	g.pf("   value the type can hold (docs/SPEC-TABLES.md §3.4). */\n")
	g.pf("static SCHEMA_UNUSED const int64_t %s_fixed_body_bytes = %d;\n", n, body)
	g.pf("static SCHEMA_UNUSED const int64_t %s_fixed_record_bytes = 8 + %d; /* the hash and the body */\n", n, body)
	g.pf("static SCHEMA_UNUSED const uint64_t %s_fixed_hash = 0x%016xull; /* fnv1a64 over the layout and the definitions digest (bill §13) */\n\n", n, hash)

	g.pf("/* THE LAYOUT (form 1 calls this the vocabulary block): %d entries, a\n", len(entries))
	g.pf("   PRE-ORDER walk of the closure in the writer's declared order.\n")
	g.pf("   Every byte is settled by the schema compiler. */\n")
	g.pf("static SCHEMA_UNUSED const uint8_t %s_fixed_layout[] = {\n", n)
	g.emitFixedByteArray(layout)
	g.pf("};\n")
	g.pf("static SCHEMA_UNUSED const int64_t %s_fixed_layout_bytes = (int64_t) sizeof( %s_fixed_layout );\n\n", n, n)

	g.pf("/* MY SIDE of the layout, one row per entry: the storage facts a layout\n")
	g.pf("   entry cannot carry, which is what a plan compiled from another writer's\n")
	g.pf("   layout lands values through. */\n")
	g.pf("static SCHEMA_UNUSED const TableFixedDst %s_fixed_dst[] = {\n", n)
	for _, e := range entries {
		g.pf("    %s, /* %s */\n", g.fixedDstRow(e), e.Note)
	}
	g.pf("};\n\n")

	plan, guarded := ir.TableFixedBuildPlan(g.unit, st)
	g.pf("/* THE IDENTITY PLAN, coalesced out of %d leaves by the schema compiler —\n", ir.TableFixedLeafCount(g.unit, st))
	g.pf("   the one walk every backend lays down (ir/fixedform.go), so no two ports\n")
	g.pf("   can disagree about what the coalescer did; the asserts above tie every\n")
	g.pf("   destination in it to this compiler's own ABI. UNGUARDED ENTRIES FIRST,\n")
	g.pf("   then the arms: the entries that are nearly all of a plan never test a\n")
	g.pf("   guard at all. */\n")
	g.pf("static SCHEMA_UNUSED const TableFixedEntry %s_fixed_plan[] = {\n", n)
	for _, e := range plan {
		guard := "SCHEMA_TABLE_FIXED_NO_GUARD"
		if e.Guard != ir.TableFixedNoGuard {
			guard = fmt.Sprintf("%du", e.Guard)
		}
		argw := e.ArgW
		if argw == 0 {
			argw = 1
		}
		g.pf("    { %du, %du, %du, %du, %s, %s, %d, %d, 0, 0, %d }, /* %s */\n",
			e.Src, e.Dst, e.Size, e.Aux, guard, fixedOpName(e.Op), e.Arg, e.Meta, argw, e.Note)
	}
	g.pf("};\n")
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_plan_count = %d;\n", n, len(plan))
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_plan_guarded = %d;\n\n", n, guarded)

	cover := ir.TableFixedIdentityCoverage(g.unit, st)
	g.pf("/* THE TYPE'S VALUE BYTES: every byte of this build's own storage that holds\n")
	g.pf("   a DECLARED VALUE, sorted and merged. Padding is not in it — a byte between\n")
	g.pf("   two fields holds nothing, so nothing defaults it — and neither is anything\n")
	g.pf("   else the identity plan above does not land, because this IS that plan's\n")
	g.pf("   destinations. THE PREFILL IS THIS SET MINUS WHAT A PLAN LANDS (§3.4), so\n")
	g.pf("   against the identity plan it is empty and the identity read writes no byte\n")
	g.pf("   twice; a plan compiled from a stranger's layout subtracts itself from it\n")
	g.pf("   once, when the plan is compiled, and prefills exactly the rest. */\n")
	g.pf("static SCHEMA_UNUSED const TableFixedFill %s_fixed_cover[] = {\n", n)
	for _, r := range cover {
		g.pf("    { %du, %du },\n", r.Dst, r.Size)
	}
	g.pf("};\n")
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_cover_count = %d;\n\n", n, len(cover))

	// ---- THE LINEAGE AS STATIC DATA (§5.2, COMPILE(lock, T)) ----
	//
	// One entry per locked layout, OLDEST FIRST, the current one LAST, with the
	// hash a file is matched on, the layout bytes VERBATIM and the record size
	// the lock recorded. LOAD selects on the hash and compares the bytes; it
	// never walks them (§5.3, §5.6).
	lineage, floor := g.fixedLineage(st, FixedLineageEntry{Wire: hash, Layout: layout, Record: 8 + body})
	for i, e := range lineage {
		if e.Wire == hash {
			continue
		}
		g.pf("/* lineage entry %d: the layout bytes THE LOCK recorded, verbatim", i)
		if e.Retired {
			g.pf(" — RETIRED: %s", e.Reason)
		}
		g.pf(" */\n")
		g.pf("static SCHEMA_UNUSED const uint8_t %s_fixed_known_%d[] = {\n", n, i)
		g.emitFixedByteArray(e.Layout)
		g.pf("};\n")
	}
	g.pf("/* THE LINEAGE, OLDEST FIRST, THE CURRENT LAYOUT LAST (§5.2). THE ONLY FACT\n")
	g.pf("   A FILE IS MATCHED ON IS THE HASH; the layout bytes are what a known hash\n")
	g.pf("   is held to by memcmp, and the record size is taken from here and NEVER\n")
	g.pf("   from the file. */\n")
	g.pf("static SCHEMA_UNUSED const TableFixedKnownLayout %s_fixed_known[] = {\n", n)
	for i, e := range lineage {
		bytes := fmt.Sprintf("%s_fixed_known_%d", n, i)
		if e.Wire == hash {
			bytes = n + "_fixed_layout"
		}
		g.pf("    { 0x%016xull, %s, (int64_t) sizeof( %s ), %d },\n", e.Wire, bytes, bytes, e.Record)
	}
	g.pf("};\n")
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_known_count = %d;\n", n, len(lineage))
	g.pf("/* THE FLOOR IS ONE NUMBER AND THE LINEAGE IS ONE ARRAY, so \"retired\" is an\n")
	g.pf("   index cut and the operator's two answers stay distinct: below the floor is\n")
	g.pf("   layout_unsupported (upgrade the client), outside the lineage is\n")
	g.pf("   layout_newer (ship the reader). A retired entry stays forever (§5.2). */\n")
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_floor = %d;\n\n", n, floor)

	older := 0
	for _, e := range lineage {
		if e.Wire != hash {
			older++
		}
	}
	if older > 0 {
		stride := ir.TableFixedLeafCount(g.unit, st)*2 + len(entries)*2 + 128
		g.pf("/* ONE PLAN PER OLDER LINEAGE ENTRY, BUILT FROM THE LOCK'S OWN BYTES and\n")
		g.pf("   never from a file's (§5.9 #3). The storage is STATIC — this codec\n")
		g.pf("   allocates nothing — and the walk runs ONCE PER PROCESS, off the record\n")
		g.pf("   loop: a load selects a plan and never builds one, and a load can no\n")
		g.pf("   longer fail for want of one. C HAS NO PACKAGE INITIALIZER (§5.9 #7 of\n")
		g.pf("   this leg's PR), so \"once\" is this flag and not a language phase; what\n")
		g.pf("   §5.8 row 3 forbids — compiling from A FILE'S bytes into a cache a load\n")
		g.pf("   can miss — is gone either way: the bytes here are the lock's. */\n")
		g.pf("#define %s_FIXED_LINEAGE_STRIDE %d\n", strings.ToUpper(n), stride)
		g.pf("%s", tableFixedOnce)
		g.pf("static TableFixedEntry %s_fixed_lineage_storage[%d][%s_FIXED_LINEAGE_STRIDE];\n", n, len(lineage), strings.ToUpper(n))
		g.pf("static TableFixedLineagePlan %s_fixed_lineage[%d];\n", n, len(lineage))
		g.pf("static TableFixedOnce %s_fixed_lineage_ready = SCHEMA_TABLE_FIXED_ONCE_UNBUILT;\n", n)
		g.pf("static SCHEMA_UNUSED void %s_fixed_lineage_build( void )\n{\n", n)
		g.pf("    int32_t i;\n")
		g.pf("    /* THE FLAG IS PUBLISHED LAST, AFTER THE LOOP, and a racing load WAITS for\n")
		g.pf("       it: a second thread that sees the plans unpublished must never read\n")
		g.pf("       them, because an unbuilt TableFixedLineagePlan is all zeros — entries\n")
		g.pf("       NULL, count 0 — and zeros are a plan step 8 accepts, landing a\n")
		g.pf("       prefill-only record where the read owed a refusal. So: READY is an\n")
		g.pf("       acquire load, the build is CLAIMED by one thread, every other thread\n")
		g.pf("       waits on that acquire, and the publish is a release store of READY\n")
		g.pf("       after the last entry is written (§5.9 #7). */\n")
		g.pf("    if ( table_fixed_once_ready( &%s_fixed_lineage_ready ) ) { return; }\n", n)
		g.pf("    if ( !table_fixed_once_claim( &%s_fixed_lineage_ready ) )\n    {\n", n)
		g.pf("        table_fixed_once_wait( &%s_fixed_lineage_ready ); /* WAIT — never the zeroed plan */\n", n)
		g.pf("        return;\n    }\n")
		g.pf("    for ( i = 0; i < %s_fixed_known_count; ++i )\n    {\n", n)
		g.pf("        TableFixedLayoutView parsed;\n        TableReport census;\n")
		g.pf("        TableFixedEntry * dest = %s_fixed_lineage_storage[i];\n", n)
		g.pf("        int32_t guarded = 0;\n        int32_t made;\n        int32_t fills = 0;\n        uint32_t fill_at = 0;\n        int why = SCHEMA_TABLE_LAYOUT_MALFORMED;\n")
		g.pf("        memset( &%s_fixed_lineage[i], 0, sizeof( %s_fixed_lineage[i] ) );\n", n, n)
		g.pf("        if ( %s_fixed_known[i].hash == %s_fixed_hash ) { continue; } /* the identity entry: the baked plan answers it */\n", n, n)
		g.pf("        memset( &census, 0, sizeof( census ) );\n")
		g.pf("        /* A LINEAGE ENTRY THAT IS NOT A PARSEABLE LAYOUT IS A BUG IN THE LOCK\n")
		g.pf("           (§5.9 #8). C cannot fail a build from a header, so the entry carries\n")
		g.pf("           the name and LOAD refuses by it if a file ever matches its hash. */\n")
		g.pf("        if ( !table_fixed_parse_layout( %s_fixed_known[i].layout, %s_fixed_known[i].layout_bytes, &parsed, &why ) )\n", n, n)
		g.pf("        { %s_fixed_lineage[i].reason = SCHEMA_TABLE_LAYOUT_MALFORMED; continue; }\n", n)
		g.pf("        made = table_fixed_compile( &parsed, %s_fixed_layout, (int32_t) sizeof( %s_fixed_layout ), %s_fixed_dst,\n", n, n, n)
		g.pf("                                    %s_fixed_cover, %s_fixed_cover_count,\n", n, n)
		g.pf("                                    dest, %s_FIXED_LINEAGE_STRIDE, &guarded, &fill_at, &fills, &census );\n", strings.ToUpper(n))
		g.pf("        if ( made < 0 )\n        {\n")
		g.pf("            /* §5.9 #4: the DECLARED CAPACITY a build holds an entry to is real,\n")
		g.pf("               and an entry whose plan exceeds it records plan_too_large ON THAT\n")
		g.pf("               ENTRY. A layout that is not one is the lock's bug, by its name. */\n")
		g.pf("            %s_fixed_lineage[i].reason = ( made == -2 ) ? SCHEMA_TABLE_LAYOUT_MALFORMED : SCHEMA_TABLE_PLAN_TOO_LARGE;\n", n)
		g.pf("            continue;\n        }\n")
		g.pf("        %s_fixed_lineage[i].entries = dest;\n", n)
		g.pf("        %s_fixed_lineage[i].count = made;\n", n)
		g.pf("        %s_fixed_lineage[i].guarded = guarded;\n", n)
		g.pf("        %s_fixed_lineage[i].fill = ( fills > 0 ) ? (const TableFixedFill *) (const void *) ( (const uint8_t *) dest + fill_at ) : NULL;\n", n)
		g.pf("        %s_fixed_lineage[i].fill_count = fills;\n", n)
		g.pf("        /* THE COMPILE CENSUS, ONCE PER PEER and never per record (§5.4). */\n")
		g.pf("        %s_fixed_lineage[i].unknown = census.unknown;\n", n)
		g.pf("        %s_fixed_lineage[i].kind_mismatch = census.kind_mismatch;\n", n)
		g.pf("    }\n")
		g.pf("    /* AND THE FLAG LAST: every entry above is written before this store, and\n")
		g.pf("       this store is what a waiting thread's acquire is waiting for. */\n")
		g.pf("    table_fixed_once_publish( &%s_fixed_lineage_ready );\n}\n\n", n)
	}

	g.pf("/* A FILE: THE HEADER (docs/SPEC-TABLES.md §3, one rule for all five forms)\n")
	g.pf("   — form byte, seven reserved zero bytes, the LAYOUT HASH at 8, body at 16 —\n")
	g.pf("   then the layout behind its u32 length, then the records to the end of it. */\n")
	g.pf("static SCHEMA_UNUSED int64_t %s( int64_t count )\n{\n", g.api(st.Name, "fixed_measure"))
	g.pf("    return kTableFixedHeaderBytes + 4 + (int64_t) sizeof( %s_fixed_layout ) + count * ( 8 + %d );\n}\n\n", n, body)

	g.pf("static SCHEMA_UNUSED int64_t %s( const %s * values, int64_t count, uint8_t * buffer, int64_t capacity )\n{\n",
		g.api(st.Name, "fixed_save"), st.Name)
	g.pf("    const int64_t need = %s( count );\n", g.api(st.Name, "fixed_measure"))
	g.pf("    uint8_t * at;\n    int64_t k;\n")
	g.pf("    if ( count < 0 || values == NULL || buffer == NULL || capacity < need ) { return -1; }\n")
	g.pf("    memset( buffer, 0, (size_t) kTableFixedHeaderBytes ); /* the seven reserved bytes, and the rest of the header */\n")
	g.pf("    buffer[0] = kTableFixedForm;\n")
	g.pf("    table_fixed_put64( buffer + kTableFixedHashAt, %s_fixed_hash );\n", n)
	g.pf("    table_fixed_put32( buffer + kTableFixedHeaderBytes, (uint32_t) sizeof( %s_fixed_layout ) );\n", n)
	g.pf("    memcpy( buffer + kTableFixedHeaderBytes + 4, %s_fixed_layout, sizeof( %s_fixed_layout ) );\n", n, n)
	g.pf("    at = buffer + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( %s_fixed_layout );\n", n)
	g.pf("    for ( k = 0; k < count; ++k )\n    {\n")
	g.pf("        table_fixed_put64( at, %s_fixed_hash );\n", n)
	g.pf("        memset( at + 8, 0, (size_t) %d ); /* the prefill's zeros */\n", body)
	g.pf("        %s( at + 8, values + k );\n", g.sym(st.Name, "fixed_write_body"))
	g.pf("        at += 8 + %d;\n    }\n    return need;\n}\n\n", body)

	// ---- LOAD(R, file): §5.3'S ELEVEN STEPS, IN ORDER ----
	//
	// The order is load-bearing and it is NOT §1.1's order. A layout arriving on
	// the wire is no longer walked: the hash is looked up, then the floor, then
	// the bytes are COMPARED with the lock's. The seven §1.1 malformations under
	// a KNOWN hash all come back as ONE name, layout_malformed — "a lie about a
	// known version" (§5.3, bill §12.4).
	g.pf("/* THE READ: SELECT BY HASH, NEVER PARSE A STRANGER (docs/FIXED-FORM-ALGORITHM.md\n")
	g.pf("   §5.3). The header's hash is TAKEN AS GIVEN — the definitions digest is not\n")
	g.pf("   on the wire, so the hash cannot be re-derived from a file at all — and it\n")
	g.pf("   selects one entry of the lineage THE BUILD laid down. Outside the lineage is\n")
	g.pf("   layout_newer (ship the reader); below the floor is layout_unsupported\n")
	g.pf("   (upgrade the client); a known hash whose bytes differ is layout_malformed.\n")
	g.pf("   Then ONE loop over ONE plan, the identity plan for this build's own hash and\n")
	g.pf("   the entry's own static plan otherwise. Same loop either way (§3.4).\n\n")
	g.pf("   THE PREFILL IS EXACTLY THE BYTES THE PLAN DOES NOT LAND, and on the identity\n")
	g.pf("   plan that list is EMPTY — so this read writes no byte twice, and it is one\n")
	g.pf("   rule for both plans rather than a flag that asks which one this is.\n\n")
	g.pf("   THE PLAN ARGUMENTS STAY AND THE CACHE IS IGNORED (§5.9 #5): `plan` and\n")
	g.pf("   `plan_capacity` are a CAPACITY DECLARATION and are no longer written\n")
	g.pf("   through — an entry whose plan is longer refuses plan_too_large BY NAME — and\n")
	g.pf("   `cache` has nothing left to hold, because every older plan is static and\n")
	g.pf("   built from the lock's bytes. The signature keeps both so every caller in\n")
	g.pf("   test/c-tables and the paired bench still compiles (§5.6 retires mechanisms,\n")
	g.pf("   not API). */\n")
	g.pf("static SCHEMA_UNUSED int64_t %s( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n",
		g.api(st.Name, "fixed_load"), st.Name)
	g.pf("                                 TableFixedEntry * plan, int32_t plan_capacity, TableFixedPlanCache * cache, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n")
	g.pf("    uint32_t layout_bytes;\n    const uint8_t * layout;\n    const uint8_t * at;\n")
	g.pf("    uint64_t hash;\n    int64_t rest, record_bytes, count, k;\n")
	g.pf("    int32_t pick;\n")
	g.pf("    int32_t census_unknown = 0;\n    int32_t census_kind = 0;\n")
	g.pf("    const TableFixedEntry * entries = %s_fixed_plan;\n", n)
	g.pf("    int32_t entry_count = %s_fixed_plan_count;\n", n)
	g.pf("    int32_t entry_guarded = %s_fixed_plan_guarded;\n", n)
	g.pf("    const TableFixedFill * fill = NULL;\n")
	g.pf("    int32_t fill_count = 0;\n")
	g.pf("    %s defaults;\n", st.Name)
	g.pf("    (void) plan; /* a CAPACITY DECLARATION now, never written through (§5.9 #5) */\n")
	g.pf("    (void) plan_capacity; /* read where a lineage entry has a plan to measure */\n")
	g.pf("    (void) cache; /* the plans are static: there is no cache to miss (§5.8 row 3) */\n")
	g.pf("    memset( &local, 0, sizeof( local ) );\n")
	g.pf("    if ( report == NULL ) { report = &local; }\n")
	// STEP 1.
	g.pf("    if ( values == NULL || data == NULL || bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = 1; return -1; }\n")
	// STEP 2.
	g.pf("    /* 2. THE FORM BYTE, AND IT SAYS WHICH DIRECTION (§3, §5.3): the registry is\n")
	g.pf("       ordered, so a byte this reader does not carry is named by where it sits\n")
	g.pf("       relative to this form and never by one word for both. */\n")
	g.pf("    if ( data[0] != kTableFixedForm )\n    {\n")
	g.pf("        report->refused = 1;\n")
	g.pf("        report->reason = data[0] == 1 ? SCHEMA_TABLE_PREVIOUS_FORM\n")
	g.pf("                       : data[0] == 2 ? SCHEMA_TABLE_MESSAGE_FORM_AS_FILE\n")
	g.pf("                       : SCHEMA_TABLE_NEWER_FORM;\n")
	g.pf("        return -1;\n    }\n")
	// STEP 3.
	g.pf("    /* 3. the layout's length, and the bytes behind it. */\n")
	g.pf("    layout_bytes = table_fixed_get32( data + kTableFixedHeaderBytes );\n")
	g.pf("    if ( (int64_t) layout_bytes + kTableFixedHeaderBytes + 4 > bytes ) { report->refused = 1; report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED; return -1; }\n")
	g.pf("    layout = data + kTableFixedHeaderBytes + 4;\n")
	// STEP 4.
	g.pf("    /* 4. THE HEADER'S HASH, TAKEN AS GIVEN. Nothing is recomputed from the wire:\n")
	g.pf("       the digest is not on the wire, so the hash cannot be re-derived (§5.6). */\n")
	g.pf("    hash = table_fixed_get64( data + kTableFixedHashAt );\n")
	// STEPS 5 and 6.
	g.pf("    /* 5 and 6. SELECT BY HASH, THEN THE FLOOR. Both report THE FILE'S hash. */\n")
	g.pf("    pick = table_fixed_select( %s_fixed_known, %s_fixed_known_count, hash );\n", n, n)
	g.pf("    if ( pick < 0 ) { return table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_NEWER, hash ); }\n")
	g.pf("    if ( pick < %s_fixed_floor ) { return table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_UNSUPPORTED, hash ); }\n", n)
	// STEP 7.
	g.pf("    /* 7. A KNOWN HASH IS READ UNDER THE LOCK'S LAYOUT BYTES. A difference is ONE\n")
	g.pf("       name — a lie about a known version — and §1.1's seven rules do not run at\n")
	g.pf("       read time at all: no stranger's layout is ever walked. */\n")
	g.pf("    if ( (int64_t) layout_bytes != %s_fixed_known[pick].layout_bytes ||\n", n)
	g.pf("         memcmp( layout, %s_fixed_known[pick].layout, (size_t) layout_bytes ) != 0 )\n", n)
	g.pf("    { report->refused = 1; report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED; return -1; }\n")
	// STEP 8.
	g.pf("    /* 8. the plan this peer resolves to, and the record size FROM THE LOCK. */\n")
	g.pf("    record_bytes = %s_fixed_known[pick].record_bytes;\n", n)
	if older > 0 {
		g.pf("    if ( hash != %s_fixed_hash )\n    {\n", n)
		g.pf("        %s_fixed_lineage_build(); /* once per process, from THE LOCK'S bytes */\n", n)
		g.pf("        if ( %s_fixed_lineage[pick].reason != 0 ) { report->refused = 1; report->reason = %s_fixed_lineage[pick].reason; return -1; }\n", n, n)
		g.pf("        if ( %s_fixed_lineage[pick].count > plan_capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE; return -1; }\n", n)
		g.pf("        entries = %s_fixed_lineage[pick].entries;\n", n)
		g.pf("        entry_count = %s_fixed_lineage[pick].count;\n", n)
		g.pf("        entry_guarded = %s_fixed_lineage[pick].guarded;\n", n)
		g.pf("        fill = %s_fixed_lineage[pick].fill;\n", n)
		g.pf("        fill_count = %s_fixed_lineage[pick].fill_count;\n", n)
		g.pf("        /* THE CENSUS IS THE PLAN'S OWN NUMBER, landed ONCE after the record loop\n")
		g.pf("           and only on a read that RETURNS: REFUSE moves no counter (§5.9 #6). */\n")
		g.pf("        census_unknown = %s_fixed_lineage[pick].unknown;\n", n)
		g.pf("        census_kind = %s_fixed_lineage[pick].kind_mismatch;\n", n)
		g.pf("    }\n")
	}
	// STEPS 9 and 10.
	g.pf("    /* 9 and 10. the tail must be whole records, and they must fit the caller. */\n")
	g.pf("    at = layout + layout_bytes;\n")
	g.pf("    rest = bytes - kTableFixedHeaderBytes - 4 - (int64_t) layout_bytes;\n")
	g.pf("    if ( record_bytes <= 8 || rest %% record_bytes != 0 ) { report->malformed = 1; return -1; }\n")
	g.pf("    count = rest / record_bytes;\n")
	g.pf("    if ( count > capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_BATCH_TOO_LARGE; return -1; }\n")
	// STEP 11.
	g.pf("    /* 11. ONE RECORD LOOP, AND THE PER-RECORD HASH CHECK COMES FIRST — before\n")
	g.pf("       the prefill, so a refusal has written not one destination byte (§5.3,\n")
	g.pf("       §5.8 row 9). THE DEFAULT IMAGE IS RESET ONCE PER READ, not once per\n")
	g.pf("       record, and only where there is a range to prefill at all. It is the\n")
	g.pf("       caller's own stack and this codec allocates nothing. */\n")
	g.pf("    if ( fill_count > 0 )\n    {\n")
	g.pf("        memset( (void *) &defaults, 0, sizeof( defaults ) ); /* padding included, so the prefill is deterministic */\n")
	g.pf("        %s( &defaults );\n    }\n", g.api(st.Name, "reset"))
	g.pf("    for ( k = 0; k < count; ++k )\n    {\n")
	g.pf("        if ( table_fixed_get64( at ) != hash ) { report->refused = 1; report->reason = SCHEMA_TABLE_NO_LAYOUT; return -1; }\n")
	g.pf("        table_fixed_fill_run( fill, fill_count, (const uint8_t *) &defaults, (uint8_t *) ( values + k ) );\n")
	g.pf("        table_fixed_run( entries, entry_count, entry_guarded, at + 8, (uint8_t *) ( values + k ), report );\n")
	if ir.TableFixedClampNeeded(st) {
		g.pf("        /* AND THE BOUNDS THE LOOP DOES NOT HOLD, straight-line over the\n")
		g.pf("           storage it just wrote: the same pass for either plan (§3.4). */\n")
		g.pf("        %s( values + k, report );\n", g.sym(st.Name, "fixed_clamp"))
	}
	g.pf("        at += record_bytes;\n    }\n")
	g.pf("    /* THE COMPILE CENSUS LANDS HERE, ONCE, on a read that returns (§5.9 #6). */\n")
	g.pf("    report->unknown += census_unknown;\n")
	g.pf("    report->kind_mismatch += census_kind;\n")
	g.pf("    return count;\n}\n\n")
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

// fixedDstRow is one TableFixedDst: MY side of a layout entry, in C. Every
// number in it is asserted against offsetof above.
func (g *tableGen) fixedDstRow(e ir.TableFixedLayoutEntry) string {
	d := e.Dst
	stride := int64(0)
	switch {
	case d.Stride1:
		stride = 1
	case d.Stride != nil:
		stride = ir.TableFixedStrideBytes(g.unit, d.Stride)
	}
	dst := g.fixedTerms(d.Dst)
	aux := g.fixedTerms(d.Aux)
	if d.AuxExtendsDst {
		// a union's tag sits INSIDE the union storage the destination named,
		// and the tag leads that storage, so the two offsets are the same byte
		aux = dst + g.fixedTerms(d.Aux)
	}
	return fmt.Sprintf("{ %d, %d, %d, %d, %d }", dst, stride, aux, d.Counted, d.Meta)
}

// fixedTerms sums a destination's offsetof terms.
func (g *tableGen) fixedTerms(terms []ir.TableFixedTerm) int64 {
	total := int64(0)
	for _, t := range terms {
		total += g.fixedTermOffset(t)
	}
	return total
}

func (g *tableGen) fixedTermOffset(t ir.TableFixedTerm) int64 {
	if un := g.unionNamed(t.Type); un != nil {
		if t.Member == "type" {
			return 0
		}
		return ir.TableFixedUnionArmOffset(g.unit, un)
	}
	if st := g.structNamed(t.Type); st != nil {
		return ir.TableFixedMemberOffset(g.unit, st, t.Member)
	}
	return 0
}

func (g *tableGen) unionNamed(name string) *ir.Union {
	if un := g.unit.Unions[name]; un != nil {
		return un
	}
	return g.unit.TableUnions[name]
}

func (g *tableGen) structNamed(name string) *ir.Struct {
	if st := g.unit.Tables[name]; st != nil {
		return st
	}
	return g.unit.Structs[name]
}

func (g *tableGen) emitFixedByteArray(b []byte) {
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

/* ---- the read-side bounds --------------------------------------------------

A fixed record is a positional image, so the ONE read loop moves bytes and asks
nothing about what they mean. What a declaration bounds is held HERE, as
straight-line code in the generated decode, after the copy — never as plan
entries, which would be a test per bounded field on every read of every record
and is the cost the identity plan exists to avoid.

THE PASS RUNS OVER STORAGE, so ONE pass covers both plans. It walks only what a
read can have written: a counted array's LIVE elements and never its slack, an
optional's payload only when the present byte says so. */

// emitFixedClampBodyFn is ONE type's bounds, the twin of its write body.
func (g *tableGen) emitFixedClampBodyFn(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("/* %s's read-side bounds. */\n", st.Name)
	g.pf("static SCHEMA_UNUSED %s void %s( %s * value, int32_t * clamped, int32_t * damaged )\n{\n",
		tableInlineMacro(g.unit.Package), g.sym(st.Name, "fixed_clamp_body"), st.Name)
	g.pf("    (void) value; (void) clamped; (void) damaged;\n")
	for _, f := range st.Fields {
		g.emitFixedClampField(f, "value->", 4)
	}
	g.pf("}\n\n")
}

// emitFixedClamp is the ROOT's entry point: the one call a read makes, and the
// one place the count reaches the report.
func (g *tableGen) emitFixedClamp(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("/* THE READ-SIDE BOUNDS (docs/SPEC-TABLES.md §3.4): a ranged scalar's\n")
	g.pf("   declared min and max, and an ORDINAL's set — a union tag past the arm\n")
	g.pf("   count, an enum ordinal past the enum's top value. Straight-line, after\n")
	g.pf("   the copy, over STORAGE, so the identity plan and a plan compiled from a\n")
	g.pf("   stranger's layout are held to the same numbers by the same pass. Every\n")
	g.pf("   clamp COUNTS. */\n")
	g.pf("static SCHEMA_UNUSED void %s( %s * value, TableReport * report )\n{\n",
		g.sym(st.Name, "fixed_clamp"), st.Name)
	g.pf("    int32_t clamped = 0;\n")
	g.pf("    int32_t damaged = 0;\n")
	g.pf("    %s( value, &clamped, &damaged );\n", g.sym(st.Name, "fixed_clamp_body"))
	g.pf("    report->clamped += clamped;\n")
	g.pf("    /* ILL-FORMED TEXT IS FRAMING-CLASS DAMAGE (§3, §4), so it lands on the\n")
	g.pf("       one flag and not on a counter: the field read its declared default\n")
	g.pf("       and the rest of the record stands. */\n")
	g.pf("    if ( damaged != 0 ) { report->malformed = 1; }\n}\n\n")
}

func (g *tableGen) emitFixedClampField(f *ir.Field, val string, indent int) {
	if !ir.TableFixedClampNeededField(f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		/* AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§3.4), so it is not
		   held to a bound either: what rides there is zero from the template
		   and nobody wrote it. */
		g.pf("%sif ( %s%s_present )\n%s{\n", ind, val, f.Name, ind)
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
		g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < %d; ++i )\n%s    {\n", ind, ind, ind, f.KeyEnumRef.Max, ind)
		// C's storage IS the array — the keyed wrapper is C++'s (§2.4).
		g.emitFixedClampElement(f, val+f.Name+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed:
		g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < %d; ++i )\n%s    {\n", ind, ind, ind, f.ArrayBound, ind)
		g.emitFixedClampElement(f, val+f.Name+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayCounted:
		g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < (int64_t) %s%s_count; ++i )\n%s    {\n", ind, ind, ind, val, f.Name, ind)
		g.emitFixedClampElement(f, val+f.Name+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		g.emitFixedTextContent(f, val, ind)
	default:
		g.emitFixedClampElement(f, val+f.Name, indent)
	}
}

/*
emitFixedTextContent is THE ONE CONTENT RULE THE WIRE HAS (§3, §4), the C

	twin of the reference's: a `string(N)`'s used bytes are well-formed UTF-8
	with no zero among them and a `wstring(N)`'s used units are paired UTF-16
	with no zero unit, checked over the USED LENGTH and nothing else. A payload
	that is not text is DAMAGE and not data: the field reads its declared
	default, one `malformed` counts, and the rest of the record stands.
*/
func (g *tableGen) emitFixedTextContent(f *ir.Field, val, ind string) {
	call := fmt.Sprintf("table_wire_utf8( (const uint8_t *) %s%s, (uint64_t) %s%s_length )", val, f.Name, val, f.Name)
	if f.Type.Kind == ir.TWString {
		call = fmt.Sprintf("table_wire_utf16( (const uint8_t *) %s%s, (int64_t) %s%s_length )", val, f.Name, val, f.Name)
	}
	g.pf("%sif ( !%s )\n%s{\n", ind, call, ind)
	g.pf("%s    memset( %s%s, 0, sizeof( %s%s ) );\n", ind, val, f.Name, val, f.Name)
	if f.HasDefault && len(f.DefBytes) != 0 {
		g.pf("%s    { static const uint8_t initial[] = { %s }; memcpy( %s%s, initial, sizeof( initial ) ); } /* the declared default */\n",
			ind, byteLiterals(f.DefBytes), val, f.Name)
	}
	g.pf("%s    %s%s_length = %d;\n", ind, val, f.Name, len(f.DefBytes))
	g.pf("%s    (*damaged)++;\n%s}\n", ind, ind)
}

func (g *tableGen) emitFixedClampElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if ir.TableFixedClampNeeded(r) {
				g.pf("%s%s( &%s, clamped, damaged );\n", ind, g.sym(r.Name, "fixed_clamp_body"), expr)
			}
			return
		case *ir.Union:
			g.pf("%sif ( (uint32_t) %s.type > %du ) { %s.type = %s; (*clamped)++; }\n",
				ind, expr, r.Max, expr, enumNoneConst(f.Type.Name+"Type"))
			if !ir.TableFixedClampNeededUnionArm(r) {
				return
			}
			g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
			for _, v := range r.Variants {
				if v.F == nil || !ir.TableFixedClampNeededField(v.F) {
					continue
				}
				g.pf("%s    case %s:\n%s    {\n", ind, enumConst(f.Type.Name+"Type", v.Name), ind)
				g.emitFixedClampElement(v.F, fmt.Sprintf("%s.as.%s", expr, v.Name), indent+8)
				g.pf("%s        break;\n%s    }\n", ind, ind)
			}
			g.pf("%s    default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			g.pf("%sif ( (uint64_t) %s > %du ) { %s = %s; (*clamped)++; }\n",
				ind, expr, r.Max, expr, enumNoneConst(f.Type.Name))
			return
		}
		return
	}
	width := int(ir.TableFixedStorageBytes(f.Type))
	switch {
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		/* A BOUNDED FLOAT'S LOW TEST IS NEGATED, the reference's twin: IEEE
		   says every ordered comparison against a NaN is false, so `v < lo`
		   and `v > hi` are both false and a NaN would land WHOLE and count
		   nothing. `!( v >= lo )` is true for a NaN and for every value under
		   the minimum, so a NaN lands `min` and counts one, as -inf does; and
		   -0.0 against a min of +0.0 compares equal, so it stays in range and
		   lands as written. */
		g.fixedClampBoth(expr, "!( "+expr+" >= "+formatFloat(f.FMin, true)+" )", formatFloat(f.FMin, true),
			expr+" > "+formatFloat(f.FMax, true), formatFloat(f.FMax, true), ind)
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		g.fixedClampBoth(expr, "!( "+expr+" >= "+formatFloat(f.FMin, false)+" )", formatFloat(f.FMin, false),
			expr+" > "+formatFloat(f.FMax, false), formatFloat(f.FMax, false), ind)
	default:
		signed := ir.TableKindSigned(ir.TableScalarKind(f))
		// A FIXED-POINT FIELD'S BOUNDS ARE IN VALUE UNITS and its storage is
		// raw, so ir.TableRawRange shifts them by F before either end is
		// spelled — the same numbers the variable form's codec clamps at.
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			// A 128-BIT VALUE IS A STRUCT ON THIS LEG, so < and > are the
			// runtime's compare and not an operator (fixedruntime.go's
			// table_fixed_cmp128_*). Everything narrower is the plain compare.
			cmp := func(op, lit string, v *big.Int) string {
				if width != 16 {
					return fmt.Sprintf("%s %s %s", expr, op, lit)
				}
				fn := "table_fixed_cmp128_u"
				if signed {
					fn = "table_fixed_cmp128_i"
				}
				loLane, hiLane := wideLanes(v)
				return fmt.Sprintf("%s( %s, %sull, %sull ) %s 0", fn, expr, hiLane, loLane, op)
			}
			lo, hi := tableIntLit(rlo, signed, width), tableIntLit(rhi, signed, width)
			switch {
			case low && high:
				g.fixedClampBoth(expr, cmp("<", lo, rlo), lo, cmp(">", hi, rhi), hi, ind)
			case low:
				g.fixedClampEnd(expr, cmp("<", lo, rlo), lo, ind)
			case high:
				g.fixedClampEnd(expr, cmp(">", hi, rhi), hi, ind)
			}
		}
		if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
			maxv := fmt.Sprintf("%dull", (uint64(1)<<f.Type.Width)-1)
			g.pf("%s/* bits(%d) width clamp */\n", ind, f.Type.Width)
			g.fixedClampEnd(expr, expr+" > "+maxv, maxv, ind)
		}
	}
}

/*
THE CLAMP IS BRANCHLESS AND THE COUNT IS AN ADD, the C twin of the

	reference's: a bounds pass over a record that is nearly always in range is a
	pass of branches nearly always not taken, and a branch in an array loop's
	body is what stops the loop vectorizing at all.
*/
func (g *tableGen) fixedClampBoth(expr, loTest, lo, hiTest, hi, ind string) {
	/* THE COUNT IS AN OR OF THE TWO ENDS, and the casts are what say the `|` is
	   meant: clang reads a bitwise operator between two comparisons as a
	   mistyped `||` and reds it (-Wbitwise-instead-of-logical), which is a good
	   warning about code that is not this. A sum would silence it too and is
	   measurably slower (test/bench/fixedform_measure.cpp). */
	g.pf("%s(*clamped) += (int) ( %s ) | (int) ( %s );\n", ind, loTest, hiTest)
	g.pf("%s%s = ( %s ) ? %s : ( ( %s ) ? %s : %s );\n", ind, expr, loTest, lo, hiTest, hi, expr)
}

/*
fixedClampEnd is the one-ended twin: an end the storage's own width already

	holds is an end the emitter drops (tableClampEnds).
*/
func (g *tableGen) fixedClampEnd(expr, test, bound, ind string) {
	g.pf("%s(*clamped) += ( %s );\n", ind, test)
	g.pf("%s%s = ( %s ) ? %s : %s;\n", ind, expr, test, bound, expr)
}

// tableFixedOnce is THE ONCE-FLAG AND ITS PUBLICATION, emitted beside the lazy
// build of the older lineage plans and nowhere else — a unit with no older peer
// has no lazy build, so it carries not one line of this.
//
// THE BUG IT CLOSES: the flag used to be raised BEFORE the loop, which made a
// second thread racing the first older-peer load return from the build with the
// plans still zeros. A zeroed TableFixedLineagePlan is entries NULL, count 0 and
// reason 0 — and step 8 of §5.3 accepts that: reason 0 is "this peer planned
// fine", count 0 is "a plan with no entries", so the read would land a
// PREFILL-ONLY record instead of refusing. The fix is the ordinary one: build
// into the static storage first, publish the flag LAST, and make a load that
// arrives before the publication WAIT for it rather than read what is not there.
//
// WHY NOT <stdatomic.h>: the generated headers compile under -std=c99 (the
// Makefile's TABLES_CFLAGS), and the zero-cost gate in make/c.mk REFUSES the
// string "stdatomic" in a value-only unit's header by name. So the acquire and
// the release are the GNU/clang __atomic builtins, which C99 has and which name
// no header; MSVC gets the interlocked intrinsic it already has; and a compiler
// with neither falls back to a plain store AFTER THE LOOP, where the ordering
// the comment asks for is the only ordering left to ask for.
//
// THE REMAINING WINDOW, NAMED, on that last fallback only: with no
// compare-and-exchange there is nothing to claim with, so two threads can both
// walk the build and write THE SAME BYTES into the same storage. No load ever
// reads a zeroed plan — a thread reaches the plan only after its own build
// returned — but the duplicate write is a data race in the abstract machine and
// this leg owes the fix the spec already names: §5.9 #7's GENERATOR-SIDE
// COMPILE, which lays every older peer's plan down as static const data at
// generate time and deletes the lazy build, the flag and this whole block with
// it. Until then the build is this, and the ordering is published.
const tableFixedOnce = `
#ifndef SCHEMA_TABLE_FIXED_ONCE
#define SCHEMA_TABLE_FIXED_ONCE
/* 0 = nobody has built, 1 = one thread is building, 2 = the plans are readable */
#define SCHEMA_TABLE_FIXED_ONCE_UNBUILT 0
#define SCHEMA_TABLE_FIXED_ONCE_BUILDING 1
#define SCHEMA_TABLE_FIXED_ONCE_READY 2
#if defined( __GNUC__ ) || defined( __clang__ )
typedef int TableFixedOnce;
#define table_fixed_once_ready( f ) ( __atomic_load_n( ( f ), __ATOMIC_ACQUIRE ) == SCHEMA_TABLE_FIXED_ONCE_READY )
#define table_fixed_once_publish( f ) __atomic_store_n( ( f ), SCHEMA_TABLE_FIXED_ONCE_READY, __ATOMIC_RELEASE )
static SCHEMA_UNUSED int table_fixed_once_claim( TableFixedOnce * f )
{
    int want = SCHEMA_TABLE_FIXED_ONCE_UNBUILT;
    return __atomic_compare_exchange_n( f, &want, SCHEMA_TABLE_FIXED_ONCE_BUILDING, 0,
                                        __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE ) ? 1 : 0;
}
/* THE WAIT. The build is a BOUNDED walk over the lock's own bytes — no file, no
   allocation, no lock of its own and no call out — so the thread that claimed it
   always reaches the publish, and this spin always ends. It runs at most once
   per process per table and never on the record loop. */
static SCHEMA_UNUSED void table_fixed_once_wait( TableFixedOnce * f )
{
    while ( __atomic_load_n( f, __ATOMIC_ACQUIRE ) != SCHEMA_TABLE_FIXED_ONCE_READY )
    {
        /* nothing: the load is the whole body, and it is an acquire */
    }
}
#elif defined( _MSC_VER )
typedef long TableFixedOnce;
#define table_fixed_once_ready( f ) ( _InterlockedCompareExchange( ( f ), SCHEMA_TABLE_FIXED_ONCE_READY, SCHEMA_TABLE_FIXED_ONCE_READY ) == SCHEMA_TABLE_FIXED_ONCE_READY )
#define table_fixed_once_publish( f ) ( (void) _InterlockedExchange( ( f ), SCHEMA_TABLE_FIXED_ONCE_READY ) )
#define table_fixed_once_claim( f ) ( _InterlockedCompareExchange( ( f ), SCHEMA_TABLE_FIXED_ONCE_BUILDING, SCHEMA_TABLE_FIXED_ONCE_UNBUILT ) == SCHEMA_TABLE_FIXED_ONCE_UNBUILT )
#define table_fixed_once_wait( f ) do { while ( !table_fixed_once_ready( f ) ) { } } while ( 0 )
#else
/* NO ATOMIC ON THIS COMPILER, AND THE WINDOW IS NAMED. The publish is a plain
   store AFTER the loop, so no thread can read a zeroed plan: a thread reaches
   the plans only after its own build wrote them. What is left is that two
   threads may build at once and write THE SAME BYTES twice — a race in the
   abstract machine with one outcome in practice — and the fix is not a bigger
   macro here, it is §5.9 #7's generator-side COMPILE, which lays these plans
   down as static const data and removes the lazy build entirely. OWED. */
typedef int TableFixedOnce;
#define table_fixed_once_ready( f ) ( *( f ) == SCHEMA_TABLE_FIXED_ONCE_READY )
#define table_fixed_once_publish( f ) ( *( f ) = SCHEMA_TABLE_FIXED_ONCE_READY )
#define table_fixed_once_claim( f ) ( 1 )
#define table_fixed_once_wait( f ) ( (void) ( f ) )
#endif
#endif
`
