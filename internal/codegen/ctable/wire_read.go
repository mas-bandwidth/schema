package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Named element types keep their declared wire kind, including flags.
func wireElementWidenTest(f *ir.Field, kind int, expr string) string {
	if f.Type.Pointer {
		return "0"
	}
	// Named flags carry unsigned kind 9 and obey the same widening ladder.
	return fmt.Sprintf("table_kind_widens(%s,%d)", expr, kind)
}

func (g *tableGen) wireEnumRead(e *ir.Enum, dst, rdr, ind, onBad string) {
	g.pf("%s{\n%s    uint64_t variant_ref;\n", ind, ind)
	g.pf("%s    if ( !table_reader_leb( &%s, &variant_ref ) || variant_ref > %s.id_count ) { %s }\n", ind, rdr, rdr, onBad)
	g.pf("%s    if ( variant_ref == 0 ) { %s = %s; }\n%s    else\n%s    {\n", ind, dst, enumNoneConst(e.Name), ind, ind)
	g.pf("%s        switch ( table_reader_id_at( &%s, variant_ref ) )\n%s        {\n", ind, rdr, ind)
	for i, v := range e.Variants {
		g.pf("%s            case 0x%016xull: %s = %s; break;\n", ind, ir.TableWireId(e.VariantWireName(i)), dst, enumConst(e.Name, v))
	}
	g.pf("%s            default: %s = %s; "+g.unknownEvent()+" break;\n", ind, dst, enumNoneConst(e.Name))
	g.pf("%s        }\n%s    }\n%s}\n", ind, ind, ind)
}

