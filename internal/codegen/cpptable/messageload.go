// THE MESSAGE FORM's READ (docs/SPEC-TABLES.md §3.3), and the BATCH's three
// verbs.
//
// A body carries no kind byte, so the two things a tolerant reader needs come
// from the announcement instead: the ENTRY's shape says how wide each payload
// is and how to step over one this build cannot name, and the KIND MISMATCH §4
// already counts is found by comparing that kind against the reader's own
// declaration rather than by reading a byte. Everything else — the prefill of
// declared defaults, the overlay, the clamps and the counters — is the file
// form's, unchanged.
//
// DAMAGE IS TERMINAL FOR THE BATCH: a bit stream has no place to resume, so a
// reader that has lost its position has lost it for the rest of the buffer.
package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageLoadBody(st *ir.Struct) {
	g.pf("// The BITPACKED body's read (docs/SPEC-TABLES.md §3.3): the declared\n")
	g.pf("// defaults first, then whatever the wire says, field by field. An entry this\n")
	g.pf("// build cannot name is skipped by its SHAPE and counted; one whose kind is\n")
	g.pf("// not this field's is a kind mismatch and skipped the same way.\n")
	g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, %s & value )\n{\n", st.Name, st.Name)
	g.pf("    %sReset( value );\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t ref = 0;\n")
	g.pf("        if ( !r.get( ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n")
	g.pf("        if ( ref == 0 ) { return true; } // the body ENDS AT ITS OWN ZERO REFERENCE\n")
	g.pf("        if ( ref > (uint64_t) vocabulary.count ) { report->malformed = true; return false; }\n")
	g.pf("        const TableMessageEntry entry = TableVocabularyEntryAt( vocabulary, ref );\n")
	g.pf("        // A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS\n")
	g.pf("        // MALFORMED (§3.1, §3.3)\n")
	g.pf("        if ( entry.id == kTableBuildVersionFieldId || entry.id == kTableMessageVocabularyFieldId || entry.id == kTableNodeWireId ) { report->malformed = true; return false; }\n")
	g.pf("        switch ( entry.id )\n        {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("            case 0x%016xull: // %s\n            {\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                // THE KIND MISMATCH IS FOUND IN THE ANNOUNCEMENT, not on the body.\n")
		g.pf("                // A RANGE that moved is not one: the shapes differ and the entry\n")
		g.pf("                // carries the SENDER's, so the field decodes and clamps (§4).\n")
		g.pf("                if ( entry.kind != %d || entry.elem_kind != %d )\n                {\n", mine.Kind, mine.Shape.Elem)
		g.pf("                    report->kind_mismatch++;\n")
		g.pf("                    if ( !TableMessageSkip( r, vocabulary, entry ) ) { report->malformed = true; return false; }\n")
		g.pf("                    break;\n                }\n")
		g.emitMessageReadField(f)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n")
	g.pf("                report->unknown++;\n")
	g.pf("                if ( !TableMessageSkip( r, vocabulary, entry ) ) { report->malformed = true; return false; }\n")
	g.pf("                break;\n")
	g.pf("        }\n    }\n}\n\n")
}

func (g *tableGen) emitMessageReadField(f *ir.Field) {
	ind := "                "
	name := f.Name
	switch {
	case f.Type.Optional:
		g.emitMessageReadPayload(f, "value."+name, ind)
		g.pf("%svalue.%s_present = true; // the field rode, so it is PRESENT (§2.3)\n", ind, name)
	case f.KeyEnum != "":
		g.emitMessageReadKeyed(f, ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.emitMessageReadText(f, "value."+name, fmt.Sprintf("value.%s_length", name), ind)
	case f.Array != ir.ArrayNone:
		g.emitMessageReadArray(f, "value."+name, ind)
	case tableScalarKind(f) == tkUnion:
		g.emitMessageReadUnion(f, "value."+name, ind)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, value.%s ) ) { return false; }\n", ind, f.Type.Name, name)
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, "value."+name, ind)
	default:
		g.emitMessageReadScalar(f, "value."+name, ind, "entry")
	}
}

func (g *tableGen) emitMessageReadPayload(f *ir.Field, expr, ind string) {
	switch {
	case f.Array != ir.ArrayNone:
		g.emitMessageReadArray(f, expr, ind)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, f.Type.Name, expr)
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, expr, ind)
	default:
		g.emitMessageReadScalar(f, expr, ind, "entry")
	}
}

