package ctable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"slices"
)

// Cooks are canonical artifacts. Native record padding and byte order never
// enter the file: zero the complete output and store each declared piece.
const tableCookWriteRuntime = `
#ifndef SCHEMA_TABLE_COOK_WRITE_RUNTIME
#define SCHEMA_TABLE_COOK_WRITE_RUNTIME
struct TableNumbering;
typedef enum TableByteOrder { TableByteOrder_Little=1, TableByteOrder_Big=2 } TableByteOrder;
static SCHEMA_UNUSED void table_cook_put(uint8_t * at,uint64_t value,int width,int order)
{
    int i; for(i=0;i<width;i++) { at[i]=(uint8_t)(value >> (8*(order==TableByteOrder_Little ? i : width-1-i))); }
}
static SCHEMA_UNUSED void table_cook_put128(uint8_t * at,uint64_t lo,uint64_t hi,int order)
{
    table_cook_put(at,order==TableByteOrder_Little ? lo : hi,8,order);
    table_cook_put(at+8,order==TableByteOrder_Little ? hi : lo,8,order);
}
static SCHEMA_UNUSED void table_cook_bytes(uint8_t * at,const void * source,int64_t used,int64_t capacity)
{ if(used>0) { memcpy(at,source,(size_t)(used<capacity ? used : capacity)); } }
static SCHEMA_UNUSED void table_cook_units(uint8_t * at,const uint16_t * source,int64_t used,int64_t capacity,int order)
{
    int64_t i, count=used<capacity ? used : capacity;
    for(i=0;i<count;i++) { table_cook_put(at+i*2,source[i],2,order); }
}
static SCHEMA_UNUSED void table_cook_header(uint8_t * raw,uint64_t version,int64_t bytes,int64_t count,int64_t align,int order)
{
    table_cook_put(raw,table_cook_magic,8,order); table_cook_put(raw+8,version,8,order);
    table_cook_put(raw+16,order==TableByteOrder_Big ? 2 : 1,8,order);
    table_cook_put(raw+24,(uint64_t)bytes,8,order); table_cook_put(raw+32,(uint64_t)count*16,8,order);
    table_cook_put(raw+40,(uint64_t)align,8,order);
}
#endif
`

