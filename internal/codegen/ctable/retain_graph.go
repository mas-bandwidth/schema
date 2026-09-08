package ctable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cRetainGraph = `
typedef int (*TableRetainSaveNode)(TableWriter *,const void *,uint64_t,TableRetainWalk);
static SCHEMA_UNUSED int table_retain_records(TableWriter * w,TableRetainWalk retention,TableRetainSaveNode save)
{
 uint64_t i;const TableNumbering * n=w->nodes;table_writer_leb(w,n->count-1);
 for(i=1;i<n->count;i++){const TableNodeEntry * e=n->entries+i;retention.path=table_retain_root(e->value,0);table_retain_writer_id(w,e->type->id,retention);
 if(!w->buffer){int64_t begin=w->offset;if(!save(w,e->value,e->type->id,retention))return 0;table_writer_leb(w,(uint64_t)(w->offset-begin));}
 else {TableWriter probe=table_retain_probe(w,retention);if(!save(&probe,e->value,e->type->id,retention))return 0;table_retain_rewind(&probe,retention);table_writer_leb(w,(uint64_t)probe.offset);if(!save(w,e->value,e->type->id,retention))return 0;}}
 return !w->overflow;
}
static SCHEMA_UNUSED int64_t table_retain_graph_save(const void * root,const TableNodeType * type,TableRetain * retain,uint8_t * buffer,int64_t capacity,TableReport * report,TableAllocator allocator,TableRetainSaveNode save)
{
 TableNumbering numbering;TableRetainIds ids;TableRetainWalk retention;TableWriter w;int64_t result=-1;
 memset(&ids,0,sizeof(ids));ids.retain=retain;memset(&retention,0,sizeof(retention));retention.retain=retain;retention.ids=&ids;retention.path=table_retain_root(root,1);
 if(retain)retain->id_used=0;if(buffer)table_retain_clear_placed(retain);w=table_writer_make(buffer,capacity,&ids.known);
 if(!table_number_build(&numbering,NULL,root,type,allocator)){table_number_dispose(&numbering);return -1;}w.nodes=&numbering;
 table_writer_put8(&w,1);if(!save(&w,root,type->id,retention))goto done;w.offset--;
 if(numbering.count>1){table_retain_writer_id(&w,UINT64_MAX,retention);table_writer_put8(&w,12);
 if(!buffer){int64_t begin=w.offset;if(!table_retain_records(&w,retention,save))goto done;table_writer_leb(&w,(uint64_t)(w.offset-begin));}
 else {TableWriter probe=table_retain_probe(&w,retention);if(!table_retain_records(&probe,retention,save))goto done;table_retain_rewind(&probe,retention);table_writer_leb(&w,(uint64_t)probe.offset);if(!table_retain_records(&w,retention,save))goto done;}}
 table_writer_put8(&w,0);table_retain_finish(&w,&ids);if(!w.overflow){result=w.offset;if(buffer)table_retain_count_lost(retain,report);}
 done:table_number_dispose(&numbering);return result;
}
`

func (g *tableGen) emitRetainRoots(members []*ir.Struct) {
	if !g.anyVariable {
		return
	}
	for _, st := range members {
		if !g.isVar(st.Name) || st.IsMapEntry() {
			continue
		}
		g.emitRetainFileRoot(st)
		g.emitRetainMessageRoot(st)
	}
}
func (g *tableGen) emitRetainFileRoot(st *ir.Struct) {
	n := st.Name
	// Reuse the graph fill itself with retained body calls; region sizing and
	// allocation are unchanged. No plain load executes a retention branch.
	previous := g.body.String()
	g.body.Reset()
	g.retain = true
	g.emitGraphLoad(st)
	g.retain = false
	names := map[string]string{g.sym(n, "load_graph"): g.sym(n, "load_graph_retain")}
	for name := range ir.TableClosure(g.unit) {
		fn := g.api(name, "load_body")
		names[fn] = fn + "_retain"
	}
	code := g.body.String()
	g.body.Reset()
	g.body.WriteString(previous)
	g.body.WriteString(retainedCode(code, names, g.sym(n, "load_graph")))
	g.pf("static SCHEMA_UNUSED const %s * %s(uint8_t * region,int64_t region_bytes,const uint8_t * wire,int64_t wire_bytes,TableRetain * retain,TableReport * report)\n{\n", n, g.api(n, "load_retain"))
	g.pf(" TableReport ignored={0};TableReader reader;TableNodeMap nodes;TableNodeDirEntry * directory;TableRegionSink place;TableSink sink;TableRetainWalk retention;int64_t data,records,total;\n if(report==NULL)report=&ignored;table_retain_reset(retain,NULL,NULL);\n if(!table_wire_open(&reader,wire,wire_bytes,report))return NULL;total=%s(&reader,&data,&records);\n if(total<0||region==NULL||region_bytes<total||((uintptr_t)region&(kTableAlign-1))){report->malformed=1;return NULL;}\n memset(region,0,(size_t)total);directory=(TableNodeDirEntry *)(void *)(region+data);directory[0].offset=0;directory[0].type_id=UINT64_C(0x%016x);\n memset(&nodes,0,sizeof(nodes));nodes.base=region;nodes.entries=directory;nodes.count=(uint64_t)records+1;place.base=region;place.capacity=data;place.used=%s(&reader);sink.region=&place;sink.worker=NULL;\n table_retain_reset(retain,&nodes,region);memset(&retention,0,sizeof(retention));retention.retain=retain;retention.path=table_retain_root(region,1);\n if(!%s(&reader,(%s *)(void *)region,&nodes,directory,&sink,retention))return NULL;return (const %s *)(const void *)region;\n}\n", g.sym(n, "load_layout"), ir.TableWireId(st.WireName()), g.sym(n, "wire_storage"), g.sym(n, "load_graph_retain"), n, n)
	dispatch := g.sym(n, "retain_save_node")
	g.pf("static SCHEMA_UNUSED int %s(TableWriter * w,const void * value,uint64_t id,TableRetainWalk retention)\n{\n switch(id){\n", dispatch)
	seen := map[string]bool{}
	for _, t := range append([]*ir.Struct{st}, ir.PointerReachable(st)...) {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		g.pf(" case UINT64_C(0x%016x):return %s_retain(w,(const %s *)value,retention);\n", ir.TableWireId(t.WireName()), g.api(t.Name, "save_body"), t.Name)
	}
	b, s := ir.PointerReachableBlobs(st)
	for _, blob := range []struct {
		yes bool
		id  uint64
	}{{b, ir.BytesWireTypeId}, {s, ir.StringWireTypeId}} {
		if blob.yes {
			g.pf(" case UINT64_C(0x%016x):{const TableBlob * blob=(const TableBlob *)value;table_writer_raw(w,blob+1,blob->length);return !w->overflow;}\n", blob.id)
		}
	}
	g.pf(" default:return 0;\n }\n}\n")
	g.pf("static SCHEMA_UNUSED int64_t %s(const %s * root,TableRetain * retain,TableAllocator allocator)\n{if(!root)return -1;return table_retain_graph_save(root,&%s,retain,NULL,INT64_MAX,NULL,allocator,%s);}\n", g.api(n, "measure_retain_with_allocator"), n, g.graphType(n), dispatch)
	g.pf("static SCHEMA_UNUSED int64_t %s(const %s * root,TableRetain * retain)\n{return %s(root,retain,table_default_allocator());}\n", g.api(n, "measure_retain"), n, g.api(n, "measure_retain_with_allocator"))
	g.pf("static SCHEMA_UNUSED int64_t %s(const %s * root,TableRetain * retain,uint8_t * buffer,int64_t capacity,TableReport * report,TableAllocator allocator)\n{if(!root||!buffer||capacity<0||!report)return -1;return table_retain_graph_save(root,&%s,retain,buffer,capacity,report,allocator,%s);}\n", g.api(n, "save_retain_with_allocator"), n, g.graphType(n), dispatch)
	g.pf("static SCHEMA_UNUSED int64_t %s(const %s * root,TableRetain * retain,uint8_t * buffer,int64_t capacity,TableReport * report)\n{return %s(root,retain,buffer,capacity,report,table_default_allocator());}\n", g.api(n, "save_retain"), n, g.api(n, "save_retain_with_allocator"))
}

