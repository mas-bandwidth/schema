package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) wireEnumRead(e *ir.Enum, dst, rdr, ind, onBad string) {
	g.pf("%s{\n%s    uint64_t variant_ref;\n", ind, ind)
	g.pf("%s    if ( !table_reader_leb( &%s, &variant_ref ) || variant_ref > %s.id_count ) { %s }\n", ind, rdr, rdr, onBad)
	g.pf("%s    if ( variant_ref == 0 ) { %s = %s; }\n%s    else\n%s    {\n", ind, dst, enumNoneConst(e.Name), ind, ind)
	g.pf("%s        switch ( table_reader_id_at( &%s, variant_ref ) )\n%s        {\n", ind, rdr, ind)
	for i, v := range e.Variants {
		g.pf("%s            case 0x%016xull: %s = %s; break;\n", ind, ir.TableWireId(e.VariantWireName(i)), dst, enumConst(e.Name, v))
	}
	g.pf("%s            default: %s = %s; r->report->unknown++; break;\n", ind, dst, enumNoneConst(e.Name))
	g.pf("%s        }\n%s    }\n%s}\n", ind, ind, ind)
}

func (g *tableGen) wireScalarRead(f *ir.Field, dst, rdr, wireKind, ind, onBad string) {
	if e := enumRef(f); e != nil {
		g.wireEnumRead(e, dst, rdr, ind, onBad)
		return
	}
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	// The caller has already accepted this kind or a precise widening. Keep
	// each numeric decode at the source width, including its range clamps.
	g.pf("%sswitch ( %s )\n%s{\n", ind, wireKind, ind)
	for source := 1; source <= 11; source++ {
		if source != kind && !ir.TableKindWidens(source, kind) {
			continue
		}
		g.pf("%s    case %d:\n%s    {\n", ind, source, ind)
		// onBad can contain break, which must leave the caller's element loop,
		// not this dispatch switch. A per-field label supplies that boundary.
		if source == kind {
			g.emitTableReadScalarFrom(f, source, dst, ind+"        ", rdr, onBad)
		} else {
			g.wireWidenedScalar(f, source, dst, rdr, ind+"        ", onBad)
		}
		g.pf("%s        break;\n%s    }\n", ind, ind)
	}
	g.pf("%s    default: %s\n%s}\n", ind, onBad, ind)
}

func (g *tableGen) wireWidenedScalar(f *ir.Field, source int, dst, rdr, ind, onBad string) {
	width := tableKindWidth(source)
	g.pf("%sif ( !table_reader_has( &%s, %d ) ) { %s }\n", ind, rdr, width, onBad)
	if source == tkF32 {
		g.pf("%sdouble decoded_wide = table_wire_widen_f32( table_reader_get32( &%s ) );\n", ind, rdr)
		if f.HasFloatRange {
			g.pf("%sif ( decoded_wide < %s ) { decoded_wide = %s; r->report->clamped++; }\n", ind, formatFloat(f.FMin, false), formatFloat(f.FMin, false))
			g.pf("%selse if ( decoded_wide > %s ) { decoded_wide = %s; r->report->clamped++; }\n", ind, formatFloat(f.FMax, false), formatFloat(f.FMax, false))
		}
	} else {
		signed := ir.TableKindSigned(source)
		storage, cast := "uint64_t", fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage, cast = "int64_t", fmt.Sprintf("int%d_t", width*8)
		}
		g.pf("%s%s decoded_wide = (%s) %s( &%s );\n", ind, storage, cast, tableGet(width), rdr)
		if f.HasIntRange {
			low, high := tableClampEnds(f, 8)
			if low {
				g.pf("%sif ( decoded_wide < %s ) { decoded_wide = %s; r->report->clamped++; }\n", ind, tableIntLit(f.IntMin, signed, 8), tableIntLit(f.IntMin, signed, 8))
			}
			if high {
				g.pf("%sif ( decoded_wide > %s ) { decoded_wide = %s; r->report->clamped++; }\n", ind, tableIntLit(f.IntMax, signed, 8), tableIntLit(f.IntMax, signed, 8))
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := (uint64(1) << f.Type.Width) - 1
			g.pf("%sif ( decoded_wide > %dull ) { decoded_wide = %dull; r->report->clamped++; }\n", ind, max, max)
		}
	}
	g.pf("%s%s = decoded_wide;\n", ind, dst)
}

