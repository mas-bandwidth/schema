// THE MESSAGE FORM's READ (docs/SPEC-TABLES.md §3.3), and the entry points a
// BATCH is written and read through.
//
// A body carries no kind byte, so the two things a tolerant reader needs come
// from the announcement instead: the RECORD says how wide each payload is and
// how to step over one this build cannot name, and the KIND MISMATCH §4
// already counts is found by comparing that record against the reader's own
// declaration rather than by reading a byte. Everything else — the prefill of
// declared defaults, the overlay, the clamps and the counters — is the file
// form's, unchanged.
package cpptable

import (
	"fmt"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageLoadBody(st *ir.Struct) {
	g.pf("// The BITPACKED body's read (docs/SPEC-TABLES.md §3.3): the declared\n")
	g.pf("// defaults first, then whatever the wire says, field by field. An id this\n")
	g.pf("// build cannot name is skipped by its RECORD and counted; a record whose\n")
	g.pf("// kind is not this field's is a kind mismatch and skipped the same way.\n")
	g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, %s & value )\n{\n", st.Name, st.Name)
	g.pf("    %sReset( value );\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t ref = 0;\n")
	g.pf("        if ( !r.get( ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n")
	g.pf("        if ( ref == 0 ) { return true; } // the body ENDS AT ITS OWN ZERO REFERENCE\n")
	g.pf("        if ( ref > (uint64_t) vocabulary.table.count ) { report->malformed = true; return false; }\n")
	g.pf("        const uint64_t id = vocabulary.table.at( ref );\n")
	g.pf("        const TableMessageRecord rec = TableMessageRecordAt( vocabulary.records, ref );\n")
	g.pf("        // ONE ID, TWO KINDS in the sender's unit: the record spells neither, so\n")
	g.pf("        // the field can be neither read nor stepped over (§3.3)\n")
	g.pf("        if ( ( rec.flags & kTableMessageAmbiguous ) != 0 ) { report->malformed = true; return false; }\n")
	g.pf("        // A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS MALFORMED (§3.1, §3.3)\n")
	g.pf("        if ( id == kTableBuildVersionFieldId || id == kTableMessageRecordsFieldId ) { report->malformed = true; return false; }\n")
	g.pf("        switch ( id )\n        {\n")
	for _, f := range st.Fields {
		rec := g.messageRecord(f)
		g.pf("            case 0x%016xull: // %s\n            {\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                // THE KIND MISMATCH IS FOUND IN THE ANNOUNCEMENT, not on the body.\n")
		g.pf("                // A RANGE that moved is not one: the widths differ and the record\n")
		g.pf("                // carries the sender's, which is what makes a message from another\n")
		g.pf("                // build the ordinary case (§4, §3.3).\n")
		g.pf("                if ( rec.kind != %d || rec.elem_kind != %d )\n                {\n", rec.Kind, rec.ElemKind)
		g.pf("                    report->kind_mismatch++;\n")
		g.pf("                    if ( !TableMessageSkip( r, vocabulary, rec ) ) { report->malformed = true; return false; }\n")
		g.pf("                    break;\n                }\n")
		g.emitMessageReadField(f, rec)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n")
	g.pf("                report->unknown++;\n")
	g.pf("                if ( !TableMessageSkip( r, vocabulary, rec ) ) { report->malformed = true; return false; }\n")
	g.pf("                break;\n")
	g.pf("        }\n    }\n}\n\n")
}

func (g *tableGen) emitMessageReadField(f *ir.Field, rec ir.TableMessageDescriptor) {
	ind := "                "
	name := f.Name
	switch {
	case f.Type.Optional:
		g.emitMessageReadPayload(f, rec, "value."+name, ind)
		g.pf("%svalue.%s_present = true; // the field rode, so it is PRESENT (§2.3)\n", ind, name)

	case f.KeyEnum != "":
		g.emitMessageReadKeyed(f, rec, ind)

	case f.Type.Kind == ir.TString:
		g.emitMessageReadLength(rec, "n", ind)
		g.pf("%sint32_t kept = (int32_t) n;\n", ind)
		g.pf("%sif ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sfor ( uint64_t i = 0; i < n; i++ )\n%s{\n", ind, ind)
		g.pf("%s    uint64_t by = 0;\n%s    if ( !r.get( by, 8 ) ) { report->malformed = true; return false; }\n", ind, ind)
		g.pf("%s    if ( (int32_t) i < kept ) { value.%s[i] = (char) by; }\n%s}\n", ind, name, ind)
		g.pf("%svalue.%s[kept] = 0; value.%s_length = kept;\n", ind, name, name)

	case f.Type.Kind == ir.TBytes:
		g.emitMessageReadLength(rec, "n", ind)
		g.pf("%sint32_t kept = (int32_t) n;\n", ind)
		g.pf("%sif ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sfor ( uint64_t i = 0; i < n; i++ )\n%s{\n", ind, ind)
		g.pf("%s    uint64_t by = 0;\n%s    if ( !r.get( by, 8 ) ) { report->malformed = true; return false; }\n", ind, ind)
		g.pf("%s    if ( (int32_t) i < kept ) { value.%s[i] = (uint8_t) by; }\n%s}\n", ind, name, ind)
		g.pf("%svalue.%s_length = kept;\n", ind, name)

	case f.Array != ir.ArrayNone:
		g.emitMessageReadArray(f, rec, "value."+name, ind)

	case tableScalarKind(f) == tkUnion:
		g.emitMessageReadUnion(f, "value."+name, ind)

	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, value.%s ) ) { return false; }\n", ind, f.Type.Name, name)

	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, "value."+name, ind)

	default:
		g.emitMessageReadScalar(f, rec, "value."+name, ind)
	}
}

