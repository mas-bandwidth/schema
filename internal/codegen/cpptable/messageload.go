// THE MESSAGE FORM's READ (docs/SPEC-TABLES.md §3.3), and the BATCH's three
// verbs over a FIXED root.
//
// A body carries no kind byte, so the two things a tolerant reader needs come
// from the announcement instead: the ENTRY's shape says how wide each payload
// is and how to step over one this build cannot name, and the KIND MISMATCH §4
// already counts is found by comparing that kind against the reader's own
// declaration rather than by reading a byte. Everything else is the file
// form's, unchanged: the prefill of declared defaults, the overlay, the clamps
// and the counters.
//
// A VARIABLE table's read takes the node map its pointer slots resolve
// through and the index width the body's node table settled (§3.1, §3.3): a
// pointer index is read at that width and resolved, never followed, and a map
// carves its entries from the node's extent exactly as the file form's does.
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
	if g.isVar(st.Name) {
		g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, const TableNodeMap & nodes, int64_t index_bits, %s & value )\n{\n", st.Name, st.Name)
		g.pf("    (void) nodes; (void) index_bits;\n")
	} else {
		g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, int64_t index_bits, %s & value )\n{\n", st.Name, st.Name)
	}
	g.pf("    %sReset( value );\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t ref = 0;\n")
	g.pf("        if ( !r.get( ref, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n")
	g.pf("        if ( ref == 0 ) { return true; } // the body ENDS AT ITS OWN ZERO REFERENCE\n")
	g.pf("        if ( ref > (uint64_t) vocabulary.count ) { report->malformed = true; return false; }\n")
	g.pf("        const TableMessageEntry & entry = TableVocabularyEntryAt( vocabulary, ref );\n")
	g.pf("        // A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS\n")
	g.pf("        // MALFORMED (§3.1, §3.3): the node table is the ROOT body's first\n")
	g.pf("        // field and is read before this walk begins, so meeting one here is\n")
	g.pf("        // a second numbering wherever it sits\n")
	g.pf("        if ( TableMessageReserved( entry.id ) ) { report->malformed = true; return false; }\n")
	g.pf("        switch ( entry.id )\n        {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("            case 0x%016xull: // %s\n            {\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                // THE KIND MISMATCH IS FOUND IN THE ANNOUNCEMENT, not on the body.\n")
		g.pf("                // A RANGE that moved is not one: the shapes differ and the entry\n")
		g.pf("                // carries the SENDER's, so the field decodes and clamps (§4).\n")
		g.pf("                if ( entry.kind != %d || entry.elem_kind != %d )\n                {\n", mine.Kind, mine.Shape.Elem)
		g.emitMessageWidenBranch(f, mine, "entry", "                    ")
		g.pf("                    report->kind_mismatch++;\n")
		g.pf("                    if ( !TableMessageSkip( r, vocabulary, index_bits, entry ) ) { report->malformed = true; return false; }\n")
		g.pf("                    break;\n                }\n")
		g.emitMessageReadField(f)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n")
	g.pf("                report->unknown++;\n")
	g.pf("                if ( !TableMessageSkip( r, vocabulary, index_bits, entry ) ) { report->malformed = true; return false; }\n")
	g.pf("                break;\n")
	g.pf("        }\n    }\n}\n\n")
}

// emitMessageWidenBranch is §4's WIDENING RULE at a message field, which §3.3
// holds to the file form's word: an announced kind below this reader's on the
// same ladder decodes EXACTLY at the width the ANNOUNCEMENT states, the value
// lands, and one `widened` counts. It is emitted only where a widening is
// possible and only INSIDE the mismatch branch the reader already takes, so a
// payload under the declared kind never reaches it and the matching path pays
// nothing.
func (g *tableGen) emitMessageWidenBranch(f *ir.Field, mine ir.TableVocabularyEntry, from, ind string) {
	switch {
	case plainScalar(f):
		g.pf("%sif ( %s.elem_kind == %d && TableKindWidens( %s.kind, %d ) )\n%s{\n", ind, from, mine.Shape.Elem, from, mine.Kind, ind)
		g.emitMessageReadScalarFromEntry(f, "value."+f.Name, ind+"    ", from, false, true)
		if f.Type.Optional {
			g.pf("%s    value.%s_present = true; // the field rode, so it is PRESENT (§2.3)\n", ind, f.Name)
		}
		g.pf("%s    report->widened++;\n%s    break;\n%s}\n", ind, ind, ind)
	case f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed:
		// a POSITIONAL array only: an enum-keyed body, a map and a blob are
		// each their own construct with their own reader (§3.2, §2.5, §2.8)
		if f.KeyEnum != "" || f.IsMap() || f.Type.Blob() || !widenableElement(f) {
			return
		}
		// AN ARRAY COUNTS ONE `widened` FOR THE FIELD however many elements
		// it holds (§4), and every element decodes at the announced width.
		g.pf("%sif ( %s.kind == %d && TableKindWidens( %s.elem_kind, %d ) )\n%s{\n", ind, from, mine.Kind, from, mine.Shape.Elem, ind)
		g.emitMessageReadArrayFrom(f, "value."+f.Name, fmt.Sprintf("value.%s_count", f.Name), ind+"    ", from, true)
		g.pf("%s    report->widened++;\n%s    break;\n%s}\n", ind, ind, ind)
	}
}

func (g *tableGen) emitMessageReadField(f *ir.Field) {
	ind := "                "
	name := f.Name
	switch {
	case f.IsMap():
		g.emitMessageReadMap(f, ind)
	case f.IsList():
		g.emitMessageReadList(f, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitMessageReadIndex(f, "value."+name, ind)
	case f.Type.Optional:
		g.emitMessageReadPayload(f, "value."+name, ind)
		g.pf("%svalue.%s_present = true; // the field rode, so it is PRESENT (§2.3)\n", ind, name)
	case f.KeyEnum != "":
		g.emitMessageReadKeyed(f, ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		g.emitMessageReadTextFrom(f, "value."+name, fmt.Sprintf("value.%s_length", name), ind, "entry")
	case f.Array != ir.ArrayNone:
		g.emitMessageReadArrayFrom(f, "value."+name, fmt.Sprintf("value.%s_count", name), ind, "entry", false)
	case tableScalarKind(f) == tkUnion:
		g.emitMessageReadUnion(f, "value."+name, ind)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%s ) { return false; }\n", ind, g.msgLoadCall(f.Type.Name, "r", "value."+name))
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, "value."+name, ind)
	default:
		g.emitMessageReadScalarFromEntry(f, "value."+name, ind, "entry", false, false)
	}
}

func (g *tableGen) emitMessageReadPayload(f *ir.Field, expr, ind string) {
	switch {
	case f.Array != ir.ArrayNone:
		g.emitMessageReadArrayFrom(f, expr, fmt.Sprintf("value.%s_count", f.Name), ind, "entry", false)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%s ) { return false; }\n", ind, g.msgLoadCall(f.Type.Name, "r", expr))
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, expr, ind)
	default:
		g.emitMessageReadScalarFromEntry(f, expr, ind, "entry", false, false)
	}
}