// emitMessageReadText reads a `string(N)` or a `bytes(N)`: the length at the
// SENDER's own width, the ALIGN, then the bytes. A payload longer than this
// reader's bound keeps what fits and counts `clamped`, which is not damage.
func (g *tableGen) emitMessageReadText(f *ir.Field, value, count, ind string) {
	width := "TableBitsRequired( 0, entry.max )"
	if f.Type.Kind == ir.TBytes {
		width = "TableBitsRequired( entry.min, entry.max )"
	}
	g.pf("%s{\n%s    uint64_t n = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( n, %s ) || !r.align() ) { report->malformed = true; return false; }\n", ind, width)
	g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
	g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
	g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t by = 0;\n%s        if ( !r.get( by, 8 ) ) { report->malformed = true; return false; }\n", ind, ind)
	cast := "(uint8_t)"
	if f.Type.Kind == ir.TString {
		cast = "(char)"
	}
	g.pf("%s        if ( (int32_t) i < kept ) { %s[i] = %s by; }\n%s    }\n", ind, value, cast, ind)
	if f.Type.Kind == ir.TString {
		g.pf("%s    %s[kept] = 0;\n", ind, value)
	}
	g.pf("%s    %s = kept;\n%s}\n", ind, count, ind)
}

// emitMessageReadArray reads a positional array. NO COUNT RIDES where the
// sender's `min` equals its `max`. A count above this reader's own bound keeps
// the first N and counts `clamped`: the elements past it are READ and dropped,
// because the stream advances past them either way.
func (g *tableGen) emitMessageReadArray(f *ir.Field, base, ind string) {
	g.pf("%s{\n%s    uint64_t n = (uint64_t) entry.min;\n", ind, ind)
	g.pf("%s    const int64_t count_bits = TableBitsRequired( entry.min, entry.max );\n", ind)
	g.pf("%s    if ( count_bits > 0 )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t raw = 0;\n%s        if ( !r.get( raw, count_bits ) ) { report->malformed = true; return false; }\n", ind, ind)
	g.pf("%s        n = raw + (uint64_t) entry.min;\n%s    }\n", ind, ind)
	g.pf("%s    if ( entry.elem_kind == 6 && !r.align() ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
	g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.ArrayBound, f.ArrayBound)
	g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
	inner := ind + "        "
	g.pf("%sconst bool in_bounds = (int32_t) i < kept;\n", inner)
	switch tableScalarKind(f) {
	case tkTable:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.pf("%s%sReset( scratch );\n", inner, f.Type.Name)
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, in_bounds ? %s[i] : scratch ) ) { return false; }\n", inner, f.Type.Name, base)
	case tkEnum:
		g.pf("%s%s slot_value = %s::None;\n", inner, f.Type.Name, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value", inner)
		g.pf("%sif ( in_bounds ) { %s[i] = slot_value; }\n", inner, base)
	case tkUnion:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.emitMessageReadUnion(f, fmt.Sprintf("( in_bounds ? %s[i] : scratch )", base), inner)
	default:
		g.emitMessageReadScalarElement(f, fmt.Sprintf("%s[ in_bounds ? i : 0 ]", base), inner)
	}
	g.pf("%s    }\n", ind)
	if f.CountedOnWire() {
		g.pf("%s    value.%s_count = kept;\n", ind, f.Name)
	}
	if f.Type.Optional {
		g.pf("%s    value.%s_present = true;\n", ind, f.Name)
	}
	g.pf("%s}\n", ind)
}

// emitMessageReadEnum reads an enum value: the reference naming its VARIANT's
// name, `0` for None. A reference this reader's enum cannot name is §4's
// ordinary `unknown` — the field reads None and one event counts.
func (g *tableGen) emitMessageReadEnum(f *ir.Field, dst, ind string) {
	e := f.Type.Name
	g.pf("%s{\n%s    uint64_t variant_ref = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( variant_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    if ( variant_ref == 0 ) { %s = %s::None; } // the zero reference is the enum's None\n", ind, dst, e)
	g.pf("%s    else if ( variant_ref > (uint64_t) vocabulary.count ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    else if ( !TableEnumValue( TableVocabularyEntryAt( vocabulary, variant_ref ).id, %s ) ) { %s = %s::None; report->unknown++; }\n", ind, dst, dst, e)
	g.pf("%s}\n", ind)
}