func (g *tableGen) emitMessageReadPayload(f *ir.Field, rec ir.TableMessageDescriptor, expr, ind string) {
	switch {
	case f.Array != ir.ArrayNone:
		g.emitMessageReadArray(f, rec, expr, ind)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, f.Type.Name, expr)
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, expr, ind)
	default:
		g.emitMessageReadScalar(f, rec, expr, ind)
	}
}

// emitMessageReadLength reads one length or count at the width the SENDER's
// record publishes.
func (g *tableGen) emitMessageReadLength(rec ir.TableMessageDescriptor, into, ind string) {
	g.pf("%suint64_t %s = 0;\n", ind, into)
	g.pf("%sif ( !TableMessageLength( r, rec, %s ) ) { report->malformed = true; return false; }\n", ind, into)
}

// emitMessageReadArray reads a positional array. A count above this reader's
// own bound keeps the first N and counts `clamped`: the elements past it are
// READ and dropped, because the stream advances past them either way.
func (g *tableGen) emitMessageReadArray(f *ir.Field, rec ir.TableMessageDescriptor, base, ind string) {
	g.emitMessageReadLength(rec, "n", ind)
	g.pf("%sint32_t kept = (int32_t) n;\n", ind)
	g.pf("%sif ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.ArrayBound, f.ArrayBound)
	g.pf("%sfor ( uint64_t i = 0; i < n; i++ )\n%s{\n", ind, ind)
	g.pf("%s    const bool in_bounds = (int32_t) i < kept;\n", ind)
	inner := ind + "    "
	elem := fmt.Sprintf("%s[ in_bounds ? i : 0 ]", base)
	switch int(rec.ElemKind) {
	case tkTable:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, in_bounds ? %s[i] : scratch ) ) { return false; }\n", inner, f.Type.Name, base)
	case tkEnum:
		g.pf("%s%s slot_value = %s::None;\n", inner, f.Type.Name, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value", inner)
		g.pf("%sif ( in_bounds ) { %s[i] = slot_value; }\n", inner, base)
	case tkUnion:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.emitMessageReadUnion(f, fmt.Sprintf("( in_bounds ? %s[i] : scratch )", base), inner)
	default:
		g.emitMessageReadScalar(f, rec, elem, inner)
	}
	g.pf("%s}\n", ind)
	if f.CountedOnWire() {
		g.pf("%svalue.%s_count = kept;\n", ind, f.Name)
	}
	if f.Type.Optional {
		g.pf("%svalue.%s_present = true;\n", ind, f.Name)
	}
}

