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
	return f.Array == ir.ArrayCounted || f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes
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

// emitArmMeasure adds ONE ARM'S PAYLOAD length to `bytes` — the bytes under
// the arm's `L`, never the framing — and returns through `onBad` where the
// storage invariant or the wire identity refuses the value, exactly as the
// field paths do.
func (g *tableGen) emitArmMeasure(v ir.UnionVariant, base, ind, onBad, sfx string) {
	if v.Void() {
		g.pf("%s// a payload-free arm: the arm id and L = 0 are the whole of it (§2.6)\n", ind)
		return
	}
	f := v.F
	kind := tableScalarKind(f)
	value, count := armValue(base, v), armCount(base, v)
	body, elem, i := "arm_body"+sfx, "arm_elem"+sfx, "arm_i"+sfx
	switch {
	case v.Body():
		g.noteRef(v.Type)
		g.pf("%s{\n%s    int64_t %s = %s;\n", ind, ind, body, g.measureCall(v.Type, value))
		g.pf("%s    if ( %s < 0 ) { %s }\n", ind, body, onBad)
		g.pf("%s    bytes += %s; // the arm's own table body (§3)\n%s}\n", ind, body, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%sbytes += 4; // a node index (§3.1)\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sbytes += %s; // the string's bytes under the arm's L\n", ind, count)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sbytes += 5 + %s; // element kind 6, N, then the bytes\n", ind, count)
	case f.Array != ir.ArrayNone:
		n := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.ArrayBound, onBad)
			n = count
		}
		g.pf("%sbytes += 5; // element kind, N\n", ind)
		switch {
		case f.Type.Pointer:
			g.pf("%sbytes += 4 * (int64_t) %s; // node indices (§3.1)\n", ind, n)
		case kind == tkTable:
			g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
			g.pf("%s    int64_t %s = %s;\n", ind, elem, g.measureCall(f.Type.Name, value+"["+i+"]"))
			g.pf("%s    if ( %s < 0 ) { %s }\n", ind, elem, onBad)
			g.pf("%s    bytes += 4 + %s;\n%s}\n", ind, elem, ind)
		case kind == tkUnion:
			g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
			g.emitUnionElementMeasure(f, value+"["+i+"]", ind+"    ", sfx+"e")
			g.pf("%s}\n", ind)
		default:
			g.emitEnumElementCheckAt(f, value+"[%s]", n, ind, onBad, sfx)
			g.pf("%sbytes += (int64_t) %s * %d;\n", ind, n, tableKindWidth(kind))
		}
	case enumRef(f) != nil:
		g.pf("%s{\n%s    uint16_t arm_named%s = 0;\n", ind, ind, sfx)
		g.pf("%s    if ( !TableEnumId( %s, arm_named%s ) ) { %s } // no variant names this value\n", ind, value, sfx, onBad)
		g.pf("%s    bytes += 2; // the variant's name hash\n%s}\n", ind, ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sbytes += 2; // the nested union's arm id\n", ind)
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, value, ind)
		g.pf("%s    case %sType::None: break;\n", ind, un.Name)
		for _, inner := range un.Variants {
			g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(inner.Name), ind)
			g.pf("%s        bytes += 4;\n", ind)
			g.emitArmMeasure(inner, value, ind+"        ", onBad, sfx+"a")
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: %s\n%s}\n", ind, onBad, ind)
	default:
		g.pf("%sbytes += %d; // %s\n", ind, tableKindWidth(kind), ir.FieldTypeSpelling(f))
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
	i, index := "arm_i"+sfx, "arm_index"+sfx
	switch {
	case v.Body():
		g.pf("%sif ( !%s ) { %s }\n", ind, g.saveCall(v.Type, value), onBad)
	case f.Type.Blob():
		blob := "arm_blob" + sfx
		g.pf("%s{\n%s    const TableBlob * %s = TableBlobAt( ctx, %s );\n", ind, ind, blob, value)
		g.pf("%s    uint32_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, blob, blob, index, onBad)
		g.pf("%s    w.put32( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, index, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		t, pointee := f.Type.Name, "arm_pointee"+sfx
		g.pf("%s{\n%s    const %s * %s = %sAt( ctx, %s );\n", ind, ind, t, pointee, t, value)
		g.pf("%s    uint32_t %s = 0;\n", ind, index)
		g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, pointee, pointee, index, onBad)
		g.pf("%s    w.put32( %s ); // a node index, null as 0 (§3.1)\n%s}\n", ind, index, ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sw.raw( %s, %s );\n", ind, value, count)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sif ( %s < 0 || %s > %d ) { %s } // storage invariant\n", ind, count, count, f.Type.Size, onBad)
		g.pf("%sw.put8( %d ); w.put32( uint32_t( %s ) ); // bytes ride as an array of u8 (§2.5)\n", ind, tkU8, count)
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
		g.pf("%sw.put8( %d ); w.put32( uint32_t( %s ) );\n", ind, elemKind, n)
		g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
		g.emitTableWriteElement(f, elemKind, value+"["+i+"]", ind+"    ", sfx+"e")
		g.pf("%s}\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		at := "arm_inner_at" + sfx
		g.pf("%sswitch ( %s.type ) // the nested union's payload in place (§3)\n%s{\n", ind, value, ind)
		g.pf("%s    case %sType::None: w.put16( 0 ); break;\n", ind, un.Name)
		for _, inner := range un.Variants {
			g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(inner.Name), ind)
			g.pf("%s        w.put16( 0x%04x ); // the arm's NAME hash (§5)\n", ind, ir.VariantId(inner.Name))
			g.pf("%s        int64_t %s = w.offset; w.put32( 0 );\n", ind, at)
			g.emitArmSave(inner, value, ind+"        ", onBad, sfx+"a")
			g.pf("%s        w.patch32( %s, uint32_t( w.offset - %s - 4 ) );\n", ind, at, at)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: %s\n%s}\n", ind, onBad, ind)
	default:
		g.emitTableWriteElement(f, kind, value, ind, sfx)
	}
}

