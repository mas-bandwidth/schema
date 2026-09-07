package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) sequenceCursor(f *ir.Field, n, value string) string {
	if !f.IsMap() {
		return fmt.Sprintf("table_sequence_cursor(%s ? %s->ctx : NULL,&%s,sizeof(%s))", n, n, value, g.sequenceType(f))
	}
	return fmt.Sprintf("table_sequence_order(%s ? %s->ctx : NULL,&%s,sizeof(%s),%s,%s ? %s->allocator : table_default_allocator(),%s ? &%s->orders : NULL)", n, n, value, g.sequenceType(f), g.sym(f.MapEntry.Name, "compare"), n, n, n, n)
}

func (g *tableGen) mapKeyType(f *ir.Field) string {
	if ir.MapKeyField(f).Type.Kind == ir.TString {
		return "TableMapKey"
	}
	return g.cFieldType(ir.MapKeyField(f).Type)
}

func mapPair(f *ir.Field) bool {
	v := ir.MapValueField(f)
	return v.Array == ir.ArrayCounted || !v.Type.Pointer && (v.Type.Kind == ir.TString || v.Type.Kind == ir.TWString || v.Type.Kind == ir.TBytes)
}

func (g *tableGen) mapValueType(f *ir.Field) string {
	v := ir.MapValueField(f)
	switch {
	case v.IsMap():
		return "TableMap"
	case v.IsList():
		return "TableList"
	case mapPair(f):
		return f.MapEntry.Name
	case v.Array == ir.ArrayFixed:
		return f.MapEntry.Name + "Value"
	case v.Type.Pointer:
		return "TableRef"
	default:
		return g.cFieldType(v.Type)
	}
}

const tableMapRuntime = `
#ifndef SCHEMA_TABLE_MAP_RUNTIME
#define SCHEMA_TABLE_MAP_RUNTIME
typedef TableSequence TableMap;
typedef struct TableMapKey { const char * data; int32_t length; } TableMapKey;
typedef struct TableMapIndex { int32_t * slots; int32_t capacity; int good; } TableMapIndex;
static SCHEMA_UNUSED int table_map_key_compare(TableMapKey a,TableMapKey b)
{
    int32_t common;
    if(a.length<0 || b.length<0) { return a.length<0 ? -1 : 1; }
    common=a.length<b.length ? a.length : b.length;
    int order=common ? memcmp(a.data,b.data,(size_t)common) : 0;
    return order ? (order<0 ? -1 : 1) : (a.length<b.length ? -1 : (a.length>b.length ? 1 : 0));
}
static SCHEMA_UNUSED TableMapKey table_map_key(const char * data,int32_t bound)
{
    TableMapKey key; key.data=data; key.length=0;
    if(data==NULL) { key.length=-1; return key; }
    while(key.length<=bound && data[key.length]) { key.length++; } return key;
}
static SCHEMA_UNUSED uint64_t table_map_hash(const void * data,int64_t bytes)
{
    const uint8_t * p=(const uint8_t *)data; uint64_t hash=UINT64_C(14695981039346656037); int64_t i;
    for(i=0;i<bytes;i++) { hash^=p[i]; hash*=UINT64_C(1099511628211); } return hash;
}
static SCHEMA_UNUSED int32_t table_map_index_slots(int32_t count)
{
    int64_t slots=2; if(count<0 || count>INT32_MAX/2) { return 0; }
    while(slots<(int64_t)count*2) { slots*=2; } return slots<=INT32_MAX ? (int32_t)slots : 0;
}
#endif
`