// emitMessageReadEnum reads an enum value: the reference to its VARIANT NAME's
// id, `0` for None. A reference this reader's enum cannot name is §4's
// ordinary `unknown` — the field reads None and one event counts.
func (g *tableGen) emitMessageReadEnum(f *ir.Field, dst, ind string) {
	e := f.Type.Name
	g.pf("%s{\n%s    uint64_t variant_ref = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( variant_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    if ( variant_ref == 0 ) { %s = %s::None; } // the zero reference is the enum's None\n", ind, dst, e)
	g.pf("%s    else if ( variant_ref > (uint64_t) vocabulary.table.count ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    else if ( !TableEnumValue( vocabulary.table.at( variant_ref ), %s ) ) { %s = %s::None; report->unknown++; }\n", ind, dst, dst, e)
	g.pf("%s}\n", ind)
}

// emitMessageReadUnion reads a union: the ARM's reference, then the payload a
// FIELD of the arm's type carries. An arm this reader cannot name is §4's
// ordinary `unknown`, and its payload is stepped over by the ARM's own record.
func (g *tableGen) emitMessageReadUnion(f *ir.Field, dst, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{\n%s    uint64_t arm_ref = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( arm_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    if ( arm_ref == 0 ) { %s.type = %sType::None; }\n", ind, dst, un.Name)
	g.pf("%s    else if ( arm_ref > (uint64_t) vocabulary.table.count ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    else\n%s    {\n", ind, ind)
	g.pf("%s        const uint64_t arm_id = vocabulary.table.at( arm_ref );\n", ind)
	g.pf("%s        const TableMessageRecord arm = TableMessageRecordAt( vocabulary.records, arm_ref );\n", ind)
	g.pf("%s        switch ( arm_id )\n%s        {\n", ind, ind)
	for _, v := range un.Variants {
		rec := g.messageArmRecord(v)
		g.noteRef(v.Type)
		g.pf("%s            case 0x%016xull: // %s\n%s            {\n", ind, ir.TableWireId(v.Name), v.Name, ind)
		g.pf("%s                if ( arm.kind != %d || arm.elem_kind != %d )\n%s                {\n", ind, rec.Kind, rec.ElemKind, ind)
		g.pf("%s                    %s.type = %sType::None; report->kind_mismatch++;\n", ind, dst, un.Name)
		g.pf("%s                    if ( !TableMessageSkip( r, vocabulary, arm ) ) { report->malformed = true; return false; }\n", ind)
		g.pf("%s                    break;\n%s                }\n", ind, ind)
		g.pf("%s                %s.type = %sType::%s;\n", ind, dst, un.Name, ir.GoExportName(v.Name))
		switch {
		case v.Void():
		case v.Body():
			g.pf("%s                if ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, v.Type, armValue(dst, v))
		default:
			g.emitMessageReadArm(v, rec, dst, ind+"                ")
		}
		g.pf("%s                break;\n%s            }\n", ind, ind)
	}
	g.pf("%s            default:\n", ind)
	g.pf("%s                %s.type = %sType::None; report->unknown++;\n", ind, dst, un.Name)
	g.pf("%s                if ( !TableMessageSkip( r, vocabulary, arm ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s                break;\n%s        }\n%s    }\n%s}\n", ind, ind, ind, ind)
}

func (g *tableGen) emitMessageReadArm(v ir.UnionVariant, rec ir.TableMessageDescriptor, base, ind string) {
	af := v.F
	value, count := armValue(base, v), armCount(base, v)
	switch {
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		g.pf("%s{\n", ind)
		g.emitMessageReadLength(rec, "n", ind+"    ")
		g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
		g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, af.Type.Size, af.Type.Size)
		g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
		g.pf("%s        uint64_t by = 0;\n%s        if ( !r.get( by, 8 ) ) { report->malformed = true; return false; }\n", ind, ind)
		if af.Type.Kind == ir.TString {
			g.pf("%s        if ( (int32_t) i < kept ) { %s[i] = (char) by; }\n%s    }\n", ind, value, ind)
			g.pf("%s    %s[kept] = 0;\n", ind, value)
		} else {
			g.pf("%s        if ( (int32_t) i < kept ) { %s[i] = (uint8_t) by; }\n%s    }\n", ind, value, ind)
		}
		g.pf("%s    %s = kept;\n%s}\n", ind, count, ind)
	case af.Array != ir.ArrayNone:
		g.pf("%s{\n", ind)
		g.emitMessageReadLength(rec, "n", ind+"    ")
		g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
		g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, af.ArrayBound, af.ArrayBound)
		g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
		g.pf("%s        const bool in_bounds = (int32_t) i < kept;\n", ind)
		g.emitMessageReadScalar(af, rec, fmt.Sprintf("%s[ in_bounds ? i : 0 ]", value), ind+"        ")
		g.pf("%s    }\n", ind)
		if af.Array == ir.ArrayCounted {
			g.pf("%s    %s = kept;\n", ind, count)
		}
		g.pf("%s}\n", ind)
	case tableScalarKind(af) == tkEnum:
		g.emitMessageReadEnum(af, value, ind)
	case tableScalarKind(af) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, af.Type.Name, value)
	default:
		g.emitMessageReadScalar(af, rec, value, ind)
	}
}

