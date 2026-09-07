package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Lists and maps share a relocatable reference/count record. Arena segments
// retain element addresses through append and erase; region elements are one
// contiguous array inside the owning node's extent.
const tableSequenceRuntime = `
#ifndef SCHEMA_TABLE_SEQUENCE_RUNTIME
#define SCHEMA_TABLE_SEQUENCE_RUNTIME
typedef struct TableSequence { TableRef items; int32_t count, padding; } TableSequence;
typedef struct TableSequenceHead { uint32_t first, last; int32_t live, reserved; } TableSequenceHead;
typedef struct TableSequenceSegment { uint32_t next, dead; int32_t used, reserved; } TableSequenceSegment;
typedef struct TableSequenceCursor
{
    const TableArena * arena;
    const uint8_t * array;
    const void * const * ordered;
    const TableSequenceSegment * segment;
    int64_t width;
    int32_t index, left;
    int ok;
} TableSequenceCursor;
static SCHEMA_UNUSED TableSequenceCursor table_sequence_cursor(const TableCtx * ctx, const TableSequence * value, int64_t width)
{
    TableSequenceCursor c; memset(&c,0,sizeof(c)); c.width=width; c.left=value->count;
    c.ok=value->count>=0 && (value->count==0 || value->items.value!=0);
    if(!c.ok || value->count==0) { return c; }
    if(ctx!=NULL && ctx->arena!=NULL) {
        const TableSequenceHead * head=(const TableSequenceHead *)table_arena_at(ctx->arena,(uint32_t)value->items.value);
        c.arena=ctx->arena; c.ok=head!=NULL && head->live==value->count;
        if(c.ok) { c.segment=(const TableSequenceSegment *)table_arena_at(c.arena,head->first); }
    } else { c.array=(const uint8_t *)(const void *)&value->items+value->items.value; }
    return c;
}
static SCHEMA_UNUSED const void * table_sequence_next(TableSequenceCursor * c)
{
    if(!c->ok || c->left<=0) { return NULL; }
    if(c->ordered!=NULL) { c->left--; return c->ordered[c->index++]; }
    if(c->array!=NULL) { const void * p=c->array+(int64_t)c->index++*c->width; c->left--; return p; }
    while(c->segment!=NULL) {
        while(c->index<c->segment->used) {
            int32_t i=c->index++;
            if((c->segment->dead & (UINT32_C(1)<<i))==0) { c->left--; return (const uint8_t *)(c->segment+1)+(int64_t)i*c->width; }
        }
        c->segment=c->segment->next ? (const TableSequenceSegment *)table_arena_at(c->arena,c->segment->next) : NULL; c->index=0;
    }
    c->ok=0; return NULL;
}
typedef struct TableSequenceOrder
{
    const TableSequence * value;
    const void ** entries;
    struct TableSequenceOrder * next;
} TableSequenceOrder;
typedef int (*TableSequenceCompare)(const void *,const void *);
static SCHEMA_UNUSED void table_sequence_sift(const void ** entries,int64_t at,int64_t count,TableSequenceCompare compare)
{
    for(;;) {
        int64_t child=at*2+1; const void * swap;
        if(child>=count) { return; }
        if(child+1<count && compare(entries[child],entries[child+1])<0) { child++; }
        if(compare(entries[at],entries[child])>=0) { return; }
        swap=entries[at]; entries[at]=entries[child]; entries[child]=swap; at=child;
    }
}
// The arena's sort is cached only for the duration of one save or lock.
// Builders retain insertion order and stable addresses; no index is stored.
static SCHEMA_UNUSED TableSequenceCursor table_sequence_order(const TableCtx * ctx,const TableSequence * value,int64_t width,
    TableSequenceCompare compare,TableAllocator allocator,TableSequenceOrder ** orders)
{
    TableSequenceCursor cursor=table_sequence_cursor(ctx,value,width);
    TableSequenceOrder * order; int64_t i;
    if(!cursor.ok || value->count==0 || ctx==NULL || ctx->arena==NULL) { return cursor; }
    for(order=*orders;order!=NULL;order=order->next) {
        if(order->value==value) { cursor.ordered=order->entries; return cursor; }
    }
    order=(TableSequenceOrder *)table_allocate(allocator,sizeof(*order));
    if(order==NULL) { cursor.ok=0; return cursor; }
    order->entries=(const void **)table_allocate(allocator,(int64_t)value->count*sizeof(void *));
    if(order->entries==NULL) { table_release(allocator,order); cursor.ok=0; return cursor; }
    for(i=0;i<value->count;i++) {
        order->entries[i]=table_sequence_next(&cursor);
        if(order->entries[i]==NULL) { table_release(allocator,order->entries); table_release(allocator,order); return cursor; }
    }
    for(i=(int64_t)value->count/2;i>0;i--) { table_sequence_sift(order->entries,i-1,value->count,compare); }
    for(i=(int64_t)value->count-1;i>0;i--) {
        const void * swap=order->entries[0]; order->entries[0]=order->entries[i]; order->entries[i]=swap;
        table_sequence_sift(order->entries,0,i,compare);
    }
    order->value=value; order->next=*orders; *orders=order;
    cursor=table_sequence_cursor(ctx,value,width); cursor.ordered=order->entries; return cursor;
}
static SCHEMA_UNUSED void table_sequence_orders_release(TableAllocator allocator,TableSequenceOrder * order)
{
    while(order!=NULL) { TableSequenceOrder * next=order->next; table_release(allocator,order->entries); table_release(allocator,order); order=next; }
}
static SCHEMA_UNUSED void * table_sequence_add(TableWorker * worker,TableSequence * list,int64_t width)
{
    TableSequenceHead * head; TableSequenceSegment * segment; uint32_t at;
    if(worker==NULL || worker->arena==NULL || worker->arena->locked || list->count<0 || list->count==INT32_MAX || width<=0 || width>(INT64_MAX-16)/32) { return NULL; }
    if(list->items.value==0) {
        at=table_worker_alloc(worker,sizeof(TableSequenceHead)); if(at==kTableAllocFailed) { return NULL; }
        list->items.value=at;
    }
    head=(TableSequenceHead *)table_arena_at(worker->arena,(uint32_t)list->items.value);
    if(head==NULL || head->live!=list->count) { return NULL; }
    segment=head->last ? (TableSequenceSegment *)table_arena_at(worker->arena,head->last) : NULL;
    if(segment==NULL || segment->used==32) {
        at=table_worker_alloc(worker,16+32*width); if(at==kTableAllocFailed) { return NULL; }
        if(segment!=NULL) { segment->next=at; } else { head->first=at; }
        head->last=at; segment=(TableSequenceSegment *)table_arena_at(worker->arena,at);
    }
    { void * p=(uint8_t *)(segment+1)+(int64_t)segment->used++*width; head->live++; list->count++; return p; }
}
static SCHEMA_UNUSED int table_sequence_erase(TableArena * arena,TableSequence * list,const void * element,int64_t width)
{
    TableSequenceHead * head; uint32_t at;
    if(arena==NULL || arena->locked || list->items.value==0 || element==NULL) { return 0; }
    head=(TableSequenceHead *)table_arena_at(arena,(uint32_t)list->items.value);
    for(at=head->first;at;) {
        TableSequenceSegment * s=(TableSequenceSegment *)table_arena_at(arena,at); int32_t i;
        for(i=0;i<s->used;i++) {
            if(element==(const uint8_t *)(s+1)+(int64_t)i*width && !(s->dead & (UINT32_C(1)<<i))) {
                s->dead|=UINT32_C(1)<<i; list->count--; head->live--; return 1;
            }
        }
        at=s->next;
    }
    return 0;
}
static SCHEMA_UNUSED int table_extent_reserve(int64_t * at,int64_t count,int64_t width,int64_t alignment)
{
    if(count<0 || width<=0 || *at<0 || *at>INT64_MAX-alignment || count>(INT64_MAX-*at-alignment)/width) { return 0; }
    *at=(*at+alignment-1)&~(alignment-1); *at+=count*width; return 1;
}
typedef struct TableSequenceFill
{
    TableSequence * value; TableWorker * worker; uint8_t * array;
    int64_t width; int32_t capacity; int ok,refused;
} TableSequenceFill;
static SCHEMA_UNUSED TableSequenceFill table_sequence_fill(TableSink * sink,TableSequence * value,uint64_t count,int64_t width,int64_t alignment)
{
    TableSequenceFill f; memset(&f,0,sizeof(f)); f.value=value; f.width=width;
    memset(value,0,sizeof(*value));
    if(sink==NULL) { return f; }
    if(count>INT32_MAX) { f.refused=sink->worker!=NULL; return f; }
    f.worker=sink->worker; f.capacity=(int32_t)count;
    if(f.worker!=NULL) { f.ok=1; return f; }
    if(sink->region!=NULL) {
        TableRegionSink * region=sink->region; int64_t at=region->used;
        if(!table_extent_reserve(&at,(int64_t)count,width,alignment) || at>region->capacity) { return f; }
        f.array=region->base+at-(int64_t)count*width; region->used=at;
        if(count) { value->items.value=(int64_t)(f.array-(uint8_t *)(void *)&value->items); }
        f.ok=1;
    }
    return f;
}
static SCHEMA_UNUSED void * table_sequence_fill_next(TableSequenceFill * f)
{
    if(!f->ok || f->value->count>=f->capacity) { return NULL; }
    if(f->worker!=NULL) { return table_sequence_add(f->worker,f->value,f->width); }
    return f->array+(int64_t)f->value->count++*f->width;
}
static SCHEMA_UNUSED void table_sequence_fill_drop(TableSequenceFill * f,void * element)
{
    if(f->worker!=NULL) { table_sequence_erase(f->worker->arena,f->value,element,f->width); }
    else if(f->value->count>0) { f->value->count--; }
}
static SCHEMA_UNUSED void table_sequence_fill_end(TableSequenceFill * f)
{ if(f->worker==NULL && f->value->count==0) { f->value->items.value=0; } }
#endif
`