// emitMessageReadIndex reads one NODE INDEX at the body's index width and
// resolves it through the numbering, never following it (docs/SPEC-TABLES.md
// §3.1, §3.3).
func (g *tableGen) emitMessageReadIndex(f *ir.Field, dst, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	target := fmt.Sprintf("0x%016xull", ir.TableWireId(f.Type.Name))
	comment := "*" + f.Type.Name
	if f.Type.Blob() {
		target = blobTypeIdConst(f)
		comment = "*" + blobWord(f)
	}
	g.pf("%s{\n%s    uint64_t node_index%s = 0;\n", ind, ind, sfx)
	g.pf("%s    if ( !r.get( node_index%s, index_bits ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    TableNodeResolve( nodes, %s, node_index%s, %s, report ); // %s\n%s}\n", ind, dst, sfx, target, comment, ind)
}

// emitMessageReadTextFrom reads a `string(N)` or a `bytes(N)`: the length at
// the SENDER's own width, the ALIGN, then the bytes; or a `wstring(N)`: the
// length, no align, sixteen bits a unit. A payload longer than this reader's
// bound keeps what fits and counts `clamped`, which is not damage.
func (g *tableGen) emitMessageReadTextFrom(f *ir.Field, value, count, ind, from string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	width := fmt.Sprintf("TableBitsRequired( 0, %s.max )", from)
	if f.Type.Kind == ir.TBytes {
		width = fmt.Sprintf("TableBitsRequired( %s.min, %s.max )", from, from)
	}
	g.pf("%s{\n%s    uint64_t n%s = 0;\n", ind, ind, sfx)
	// A LENGTH WHOSE PAYLOAD RUNS PAST THE BATCH IS DAMAGE at the field that
	// carried it (§3.3), found before the clamp so nothing else counts
	if f.Type.Kind == ir.TWString {
		g.pf("%s    if ( !r.get( n%s, %s ) || !r.has( (int64_t) n%s * 16 ) ) { report->malformed = true; return false; }\n", ind, sfx, width, sfx)
	} else {
		g.pf("%s    if ( !r.get( n%s, %s ) || !r.align() || !r.has( (int64_t) n%s * 8 ) ) { report->malformed = true; return false; }\n", ind, sfx, width, sfx)
	}
	// THE BOUND APPLIES WHILE THE COUNT IS WIDE (§3.3), which is M6's
	// discipline over a count rather than a value: a length at or above 2^31
	// narrowed FIRST is negative, passes a signed test against the bound
	// untouched, and lands a negative length in the caller's storage
	g.pf("%s    int32_t kept%s = 0;\n", ind, sfx)
	g.pf("%s    if ( n%s > (uint64_t) %d ) { kept%s = %d; report->clamped++; } else { kept%s = (int32_t) n%s; }\n", ind, sfx, f.Type.Size, sfx, f.Type.Size, sfx, sfx)
	g.pf("%s    for ( uint64_t %s = 0; %s < n%s; %s++ )\n%s    {\n", ind, idx, idx, sfx, idx, ind)
	if f.Type.Kind == ir.TWString {
		g.pf("%s        uint64_t unit%s = 0;\n%s        if ( !r.get( unit%s, 16 ) ) { report->malformed = true; return false; }\n", ind, sfx, ind, sfx)
		g.pf("%s        if ( (int32_t) %s < kept%s ) { %s[%s] = (char16_t) unit%s; }\n%s    }\n", ind, idx, sfx, value, idx, sfx, ind)
		g.pf("%s    %s[kept%s] = 0;\n", ind, value, sfx)
	} else {
		g.pf("%s        uint64_t by%s = 0;\n%s        if ( !r.get( by%s, 8 ) ) { report->malformed = true; return false; }\n", ind, sfx, ind, sfx)
		cast := "(uint8_t)"
		if f.Type.Kind == ir.TString {
			cast = "(char)"
		}
		g.pf("%s        if ( (int32_t) %s < kept%s ) { %s[%s] = %s by%s; }\n%s    }\n", ind, idx, sfx, value, idx, cast, sfx, ind)
		if f.Type.Kind == ir.TString {
			g.pf("%s    %s[kept%s] = 0;\n", ind, value, sfx)
		}
	}
	g.pf("%s    %s = kept%s;\n%s}\n", ind, count, sfx, ind)
}