// armFixedWidth is the payload width an arm's `L` must equal, or 0 where the
// arm's payload is length-shaped. It is the whole of what a reader can check
// about an arm's type: no per-arm kind byte rides (§3), so an `L` that is not
// this width is a KIND MISMATCH and an arm retyped under one width is §4.1's
// silent class, which §18's baseline refuses.
func armFixedWidth(v ir.UnionVariant) int { return ir.ArmFixedWidth(v.F) }

// emitArmLoad reads ONE ARM'S PAYLOAD out of `rdr`, a reader bounded to the
// arm's `L`. `tag` is the union's tag lvalue and `none` the tag value to put
// back where the payload cannot be the arm's: a fixed-width arm whose length
// is not its width is a KIND MISMATCH, counted, the union left None and the
// parent reading on past `L` (§2.6, §4).
func (g *tableGen) emitArmLoad(v ir.UnionVariant, base, ind, rdr, tag, none, sfx string) {
	if v.Void() {
		g.pf("%sif ( %s.size != 0 ) { %s = %s; r.report->kind_mismatch++; break; } // a payload-free arm carries no payload (§2.6)\n", ind, rdr, tag, none)
		return
	}
	f := v.F
	kind := tableScalarKind(f)
	value, count := armValue(base, v), armCount(base, v)
	if w := armFixedWidth(v); w > 0 {
		g.pf("%sif ( %s.size != %d ) { %s = %s; r.report->kind_mismatch++; break; } // an arm carries no kind byte: the length is the check (§2.6)\n",
			ind, rdr, w, tag, none)
	}
	switch {
	case v.Body():
		g.pf("%s%s;\n", ind, g.loadCall(v.Type, rdr, value))
		// A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3): an arm whose
		// terminator is not the last two bytes of its `L` is framing damage —
		// the payload stops, the union reads None, and the enclosing body
		// continues past the arm by `L`.
		g.pf("%sif ( %s.offset != %s.size ) { %s = %s; r.report->malformed = true; break; }\n",
			ind, rdr, rdr, tag, none)
	case f.Type.Blob():
		g.pf("%sTableNodeResolve( nodes, %s, %s.get32(), %s, r.report ); // *%s\n", ind, value, rdr, blobTypeIdConst(f), blobWord(f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%sTableNodeResolve( nodes, %s, %s.get32(), 0x%016xull, r.report ); // *%s\n", ind, value, rdr, ir.TableTypeId(f.Type.Name), f.Type.Name)
	case f.Type.Kind == ir.TString:
		g.establishArm(v, base, ind)
		length, keep := "arm_len"+sfx, "arm_keep"+sfx
		g.pf("%s{\n%s    uint32_t %s = uint32_t( %s.size );\n", ind, ind, length, rdr)
		g.pf("%s    uint32_t %s = %s;\n", ind, keep, length)
		g.pf("%s    if ( %s > %d ) { %s = %d; r.report->clamped++; }\n", ind, keep, f.Type.Size, keep, f.Type.Size)
		g.pf("%s    memcpy( %s, %s.buffer + %s.offset, %s );\n", ind, value, rdr, rdr, keep)
		g.pf("%s    %s[%s] = 0;\n", ind, value, keep)
		g.pf("%s    %s = (int32_t) %s;\n", ind, count, keep)
		g.pf("%s    %s.offset += %s;\n%s}\n", ind, rdr, length, ind)
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
		ek, n, keep := "arm_kind"+sfx, "arm_count"+sfx, "arm_keep"+sfx
		i, decoded := "arm_i"+sfx, "arm_decoded"+sfx
		sub, elems, left := "arm_sub"+sfx, "arm_elems"+sfx, "arm_left"+sfx
		// A BODY TOO SHORT FOR ITS OWN HEADER IS INERT (§3, §4): no element is
		// decoded and no counter fires, so the union selects the arm at its
		// declared defaults
		g.pf("%sif ( !%s.has( 5 ) ) { break; }\n", ind, rdr)
		g.pf("%s{\n%s    uint8_t %s = %s.get8();\n", ind, ind, ek, rdr)
		g.pf("%s    uint32_t %s = %s.get32();\n", ind, n, rdr)
		g.pf("%s    if ( %s != %d ) { %s = %s; r.report->kind_mismatch++; break; }\n", ind, ek, elemKind, tag, none)
		g.pf("%s    uint32_t %s = %s;\n", ind, keep, n)
		g.pf("%s    if ( %s > %d ) { %s = %d; r.report->clamped++; }\n", ind, keep, bound, keep, bound)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    if ( !%s.has( %s ) ) { %s = uint32_t( %s.size - %s.offset ); r.report->malformed = true; }\n", ind, rdr, keep, keep, rdr, rdr)
			g.pf("%s    memcpy( %s, %s.buffer + %s.offset, %s );\n", ind, value, rdr, rdr, keep)
			g.pf("%s    %s = (int32_t) %s;\n", ind, count, keep)
		} else {
			// the element decode names its reader, so the two locals ahead of
			// it keep the initializer off the name being declared
			g.pf("%s    const uint8_t * %s = %s.buffer + %s.offset;\n", ind, elems, rdr, rdr)
			g.pf("%s    int64_t %s = %s.size - %s.offset;\n", ind, left, rdr, rdr)
			g.pf("%s    TableReader %s( %s, %s, r.report );\n", ind, sub, elems, left)
			if counted {
				g.pf("%s    uint32_t %s = 0;\n", ind, decoded)
			}
			g.pf("%s    for ( uint32_t %s = 0; %s < %s; %s++ )\n%s    {\n", ind, i, i, keep, i, ind)
			g.emitTableReadElementInto(f, elemKind, value+"["+i+"]", ind+"        ", sub, sfx+"e")
			if counted {
				g.pf("%s        %s = %s + 1;\n", ind, decoded, i)
			}
			g.pf("%s    }\n", ind)
			if counted {
				g.pf("%s    %s = (int32_t) %s;\n", ind, count, decoded)
			}
		}
		g.pf("%s}\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		id, length, inner := "arm_inner_id"+sfx, "arm_inner_len"+sfx, "arm_inner"+sfx
		g.pf("%sif ( !%s.has( 2 ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, tag, none)
		g.pf("%s{\n%s    uint16_t %s = %s.get16();\n", ind, ind, id, rdr)
		g.pf("%s    %s.type = %sType::None;\n", ind, value, un.Name)
		g.pf("%s    if ( %s != 0 )\n%s    {\n", ind, id, ind)
		// A LENGTH-SHAPED ARM'S DAMAGED PAYLOAD IS THAT ARM'S OWN FRAMING
		// DAMAGE (§3): the inner union runs past the arm's `L`, so the arm is
		// not decoded — THIS union reads None, malformed counts, and the
		// enclosing body continues past the arm by `L`
		g.pf("%s        if ( !%s.has( 4 ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, tag, none)
		g.pf("%s        uint32_t %s = %s.get32();\n", ind, length, rdr)
		g.pf("%s        if ( !%s.has( %s ) ) { %s = %s; r.report->malformed = true; break; }\n", ind, rdr, length, tag, none)
		g.pf("%s        TableReader %s( %s.buffer + %s.offset, %s, r.report );\n", ind, inner, rdr, rdr, length)
		g.pf("%s        switch ( %s ) // the arm's NAME hash (§5)\n%s        {\n", ind, id, ind)
		for _, in := range un.Variants {
			g.pf("%s            case 0x%04x: // %s\n%s            {\n", ind, ir.VariantId(in.Name), in.Name, ind)
			g.pf("%s                %s.type = %sType::%s;\n", ind, value, un.Name, ir.GoExportName(in.Name))
			g.emitArmLoad(in, value, ind+"                ", inner, value+".type", un.Name+"Type::None", sfx+"a")
			g.pf("%s                break;\n%s            }\n", ind, ind)
		}
		g.pf("%s            default: r.report->unknown++; break; // an arm this reader cannot name\n", ind)
		g.pf("%s        }\n", ind)
		g.pf("%s        %s.offset += %s;\n", ind, rdr, length)
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
func (g *tableGen) establishArm(v ir.UnionVariant, base, ind string) {
	// the WHOLE of the arm's storage: the unnamed struct where the arm carries
	// a companion, the member itself where it does not
	storage := base + "." + v.Name
	g.pf("%smemset( &%s, 0, sizeof( %s ) ); // selection establishes the arm (§2.6)\n", ind, storage, storage)
}