const tableCookGraphRuntime = `
#ifndef SCHEMA_TABLE_COOK_GRAPH_RUNTIME
#define SCHEMA_TABLE_COOK_GRAPH_RUNTIME
static SCHEMA_UNUSED int table_cook_ref(TableNumbering * n,uint8_t * at,const TableRef * slot,int order)
{
    const void * pointee=table_ref_at(n->ctx,slot);
    uint64_t index=table_number_find(n,pointee);
    if(pointee!=NULL && index==0) { return 0; }
    table_cook_put(at,index ? n->entries[index-1].packed_offset-(uint64_t)(at-n->pack_base) : 0,8,order);
    return 1;
}
static SCHEMA_UNUSED int64_t table_cook_layout(TableNumbering * n,int64_t * alignment,int64_t * data_bytes)
{
    uint64_t i;
    int64_t total=0, align=8;
    for(i=0;i<n->count;i++) {
        TableNodeEntry * entry=&n->entries[i]; const TableNodeType * type=entry->type;
        int64_t size=type->size, node_align=type->alignment;
        if(type->blob) { size=8+(int64_t)((const TableBlob *)entry->value)->length+(type->blob==2); }
        else if(type->has_extent) {
            int64_t extent=0;
            if(!type->cook_extent(n,entry->value,NULL,NULL,&extent,TableByteOrder_Little) || size>INT64_MAX-7) { return -1; }
            size=(size+7)&~INT64_C(7); if(extent>INT64_MAX-size) { return -1; } size+=extent;
        }
        if(node_align<1 || total>INT64_MAX-(node_align-1)) { return -1; }
        total=(total+node_align-1)&~(node_align-1);
        entry->packed_offset=(uint64_t)total;
        if(size<0 || size>INT64_MAX-total) { return -1; } total+=size;
        if(node_align>align) { align=node_align; }
    }
    if(total>INT64_MAX-(align-1)) { return -1; } total=(total+align-1)&~(align-1);
    *alignment=align; *data_bytes=total;
    if(total>INT64_MAX-64 || n->count>(uint64_t)(INT64_MAX-64-total)/16) { return -1; }
    return 64+total+(int64_t)n->count*16;
}
static SCHEMA_UNUSED int64_t table_graph_cook(const TableCtx * ctx,const void * root,const TableNodeType * type,
    void * output,uint64_t capacity,int order,TableAllocator allocator,uint64_t version,int measure)
{
    TableNumbering n; int64_t result=-1,align=8,bytes=0,need; uint64_t i; uint8_t * raw=(uint8_t *)output;
    if(!table_number_build(&n,ctx,root,type,allocator)) { table_number_dispose(&n); return -1; }
    need=table_cook_layout(&n,&align,&bytes); if(need<0) { goto done; }
    if(measure) { result=need; goto done; }
    if(raw==NULL || (uint64_t)need>capacity || (uint64_t)need>SIZE_MAX) { goto done; }
    memset(raw,0,(size_t)need); n.pack_base=raw+64;
    for(i=0;i<n.count;i++) {
        const TableNodeEntry * entry=&n.entries[i]; uint8_t * at=n.pack_base+entry->packed_offset;
        if(entry->type->blob) {
            const TableBlob * blob=(const TableBlob *)entry->value;
            table_cook_put(at,blob->length,4,order); table_cook_bytes(at+8,blob+1,blob->length,blob->length);
        } else {
            int64_t extent=0;
            if(!entry->type->cook_body(&n,at,entry->value,order)) { goto done; }
            if(entry->type->has_extent) {
                int64_t expected=0,record=(entry->type->size+7)&~INT64_C(7);
                if(!entry->type->cook_extent(&n,entry->value,NULL,NULL,&expected,order) ||
                   !entry->type->cook_extent(&n,entry->value,at,at+record,&extent,order) || extent!=expected) { goto done; }
            }
        }
        table_cook_put(n.pack_base+bytes+i*16,entry->packed_offset,8,order);
        table_cook_put(n.pack_base+bytes+i*16+8,entry->type->id,8,order);
    }
    /* A failed body must never leave a recognizable cook header. */
    table_cook_header(raw,version,bytes,(int64_t)n.count,align,order); result=need;
done:
    table_number_dispose(&n); return result;
}
#endif
`

func cookAlignUp(v, a int64) int64 { return (v + a - 1) / a * a }
func (g *tableGen) hasCookExtent(st *ir.Struct) bool {
	return slices.ContainsFunc(st.Fields, g.cookFieldExtent)
}
func (g *tableGen) cookFieldExtent(f *ir.Field) bool {
	if f.IsList() || f.IsMap() {
		return true
	}
	if f.Type.Pointer {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return g.hasCookExtent(ref)
	case *ir.Union:
		for _, v := range ref.Variants {
			if !v.Void() && g.cookFieldExtent(v.F) {
				return true
			}
		}
	}
	return false
}
func (g *tableGen) cookBodySignature(st *ir.Struct) string {
	return fmt.Sprintf("static SCHEMA_UNUSED int %s(struct TableNumbering * n,uint8_t * at,const void * storage,int order)", g.sym(st.Name, "cook_body"))
}
func (g *tableGen) cookExtentSignature(st *ir.Struct) string {
	return fmt.Sprintf("static SCHEMA_UNUSED int %s(TableNumbering * n,const void * storage,uint8_t * record,uint8_t * extent,int64_t * at,int order)", g.sym(st.Name, "cook_extent"))
}
func (g *tableGen) emitCookWriteDeclarations(members []*ir.Struct) {
	for _, st := range members {
		g.pf("%s;\n", g.cookBodySignature(st))
		if g.hasCookExtent(st) {
			g.pf("%s;\n", g.cookExtentSignature(st))
		}
	}
}
func (g *tableGen) emitCookWriteSurface(members []*ir.Struct) {
	for _, st := range members {
		g.pf("%s\n{\n    const %s * value=(const %s *)storage;\n    (void)n; (void)at; (void)value; (void)order;\n", g.cookBodySignature(st), st.Name, st.Name)
		step := 0
		for _, fl := range ir.RecordLayout(g.unit, st).Fields {
			g.cookField(fl.Field, "value->"+fl.Field.Name, fmt.Sprintf("at+%d", fl.Offset), fl.Offset, "    ", &step)
		}
		g.pf("    return 1;\n}\n")
		if g.hasCookExtent(st) {
			g.pf("%s\n{\n    const %s * value=(const %s *)storage;\n    (void)n; (void)value; (void)record; (void)extent; (void)at; (void)order;\n", g.cookExtentSignature(st), st.Name, st.Name)
			step = 0
			for _, fl := range ir.RecordLayout(g.unit, st).Fields {
				g.cookExtentField(fl.Field, "value->"+fl.Field.Name, fmt.Sprintf("(record!=NULL ? record+%d : NULL)", fl.Offset), "    ", &step)
			}
			g.pf("    return 1;\n}\n")
		}
		if !st.IsTable || st.IsMapEntry() {
			continue
		}
		if g.isVar(st.Name) {
			g.emitCookVariableRoot(st)
		} else {
			g.emitCookFixedRoot(st)
		}
	}
}