// emitMessageReadArrayFrom reads a positional array. NO COUNT RIDES where the
// sender's `min` equals its `max`. A count above this reader's own bound keeps
// the first N and counts `clamped`: the surplus is stepped over, because the
// stream advances past it either way, and how it is stepped over depends on
// the element. A FIXED-WIDTH ELEMENT IS ARITHMETIC, the surplus count times
// the element's width, and an element that RESOLVES something (a nested body,
// a union arm, an enum's variant, a node index) is walked, because a resolve
// that contradicts its position is damage this reader must still find (§3.3).
func (g *tableGen) emitMessageReadArrayFrom(f *ir.Field, base, count, ind, from string, widened bool) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	kind := tableScalarKind(f)
	fixedSurplus := !f.Type.Pointer && kind != tkTable && kind != tkEnum && kind != tkUnion
	g.pf("%s{\n%s    uint64_t n%s = (uint64_t) %s.min;\n", ind, ind, sfx, from)
	g.pf("%s    const int64_t count_bits%s = TableBitsRequired( %s.min, %s.max );\n", ind, sfx, from, from)
	g.pf("%s    if ( count_bits%s > 0 )\n%s    {\n", ind, sfx, ind)
	g.pf("%s        uint64_t raw%s = 0;\n%s        if ( !r.get( raw%s, count_bits%s ) ) { report->malformed = true; return false; }\n", ind, sfx, ind, sfx, sfx)
	g.pf("%s        n%s = raw%s + (uint64_t) %s.min;\n%s    }\n", ind, sfx, sfx, from, ind)
	g.pf("%s    if ( %s.elem_kind == 6 && !r.align() ) { report->malformed = true; return false; }\n", ind, from)
	// THE BOUND APPLIES WHILE THE COUNT IS WIDE (§3.3), M6's discipline over a
	// count: a count at or above 2^31 narrowed FIRST is negative, passes a
	// signed test against the bound untouched, and lands a negative count in
	// the caller's storage
	g.pf("%s    int32_t kept%s = 0;\n", ind, sfx)
	g.pf("%s    if ( n%s > (uint64_t) %d ) { kept%s = %d; report->clamped++; } else { kept%s = (int32_t) n%s; }\n", ind, sfx, f.ArrayBound, sfx, f.ArrayBound, sfx, sfx)
	if fixedSurplus {
		g.pf("%s    const int64_t surplus_bits%s = %s.elem_value_bits;\n", ind, sfx, from)
		g.pf("%s    uint64_t walk%s = n%s;\n", ind, sfx, sfx)
		// the walk's bound is this reader's OWN, a compile-time constant, and
		// not the count narrowed above: the two agree, and a bound that
		// cannot be moved by a number off the wire is the one to loop against
		g.pf("%s    if ( surplus_bits%s >= 0 && walk%s > (uint64_t) %d ) { walk%s = (uint64_t) %d; } // the surplus is arithmetic\n", ind, sfx, sfx, f.ArrayBound, sfx, f.ArrayBound)
	} else {
		g.pf("%s    const uint64_t walk%s = n%s;\n", ind, sfx, sfx)
	}
	g.pf("%s    for ( uint64_t %s = 0; %s < walk%s; %s++ )\n%s    {\n", ind, idx, idx, sfx, idx, ind)
	inner := ind + "        "
	g.pf("%sconst bool in_bounds%s = (int32_t) %s < kept%s;\n", inner, sfx, idx, sfx)
	elem := fmt.Sprintf("( in_bounds%s ? %s[%s] : scratch%s )", sfx, base, idx, sfx)
	switch {
	case f.Type.Pointer:
		g.pf("%sTableRef scratch%s;\n", inner, sfx)
		g.emitMessageReadIndex(f, elem, inner)
	case tableScalarKind(f) == tkTable:
		g.pf("%s%s scratch%s;\n", inner, f.Type.Name, sfx)
		g.pf("%s%sReset( scratch%s );\n", inner, f.Type.Name, sfx)
		g.pf("%sif ( !%s ) { return false; }\n", inner, g.msgLoadCall(f.Type.Name, "r", elem))
	case tableScalarKind(f) == tkEnum:
		g.pf("%s%s slot_value%s = %s::None;\n", inner, f.Type.Name, sfx, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value"+sfx, inner)
		g.pf("%sif ( in_bounds%s ) { %s[%s] = slot_value%s; }\n", inner, sfx, base, idx, sfx)
	case tableScalarKind(f) == tkUnion:
		g.pf("%s%s scratch%s;\n", inner, f.Type.Name, sfx)
		g.emitMessageReadUnion(f, elem, inner)
	default:
		// A DISCARDED SURPLUS ELEMENT NEVER ACQUIRES A LIVE DESTINATION. A
		// fixed-width surplus is stepped over by the arithmetic below and
		// never decoded at all; a WALKED one decodes into scratch, and only
		// an in-bounds element lands
		typ, _ := g.cppFieldType(f.Type)
		g.pf("%s%s scratch%s = %s;\n", inner, typ, sfx, g.fieldDefaultExpr(f))
		g.emitMessageReadScalarFromEntry(f, "scratch"+sfx, inner, from, true, widened)
		g.pf("%sif ( in_bounds%s ) { %s[%s] = scratch%s; }\n", inner, sfx, base, idx, sfx)
	}
	g.pf("%s    }\n", ind)
	if fixedSurplus {
		g.pf("%s    if ( walk%s < n%s && !TableMessageSkipRun( r, n%s - walk%s, surplus_bits%s ) ) { report->malformed = true; return false; }\n", ind, sfx, sfx, sfx, sfx, sfx)
	}
	if f.CountedOnWire() {
		g.pf("%s    %s = kept%s;\n", ind, count, sfx)
	}
	if f.Type.Optional {
		g.pf("%s    value.%s_present = true;\n", ind, f.Name)
	}
	g.pf("%s}\n", ind)
}

// emitMessageReadEnum reads an enum value: the reference naming its VARIANT's
// name, `0` for None. A reference this reader's enum cannot name is §4's
// ordinary `unknown`: the field reads None and one event counts. A VARIANT
// REFERENCE NAMING AN ENTRY OF THE WRONG SORT IS MALFORMED, and the
// reserved-id rule outranks it (§3.3).
func (g *tableGen) emitMessageReadEnum(f *ir.Field, dst, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	e := f.Type.Name
	g.pf("%s{\n%s    uint64_t variant_ref%s = 0;\n", ind, ind, sfx)
	g.pf("%s    if ( !r.get( variant_ref%s, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    TableMessageEntry variant_entry%s;\n", ind, sfx)
	g.pf("%s    if ( variant_ref%s == 0 ) { %s = %s::None; } // the zero reference is the enum's None\n", ind, sfx, dst, e)
	g.pf("%s    else if ( !TableMessageNameEntry( vocabulary, variant_ref%s, variant_entry%s ) ) { report->malformed = true; return false; }\n", ind, sfx, sfx)
	g.pf("%s    else if ( !TableEnumValue( variant_entry%s.id, %s ) ) { %s = %s::None; report->unknown++; }\n", ind, sfx, dst, dst, e)
	g.pf("%s}\n", ind)
}

// emitMessageReadUnion reads a union: the ARM's reference, then the payload a
// FIELD of the arm's type carries. An arm this reader cannot name is §4's
// ordinary `unknown`, and its payload is stepped over by the ARM's own entry.
// AN ARM REFERENCE NAMING A KIND-0 ENTRY IS MALFORMED: the reader has nothing
// to frame the payload with (§3.3).
func (g *tableGen) emitMessageReadUnion(f *ir.Field, dst, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	un := f.Type.Ref.(*ir.Union)
	arm := "arm" + sfx
	g.pf("%s{\n%s    uint64_t arm_ref%s = 0;\n", ind, ind, sfx)
	g.pf("%s    if ( !r.get( arm_ref%s, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    TableMessageEntry %s;\n", ind, arm)
	g.pf("%s    if ( arm_ref%s == 0 ) { %s.type = %sType::None; }\n", ind, sfx, dst, un.Name)
	g.pf("%s    else if ( !TableMessageArmEntry( vocabulary, arm_ref%s, %s ) ) { report->malformed = true; return false; }\n", ind, sfx, arm)
	g.pf("%s    else\n%s    {\n", ind, ind)
	g.pf("%s        switch ( %s.id )\n%s        {\n", ind, arm, ind)
	for _, v := range un.Variants {
		mine := ir.TableArmEntry(v)
		g.noteRef(v.Type)
		g.pf("%s            case 0x%016xull: // %s\n%s            {\n", ind, ir.TableWireId(v.Name), v.Name, ind)
		g.pf("%s                if ( %s.kind != %d || %s.elem_kind != %d )\n%s                {\n", ind, arm, mine.Kind, arm, mine.Shape.Elem, ind)
		if !v.Void() && !v.Body() && plainScalar(v.F) {
			// WIDENED AT AN ARM (§3.3, §4): the arm is SELECTED and its
			// payload decodes at the width the ANNOUNCEMENT states, rather
			// than skipped. The branch sits inside the mismatch branch the
			// reader already takes, so the matching path pays nothing.
			g.pf("%s                    if ( TableKindWidens( %s.kind, %d ) )\n%s                    {\n", ind, arm, mine.Kind, ind)
			g.pf("%s                        %s.type = %sType::%s;\n", ind, dst, un.Name, ir.GoExportName(v.Name))
			g.establishArm(v, dst, ind+"                        ")
			g.emitMessageReadScalarFromEntry(v.F, armValue(dst, v), ind+"                        ", arm, false, true)
			g.pf("%s                        report->widened++;\n", ind)
			g.pf("%s                        break;\n%s                    }\n", ind, ind)
		}
		g.pf("%s                    %s.type = %sType::None; report->kind_mismatch++;\n", ind, dst, un.Name)
		g.pf("%s                    if ( !TableMessageSkip( r, vocabulary, index_bits, %s ) ) { report->malformed = true; return false; }\n", ind, arm)
		g.pf("%s                    break;\n%s                }\n", ind, ind)
		g.pf("%s                %s.type = %sType::%s;\n", ind, dst, un.Name, ir.GoExportName(v.Name))
		switch {
		case v.Void():
		case v.Body():
			g.pf("%s                if ( !%s ) { return false; }\n", ind, g.msgLoadCall(v.Type, "r", armValue(dst, v)))
		default:
			g.emitMessageReadArm(v, dst, ind+"                ", arm, false)
		}
		g.pf("%s                break;\n%s            }\n", ind, ind)
	}
	g.pf("%s            default:\n", ind)
	g.pf("%s                %s.type = %sType::None; report->unknown++;\n", ind, dst, un.Name)
	g.pf("%s                if ( !TableMessageSkip( r, vocabulary, index_bits, %s ) ) { report->malformed = true; return false; }\n", ind, arm)
	g.pf("%s                break;\n%s        }\n%s    }\n%s}\n", ind, ind, ind, ind)
}