func (g *tableGen) emitMapHelpers(owner *ir.Struct, f *ir.Field) {
	n := f.MapEntry.Name
	key := ir.MapKeyField(f)
	ktype := g.mapKeyType(f)
	stringKey := key.Type.Kind == ir.TString
	value := ir.MapValueField(f)
	vtype := g.mapValueType(f)
	surface := owner.Name + ir.GoExportName(f.Name)
	if value.Array == ir.ArrayFixed {
		g.pf("typedef %s %s[%d];\n", g.sequenceType(value), vtype, value.ArrayBound)
	}
	g.pf("static SCHEMA_UNUSED %s %s(const %s * entry)\n{\n", ktype, g.sym(n, "key"), n)
	if stringKey {
		g.pf("    TableMapKey key={entry->key,entry->key_length}; return key;\n")
	} else {
		g.pf("    return entry->key;\n")
	}
	g.pf("}\n")
	g.pf("static SCHEMA_UNUSED int %s(%s a,%s b)\n{ return ", g.sym(n, "key_compare"), ktype, ktype)
	if stringKey {
		g.pf("table_map_key_compare(a,b)")
	} else {
		g.pf("a<b ? -1 : (a>b ? 1 : 0)")
	}
	g.pf("; }\n")
	g.pf("static SCHEMA_UNUSED int %s(const void * a,const void * b)\n{ return %s(%s((const %s *)a),%s((const %s *)b)); }\n", g.sym(n, "compare"), g.sym(n, "key_compare"), g.sym(n, "key"), n, g.sym(n, "key"), n)
	g.pf("static SCHEMA_UNUSED %s * %s(const TableCtx * ctx,const TableMap * map,%s key)\n{\n", n, g.sym(n, "find"), ktype)
	g.pf("    if(ctx!=NULL && ctx->arena!=NULL) { TableSequenceCursor cursor=table_sequence_cursor(ctx,map,sizeof(%s)); const %s * entry;\n        while((entry=(const %s *)table_sequence_next(&cursor))!=NULL) { if(%s(%s(entry),key)==0) { return (%s *)(uintptr_t)entry; } }\n    } else {\n", n, n, n, g.sym(n, "key_compare"), g.sym(n, "key"), n)
	g.pf("        int32_t lo=0,hi=map->count; const %s * entries=map->items.value ? (const %s *)((const uint8_t *)(const void *)&map->items+map->items.value) : NULL;\n        if(entries==NULL) { return NULL; }\n        while(lo<hi) { int32_t mid=lo+(hi-lo)/2; int order=%s(%s(entries+mid),key); if(order<0) { lo=mid+1; } else if(order>0) { hi=mid; } else { return (%s *)(uintptr_t)(entries+mid); } }\n    } return NULL;\n}\n", n, n, g.sym(n, "key_compare"), g.sym(n, "key"), n)
	g.pf("static SCHEMA_UNUSED %s * %s(TableWorker * worker,TableMap * map,%s key)\n{\n    TableCtx ctx; %s * entry;\n    if(worker==NULL || worker->arena==NULL || worker->arena->locked) { return NULL; } ctx.arena=worker->arena;\n", n, g.sym(n, "place"), ktype, n)
	if stringKey {
		g.pf("    if(key.length<0 || key.length>%d || (key.length && key.data==NULL)) { return NULL; }\n", key.Type.Size)
	}
	g.pf("    entry=%s(&ctx,map,key);\n    if(entry!=NULL) {\n", g.sym(n, "find"))
	// Reset just the value: string keys may point into this same entry.
	g.pf("        %s * value=entry; (void)value;\n", n)
	g.emitTableResetField(value)
	g.pf("        return entry;\n    }\n    entry=(%s *)table_sequence_add(worker,map,sizeof(%s)); if(entry==NULL) { return NULL; }\n    %s(entry);\n", n, n, g.api(n, "reset"))
	if stringKey {
		g.pf("    if(key.length) { memcpy(entry->key,key.data,(size_t)key.length); } entry->key[key.length]=0; entry->key_length=key.length;\n")
	} else {
		g.pf("    entry->key=key;\n")
	}
	g.pf("    return entry;\n}\n")
	keyParam, keyArg := ktype, "key"
	if stringKey {
		keyParam = "const char *"
		keyArg = fmt.Sprintf("table_map_key(key,%d)", key.Type.Size)
	}
	handle := "&entry->value"
	if mapPair(f) {
		handle = "entry"
	}
	g.pf("static SCHEMA_UNUSED %s * %s(TableWorker * worker,TableMap * map,%s key)\n{ %s * entry=%s(worker,map,%s); return entry ? %s : NULL; }\n", vtype, g.api(surface, "insert"), keyParam, n, g.sym(n, "place"), keyArg, handle)
	g.pf("static SCHEMA_UNUSED %s * %s(TableArena * arena,TableMap * map,%s key)\n{ TableCtx ctx; %s * entry; ctx.arena=arena; if(arena==NULL || arena->locked) { return NULL; } entry=%s(&ctx,map,%s); return entry ? %s : NULL; }\n", vtype, g.api(surface, "find_mut"), keyParam, n, g.sym(n, "find"), keyArg, handle)
	result := "const " + vtype + " *"
	found := handle
	if value.Type.Pointer && value.Array == ir.ArrayNone {
		target := value.Type.Name
		if value.Type.Blob() {
			target = "TableBlob"
		}
		result = "const " + target + " *"
		found = "(" + result + ")table_ref_at(NULL,&entry->value)"
	}
	g.pf("static SCHEMA_UNUSED %s %s(const TableMap * map,%s key)\n{ const %s * entry=%s(NULL,map,%s); return entry ? %s : NULL; }\n", result, g.api(surface, "find"), keyParam, n, g.sym(n, "find"), keyArg, found)
	g.pf("static SCHEMA_UNUSED int %s(TableArena * arena,TableMap * map,%s key)\n{ TableCtx ctx; %s * entry; ctx.arena=arena; if(arena==NULL || arena->locked) { return 0; } entry=%s(&ctx,map,%s); return entry ? table_sequence_erase(arena,map,entry,sizeof(%s)) : 0; }\n", g.api(surface, "erase"), keyParam, n, g.sym(n, "find"), keyArg, n)
	g.pf("static SCHEMA_UNUSED TableSequenceCursor %s(const TableCtx * ctx,const TableMap * map)\n{ return table_sequence_cursor(ctx,map,sizeof(%s)); }\n", g.api(surface, "each"), n)
	g.pf("static SCHEMA_UNUSED void * %s(void * worker,void * slot,const char * text,int32_t length,int64_t number)\n{\n", g.sym(n, "json_place"))
	if stringKey {
		g.pf("    TableMapKey key={text,length}; (void)number; return %s((TableWorker *)worker,(TableMap *)slot,key);\n", g.sym(n, "place"))
	} else {
		g.pf("    (void)text; (void)length; return %s((TableWorker *)worker,(TableMap *)slot,(%s)number);\n", g.sym(n, "place"), ktype)
	}
	g.pf("}\n")
	g.emitMapKeyRead(f)
	g.emitMapIndex(owner, f, result, found, keyParam, keyArg)
}

