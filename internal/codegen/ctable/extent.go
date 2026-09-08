package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitExtentDeclarations(members []*ir.Struct) {
	for _, st := range g.varMembers(members) {
		g.pf("static SCHEMA_UNUSED int %s(TableNumbering * n,const void * storage,uint8_t * target,int64_t * at);\n", g.sym(st.Name, "extent"))
		g.pf("static SCHEMA_UNUSED int %s(TableReader r,int64_t * at);\n", g.sym(st.Name, "wire_extent"))
		g.pf("static SCHEMA_UNUSED int64_t %s(const TableReader * r);\n", g.sym(st.Name, "wire_storage"))
	}
	for _, un := range g.fileWireUnions() {
		g.pf("static SCHEMA_UNUSED int %s(TableReader * r,int64_t * at);\n", g.sym(un.Name, "wire_extent"))
	}
}

func (g *tableGen) emitExtentBodies(members []*ir.Struct) {
	for _, un := range g.fileWireUnions() {
		g.pf("static SCHEMA_UNUSED int %s(TableReader * r,int64_t * at)\n{\n    uint64_t ref,id; TableReader body;\n    (void)at;\n", g.sym(un.Name, "wire_extent"))
		g.pf("    if(!table_reader_leb(r,&ref)) { r->offset=r->size; return 1; } if(ref==0) { return 1; }\n    if(ref>r->id_count || !table_reader_has(r,1)) { r->offset=r->size; return 1; } id=table_reader_id_at(r,ref); r->offset++;\n    if(!table_reader_span(r,&body)) { r->offset=r->size; return 1; }\n    switch(id) {\n")
		for _, v := range un.Variants {
			if !v.Void() {
				g.pf("    case UINT64_C(0x%016x): {\n", ir.TableWireId(v.WireName()))
				step := 0
				g.emitWireExtentField(v.F, "body", true, "        ", &step)
				g.pf("        } break;\n")
			}
		}
		g.pf("    default: break;\n    } return 1;\n}\n")
	}
	for _, st := range g.varMembers(members) {
		g.pf("static SCHEMA_UNUSED int %s(TableNumbering * n,const void * storage,uint8_t * target,int64_t * at)\n{\n    const %s * value=(const %s *)storage; %s * out=(%s *)(void *)target;\n    (void)n; (void)value; (void)out; (void)at;\n", g.sym(st.Name, "extent"), st.Name, st.Name, st.Name, st.Name)
		step := 0
		guards := tableGuardExprs(st)
		for _, f := range st.Fields {
			if guard := guards[f.Name]; guard != "" {
				g.pf("    if(%s) {\n", guard)
			}
			g.emitExtentField(f, "value->"+f.Name, "out->"+f.Name, "    ", &step)
			if guards[f.Name] != "" {
				g.pf("    }\n")
			}
		}
		g.pf("    return 1;\n}\n")
		g.pf("static SCHEMA_UNUSED int %s(TableReader r,int64_t * at)\n{\n    (void)at;\n    for(;;) { uint64_t ref,id; uint8_t kind;\n        if(!table_reader_leb(&r,&ref) || ref==0 || ref>r.id_count || !table_reader_has(&r,1)) { return 1; }\n        id=table_reader_id_at(&r,ref); kind=table_reader_get8(&r);\n        switch(id) {\n", g.sym(st.Name, "wire_extent"))
		step = 0
		for _, f := range st.Fields {
			if !g.fieldHasExtent(f) {
				continue
			}
			kind := ir.TableWireScalarKind(f)
			if f.Array != ir.ArrayNone || f.IsMap() {
				kind = tkArray
			}
			if f.KeyEnum != "" {
				kind = tkKeyed
			}
			g.pf("        case UINT64_C(0x%016x): if(kind==%d) {\n", ir.TableFieldWireId(f), kind)
			g.emitWireExtentField(f, "r", false, "            ", &step)
			g.pf("            continue; } break;\n")
		}
		g.pf("        default: break;\n        }\n        if(!table_reader_skip(&r,kind)) { return 1; }\n    }\n}\n")
		g.pf("static SCHEMA_UNUSED int64_t %s(const TableReader * r)\n{\n    int64_t at=sizeof(%s); if(!%s(*r,&at) || at>INT64_MAX-kTableAlign) { return -1; } return table_align_up64(at);\n}\n", g.sym(st.Name, "wire_storage"), st.Name, g.sym(st.Name, "wire_extent"))
	}
}