func (g *tableGen) emitWireRead(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s( TableReader * r, %s * value )\n{\n", g.api(st.Name, "load_body"), st.Name)
	g.pf("    %s( value );\n", g.api(st.Name, "reset"))
	g.pf("    for ( ;; )\n    {\n        uint64_t ref, id; uint8_t kind;\n")
	g.pf("        if ( !table_reader_leb( r, &ref ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        if ( ref == 0 ) { return 1; }\n")
	g.pf("        if ( ref > r->id_count || !table_reader_has( r, 1 ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        id = table_reader_id_at( r, ref ); kind = table_reader_get8( r );\n")
	g.pf("        if ( (id == 0xffffffffffffffffull && r->nested) || id == 0xfffffffffffffffeull || id == 0xfffffffffffffffdull ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        switch ( id )\n        {\n")
	for _, f := range st.Fields {
		kind := ir.TableWireScalarKind(f)
		wireKind := kind
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		if f.KeyEnum != "" {
			wireKind = tkKeyed
		}
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		plain := f.Array == ir.ArrayNone && f.Type.Kind != ir.TBytes && kind <= tkF64
		g.pf("            case 0x%016xull: /* %s */\n            {\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                if ( kind != %d )\n                {\n", wireKind)
		if plain {
			g.pf("                    if ( table_kind_widens( kind, %d ) )\n                    {\n", wireKind)
			g.wireScalarRead(f, "value->"+f.Name, "(*r)", "kind", "                        ", "r->report->malformed = 1; return 0;")
			g.pf("                        r->report->widened++;\n")
			if f.Type.Optional {
				g.pf("                        value->%s_present = 1;\n", f.Name)
			}
			g.pf("                        break;\n                    }\n")
		}
		g.pf("                    r->report->kind_mismatch++;\n                    if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n                    break;\n                }\n")
		g.wireReadField(f, kind)
		if f.Type.Optional {
			g.pf("                value->%s_present = 1;\n", f.Name)
		}
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n                r->report->unknown++;\n                if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n                break;\n        }\n    }\n}\n\n")
	g.pf("static SCHEMA_UNUSED int %s( %s * value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", g.api(st.Name, "load"), st.Name)
	g.pf("    TableReport ignored; TableReader r;\n    memset( &ignored, 0, sizeof( ignored ) );\n    if ( report == NULL ) { report = &ignored; }\n")
	g.pf("    %s( value );\n    if ( !table_wire_open( &r, buffer, bytes, report ) ) { return 0; }\n", g.api(st.Name, "reset"))
	g.pf("    return %s( &r, value );\n}\n\n", g.api(st.Name, "load_body"))
}

