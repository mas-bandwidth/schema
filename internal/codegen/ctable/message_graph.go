package ctable

import "fmt"

import "github.com/mas-bandwidth/schema/v2/ir"

const cMessageNodes = `
static SCHEMA_UNUSED int table_message_nodes_open(TableMessageReader * r,uint64_t * count)
{
 uint64_t ref;int64_t start=r->bits.offset;*count=0;
 if(!table_bit_get(&r->bits,&ref,r->vocabulary->ref_bits))return 0;
 if(ref>(uint64_t)r->vocabulary->count)return 0;
 if(ref==0 || r->vocabulary->entries[ref-1].id!=UINT64_MAX){r->bits.offset=start;r->index_bits=1;return 1;}
 if(!table_bit_get(&r->bits,count,32))return 0;
 r->index_bits=table_bits_required(0,(int64_t)*count+1);
 /* Every record spends at least its type reference. Bound the walk before
    inspecting any record, even when its body holds no scalar bits. */
 return r->vocabulary->ref_bits>0 && *count<=(uint64_t)((r->bits.bits-r->bits.offset)/r->vocabulary->ref_bits);
}
static SCHEMA_UNUSED int table_message_blob_scan(TableMessageReader * r,uint64_t * length)
{
 return table_bit_get(&r->bits,length,32) && table_bit_align_read(&r->bits) && table_message_skip_run(r,*length,8);
}
`

func (g *tableGen) emitMessageGraph(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	g.emitMessageGraphSave(st)
	g.emitMessageGraphScan(st)
	g.emitMessageGraphLoad(st)
}