func (g *tableGen) emitMapKeyRead(f *ir.Field) {
	n := f.MapEntry.Name
	readType := g.sym(n, "key_read")
	key := ir.MapKeyField(f)
	kt := g.mapKeyType(f)
	kind := ir.TableWireScalarKind(key)
	g.pf("typedef struct %s { %s key; int kind_bad,widened,over,malformed; } %s;\n", readType, kt, readType)
	g.pf("static SCHEMA_UNUSED %s %s(TableReader r)\n{\n    %s out; memset(&out,0,sizeof(out));\n    for(;;) { uint64_t ref,id; uint8_t kind;\n        if(!table_reader_leb(&r,&ref)) { out.malformed=1; return out; } if(ref==0) { return out; }\n        if(ref>r.id_count || !table_reader_has(&r,1)) { out.malformed=1; return out; }\n        id=table_reader_id_at(&r,ref); kind=table_reader_get8(&r);\n        if(id==UINT64_C(0x%016x)) {\n", readType, g.sym(n, "read_key"), readType, ir.MapKeyWireId)
	g.pf("            if(kind!=%d && table_kind_widens(kind,%d)) { out.widened=1; } else { out.kind_bad=kind!=%d; }\n", kind, kind, kind)
	g.pf("            if(!out.kind_bad) {\n")
	if key.Type.Kind == ir.TString {
		g.pf("                TableReader text; if(!table_reader_span(&r,&text) || !table_wire_utf8(text.buffer,text.size)) { out.malformed=1; return out; }\n                out.key.data=(const char *)text.buffer; out.key.length=(int32_t)text.size; out.over=text.size>%d;\n", key.Type.Size)
	} else {
		g.wireScalarRead(key, "out.key", "r", "kind", "                ", "out.malformed=1; return out;")
	}
	g.pf("                continue;\n            }\n        }\n        if(!table_reader_skip(&r,kind)) { out.malformed=1; return out; }\n    }\n}\n")
}