func (g *tableGen) emitMessageReadArm(v ir.UnionVariant, base, ind, arm string, widened bool) {
	af := v.F
	value, count := armValue(base, v), armCount(base, v)
	switch {
	case af.Type.Pointer && af.Array == ir.ArrayNone:
		g.emitMessageReadIndex(af, value, ind)
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes || af.Type.Kind == ir.TWString:
		g.emitMessageReadTextFrom(af, value, count, ind, arm)
	case af.Array != ir.ArrayNone:
		g.emitMessageReadArrayFrom(af, value, count, ind, arm, widened)
	case tableScalarKind(af) == tkUnion:
		// a NESTED UNION ARM reads the inner union's own arm reference (§2.6)
		g.emitMessageReadUnion(af, value, ind)
	case tableScalarKind(af) == tkEnum:
		g.emitMessageReadEnum(af, value, ind)
	case tableScalarKind(af) == tkTable:
		g.pf("%sif ( !%s ) { return false; }\n", ind, g.msgLoadCall(af.Type.Name, "r", value))
	default:
		g.emitMessageReadScalarFromEntry(af, value, ind, arm, false, widened)
	}
}

// emitMessageReadKeyed reads an enum-keyed array (§3.2): the number of present
// slots, then one `(key reference, element)` pair per slot. A key this reader's
// enum cannot name drops its element and counts one `unknown`, and the element
// is read either way because the stream advances past it. A key of 0, one past
// E, one naming a reserved id or one naming an entry of the wrong sort is
// damage (§3.2, §3.3).
func (g *tableGen) emitMessageReadKeyed(f *ir.Field, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	kind := tableScalarKind(f)
	slots := g.keyedSlots("value.", f)
	g.pf("%s{\n%s    uint64_t n%s = 0;\n", ind, ind, sfx)
	g.pf("%s    if ( !r.get( n%s, TableBitsRequired( 0, entry.max ) ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    for ( uint64_t p%s = 0; p%s < n%s; p%s++ )\n%s    {\n", ind, sfx, sfx, sfx, sfx, ind)
	inner := ind + "        "
	g.pf("%suint64_t key_ref%s = 0;\n", inner, sfx)
	g.pf("%sif ( !r.get( key_ref%s, vocabulary.ref_bits ) ) { report->malformed = true; return false; }\n", inner, sfx)
	g.pf("%sTableMessageEntry key_entry%s;\n", inner, sfx)
	g.pf("%sif ( !TableMessageNameEntry( vocabulary, key_ref%s, key_entry%s ) ) { report->malformed = true; return false; }\n", inner, sfx, sfx)
	g.pf("%s%s key%s = %s::None;\n", inner, f.KeyEnum, sfx, f.KeyEnum)
	g.pf("%sconst bool named%s = TableEnumValue( key_entry%s.id, key%s );\n", inner, sfx, sfx, sfx)
	g.pf("%sif ( !named%s ) { report->unknown++; }\n", inner, sfx)
	g.pf("%sconst int32_t slot%s = named%s ? (int32_t) key%s - 1 : -1;\n", inner, sfx, sfx, sfx)
	g.pf("%sconst bool in_bounds%s = slot%s >= 0 && slot%s < %d;\n", inner, sfx, sfx, sfx, f.ArrayBound)
	elem := fmt.Sprintf("( in_bounds%s ? %s[slot%s] : scratch%s )", sfx, slots, sfx, sfx)
	switch {
	case f.Type.Pointer:
		g.pf("%sTableRef scratch%s;\n", inner, sfx)
		g.emitMessageReadIndex(f, elem, inner)
	case kind == tkTable:
		g.pf("%s%s scratch%s;\n", inner, f.Type.Name, sfx)
		g.pf("%s%sReset( scratch%s );\n", inner, f.Type.Name, sfx)
		g.pf("%sif ( !%s ) { return false; }\n", inner, g.msgLoadCall(f.Type.Name, "r", elem))
	case kind == tkEnum:
		g.pf("%s%s slot_value%s = %s::None;\n", inner, f.Type.Name, sfx, f.Type.Name)
		g.emitMessageReadEnum(f, "slot_value"+sfx, inner)
		g.pf("%sif ( in_bounds%s ) { %s[slot%s] = slot_value%s; }\n", inner, sfx, slots, sfx, sfx)
	default:
		// an unknown key's element decodes into scratch and lands nowhere
		typ, _ := g.cppFieldType(f.Type)
		g.pf("%s%s scratch%s = %s;\n", inner, typ, sfx, g.fieldDefaultExpr(f))
		g.emitMessageReadScalarFromEntry(f, "scratch"+sfx, inner, "entry", true, false)
		g.pf("%sif ( in_bounds%s ) { %s[slot%s] = scratch%s; }\n", inner, sfx, slots, sfx, sfx)
	}
	g.pf("%s    }\n%s}\n", ind, ind)
}

