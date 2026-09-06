package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WIDENING RULE (docs/SPEC-TABLES.md §4), emitted at every site this wire
// compares kinds: a field's own kind, an arm's, an array's element kind and a
// map's key kind. An integer kind read into a WIDER integer kind of the same
// signedness, and f32 read into f64, decodes EXACTLY at the wire kind's width
// and counts `widened` once per field, once per map. Every other pair stays a
// kind mismatch.
//
// ZERO COST ON THE MATCHING PATH, by construction: the branch below lives
// inside the kind-mismatch branch a reader already takes, which today skips
// the payload. A payload under the declared kind never reaches it.

// tableWidenRuntime is the runtime half, emitted after TableReader in every
// unit: the ladder predicate and the two readers that fetch a payload at ITS
// kind's width and extend it to sixty-four bits.
const tableWidenRuntime = `
// WIDENING (docs/SPEC-TABLES.md §4): a payload under a kind BELOW the reader's
// on the same ladder decodes exactly. The signed ladder is kinds 2, 3, 4, 5,
// 18, the unsigned one 6, 7, 8, 9, 19, and 10 into 11 is the float rung. Every
// other pair is a kind mismatch. The declared kind is a constant at every call
// site, so this folds to one or two comparisons on the mismatch path and to
// nothing on the matching one.
inline bool TableKindWidens( uint8_t kind, uint8_t declared )
{
    switch ( declared )
    {
        case 3: case 4: case 5: return kind >= 2 && kind < declared;
        case 18: return kind >= 2 && kind <= 5;
        case 7: case 8: case 9: return kind >= 6 && kind < declared;
        case 19: return kind >= 6 && kind <= 9;
        case 11: return kind == 10;
    }
    return false;
}

// a fixed-width kind's payload width, for the one place the width is a
// runtime fact: an arm whose kind byte the reader widens, whose L must be the
// wire kind's own width (§3)
inline int64_t TableKindWidth( uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: return 1;
        case 3: case 7: case 21: case 26: return 2;
        case 4: case 8: case 10: case 22: case 27: return 4;
        case 5: case 9: case 11: case 23: case 28: return 8;
        case 18: case 19: case 24: case 29: return 16;
    }
    return 0;
}

// the payload of a kind on the SIGNED ladder (2 to 5), sign-extended to
// sixty-four bits; false = the body cannot cover it, which is framing damage
inline bool TableReadSignedAt( TableReader & r, uint8_t kind, int64_t & out )
{
    switch ( kind )
    {
        case 2: if ( !r.has( 1 ) ) { return false; } out = (int8_t) r.get8(); return true;
        case 3: if ( !r.has( 2 ) ) { return false; } out = (int16_t) r.get16(); return true;
        case 4: if ( !r.has( 4 ) ) { return false; } out = (int32_t) r.get32(); return true;
        default: if ( !r.has( 8 ) ) { return false; } out = (int64_t) r.get64(); return true;
    }
}

// the payload of a kind on the UNSIGNED ladder (6 to 9), zero-extended
inline bool TableReadUnsignedAt( TableReader & r, uint8_t kind, uint64_t & out )
{
    switch ( kind )
    {
        case 6: if ( !r.has( 1 ) ) { return false; } out = r.get8(); return true;
        case 7: if ( !r.has( 2 ) ) { return false; } out = r.get16(); return true;
        case 8: if ( !r.has( 4 ) ) { return false; } out = r.get32(); return true;
        default: if ( !r.has( 8 ) ) { return false; } out = r.get64(); return true;
    }
}

// f32 into f64, exact: a NaN's payload is data and rides on the bits, since
// the hardware conversion would set the quiet bit (§4)
inline double TableWidenF32( uint32_t bits )
{
    if ( ( bits & 0x7F800000u ) == 0x7F800000u && ( bits & 0x007FFFFFu ) != 0 )
    {
        const uint64_t sign = (uint64_t) ( bits >> 31 ) << 63;
        const uint64_t payload = (uint64_t) ( bits & 0x007FFFFFu ) << 29;
        const uint64_t nan_bits = sign | 0x7FF0000000000000ull | payload;
        double d; memcpy( &d, &nan_bits, 8 ); return d;
    }
    float f; memcpy( &f, &bits, 4 ); return (double) f;
}
`