func (g *tableGen) emitRetainMessageRoot(st *ir.Struct) {
	n := st.Name
	previous := g.body.String()
	g.body.Reset()
	g.retain = true
	g.emitMessageGraphLoad(st)
	g.retain = false
	names := map[string]string{g.sym(n, "message_load_into"): g.sym(n, "message_load_into_retain")}
	for name := range ir.TableClosure(g.unit) {
		fn := g.api(name, "load_message_body")
		names[fn] = fn + "_retain"
	}
	code := g.body.String()
	g.body.Reset()
	g.body.WriteString(previous)
	g.body.WriteString(retainedCode(code, names, g.sym(n, "message_load_into")))
	g.pf("static SCHEMA_UNUSED int %s(const %s ** roots,int64_t * count,uint8_t * region,int64_t region_bytes,const TableVocabulary * vocabulary,const uint8_t * buffer,int64_t bytes,TableRetain * retains,TableReport * report)\n{\n", g.api(n, "load_retain_messages"), n)
	g.pf(" TableReport ignored={0};TableMessageReader r;TableRetainWalk retention;int64_t bodies,capacity,used=0,i;if(!report)report=&ignored;\n if(!roots||!count){report->malformed=1;return 0;}capacity=*count;*count=0;bodies=table_message_open(&r,vocabulary,buffer,bytes,report);if(bodies<0)return 0;\n if(bodies>capacity){*count=bodies;report->refused=1;report->reason=SCHEMA_TABLE_BATCH_TOO_LARGE;return 0;}\n if(!region||region_bytes<0||((uintptr_t)region&(kTableAlign-1))){report->malformed=1;return 0;}memset(region,0,(size_t)region_bytes);\n for(i=0;i<bodies;i++){roots[i]=NULL;memset(&retention,0,sizeof(retention));retention.retain=retains?retains+i:NULL;table_retain_reset(retention.retain,NULL,NULL);if(!%s(&r,region,region_bytes,&used,roots+i,retention))return 0;(*count)++;}return table_message_close(&r);\n}\n", g.sym(n, "message_load_into_retain"))
}

// C99 has no variadic templates. The diagnostic bit-field is instantiated only
// when the refused macro is called, so fixed-only units carry no runtime state.
func (g *tableGen) emitRetainRefusals(members []*ir.Struct) {
	for _, st := range members {
		if st.IsMapEntry() {
			continue
		}
		if !g.isVar(st.Name) {
			for _, verb := range []string{"load_retain", "measure_retain", "save_retain", "load_retain_messages"} {
				g.pf("/* Retention requires a variable root loaded into a region (SPEC-TABLES section 6.6). */\n#define %s(...) ((void)sizeof(struct { int retention_requires_variable_region_root : -1; }))\n", g.api(st.Name, verb))
			}
		}
		g.pf("/* Retained fields have no announced slots: save the FILE form or relay the original message. */\n#define %s(...) ((void)sizeof(struct { int retention_message_write_requires_file_form : -1; }))\n", g.api(st.Name, "save_retain_messages"))
	}
}
