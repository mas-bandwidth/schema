package ctable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func armCompanioned(v ir.UnionVariant) bool {
	if v.Void() || v.F.Type.Pointer && v.F.Array == ir.ArrayNone {
		return false
	}
	return v.F.Array == ir.ArrayCounted || v.F.Type.Kind == ir.TString || v.F.Type.Kind == ir.TWString || v.F.Type.Kind == ir.TBytes
}
func armValue(base string, v ir.UnionVariant) string {
	base += ".as." + v.Name
	if armCompanioned(v) {
		base += ".value"
	}
	return base
}
func armWireKind(v ir.UnionVariant) int {
	if v.Void() {
		return ir.TableKindNoPayload
	}
	return ir.TableWireFieldKind(v.F)
}
func (g *tableGen) unionWireName(un *ir.Union, verb string) string {
	return g.sym(un.Name, "wire_"+verb)
}

// Storage dependencies are visited through union arms as well as table fields.
// Pointer edges do not participate: a reference's storage is already complete.
func (g *tableGen) emitTableShapes(members []*ir.Struct) {
	done := map[string]bool{}
	var field func(*ir.Field)
	var decl func(string)
	field = func(f *ir.Field) {
		if f.IsMap() {
			decl(f.MapEntry.Name)
			return
		}
		if !f.Type.Pointer && f.Type.Kind == ir.TNamed {
			decl(f.Type.Name)
		}
	}
	decl = func(name string) {
		if done[name] || g.declBase(name) != g.file.Base {
			return
		}
		done[name] = true
		if st := g.unit.Tables[name]; st != nil {
			for _, f := range st.Fields {
				field(f)
			}
			g.emitTableStruct(st)
		} else if un := g.unit.TableUnions[name]; un != nil {
			for _, v := range un.Variants {
				if !v.Void() {
					field(v.F)
				}
			}
			g.emitTableUnion(un)
		}
	}
	for _, st := range members {
		if st.IsTable {
			decl(st.Name)
		}
	}
	for _, un := range g.file.TableUnions {
		decl(un.Name)
	}
}
func (g *tableGen) emitTableUnion(un *ir.Union) {
	tag := un.Name + "Type"
	g.pf("typedef uint%d_t %s;\n#define %s 0\n", ir.StorageBitsFor(int64(len(un.Variants))), tag, enumNoneConst(tag))
	for i, v := range un.Variants {
		g.pf("#define %s %d\n", enumConst(tag, v.Name), i+1)
	}
	g.pf("#define %s %d\n#define %s %d\n", enumConst(tag, "Count"), len(un.Variants), enumConst(tag, "Max"), len(un.Variants))
	g.pf("typedef struct %s {\n    %s type;\n", un.Name, tag)
	payloads := 0
	for _, v := range un.Variants {
		if !v.Void() {
			payloads++
		}
	}
	if payloads > 0 {
		g.pf("    union {\n")
		for _, v := range un.Variants {
			if v.Void() {
				continue
			}
			f := *v.F
			if armCompanioned(v) {
				g.pf("        struct {\n")
				f.Name = "value"
				g.emitTableStorageField(&f)
				g.pf("        } %s;\n", v.Name)
			} else {
				f.Name = v.Name
				g.emitTableStorageField(&f)
			}
		}
		g.pf("    } as;\n")
	}
	g.pf("} %s;\n\n", un.Name)
}
func (g *tableGen) fileWireUnions() []*ir.Union {
	var out []*ir.Union
	names := ir.TableClosureVocabulary(g.unit)
	for _, d := range g.file.Decls {
		if un, ok := d.(*ir.Union); ok && names[un.Name] {
			out = append(out, un)
		}
	}
	out = append(out, g.file.TableUnions...)
	return out
}
func (g *tableGen) emitUnionWireDeclarations(unions []*ir.Union) {
	for _, un := range unions {
		g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value );\n", g.unionWireName(un, "save"), un.Name)
		g.pf("static SCHEMA_UNUSED int %s( TableReader * r, %s * value, int element );\n", g.unionWireName(un, "load"), un.Name)
	}
}