// widenable reports whether a declared kind sits ABOVE the bottom of a ladder,
// so a reader of it has kinds to widen from and emits the branch.
func widenable(kind int) bool {
	switch kind {
	case tkI16, tkI32, tkI64, ir.TableKindI128, tkU16, tkU32, tkU64, ir.TableKindU128, tkF64:
		return true
	}
	return false
}

// plainScalar reports whether a field is a bare scalar on the wire: no array,
// no pointer, no map, no keyed body, no blob, and a kind that is a number.
func plainScalar(f *ir.Field) bool {
	if f.Array != ir.ArrayNone || f.Type.Pointer || f.IsMap() || f.KeyEnum != "" || f.Type.Blob() {
		return false
	}
	switch f.Type.Kind {
	case ir.TBytes, ir.TString:
		return false
	}
	return widenable(tableScalarKind(f))
}

// widenableElement reports whether an array field's ELEMENTS are widenable
// scalars: the element kind sits above a ladder's bottom and the element is a
// number rather than a table, a union, an enum, a pointer or a byte.
func widenableElement(f *ir.Field) bool {
	if f.Type.Pointer || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
		return false
	}
	if f.Type.Ref != nil {
		return false
	}
	return widenable(tableScalarKind(f))
}

// emitWidenedScalar emits the decode of one payload under `kindExpr`, a kind
// the declared kind widens: fetched at the wire kind's width, extended, then
// clamped by the reader's own declaration exactly as a payload at the
// declared width is, and stored. onTrunc runs where the body cannot cover the
// payload, which is framing damage on the body carrying it.
func (g *tableGen) emitWidenedScalar(f *ir.Field, declared int, kindExpr, lvalue, ind, rdr, onTrunc string) {
	if declared == tkF64 {
		g.pf("%sif ( !%s.has( 4 ) ) { %s }\n", ind, rdr, onTrunc)
		g.pf("%s%s = TableWidenF32( %s.get32() );\n", ind, lvalue, rdr)
		return
	}
	width := tableKindWidth(declared)
	signed := ir.TableKindSigned(declared)
	storage := fmt.Sprintf("uint%d_t", width*8)
	if signed {
		storage = fmt.Sprintf("int%d_t", width*8)
	}
	if width == 16 {
		storage = "serialize::uint128_t"
		if signed {
			storage = "serialize::int128_t"
		}
	}
	if signed {
		g.pf("%sint64_t widened_v = 0;\n", ind)
		g.pf("%sif ( !TableReadSignedAt( %s, %s, widened_v ) ) { %s }\n", ind, rdr, kindExpr, onTrunc)
	} else {
		g.pf("%suint64_t widened_v = 0;\n", ind)
		g.pf("%sif ( !TableReadUnsignedAt( %s, %s, widened_v ) ) { %s }\n", ind, rdr, kindExpr, onTrunc)
	}
	if width == 16 {
		// the two lanes of serialize's pair, the high one the sign's
		if signed {
			g.pf("%s%s decoded_v = %s( ( serialize::uint128_t( widened_v < 0 ? ~0ull : 0ull ) << 64 ) | serialize::uint128_t( (uint64_t) widened_v ) );\n", ind, storage, storage)
		} else {
			g.pf("%s%s decoded_v = %s( widened_v );\n", ind, storage, storage)
		}
	} else {
		g.pf("%s%s decoded_v = (%s) widened_v;\n", ind, storage, storage)
	}
	g.emitScalarClamps(f, width, signed, ind)
	g.pf("%s%s = decoded_v;\n", ind, lvalue)
}

// emitScalarClamps emits the reader's own clamps over `decoded_v` at the
// declared storage: the declared range on the raw scale, and a bits(N)
// field's width (docs/SPEC-TABLES.md §4).
func (g *tableGen) emitScalarClamps(f *ir.Field, width int, signed bool, ind string) {
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		low, high := tableClampEnds(f, width)
		if low {
			lo := tableIntLit(rlo, signed, width)
			g.pf("%sif ( decoded_v < %s ) { decoded_v = %s; r.report->clamped++; }\n", ind, lo, lo)
		}
		if high {
			hi := tableIntLit(rhi, signed, width)
			lead := "if"
			if low {
				lead = "else if"
			}
			g.pf("%s%s ( decoded_v > %s ) { decoded_v = %s; r.report->clamped++; }\n", ind, lead, hi, hi)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		maxv := (uint64(1) << f.Type.Width) - 1
		g.pf("%sif ( decoded_v > %dull ) { decoded_v = %dull; r.report->clamped++; } // bits(%d) width clamp\n", ind, maxv, maxv, f.Type.Width)
	}
}