func sequenceElement(f *ir.Field) *ir.Field {
	if f.IsMap() {
		return &ir.Field{Type: ir.FieldType{Kind: ir.TNamed, Name: f.MapEntry.Name, Ref: f.MapEntry}}
	}
	e := *f
	e.Array = ir.ArrayNone
	e.Type.Optional = false
	return &e
}

func (g *tableGen) sequenceType(f *ir.Field) string {
	e := sequenceElement(f)
	if e.Type.Pointer {
		g.noteRef(e.Type.Name)
		return "TableRef"
	}
	return g.cFieldType(e.Type)
}

func (g *tableGen) emitListSurface(st *ir.Struct, f *ir.Field) {
	typ := g.sequenceType(f)
	name := st.Name + ir.GoExportName(f.Name)
	g.pf("static SCHEMA_UNUSED %s * %s(TableWorker * worker, TableList * list)\n{\n", typ, g.api(name, "add"))
	g.pf("    %s * element=(%s *)table_sequence_add(worker,list,sizeof(%s)); if(element==NULL) { return NULL; }\n", typ, typ, typ)
	if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
		g.pf("    %s(element);\n", g.api(ref.Name, "reset"))
	} else {
		g.pf("    memset(element,0,sizeof(*element));\n")
	}
	g.pf("    return element;\n}\n")
	g.pf("static SCHEMA_UNUSED int %s(TableArena * arena,TableList * list,const %s * element)\n{ return table_sequence_erase(arena,list,element,sizeof(%s)); }\n", g.api(name, "erase"), typ, typ)
	g.pf("static SCHEMA_UNUSED TableSequenceCursor %s(const TableCtx * ctx,const TableList * list)\n{ return table_sequence_cursor(ctx,list,sizeof(%s)); }\n", g.api(name, "each"), typ)
	result := "const " + typ + " *"
	if f.Type.Pointer {
		result = "const " + f.Type.Name + " *"
	}
	g.pf("static SCHEMA_UNUSED %s %s(const TableList * list,int32_t index)\n{\n", result, g.api(name, "at"))
	g.pf("    const %s * element; if(index<0 || index>=list->count || list->items.value==0) { return NULL; }\n", typ)
	g.pf("    element=(const %s *)((const uint8_t *)(const void *)&list->items+list->items.value)+(int64_t)index;\n", typ)
	if f.Type.Pointer {
		g.pf("    return (const %s *)table_ref_at(NULL,element);\n", f.Type.Name)
	} else {
		g.pf("    return element;\n")
	}
	g.pf("}\n")
}