// emitMessageReadMap decodes one map field on the message wire, and it is
// where every reader rule §2.8 states lands, one difference aside: the count
// is the thirty-two bits the data decides, an entry has no L, so its KEY is
// found by a scan that also finds where the entry ends, and a DESCENDING key
// is damage the batch cannot recover from, because there is no map L for the
// parent to read on past (§3.3).
func (g *tableGen) emitMessageReadMap(f *ir.Field, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	me := mapEntryOf(f)
	key := me.Fields[0]
	n := me.Name
	stringKey := key.Type.Kind == ir.TString
	g.pf("%s{\n", ind)
	g.pf("%s    uint64_t count%s = 0;\n", ind, sfx)
	g.pf("%s    if ( !r.get( count%s, TableBitsRequired( entry.min, entry.max ) ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    count%s += (uint64_t) entry.min;\n", ind, sfx)
	g.pf("%s    TableMapFill<%s> fill%s = TableMapFillBegin( nodes, value.%s, (uint32_t) count%s );\n", ind, n, sfx, f.Name, sfx)
	g.pf("%s    if ( !fill%s.ok ) { report->malformed = true; return false; } // the measure and the load disagree\n", ind, sfx)
	if stringKey {
		g.pf("%s    const char * last_key%s = NULL; int32_t last_length%s = 0;\n", ind, sfx, sfx)
	} else {
		typ, _ := g.cppFieldType(key.Type)
		g.pf("%s    %s last_key%s = 0;\n", ind, typ, sfx)
	}
	g.pf("%s    bool landed%s = false;\n", ind, sfx)
	g.pf("%s    bool map_widened%s = false;\n", ind, sfx)
	g.pf("%s    for ( uint64_t %s = 0; %s < count%s; %s++ )\n%s    {\n", ind, idx, idx, sfx, idx, ind)
	g.pf("%s        const %sMessageKeyRead read%s = %sMessageReadKey( r, vocabulary, index_bits );\n", ind, n, sfx, n)
	g.pf("%s        if ( read%s.malformed ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s        // A KEY KIND THE DECLARATION WIDENS: the map counts ONE widened (§2.8, §4)\n", ind)
	g.pf("%s        if ( read%s.widened && !map_widened%s ) { map_widened%s = true; report->widened++; }\n", ind, sfx, sfx, sfx)
	g.pf("%s        if ( read%s.kind_bad )\n%s        {\n", ind, sfx, ind)
	g.pf("%s            // A MAP WITH HALF ITS KEYS IS NOT A MAP (§2.8): the map resets to\n", ind)
	g.pf("%s            // EMPTY, ONE kind_mismatch is counted for it, and the rest of its\n", ind)
	g.pf("%s            // entries are stepped over by their shapes\n", ind)
	g.pf("%s            report->kind_mismatch++;\n", ind)
	g.pf("%s            TableMapFillReset( fill%s );\n", ind, sfx)
	g.pf("%s            r.offset = read%s.end;\n", ind, sfx)
	g.pf("%s            for ( uint64_t j%s = %s + 1; j%s < count%s; j%s++ ) { if ( !TableMessageSkipBody( r, vocabulary, index_bits ) ) { report->malformed = true; return false; } }\n", ind, sfx, idx, sfx, sfx, sfx)
	g.pf("%s            break;\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        if ( read%s.over ) { report->clamped++; r.offset = read%s.end; continue; } // dropped whole, one count per entry\n", ind, sfx, sfx)
	if stringKey {
		g.pf("%s        const int order%s = landed%s ? TableKeyOrder( last_key%s, last_length%s, read%s.key, read%s.length ) : -1;\n", ind, sfx, sfx, sfx, sfx, sfx, sfx)
	} else {
		ord := mapKeyOrderType(f)
		g.pf("%s        const int order%s = landed%s ? TableKeyOrder( (%s) last_key%s, (%s) read%s.key ) : -1;\n", ind, sfx, sfx, ord, sfx, ord, sfx)
	}
	g.pf("%s        if ( order%s > 0 ) { report->malformed = true; return false; } // DESCENDING: not a body any conforming writer produced\n", ind, sfx)
	g.pf("%s        %s * slot%s = NULL;\n", ind, n, sfx)
	g.pf("%s        if ( order%s == 0 )\n%s        {\n", ind, sfx, ind)
	g.pf("%s            // EQUAL: a DUPLICATE. The slot that entry took is reset by the\n", ind)
	g.pf("%s            // decode below, so LAST WINS WHOLE, and the count excludes it.\n", ind)
	g.pf("%s            slot%s = TableMapFillLast( fill%s );\n", ind, sfx, sfx)
	g.pf("%s            report->duplicate++;\n", ind)
	g.pf("%s        }\n%s        else\n%s        {\n", ind, ind, ind)
	g.pf("%s            slot%s = TableMapFillNext( fill%s ); // ASCENDING: the next slot\n", ind, sfx, sfx)
	g.pf("%s        }\n", ind)
	g.pf("%s        if ( slot%s == NULL ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s        if ( !%s ) { return false; }\n", ind, g.msgLoadCall(n, "r", "*slot"+sfx))
	g.pf("%s        if ( r.offset != read%s.end ) { report->malformed = true; return false; } // the scan and the decode disagree about where the entry ends\n", ind, sfx)
	if stringKey {
		g.pf("%s        last_key%s = read%s.key; last_length%s = read%s.length; // the WIRE keys of the entries that LAND\n", ind, sfx, sfx, sfx, sfx)
	} else {
		g.pf("%s        last_key%s = read%s.key; // the WIRE keys of the entries that LAND\n", ind, sfx, sfx)
	}
	g.pf("%s        landed%s = true;\n", ind, sfx)
	g.pf("%s    }\n", ind)
	g.pf("%s    TableMapFillEnd( fill%s );\n", ind, sfx)
	g.pf("%s}\n", ind)
}

