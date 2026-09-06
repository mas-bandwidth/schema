package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6), so this file is the
// field emitters again with the framing an ARM has: the arm id names the arm
// and the arm's `L` frames it, and the payload under that length is exactly
// the bytes a FIELD of the arm's type puts after its own framing prefix (§3).
// Nothing here is a second wire — every case below is one row of §3's arm
// payload table.
//
// Every local these emitters declare carries a SUFFIX, because an arm nests:
// an arm of an array of unions holds elements that are themselves unions with
// arms, and a name reused one level in is a shadow the POSIX legs' -Wshadow
// refuses. The suffix is "" at the top level and grows one letter per level.
func (g *tableGen) emitArmStorage(v ir.UnionVariant) {
	if v.Void() {
		// A PAYLOAD-FREE ARM HAS NO STORAGE (SPEC §4.8): the tag value is the
		// whole of it, and no accessor is generated
		return
	}
	f := v.F
	switch {
	case f.Type.Blob():
		g.pf("        TableRef %s; // *%s — a byte buffer at its used size (§2.5)\n", v.Name, blobWord(f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.noteRef(f.Type.Name)
		g.pf("        TableRef %s; // *%s — a node index on the wire (§3.1)\n", v.Name, f.Type.Name)
	case f.Type.Kind == ir.TString:
		g.pf("        struct { char value[%d + 1]; int32_t value_length; } %s; // string(%d)\n", f.Type.Size, v.Name, f.Type.Size)
	case f.Type.Kind == ir.TWString:
		g.pf("        struct { char16_t value[%d + 1]; int32_t value_length; } %s; // wstring(%d), the used length in CODE UNITS\n", f.Type.Size, v.Name, f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		g.pf("        struct { uint8_t value[%d]; int32_t value_length; } %s; // bytes(%d)\n", f.Type.Size, v.Name, f.Type.Size)
	case f.Array == ir.ArrayCounted:
		g.noteRef(f.Type.Name)
		g.pf("        struct { %s value[%d]; int32_t value_count; } %s; // [..%d]%s\n",
			g.armElementType(v), f.ArrayBound, v.Name, f.ArrayBound, ir.TableTypeSpelling(f))
	case f.Array == ir.ArrayFixed:
		g.noteRef(f.Type.Name)
		g.pf("        %s %s[%d]; // [%d]%s\n", g.armElementType(v), v.Name, f.ArrayBound, f.ArrayBound, ir.TableTypeSpelling(f))
	default:
		g.noteRef(f.Type.Name)
		typ, _ := g.cppFieldType(f.Type)
		if f.Type.Width == 128 && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) {
			// SIXTEEN BYTES AT SIXTEEN on every compiler, as a field's is
			// (docs/SPEC-TABLES.md §7.2, §19.3)
			typ = "alignas( 16 ) " + typ
		}
		g.pf("        %s %s;\n", typ, v.Name)
	}
}

// armCompanioned reports whether an arm's storage needs a COMPANION beside
// its value — a string's or bytes' used length, a counted array's count. Such
// an arm rides as one member of an unnamed struct, because the pair must
// occupy one slot of the overlay and an anonymous struct is an extension the
// dialect does not spend (§2.6, §13.9).
func armCompanioned(v ir.UnionVariant) bool {
	f := v.F
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return false
	}
	return f.Array == ir.ArrayCounted || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes
}

// armValue is the arm's payload lvalue under a union expression.
func armValue(base string, v ir.UnionVariant) string {
	if armCompanioned(v) {
		return base + "." + v.Name + ".value"
	}
	return base + "." + v.Name
}

// armCount is the arm's companion lvalue: a counted array's count, a string's
// or bytes' used length.
func armCount(base string, v ir.UnionVariant) string {
	if v.F.Array == ir.ArrayCounted {
		return base + "." + v.Name + ".value_count"
	}
	return base + "." + v.Name + ".value_length"
}

// armBound is the declared extent an arm's payload is bounded by: an array's
// bound, a string's or bytes' capacity.
func armBound(v ir.UnionVariant) int64 {
	if v.F.Array != ir.ArrayNone {
		return v.F.ArrayBound
	}
	return v.F.Type.Size
}

// armElementType is the C++ element type of an ARRAY arm's storage.
func (g *tableGen) armElementType(v ir.UnionVariant) string {
	if v.F.Type.Pointer {
		return "TableRef"
	}
	typ, _ := g.cppFieldType(v.F.Type)
	return typ
}

// armWireKind is the kind byte an ARM HEADER carries (docs/SPEC-TABLES.md §3):
// kind 32 where the arm has NO PAYLOAD, and otherwise the kind a FIELD of the
// arm's type takes. It is what makes a retyped arm an ordinary kind mismatch
// instead of a value read under the wrong rule.
func armWireKind(v ir.UnionVariant) int {
	if v.Void() {
		return tkNoPayload
	}
	if v.Body() {
		return tkTable
	}
	f := v.F
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return tkNodeIndex
	}
	if f.Type.Kind == ir.TBytes {
		return tkArray
	}
	return ir.TableWireFieldKind(f)
}