func (g *tableGen) emitMapRead(f *ir.Field, dst, bounded string) {
	n := f.MapEntry.Name
	g.pf("                TableReader sub;\n")
	if bounded == "" {
		g.pf("                if(!table_reader_span(r,&sub)) { r->report->malformed=1; return 0; }\n")
	} else {
		g.pf("                sub=%s; %s.offset=%s.size;\n", bounded, bounded, bounded)
	}
	g.pf("                if(sub.size>=2) {\n                    uint8_t elem_kind; uint64_t count,i; TableSequenceFill fill; %s * last=NULL; %s previous; int widened=0; memset(&previous,0,sizeof(previous));\n", n, g.sym(n, "key_read"))
	if bounded == "" {
		g.pf("                    if(!table_reader_array_header(&sub,r,&elem_kind,&count)) { r->report->malformed=1; break; }\n")
	} else {
		g.pf("                    elem_kind=table_reader_get8(&sub); if(!table_reader_leb(&sub,&count)) { r->report->malformed=1; break; }\n")
	}
	g.pf("                    if(elem_kind!=13) { r->report->kind_mismatch++; break; }\n                    fill=table_sequence_fill(r->nodes ? r->nodes->sink : NULL,&%s,count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s));\n                    if(!fill.ok) { r->report->malformed=1; break; }\n", dst, n, n)
	g.pf("                    for(i=0;i<count;i++) {\n                        TableReader item; %s key; %s * entry; int order;\n                        if(!table_reader_span(&sub,&item)) { r->report->malformed=1; break; }\n                        key=%s(item);\n                        if(key.widened && !widened) { widened=1; r->report->widened++; }\n                        if(key.kind_bad) { r->report->kind_mismatch++; memset(&%s,0,sizeof(%s)); break; }\n                        if(key.malformed) { r->report->malformed=1; break; }\n                        if(key.over) { r->report->clamped++; continue; }\n                        order=last ? %s(previous.key,key.key) : -1;\n                        if(order>0) { r->report->malformed=1; break; }\n                        if(order==0) { entry=last; r->report->duplicate++; }\n                        else { entry=(%s *)table_sequence_fill_next(&fill); }\n                        if(entry==NULL) { r->report->malformed=1; break; }\n                        %s(&item,entry); last=entry; previous=key;\n                    }\n                    table_sequence_fill_end(&fill);\n                }\n", g.sym(n, "key_read"), n, g.sym(n, "read_key"), dst, dst, g.sym(n, "key_compare"), n, g.api(n, "load_body"))
}

func (g *tableGen) emitMapIndex(owner *ir.Struct, f *ir.Field, result, found, keyParam, keyArg string) {
	n := f.MapEntry.Name
	surface := owner.Name + ir.GoExportName(f.Name)
	stringKey := ir.MapKeyField(f).Type.Kind == ir.TString
	g.pf("static SCHEMA_UNUSED uint64_t %s(%s key)\n{ return ", g.sym(n, "hash"), g.mapKeyType(f))
	if stringKey {
		g.pf("key.length>0 ? table_map_hash(key.data,key.length) : table_map_hash(NULL,0)")
	} else {
		g.pf("table_map_hash(&key,sizeof(key))")
	}
	g.pf("; }\n")
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableMap * map)\n{ int32_t slots=table_map_index_slots(map->count); return slots ? (int64_t)slots*(int64_t)sizeof(int32_t) : -1; }\n", g.api(surface, "index_measure"))
	g.pf("static SCHEMA_UNUSED TableMapIndex %s(const TableMap * map,void * storage,int64_t bytes)\n{\n    TableMapIndex index={NULL,0,0}; int32_t i,slots=table_map_index_slots(map->count); const %s * entries;\n    if(!slots || storage==NULL || ((uintptr_t)storage & (SCHEMA_TABLE_ALIGNOF(int32_t)-1)) || bytes<(int64_t)slots*(int64_t)sizeof(int32_t)) { return index; }\n    index.slots=(int32_t *)storage; index.capacity=slots; memset(storage,0,(size_t)slots*sizeof(int32_t));\n    entries=map->items.value ? (const %s *)((const uint8_t *)(const void *)&map->items+map->items.value) : NULL;\n    if(map->count && entries==NULL) { return index; }\n    for(i=0;i<map->count;i++) { int32_t at=(int32_t)(%s(%s(entries+i))&(uint64_t)(slots-1)); while(index.slots[at]) { at=(at+1)&(slots-1); } index.slots[at]=i+1; }\n    index.good=1; return index;\n}\n", g.api(surface, "index"), n, n, g.sym(n, "hash"), g.sym(n, "key"))
	g.pf("static SCHEMA_UNUSED %s %s(const TableMapIndex * index,const TableMap * map,%s key)\n{\n    int32_t at,probe; %s lookup=%s; const %s * entries;\n    if(index==NULL || !index->good) { return %s(map,key); }\n    entries=map->items.value ? (const %s *)((const uint8_t *)(const void *)&map->items+map->items.value) : NULL; if(entries==NULL) { return NULL; }\n    at=(int32_t)(%s(lookup)&(uint64_t)(index->capacity-1));\n    for(probe=0;probe<index->capacity;probe++) { int32_t slot=index->slots[at]; const %s * entry;\n        if(slot<=0 || slot>map->count) { return NULL; } entry=entries+slot-1;\n        if(%s(%s(entry),lookup)==0) { return %s; } at=(at+1)&(index->capacity-1);\n    } return NULL;\n}\n", result, g.api(surface, "index_find"), keyParam, g.mapKeyType(f), keyArg, n, g.api(surface, "find"), n, g.sym(n, "hash"), n, g.sym(n, "key_compare"), g.sym(n, "key"), found)
}