// emitMessageReadScalarFromEntry reads ONE value at the width the SENDER's
// shape states and against its range base, and then applies THIS reader's own
// declared bounds: a value outside them clamps and counts, exactly as it does
// on a file (§4). The sender's width is a run-time fact, because a body from
// another build is the ordinary case this wire exists for.
func (g *tableGen) emitMessageReadScalarFromEntry(f *ir.Field, lvalue, ind, from string, element, widened bool) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	kind := tableScalarKind(f)
	packing, bits, base, baseHi, qmin, qdelta, qcount := from+".packing", from+".value_bits", from+".base_lo", from+".base_hi", from+".qmin", from+".qdelta", from+".qcount"
	if element {
		packing, bits, base, baseHi, qmin, qdelta, qcount = from+".elem_packing", from+".elem_value_bits", from+".elem_base_lo", from+".elem_base_hi", from+".elem_qmin", from+".elem_qdelta", from+".elem_qcount"
	}
	// THE WIDTH IS RESOLVED, not derived here: the entry carries what the
	// kind, the packing and the announced bits together settled, computed
	// once at AnnounceRead (docs/SPEC-TABLES.md §3.3)
	width := bits
	if widened && kind == tkF64 {
		// f32 WIDENED INTO f64 (§3.3, §4): the payload rides at the SENDER's
		// f32 shape, its quantization or its thirty-two raw bits, and every
		// float32 value is exactly representable, and a float64 field's
		// declared range clamps nothing on this wire, exactly as a payload at
		// sixty-four bits.
		g.pf("%s{\n", ind)
		g.pf("%s    if ( %s == 2 )\n%s    {\n", ind, packing, ind)
		g.pf("%s        uint64_t index%s = 0;\n", ind, sfx)
		g.pf("%s        if ( !r.get( index%s, %s ) ) { report->malformed = true; return false; }\n", ind, sfx, bits)
		g.pf("%s        if ( index%s > (uint64_t) %s ) { report->malformed = true; return false; } // above the step count\n", ind, sfx, qcount)
		g.pf("%s        %s = (double) TableMessageDequantize( (uint32_t) index%s, %s, %s, %s );\n%s    }\n", ind, lvalue, sfx, qmin, qdelta, qcount, ind)
		g.pf("%s    else\n%s    {\n", ind, ind)
		g.pf("%s        uint64_t raw%s = 0;\n", ind, sfx)
		g.pf("%s        if ( !r.get( raw%s, 32 ) ) { report->malformed = true; return false; }\n", ind, sfx)
		// a NaN's PAYLOAD IS DATA and rides on the bits: the hardware
		// float32 to float64 conversion would set the quiet bit (§4)
		g.pf("%s        %s = TableWidenF32( (uint32_t) raw%s );\n%s    }\n", ind, lvalue, sfx, ind)
		g.pf("%s}\n", ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s{ uint64_t raw%s = 0; if ( !r.get( raw%s, 1 ) ) { report->malformed = true; return false; } %s = raw%s != 0; }\n", ind, sfx, sfx, lvalue, sfx)
		return
	case tkF64:
		g.pf("%s{ uint64_t raw%s = 0; if ( !r.get( raw%s, 64 ) ) { report->malformed = true; return false; } %s = table_bits_to_double( raw%s ); }\n", ind, sfx, sfx, lvalue, sfx)
		return
	case tkF32:
		g.pf("%s{\n%s    float decoded_f%s = 0.0f;\n", ind, ind, sfx)
		g.pf("%s    if ( %s == 2 )\n%s    {\n", ind, packing, ind)
		g.pf("%s        uint64_t index%s = 0;\n", ind, sfx)
		g.pf("%s        if ( !r.get( index%s, %s ) ) { report->malformed = true; return false; }\n", ind, sfx, bits)
		// AN INDEX ABOVE `count` IS REJECTED, as the packet wire rejects it,
		// and is never reconstructed and clamped (§3.3, SPEC.md §4.3): the
		// width spells one whenever `count` is not one less than a power of
		// two, ten bits spelling 1023 over a count of 1000
		g.pf("%s        if ( index%s > (uint64_t) %s ) { report->malformed = true; return false; } // above the step count: the packet wire's own refusal\n", ind, sfx, qcount)
		g.pf("%s        decoded_f%s = TableMessageDequantize( (uint32_t) index%s, %s, %s, %s ); // SPEC.md §4.3's rule, in float32\n%s    }\n", ind, sfx, sfx, qmin, qdelta, qcount, ind)
		g.pf("%s    else\n%s    {\n", ind, ind)
		g.pf("%s        uint64_t raw%s = 0;\n", ind, sfx)
		g.pf("%s        if ( !r.get( raw%s, 32 ) ) { report->malformed = true; return false; }\n", ind, sfx)
		g.pf("%s        decoded_f%s = table_bits_to_float( (uint32_t) raw%s );\n%s    }\n", ind, sfx, sfx, ind)
		if f.HasFloatRange {
			g.pf("%s    if ( decoded_f%s < %s ) { decoded_f%s = %s; report->clamped++; }\n", ind, sfx, formatFloat(f.FMin, true), sfx, formatFloat(f.FMin, true))
			g.pf("%s    else if ( decoded_f%s > %s ) { decoded_f%s = %s; report->clamped++; }\n", ind, sfx, formatFloat(f.FMax, true), sfx, formatFloat(f.FMax, true))
		}
		g.pf("%s    %s = decoded_f%s;\n%s}\n", ind, lvalue, sfx, ind)
		return
	}
	signed := ir.TableKindSigned(kind)
	bytesWide := tableKindWidth(kind)
	decoded := "decoded_v" + sfx
	g.pf("%s{\n", ind)
	g.pf("%s    const int64_t width%s = %s;\n", ind, sfx, width)
	if bytesWide == 16 && widened {
		// AN INTEGER KIND WIDENED INTO A 128-BIT DECLARATION (§3.3, §4): the
		// payload rides at the SENDER's width, which is sixty-four bits or
		// fewer, so it is read there, extended, and only then met by this
		// reader's own bound, exactly as a payload at 128 bits is.
		storage := "serialize::uint128_t"
		if signed {
			storage = "serialize::int128_t"
		}
		g.pf("%s    uint64_t raw%s = 0;\n", ind, sfx)
		g.pf("%s    if ( width%s < 0 || width%s > 64 || !r.get( raw%s, width%s ) ) { report->malformed = true; return false; }\n", ind, sfx, sfx, sfx, sfx)
		if signed {
			g.pf("%s    if ( %s != 1 && width%s > 0 && width%s < 64 )\n%s    {\n", ind, packing, sfx, sfx, ind)
			g.pf("%s        const uint64_t sign%s = uint64_t(1) << ( width%s - 1 );\n", ind, sfx, sfx)
			g.pf("%s        if ( ( raw%s & sign%s ) != 0 ) { raw%s = raw%s | ~( ( uint64_t(1) << width%s ) - 1 ); }\n", ind, sfx, sfx, sfx, sfx, sfx)
			g.pf("%s    }\n", ind)
			g.pf("%s    serialize::uint128_t raw_v%s = ( serialize::uint128_t( (int64_t) raw%s < 0 ? ~uint64_t(0) : uint64_t(0) ) << 64 ) | serialize::uint128_t( raw%s );\n", ind, sfx, sfx, sfx)
		} else {
			g.pf("%s    serialize::uint128_t raw_v%s = serialize::uint128_t( raw%s );\n", ind, sfx, sfx)
		}
		g.pf("%s    if ( %s == 1 ) { raw_v%s = raw_v%s + ( ( serialize::uint128_t( (uint64_t) %s ) << 64 ) | serialize::uint128_t( (uint64_t) %s ) ); }\n", ind, packing, sfx, sfx, baseHi, base)
		g.pf("%s    %s %s = %s( raw_v%s );\n", ind, storage, decoded, storage, sfx)
		g.emitMessageClamp(f, signed, bytesWide, decoded, ind+"    ")
		g.pf("%s    %s = %s;\n%s}\n", ind, lvalue, decoded, ind)
		return
	}
	if bytesWide == 16 {
		// A 128-BIT KIND at the width the SENDER's shape states: the low half
		// first and the high half where the width reaches it, then the base
		// added at 128 bits, which is the one arithmetic measure, save and
		// read share (§3.3)
		storage := "serialize::uint128_t"
		if signed {
			storage = "serialize::int128_t"
		}
		g.pf("%s    uint64_t lo_v%s = 0, hi_v%s = 0;\n", ind, sfx, sfx)
		g.pf("%s    if ( width%s < 0 || !r.get( lo_v%s, width%s < 64 ? width%s : 64 ) || ( width%s > 64 && !r.get( hi_v%s, width%s - 64 ) ) ) { report->malformed = true; return false; }\n", ind, sfx, sfx, sfx, sfx, sfx, sfx, sfx)
		g.pf("%s    serialize::uint128_t raw_v%s = ( serialize::uint128_t( hi_v%s ) << 64 ) | serialize::uint128_t( lo_v%s );\n", ind, sfx, sfx, sfx)
		g.pf("%s    if ( %s == 1 ) { raw_v%s = raw_v%s + ( ( serialize::uint128_t( (uint64_t) %s ) << 64 ) | serialize::uint128_t( (uint64_t) %s ) ); }\n", ind, packing, sfx, sfx, baseHi, base)
		g.pf("%s    %s %s = %s( raw_v%s );\n", ind, storage, decoded, storage, sfx)
		g.emitMessageClamp(f, signed, bytesWide, decoded, ind+"    ")
		g.pf("%s    %s = %s;\n%s}\n", ind, lvalue, decoded, ind)
		return
	}
	storage := fmt.Sprintf("uint%d_t", bytesWide*8)
	if signed {
		storage = fmt.Sprintf("int%d_t", bytesWide*8)
	}
	g.pf("%s    uint64_t raw%s = 0;\n", ind, sfx)
	g.pf("%s    if ( width%s < 0 || !r.get( raw%s, width%s ) ) { report->malformed = true; return false; }\n", ind, sfx, sfx, sfx)
	g.pf("%s    int64_t decoded_wide%s = (int64_t) raw%s;\n", ind, sfx, sfx)
	g.pf("%s    if ( %s == 1 ) { decoded_wide%s = (int64_t) ( raw%s + (uint64_t) %s ); }\n", ind, packing, sfx, sfx, base)
	if signed {
		g.pf("%s    else if ( width%s > 0 && width%s < 64 )\n%s    {\n", ind, sfx, sfx, ind)
		g.pf("%s        const uint64_t sign%s = uint64_t(1) << ( width%s - 1 );\n", ind, sfx, sfx)
		g.pf("%s        if ( ( raw%s & sign%s ) != 0 ) { decoded_wide%s = (int64_t) ( raw%s | ~( ( uint64_t(1) << width%s ) - 1 ) ); }\n", ind, sfx, sfx, sfx, sfx, sfx)
		g.pf("%s    }\n", ind)
	}
	// THE BOUND APPLIES WHILE THE VALUE IS STILL WIDE, and the narrowing
	// comes after: a reconstructed value past the storage or the declared
	// range clamps to the reader's own bound and never wraps into it (§3.3)
	g.emitMessageClampWide(f, signed, bytesWide, "decoded_wide"+sfx, ind+"    ")
	g.pf("%s    %s %s = (%s) decoded_wide%s;\n", ind, storage, decoded, storage, sfx)
	g.pf("%s    %s = %s;\n%s}\n", ind, lvalue, decoded, ind)
}