func (g *tableGen) emitUnionWire(un *ir.Union) {
	// One payload writer per arm: lengths are measured from the current
	// vocabulary, exactly as a field's framed payload is measured.
	for ordinal, v := range un.Variants {
		if v.Void() {
			continue
		}
		g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value )\n{\n", g.sym(un.Name, "wire_arm_"+v.Name), un.Name)
		if g.retain {
			g.pf("    retention=table_retain_step(retention,%d,0);\n", ordinal)
		}
		expr := armValue("(*value)", v)
		g.wireBarePayload(v.F, expr, "    ")
		g.pf("    return !w->overflow;\n}\n\n")
	}
	g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value )\n{\n    switch ( value->type )\n    {\n", g.unionWireName(un, "save"), un.Name)
	g.pf("        case %s: table_writer_leb( w, 0 ); break;\n", enumNoneConst(un.Name+"Type"))
	for _, v := range un.Variants {
		g.pf("        case %s:\n", enumConst(un.Name+"Type", v.Name))
		g.pf("            table_writer_id( w, 0x%016xull ); table_writer_put8( w, %d );\n", ir.TableWireId(v.WireName()), armWireKind(v))
		if v.Void() {
			g.pf("            table_writer_leb( w, 0 );\n")
		} else {
			g.wireFrame(fmt.Sprintf("%s( w, value )", g.sym(un.Name, "wire_arm_"+v.Name)), "            ")
		}
		g.pf("            break;\n")
	}
	g.pf("        default: return 0;\n    }\n    return !w->overflow;\n}\n\n")
	g.pf("static SCHEMA_UNUSED int %s( TableReader * r, %s * value, int element )\n{\n", g.unionWireName(un, "load"), un.Name)
	g.pf("    uint64_t ref; uint8_t kind; TableReader arm;\n")
	if g.retain {
		g.pf("    table_retain_discard(retention,0,0);\n")
	}
	g.pf("    if ( !table_reader_leb( r, &ref ) || ref > r->id_count ) { r->report->malformed = 1; return 0; }\n    if ( ref == 0 ) { value->type = 0; return 1; }\n    if ( element ) { value->type = 0; }\n    if ( !table_reader_has( r, 1 ) ) { r->report->malformed = 1; return 0; }\n    kind = table_reader_get8( r );\n    if ( !table_reader_span( r, &arm ) ) { r->report->malformed = 1; return 0; }\n    value->type = 0;\n    switch ( table_reader_id_at( r, ref ) )\n    {\n")
	for ordinal, v := range un.Variants {
		g.pf("        case 0x%016xull:\n        {\n", ir.TableWireId(v.WireName()))
		k := armWireKind(v)
		g.pf("            if ( kind != %d && !table_kind_widens( kind, %d ) ) { r->report->kind_mismatch++; break; }\n", k, k)
		if !v.Void() && ir.ArmWireFixedWidth(v.F) > 0 {
			g.pf("            switch ( kind )\n            {\n")
			for source := 1; source <= 29; source++ {
				if source == k || ir.TableKindWidens(source, k) {
					g.pf("                case %d: if ( arm.size != %d ) { r->report->malformed = 1; goto next_arm_%s; } break;\n", source, tableKindWidth(source), v.Name)
				}
			}
			g.pf("                default: break;\n            }\n")
		}
		if !v.Void() {
			if g.retain {
				g.pf("            retention=table_retain_step(retention,%d,0);\n", ordinal)
			}
			g.wireReadArm(v, armValue("(*value)", v), "            ")
			g.wireRefusalCheck("            ")
		}
		if k != tkUnion {
			g.pf("            if ( arm.offset != arm.size ) { r->report->malformed = 1; break; }\n")
		}
		g.pf("            if ( kind != %d ) { r->report->widened++; }\n", k)
		g.pf("            value->type = %s;\n", enumConst(un.Name+"Type", v.Name))
		if !v.Void() && ir.ArmWireFixedWidth(v.F) > 0 {
			g.pf("            next_arm_%s: ;\n", v.Name)
		}
		g.pf("            break;\n        }\n")
	}
	g.pf("        default: r->report->unknown++; break;\n    }\n    return 1;\n}\n\n")
}

// An arm's L is its field payload's frame: no second length surrounds it.
func (g *tableGen) wireBarePayload(f *ir.Field, expr, ind string) {
	switch {
	case f.IsList() || f.IsMap():
		g.emitSequenceWrite(f, expr)
	case f.Type.Kind == ir.TString && !f.Type.Blob():
		g.pf("%sif ( %s_length < 0 || %s_length > %d ) { return 0; }\n", ind, expr, expr, f.Type.Size)
		g.pf("%stable_writer_raw( w, %s, %s_length );\n", ind, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%sif ( %s_length < 0 || %s_length > %d ) { return 0; }\n", ind, expr, expr, f.Type.Size)
		g.pf("%s{ int32_t i; for ( i=0; i<%s_length; i++ ) { table_writer_put16( w, %s[i] ); } }\n", ind, expr, expr)
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		g.pf("%sif ( %s_length < 0 || %s_length > %d ) { return 0; }\n", ind, expr, expr, f.Type.Size)
		g.pf("%stable_writer_put8( w, 6 ); table_writer_leb( w, (uint64_t)%s_length ); table_writer_raw( w, %s, %s_length );\n", ind, expr, expr, expr)
	case f.Array != ir.ArrayNone:
		n := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			n = expr + "_count"
			g.pf("%sif ( %s < 0 || %s > %d ) { return 0; }\n", ind, n, n, f.ArrayBound)
		}
		g.pf("%s{ int32_t i; table_writer_put8( w, %d ); table_writer_leb( w, (uint64_t)(%s) );\n", ind, ir.TableWireScalarKind(f), n)
		g.pf("%s  for ( i=0; i<%s; i++ ) {\n", ind, n)
		g.retainIndex(f, "i", ind+"    ")
		g.wirePayload(f, expr+"[i]", ind+"    ")
		g.pf("%s  }\n%s}\n", ind, ind)
	case ir.TableWireScalarKind(f) == tkTable:
		g.pf("%sif ( !%s( w, &%s ) ) { return 0; }\n", ind, g.api(f.Type.Name, "save_body"), expr)
	default:
		g.wirePayload(f, expr, ind)
	}
}
func (g *tableGen) wireReadArm(v ir.UnionVariant, dst, ind string) {
	f := v.F
	k := armWireKind(v)
	bad := "r->report->malformed = 1; break;"
	switch {
	case v.Body():
		g.pf("%s%s( &arm, &%s );\n", ind, g.api(v.Type, "load_body"), dst)
	case f.Type.Kind == ir.TString && !f.Type.Blob():
		g.pf("%suint64_t keep;\n%sif ( !table_wire_utf8( arm.buffer, (uint64_t)arm.size ) ) { %s }\n", ind, ind, bad)
		g.pf("%skeep = table_wire_utf8_clamp( arm.buffer, (uint64_t)arm.size, %d ); if ( keep != (uint64_t)arm.size ) { r->report->clamped++; }\n", ind, f.Type.Size)
		g.pf("%smemcpy( %s, arm.buffer, (size_t)keep ); %s[keep]=0; %s_length=(int32_t)keep; arm.offset=arm.size;\n", ind, dst, dst, dst)
	case f.Type.Kind == ir.TWString:
		g.pf("%sint64_t keep, i;\n%sif ( arm.size%%2 || !table_wire_utf16( arm.buffer, arm.size/2 ) ) { %s }\n", ind, ind, bad)
		g.pf("%skeep=table_wire_utf16_clamp( arm.buffer, arm.size/2, %d ); if ( keep != arm.size/2 ) { r->report->clamped++; }\n", ind, f.Type.Size)
		g.pf("%sfor ( i=0; i<keep; i++ ) { %s[i]=table_wire_utf16_unit( arm.buffer,i ); } %s[keep]=0; %s_length=(int32_t)keep; arm.offset=arm.size;\n", ind, dst, dst, dst)
	case f.IsMap() || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes && !f.Type.Blob():
		// Reuse the field's array payload walk with the arm's already-bounded
		// span. No cast to a synthetic C storage type is involved.
		g.pf("%smemset( &%s, 0, sizeof( %s ) );\n", ind, strings.TrimSuffix(dst, ".value"), strings.TrimSuffix(dst, ".value"))
		if f.Array != ir.ArrayNone && !f.IsList() && !f.Type.Pointer && cSelfInit(f.Type) {
			g.emitTableResetArray(dst, fmt.Sprint(f.ArrayBound), g.cFieldType(f.Type), true, f)
		}
		kind := ir.TableWireScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		g.wireReadFieldPayload(f, kind, dst, "arm")
	case k == tkUnion:
		g.pf("%sif ( !%s( &arm, &%s, 0 ) ) {\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "load"), dst)
		g.wireRefusalCheck(ind + "    ")
		g.pf("%s    %s }\n", ind, bad)
	default:
		// A short payload is confined to this arm and leaves it unset.
		g.wireScalarRead(f, dst, "arm", "kind", ind, "r->report->malformed = 1; goto bad_arm_"+v.Name+";")
		g.pf("%sgoto read_arm_%s;\n%sbad_arm_%s: break;\n%sread_arm_%s: ;\n", ind, v.Name, ind, v.Name, ind, v.Name)
	}
}

func (g *tableGen) emitUnionArmDescriptors(un *ir.Union) {
	if g.descriptorUnions == nil {
		g.descriptorUnions = map[string]bool{}
	}
	key := fmt.Sprintf("%s:%t", un.Name, g.outside)
	if g.descriptorUnions[key] {
		return
	}
	g.descriptorUnions[key] = true
	st := &ir.Struct{Name: un.Name}
	for _, v := range un.Variants {
		if v.Void() || v.Body() {
			continue
		}
		f := *v.F
		f.Name = v.Name
		g.emitFieldVocabulary(st, &f)
		g.emitTagsStatic(g.vocabularySymbol(st.Name, f.Name, "tags"), f.Tags)
		g.pf("static const TableFieldInfo %s[] = {\n", g.armDescriptorSymbol(un.Name, v.Name))
		member := "as." + v.Name
		if armCompanioned(v) {
			member += ".value"
		}
		g.emitFieldDescriptorAt(st, &f, "", member)
		g.pf("};\n")
	}
}

func (g *tableGen) armDescriptorSymbol(owner, arm string) string {
	return g.sym(owner, "arm_field_"+arm)
}
