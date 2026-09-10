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

func (g *tableGen) emitFixedForm(members []*ir.Struct) {
	roots := g.fixedRoots(members)
	if len(roots) == 0 {
		return
	}
	g.pf("/* ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n\n")
	g.pf("   A record is an eight-byte hash of the writer's LAYOUT and then\n")
	g.pf("   the values in declared order, every field at its declared storage width.\n")
	g.pf("   The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("   ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("   the writer's own layout for anybody else. */\n\n")
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	g.emitFixedLayoutAsserts(order)
	for _, st := range order {
		g.pf("static SCHEMA_UNUSED %s void %s( uint8_t * b, const %s * value );\n",
			tableInlineMacro(g.unit.Package), g.sym(st.Name, "fixed_write_body"), st.Name)
	}
	g.pf("\n")
	for _, st := range order {
		g.emitFixedWriteBody(st)
	}
	for _, st := range roots {
		g.emitFixedRoot(st)
	}
}

// emitFixedLayoutAsserts is what makes a generator-computed offset safe: every
// number the plan and the destination rows below are built from, checked
// against the compiler's own offsetof and sizeof.
func (g *tableGen) emitFixedLayoutAsserts(order []*ir.Struct) {
	g.pf("/* THE C ABI AGREES WITH THE PLAN. C has no constexpr, so the offsets in the\n")
	g.pf("   plans below were computed by the schema compiler rather than folded by\n")
	g.pf("   this one; these are that arithmetic checked against the compiler's own,\n")
	g.pf("   at build time, one line per fact the plans rest on. */\n")
	unions := map[string]bool{}
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
		for _, f := range st.Fields {
			un, ok := f.Type.Ref.(*ir.Union)
			if !ok || f.Type.Kind != ir.TNamed || unions[f.Type.Name] {
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
	ind := strings.Repeat(" ", indent)
	base := off
	if f.Type.Optional {
		g.pf("%stable_fixed_put8( %s + %d, %s%s_present ? 1 : 0 );\n", ind, buf, base, val, f.Name)
		base += ir.TableFixedPresentBytes
	}
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
	hash := ir.TableFixedLayoutHash(layout)
	body := ir.TableFixedTypeBytes(st)
	n := ir.RustSnake(st.Name)

	g.pf("/* ---- %s, the fixed form ---- */\n\n", st.Name)
	g.pf("/* THE BODY IS ONE CONSTANT on this form: it is the same size for every\n")
	g.pf("   value the type can hold (docs/SPEC-TABLES.md §3.4). */\n")
	g.pf("static SCHEMA_UNUSED const int64_t %s_fixed_body_bytes = %d;\n", n, body)
	g.pf("static SCHEMA_UNUSED const int64_t %s_fixed_record_bytes = 8 + %d; /* the hash and the body */\n", n, body)
	g.pf("static SCHEMA_UNUSED const uint64_t %s_fixed_hash = 0x%016xull; /* fnv1a64 over the layout's bytes */\n\n", n, hash)

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
		g.pf("    { %du, %du, %du, %du, %s, %s, %d, %d, 0, 0 }, /* %s */\n",
			e.Src, e.Dst, e.Size, e.Aux, guard, fixedOpName(e.Op), e.Arg, e.Meta, e.Note)
	}
	g.pf("};\n")
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_plan_count = %d;\n", n, len(plan))
	g.pf("static SCHEMA_UNUSED const int32_t %s_fixed_plan_guarded = %d;\n\n", n, guarded)

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

	g.pf("/* THE READ: a prefill and ONE loop over ONE plan — the identity plan when\n")
	g.pf("   the layout's hash is this build's own, and a plan compiled once from the\n")
	g.pf("   writer's layout otherwise. Same loop either way (§3.4). */\n")
	g.pf("static SCHEMA_UNUSED int64_t %s( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n",
		g.api(st.Name, "fixed_load"), st.Name)
	g.pf("                                 TableFixedEntry * plan, int32_t plan_capacity, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n")
	g.pf("    uint32_t layout_bytes;\n    const uint8_t * layout;\n    const uint8_t * at;\n")
	g.pf("    uint64_t hash;\n    int64_t rest, record_bytes, count, k;\n")
	g.pf("    const TableFixedEntry * entries = %s_fixed_plan;\n", n)
	g.pf("    int32_t entry_count = %s_fixed_plan_count;\n", n)
	g.pf("    int32_t entry_guarded = %s_fixed_plan_guarded;\n", n)
	g.pf("    memset( &local, 0, sizeof( local ) );\n")
	g.pf("    if ( report == NULL ) { report = &local; }\n")
	g.pf("    if ( values == NULL || data == NULL || bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = 1; return -1; }\n")
	g.pf("    /* THE FORM BYTE IS READ FIRST, AND IT SAYS WHICH DIRECTION (§3, §3.4):\n")
	g.pf("       the registry is ordered, so a byte this reader does not carry is named\n")
	g.pf("       by where it sits relative to this form and never by one word for both. */\n")
	g.pf("    if ( data[0] != kTableFixedForm )\n    {\n")
	g.pf("        report->refused = 1;\n")
	g.pf("        report->reason = data[0] == 1 ? SCHEMA_TABLE_PREVIOUS_FORM\n")
	g.pf("                       : data[0] == 2 ? SCHEMA_TABLE_MESSAGE_FORM_AS_FILE\n")
	g.pf("                       : SCHEMA_TABLE_NEWER_FORM;\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    layout_bytes = table_fixed_get32( data + kTableFixedHeaderBytes );\n")
	g.pf("    if ( (int64_t) layout_bytes + kTableFixedHeaderBytes + 4 > bytes ) { report->refused = 1; report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED; return -1; }\n")
	g.pf("    layout = data + kTableFixedHeaderBytes + 4;\n")
	g.pf("    hash = table_fixed_hash_of( layout, (int64_t) layout_bytes );\n")
	g.pf("    at = layout + layout_bytes;\n")
	g.pf("    rest = bytes - kTableFixedHeaderBytes - 4 - (int64_t) layout_bytes;\n")
	g.pf("    record_bytes = 8 + %d;\n", body)
	g.pf("    if ( hash != %s_fixed_hash )\n    {\n", n)
	g.pf("        /* ANOTHER WRITER: the same loop, over a plan compiled from its layout.\n")
	g.pf("           THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS TOUCHED, and\n")
	g.pf("           every rule it fails refuses under ITS OWN NAME (docs/SPEC-TABLES.md §3.4). */\n")
	g.pf("        TableFixedLayoutView parsed;\n        int why = SCHEMA_TABLE_LAYOUT_MALFORMED;\n        int32_t compiled_guarded = 0;\n        int32_t made;\n")
	g.pf("        if ( !table_fixed_parse_layout( layout, (int64_t) layout_bytes, &parsed, &why ) ) { report->refused = 1; report->reason = why; return -1; }\n")
	g.pf("        made = table_fixed_compile( &parsed, %s_fixed_layout, (int32_t) sizeof( %s_fixed_layout ), %s_fixed_dst, plan, plan_capacity, &compiled_guarded, report );\n", n, n, n)
	g.pf("        if ( made < 0 ) { report->refused = 1; report->reason = ( made == -2 ) ? SCHEMA_TABLE_LAYOUT_MALFORMED : SCHEMA_TABLE_PLAN_TOO_LARGE; return -1; }\n")
	g.pf("        entries = plan;\n        entry_count = made;\n        entry_guarded = compiled_guarded;\n")
	g.pf("        record_bytes = 8 + (int64_t) table_fixed_entry_at( &parsed, 0 ).size;\n")
	g.pf("    }\n")
	g.pf("    /* THE HEADER NAMES THE LAYOUT ONCE, and it is checked LAST of the three:\n")
	g.pf("       the layout's own rules each refuse under their own name first, so a\n")
	g.pf("       broken layout is never reported as a lying header. A header whose hash\n")
	g.pf("       is not the hash of the layout behind it is refused (§3). */\n")
	g.pf("    if ( table_fixed_get64( data + kTableFixedHashAt ) != hash ) { report->refused = 1; report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED; return -1; }\n")
	g.pf("    if ( record_bytes <= 8 || rest %% record_bytes != 0 ) { report->malformed = 1; return -1; }\n")
	g.pf("    count = rest / record_bytes;\n")
	g.pf("    if ( count > capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_BATCH_TOO_LARGE; return -1; }\n")
	g.pf("    for ( k = 0; k < count; ++k )\n    {\n")
	g.pf("        %s( values + k ); /* the declared defaults, one prefill */\n", g.api(st.Name, "reset"))
	g.pf("        if ( table_fixed_get64( at ) != hash ) { report->refused = 1; report->reason = SCHEMA_TABLE_NO_LAYOUT; return -1; }\n")
	g.pf("        table_fixed_run( entries, entry_count, entry_guarded, at + 8, (uint8_t *) ( values + k ), report );\n")
	g.pf("        at += record_bytes;\n    }\n    return count;\n}\n\n")
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
