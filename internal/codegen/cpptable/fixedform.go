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
//	hash the SAME LOOP runs over a plan compiled once from the writer's layout.
//	There is no second reader and no fast/slow cliff — the owner's own ruling,
//	which §3.4 quotes him on.
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

func (g *tableGen) emitFixedForm(members []*ir.Struct) {
	roots := g.fixedRoots(members)
	if len(roots) == 0 {
		return
	}
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's LAYOUT and then\n")
	g.pf("// the values in declared order, every field at its declared storage width.\n")
	g.pf("// The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("// ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("// the writer's own layout for anybody else.\n\n")
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	g.emitFixedLayoutAsserts(order)
	for _, st := range order {
		g.emitFixedWriteBody(st)
	}
	for _, st := range roots {
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
	ind := strings.Repeat(" ", indent)
	base := off
	if f.Type.Optional {
		g.pf("%sTableFixedPut8( %s + %d, %s.%s_present ? 1 : 0 );\n", ind, buf, base, val, f.Name)
		base += ir.TableFixedPresentBytes
	}
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
	hash := ir.TableFixedLayoutHash(layout)
	body := ir.TableFixedTypeBytes(st)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("// MeasureBody IS A CONSTEXPR on this form: the body is the same size for\n")
	g.pf("// every value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("constexpr int64_t %sFixedBodyBytes = %d;\n", st.Name, body)
	g.pf("constexpr int64_t %sFixedRecordBytes = 8 + %sFixedBodyBytes; // the hash and the body\n", st.Name, st.Name)
	g.pf("constexpr uint64_t %sFixedHash = 0x%016xull; // fnv1a64 over the layout's bytes\n\n", st.Name, hash)

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

	flat := ir.TableFixedIdentityFlat(g.unit, st)
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
		g.pf("    { %du, %du, %du, %du, %s, %s, %d, %d, 0, 0 }, // %s\n",
			e.Src, e.Dst, e.Size, e.Aux, guard, fixedOpName(e.Op), e.Arg, e.Meta, e.Note)
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
	g.emitFixedScatter(st)

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

	g.pf("// THE READ. ONE PLAN, and which one is the only thing a peer's shipping\n")
	g.pf("// changes: the IDENTITY PLAN for a record stamped with this build's own\n")
	g.pf("// layout hash, a plan compiled once from the writer's layout for anybody\n")
	g.pf("// else (§3.4). What differs is WHERE THE PLAN IS SPENT and never what it\n")
	g.pf("// says: the identity plan is a compile-time constant, so this compiler\n")
	g.pf("// spends it HERE — one copy of the body and a straight-line scatter, or one\n")
	g.pf("// memcpy where the storage image is the wire image — and a stranger's plan\n")
	g.pf("// arrives at run time and is spent by the interpreter it was always spent by.\n")
	g.pf("//\n")
	g.pf("// THE PREFILL IS EXACTLY THE BYTES THE PLAN DOES NOT LAND. It is a list of\n")
	g.pf("// ranges the plan compiler works out once, and on the identity plan that list\n")
	g.pf("// is EMPTY — so this read writes no byte twice, and it is one rule for both\n")
	g.pf("// plans rather than a flag that asks which one this is.\n")
	g.pf("inline int64_t %sFixedLoad( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n", st.Name, st.Name)
	g.pf("                            TableFixedEntry * plan, int32_t plan_capacity, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n    if ( report == NULL ) { report = &local; }\n")
	g.pf("    if ( data == NULL || bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = true; return -1; }\n")
	g.pf("    // THE FORM BYTE IS READ FIRST, AND IT SAYS WHICH DIRECTION (§3, §3.4):\n")
	g.pf("    // the registry is ordered, so a byte this reader does not carry is named\n")
	g.pf("    // by where it sits relative to this form and never by one word for both.\n")
	g.pf("    if ( data[0] != kTableFixedForm )\n    {\n")
	g.pf("        report->refused = true;\n")
	g.pf("        report->reason = data[0] == kTableWireForm ? previous_form\n")
	g.pf("                       : data[0] == kTableWireMessageForm ? message_form_as_file\n")
	g.pf("                       : newer_form;\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    const uint32_t layout_bytes = TableFixedGet32( data + kTableFixedHeaderBytes );\n")
	g.pf("    if ( (int64_t) layout_bytes + kTableFixedHeaderBytes + 4 > bytes ) { report->refused = true; report->reason = layout_malformed; return -1; }\n")
	g.pf("    const uint8_t * layout = data + kTableFixedHeaderBytes + 4;\n")
	g.pf("    const uint64_t hash = TableFixedHashOf( layout, layout_bytes );\n")
	g.pf("    const uint8_t * at = layout + layout_bytes;\n")
	g.pf("    const int64_t rest = bytes - kTableFixedHeaderBytes - 4 - (int64_t) layout_bytes;\n")
	g.pf("    const TableFixedEntry * entries = %sFixedPlan;\n", st.Name)
	g.pf("    int32_t entry_count = %sFixedPlanCount;\n", st.Name)
	g.pf("    int32_t entry_guarded = %sFixedPlanGuarded;\n", st.Name)
	g.pf("    int64_t record_bytes = %sFixedRecordBytes;\n", st.Name)
	g.pf("    const bool identity = ( hash == %sFixedHash );\n", st.Name)
	g.pf("    // THE PREFILL'S RANGES. EMPTY ON THE IDENTITY PLAN, and not by a flag:\n")
	g.pf("    // the rule is the type's value bytes minus what the plan lands, and the\n")
	g.pf("    // identity plan lands all of them.\n")
	g.pf("    const TableFixedFill * fill = NULL;\n")
	g.pf("    int32_t fill_count = 0;\n")
	g.pf("    if ( !identity )\n    {\n")
	g.pf("        // ANOTHER WRITER: the same loop, over a plan compiled from its layout.\n")
	g.pf("        // THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS TOUCHED, and\n")
	g.pf("        // every rule it fails refuses under ITS OWN NAME (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("        TableFixedLayoutView parsed;\n")
	g.pf("        TableMessageReason why = layout_malformed;\n")
	g.pf("        if ( !TableFixedParseLayout( layout, layout_bytes, parsed, why ) ) { report->refused = true; report->reason = why; return -1; }\n")
	g.pf("        int32_t compiled_guarded = 0;\n")
	g.pf("        uint32_t fill_at = 0;\n")
	g.pf("        const int32_t made = TableFixedCompile( parsed, %sFixedLayout, (int32_t) %sFixedLayoutBytes, %sFixedDst,\n", st.Name, st.Name, st.Name)
	g.pf("                                               %sFixedCover, %sFixedCoverCount,\n", st.Name, st.Name)
	g.pf("                                               plan, plan_capacity, &compiled_guarded, &fill_at, &fill_count, report );\n")
	g.pf("        if ( made < 0 ) { report->refused = true; report->reason = ( made == -2 ) ? layout_malformed : plan_too_large; return -1; }\n")
	g.pf("        entries = plan;\n        entry_count = made;\n        entry_guarded = compiled_guarded;\n")
	g.pf("        fill = (const TableFixedFill *) (const void *) ( (const uint8_t *) plan + fill_at );\n")
	g.pf("        record_bytes = 8 + (int64_t) TableFixedEntryAt( parsed, 0 ).size;\n")
	g.pf("    }\n")
	g.pf("    // THE HEADER NAMES THE LAYOUT ONCE, and it is checked LAST of the three:\n")
	g.pf("    // the layout's own rules each refuse under their own name first, so a\n")
	g.pf("    // broken layout is never reported as a lying header. A header whose hash\n")
	g.pf("    // is not the hash of the layout behind it is refused (§3).\n")
	g.pf("    if ( TableFixedGet64( data + kTableFixedHashAt ) != hash ) { report->refused = true; report->reason = layout_malformed; return -1; }\n")
	g.pf("    if ( record_bytes <= 8 || rest %% record_bytes != 0 ) { report->malformed = true; return -1; }\n")
	g.pf("    const int64_t n = rest / record_bytes;\n")
	g.pf("    if ( n > capacity ) { report->refused = true; report->reason = batch_too_large; return -1; }\n")
	g.pf("    // THE DEFAULT IMAGE IS RESET ONCE PER READ, not once per record, and only\n")
	g.pf("    // where there is a range to prefill at all. It is the caller's own stack\n")
	g.pf("    // and this codec allocates nothing.\n")
	g.pf("    if ( identity )\n    {\n")
	if flat {
		g.pf("        // THE STORAGE IMAGE IS THE WIRE IMAGE for this type, and that is not a\n")
		g.pf("        // claim beside the plan — it IS the plan: the coalescer folded every\n")
		g.pf("        // leaf into ONE copy of the whole body from offset zero, which it can\n")
		g.pf("        // only do when source and destination advance together the whole way.\n")
		g.pf("        // So the read is that memcpy, the scatter is empty and none is emitted,\n")
		g.pf("        // and the prefill's list is empty for the reason above.\n")
		g.pf("        static_assert( sizeof( %s ) >= (size_t) %sFixedBodyBytes, \"%s: the body lands inside the storage\" );\n", st.Name, st.Name, st.Name)
		g.pf("        for ( int64_t k = 0; k < n; ++k )\n        {\n")
		g.pf("            if ( TableFixedGet64( at ) != hash ) { report->refused = true; report->reason = no_layout; return -1; }\n")
		g.pf("            memcpy( (void *) &values[k], at + 8, (size_t) %sFixedBodyBytes );\n", st.Name)
		g.pf("            at += %sFixedRecordBytes;\n        }\n", st.Name)
	} else {
		g.pf("        // ONE COPY OF THE BODY, THEN A STRAIGHT LINE. Same plan, same clamps,\n")
		g.pf("        // same census, same bytes — every offset in the scatter above came out\n")
		g.pf("        // of the plan above it — and no per-entry dispatch, no runtime length\n")
		g.pf("        // and no plan array competing with the record for cache.\n")
		g.pf("        uint8_t image[%sFixedBodyBytes];\n", st.Name)
		g.pf("        for ( int64_t k = 0; k < n; ++k )\n        {\n")
		g.pf("            if ( TableFixedGet64( at ) != hash ) { report->refused = true; report->reason = no_layout; return -1; }\n")
		g.pf("            memcpy( image, at + 8, (size_t) %sFixedBodyBytes );\n", st.Name)
		g.pf("            %sFixedScatter( image, values[k], report );\n", st.Name)
		g.pf("            at += %sFixedRecordBytes;\n        }\n", st.Name)
	}
	g.pf("        return n;\n    }\n")
	g.pf("    // A STRANGER'S LAYOUT: the plan interpreter, and the prefill's ranges.\n")
	g.pf("    // THE DEFAULT IMAGE IS RESET ONCE PER READ, not once per record, and only\n")
	g.pf("    // where there is a range to prefill at all. It is the caller's own stack\n")
	g.pf("    // and this codec allocates nothing.\n")
	g.pf("    %s defaults;\n", st.Name)
	g.pf("    if ( fill_count > 0 )\n    {\n")
	g.pf("        memset( (void *) &defaults, 0, sizeof( defaults ) ); // padding included, so the prefill is deterministic\n")
	g.pf("        %sReset( defaults );\n    }\n", st.Name)
	g.pf("    for ( int64_t k = 0; k < n; ++k )\n    {\n")
	g.pf("        TableFixedFillRun( fill, fill_count, (const uint8_t *) &defaults, (uint8_t *) &values[k] );\n")
	g.pf("        if ( TableFixedGet64( at ) != hash ) { report->refused = true; report->reason = no_layout; return -1; }\n")
	g.pf("        TableFixedRun( entries, entry_count, entry_guarded, at + 8, (uint8_t *) &values[k], report );\n")
	g.pf("        at += record_bytes;\n    }\n    return n;\n}\n\n")
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
func (g *tableGen) emitFixedLayoutAsserts(order []*ir.Struct) {
	g.pf("// THE ABI AGREES WITH THE PLANS. The offsets below were computed by the\n")
	g.pf("// schema compiler rather than folded by this one, because they are the same\n")
	g.pf("// offsets every other port needs; these are that arithmetic checked against\n")
	g.pf("// this compiler's own, at build time, one line per fact the plans rest on.\n")
	unions := map[string]bool{}
	for _, st := range order {
		for _, m := range ir.TableFixedMembers(g.unit, st) {
			if m.Member == "" {
				g.pf("static_assert( sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );\n", m.Owner, m.Value, m.Owner)
				continue
			}
			g.pf("static_assert( %s == %d, \"%s.%s: the fixed form's plan lands here\" );\n",
				offOf(m.Owner, g.fixedMemberSpelling(st, m.Member)), m.Value, m.Owner, m.Member)
		}
		for _, f := range st.Fields {
			un, ok := f.Type.Ref.(*ir.Union)
			if !ok || f.Type.Kind != ir.TNamed || unions[f.Type.Name] {
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

// ---- THE IDENTITY READ -----------------------------------------------------
//
// A RECORD WHOSE HASH IS THIS BUILD'S OWN IS ONE COPY AND A STRAIGHT LINE. The
// plan interpreter is what a STRANGER's layout needs, and it pays for that
// generality on every record: an entry is a switch on an op, a runtime-length
// copy, and a load of five fields from a plan array that the record's own bytes
// are competing for cache with. None of that is unknown here — the identity
// plan is a compile-time constant, so every offset, every length and every
// clamp bound in it is a literal this generator can write down.
//
// So the identity read is:
//
//	  ONE COPY of the whole body into a record image, and then a STRAIGHT-LINE
//	  SCATTER out of it into the caller's storage, constant offset by constant
//	  offset, with the count and text clamps written INLINE where the plan makes
//	  them entries that break a run.
//
// and where the storage image IS the wire image (ir.TableFixedIdentityFlat, and
// the const asserts above are what make that claim checkable), it is ONE MEMCPY
// into the caller's struct and no scatter at all.
//
// THE IMAGE IS NOT A DETOUR. It is a fixed-size local the compiler knows the
// alignment of, filled by one sequential read of the record; the scatter then
// reads it with constant offsets and constant lengths, which is where the moves
// become register-width instructions instead of calls. It is also the shape the
// Rust port arrived at independently (internal/codegen/rusttable), and the two
// agreeing is worth more than either being clever.
//
// NOTHING ABOUT THE CLAMPS IS RELAXED. A count and a text length are checked on
// the read side unconditionally, here exactly as in the interpreter, and the
// clamp census lands in the same report field: the identity hash says the
// writer's LAYOUT was this build's, not that its BYTES are honest.

// fixedScatterUnit is a text entry's code-unit width, and its terminator's.
func fixedScatterUnit(meta int) int64 {
	if meta == 2 {
		return 2
	}
	return 1
}

// emitFixedScatter lays down <T>FixedScatter: the identity plan, unrolled.
func (g *tableGen) emitFixedScatter(st *ir.Struct) {
	if ir.TableFixedIdentityFlat(g.unit, st) {
		return
	}
	plan, guarded := ir.TableFixedBuildPlan(g.unit, st)
	g.pf("// %s's STRAIGHT-LINE SCATTER: the identity plan above, unrolled by this\n", st.Name)
	g.pf("// compiler into constant-offset moves. Every number in it is a number from\n")
	g.pf("// that plan, and the static_asserts at the top of this form tie each one to\n")
	g.pf("// this compiler's own offsetof and sizeof.\n")
	g.pf("inline void %sFixedScatter( const uint8_t * TABLE_RESTRICT image, %s & value, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    uint8_t * TABLE_RESTRICT dst = (uint8_t *) &value;\n")
	g.pf("    int32_t clamped = 0;\n")
	g.pf("    (void) dst; (void) clamped;\n")
	for _, e := range plan[:guarded] {
		g.emitFixedScatterEntry(e, 4)
	}
	// THE ARMS, GROUPED BY THEIR GUARD: consecutive entries of one arm share
	// one test, which is what the interpreter's partition buys per entry.
	for i := guarded; i < len(plan); {
		j := i
		for j < len(plan) && plan[j].Guard == plan[i].Guard && plan[j].Arg == plan[i].Arg {
			j++
		}
		g.pf("    if ( image[%d] == %d )\n    {\n", plan[i].Guard, plan[i].Arg)
		for _, e := range plan[i:j] {
			g.emitFixedScatterEntry(e, 8)
		}
		g.pf("    }\n")
		i = j
	}
	g.pf("    report->clamped += clamped;\n")
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedScatterEntry(e ir.TableFixedLeaf, indent int) {
	ind := strings.Repeat(" ", indent)
	switch e.Op {
	case ir.TableFixedOpCopy:
		g.pf("%smemcpy( dst + %d, image + %d, %d ); // %s\n", ind, e.Dst, e.Src, e.Size, e.Note)
	case ir.TableFixedOpCount:
		g.pf("%s{ // %s\n", ind, e.Note)
		g.pf("%s    int32_t v = (int32_t) TableFixedGet32( image + %d );\n", ind, e.Src)
		g.pf("%s    if ( v < 0 ) { v = 0; clamped++; } else if ( v > %d ) { v = %d; clamped++; }\n", ind, e.Size, e.Size)
		g.pf("%s    memcpy( dst + %d, &v, 4 );\n", ind, e.Dst)
		g.pf("%s}\n", ind)
	case ir.TableFixedOpText:
		unit := fixedScatterUnit(e.Meta)
		g.pf("%s{ // %s\n", ind, e.Note)
		g.pf("%s    int32_t v = (int32_t) TableFixedGet32( image + %d );\n", ind, e.Src)
		g.pf("%s    if ( v < 0 ) { v = 0; clamped++; } else if ( v > %d ) { v = %d; clamped++; }\n", ind, e.Size/unit, e.Size/unit)
		g.pf("%s    memcpy( dst + %d, &v, 4 );\n", ind, e.Dst)
		g.pf("%s    memcpy( dst + %d, image + %d, %d );\n", ind, e.Aux, e.Src+4, e.Size)
		if e.Meta != 3 {
			g.pf("%s    // the used length terminates the buffer, whose storage is one unit\n", ind)
			g.pf("%s    // longer than the bound for exactly this.\n", ind)
			g.pf("%s    dst[%d + (uint32_t) v * %d] = 0;\n", ind, e.Aux, unit)
			if unit == 2 {
				g.pf("%s    dst[%d + (uint32_t) v * %d + 1] = 0;\n", ind, e.Aux, unit)
			}
		}
		g.pf("%s}\n", ind)
	}
}