func (g *tableGen) wireScalarRead(f *ir.Field, dst, rdr, wireKind, ind, onBad string) {
	if f.Type.Pointer {
		g.pf("%s{ uint64_t index; if(!table_reader_leb(&%s,&index)) { %s }\n", ind, rdr, onBad)
		target := ir.TableWireId(ir.PointeeWireName(f))
		if f.Type.Blob() {
			target = ir.BlobWireTypeId(f)
		}
		g.pf("%s  table_node_resolve(%s.nodes,&%s,index,UINT64_C(0x%016x),r->report); }\n", ind, rdr, dst, target)
		return
	}
	if e := enumRef(f); e != nil {
		g.wireEnumRead(e, dst, rdr, ind, onBad)
		return
	}
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
		kind = tkU8
	}
	// The caller has already accepted this kind or a precise widening. Keep
	// each numeric decode at the source width, including its range clamps.
	g.pf("%sswitch ( %s )\n%s{\n", ind, wireKind, ind)
	for source := 1; source <= 29; source++ {
		if source != kind && !ir.TableKindWidens(source, kind) {
			continue
		}
		g.pf("%s    case %d:\n%s    {\n", ind, source, ind)
		// onBad can contain break, which must leave the caller's element loop,
		// not this dispatch switch. A per-field label supplies that boundary.
		switch {
		case tableKindWidth(kind) == 16:
			g.wireWideRead(f, source, dst, rdr, ind+"        ", onBad)
		case source == kind:
			g.emitTableReadScalarFrom(f, source, dst, ind+"        ", rdr, onBad)
		default:
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

// A builder storage-cap refusal must pass through every enclosing body without
// a default reset or an extra malformed event.
func (g *tableGen) wireRefusalCheck(ind string) {
	if g.anySequence {
		g.pf("%sif(r->nodes!=NULL && r->nodes->refused) { return 0; }\n", ind)
	}
}

func (g *tableGen) emitWireRead(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s( TableReader * r, %s * value )\n{\n", g.api(st.Name, "load_body"), st.Name)
	g.wireRefusalCheck("    ")
	g.pf("    %s( value );\n", g.api(st.Name, "reset"))
	if g.retain {
		g.pf("    TableRetainWalk body_keep=retention;table_retain_discard(retention,0,0);\n")
	}
	g.pf("    for ( ;; )\n    {\n        uint64_t ref, id; uint8_t kind;\n")
	g.pf("        if ( !table_reader_leb( r, &ref ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        if ( ref == 0 ) { return 1; }\n")
	g.pf("        if ( ref > r->id_count || !table_reader_has( r, 1 ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        id = table_reader_id_at( r, ref ); kind = table_reader_get8( r );\n")
	g.pf("        if ( (id == 0xffffffffffffffffull && r->nested) || id == 0xfffffffffffffffeull || id == 0xfffffffffffffffdull ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        switch ( id )\n        {\n")
	for ordinal, f := range st.Fields {
		kind := ir.TableWireScalarKind(f)
		wireKind := kind
		if f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) {
			wireKind = tkArray
		}
		if f.KeyEnum != "" {
			wireKind = tkKeyed
		}
		if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
			kind = tkU8
		}
		plain := !f.Type.Pointer && f.Array == ir.ArrayNone && f.KeyEnum == "" && (kind == tkI16 || kind == tkI32 || kind == tkI64 || kind == 18 || kind == tkU16 || kind == tkU32 || kind == tkU64 || kind == 19 || kind == tkF64)
		g.pf("            case 0x%016xull: /* %s */\n            {\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                if ( kind != %d )\n                {\n", wireKind)
		if plain {
			g.pf("                    if ( table_kind_widens( kind, %d ) )\n                    {\n", wireKind)
			g.wireScalarRead(f, "value->"+f.Name, "(*r)", "kind", "                        ", "r->report->malformed = 1; return 0;")
			if !st.IsMapEntry() || f.Name != ir.MapKeyFieldName {
				g.pf("                        r->report->widened++;\n")
			} // map key widening is counted once by the key scan

			if f.Type.Optional {
				g.pf("                        value->%s_present = 1;\n", f.Name)
			}
			g.pf("                        break;\n                    }\n")
		}
		g.pf("                    r->report->kind_mismatch++;\n                    if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n                    break;\n                }\n")
		g.retainReadField(f, ordinal, "                ")
		g.wireReadFieldAt(f, kind, "value->"+f.Name)
		if f.Type.Optional {
			g.pf("                value->%s_present = 1;\n", f.Name)
		}
		g.pf("                break;\n            }\n")
	}
	if g.anyVariable {
		g.pf("%s", "            case UINT64_MAX:\n                if(r->nodes==NULL) { "+g.unknownEvent()+" }\n                if(!table_reader_skip(r,kind)) { r->report->malformed=1; return 0; }\n                break;\n")
	}
	if g.retain {
		g.pf("            default:\n                r->report->unknown += 1;\n                if(!table_retain_capture(r,id,kind,body_keep)){r->report->malformed=1;return 0;}break;\n        }\n    }\n}\n\n")
	} else {
		g.pf("%s", "            default:\n                "+g.unknownEvent()+"\n                if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n                break;\n        }\n    }\n}\n\n")
	}
	if g.retain || g.isVar(st.Name) || st.IsMapEntry() {
		return
	}
	g.pf("static SCHEMA_UNUSED int %s( %s * value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", g.api(st.Name, "load"), st.Name)
	g.pf("    TableReport ignored; TableReader r;\n    memset( &ignored, 0, sizeof( ignored ) );\n    if ( report == NULL ) { report = &ignored; }\n")
	g.pf("    %s( value );\n    if ( !table_wire_open( &r, buffer, bytes, report ) ) { return 0; }\n", g.api(st.Name, "reset"))
	g.pf("    return %s( &r, value );\n}\n\n", g.api(st.Name, "load_body"))
}

func (g *tableGen) wireReadFieldAt(f *ir.Field, kind int, dst string) {
	g.wireReadFieldPayload(f, kind, dst, "")
}
func (g *tableGen) wireReadFieldPayload(f *ir.Field, kind int, dst, bounded string) {
	ind := "                "
	switch {
	case f.IsMap():
		g.emitMapRead(f, dst, bounded)
	case f.IsList():
		g.emitListRead(f, dst, bounded)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.wireScalarRead(f, dst, "(*r)", "kind", ind, "r->report->malformed=1; return 0;")
	case f.Type.Kind == ir.TWString:
		g.pf("%sTableReader sub; int64_t keep, i;\n", ind)
		g.pf("%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sif ( sub.size %% 2 != 0 || !table_wire_utf16( sub.buffer, sub.size/2 ) ) { r->report->malformed = 1; %s[0] = 0; %s_length = 0; break; }\n", ind, dst, dst)
		g.pf("%skeep = table_wire_utf16_clamp( sub.buffer, sub.size/2, %d ); if ( keep != sub.size/2 ) { r->report->clamped++; }\n", ind, f.Type.Size)
		g.pf("%sfor ( i = 0; i < keep; i++ ) { %s[i] = table_wire_utf16_unit( sub.buffer, i ); } %s[keep] = 0; %s_length = (int32_t)keep;\n", ind, dst, dst, dst)
	case (f.Type.Kind == ir.TString && !f.Type.Blob()):
		g.pf("%sTableReader sub; uint64_t keep;\n", ind)
		g.pf("%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sif ( !table_wire_utf8( sub.buffer, (uint64_t) sub.size ) )\n%s{\n", ind, ind)
		if len(f.DefBytes) != 0 {
			g.pf("%s    static const uint8_t initial[] = { %s };\n%s    memcpy( %s, initial, sizeof( initial ) );\n", ind, byteLiterals(f.DefBytes), ind, dst)
		}
		g.pf("%s    r->report->malformed = 1; %s[%d] = 0; %s_length = %d; break;\n%s}\n", ind, dst, len(f.DefBytes), dst, len(f.DefBytes), ind)
		g.pf("%skeep = (uint64_t) sub.size;\n%sif ( keep > %d ) { keep = table_wire_utf8_clamp( sub.buffer, keep, %d ); r->report->clamped++; }\n", ind, ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( %s, sub.buffer, (size_t) keep ); %s[keep] = 0; %s_length = (int32_t) keep;\n", ind, dst, dst, dst)
	case f.KeyEnum != "":
		g.wireReadKeyed(f, kind, ind)
	case f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()):
		bound := f.ArrayBound
		count := ""
		if f.Array == ir.ArrayCounted {
			count = dst + "_count"
		}
		if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
			bound = f.Type.Size
			count = dst + "_length"
		}
		g.pf("%sTableReader sub;\n", ind)
		if bounded == "" {
			g.pf("%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind)
		} else {
			g.pf("%ssub = %s; %s.offset = %s.size;\n", ind, bounded, bounded, bounded)
		}
		g.pf("%sif ( sub.size >= 2 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind; uint64_t count, kept, i;\n", ind)
		if bounded == "" {
			g.pf("%s    if ( !table_reader_array_header( &sub, r, &elem_kind, &count ) ) { r->report->malformed = 1; }\n%s    else\n%s    {\n", ind, ind, ind)
		} else {
			g.pf("%s    elem_kind = table_reader_get8( &sub );\n", ind)
			g.pf("%s    if ( !table_reader_leb( &sub, &count ) ) { r->report->malformed = 1; break; }\n%s    {\n", ind, ind)
		}
		g.pf("%s        if ( elem_kind != %d )\n%s        {\n", ind, kind, ind)
		g.pf("%s            if ( !(%s) ) { r->report->kind_mismatch++; break; }\n%s            r->report->widened++;\n%s        }\n", ind, wireElementWidenTest(f, kind, "elem_kind"), ind, ind)
		g.retainReplace(f, ind+"        ")
		g.pf("%s        kept = count; if ( kept > %d ) { kept = %d; r->report->clamped++; }\n", ind, bound, bound)
		if f.Array == ir.ArrayCounted {
			g.pf("%s        int32_t previous_count = %s_count;\n", ind, dst)
		}
		if count != "" {
			g.pf("%s        %s = 0;\n", ind, count)
		}
		g.pf("%s        for ( i = 0; i < kept; i++ )\n%s        {\n", ind, ind)
		g.retainIndex(f, "i", ind+"            ")
		bad := "r->report->malformed = 1; goto end_" + f.Name + ";"
		switch kind {
		case tkTable:
			g.pf("%s            TableReader elem;\n%s            if ( !table_reader_span( &sub, &elem ) ) { %s }\n", ind, ind, bad)
			g.pf("%s            %s( &elem, &%s[i] );\n", ind, g.api(f.Type.Name, "load_body"), dst)
			g.wireRefusalCheck(ind + "            ")
			g.pf("%s            if(elem.offset!=elem.size) { r->report->malformed=1; %s(&%s[i]); }\n", ind, g.api(f.Type.Name, "reset"), dst)
		case tkUnion:
			g.pf("%s            if ( !%s( &sub, &%s[i], 1 ) ) { %s }\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "load"), dst, bad)
		default:
			g.wireScalarRead(f, dst+"[i]", "sub", "elem_kind", ind+"            ", bad)
		}
		if count != "" {
			g.pf("%s            %s = (int32_t) i + 1;\n", ind, count)
		}
		g.pf("%s        }\n%s        end_%s: ;\n", ind, ind, f.Name)
		if f.Array == ir.ArrayCounted {
			g.wireCountedTailReset(f, dst, ind+"        ")
		}
		g.pf("%s    }\n%s}\n", ind, ind)
	case kind == tkUnion:
		g.pf("%sif ( !%s( r, &%s, 0 ) ) { return 0; }\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "load"), dst)

	case kind == tkTable:
		g.pf("%sTableReader sub;\n%sif ( !table_reader_span( r, &sub ) ) { r->report->malformed = 1; return 0; }\n", ind, ind)
		g.pf("%s%s( &sub, &%s );\n", ind, g.api(f.Type.Name, "load_body"), dst)
		g.wireRefusalCheck(ind)
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
	g.pf("%s    if ( elem_kind != %d ) { if ( !(%s) ) { r->report->kind_mismatch++; break; } r->report->widened++; }\n", ind, kind, wireElementWidenTest(f, kind, "elem_kind"))
	g.pf("%s    for ( i = 0; i < count; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t key_ref, key; int32_t slot = -1; TableReader elem;\n", ind)
	g.pf("%s        if ( !table_reader_leb( &sub, &key_ref ) || key_ref == 0 || key_ref > sub.id_count || !table_reader_span( &sub, &elem ) ) { r->report->malformed = 1; break; }\n", ind)
	g.pf("%s        key = table_reader_id_at( &sub, key_ref );\n%s        switch ( key )\n%s        {\n", ind, ind, ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%s            case 0x%016xull: slot = %d; break;\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	g.pf("%s            default: "+g.unknownEvent()+" break;\n%s        }\n%s        if ( slot < 0 ) { continue; }\n", ind, ind, ind)
	g.retainIndex(f, "slot", ind+"        ")
	dst := fmt.Sprintf("value->%s[slot]", f.Name)
	if kind == tkTable {
		g.pf("%s        %s( &elem, &%s );\n", ind, g.api(f.Type.Name, "load_body"), dst)
		g.wireRefusalCheck(ind + "        ")
		g.pf("%s        if(elem.offset!=elem.size) { r->report->malformed=1; %s(&%s); }\n", ind, g.api(f.Type.Name, "reset"), dst)
	} else {
		g.wireScalarRead(f, dst, "elem", "elem_kind", ind+"        ", "r->report->malformed = 1; goto next_"+f.Name+";")
	}
	if kind != tkTable {
		g.pf("%s        next_%s: ;\n", ind, f.Name)
	}
	g.pf("%s    }\n%s}\n", ind, ind)
}

// Keep value-initialized storage past a replacement's decoded prefix (#725).
func (g *tableGen) wireCountedTailReset(f *ir.Field, dst, ind string) {
	g.pf("%s{ uint32_t tail; for (tail=(uint32_t)%s_count;tail<(uint32_t)previous_count && tail<%d;tail++) {\n", ind, dst, f.ArrayBound)
	if st, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
		g.pf("%s %s(&%s[tail]);\n", ind, g.api(st.Name, "reset"), dst)
	} else {
		g.pf("%s memset(&%s[tail],0,sizeof(%s[tail]));\n", ind, dst, dst)
	}
	g.pf("%s} }\n", ind)
}