// emitMessageReadKeyed reads an enum-keyed array (§3.2): the number of present
// slots, then one `(key reference, element)` pair per slot. A key this reader's
// enum cannot name drops its element and counts one `unknown`, and the element
// is read either way because the stream advances past it.
func (g *tableGen) emitMessageReadKeyed(f *ir.Field, rec ir.TableMessageDescriptor, ind string) {
	kind := tableScalarKind(f)
	slots := g.keyedSlots("value.", f)
	g.emitMessageReadLength(rec, "n", ind)
	g.pf("%sfor ( uint64_t p = 0; p < n; p++ )\n%s{\n", ind, ind)
	inner := ind + "    "
	g.pf("%suint64_t key_ref = 0;\n", inner)
	g.pf("%sif ( !r.get( key_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", inner)
	g.pf("%sif ( key_ref == 0 || key_ref > (uint64_t) vocabulary.table.count ) { report->malformed = true; return false; }\n", inner)
	g.pf("%s%s key = %s::None;\n", inner, f.KeyEnum, f.KeyEnum)
	g.pf("%sconst bool named = TableEnumValue( vocabulary.table.at( key_ref ), key );\n", inner)
	g.pf("%sif ( !named ) { report->unknown++; }\n", inner)
	g.pf("%sconst int32_t slot = named ? (int32_t) key - 1 : -1;\n", inner)
	g.pf("%sconst bool in_bounds = slot >= 0 && slot < %d;\n", inner, f.ArrayBound)
	switch kind {
	case tkTable:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, in_bounds ? %s[slot] : scratch ) ) { return false; }\n", inner, f.Type.Name, slots)
	case tkEnum:
		g.pf("%s%s slot_value = %s::None;\n", inner, f.Type.Name, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value", inner)
		g.pf("%sif ( in_bounds ) { %s[slot] = slot_value; }\n", inner, slots)
	default:
		g.emitMessageReadScalar(f, rec, fmt.Sprintf("%s[ in_bounds ? slot : 0 ]", slots), inner)
	}
	g.pf("%s}\n", ind)
}