func (g *tableGen) emitSequenceWrite(f *ir.Field, expr string) {
	e := sequenceElement(f)
	typ := g.sequenceType(f)
	g.pf("    TableSequenceCursor cursor=%s; int32_t i;\n", g.sequenceCursor(f, "w->nodes", expr))
	g.pf("    if(!cursor.ok) { return 0; } table_writer_put8(w,%d); table_writer_leb(w,(uint64_t)%s.count);\n", ir.TableWireScalarKind(e), expr)
	g.pf("    for(i=0;i<%s.count;i++) {\n        const %s * element=(const %s *)table_sequence_next(&cursor); if(element==NULL) { return 0; }\n", expr, typ, typ)
	g.wirePayload(e, "(*element)", "        ")
	g.pf("    }\n")
}

func (g *tableGen) emitListRead(f *ir.Field, dst, bounded string) {
	e := sequenceElement(f)
	typ := g.sequenceType(f)
	kind := ir.TableWireScalarKind(e)
	g.pf("                TableReader sub;\n")
	if bounded == "" {
		g.pf("                if(!table_reader_span(r,&sub)) { r->report->malformed=1; return 0; }\n")
	} else {
		g.pf("                sub=%s; %s.offset=%s.size;\n", bounded, bounded, bounded)
	}
	g.pf("                if(sub.size>=2) {\n                    uint8_t elem_kind; uint64_t count,i; TableSequenceFill fill;\n")
	if bounded == "" {
		g.pf("                    if(!table_reader_array_header(&sub,r,&elem_kind,&count)) { r->report->malformed=1; break; }\n")
	} else {
		g.pf("                    elem_kind=table_reader_get8(&sub); if(!table_reader_leb(&sub,&count)) { r->report->malformed=1; break; }\n")
	}
	g.pf("                    if(elem_kind!=%d) { if(!(%s)) { r->report->kind_mismatch++; break; } r->report->widened++; }\n", kind, wireElementWidenTest(f, kind, "elem_kind"))
	g.pf("                    fill=table_sequence_fill(r->nodes ? r->nodes->sink : NULL,&%s,count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s));\n                    if(fill.refused) { r->nodes->refused=1; return 0; }\n                    if(!fill.ok) { r->report->malformed=1; break; }\n", dst, typ, typ)
	g.pf("                    for(i=0;i<count;i++) {\n                        %s * element=(%s *)table_sequence_fill_next(&fill); if(element==NULL) { r->report->malformed=1; break; }\n", typ, typ)
	if ref, ok := e.Type.Ref.(*ir.Struct); ok && !e.Type.Pointer {
		g.pf("                        %s(element);\n", g.api(ref.Name, "reset"))
	}
	g.pf("                        do {\n")
	bad := "r->report->malformed=1; table_sequence_fill_drop(&fill,element); goto list_end_" + f.Name + ";"
	switch kind {
	case tkTable:
		g.pf("                            TableReader item; if(!table_reader_span(&sub,&item)) { %s }\n                            %s(&item,element);\n", bad, g.api(e.Type.Name, "load_body"))
		g.wireRefusalCheck("                            ")
		g.pf("                            if(item.offset!=item.size) { r->report->malformed=1; %s(element); }\n", g.api(e.Type.Name, "reset"))
	case tkUnion:
		g.pf("                            if(!%s(&sub,element,1)) { %s }\n", g.unionWireName(e.Type.Ref.(*ir.Union), "load"), bad)
	default:
		g.wireScalarRead(e, "(*element)", "sub", "elem_kind", "                            ", bad)
	}
	g.pf("                        } while(0);\n                    }\n                    list_end_%s: table_sequence_fill_end(&fill);\n                }\n", f.Name)
}

func sequenceFloor(f *ir.Field) int {
	k := ir.TableWireScalarKind(sequenceElement(f))
	switch k {
	case tkTable:
		return 2
	case tkUnion, ir.TableKindEnum, 17:
		return 1
	}
	return tableKindWidth(k)
}

func (g *tableGen) sequenceName(f *ir.Field) string {
	return fmt.Sprintf("%s_%s", g.owner.Name, f.Name)
}
