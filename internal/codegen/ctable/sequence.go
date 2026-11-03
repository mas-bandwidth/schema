package ctable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Lists and maps share a relocatable reference/count record. Arena segments
// retain element addresses through append and erase; region elements are one
// contiguous array inside the owning node's extent.
const tableSequenceRuntimeBase = `
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
/* a list or map term past the storage cap (docs/SPEC-TABLES.md §11) is refused before it is summed */
static SCHEMA_UNUSED int table_extent_over_cap(int64_t at,uint64_t count,int64_t width,int64_t alignment)
{
    if(count==0 || width<=0) { return 0; }
    if(at<0 || at>SCHEMA_TABLE_STORAGE_CAP) { return 1; }
    at=(at+alignment-1)&~(alignment-1);
    return at>SCHEMA_TABLE_STORAGE_CAP || count>(uint64_t)((SCHEMA_TABLE_STORAGE_CAP-at)/width);
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

const tableUnboundedTextSequenceRuntime = `
static SCHEMA_UNUSED int schema_table_text_place_(TableSink * sink,TableRef * ref,const uint8_t * text,uint64_t size,int * refused)
{
    ref->value=0;
    if(size==0) { return 1; }
    if(size>UINT32_MAX || sink==NULL) { return 0; }
    if(sink->worker!=NULL) {
        if(table_string_emplace(sink->worker,ref,(const char *)text,(int64_t)size)==NULL) { if(refused!=NULL) { *refused=1; } return 0; }
        return 1;
    }
    if(sink->region!=NULL) {
        TableRegionSink * region=sink->region; int64_t at=region->used,bytes=table_blob_storage((int64_t)size,1); TableBlob * blob; char * data;
        if(bytes<0 || !table_extent_reserve(&at,1,bytes,8) || at>region->capacity) { return 0; }
        blob=(TableBlob *)(void *)(region->base+at-bytes); region->used=at;
        blob->length=(uint32_t)size; blob->zero=0;
        data=(char *)(blob+1); memcpy(data,text,(size_t)size); data[size]=0;
        ref->value=(int64_t)((uint8_t *)(void *)blob-(uint8_t *)(void *)ref);
        return 1;
    }
    return 0;
}
static SCHEMA_UNUSED int schema_table_text_extent_(const TableCtx * ctx,uint8_t * pack_base,const TableRef * src,TableRef * dst,int64_t * at)
{
    int32_t len=0; int64_t bytes; const char * text;
    if(src->value==0) { if(dst!=NULL) { dst->value=0; } return 1; }
    text=schema_table_text_at_(ctx,src,&len);
    if(len<0) { return 0; }
    if(len==0) { if(dst!=NULL) { dst->value=0; } return 1; }
    bytes=table_blob_storage((int64_t)len,1);
    if(bytes<0 || !table_extent_reserve(at,1,bytes,8)) { return 0; }
    if(dst!=NULL) {
        uint8_t * raw=pack_base+*at-bytes;
        TableBlob * blob=(TableBlob *)(void *)raw;
        blob->length=(uint32_t)len; blob->zero=0;
        memcpy(blob+1,text,(size_t)len); ((char *)(blob+1))[len]=0;
        dst->value=(int64_t)(raw-(uint8_t *)(void *)dst);
    }
    return 1;
}`

func tableSequenceRuntime(anyUnboundedText bool) string {
	if !anyUnboundedText {
		return tableSequenceRuntimeBase
	}
	s := tableSequenceRuntimeBase
	s = strings.Replace(s,
		"typedef int (*TableSequenceCompare)(const void *,const void *);",
		"typedef int (*TableSequenceCompare)(const TableCtx *,const void *,const void *);", 1)
	s = strings.Replace(s,
		"static SCHEMA_UNUSED void table_sequence_sift(const void ** entries,int64_t at,int64_t count,TableSequenceCompare compare)",
		"static SCHEMA_UNUSED void table_sequence_sift(const TableCtx * ctx,const void ** entries,int64_t at,int64_t count,TableSequenceCompare compare)", 1)
	s = strings.Replace(s,
		`if(child+1<count && compare(entries[child],entries[child+1])<0) { child++; }
        if(compare(entries[at],entries[child])>=0) { return; }`,
		`if(child+1<count && compare(ctx,entries[child],entries[child+1])<0) { child++; }
        if(compare(ctx,entries[at],entries[child])>=0) { return; }`, 1)
	s = strings.ReplaceAll(s, "table_sequence_sift(order->entries,", "table_sequence_sift(ctx,order->entries,")
	s = strings.Replace(s, "\n#endif\n", tableUnboundedTextSequenceRuntime+"\n#endif\n", 1)
	return s
}

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
	if e.Type.TextUnbounded() {
		return "TableRef"
	}
	return g.cFieldType(e.Type)
}

func (g *tableGen) sequenceAlign(f *ir.Field) int64 {
	if f.IsMap() {
		return ir.RecordLayout(g.unit, f.MapEntry).Align
	}
	_, align := ir.ListElementLayout(g.unit, f)
	return align
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

func sequenceWireKind(f *ir.Field) int {
	if f.IsMap() {
		return tkTable
	}
	return ir.TableWireScalarKind(f)
}

func (g *tableGen) emitUnboundedTextListSurface(st *ir.Struct, f *ir.Field) {
	name := st.Name + ir.GoExportName(f.Name)
	elem := f.Type.Name
	optional := ir.TextListOptional(f)
	absent := ""
	present := ""
	if optional {
		absent = " || !p->value_present"
		present = " p->value_present = 1;"
	}
	if f.IsList() {
		g.pf("static SCHEMA_UNUSED const char * %s(const TableCtx * ctx, const %s * value, int32_t i, int32_t * out_len)\n{\n", g.api(name, "text_at"), st.Name)
		g.pf("    TableSequenceCursor c; const %s * p = NULL; int32_t k;\n", elem)
		g.pf("    if ( out_len != NULL ) { *out_len = 0; }\n")
		g.pf("    if ( value == NULL || i < 0 || i >= value->%s.count ) { return NULL; }\n", f.Name)
		g.pf("    c = table_sequence_cursor( ctx, &value->%s, sizeof( %s ) ); if ( !c.ok ) { return NULL; }\n", f.Name, elem)
		g.pf("    for ( k = 0; k <= i; k++ ) { p = (const %s *) table_sequence_next( &c ); }\n", elem)
		g.pf("    if ( p == NULL%s ) { return NULL; }\n", absent)
		g.pf("    return schema_table_text_at_( ctx, &p->value, out_len );\n}\n")
		g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value, const char * text, int32_t length)\n{\n", g.api(name, "append"), st.Name)
		g.pf("    TableRef ref = { 0 }; %s * p;\n", elem)
		g.pf("    if ( value == NULL || !schema_table_text_store_( worker, &ref, text, length ) ) { return 0; }\n")
		g.pf("    p = %s( worker, &value->%s ); if ( p == NULL ) { return 0; }\n", g.api(name, "add"), f.Name)
		g.pf("    p->value = ref;%s return 1;\n}\n", present)
		if optional {
			g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value)\n{\n", g.api(name, "append_absent"), st.Name)
			g.pf("    if ( value == NULL ) { return 0; }\n    return %s( worker, &value->%s ) != NULL;\n}\n", g.api(name, "add"), f.Name)
		}
	} else {
		g.pf("static SCHEMA_UNUSED const char * %s(const TableCtx * ctx, const %s * value, int32_t i, int32_t * out_len)\n{\n", g.api(name, "text_at"), st.Name)
		g.pf("    const %s * p;\n    if ( out_len != NULL ) { *out_len = 0; }\n", elem)
		g.pf("    if ( value == NULL || i < 0 || i >= value->%s_count || value->%s_count > %d ) { return NULL; }\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    p = &value->%s[i];\n    if ( 0%s ) { return NULL; }\n", f.Name, absent)
		g.pf("    return schema_table_text_at_( ctx, &p->value, out_len );\n}\n")
		g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value, const char * text, int32_t length)\n{\n", g.api(name, "append"), st.Name)
		g.pf("    TableRef ref = { 0 }; %s * p;\n", elem)
		g.pf("    if ( value == NULL || value->%s_count < 0 || value->%s_count >= %d ) { return 0; }\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( !schema_table_text_store_( worker, &ref, text, length ) ) { return 0; }\n")
		g.pf("    p = &value->%s[value->%s_count]; %s( p );\n", f.Name, f.Name, g.api(elem, "reset"))
		g.pf("    p->value = ref;%s value->%s_count++; return 1;\n}\n", present, f.Name)
		if optional {
			g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value)\n{\n", g.api(name, "append_absent"), st.Name)
			g.pf("    (void) worker;\n    if ( value == NULL || value->%s_count < 0 || value->%s_count >= %d ) { return 0; }\n", f.Name, f.Name, f.ArrayBound)
			g.pf("    %s( &value->%s[value->%s_count] ); value->%s_count++; return 1;\n}\n", g.api(elem, "reset"), f.Name, f.Name, f.Name)
		}
	}
}

func (g *tableGen) emitTextListSurface(st *ir.Struct, f *ir.Field) {
	if ir.TextListUnbounded(f) {
		g.emitUnboundedTextListSurface(st, f)
		return
	}
	name := st.Name + ir.GoExportName(f.Name)
	elem := f.Type.Name
	v := ir.TextListValue(f)
	m := v.Type.Size
	optional := ir.TextListOptional(f)
	absent := ""
	present := ""
	if optional {
		absent = " || !p->value_present"
		present = " p->value_present = 1;"
	}
	if f.IsList() {
		g.pf("static SCHEMA_UNUSED const char * %s(const TableCtx * ctx, const %s * value, int32_t i, int32_t * out_len)\n{\n", g.api(name, "text_at"), st.Name)
		g.pf("    TableSequenceCursor c; const %s * p = NULL; int32_t k;\n", elem)
		g.pf("    if ( out_len != NULL ) { *out_len = 0; }\n")
		g.pf("    if ( value == NULL || i < 0 || i >= value->%s.count ) { return NULL; }\n", f.Name)
		g.pf("    c = table_sequence_cursor( ctx, &value->%s, sizeof( %s ) ); if ( !c.ok ) { return NULL; }\n", f.Name, elem)
		g.pf("    for ( k = 0; k <= i; k++ ) { p = (const %s *) table_sequence_next( &c ); }\n", elem)
		g.pf("    if ( p == NULL || p->value_length < 0 || p->value_length > %d%s ) { return NULL; }\n", m, absent)
		g.pf("    if ( out_len != NULL ) { *out_len = p->value_length; }\n    return p->value;\n}\n")
		g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value, const char * text, int32_t length)\n{\n", g.api(name, "append"), st.Name)
		g.pf("    %s * p;\n", elem)
		g.pf("    if ( worker == NULL || worker->arena == NULL || value == NULL || length < 0 || length > %d || ( length > 0 && text == NULL ) ) { return 0; }\n", m)
		g.pf("    if ( !table_wire_utf8( (const uint8_t *) text, (uint64_t) length ) ) { return 0; }\n")
		if f.TextTotal > 0 {
			g.pf("    { TableCtx ctx; TableSequenceCursor c; int64_t spent = (int64_t) length; int32_t k;\n")
			g.pf("      memset( &ctx, 0, sizeof( ctx ) ); ctx.arena = worker->arena;\n")
			g.pf("      c = table_sequence_cursor( &ctx, &value->%s, sizeof( %s ) ); if ( !c.ok ) { return 0; }\n", f.Name, elem)
			g.pf("      for ( k = 0; k < value->%s.count; k++ ) {\n", f.Name)
			g.pf("          const %s * e = (const %s *) table_sequence_next( &c ); if ( e == NULL ) { return 0; }\n", elem, elem)
			if optional {
				g.pf("          if ( e->value_present && e->value_length > 0 ) { spent += (int64_t) e->value_length; }\n")
			} else {
				g.pf("          if ( e->value_length > 0 ) { spent += (int64_t) e->value_length; }\n")
			}
			g.pf("      }\n      if ( spent > %d ) { return 0; }\n    }\n", f.TextTotal)
		}
		g.pf("    p = %s( worker, &value->%s ); if ( p == NULL ) { return 0; }\n", g.api(name, "add"), f.Name)
		g.pf("    if ( length > 0 ) { memcpy( p->value, text, (size_t) length ); }\n")
		g.pf("    p->value[length] = 0; p->value_length = length;%s return 1;\n}\n", present)
		if optional {
			g.pf("static SCHEMA_UNUSED int %s(TableWorker * worker, %s * value)\n{\n", g.api(name, "append_absent"), st.Name)
			g.pf("    if ( value == NULL ) { return 0; }\n    return %s( worker, &value->%s ) != NULL;\n}\n", g.api(name, "add"), f.Name)
		}
	} else {
		g.pf("static SCHEMA_UNUSED const char * %s(const %s * value, int32_t i, int32_t * out_len)\n{\n", g.api(name, "text_at"), st.Name)
		g.pf("    const %s * p;\n    if ( out_len != NULL ) { *out_len = 0; }\n", elem)
		g.pf("    if ( value == NULL || i < 0 || i >= value->%s_count || value->%s_count > %d ) { return NULL; }\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    p = &value->%s[i];\n    if ( p->value_length < 0 || p->value_length > %d%s ) { return NULL; }\n", f.Name, m, absent)
		g.pf("    if ( out_len != NULL ) { *out_len = p->value_length; }\n    return p->value;\n}\n")
		g.pf("static SCHEMA_UNUSED int %s(%s * value, const char * text, int32_t length)\n{\n", g.api(name, "append"), st.Name)
		g.pf("    %s * p;\n", elem)
		g.pf("    if ( value == NULL || length < 0 || length > %d || ( length > 0 && text == NULL ) ) { return 0; }\n", m)
		g.pf("    if ( !table_wire_utf8( (const uint8_t *) text, (uint64_t) length ) ) { return 0; }\n")
		g.pf("    if ( value->%s_count < 0 || value->%s_count >= %d ) { return 0; }\n", f.Name, f.Name, f.ArrayBound)
		if f.TextTotal > 0 {
			g.pf("    { int64_t spent = (int64_t) length; int32_t k;\n")
			g.pf("      for ( k = 0; k < value->%s_count && k < %d; k++ ) {\n", f.Name, f.ArrayBound)
			if optional {
				g.pf("          if ( value->%s[k].value_present && value->%s[k].value_length > 0 ) { spent += (int64_t) value->%s[k].value_length; }\n", f.Name, f.Name, f.Name)
			} else {
				g.pf("          if ( value->%s[k].value_length > 0 ) { spent += (int64_t) value->%s[k].value_length; }\n", f.Name, f.Name)
			}
			g.pf("      }\n      if ( spent > %d ) { return 0; }\n    }\n", f.TextTotal)
		}
		g.pf("    p = &value->%s[value->%s_count]; %s( p );\n", f.Name, f.Name, g.api(elem, "reset"))
		g.pf("    if ( length > 0 ) { memcpy( p->value, text, (size_t) length ); }\n")
		g.pf("    p->value[length] = 0; p->value_length = length;%s value->%s_count++; return 1;\n}\n", present, f.Name)
		if optional {
			g.pf("static SCHEMA_UNUSED int %s(%s * value)\n{\n", g.api(name, "append_absent"), st.Name)
			g.pf("    if ( value == NULL || value->%s_count < 0 || value->%s_count >= %d ) { return 0; }\n", f.Name, f.Name, f.ArrayBound)
			g.pf("    %s( &value->%s[value->%s_count] ); value->%s_count++; return 1;\n}\n", g.api(elem, "reset"), f.Name, f.Name, f.Name)
		}
	}
}

func (g *tableGen) emitSequenceWrite(f *ir.Field, expr string) {
	e := sequenceElement(f)
	typ := g.sequenceType(f)
	kind := sequenceWireKind(f)
	if f.IsTextList() && f.TextTotal > 0 {
		val := ir.TextListValue(f)
		g.pf("    { TableSequenceCursor tc=%s; int64_t total=0; int32_t ti;\n", g.sequenceCursor(f, "w->nodes", expr))
		g.pf("      if(!tc.ok) { return 0; }\n")
		g.pf("      for(ti=0;ti<%s.count;ti++) {\n", expr)
		g.pf("          const %s * te=(const %s *)table_sequence_next(&tc); if(te==NULL) { return 0; }\n", typ, typ)
		if val.Type.Optional {
			g.pf("          if(!te->value_present) { continue; }\n")
		}
		g.pf("          if(te->value_length<0 || te->value_length>%d) { return 0; }\n", val.Type.Size)
		g.pf("          total+=te->value_length; if(total>%d) { return 0; }\n", f.TextTotal)
		g.pf("      }\n    }\n")
	}
	g.pf("    TableSequenceCursor cursor=%s; int32_t i;\n", g.sequenceCursor(f, "w->nodes", expr))
	g.pf("    if(!cursor.ok) { return 0; } table_writer_put8(w,%d); table_writer_leb(w,(uint64_t)%s.count);\n", kind, expr)
	g.pf("    for(i=0;i<%s.count;i++) {\n        const %s * element=(const %s *)table_sequence_next(&cursor); if(element==NULL) { return 0; }\n", expr, typ, typ)
	g.retainIndex(f, "i", "        ")
	if ir.TextListPlain(f) {
		g.wirePayload(f, "(*element)", "        ")
	} else {
		g.wirePayload(e, "(*element)", "        ")
	}
	g.pf("    }\n")
}

func (g *tableGen) emitListRead(f *ir.Field, dst, bounded string) {
	e := sequenceElement(f)
	typ := g.sequenceType(f)
	kind := sequenceWireKind(f)
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
	g.retainReplace(f, "                    ")
	g.pf("                    fill=table_sequence_fill(r->nodes ? r->nodes->sink : NULL,&%s,count,sizeof(%s),%d);\n                    if(fill.refused) { r->nodes->refused=1; return 0; }\n                    if(!fill.ok) { r->report->malformed=1; break; }\n", dst, typ, g.sequenceAlign(f))
	if f.IsTextList() && f.TextTotal > 0 {
		g.pf("                    int64_t spent=0;\n")
	}
	g.pf("                    for(i=0;i<count;i++) {\n                        %s * element=(%s *)table_sequence_fill_next(&fill); if(element==NULL) { r->report->malformed=1; break; }\n", typ, typ)
	g.retainIndex(f, "i", "                        ")
	if ref, ok := e.Type.Ref.(*ir.Struct); ok && !e.Type.Pointer {
		g.pf("                        %s(element);\n", g.api(ref.Name, "reset"))
	}
	g.pf("                        do {\n")
	bad := "r->report->malformed=1; table_sequence_fill_drop(&fill,element); goto list_end_" + f.Name + ";"
	switch kind {
	case tkString:
		val := ir.TextListValue(f)
		if val.Type.TextUnbounded() {
			g.pf("                            TableReader item; int refused=0;\n")
			g.pf("                            if(!table_reader_span(&sub,&item)) { %s }\n", bad)
			g.pf("                            if(!table_wire_utf8(item.buffer,(uint64_t)item.size)) { %s(element); %s }\n", g.api(f.Type.Name, "reset"), bad)
			g.pf("                            if(!schema_table_text_place_(r->nodes ? r->nodes->sink : NULL,&element->value,item.buffer,(uint64_t)item.size,&refused)) { if(refused) { r->nodes->refused=1; return 0; } %s(element); %s }\n", g.api(f.Type.Name, "reset"), bad)
		} else {
			g.pf("                            TableReader item; uint64_t keep;\n")
			g.pf("                            if(!table_reader_span(&sub,&item)) { %s }\n", bad)
			g.pf("                            if(!table_wire_utf8(item.buffer,(uint64_t)item.size)) { %s(element); %s }\n", g.api(f.Type.Name, "reset"), bad)
			g.pf("                            keep=(uint64_t)item.size;\n")
			g.pf("                            if(keep>%d) { keep=table_wire_utf8_clamp(item.buffer,keep,%d); r->report->clamped++; }\n", val.Type.Size, val.Type.Size)
			g.pf("                            memset(element->value,0,sizeof(element->value));\n")
			g.pf("                            memcpy(element->value,item.buffer,(size_t)keep); element->value[keep]=0; element->value_length=(int32_t)keep;\n")
		}
	case tkTable:
		g.pf("                            TableReader item; if(!table_reader_span(&sub,&item)) { %s }\n                            %s(&item,element);\n", bad, g.api(e.Type.Name, "load_body"))
		g.wireRefusalCheck("                            ")
		g.pf("                            if(item.offset!=item.size) { r->report->malformed=1; %s(element); }\n", g.api(e.Type.Name, "reset"))
	case tkUnion:
		g.pf("                            if(!%s(&sub,element,1)) {\n", g.unionWireName(e.Type.Ref.(*ir.Union), "load"))
		g.wireRefusalCheck("                                ")
		g.pf("                                %s }\n", bad)
	default:
		g.wireScalarRead(e, "(*element)", "sub", "elem_kind", "                            ", bad)
	}
	if f.IsTextList() && f.TextTotal > 0 {
		if ir.TextListOptional(f) {
			g.pf("                            if(element->value_present && element->value_length>0) { spent+=(int64_t)element->value_length; }\n")
		} else {
			g.pf("                            if(element->value_length>0) { spent+=(int64_t)element->value_length; }\n")
		}
		g.pf("                            if(spent>%d) { r->report->clamped++; %s(element); table_sequence_fill_drop(&fill,element); goto list_end_%s; }\n", f.TextTotal, g.api(f.Type.Name, "reset"), f.Name)
	}
	g.pf("                        } while(0);\n                    }\n                    list_end_%s: table_sequence_fill_end(&fill);\n                }\n", f.Name)
}

func sequenceFloor(f *ir.Field) int {
	k := sequenceWireKind(f)
	switch k {
	case tkTable:
		return 2
	case tkUnion, ir.TableKindEnum, 17, tkString:
		return 1
	}
	return tableKindWidth(k)
}