// emitMessageReadUnion reads a union: the ARM's reference, then the payload a
// FIELD of the arm's type carries. An arm this reader cannot name is §4's
// ordinary `unknown`, and its payload is stepped over by the ARM's own entry.
func (g *tableGen) emitMessageReadUnion(f *ir.Field, dst, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{\n%s    uint64_t arm_ref = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( arm_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    if ( arm_ref == 0 ) { %s.type = %sType::None; }\n", ind, dst, un.Name)
	g.pf("%s    else if ( arm_ref > (uint64_t) vocabulary.count ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    else\n%s    {\n", ind, ind)
	g.pf("%s        const TableMessageEntry arm = TableVocabularyEntryAt( vocabulary, arm_ref );\n", ind)
	g.pf("%s        switch ( arm.id )\n%s        {\n", ind, ind)
	for _, v := range un.Variants {
		mine := ir.TableArmEntry(v)
		g.noteRef(v.Type)
		g.pf("%s            case 0x%016xull: // %s\n%s            {\n", ind, ir.TableWireId(v.Name), v.Name, ind)
		g.pf("%s                if ( arm.kind != %d || arm.elem_kind != %d )\n%s                {\n", ind, mine.Kind, mine.Shape.Elem, ind)
		g.pf("%s                    %s.type = %sType::None; report->kind_mismatch++;\n", ind, dst, un.Name)
		g.pf("%s                    if ( !TableMessageSkip( r, vocabulary, arm ) ) { report->malformed = true; return false; }\n", ind)
		g.pf("%s                    break;\n%s                }\n", ind, ind)
		g.pf("%s                %s.type = %sType::%s;\n", ind, dst, un.Name, ir.GoExportName(v.Name))
		switch {
		case v.Void():
		case v.Body():
			g.pf("%s                if ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, v.Type, armValue(dst, v))
		default:
			g.emitMessageReadArm(v, dst, ind+"                ")
		}
		g.pf("%s                break;\n%s            }\n", ind, ind)
	}
	g.pf("%s            default:\n", ind)
	g.pf("%s                %s.type = %sType::None; report->unknown++;\n", ind, dst, un.Name)
	g.pf("%s                if ( !TableMessageSkip( r, vocabulary, arm ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s                break;\n%s        }\n%s    }\n%s}\n", ind, ind, ind, ind)
}

func (g *tableGen) emitMessageReadArm(v ir.UnionVariant, base, ind string) {
	af := v.F
	value, count := armValue(base, v), armCount(base, v)
	switch {
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		g.emitMessageReadTextFrom(af, value, count, ind, "arm")
	case af.Array != ir.ArrayNone:
		g.emitMessageReadArrayFrom(af, value, count, ind, "arm")
	case tableScalarKind(af) == tkEnum:
		g.emitMessageReadEnum(af, value, ind)
	case tableScalarKind(af) == tkTable:
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, %s ) ) { return false; }\n", ind, af.Type.Name, value)
	default:
		g.emitMessageReadScalar(af, value, ind, "arm")
	}
}

// emitMessageReadTextFrom is emitMessageReadText against a named entry, which
// an ARM's payload needs because its shape is the arm's rather than the
// field's.
func (g *tableGen) emitMessageReadTextFrom(f *ir.Field, value, count, ind, from string) {
	width := fmt.Sprintf("TableBitsRequired( 0, %s.max )", from)
	if f.Type.Kind == ir.TBytes {
		width = fmt.Sprintf("TableBitsRequired( %s.min, %s.max )", from, from)
	}
	g.pf("%s{\n%s    uint64_t n = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( n, %s ) || !r.align() ) { report->malformed = true; return false; }\n", ind, width)
	g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
	g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
	g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t by = 0;\n%s        if ( !r.get( by, 8 ) ) { report->malformed = true; return false; }\n", ind, ind)
	cast := "(uint8_t)"
	if f.Type.Kind == ir.TString {
		cast = "(char)"
	}
	g.pf("%s        if ( (int32_t) i < kept ) { %s[i] = %s by; }\n%s    }\n", ind, value, cast, ind)
	if f.Type.Kind == ir.TString {
		g.pf("%s    %s[kept] = 0;\n", ind, value)
	}
	g.pf("%s    %s = kept;\n%s}\n", ind, count, ind)
}

func (g *tableGen) emitMessageReadArrayFrom(f *ir.Field, value, count, ind, from string) {
	g.pf("%s{\n%s    uint64_t n = (uint64_t) %s.min;\n", ind, ind, from)
	g.pf("%s    const int64_t count_bits = TableBitsRequired( %s.min, %s.max );\n", ind, from, from)
	g.pf("%s    if ( count_bits > 0 )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t raw = 0;\n%s        if ( !r.get( raw, count_bits ) ) { report->malformed = true; return false; }\n", ind, ind)
	g.pf("%s        n = raw + (uint64_t) %s.min;\n%s    }\n", ind, from, ind)
	g.pf("%s    if ( %s.elem_kind == 6 && !r.align() ) { report->malformed = true; return false; }\n", ind, from)
	g.pf("%s    int32_t kept = (int32_t) n;\n", ind)
	g.pf("%s    if ( kept > %d ) { kept = %d; report->clamped++; }\n", ind, f.ArrayBound, f.ArrayBound)
	g.pf("%s    for ( uint64_t i = 0; i < n; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        const bool in_bounds = (int32_t) i < kept;\n", ind)
	g.emitMessageReadScalarFromEntry(f, fmt.Sprintf("%s[ in_bounds ? i : 0 ]", value), ind+"        ", from, true)
	g.pf("%s    }\n", ind)
	if f.Array == ir.ArrayCounted {
		g.pf("%s    %s = kept;\n", ind, count)
	}
	g.pf("%s}\n", ind)
}

// emitMessageReadKeyed reads an enum-keyed array (§3.2): the number of present
// slots, then one `(key reference, element)` pair per slot. A key this reader's
// enum cannot name drops its element and counts one `unknown`, and the element
// is read either way because the stream advances past it.
func (g *tableGen) emitMessageReadKeyed(f *ir.Field, ind string) {
	kind := tableScalarKind(f)
	slots := g.keyedSlots("value.", f)
	g.pf("%s{\n%s    uint64_t n = 0;\n", ind, ind)
	g.pf("%s    if ( !r.get( n, TableBitsRequired( 0, entry.max ) ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    for ( uint64_t p = 0; p < n; p++ )\n%s    {\n", ind, ind)
	inner := ind + "        "
	g.pf("%suint64_t key_ref = 0;\n", inner)
	g.pf("%sif ( !r.get( key_ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", inner)
	g.pf("%sif ( key_ref == 0 || key_ref > (uint64_t) vocabulary.count ) { report->malformed = true; return false; }\n", inner)
	g.pf("%s%s key = %s::None;\n", inner, f.KeyEnum, f.KeyEnum)
	g.pf("%sconst bool named = TableEnumValue( TableVocabularyEntryAt( vocabulary, key_ref ).id, key );\n", inner)
	g.pf("%sif ( !named ) { report->unknown++; }\n", inner)
	g.pf("%sconst int32_t slot = named ? (int32_t) key - 1 : -1;\n", inner)
	g.pf("%sconst bool in_bounds = slot >= 0 && slot < %d;\n", inner, f.ArrayBound)
	switch kind {
	case tkTable:
		g.pf("%s%s scratch;\n", inner, f.Type.Name)
		g.pf("%s%sReset( scratch );\n", inner, f.Type.Name)
		g.pf("%sif ( !%sLoadMessageBody( r, vocabulary, report, in_bounds ? %s[slot] : scratch ) ) { return false; }\n", inner, f.Type.Name, slots)
	case tkEnum:
		g.pf("%s%s slot_value = %s::None;\n", inner, f.Type.Name, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value", inner)
		g.pf("%sif ( in_bounds ) { %s[slot] = slot_value; }\n", inner, slots)
	default:
		g.emitMessageReadScalarElement(f, fmt.Sprintf("%s[ in_bounds ? slot : 0 ]", slots), inner)
	}
	g.pf("%s    }\n%s}\n", ind, ind)
}

// emitMessageReadScalar reads ONE value at the width the SENDER's shape states
// and against its range base, and then applies THIS reader's own declared
// bounds: a value outside them clamps and counts, exactly as it does on a file
// (§4). The sender's width is a run-time fact, because a body from another
// build is the ordinary case this wire exists for.
func (g *tableGen) emitMessageReadScalar(f *ir.Field, lvalue, ind, from string) {
	g.emitMessageReadScalarFromEntry(f, lvalue, ind, from, false)
}

// emitMessageReadScalarElement reads one ELEMENT, whose shape is the entry's
// inner one.
func (g *tableGen) emitMessageReadScalarElement(f *ir.Field, lvalue, ind string) {
	g.emitMessageReadScalarFromEntry(f, lvalue, ind, "entry", true)
}

func (g *tableGen) emitMessageReadScalarFromEntry(f *ir.Field, lvalue, ind, from string, element bool) {
	kind := tableScalarKind(f)
	packing, bits, base, qmin, qstep := from+".packing", from+".value_bits", from+".base_lo", from+".qmin", from+".qstep"
	if element {
		packing, bits, base, qmin, qstep = from+".elem_packing", from+".elem_value_bits", from+".elem_base_lo", from+".elem_qmin", from+".elem_qstep"
	}
	width := fmt.Sprintf("TableMessageValueBits( %d, %s, %s )", kind, packing, bits)
	switch kind {
	case tkBool:
		g.pf("%s{ uint64_t raw = 0; if ( !r.get( raw, 1 ) ) { report->malformed = true; return false; } %s = raw != 0; }\n", ind, lvalue)
		return
	case tkF64:
		g.pf("%s{ uint64_t raw = 0; if ( !r.get( raw, 64 ) ) { report->malformed = true; return false; } %s = table_bits_to_double( raw ); }\n", ind, lvalue)
		return
	case tkF32:
		g.pf("%s{\n%s    float decoded_f = 0.0f;\n", ind, ind)
		g.pf("%s    if ( %s == 2 )\n%s    {\n", ind, packing, ind)
		g.pf("%s        uint64_t index = 0;\n", ind)
		g.pf("%s        if ( !r.get( index, %s ) ) { report->malformed = true; return false; }\n", ind, bits)
		g.pf("%s        decoded_f = %s + float( index ) * %s;\n%s    }\n", ind, qmin, qstep, ind)
		g.pf("%s    else\n%s    {\n", ind, ind)
		g.pf("%s        uint64_t raw = 0;\n", ind)
		g.pf("%s        if ( !r.get( raw, 32 ) ) { report->malformed = true; return false; }\n", ind)
		g.pf("%s        decoded_f = table_bits_to_float( (uint32_t) raw );\n%s    }\n", ind, ind)
		if f.HasFloatRange {
			g.pf("%s    if ( decoded_f < %s ) { decoded_f = %s; report->clamped++; }\n", ind, formatFloat(f.FMin, true), formatFloat(f.FMin, true))
			g.pf("%s    else if ( decoded_f > %s ) { decoded_f = %s; report->clamped++; }\n", ind, formatFloat(f.FMax, true), formatFloat(f.FMax, true))
		}
		g.pf("%s    %s = decoded_f;\n%s}\n", ind, lvalue, ind)
		return
	}
	signed := ir.TableKindSigned(kind)
	bytesWide := tableKindWidth(kind)
	g.pf("%s{\n", ind)
	if bytesWide == 16 {
		storage := "serialize::uint128_t"
		if signed {
			storage = "serialize::int128_t"
		}
		g.pf("%s    uint64_t lo_v = 0, hi_v = 0;\n", ind)
		g.pf("%s    if ( !r.get( lo_v, 64 ) || !r.get( hi_v, 64 ) ) { report->malformed = true; return false; }\n", ind)
		g.pf("%s    %s decoded_v = %s( ( serialize::uint128_t( hi_v ) << 64 ) | serialize::uint128_t( lo_v ) );\n", ind, storage, storage)
		g.emitMessageClamp(f, signed, bytesWide, ind+"    ")
		g.pf("%s    %s = decoded_v;\n%s}\n", ind, lvalue, ind)
		return
	}
	storage := fmt.Sprintf("uint%d_t", bytesWide*8)
	if signed {
		storage = fmt.Sprintf("int%d_t", bytesWide*8)
	}
	g.pf("%s    uint64_t raw = 0;\n", ind)
	g.pf("%s    const int64_t width = %s;\n", ind, width)
	g.pf("%s    if ( width < 0 || !r.get( raw, width ) ) { report->malformed = true; return false; }\n", ind)
	g.pf("%s    int64_t decoded_wide = (int64_t) raw;\n", ind)
	g.pf("%s    if ( %s == 1 ) { decoded_wide = (int64_t) raw + %s; }\n", ind, packing, base)
	if signed {
		g.pf("%s    else if ( width > 0 && width < 64 )\n%s    {\n", ind, ind)
		g.pf("%s        const uint64_t sign = uint64_t(1) << ( width - 1 );\n", ind)
		g.pf("%s        if ( ( raw & sign ) != 0 ) { decoded_wide = (int64_t) ( raw | ~( ( uint64_t(1) << width ) - 1 ) ); }\n", ind)
		g.pf("%s    }\n", ind)
	}
	g.pf("%s    %s decoded_v = (%s) decoded_wide;\n", ind, storage, storage)
	g.emitMessageClamp(f, signed, bytesWide, ind+"    ")
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

// ---- the batch's three verbs ----

// emitMessageEntries emits one ROOT's message surface (§3.3). The verbs are
// PLURAL because the primitive is a batch and a single message is the batch of
// one: a surface with a singular verb beside them would let a caller write one
// body a call and never learn that the batch is where the bandwidth is.
func (g *tableGen) emitMessageEntries(st *ir.Struct) {
	if st.IsMapEntry() || !g.messageCarried(st) {
		// a root whose closure this codec does not carry gets no surface at
		// all, rather than half of one
		return
	}
	n := st.Name
	g.pf("// THE PRIMITIVE IS A BATCH (docs/SPEC-TABLES.md §3.3): a number of bodies of\n")
	g.pf("// ONE ROOT in one buffer, one count and one continuous bit stream with no\n")
	g.pf("// alignment between them. A single message is the batch of one, and there is\n")
	g.pf("// no singular verb.\n")
	g.pf("//\n")
	g.pf("// Every reference is a compile-time SLOT and every width a literal, so a save\n")
	g.pf("// does no lookup at all and costs what a save costs.\n")
	g.pf("inline int64_t %sMeasureMessages( const %s * values, int64_t count )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 1 || count > kTableMessageBatchMax ) { return -1; }\n")
	g.pf("    int64_t bits = 8; // the body count, a ranged integer over [1, 256]\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        const int64_t body = %sMeasureMessageBody( bits, values[i] );\n", n)
	g.pf("        if ( body < 0 ) { return -1; }\n")
	g.pf("        bits += body;\n    }\n")
	g.pf("    return 1 + ( bits + 7 ) / 8; // the form byte, then the stream padded to a byte\n}\n\n")

	g.pf("inline int64_t %sSaveMessages( const %s * values, int64_t count, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    TableMessageBatch batch;\n")
	g.pf("    if ( values == NULL || !TableMessageBatchBegin( batch, buffer, capacity, count ) ) { return -1; }\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( !%sSaveMessageBody( batch.w, values[i] ) ) { return -1; }\n", n)
	g.pf("        batch.written++;\n    }\n")
	g.pf("    return TableMessageBatchEnd( batch ); // == %sMeasureMessages( values, count )\n}\n\n", n)

	g.pf("// A form 2 wire with NO VOCABULARY for the announcement is REFUSED BY NAME:\n")
	g.pf("// nothing is decoded, the reader says it holds no vocabulary, no counter\n")
	g.pf("// moves and malformed does not fire. A reader does not fall back to the file\n")
	g.pf("// form on its own and does not guess a vocabulary, because a guessed one\n")
	g.pf("// decodes a body under the wrong names in silence.\n")
	g.pf("//\n")
	g.pf("// It answers how many bodies it read, or -1. `capacity` is the caller's own\n")
	g.pf("// bound: a count of 256 over a two-body buffer is exhaustion, never an\n")
	g.pf("// allocation.\n")
	g.pf("inline int64_t %sLoadMessages( %s * values, int64_t capacity, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * to = report != NULL ? report : &ignored;\n")
	g.pf("    TableMessageBatchReader br;\n")
	g.pf("    const int64_t count = TableMessageBatchOpen( br, vocabulary, buffer, bytes, to );\n")
	g.pf("    if ( count < 0 ) { return -1; }\n")
	g.pf("    if ( values == NULL || count > capacity ) { to->malformed = true; return -1; }\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( !%sLoadMessageBody( br.r, vocabulary, to, values[i] ) ) { return -1; }\n", n)
	g.pf("        br.remaining--;\n    }\n")
	g.pf("    if ( !TableMessageBatchClose( br ) ) { return -1; }\n")
	g.pf("    return count;\n}\n\n")
}