// emitArmMeasure adds ONE ARM'S PAYLOAD length to `into` — the bytes under
// the arm's `L`, never the framing — and returns through `onBad` where the
// storage invariant or the wire identity refuses the value, exactly as the
// field paths do.
func (g *tableGen) emitArmMeasure(v ir.UnionVariant, base, into, ind, onBad, sfx string) {
	if v.Void() {
		g.pf("%s// a payload-free arm: the arm reference, kind 32 and a zero L are the whole of it (§2.6, §3)\n", ind)
		return
	}
	f := v.F
	kind := tableScalarKind(f)
	value, count := armValue(base, v), armCount(base, v)
	body := "arm_body" + sfx
	switch {
	case v.Body():
		g.noteRef(v.Type)
		g.pf("%s{\n%s    const int64_t %s = %s;\n", ind, ind, body, g.measureCall(g.unionField, v.Type, value))
		g.pf("%s    if ( %s < 0 ) { %s }\n", ind, body, onBad)
		g.pf("%s    %s += %s; // the arm's own table body (§3)\n%s}\n", ind, into, body, ind)
	case f.Type.Blob():
		blob, index := "arm_blob"+sfx, "arm_index"+sfx
		g.pf("%s{\n%s    const TableBlob * %s = TableBlobAt( ctx, %s );\n", ind, ind, blob, value)
		g.pf("%s    uint64_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, blob, blob, index, onBad)
		g.pf("%s    %s += TableLebBytes( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, into, index, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		t, pointee, index := f.Type.Name, "arm_pointee"+sfx, "arm_index"+sfx
		g.pf("%s{\n%s    const %s * %s = %sAt( ctx, %s );\n", ind, ind, t, pointee, t, value)
		g.pf("%s    uint64_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, pointee, pointee, index, onBad)
		g.pf("%s    %s += TableLebBytes( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, into, index, ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%s%s += %s; // the string's bytes under the arm's L\n", ind, into, count)
	case f.Type.Kind == ir.TWString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%s%s += (int64_t) %s * 2; // the code units under the arm's L, which is a BYTE length (§3)\n", ind, into, count)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%s%s += 1 + TableLebBytes( (uint64_t) %s ) + %s; // element kind 6, N, then the bytes\n", ind, into, count, count)
	case f.Array != ir.ArrayNone:
		n := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.ArrayBound, onBad)
			n = count
		}
		elemKind := kind
		if f.Type.Pointer {
			elemKind = tkNodeIndex
		}
		g.emitArrayBodyMeasure(f, elemKind, into, n, value+"[%s]", ind, onBad, sfx+"e")
	case kind == tkEnum:
		ref := "arm_variant" + sfx
		g.pf("%s{\n%s    uint64_t %s = 0;\n", ind, ind, ref)
		g.pf("%s    if ( !TableEnumRef( ids, %s, %s ) ) { %s } // no variant names this value\n", ind, value, ref, onBad)
		g.pf("%s    %s += TableLebBytes( %s ); // the variant's reference\n%s}\n", ind, into, ref, ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif ( %s.type == %sType::None ) { %s += 1; } // the inner union's None: L = 1 and that one zero byte (§3)\n", ind, value, un.Name, into)
		g.pf("%selse\n%s{\n", ind, ind)
		g.emitUnionPayloadMeasure(f, value, into, ind+"    ", sfx+"a")
		g.pf("%s}\n", ind)
	default:
		g.pf("%s%s += %d; // %s\n", ind, into, tableKindWidth(kind), ir.FieldTypeSpelling(f))
	}
}

// emitArmSave writes ONE ARM'S PAYLOAD — the bytes under the arm's `L`, which
// the caller frames.
func (g *tableGen) emitArmSave(v ir.UnionVariant, base, ind, onBad, sfx string) {
	if v.Void() {
		g.pf("%s// a payload-free arm writes nothing under its L (§2.6)\n", ind)
		return
	}
	f := v.F
	kind := tableScalarKind(f)
	value, count := armValue(base, v), armCount(base, v)
	index := "arm_index" + sfx
	switch {
	case v.Body():
		g.pf("%sif ( !%s ) { %s }\n", ind, g.saveCall(g.unionField, v.Type, value), onBad)
	case f.Type.Blob():
		blob := "arm_blob" + sfx
		g.pf("%s{\n%s    const TableBlob * %s = TableBlobAt( ctx, %s );\n", ind, ind, blob, value)
		g.pf("%s    uint64_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, blob, blob, index, onBad)
		g.pf("%s    w.putleb( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, index, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		t, pointee := f.Type.Name, "arm_pointee"+sfx
		g.pf("%s{\n%s    const %s * %s = %sAt( ctx, %s );\n", ind, ind, t, pointee, t, value)
		g.pf("%s    uint64_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, pointee, pointee, index, onBad)
		g.pf("%s    w.putleb( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, index, ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sw.raw( %s, %s );\n", ind, value, count)
	case f.Type.Kind == ir.TWString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sfor ( int32_t i%s = 0; i%s < %s; i%s++ ) { w.put16( (uint16_t) %s[i%s] ); } // two bytes each, little-endian\n", ind, sfx, sfx, count, sfx, value, sfx)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sw.put8( %d ); w.putleb( (uint64_t) %s ); // bytes ride as an array of u8 (§2.5)\n", ind, tkU8, count)
		g.pf("%sw.raw( %s, %s );\n", ind, value, count)
	case f.Array != ir.ArrayNone:
		n := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.ArrayBound, onBad)
			n = count
		}
		elemKind := kind
		if f.Type.Pointer {
			elemKind = tkNodeIndex
		}
		g.emitArrayBodyWrite(f, elemKind, n, value+"[%s]", ind, sfx+"e")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif ( %s.type == %sType::None ) { w.putleb( 0 ); } // the inner union's None (§3)\n", ind, value, un.Name)
		g.pf("%selse\n%s{\n", ind, ind)
		g.emitUnionPayloadSave(f, value, ind+"    ", onBad, sfx+"a")
		g.pf("%s}\n", ind)
	default:
		g.emitTableWriteElement(f, kind, value, ind, sfx)
	}
}

// armWireLength is the byte count an arm's `L` must equal, or 0 where the
// arm's payload is length-shaped or reference-shaped (docs/SPEC-TABLES.md §3's
// arm payload table). The arm's KIND BYTE already separates every pair of arm
// types, so what is left to check is the length: an arm whose `L` is not its
// kind's width is that arm's own framing damage.
func armWireLength(v ir.UnionVariant) int { return ir.ArmWireFixedWidth(v.F) }

// emitArmLoad reads ONE ARM'S PAYLOAD out of `rdr`, a reader bounded to the
// arm's `L`. The caller has already checked the arm's KIND BYTE; `tag` is the
// union's tag lvalue and `none` the tag value to put back where the LENGTH
// says the payload cannot be the arm's — that is framing damage, the union
// reads None, `malformed` counts, and the parent reads on past `L` (§3, §4).
func (g *tableGen) emitArmLoad(v ir.UnionVariant, base, ind, rdr, tag, none, sfx string) {
	if v.Void() {
		g.pf("%sif ( %s.size != 0 ) { %s = %s; r.report->malformed = true; break; } // a payload-free arm carries no payload (§2.6, §3)\n", ind, rdr, tag, none)
		return
	}
	f := v.F
	kind := tableScalarKind(f)
	value, count := armValue(base, v), armCount(base, v)
	if w := armWireLength(v); w > 0 {
		g.pf("%sif ( %s.size != %d ) { %s = %s; r.report->malformed = true; break; } // an L that is not the kind's width is that arm's own framing damage (§3)\n",
			ind, rdr, w, tag, none)
	}
	switch {
	case v.Body():
		g.pf("%s%s;\n", ind, g.loadCall(g.unionField, v.Type, rdr, value))
		// A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3): an arm whose
		// terminator is not the last byte of its `L` is framing damage — the
		// payload stops, the union reads None, and the enclosing body
		// continues past the arm by `L`.
		g.pf("%sif ( %s.offset != %s.size ) { %s = %s; r.report->malformed = true; break; }\n",
			ind, rdr, rdr, tag, none)
	case f.Type.Blob(), f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitNodeIndexLoad(f, value, ind, rdr,
			fmt.Sprintf("%s = %s; r.report->malformed = true; break;", tag, none), sfx, true)
	case f.Type.Kind == ir.TString:
		g.establishArm(v, base, ind)
		length, keep := "arm_len"+sfx, "arm_keep"+sfx
		g.pf("%s{\n%s    uint32_t %s = uint32_t( %s.size );\n", ind, ind, length, rdr)
		g.pf("%s    uint32_t %s = %s;\n", ind, keep, length)
		g.pf("%s    // ILL-FORMED TEXT at an arm (§3): the union reads None, one malformed counts\n", ind)
		g.pf("%s    if ( !TableUtf8Valid( %s.buffer + %s.offset, %s.size ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, rdr, rdr, tag, none)
		g.pf("%s    if ( %s > %d ) { %s = (uint32_t) TableUtf8Clamp( %s.buffer + %s.offset, %s.size, %d ); r.report->clamped++; } // at a code point boundary\n", ind, keep, f.Type.Size, keep, rdr, rdr, rdr, f.Type.Size)
		g.pf("%s    memcpy( %s, %s.buffer + %s.offset, %s );\n", ind, value, rdr, rdr, keep)
		g.pf("%s    %s[%s] = 0;\n", ind, value, keep)
		g.pf("%s    %s = (int32_t) %s;\n", ind, count, keep)
		g.pf("%s    %s.offset += %s;\n%s}\n", ind, rdr, length, ind)
	case f.Type.Kind == ir.TWString:
		// AN ARM'S L IS THE WSTRING'S BYTE LENGTH (§3's arm payload table), so
		// an ODD L is that arm's own framing damage: the union reads None.
		g.pf("%sif ( ( %s.size & 1 ) != 0 ) { %s = %s; r.report->malformed = true; break; } // an odd L is not a wstring at any length (§3)\n", ind, rdr, tag, none)
		g.establishArm(v, base, ind)
		units, keep := "arm_units"+sfx, "arm_keep"+sfx
		g.pf("%s{\n%s    const int64_t %s = %s.size / 2;\n", ind, ind, units, rdr)
		g.pf("%s    int64_t %s = %s;\n", ind, keep, units)
		g.pf("%s    // ILL-FORMED TEXT at an arm (§3): the union reads None, one malformed counts\n", ind)
		g.pf("%s    if ( !TableUtf16Valid( %s.buffer + %s.offset, %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, rdr, units, tag, none)
		g.pf("%s    if ( %s > %d ) { %s = TableUtf16Clamp( %s.buffer + %s.offset, %s, %d ); r.report->clamped++; } // never splitting a pair\n", ind, keep, f.Type.Size, keep, rdr, rdr, units, f.Type.Size)
		g.pf("%s    for ( int64_t i%s = 0; i%s < %s; i%s++ ) { %s[i%s] = (char16_t) TableUtf16Unit( %s.buffer + %s.offset, i%s ); }\n", ind, sfx, sfx, keep, sfx, value, sfx, rdr, rdr, sfx)
		g.pf("%s    %s[%s] = 0;\n", ind, value, keep)
		g.pf("%s    %s = (int32_t) %s;\n", ind, count, keep)
		g.pf("%s    %s.offset += %s.size;\n%s}\n", ind, rdr, rdr, ind)
	case f.Type.Kind == ir.TBytes || f.Array != ir.ArrayNone:
		g.establishArm(v, base, ind)
		bound := armBound(v)
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		elemKind := kind
		if f.Type.Kind == ir.TBytes {
			elemKind = tkU8
		} else if f.Type.Pointer {
			elemKind = tkNodeIndex
		}
		ek, n, keep := "arm_elem_kind"+sfx, "arm_count"+sfx, "arm_keep"+sfx
		i, decoded := "arm_i"+sfx, "arm_decoded"+sfx
		sub, elems, left := "arm_sub"+sfx, "arm_elems"+sfx, "arm_left"+sfx
		// A BODY TOO SHORT FOR ITS OWN HEADER IS INERT (§3, §4): no element is
		// decoded and no counter fires, so the union selects the arm at its
		// declared defaults
		g.pf("%sif ( !%s.has( 2 ) ) { break; }\n", ind, rdr)
		g.pf("%s{\n%s    uint8_t %s = %s.get8();\n", ind, ind, ek, rdr)
		g.pf("%s    uint64_t %s = 0;\n", ind, n)
		g.pf("%s    if ( !%s.getleb( %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, n, tag, none)
		if widenableElement(f) {
			// THE WIDENING BRANCH at an array arm's element kind (§4), inside
			// the mismatch branch: the arm is already selected, so the
			// elements decode at the wire kind's width and the arm is done
			g.pf("%s    if ( %s != %d )\n%s    {\n", ind, ek, elemKind, ind)
			g.pf("%s        if ( !TableKindWidens( %s, %d ) ) { %s = %s; r.report->kind_mismatch++; break; }\n", ind, ek, elemKind, tag, none)
			g.pf("%s        uint64_t widened_keep%s = %s;\n", ind, sfx, n)
			g.pf("%s        if ( widened_keep%s > %d ) { widened_keep%s = %d; r.report->clamped++; }\n", ind, sfx, bound, sfx, bound)
			g.pf("%s        TableReader widened_sub%s( %s.buffer + %s.offset, %s.size - %s.offset, r.report, r.ids );\n", ind, sfx, rdr, rdr, rdr, rdr)
			countLvalue := ""
			if counted {
				countLvalue = count
			}
			g.emitWidenedElements(f, elemKind, ek, value+"[%s]", countLvalue, "widened_keep"+sfx, ind+"        ", "widened_sub"+sfx)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		} else {
			g.pf("%s    if ( %s != %d ) { %s = %s; r.report->kind_mismatch++; break; }\n", ind, ek, elemKind, tag, none)
		}
		g.pf("%s    uint64_t %s = %s;\n", ind, keep, n)
		g.pf("%s    if ( %s > %d ) { %s = %d; r.report->clamped++; }\n", ind, keep, bound, keep, bound)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    if ( !%s.room( %s ) ) { %s = uint64_t( %s.size - %s.offset ); r.report->malformed = true; }\n", ind, rdr, keep, keep, rdr, rdr)
			g.pf("%s    memcpy( %s, %s.buffer + %s.offset, (size_t) %s );\n", ind, value, rdr, rdr, keep)
			g.pf("%s    %s = (int32_t) %s;\n", ind, count, keep)
		} else {
			// the element decode names its reader, so the two locals ahead of
			// it keep the initializer off the name being declared
			g.pf("%s    const uint8_t * %s = %s.buffer + %s.offset;\n", ind, elems, rdr, rdr)
			g.pf("%s    int64_t %s = %s.size - %s.offset;\n", ind, left, rdr, rdr)
			g.pf("%s    TableReader %s( %s, %s, r.report, r.ids );\n", ind, sub, elems, left)
			if counted {
				g.pf("%s    uint64_t %s = 0;\n", ind, decoded)
			}
			g.pf("%s    for ( uint64_t %s = 0; %s < %s; %s++ )\n%s    {\n", ind, i, i, keep, i, ind)
			g.inStep(i, func() {
				g.emitTableReadElementInto(f, elemKind, value+"[(int32_t) "+i+"]", ind+"        ", sub, sfx+"e")
			})
			if counted {
				g.pf("%s        %s = %s + 1;\n", ind, decoded, i)
			}
			g.pf("%s    }\n", ind)
			if counted {
				g.pf("%s    %s = (int32_t) %s;\n", ind, count, decoded)
			}
		}
		g.pf("%s}\n", ind)
	case kind == tkEnum:
		g.pf("%s{\n", ind)
		g.emitEnumRefLoad(f, value, ind+"    ", rdr, fmt.Sprintf("%s = %s; r.report->malformed = true; break;", tag, none))
		g.pf("%s    if ( %s.offset != %s.size ) { %s = %s; r.report->malformed = true; break; } // an L that is not the reference's own length (§3)\n", ind, rdr, rdr, tag, none)
		g.pf("%s}\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		ref, id, length := "arm_inner_ref"+sfx, "arm_inner_id"+sfx, "arm_inner_len"+sfx
		inner, innerKind := "arm_inner"+sfx, "arm_inner_kind"+sfx
		g.pf("%s{\n%s    uint64_t %s = 0;\n", ind, ind, ref)
		g.pf("%s    if ( !%s.getleb( %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, ref, tag, none)
		g.pf("%s    %s.type = %sType::None;\n", ind, value, un.Name)
		g.pf("%s    if ( %s != 0 ) // the zero reference is the inner union's None\n%s    {\n", ind, ref, ind)
		// A LENGTH-SHAPED ARM'S DAMAGED PAYLOAD IS THAT ARM'S OWN FRAMING
		// DAMAGE (§3): the inner union runs past the arm's `L`, so the arm is
		// not decoded — THIS union reads None, malformed counts, and the
		// enclosing body continues past the arm by `L`
		g.pf("%s        if ( %s > (uint64_t) r.ids->count ) { %s = %s; r.report->malformed = true; break; }\n", ind, ref, tag, none)
		g.pf("%s        const uint64_t %s = r.ids->at( %s );\n", ind, id, ref)
		g.pf("%s        if ( !%s.has( 1 ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, tag, none)
		g.pf("%s        const uint8_t %s = %s.get8();\n", ind, innerKind, rdr)
		g.pf("%s        uint64_t %s = 0;\n", ind, length)
		g.pf("%s        if ( !%s.getleb( %s ) || !%s.room( %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, length, rdr, length, tag, none)
		g.pf("%s        TableReader %s( %s.buffer + %s.offset, (int64_t) %s, r.report, r.ids );\n", ind, inner, rdr, rdr, length)
		g.pf("%s        switch ( %s ) // the arm's NAME hash (§5)\n%s        {\n", ind, id, ind)
		for _, in := range un.Variants {
			g.pf("%s            case 0x%016xull: // %s\n%s            {\n", ind, ir.TableWireId(in.WireName()), in.Name, ind)
			g.pf("%s                if ( %s != %d )\n%s                {\n", ind, innerKind, armWireKind(in), ind)
			g.emitArmWiden(in, value, innerKind, inner, value+".type",
				un.Name+"Type::"+ir.GoExportName(in.Name), un.Name+"Type::None", ind+"                    ")
			g.pf("%s                    %s.type = %sType::None; r.report->kind_mismatch++; break;\n%s                }\n", ind, value, un.Name, ind)
			g.pf("%s                %s.type = %sType::%s;\n", ind, value, un.Name, ir.GoExportName(in.Name))
			g.emitArmLoad(in, value, ind+"                ", inner, value+".type", un.Name+"Type::None", sfx+"a")
			g.pf("%s                break;\n%s            }\n", ind, ind)
		}
		g.pf("%s            default: r.report->unknown++;%s break; // an arm this reader cannot name\n", ind, g.retainLostInline())
		g.pf("%s        }\n", ind)
		g.pf("%s        %s.offset += (int64_t) %s;\n", ind, rdr, length)
		g.pf("%s    }\n%s}\n", ind, ind)
	default:
		g.emitTableReadScalarFrom(f, kind, value, ind, rdr,
			fmt.Sprintf("%s = %s; r.report->malformed = true; break;", tag, none))
	}
}

// establishArm zeroes an arm's whole storage the moment the arm is SELECTED
// (docs/SPEC-TABLES.md §2.6): an arm takes no specified default, so zero is
// the establishment, and the bytes its payload does not reach — a counted
// array's tail past the live count, a string's past its length — are then the
// same bytes in a text, a cook image and a block row as they are in the
// engine's own model. Only the arms whose payload can leave a tail take it;
// a fixed-width arm writes its storage whole.
//
// It is a memset and not `= {}` because the PADDING is part of the storage a
// cook image and a block row carry, and value-initialization leaves padding
// alone. The address is cast to `void *` so a pointer arm's slots take it too:
// `TableRef` has a default member initializer, which makes it non-trivial to
// DEFAULT-CONSTRUCT while leaving it trivially copyable, and gcc's
// -Wclass-memaccess reads only the first of those.
func (g *tableGen) establishArm(v ir.UnionVariant, base, ind string) {
	// the WHOLE of the arm's storage: the unnamed struct where the arm carries
	// a companion, the member itself where it does not
	storage := base + "." + v.Name
	g.pf("%smemset( (void *) &%s, 0, sizeof( %s ) ); // selection establishes the arm (§2.6)\n", ind, storage, storage)
}