// emitMessageClampWide is §4's clamp taken on the sixty-four bit value a
// narrower kind was reconstructed into: the declared range where there is
// one, intersected with the kind's own storage range, on the signed or the
// unsigned scale the kind names, and a `bits(N)` width clamp beside it.
func (g *tableGen) emitMessageClampWide(f *ir.Field, signed bool, width int, decoded, ind string) {
	lo, hi := tableStorageRange(signed, width*8)
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		if rlo.Cmp(lo) > 0 {
			lo = rlo
		}
		if rhi.Cmp(hi) < 0 {
			hi = rhi
		}
	}
	slo, shi := tableStorageRange(signed, 64)
	if lo.Cmp(slo) > 0 {
		if signed {
			g.pf("%sif ( %s < %s ) { %s = %s; report->clamped++; }\n", ind, decoded, tableIntLit(lo, true, 8), decoded, tableIntLit(lo, true, 8))
		} else {
			g.pf("%sif ( (uint64_t) %s < %s ) { %s = (int64_t) %s; report->clamped++; }\n", ind, decoded, tableIntLit(lo, false, 8), decoded, tableIntLit(lo, false, 8))
		}
	}
	if hi.Cmp(shi) < 0 {
		if signed {
			g.pf("%sif ( %s > %s ) { %s = %s; report->clamped++; }\n", ind, decoded, tableIntLit(hi, true, 8), decoded, tableIntLit(hi, true, 8))
		} else {
			g.pf("%sif ( (uint64_t) %s > %s ) { %s = (int64_t) %s; report->clamped++; }\n", ind, decoded, tableIntLit(hi, false, 8), decoded, tableIntLit(hi, false, 8))
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		maxv := (uint64(1) << f.Type.Width) - 1
		g.pf("%sif ( (uint64_t) %s > %dull ) { %s = (int64_t) %dull; report->clamped++; } // bits(%d) width clamp\n", ind, decoded, maxv, decoded, maxv, f.Type.Width)
	}
}

// emitMessageClamp is §4's clamp, the file form's own text: the declared range
// on the RAW scale, and a `bits(N)` width clamp beside it.
func (g *tableGen) emitMessageClamp(f *ir.Field, signed bool, width int, decoded, ind string) {
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		low, high := tableClampEnds(f, width)
		if low {
			lo := tableIntLit(rlo, signed, width)
			g.pf("%sif ( %s < %s ) { %s = %s; report->clamped++; }\n", ind, decoded, lo, decoded, lo)
		}
		if high {
			hi := tableIntLit(rhi, signed, width)
			lead := "if"
			if low {
				lead = "else if"
			}
			g.pf("%s%s ( %s > %s ) { %s = %s; report->clamped++; }\n", ind, lead, decoded, hi, decoded, hi)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		maxv := (uint64(1) << f.Type.Width) - 1
		g.pf("%sif ( %s > %dull ) { %s = %dull; report->clamped++; } // bits(%d) width clamp\n", ind, decoded, maxv, decoded, maxv, f.Type.Width)
	}
}

// ---- the batch's three verbs, over a FIXED root ----