// emitArmWiden emits the WIDENED branch at an arm's kind check (§4): the
// arm's kind byte is on the ladder below the declared arm's, so its payload
// is decoded rather than skipped. The arm's L must be the wire kind's own
// width, else it is that arm's own framing damage (§3). Emitted only for a
// plain scalar arm, inside the mismatch branch the caller opened.
func (g *tableGen) emitArmWiden(v ir.UnionVariant, base, armKindExpr, rdr, tag, selected, none, ind string) {
	if v.Void() || v.Body() || !plainScalar(v.F) {
		return
	}
	declared := armWireKind(v)
	g.pf("%sif ( TableKindWidens( %s, %d ) )\n%s{\n", ind, armKindExpr, declared, ind)
	g.pf("%s    // WIDENED AT AN ARM (§4): the payload decodes at its own width; an L\n", ind)
	g.pf("%s    // that is not that width is the arm's own framing damage (§3)\n", ind)
	g.pf("%s    if ( %s.size != TableKindWidth( %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, armKindExpr, tag, none)
	g.establishArm(v, base, ind+"    ")
	g.emitWidenedScalar(v.F, declared, armKindExpr, armValue(base, v), ind+"    ", rdr,
		fmt.Sprintf("%s = %s; r.report->malformed = true; break;", tag, none))
	g.pf("%s    %s = %s;\n", ind, tag, selected)
	g.pf("%s    r.report->widened++;\n", ind)
	g.pf("%s    break;\n%s}\n", ind, ind)
}

// emitWidenedElements emits the element loop of a positional array whose
// header carried a kind the declared element kind widens (§4): ONE `widened`
// for the field, every element decoded at the wire kind's width, the count
// clamped and bounded exactly as the matching loop bounds it. `elems` is the
// element lvalue with `%s` for the index, `count` the companion to set or "".
func (g *tableGen) emitWidenedElements(f *ir.Field, declared int, kindExpr, elems, count, keepExpr, ind, rdr string) {
	g.pf("%sr.report->widened++;\n", ind)
	g.pf("%suint64_t widened_decoded = 0;\n", ind)
	g.pf("%sfor ( uint64_t widened_i = 0; widened_i < %s; widened_i++ )\n%s{\n", ind, keepExpr, ind)
	g.emitWidenedScalar(f, declared, kindExpr, fmt.Sprintf(elems, "(int32_t) widened_i"), ind+"    ", rdr, "r.report->malformed = true; break;")
	g.pf("%s    widened_decoded = widened_i + 1;\n", ind)
	g.pf("%s}\n", ind)
	if count != "" {
		g.pf("%s%s = (int32_t) widened_decoded;\n", ind, count)
	} else {
		g.pf("%s(void) widened_decoded;\n", ind)
	}
}

// emitKeyedTriples emits the loop over an enum-keyed body's triples
// (docs/SPEC-TABLES.md §3.2): each placed by its key reference, a key this
// reader cannot name skipped by its length and counted unknown. With
// `widened` the element decodes at the kind the header carried, which the
// declared kind widens (§4); without it, at the declared width.
func (g *tableGen) emitKeyedTriples(f *ir.Field, kind int, ind string, widened bool) {
	g.pf("%s    TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report, r.ids );\n", ind)
	g.pf("%s    for ( uint64_t i = 0; i < count; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t key_ref = 0;\n", ind)
	g.pf("%s        if ( !sub.getleb( key_ref ) ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        if ( key_ref == 0 )\n%s        {\n", ind, ind)
	g.pf("%s            // None is the NULL KEY, and a stored key reference of 0 names\n", ind)
	g.pf("%s            // no id at all, so a body carrying one is DAMAGED rather than\n", ind)
	g.pf("%s            // merely foreign: the read stops this body, keeps what it\n", ind)
	g.pf("%s            // decoded, and the parent reads on past the length (§3.2, §4).\n", ind)
	g.pf("%s            r.report->malformed = true;\n%s            break;\n%s        }\n", ind, ind, ind)
	g.pf("%s        if ( key_ref > (uint64_t) r.ids->count ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        uint64_t elem_len = 0;\n", ind)
	g.pf("%s        if ( !sub.getleb( elem_len ) || !sub.room( elem_len ) ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        %s slot = %s::None;\n", ind, f.KeyEnum, f.KeyEnum)
	g.pf("%s        if ( !TableEnumValue( r.ids->at( key_ref ), slot ) )\n%s        {\n", ind, ind)
	g.pf("%s            r.report->unknown++; // a slot this reader cannot name\n", ind)
	g.pf("%s            sub.offset += (int64_t) elem_len;\n%s            continue;\n%s        }\n", ind, ind, ind)
	g.pf("%s        {\n%s            TableReader elem( sub.buffer + sub.offset, (int64_t) elem_len, r.report, r.ids );\n", ind, ind)
	// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
	slot := g.keyedSlots("value.", f) + "[int32_t( slot ) - 1]"
	onTrunc := "r.report->malformed = true; sub.offset += (int64_t) elem_len; continue;"
	switch {
	case widened:
		g.emitWidenedScalar(f, kind, "elem_kind", slot, ind+"            ", "elem", onTrunc)
	case kind == tkTable:
		g.inStep("int32_t( slot ) - 1", func() {
			g.pf("%s            %s;\n", ind, g.loadCall(f, f.Type.Name, "elem", slot))
		})
	case kind == tkEnum:
		g.emitEnumRefLoad(f, slot, ind+"            ", "elem", onTrunc)
	default:
		g.emitTableReadScalarFrom(f, kind, slot, ind+"            ", "elem", onTrunc)
	}
	g.pf("%s        }\n", ind)
	g.pf("%s        sub.offset += (int64_t) elem_len;\n", ind)
	g.pf("%s    }\n", ind)
}

// tableRefuseReasonEnum is the ONE refusal vocabulary (docs/SPEC-TABLES.md
// §6.5, §7, §19.2): a cook's Open and a block's BlockOpen answer a value of it
// beside their null, and LoadMeasure's -1 answers the same way. §7's clauses
// come first in the order the check runs them, then the block's own clause,
// then the measure's five. No call returns a value belonging to the other's
// clauses. It is a NATIVE enum, never a schema-language one, and its values
// are written on the refusal path only.
const tableRefuseReasonEnum = `

// WHY A FILE WAS REFUSED, by name (docs/SPEC-TABLES.md §6.5, §7, §19.2): the
// one vocabulary a cook's Open, a block's BlockOpen and a load measure's -1
// share, because a caller asking "why can I not have this file" is
// asking one question whichever call refused it. The FIRST failing clause names the
// reason, in the order §7 enumerates, so one file answers one value in every
// language. A refusal moves no counter, and a match writes nothing: the
// out-parameter is touched on the refusal path only.
//
// It is not the MESSAGE FORM's vocabulary (TableMessageReason, §3.3): a caller
// meeting one of these has been refused a FILE, by a header match or by a
// measure.
enum TableRefuseReason
{
    ok,                    // no clause failed: the only value beside a non-null root (§7)
    not_a_cook,            // the magic is neither this build's constant nor its byte reversal, or the byte-order word contradicts the magic
    foreign_order,         // the magic byte-reversed: a cook of the other byte order (§7.1)
    wrong_build_version,   // the build_version word is not this build's (§20)
    reserved_not_zero,     // a reserved header word is not zero (§7.1)
    bad_alignment,         // the alignment word is not a power of two, is below eight, is above sixty-four, or is not a multiple of the root's own alignof
    truncated,             // the part lengths against the caller's length, or a data part too short to hold the root
    unaligned_base,        // the pointer the caller passed is not aligned for the region: the caller's defect, not the file's
    bad_layout,            // BlockOpen (§19.2): a pitch, a count, an offset or an extent that disagrees with this build's or leaves the block
    unknown_form,          // at a MEASURE (§3, §6.5): a form byte this build does not carry, refused before any read
    count_over_length,     // an array or map count whose elements cannot fit the field's own L (§2.8, §2.9)
    count_over_extent_cap, // a count above the int32 extent cap (§2.2), which no region can hold whatever its size
    blob_over_size_cap,    // a blob whose length is past the derived-size cap (§3.1, §11)
    data_cycle             // a data cycle reached from a builder: the AUTHORING side's -1 (§3.1, §7.6)
};`