// emitMessageReadScalar reads ONE value at the width the SENDER's record
// publishes and against its range base, and then applies THIS reader's own
// declared bounds: a value outside them clamps and counts, exactly as it does
// on a file (§4). The sender's width is a run-time fact, because a message
// from another build is the ordinary case this wire exists for.
func (g *tableGen) emitMessageReadScalar(f *ir.Field, rec ir.TableMessageDescriptor, lvalue, ind string) {
	kind := tableScalarKind(f)
	switch kind {
	case tkBool:
		g.pf("%s{ uint64_t raw = 0; if ( !r.get( raw, rec.value_bits ) ) { report->malformed = true; return false; } %s = raw != 0; }\n", ind, lvalue)
		return
	case tkF32:
		g.pf("%s{\n%s    uint64_t raw = 0;\n", ind, ind)
		g.pf("%s    if ( !r.get( raw, 32 ) ) { report->malformed = true; return false; }\n", ind)
		g.pf("%s    float decoded_f = table_bits_to_float( (uint32_t) raw );\n", ind)
		if f.HasFloatRange {
			g.pf("%s    if ( decoded_f < %s ) { decoded_f = %s; report->clamped++; }\n", ind, formatFloat(f.FMin, true), formatFloat(f.FMin, true))
			g.pf("%s    else if ( decoded_f > %s ) { decoded_f = %s; report->clamped++; }\n", ind, formatFloat(f.FMax, true), formatFloat(f.FMax, true))
		}
		g.pf("%s    %s = decoded_f;\n%s}\n", ind, lvalue, ind)
		return
	case tkF64:
		g.pf("%s{ uint64_t raw = 0; if ( !r.get( raw, 64 ) ) { report->malformed = true; return false; } %s = table_bits_to_double( raw ); }\n", ind, lvalue)
		return
	}
	signed := ir.TableKindSigned(kind)
	width := tableKindWidth(kind)
	g.pf("%s{\n", ind)
	if width == 16 {
		g.pf("%s    uint64_t lo_v = 0, hi_v = 0;\n", ind)
		g.pf("%s    if ( !r.get( lo_v, 64 ) || !r.get( hi_v, 64 ) ) { report->malformed = true; return false; }\n", ind)
		storage := "serialize::uint128_t"
		if signed {
			storage = "serialize::int128_t"
		}
		g.pf("%s    %s decoded_v = %s( ( serialize::uint128_t( hi_v ) << 64 ) | serialize::uint128_t( lo_v ) );\n", ind, storage, storage)
		g.emitMessageClamp(f, signed, width, ind+"    ")
		g.pf("%s    %s = decoded_v;\n%s}\n", ind, lvalue, ind)
		return
	}
	storage := fmt.Sprintf("uint%d_t", width*8)
	if signed {
		storage = fmt.Sprintf("int%d_t", width*8)
	}
	g.pf("%s    uint64_t raw = 0;\n", ind)
	g.pf("%s    if ( !r.get( raw, rec.value_bits ) ) { report->malformed = true; return false; }\n", ind)
	if signed {
		g.pf("%s    %s decoded_v = (%s) TableMessageSigned( raw, rec );\n", ind, storage, storage)
	} else {
		g.pf("%s    %s decoded_v = (%s) ( raw + (uint64_t) rec.min );\n", ind, storage, storage)
	}
	g.emitMessageClamp(f, signed, width, ind+"    ")
	g.pf("%s    %s = decoded_v;\n%s}\n", ind, lvalue, ind)
}

// emitMessageClamp is §4's clamp, the file form's own text: the declared range
// on the RAW scale, and a `bits(N)` width clamp beside it.
func (g *tableGen) emitMessageClamp(f *ir.Field, signed bool, width int, ind string) {
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		low, high := tableClampEnds(f, width)
		if low {
			lo := tableIntLit(rlo, signed, width)
			g.pf("%sif ( decoded_v < %s ) { decoded_v = %s; report->clamped++; }\n", ind, lo, lo)
		}
		if high {
			hi := tableIntLit(rhi, signed, width)
			lead := "if"
			if low {
				lead = "else if"
			}
			g.pf("%s%s ( decoded_v > %s ) { decoded_v = %s; report->clamped++; }\n", ind, lead, hi, hi)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		maxv := (uint64(1) << f.Type.Width) - 1
		g.pf("%sif ( decoded_v > %dull ) { decoded_v = %dull; report->clamped++; } // bits(%d) width clamp\n", ind, maxv, maxv, f.Type.Width)
	}
}

// ---- the batch's entry points ----