func (g *tableGen) cookField(f *ir.Field, src, at string, offset int64, ind string, step *int) {
	if f.IsMap() || f.IsList() {
		return
	} // zero until the extent writer places the array
	pieces := ir.FieldPieces(g.unit, f, offset)
	if len(pieces) == 0 {
		return
	}
	if f.Type.Optional {
		g.pf("%stable_cook_put(%s+%d,%s_present ? 1 : 0,1,order);\n", ind, at, pieces[len(pieces)-1].Offset-offset, src)
	}
	if f.Array != ir.ArrayNone {
		*step++
		i := fmt.Sprintf("cook_i%d", *step)
		elem := sequenceElement(f)
		g.pf("%s{ int32_t %s; for(%s=0;%s<%d;%s++) {\n", ind, i, i, i, f.ArrayBound, i)
		g.cookField(elem, src+"["+i+"]", fmt.Sprintf("%s+%s*%d", at, i, pieces[0].Size/f.ArrayBound), 0, ind+"    ", step)
		g.pf("%s} }\n", ind)
		if f.Array == ir.ArrayCounted {
			g.pf("%stable_cook_put(%s+%d,(uint32_t)%s_count,4,order);\n", ind, at, pieces[1].Offset-offset, src)
		}
		return
	}
	if f.Type.Pointer {
		g.pf("%sif(!table_cook_ref(n,%s,&%s,order)) { return 0; }\n", ind, at, src)
		return
	}
	t := f.Type
	switch t.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		if t.Kind == ir.TWString {
			g.pf("%stable_cook_units(%s,%s,%s_length,%d,order);\n", ind, at, src, src, t.Size+1)
		} else {
			g.pf("%stable_cook_bytes(%s,%s,%s_length,%d);\n", ind, at, src, src, pieces[0].Size)
		}
		g.pf("%stable_cook_put(%s+%d,(uint32_t)%s_length,4,order);\n", ind, at, pieces[1].Offset-offset, src)
	case ir.TBool:
		g.pf("%stable_cook_put(%s,%s ? 1 : 0,1,order);\n", ind, at, src)
	case ir.TFloat32, ir.TFloat64:
		width := 32
		if t.Kind == ir.TFloat64 {
			width = 64
		}
		g.pf("%s{ uint%d_t bits; memcpy(&bits,&%s,%d); table_cook_put(%s,bits,%d,order); }\n", ind, width, src, width/8, at, width/8)
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			g.pf("%stable_cook_put128(%s,%s.lo,%s.hi,order);\n", ind, at, src, src)
		} else {
			g.pf("%stable_cook_put(%s,(uint64_t)%s,%d,order);\n", ind, at, src, t.Width/8)
		}
	case ir.TBits:
		width := 8
		if t.Width <= 32 {
			width = 4
		}
		g.pf("%stable_cook_put(%s,(uint64_t)%s,%d,order);\n", ind, at, src, width)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.pf("%stable_cook_put(%s,(uint64_t)%s,%d,order);\n", ind, at, src, ir.StorageBitsFor(ref.Max)/8)
		case *ir.Flags:
			g.pf("%stable_cook_put(%s,(uint64_t)%s,8,order);\n", ind, at, src)
		case *ir.Struct:
			g.pf("%sif(!%s(n,%s,&%s,order)) { return 0; }\n", ind, g.sym(ref.Name, "cook_body"), at, src)
		case *ir.Union:
			_, _, tag, offset := ir.UnionLayout(g.unit, ref)
			g.pf("%stable_cook_put(%s,(uint64_t)%s.type,%d,order);\n%sswitch(%s.type) {\n", ind, at, src, tag, ind, src)
			for _, v := range ref.Variants {
				if !v.Void() {
					g.pf("%scase %s: {\n", ind, enumConst(ref.Name+"Type", v.Name))
					g.cookField(v.F, armValue(src, v), fmt.Sprintf("%s+%d", at, offset), offset, ind+"    ", step)
					g.pf("%s    break; }\n", ind)
				}
			}
			g.pf("%sdefault: break;\n%s}\n", ind, ind)
		}
	}
}