func (g *tableGen) fieldHasExtent(f *ir.Field) bool {
	if f.IsMap() || f.IsList() {
		return true
	}
	if f.Type.Pointer {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return g.isVar(ref.Name)
	case *ir.Union:
		for _, v := range ref.Variants {
			if !v.Void() && g.fieldHasExtent(v.F) {
				return true
			}
		}
	}
	return false
}

func (g *tableGen) emitExtentField(f *ir.Field, src, dst, ind string, step *int) {
	if f.Type.Optional {
		g.pf("%sif(%s_present) {\n", ind, src)
	}
	switch {
	case f.IsList() || f.IsMap():
		*step++
		label := fmt.Sprintf("extent%d", *step)
		typ := g.sequenceType(f)
		elem := sequenceElement(f)
		g.pf("%s{ TableSequenceCursor %s=%s; int32_t %s_i; uint8_t * %s_array=NULL;\n", ind, label, g.sequenceCursor(f, "n", src), label, label)
		g.pf("%s  if(!%s.ok || !table_extent_reserve(at,%s.count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s))) { return 0; }\n", ind, label, src, typ, typ)
		g.pf("%s  if(target!=NULL) { %s_array=n->pack_base+*at-(int64_t)%s.count*sizeof(%s); %s.items.value=%s.count ? (int64_t)(%s_array-(uint8_t *)(void *)&%s.items) : 0; }\n", ind, label, src, typ, dst, src, label, dst)
		g.pf("%s  for(%s_i=0;%s_i<%s.count;%s_i++) {\n", ind, label, label, src, label)
		g.pf("%s    const %s * %s_in=(const %s *)table_sequence_next(&%s); %s * %s_out=target ? (%s *)(void *)%s_array+%s_i : NULL;\n", ind, typ, label, typ, label, typ, label, typ, label, label)
		g.pf("%s    if(%s_in==NULL) { return 0; } if(%s_out!=NULL) { memcpy(%s_out,%s_in,sizeof(%s)); }\n", ind, label, label, label, label, typ)
		g.emitExtentField(elem, "(*"+label+"_in)", "(*"+label+"_out)", ind+"    ", step)
		g.pf("%s  }\n%s}\n", ind, ind)
	case f.Array != ir.ArrayNone:
		*step++
		index := fmt.Sprintf("extent_i%d", *step)
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = src + "_count"
		}
		g.pf("%s{ int32_t %s; for(%s=0;%s<%s;%s++) {\n", ind, index, index, index, count, index)
		e := *f
		e.Array = ir.ArrayNone
		e.Type.Optional = false
		g.emitExtentField(&e, src+"["+index+"]", dst+"["+index+"]", ind+"    ", step)
		g.pf("%s} }\n", ind)
	case f.Type.Pointer:
		g.pf("%sif(target!=NULL) { const void * node=table_ref_at(n->ctx,&%s); uint64_t index=table_number_find(n,node);\n", ind, src)
		g.pf("%s    if(node!=NULL && index==0) { return 0; } %s.value=index ? (int64_t)((n->pack_base+n->entries[index-1].packed_offset)-(uint8_t *)(void *)&%s) : 0; }\n", ind, dst, dst)
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if g.needsWalkers(ref.Name) {
				g.pf("%sif(!%s(n,&%s,target ? (uint8_t *)(void *)&%s : NULL,at)) { return 0; }\n", ind, g.sym(ref.Name, "extent"), src, dst)
			}
		case *ir.Union:
			g.pf("%sswitch(%s.type) {\n", ind, src)
			for _, v := range ref.Variants {
				if !v.Void() {
					g.pf("%scase %s: {\n", ind, enumConst(ref.Name+"Type", v.Name))
					g.emitExtentField(v.F, armValue(src, v), armValue(dst, v), ind+"    ", step)
					g.pf("%s    break; }\n", ind)
				}
			}
			g.pf("%sdefault: break;\n%s}\n", ind, ind)
		}
	}
	if f.Type.Optional {
		g.pf("%s}\n", ind)
	}
}