func (g *tableGen) emitMessageGraphSave(st *ir.Struct) {
	n := st.Name
	g.pf("static SCHEMA_UNUSED int %s(TableBitWriter * w,const %s * root,TableAllocator allocator)\n{\n TableNumbering numbering;uint64_t i;int ok=0;\n if(!table_number_build(&numbering,NULL,root,&%s,allocator))goto done;\n w->nodes=&numbering;w->index_bits=table_bits_required(0,(int64_t)numbering.count);\n if(numbering.count-1>UINT32_MAX)goto done;\n if(numbering.count>1){\n", g.sym(n, "message_graph_save"), n, g.graphType(n))
	g.messageHeader(ir.TableVocabularyEntry{Id: ir.TableNodeWireId}, " ")
	g.pf(" table_bit_put(w,numbering.count-1,32);for(i=1;i<numbering.count;i++){const TableNodeEntry * node=numbering.entries+i;switch(node->type->id){\n")
	bytesBlob, stringBlob := ir.PointerReachableBlobs(st)
	for _, b := range []struct {
		yes bool
		id  uint64
	}{{bytesBlob, ir.BytesWireTypeId}, {stringBlob, ir.StringWireTypeId}} {
		if !b.yes {
			continue
		}
		g.pf(" case UINT64_C(0x%016x): {const TableBlob * blob=(const TableBlob *)node->value;\n", b.id)
		g.messageHeader(ir.TableVocabularyEntry{Id: b.id}, " ")
		g.pf(" table_bit_put(w,blob->length,32);table_bit_align_write(w);table_bit_putbytes(w,blob+1,blob->length);break;}\n")
	}
	for _, t := range ir.PointerReachable(st) {
		g.pf(" case UINT64_C(0x%016x):\n", ir.TableWireId(t.WireName()))
		g.messageHeader(ir.TableVocabularyEntry{Id: ir.TableWireId(t.WireName())}, " ")
		g.pf(" if(!%s(w,(const %s *)node->value))goto done;break;\n", g.api(t.Name, "save_message_body"), t.Name)
	}
	g.pf(" default:goto done;\n } } }ok=%s(w,root);\n done:table_number_dispose(&numbering);w->nodes=NULL;return ok&&!w->overflow;\n}\n", g.api(n, "save_message_body"))
	for _, measure := range []bool{true, false} {
		verb, extra, buffer, capacity := "save_messages", ",uint8_t * buffer,int64_t capacity", "buffer", "capacity"
		if measure {
			verb, extra, buffer, capacity = "measure_messages", "", "NULL", "INT64_MAX"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s(const %s * const * roots,int64_t count%s,TableReport * report,TableAllocator allocator)\n{\n TableBitWriter w=table_bit_writer(%s,%s);int64_t i;\n if(roots==NULL||count<1)return -1;if(count>kTableMessageBatchMax){if(report){report->refused=1;report->reason=SCHEMA_TABLE_BATCH_TOO_LARGE;}return -1;}\n", g.api(n, verb+"_with_allocator"), n, extra, buffer, capacity)
		if !measure {
			g.pf(" if(buffer==NULL||capacity<0)return -1;\n")
		}
		g.pf(" table_bit_put(&w,2,8);table_bit_put(&w,(uint64_t)(count-1),8);for(i=0;i<count;i++)if(!roots[i]||!%s(&w,roots[i],allocator))return -1;\n table_bit_align_write(&w);return w.overflow?-1:w.bits/8;\n}\n", g.sym(n, "message_graph_save"))
		args := ""
		if !measure {
			args = ",buffer,capacity"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s(const %s * const * roots,int64_t count%s,TableReport * report)\n{ return %s(roots,count%s,report,table_default_allocator()); }\n", g.api(n, verb), n, extra, g.api(n, verb+"_with_allocator"), args)
	}
}

func (g *tableGen) emitMessageGraphScan(st *ir.Struct) {
	n := st.Name
	bytesBlob, stringBlob := ir.PointerReachableBlobs(st)
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,uint64_t * id,int64_t * size,uint64_t * length)\n{\n const TableMessageEntry * entry;int64_t at=0;*size=0;*length=0;\n if(!table_message_ref(r,&entry,1)||entry==NULL)return 0;*id=entry->id;\n if(*id==UINT64_C(0x%016x)||*id==UINT64_C(0x%016x)){\n if(!table_message_blob_scan(r,length))return 0;\n", g.sym(n, "message_record_scan"), ir.BytesWireTypeId, ir.StringWireTypeId)
	if bytesBlob {
		g.pf(" if(*id==UINT64_C(0x%016x))*size=table_blob_storage((int64_t)*length,0);\n", ir.BytesWireTypeId)
	}
	if stringBlob {
		g.pf(" if(*id==UINT64_C(0x%016x))*size=table_blob_storage((int64_t)*length,1);\n", ir.StringWireTypeId)
	}
	g.pf(" return *size>=0; }switch(*id){\n")
	for _, t := range ir.PointerReachable(st) {
		g.pf(" case UINT64_C(0x%016x):at=sizeof(%s);if(!%s(r,&at))return 0;break;\n", ir.TableWireId(t.WireName()), t.Name, g.sym(t.Name, "message_extent"))
	}
	g.pf(" default:return table_message_skip_body(r);\n }if(at>INT64_MAX-kTableAlign)return 0;*size=table_align_up64(at);return 1;\n}\n")
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,uint64_t * records,int64_t * data,int64_t * root_bytes,int * complete)\n{\n uint64_t i,id,length;int64_t size,root=sizeof(%s);*data=0;*complete=1;\n if(!table_message_nodes_open(r,records))return 0;for(i=0;i<*records;i++){\n if(!%s(r,&id,&size,&length)||size>INT64_MAX-*data)return 0;*data+=size;}\n if(!%s(r,&root))*complete=0;if(r->extent_refused)return 0;if(root>INT64_MAX-kTableAlign)return 0;*root_bytes=table_align_up64(root);if(*root_bytes>INT64_MAX-*data)return 0;*data+=*root_bytes;return 1;\n}\n", g.sym(n, "message_storage"), n, g.sym(n, "message_record_scan"), g.sym(n, "message_extent"))
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableVocabulary * vocabulary,const uint8_t * buffer,int64_t bytes,int64_t * attribution,TableRefuseReason * reason)\n{\n TableReport report={0};TableMessageReader r;int64_t total=0,dirs=0,bodies,i;if(attribution)*attribution=0;\n bodies=table_message_open(&r,vocabulary,buffer,bytes,&report);if(bodies<0)return -1;\n for(i=0;i<bodies;i++){uint64_t records;int64_t data,root,dir;int complete;\n if(!%s(&r,&records,&data,&root,&complete)){if(reason)*reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_LENGTH;return -1;}\n if(records>=(uint64_t)(INT64_MAX/(int64_t)sizeof(TableNodeDirEntry)))return -1;dir=(int64_t)(records+1)*(int64_t)sizeof(TableNodeDirEntry);\n if(data>INT64_MAX-dir || data+dir>INT64_MAX-total)return -1;total+=data+dir;dirs+=dir;if(!complete)break;\n }if(attribution)*attribution=dirs;return total;\n}\n", g.api(n, "load_measure_messages_ex"), g.sym(n, "message_storage"))
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableVocabulary * vocabulary,const uint8_t * buffer,int64_t bytes)\n{ return %s(vocabulary,buffer,bytes,NULL,NULL); }\n", g.api(n, "load_measure_messages"), g.api(n, "load_measure_messages_ex"))
}

func (g *tableGen) emitMessageGraphLoad(st *ir.Struct) {
	n := st.Name
	_, stringBlob := ir.PointerReachableBlobs(st)
	rootScan := ""
	if g.messageHasExtent(st) {
		rootScan = fmt.Sprintf("if(!%s(&scan,&root_size))goto malformed;", g.sym(n, "message_extent"))
	}
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,uint8_t * region,int64_t capacity,int64_t * used,const %s ** out)\n{\n uint64_t count,i,id,length;int64_t dir_bytes,records_start,fields_start,size,root_size=sizeof(%s);\n TableNodeDirEntry * directory;TableNodeMap nodes;TableRegionSink carve;TableSink sink;TableMessageReader scan;%s * root;int ok;\n if(!table_message_nodes_open(r,&count))goto malformed;dir_bytes=(int64_t)(count+1)*(int64_t)sizeof(TableNodeDirEntry);\n if(*used>capacity||dir_bytes>capacity-*used)goto malformed;directory=(TableNodeDirEntry *)(void *)(region+*used);*used+=dir_bytes;\n memset(&nodes,0,sizeof(nodes));nodes.base=region;nodes.entries=directory;nodes.count=count+1;records_start=r->bits.offset;\n for(i=0;i<count;i++){if(!%s(r,&id,&size,&length))goto malformed;directory[i+1].type_id=id;directory[i+1].offset=UINT64_MAX;\n if(size==0){r->report->unknown++;continue;}if(size>capacity-*used)goto malformed;directory[i+1].offset=(uint64_t)*used;\n if(id==UINT64_C(0x%016x)||id==UINT64_C(0x%016x)){TableBlob * blob=(TableBlob *)(void *)(region+*used);blob->length=(uint32_t)length;blob->zero=0;}*used+=size;\n }\n fields_start=r->bits.offset;scan=*r;(void)scan;%sif(root_size>INT64_MAX-kTableAlign)goto malformed;root_size=table_align_up64(root_size);\n if(root_size>capacity-*used)goto malformed;root=(%s *)(void *)(region+*used);directory[0].offset=(uint64_t)*used;directory[0].type_id=UINT64_C(0x%016x);*used+=root_size;%s(root);*out=root;nodes.good=1;r->nodes=&nodes;\n sink.worker=NULL;sink.region=&carve;nodes.sink=&sink;r->bits.offset=records_start;\n", g.sym(n, "message_load_into"), n, n, n, g.sym(n, "message_record_scan"), ir.BytesWireTypeId, ir.StringWireTypeId, rootScan, n, ir.TableWireId(st.WireName()), g.api(n, "reset"))
	if g.retain {
		g.pf(" table_retain_reset(retention.retain,&nodes,region);\n")
	}
	g.pf(" for(i=0;i<count;i++){const TableMessageEntry * entry;uint8_t * value;\n if(!table_message_ref(r,&entry,1)||entry==NULL)goto malformed;id=entry->id;\n if(id==UINT64_C(0x%016x)||id==UINT64_C(0x%016x)){\n if(!table_bit_get(&r->bits,&length,32)||!table_bit_align_read(&r->bits)||!table_bit_has(&r->bits,(int64_t)length*8))goto malformed;\n", ir.BytesWireTypeId, ir.StringWireTypeId)
	if stringBlob {
		g.pf(" if(id==UINT64_C(0x%016x)&&!table_wire_utf8(r->bits.buffer+(r->bits.offset>>3),length))goto malformed;\n", ir.StringWireTypeId)
	}
	g.pf(" if(directory[i+1].offset!=UINT64_MAX)memcpy(region+directory[i+1].offset+sizeof(TableBlob),r->bits.buffer+(r->bits.offset>>3),(size_t)length);r->bits.offset+=(int64_t)length*8;continue;\n }\n if(directory[i+1].offset==UINT64_MAX){if(!table_message_skip_body(r))goto malformed;continue;}\n value=region+directory[i+1].offset;carve.base=value;switch(id){\n")
	for _, t := range ir.PointerReachable(st) {
		if g.retain {
			g.pf(" case UINT64_C(0x%016x):retention.path=table_retain_root(value,(uint32_t)i+2);\n", ir.TableWireId(t.WireName()))
		}
		prefix := "case UINT64_C(0x%016x):"
		if g.retain {
			prefix = "/* type 0x%016x */"
		}
		g.pf(" "+prefix+"carve.used=sizeof(%s);carve.capacity=sizeof(%s);scan=*r;if(!%s(&scan,&carve.capacity))goto malformed;if(!%s(r,(%s *)(void *)value))goto malformed;break;\n", ir.TableWireId(t.WireName()), t.Name, t.Name, g.sym(t.Name, "message_extent"), g.api(t.Name, "load_message_body"), t.Name)
	}
	g.pf(" default:goto malformed;}\n }if(r->bits.offset!=fields_start)goto malformed;%scarve.base=(uint8_t *)(void *)root;carve.used=sizeof(*root);carve.capacity=root_size;\n ok=%s(r,root);r->nodes=NULL;if(!ok)return 0;*out=root;return 1;\n malformed:r->nodes=NULL;r->report->malformed=1;return 0;\n}\n", g.retainMessageRoot(), g.api(n, "load_message_body"))
	if g.retain {
		return
	}
	g.pf("static SCHEMA_UNUSED int %s(const %s ** roots,int64_t * count,uint8_t * region,int64_t region_bytes,const TableVocabulary * vocabulary,const uint8_t * buffer,int64_t bytes,TableReport * report)\n{\n TableReport ignored={0};TableMessageReader r;int64_t bodies,capacity,used=0,i;if(report==NULL)report=&ignored;\n if(roots==NULL||count==NULL){report->malformed=1;return 0;}capacity=*count;*count=0;bodies=table_message_open(&r,vocabulary,buffer,bytes,report);if(bodies<0)return 0;\n if(bodies>capacity){*count=bodies;report->refused=1;report->reason=SCHEMA_TABLE_BATCH_TOO_LARGE;return 0;}\n if(region==NULL||region_bytes<0||((uintptr_t)region&(kTableAlign-1))){report->malformed=1;return 0;}memset(region,0,(size_t)region_bytes);\n for(i=0;i<bodies;i++){roots[i]=NULL;if(!%s(&r,region,region_bytes,&used,roots+i))return 0;(*count)++;}return table_message_close(&r);\n}\n", g.api(n, "load_messages"), n, g.sym(n, "message_load_into"))
}

func (g *tableGen) retainMessageRoot() string {
	if g.retain {
		return "retention.path=table_retain_root(root,1);"
	}
	return ""
}