func (g *tableGen) cookExtentField(f *ir.Field, src, dst, ind string, step *int) {
	if !g.cookFieldExtent(f) {
		return
	}
	*step++
	label := fmt.Sprintf("cook_extent%d", *step)
	g.pf("%suint8_t * %s_slot=%s;\n", ind, label, dst)
	dst = label + "_slot"
	switch {
	case f.IsList() || f.IsMap():
		typ := g.sequenceType(f)
		elem := sequenceElement(f)
		g.pf("%s{ TableSequenceCursor %s=%s; int32_t %s_i; uint8_t * %s_array;\n", ind, label, g.sequenceCursor(f, "n", src), label, label)
		g.pf("%s  if(!%s.ok || !table_extent_reserve(at,%s.count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s))) { return 0; }\n", ind, label, src, typ, typ)
		g.pf("%s  %s_array=%s!=NULL ? extent+*at-(int64_t)%s.count*sizeof(%s) : NULL;\n", ind, label, dst, src, typ)
		g.pf("%s  if(%s_array!=NULL) { table_cook_put(%s,%s.count ? (uint64_t)(%s_array-(%s)) : 0,8,order); table_cook_put(%s+8,(uint32_t)%s.count,4,order); }\n", ind, label, dst, src, label, dst, dst, src)
		g.pf("%s  for(%s_i=0;%s_i<%s.count;%s_i++) {\n", ind, label, label, src, label)
		g.pf("%s    const %s * %s_in=(const %s *)table_sequence_next(&%s); if(%s_in==NULL) { return 0; }\n", ind, typ, label, typ, label, label)
		out := fmt.Sprintf("(%s_array!=NULL ? %s_array+%s_i*sizeof(%s) : NULL)", label, label, label, typ)
		g.pf("%s    if(%s_array!=NULL) {\n", ind, label)
		g.cookField(elem, "(*"+label+"_in)", out, 0, ind+"        ", step)
		g.pf("%s    }\n", ind)
		g.cookExtentField(elem, "(*"+label+"_in)", out, ind+"    ", step)
		g.pf("%s  }\n%s}\n", ind, ind)
	case f.Array != ir.ArrayNone:
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = src + "_count"
		}
		elem := sequenceElement(f)
		pieces := ir.FieldPieces(g.unit, f, 0)
		stride := pieces[0].Size / f.ArrayBound
		g.pf("%s{ int32_t %s; for(%s=0;%s<%s && %s<%d;%s++) {\n", ind, label, label, label, count, label, f.ArrayBound, label)
		g.cookExtentField(elem, src+"["+label+"]", fmt.Sprintf("(%s!=NULL ? %s+%s*%d : NULL)", dst, dst, label, stride), ind+"    ", step)
		g.pf("%s}\n", ind)
		if f.Array == ir.ArrayCounted {
			// A hidden container has no place in the region. Measure that
			// slot independently and refuse even when its shape is invalid.
			g.pf("%sfor(;%s<%d;%s++) { int64_t %s_hidden=0; int64_t * %s_saved_at=at; at=&%s_hidden;\n", ind, label, f.ArrayBound, label, label, label, label)
			g.cookExtentField(elem, src+"["+label+"]", "((uint8_t *)NULL)", ind+"    ", step)
			g.pf("%s    at=%s_saved_at; if(%s_hidden!=0) { return 0; }\n%s}\n", ind, label, label, ind)
		}
		g.pf("%s}\n", ind)
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sif(!%s(n,&%s,%s,extent,at,order)) { return 0; }\n", ind, g.sym(ref.Name, "cook_extent"), src, dst)
		case *ir.Union:
			_, _, _, offset := ir.UnionLayout(g.unit, ref)
			g.pf("%sswitch(%s.type) {\n", ind, src)
			for _, v := range ref.Variants {
				if !v.Void() && g.cookFieldExtent(v.F) {
					g.pf("%scase %s: {\n", ind, enumConst(ref.Name+"Type", v.Name))
					g.cookExtentField(v.F, armValue(src, v), fmt.Sprintf("(%s!=NULL ? %s+%d : NULL)", dst, dst, offset), ind+"    ", step)
					g.pf("%s    break; }\n", ind)
				}
			}
			g.pf("%sdefault: break;\n%s}\n", ind, ind)
		}
	}
}