// Only framing contributes to LoadMeasure. Damage stops a local scan; a count
// that cannot fit its storage or enclosing L refuses before any allocation.
func (g *tableGen) emitWireExtentField(f *ir.Field, r string, bounded bool, ind string, step *int) {
	if !g.fieldHasExtent(f) {
		return
	}
	if !f.IsList() && !f.IsMap() && f.Array == ir.ArrayNone && !f.Type.Pointer {
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			g.pf("%sif(!%s(&%s,at)) { return 0; }\n", ind, g.sym(un.Name, "wire_extent"), r)
			return
		}
	}
	*step++
	b := fmt.Sprintf("extent_body%d", *step)
	g.pf("%s{ TableReader %s;\n", ind, b)
	if bounded {
		g.pf("%s  %s=%s;\n", ind, b, r)
	} else {
		g.pf("%s  if(!table_reader_span(&%s,&%s)) { return 1; }\n", ind, r, b)
	}
	if f.Array != ir.ArrayNone || f.IsMap() {
		e := sequenceElement(f)
		kind := ir.TableWireScalarKind(e)
		done := b + "_done"
		g.pf("%s  if(%s.size>=2) { uint64_t %s_count;\n", ind, b, b)
		g.pf("%s    uint8_t wire_kind=table_reader_get8(&%s);\n", ind, b)
		g.pf("%s    if((wire_kind!=%d && !(%s)) || !table_reader_leb(&%s,&%s_count)) { goto %s; }\n", ind, kind, wireElementWidenTest(f, kind, "wire_kind"), b, b, done)
		if f.IsList() || f.IsMap() {
			typ := g.sequenceType(f)
			g.pf("%s    int64_t floor=%d;\n", ind, sequenceFloor(f))
			if ir.TableKindSigned(kind) || kind >= tkU8 && kind <= tkU64 || kind == tkF64 {
				for source := 1; source <= 29; source++ {
					if ir.TableKindWidens(source, kind) {
						g.pf("%s    if(wire_kind==%d)floor=%d;\n", ind, source, tableKindWidth(source))
					}
				}
			}
			g.pf("%s    if(%s_count>INT32_MAX) { %s.report->reason=11; return 0; }\n", ind, b, b)
			g.pf("%s    if(%s_count>(uint64_t)((%s.size-%s.offset)/floor)) { %s.report->reason=10; return 0; }\n", ind, b, b, b, b)
			g.pf("%s    if(!table_extent_reserve(at,(int64_t)%s_count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s))) { return 0; }\n", ind, b, typ, typ)
		}
		if g.fieldHasExtent(e) {
			g.pf("%s    { uint64_t i; for(i=0;i<%s_count;i++) {\n", ind, b)
			g.pf("%s      if(!table_reader_has(&%s,1)) { goto %s; }\n", ind, b, done)
			if f.KeyEnum != "" {
				g.pf("%s      uint64_t key; if(!table_reader_leb(&%s,&key)) { goto %s; }\n", ind, b, done)
			}
			switch ref := e.Type.Ref.(type) {
			case *ir.Struct:
				g.pf("%s      TableReader item; if(!table_reader_span(&%s,&item)) { goto %s; }\n", ind, b, done)
				g.pf("%s      if(!%s(item,at)) { return 0; }\n", ind, g.sym(ref.Name, "wire_extent"))
			case *ir.Union:
				g.pf("%s      if(!%s(&%s,at)) { return 0; }\n", ind, g.sym(ref.Name, "wire_extent"), b)
			}
			g.pf("%s    } }\n", ind)
		}
		g.pf("%s  }\n%s%s: ;\n", ind, ind, done)

	} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && g.needsWalkers(ref.Name) {
		g.pf("%s  if(!%s(%s,at)) { return 0; }\n", ind, g.sym(ref.Name, "wire_extent"), b)
	}
	g.pf("%s}\n", ind)
}