// emitMessageEntries emits one FIXED ROOT's message surface (§3.3). The verbs
// are PLURAL because the primitive is a batch and a single message is the
// batch of one: a surface with a singular verb beside them would let a caller
// write one body a call and never learn that the batch is where the bandwidth
// is. A VARIABLE root's three ride with its builder surface, over a region.
func (g *tableGen) emitMessageEntries(st *ir.Struct) {
	if st.IsMapEntry() || g.isVar(st.Name) {
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
	g.pf("//\n")
	g.pf("// M ABOVE 256 IS A REFUSAL BY NAME on both verbs: -1, with the report's\n")
	g.pf("// verdict set and the reason batch_too_large on it, and nothing written. The\n")
	g.pf("// refusal is learned at MEASURE time, before a buffer is allocated, and a\n")
	g.pf("// caller with more bodies calls again.\n")
	g.pf("inline int64_t %sMeasureMessages( const %s * values, int64_t count, TableReport * report )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 1 ) { return -1; }\n")
	g.pf("    if ( count > kTableMessageBatchMax ) { TableMessageRefuseBatch( report ); return -1; }\n")
	g.pf("    int64_t bits = 8; // the body count, a ranged integer over [1, 256]\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        const int64_t body = %sMeasureMessageBody( bits, values[i] );\n", n)
	g.pf("        if ( body < 0 ) { return -1; }\n")
	g.pf("        bits += body;\n    }\n")
	g.pf("    return 1 + ( bits + 7 ) / 8; // the form byte, then the stream padded to a byte\n}\n\n")

	g.pf("inline int64_t %sSaveMessages( const %s * values, int64_t count, uint8_t * buffer, int64_t capacity, TableReport * report )\n{\n", n, n)
	g.pf("    if ( values == NULL || count < 1 ) { return -1; }\n")
	g.pf("    if ( count > kTableMessageBatchMax ) { TableMessageRefuseBatch( report ); return -1; }\n")
	g.pf("    TableMessageBatch batch;\n")
	g.pf("    if ( !TableMessageBatchBegin( batch, buffer, capacity, count ) ) { return -1; }\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( !%sSaveMessageBody( batch.w, values[i] ) ) { return -1; }\n", n)
	g.pf("        batch.written++;\n    }\n")
	g.pf("    return TableMessageBatchEnd( batch ); // == %sMeasureMessages( values, count, report )\n}\n\n", n)

	g.pf("// A form 2 wire with NO VOCABULARY for the announcement is REFUSED BY NAME:\n")
	g.pf("// nothing is decoded, the reader says it holds no vocabulary, no counter\n")
	g.pf("// moves and malformed does not fire. A reader does not fall back to the file\n")
	g.pf("// form on its own and does not guess a vocabulary, because a guessed one\n")
	g.pf("// decodes a body under the wrong names in silence.\n")
	g.pf("//\n")
	g.pf("// `count` is IN and OUT: in, the storage the caller has room for, and out,\n")
	g.pf("// what it got. M ABOVE THE CALLER'S CAPACITY IS A REFUSAL BY NAME,\n")
	g.pf("// batch_too_large, found from the count before a body is decoded, and count\n")
	g.pf("// comes back holding the WIRE's M, so the caller calls again with storage at\n")
	g.pf("// or above it and never parses a byte itself. DAMAGE INSIDE BODY k DELIVERS\n")
	g.pf("// BODIES 1 TO k - 1: count says k - 1, one malformed counts, and the storage\n")
	g.pf("// after it is not a body.\n")
	g.pf("inline bool %sLoadMessages( %s * values, int64_t * count, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * to = report != NULL ? report : &ignored;\n")
	g.pf("    if ( values == NULL || count == NULL ) { to->malformed = true; return false; }\n")
	g.pf("    const int64_t capacity = *count;\n")
	g.pf("    *count = 0;\n")
	g.pf("    TableMessageBatchReader br;\n")
	g.pf("    const int64_t bodies = TableMessageBatchOpen( br, vocabulary, buffer, bytes, to );\n")
	g.pf("    if ( bodies < 0 ) { return false; }\n")
	g.pf("    if ( bodies > capacity ) { *count = bodies; TableMessageRefuseBatch( to ); return false; }\n")
	g.pf("    for ( int64_t i = 0; i < bodies; i++ )\n    {\n")
	g.pf("        if ( !%sLoadMessageBody( br.r, vocabulary, to, 0, values[i] ) ) { *count = i; return false; } // a fixed root numbers no node: no index width\n", n)
	g.pf("        br.remaining--;\n    }\n")
	g.pf("    *count = bodies;\n")
	g.pf("    return TableMessageBatchClose( br );\n}\n\n")
}

// emitMessageReadList decodes one UNBOUNDED ARRAY on the message wire
// (docs/SPEC-TABLES.md §2.9, §3.3): the count at the thirty-two bits the data
// decides, then the elements into slots the fill carves from the node's own
// extent, in index order. There is no bound, so `clamped` cannot fire on the
// count, and a count above the int32 storage cap is the fill's refusal.
func (g *tableGen) emitMessageReadList(f *ir.Field, ind string) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	g.pf("%s{\n", ind)
	g.pf("%s    uint64_t n%s = 0;\n", ind, sfx)
	g.pf("%s    if ( !r.get( n%s, TableBitsRequired( entry.min, entry.max ) ) ) { report->malformed = true; return false; }\n", ind, sfx)
	g.pf("%s    n%s += (uint64_t) entry.min;\n", ind, sfx)
	if listElementWireKind(f) == tkU8 {
		g.pf("%s    if ( !r.align() ) { report->malformed = true; return false; } // an array of kind 6 aligns before its elements\n", ind)
	}
	g.pf("%s    TableListFill<%s> fill%s = TableListFillBegin( nodes, value.%s, n%s );\n", ind, g.listTypeArg(f), sfx, f.Name, sfx)
	g.pf("%s    if ( fill%s.refused ) { nodes.refused = true; return false; }\n", ind, sfx)
	g.pf("%s    if ( !fill%s.ok ) { report->malformed = true; return false; } // the measure and the load disagree\n", ind, sfx)
	g.pf("%s    for ( uint64_t %s = 0; %s < n%s; %s++ )\n%s    {\n", ind, idx, idx, sfx, idx, ind)
	inner := ind + "        "
	g.pf("%s%s * slot%s = TableListFillNext( fill%s );\n", inner, g.listElementType(f), sfx, sfx)
	g.pf("%sif ( slot%s == NULL ) { report->malformed = true; return false; } // the arena could not carve\n", inner, sfx)
	elem := "( *slot" + sfx + " )"
	switch {
	case f.Type.Pointer:
		g.emitMessageReadIndex(f, elem, inner)
	case tableScalarKind(f) == tkTable:
		g.pf("%sif ( !%s ) { return false; }\n", inner, g.msgLoadCall(f.Type.Name, "r", elem))
	case tableScalarKind(f) == tkEnum:
		g.emitMessageReadEnum(f, elem, inner)
	case tableScalarKind(f) == tkUnion:
		g.emitMessageReadUnion(f, elem, inner)
	default:
		g.emitMessageReadScalarFromEntry(f, elem, inner, "entry", true, false)
	}
	g.pf("%s    }\n", ind)
	g.pf("%s    TableListFillEnd( fill%s );\n", ind, sfx)
	g.pf("%s}\n", ind)
}