func (g *tableGen) emitCookFixedRoot(st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(ml.Align)
	bytes := cookAlignUp(ml.Size, align)
	need := 64 + bytes + 16
	g.pf("static SCHEMA_UNUSED int64_t %s(const %s * value) { (void)value; return %d; }\n", g.api(st.Name, "cook_measure"), st.Name, need)
	g.pf("static SCHEMA_UNUSED int %s(const %s * value,void * output,uint64_t capacity,TableByteOrder order)\n{\n    uint8_t * raw=(uint8_t *)output;\n    if(value==NULL || raw==NULL || capacity<%d) { return 0; }\n    memset(raw,0,%d);\n    if(!%s(NULL,raw+64,value,order)) { return 0; }\n", g.api(st.Name, "cook"), st.Name, need, need, g.sym(st.Name, "cook_body"))
	g.pf("    table_cook_put(raw+%d,UINT64_C(0x%016x),8,order);\n    table_cook_header(raw,%s,%d,1,%d,order); return 1;\n}\n", 64+bytes+8, ir.TableWireId(st.WireName()), buildVersionName(g.unit.Package), bytes, align)
}
func (g *tableGen) emitCookVariableRoot(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableCtx * ctx,const %s * value,TableAllocator allocator)\n{ return table_graph_cook(ctx,value,&%s,NULL,0,TableByteOrder_Little,allocator,%s,1); }\n", g.api(st.Name, "cook_measure_with_allocator"), st.Name, g.graphType(st.Name), buildVersionName(g.unit.Package))
	g.pf("static SCHEMA_UNUSED int %s(const TableCtx * ctx,const %s * value,void * output,uint64_t capacity,TableByteOrder order,TableAllocator allocator)\n{ return table_graph_cook(ctx,value,&%s,output,capacity,order,allocator,%s,0)>=0; }\n", g.api(st.Name, "cook_with_allocator"), st.Name, g.graphType(st.Name), buildVersionName(g.unit.Package))
	alloc := "ctx!=NULL && ctx->arena!=NULL ? ctx->arena->allocator : table_default_allocator()"
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableCtx * ctx,const %s * value)\n{ return %s(ctx,value,%s); }\n", g.api(st.Name, "cook_measure"), st.Name, g.api(st.Name, "cook_measure_with_allocator"), alloc)
	g.pf("static SCHEMA_UNUSED int %s(const TableCtx * ctx,const %s * value,void * output,uint64_t capacity,TableByteOrder order)\n{ return %s(ctx,value,output,capacity,order,%s); }\n", g.api(st.Name, "cook"), st.Name, g.api(st.Name, "cook_with_allocator"), alloc)
}