func (g *tableGen) wireReadField(f *ir.Field, kind int) {
	ind := "                "
	dst := "value->" + f.Name
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sTableReader sub; uint64_t keep;\n", ind)
		g.pf("%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sif ( !table_wire_utf8( sub.buffer, (uint64_t) sub.size ) ) { r->report->malformed = 1; %s[0] = 0; %s_length = 0; break; }\n", ind, dst, dst)
		g.pf("%skeep = (uint64_t) sub.size;\n%sif ( keep > %d ) { keep = table_wire_utf8_clamp( sub.buffer, keep, %d ); r->report->clamped++; }\n", ind, ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( %s, sub.buffer, (size_t) keep ); %s[keep] = 0; %s_length = (int32_t) keep;\n", ind, dst, dst, dst)
	case f.KeyEnum != "":
		g.wireReadKeyed(f, kind, ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		count := ""
		if f.Array == ir.ArrayCounted {
			count = dst + "_count"
		}
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
			count = dst + "_length"
		}
		g.pf("%sTableReader sub;\n%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
		g.pf("%sif ( sub.size >= 2 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind; uint64_t count, keep, i;\n", ind)
		g.pf("%s    if ( !table_reader_array_header( &sub, r, &elem_kind, &count ) ) { r->report->malformed = 1; }\n%s    else\n%s    {\n", ind, ind, ind)
		g.pf("%s        if ( elem_kind != %d )\n%s        {\n", ind, kind, ind)
		g.pf("%s            if ( !table_kind_widens( elem_kind, %d ) ) { r->report->kind_mismatch++; break; }\n%s            r->report->widened++;\n%s        }\n", ind, kind, ind, ind)
		g.pf("%s        keep = count; if ( keep > %d ) { keep = %d; r->report->clamped++; }\n", ind, bound, bound)
		if count != "" {
			g.pf("%s        %s = 0;\n", ind, count)
		}
		g.pf("%s        for ( i = 0; i < keep; i++ )\n%s        {\n", ind, ind)
		bad := "r->report->malformed = 1; goto end_" + f.Name + ";"
		if kind == tkTable {
			g.pf("%s            TableReader elem;\n%s            if ( !table_reader_span( &sub, &elem ) ) { %s }\n", ind, ind, bad)
			g.pf("%s            %s( &elem, &%s[i] );\n", ind, g.api(f.Type.Name, "load_body"), dst)
			// Array elements retain the decoded prefix, matching the reference
			// rather than applying a nested field's exact-extent reset.
		} else {
			g.wireScalarRead(f, dst+"[i]", "sub", "elem_kind", ind+"            ", bad)
		}
		if count != "" {
			g.pf("%s            %s = (int32_t) i + 1;\n", ind, count)
		}
		g.pf("%s        }\n%s        end_%s: ;\n%s    }\n%s}\n", ind, ind, f.Name, ind, ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		none := enumConst(un.Name+"Type", "None")
		g.pf("%suint64_t arm_ref, arm_id; uint8_t arm_kind; TableReader sub;\n", ind)
		g.pf("%sif ( !table_reader_leb( r, &arm_ref ) || arm_ref > r->id_count ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sif ( arm_ref == 0 ) { %s.type = %s; break; }\n", ind, dst, none)
		g.pf("%sarm_id = table_reader_id_at( r, arm_ref );\n%sif ( !table_reader_has( r, 1 ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
		g.pf("%sarm_kind = table_reader_get8( r );\n%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
		g.pf("%s%s.type = %s;\n%sswitch ( arm_id )\n%s{\n", ind, dst, none, ind, ind)
		for _, v := range un.Variants {
			g.pf("%s    case 0x%016xull:\n", ind, ir.TableWireId(v.WireName()))
			g.pf("%s        if ( arm_kind != %d ) { r->report->kind_mismatch++; break; }\n", ind, tkTable)
			g.pf("%s        %s.type = %s;\n", ind, dst, enumConst(un.Name+"Type", v.Name))
			g.pf("%s        %s( &sub, &%s.as.%s );\n", ind, g.api(v.Type, "load_body"), dst, v.Name)
			g.pf("%s        if ( sub.offset != sub.size ) { r->report->malformed = 1; %s.type = %s; }\n%s        break;\n", ind, dst, none, ind)
		}
		g.pf("%s    default: r->report->unknown++; break;\n%s}\n", ind, ind)
	case kind == tkTable:
		g.pf("%sTableReader sub;\n%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
		g.pf("%s%s( &sub, &%s );\n", ind, g.api(f.Type.Name, "load_body"), dst)
		g.pf("%sif ( sub.offset != sub.size ) { r->report->malformed = 1; %s( &%s ); }\n", ind, g.api(f.Type.Name, "reset"), dst)
	default:
		g.wireScalarRead(f, dst, "(*r)", "kind", ind, "r->report->malformed = 1; return 0;")
	}
}

func (g *tableGen) wireReadKeyed(f *ir.Field, kind int, ind string) {
	g.pf("%sTableReader sub;\n%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
	g.pf("%sif ( sub.size >= 2 )\n%s{\n", ind, ind)
	g.pf("%s    uint8_t elem_kind; uint64_t count, i;\n", ind)
	g.pf("%s    if ( !table_reader_array_header( &sub, r, &elem_kind, &count ) ) { r->report->malformed = 1; break; }\n", ind)
	g.pf("%s    if ( elem_kind != %d ) { if ( !table_kind_widens( elem_kind, %d ) ) { r->report->kind_mismatch++; break; } r->report->widened++; }\n", ind, kind, kind)
	g.pf("%s    for ( i = 0; i < count; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t key_ref, key; int32_t slot = -1; TableReader elem;\n", ind)
	g.pf("%s        if ( !table_reader_leb( &sub, &key_ref ) || key_ref == 0 || key_ref > sub.id_count || !table_reader_span( &sub, &elem ) ) { r->report->malformed = 1; break; }\n", ind)
	g.pf("%s        key = table_reader_id_at( &sub, key_ref );\n%s        switch ( key )\n%s        {\n", ind, ind, ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%s            case 0x%016xull: slot = %d; break;\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	g.pf("%s            default: r->report->unknown++; break;\n%s        }\n%s        if ( slot < 0 ) { continue; }\n", ind, ind, ind)
	dst := fmt.Sprintf("value->%s[slot]", f.Name)
	if kind == tkTable {
		g.pf("%s        %s( &elem, &%s );\n", ind, g.api(f.Type.Name, "load_body"), dst)
	} else {
		g.wireScalarRead(f, dst, "elem", "elem_kind", ind+"        ", "r->report->malformed = 1; goto next_"+f.Name+";")
	}
	if kind != tkTable {
		g.pf("%s        next_%s: ;\n", ind, f.Name)
	}
	g.pf("%s    }\n%s}\n", ind, ind)
}
