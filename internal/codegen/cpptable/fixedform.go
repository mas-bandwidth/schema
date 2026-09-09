// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the reference
// writer, the reference reader and the layout, for C++.
//
// A fixed-table record is an eight-byte hash of the writer's layout
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
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SIZES, THE WALK, THE LAYOUT AND THE HASH ARE NOT HERE. They produce the
// BYTES a fixed record and its layout are made of, and a second copy
// of them in a second backend is a second answer waiting to happen, so they
// live in ir (ir/fixedform.go) and every port renders the same walk. What is
// left in this file is C++: how an offset is spelled, what an element's
// storage type is called, and how a plan is laid down in the source.

// offOf is C++'s offsetof, in the spelling that is a constant expression in
// every compiler this repo builds under.
func offOf(owner, member string) string {
	return fmt.Sprintf("(uint32_t) __builtin_offsetof( %s, %s )", owner, member)
}

// dstTerms renders a destination — a sum of offsetofs, and nothing at all is
// the record's own base.
func dstTerms(terms []ir.FixedTerm) string {
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
func (g *tableGen) dstRow(e ir.FixedLayoutEntry) string {
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
	return fmt.Sprintf("{ %s, %s, %s, %d, %d }", dstTerms(d.Dst), stride, aux, d.Counted, d.Arg)
}

// ---------------------------------------------------------------------------
// THE EMISSION
// ---------------------------------------------------------------------------

// fixedRoots is every table of this file the fixed form is emitted for.
func (g *tableGen) fixedRoots(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if !st.IsTable || st.IsMapEntry() || g.isVar(st.Name) || !ir.FixedSupported(st, 0) {
			continue
		}
		// PAST THE WIRE'S CEILING THE FORM IS NOT EMITTED AT ALL, and the
		// compiler said so by name (ir.FixedRecordBounds). No layout, no
		// writer, no reader: a record that size is one no conforming reader
		// decodes, so a writer for it would produce bytes nobody can read.
		if !ir.FixedFormCarried(st) {
			continue
		}
		// A PLAN IS BUILT AT COMPILE TIME, in an array the compiler sizes, so a
		// type whose leaves do not fit one does not carry this form. It is a
		// bound on the REFERENCE and not on the wire: nothing about §3.4 stops
		// such a type, and the follow-on is a plan built at load time instead.
		if ir.FixedLeafCount(g.unit, st) > ir.FixedLeafCap {
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
	g.pf("// A record is an eight-byte hash of the writer's layout and then\n")
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
		off += ir.FixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, indent int) { //nolint:gocyclo
	ind := strings.Repeat(" ", indent)
	base := off
	if f.Type.Optional {
		g.pf("%sTableFixedPut8( %s + %d, %s.%s_present ? 1 : 0 );\n", ind, buf, base, val, f.Name)
		base += ir.FixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteLoop(f, base, f.KeyEnumRef.Max, buf, val+"."+ir.FixedKeyedSlots(g.fixedOwner, f), indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, base, f.ArrayBound, buf, val+"."+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_count );\n", ind, buf, base, val, f.Name)
		g.emitFixedWriteLoop(f, base+ir.FixedCountBytes, f.ArrayBound, buf, val+"."+f.Name, indent)
	case f.Type.Kind == ir.TString:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, f.Type.Size)
	case f.Type.Kind == ir.TWString:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, 2*f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, f.Type.Size)
	default:
		g.emitFixedWriteElement(f, base, buf, val+"."+f.Name, indent)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base, count int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := ir.FixedElementBytes(f)
	g.pf("%sfor ( int64_t i = 0; i < %d; ++i )\n%s{\n", ind, count, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s + %d + i * %d", buf, base, elem), expr+"[i]", indent+4)
	g.pf("%s}\n", ind)
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
	w := ir.FixedStorageBytes(f.Type)
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
	entries := ir.FixedWalkRoot(st)
	layout := ir.FixedLayoutBytes(entries)
	hash := ir.FixedLayoutHash(layout)
	body := ir.FixedTypeBytes(st)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("// MeasureBody IS A CONSTEXPR on this form: the body is the same size for\n")
	g.pf("// every value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("constexpr int64_t %sFixedBodyBytes = %d;\n", st.Name, body)
	g.pf("constexpr int64_t %sFixedRecordBytes = 8 + %sFixedBodyBytes; // the hash and the body\n", st.Name, st.Name)
	g.pf("constexpr uint64_t %sFixedHash = 0x%016xull; // fnv1a64 over the layout's bytes\n\n", st.Name, hash)

	g.pf("// THE LAYOUT (form 1 called this the vocabulary block): %d entries, a\n", len(entries))
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

	plan, guarded := ir.FixedBuildPlan(g.unit, st)
	g.pf("// THE IDENTITY PLAN, coalesced out of %d leaves by the schema compiler —\n", ir.FixedLeafCount(g.unit, st))
	g.pf("// the one walk every backend lays down (ir/fixedform.go), so no two ports\n")
	g.pf("// can disagree about what the coalescer did; the asserts above tie every\n")
	g.pf("// destination in it to this compiler's own ABI. UNGUARDED ENTRIES FIRST,\n")
	g.pf("// then the arms: the entries that are nearly all of a plan never test a\n")
	g.pf("// guard at all.\n")
	g.pf("constexpr TableFixedEntry %sFixedPlan[] = {\n", st.Name)
	for _, e := range plan {
		guard := "kTableFixedNoGuard"
		if e.Guard != ir.FixedNoGuard {
			guard = fmt.Sprintf("%du", e.Guard)
		}
		g.pf("    { %du, %du, %du, %du, %s, %s, %d, 0, 0 }, // %s\n",
			e.Src, e.Dst, e.Size, e.Aux, guard, fixedOpName(e.Op), e.Arg, e.Note)
	}
	g.pf("};\n")
	g.pf("constexpr int32_t %sFixedPlanCount = %d;\n", st.Name, len(plan))
	g.pf("constexpr int32_t %sFixedPlanGuarded = %d;\n\n", st.Name, guarded)

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

	g.pf("// THE READ: a prefill and ONE loop over ONE plan — the identity plan when\n")
	g.pf("// the layout's hash is this build's own, and a plan compiled once from the\n")
	g.pf("// writer's layout otherwise. Same loop either way (§3.4).\n")
	g.pf("inline int64_t %sFixedLoad( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n", st.Name, st.Name)
	g.pf("                            TableFixedEntry * plan, int32_t plan_capacity, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n    if ( report == NULL ) { report = &local; }\n")
	g.pf("    if ( data == NULL || bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = true; return -1; }\n")
	g.pf("    // THE FORM BYTE IS READ FIRST, AND IT SAYS WHICH DIRECTION (§3, §3.4):\n")
	g.pf("    // the registry is ordered, so a byte this reader does not carry is named\n")
	g.pf("    // by where it sits relative to this form and never by one word for both.\n")
	g.pf("    // Each moves no counter and reports no damage.\n")
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
	g.pf("    if ( hash != %sFixedHash )\n    {\n", st.Name)
	g.pf("        // ANOTHER WRITER: the same loop, over a plan compiled from its layout.\n")
	g.pf("        // THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS TOUCHED, and\n")
	g.pf("        // every rule it fails refuses under ITS OWN NAME (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("        TableFixedLayout parsed;\n")
	g.pf("        TableMessageReason why = layout_malformed;\n")
	g.pf("        if ( !TableFixedParseLayout( layout, layout_bytes, parsed, why ) ) { report->refused = true; report->reason = why; return -1; }\n")
	g.pf("        int32_t compiled_guarded = 0;\n")
	g.pf("        const int32_t made = TableFixedCompile( parsed, %sFixedLayout, (int32_t) %sFixedLayoutBytes, %sFixedDst, plan, plan_capacity, &compiled_guarded, report );\n", st.Name, st.Name, st.Name)
	g.pf("        // THE COMPILER'S OWN TWO ANSWERS, and each keeps the name its cause\n")
	g.pf("        // earned: -2 is an entry the walk placed past the writer's declared\n")
	g.pf("        // record, which is a layout whose sizes do not account for their\n")
	g.pf("        // children by an arithmetic the tree walk above cannot reach, and -1\n")
	g.pf("        // is a plan larger than the storage the caller declared.\n")
	g.pf("        if ( made < 0 ) { report->refused = true; report->reason = ( made == -2 ) ? layout_size_mismatch : plan_too_large; return -1; }\n")
	g.pf("        entries = plan;\n        entry_count = made;\n        entry_guarded = compiled_guarded;\n")
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
	g.pf("    for ( int64_t k = 0; k < n; ++k )\n    {\n")
	g.pf("        %sReset( values[k] ); // the declared defaults, one prefill\n", st.Name)
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
		for _, m := range ir.FixedMembers(g.unit, st) {
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
			return ir.FixedKeyedSlots(st, f)
		}
	}
	return member
}

func fixedOpName(op int) string {
	switch op {
	case ir.FixedOpCount:
		return "kTableFixedCount"
	case ir.FixedOpText:
		return "kTableFixedText"
	}
	return "kTableFixedCopy"
}