// emitMessageEntries emits one ROOT's message surface (§3.3): the batch's three
// verbs, and the batch-of-one spellings beside them.
func (g *tableGen) emitMessageEntries(st *ir.Struct) {
	if st.IsMapEntry() || !g.messageCarried(st) {
		// a root whose closure this codec does not carry gets no entry points
		// at all, rather than half of one
		return
	}
	n := st.Name
	g.pf("// THE PRIMITIVE IS A BATCH (docs/SPEC-TABLES.md §3.3): a number of messages\n")
	g.pf("// in one buffer, one count and one continuous bit stream with no alignment\n")
	g.pf("// between the bodies. A single message is the batch of one.\n")
	g.pf("//\n")
	g.pf("// Every reference is a compile-time SLOT and every width a literal, so a save\n")
	g.pf("// does no lookup at all and costs what a save costs.\n")
	g.pf("inline int64_t %sMeasureMessages( const %s * values, int64_t count )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 0 ) { return -1; }\n")
	g.pf("    int64_t bits = 0;\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        const int64_t body = %sMeasureMessageBody( values[i] );\n", n)
	g.pf("        if ( body < 0 ) { return -1; }\n")
	g.pf("        bits += body;\n    }\n")
	g.pf("    return bits; // BITS, which TableMessageBatchBytes turns into a buffer size\n}\n\n")

	g.pf("inline bool %sSaveMessages( TableMessageBatch & batch, const %s * values, int64_t count )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 0 ) { return false; }\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( !%sSaveMessageBody( batch.w, values[i] ) ) { return false; }\n", n)
	g.pf("        batch.written++;\n    }\n")
	g.pf("    return !batch.w.overflow;\n}\n\n")

	g.pf("inline bool %sLoadMessages( TableMessageBatchReader & br, %s * values, int64_t count )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 0 || count > br.remaining ) { return false; }\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( !%sLoadMessageBody( br.r, *br.vocabulary, br.report, values[i] ) ) { return false; }\n", n)
	g.pf("        br.remaining--;\n    }\n")
	g.pf("    return true;\n}\n\n")

	g.pf("// The BATCH OF ONE, which is the only sense in which this wire carries a\n")
	g.pf("// single message.\n")
	g.pf("inline int64_t %sMeasureMessage( const %s & value )\n{\n", n, n)
	g.pf("    return TableMessageBatchBytes( 1, %sMeasureMessages( &value, 1 ) );\n}\n\n", n)
	g.pf("inline int64_t %sSaveMessage( const %s & value, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    TableMessageBatch batch;\n")
	g.pf("    if ( !TableMessageBatchBegin( batch, buffer, capacity, 1 ) ) { return -1; }\n")
	g.pf("    if ( !%sSaveMessages( batch, &value, 1 ) ) { return -1; }\n", n)
	g.pf("    return TableMessageBatchEnd( batch ); // == %sMeasureMessage( value )\n}\n\n", n)
	g.pf("// A form 2 wire with NO TABLE for the announcement is REFUSED BY NAME:\n")
	g.pf("// nothing is decoded, the reader says it holds no table, no counter moves\n")
	g.pf("// and malformed does not fire. A reader does not fall back to the file form\n")
	g.pf("// on its own and does not guess a table, because a guessed table decodes a\n")
	g.pf("// body under the wrong names in silence.\n")
	g.pf("inline bool %sLoadMessage( %s & value, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * to = report != NULL ? report : &ignored;\n")
	g.pf("    %sReset( value );\n", n)
	g.pf("    TableMessageBatchReader br;\n")
	g.pf("    if ( TableMessageBatchOpen( br, vocabulary, buffer, bytes, to ) != 1 ) { return false; }\n")
	g.pf("    return %sLoadMessages( br, &value, 1 );\n}\n\n", n)
}

// messageEntryRoots is the roots this codec emits entry points for, named so a
// reader of the header can see at a glance which of the unit's roots carry the
// message form today.
func messageEntryRootNames(roots []*ir.Struct) string {
	names := make([]string, 0, len(roots))
	for _, st := range roots {
		names = append(names, strconv.Quote(st.Name))
	}
	return fmt.Sprint(names)
}
