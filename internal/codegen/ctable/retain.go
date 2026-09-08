package ctable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// Retention owns two caller-sized stores. Its records are private resolved
// fields: directory node, declaration/index path, full IDs and patched lengths.
// The ordinary codec carries none of this state.
func (g *tableGen) retainRuntime() string {
	var ids strings.Builder
	for _, id := range ir.TableWireIds(g.unit) {
		fmt.Fprintf(&ids, "UINT64_C(0x%016x),", id)
	}
	return strings.NewReplacer("@DEPTH@", fmt.Sprint(g.retainDepth()), "@KNOWN@", ids.String(), "@RETAIN_OUT@", cRetainOut, "@RETAIN_MESSAGE@", cRetainMessage+cRetainGraph).Replace(cRetainRuntime)
}
func (g *tableGen) retainDepth() int {
	var field func(*ir.Field) int
	var record func(*ir.Struct) int
	memo := map[string]int{}
	record = func(st *ir.Struct) int {
		if n, ok := memo[st.Name]; ok {
			return n
		}
		n := 0
		for _, f := range st.Fields {
			n = max(n, field(f))
		}
		memo[st.Name] = n
		return n
	}
	field = func(f *ir.Field) int {
		if f.Type.Pointer {
			return 0
		}
		if f.IsMap() {
			return 1 + record(f.MapEntry)
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return 1 + record(ref)
		case *ir.Union:
			n := 0
			for _, v := range ref.Variants {
				if !v.Void() {
					n = max(n, 1+field(v.F))
				}
			}
			return n + 1
		}
		return 0
	}
	n := 1
	for name := range ir.TableClosure(g.unit) {
		st := g.unit.Tables[name]
		if st == nil {
			st = g.unit.Structs[name]
		}
		if st != nil {
			n = max(n, record(st))
		}
	}
	return n
}

const cRetainRuntime = `
#ifndef SCHEMA_TABLE_RETAIN_RUNTIME
#define SCHEMA_TABLE_RETAIN_RUNTIME
#define SCHEMA_TABLE_RETAIN_DEPTH_MAX @DEPTH@
#define SCHEMA_TABLE_RETAIN_WALK_MAX 64
#define SCHEMA_TABLE_RETAIN_HEADER 26

typedef struct TableRetainId { uint64_t id; int32_t slot; } TableRetainId;
typedef struct TableRetain {
 uint8_t * bytes; int64_t capacity,used;
 TableRetainId * ids; int32_t id_capacity,id_used,count;
 const uint8_t * base; const TableNodeDirEntry * directory; int64_t directory_count;
} TableRetain;
typedef struct TableRetainStep {uint32_t ordinal,index;} TableRetainStep;
typedef struct TableRetainPath {
 const void * at; uint32_t node; int32_t depth;
 TableRetainStep steps[SCHEMA_TABLE_RETAIN_DEPTH_MAX];
} TableRetainPath;
typedef struct TableRetainIds {
 TableWriteIds known;
 int32_t slots[sizeof(((TableWriteIds *)0)->ids)/sizeof(uint64_t)];
 TableRetain * retain; int32_t count; int lost;
} TableRetainIds;
typedef struct TableRetainWalk {TableRetain * retain;TableRetainIds * ids;TableRetainPath path;} TableRetainWalk;
static const uint64_t table_retain_known[]={@KNOWN@};
static SCHEMA_UNUSED uint32_t table_retain_u32(const uint8_t * p)
{return (uint32_t)p[0]|(uint32_t)p[1]<<8|(uint32_t)p[2]<<16|(uint32_t)p[3]<<24;}
static SCHEMA_UNUSED uint64_t table_retain_u64(const uint8_t * p)
{return table_retain_u32(p)|(uint64_t)table_retain_u32(p+4)<<32;}
static SCHEMA_UNUSED void table_retain_put32(uint8_t * p,uint32_t v)
{int i;for(i=0;i<4;i++)p[i]=(uint8_t)(v>>(8*i));}
static SCHEMA_UNUSED void table_retain_put64(uint8_t * p,uint64_t v)
{table_retain_put32(p,(uint32_t)v);table_retain_put32(p+4,(uint32_t)(v>>32));}
static SCHEMA_UNUSED int64_t table_retain_leb_bytes(uint64_t v)
{int64_t n=1;while(v>=128){v>>=7;n++;}return n;}
static SCHEMA_UNUSED int table_retain_reserved(uint64_t id)
{return id>=UINT64_MAX-2;}
static SCHEMA_UNUSED TableRetainPath table_retain_root(const void * at,uint32_t node)
{TableRetainPath p;memset(&p,0,sizeof(p));p.at=at;p.node=node;return p;}
static SCHEMA_UNUSED TableRetainWalk table_retain_step(TableRetainWalk keep,uint32_t ordinal,uint32_t index)
{if(keep.path.depth<SCHEMA_TABLE_RETAIN_DEPTH_MAX){keep.path.steps[keep.path.depth].ordinal=ordinal;keep.path.steps[keep.path.depth].index=index;}keep.path.depth++;return keep;}
static SCHEMA_UNUSED TableRetainWalk table_retain_index(TableRetainWalk keep,uint32_t index)
{if(keep.path.depth>0&&keep.path.depth<=SCHEMA_TABLE_RETAIN_DEPTH_MAX)keep.path.steps[keep.path.depth-1].index=index;return keep;}
static SCHEMA_UNUSED uint8_t * table_retain_payload(uint8_t * record)
{return record+SCHEMA_TABLE_RETAIN_HEADER+(int64_t)table_retain_u32(record+8)*8;}
static SCHEMA_UNUSED const void * table_retain_at(const TableRetain * retain,const uint8_t * record)
{uint32_t node=table_retain_u32(record+4);if(!retain->directory||!node||node>retain->directory_count)return NULL;return retain->base+retain->directory[node-1].offset;}
static SCHEMA_UNUSED int table_retain_under(const TableRetain * retain,const uint8_t * record,const TableRetainPath * path,int field,uint32_t ordinal)
{
 uint32_t depth=table_retain_u32(record+8);int32_t i;const uint8_t * steps=record+SCHEMA_TABLE_RETAIN_HEADER;
 if(path->depth<0||path->depth>SCHEMA_TABLE_RETAIN_DEPTH_MAX||table_retain_at(retain,record)!=path->at)return 0;
 if(depth<(uint32_t)path->depth||(field&&depth==(uint32_t)path->depth))return 0;
 for(i=0;i<path->depth;i++)if(table_retain_u32(steps+8*i)!=path->steps[i].ordinal||table_retain_u32(steps+8*i+4)!=path->steps[i].index)return 0;
 return !field||table_retain_u32(steps+8*path->depth)==ordinal;
}
static SCHEMA_UNUSED int table_retain_here(const TableRetain * retain,const uint8_t * record,const TableRetainPath * path)
{return table_retain_u32(record+8)==(uint32_t)path->depth&&table_retain_under(retain,record,path,0,0);}
static SCHEMA_UNUSED void table_retain_discard(TableRetainWalk keep,int field,uint32_t ordinal)
{
 TableRetain * r=keep.retain;int64_t read=0,write=0;int32_t i,kept=0;if(!r||!r->bytes)return;
 for(i=0;i<r->count;i++){uint32_t n=table_retain_u32(r->bytes+read);if(!table_retain_under(r,r->bytes+read,&keep.path,field,ordinal)){if(read!=write)memmove(r->bytes+write,r->bytes+read,n);write+=n;kept++;}read+=n;}r->used=write;r->count=kept;
}
static SCHEMA_UNUSED void table_retain_reset(TableRetain * retain,const TableNodeMap * nodes,const uint8_t * region)
{if(retain){retain->used=0;retain->id_used=0;retain->count=0;retain->base=region;retain->directory=nodes?nodes->entries:NULL;retain->directory_count=nodes?(int64_t)nodes->count:0;}}
static SCHEMA_UNUSED int table_retain_nameable(uint64_t id)
{int64_t lo=0,hi=(int64_t)(sizeof(table_retain_known)/sizeof(table_retain_known[0]))-1;while(lo<=hi){int64_t mid=lo+(hi-lo)/2;if(table_retain_known[mid]==id)return 1;if(table_retain_known[mid]<id)lo=mid+1;else hi=mid-1;}return 0;}
static SCHEMA_UNUSED uint64_t table_retain_ref(TableRetainIds * ids,uint64_t id,int record,int * overflow)
{
 int32_t i;TableRetain * r=ids->retain;TableWriteIds * v=&ids->known;
 if(record&&!table_retain_nameable(id)){
  if(!r){ids->lost=1;return 0;}for(i=0;i<r->id_used;i++)if(r->ids[i].id==id)return (uint64_t)r->ids[i].slot;
  if(!r->ids||r->id_used>=r->id_capacity||ids->count==INT32_MAX){ids->lost=1;return 0;}
  r->ids[r->id_used].id=id;r->ids[r->id_used++].slot=++ids->count;return (uint64_t)ids->count;
 }
 for(i=0;i<v->count;i++)if(v->ids[i]==id)return (uint64_t)ids->slots[i];
 if(v->count==(int)(sizeof(v->ids)/sizeof(v->ids[0]))||ids->count==INT32_MAX){*overflow=1;return 0;}
 v->ids[v->count]=id;ids->slots[v->count++]=++ids->count;return (uint64_t)ids->count;
}
static SCHEMA_UNUSED void table_retain_truncate(TableRetainIds * ids,int32_t mark)
{while(ids->known.count&&ids->slots[ids->known.count-1]>mark)ids->known.count--;if(ids->retain)while(ids->retain->id_used&&ids->retain->ids[ids->retain->id_used-1].slot>mark)ids->retain->id_used--;ids->count=mark;}
static SCHEMA_UNUSED void table_retain_writer_id(TableWriter * w,uint64_t id,TableRetainWalk keep)
{table_writer_leb(w,table_retain_ref(keep.ids,id,0,&w->overflow));}
static SCHEMA_UNUSED TableWriter table_retain_probe(const TableWriter * w,TableRetainWalk keep)
{TableWriter p=*w;p.buffer=NULL;p.capacity=INT64_MAX;p.offset=0;p.overflow=0;p.id_checkpoint=keep.ids->count;return p;}
static SCHEMA_UNUSED void table_retain_rewind(const TableWriter * probe,TableRetainWalk keep)
{table_retain_truncate(keep.ids,probe->id_checkpoint);}
static SCHEMA_UNUSED void table_retain_finish(TableWriter * w,TableRetainIds * ids)
{int32_t i=0,j=0;TableRetain * r=ids->retain;while(i<ids->known.count||(r&&j<r->id_used)){if(!r||j>=r->id_used||(i<ids->known.count&&ids->slots[i]<r->ids[j].slot))table_writer_put64(w,ids->known.ids[i++]);else table_writer_put64(w,r->ids[j++].id);}table_writer_put64(w,(uint64_t)ids->count);}

/* Capture writes only within the remaining record capacity. Failure leaves
   used/count unchanged; a huge zero-bit message count reaches this bound
   before it can command an unbounded transcode. */
typedef struct TableRetainIn {
 TableReader input;uint8_t * out;int64_t capacity,used;int failed;
} TableRetainIn;
static SCHEMA_UNUSED int table_retain_raw(TableRetainIn * s,const void * data,int64_t bytes)
{if(s->failed||bytes<0||s->used>s->capacity||bytes>s->capacity-s->used){s->failed=1;return 0;}if(bytes)memcpy(s->out+s->used,data,(size_t)bytes);s->used+=bytes;return 1;}
static SCHEMA_UNUSED int table_retain_leb(TableRetainIn * s,uint64_t v)
{uint8_t b[10];int n=0;do{b[n++]=(uint8_t)((v&127)|(v>=128?128:0));v>>=7;}while(v);return table_retain_raw(s,b,n);}
static SCHEMA_UNUSED int table_retain_id(TableRetainIn * s,uint64_t id)
{uint8_t b[8];table_retain_put64(b,id);return table_retain_raw(s,b,8);}
static SCHEMA_UNUSED int table_retain_in_ref(TableRetainIn * s,int zero)
{uint64_t ref,id;if(!table_reader_leb(&s->input,&ref))return 0;if(!ref)return zero&&table_retain_id(s,0);if(ref>s->input.id_count)return 0;id=table_reader_id_at(&s->input,ref);return id&&!table_retain_reserved(id)&&table_retain_id(s,id);}
static SCHEMA_UNUSED int table_retain_in_payload(TableRetainIn *,uint8_t,int32_t);
static SCHEMA_UNUSED int table_retain_in_content(TableRetainIn *,uint8_t,int64_t,int32_t);
static SCHEMA_UNUSED int table_retain_in_frame(TableRetainIn * s,uint8_t kind,int64_t length,int32_t depth)
{uint8_t zero[8]={0};int64_t mark=s->used;if(!table_retain_raw(s,zero,8)||!table_retain_in_content(s,kind,length,depth))return 0;if(s->used-mark-8>UINT32_MAX)return 0;table_retain_put32(s->out+mark,(uint32_t)(s->used-mark-8));return 1;}
static SCHEMA_UNUSED int table_retain_in_content(TableRetainIn * s,uint8_t kind,int64_t length,int32_t depth)
{
 int64_t end;TableReader * r=&s->input;if(depth>SCHEMA_TABLE_RETAIN_WALK_MAX||!table_reader_has(r,length))return 0;end=r->offset+length;
 switch(kind){
 case 13:for(;;){uint64_t ref;int64_t mark=r->offset;uint8_t k;if(!table_reader_leb(r,&ref))return 0;if(!ref){if(!table_retain_id(s,0))return 0;break;}r->offset=mark;if(!table_retain_in_ref(s,0)||r->offset>=end)return 0;k=table_reader_get8(r);if(!table_retain_raw(s,&k,1)||!table_retain_in_payload(s,k,depth)||r->offset>end)return 0;}break;
 case 14:case 16:{uint8_t elem;uint64_t n,i;if(r->offset>=end)return 0;elem=table_reader_get8(r);if(!table_retain_raw(s,&elem,1)||!table_reader_leb(r,&n)||!table_retain_leb(s,n))return 0;
 for(i=0;i<n;i++){if(kind==16){uint64_t bytes;if(!table_retain_in_ref(s,0)||!table_reader_leb(r,&bytes)||bytes>(uint64_t)(end-r->offset)||!table_retain_in_frame(s,elem,(int64_t)bytes,depth+1))return 0;}else if(!table_retain_in_payload(s,elem,depth))return 0;if(r->offset>end)return 0;}break;}
 case 15:case 30:if(!table_retain_in_payload(s,kind,depth))return 0;break;
 case 17:return 0;
 default:if(!table_retain_raw(s,r->buffer+r->offset,length))return 0;r->offset+=length;break;
 }return r->offset==end;
}
static SCHEMA_UNUSED int table_retain_width(uint8_t kind)
{switch(kind){case 1:case 2:case 6:case 20:case 25:return 1;case 3:case 7:case 21:case 26:return 2;case 4:case 8:case 10:case 22:case 27:return 4;case 5:case 9:case 11:case 23:case 28:return 8;case 18:case 19:case 24:case 29:return 16;default:return 0;}}
static SCHEMA_UNUSED int table_retain_in_payload(TableRetainIn * s,uint8_t kind,int32_t depth)
{
 TableReader * r=&s->input;uint64_t length;int width=table_retain_width(kind);
 if(width){if(!table_reader_has(r,width)||!table_retain_raw(s,r->buffer+r->offset,width))return 0;r->offset+=width;return 1;}
 switch(kind){
 case 12:case 31:case 32:case 33:if(!table_reader_leb(r,&length)||!table_reader_room(r,length)||!table_retain_leb(s,length)||!table_retain_raw(s,r->buffer+r->offset,(int64_t)length))return 0;r->offset+=(int64_t)length;return 1;
 case 13:case 14:case 16:if(!table_reader_leb(r,&length)||!table_reader_room(r,length))return 0;return table_retain_in_frame(s,kind,(int64_t)length,depth+1);
 case 15:{uint64_t arm;uint8_t k;int64_t mark=r->offset;if(!table_reader_leb(r,&arm))return 0;r->offset=mark;if(!table_retain_in_ref(s,1))return 0;if(!arm)return 1;if(!table_reader_has(r,1))return 0;k=table_reader_get8(r);if(!table_retain_raw(s,&k,1)||!table_reader_leb(r,&length)||!table_reader_room(r,length))return 0;return table_retain_in_frame(s,k,(int64_t)length,depth+1);}
 case 30:return table_retain_in_ref(s,1);
 default:return 0;
 }
}
static SCHEMA_UNUSED uint8_t * table_retain_begin(TableRetainWalk keep,uint64_t id,uint8_t kind,TableRetainIn * s)
{
 TableRetain * r=keep.retain;int64_t header=SCHEMA_TABLE_RETAIN_HEADER+8*(int64_t)keep.path.depth;uint8_t * p;int32_t i;
 if(table_retain_reserved(id)||!r||!r->bytes||keep.path.depth<0||keep.path.depth>SCHEMA_TABLE_RETAIN_DEPTH_MAX||r->used<0||r->used>r->capacity||header>r->capacity-r->used||r->count==INT32_MAX)return NULL;
 p=r->bytes+r->used;table_retain_put32(p+4,keep.path.node);table_retain_put32(p+8,(uint32_t)keep.path.depth);table_retain_put64(p+16,id);p[24]=kind;p[25]=0;
 for(i=0;i<keep.path.depth;i++){table_retain_put32(p+SCHEMA_TABLE_RETAIN_HEADER+8*i,keep.path.steps[i].ordinal);table_retain_put32(p+SCHEMA_TABLE_RETAIN_HEADER+8*i+4,keep.path.steps[i].index);}
 memset(s,0,sizeof(*s));s->out=p+header;s->capacity=r->capacity-r->used-header;if(s->capacity>UINT32_MAX-header)s->capacity=UINT32_MAX-header;return p;
}
static SCHEMA_UNUSED void table_retain_commit(TableRetainWalk keep,uint8_t * record,const TableRetainIn * s,TableReport * report)
{int64_t bytes=SCHEMA_TABLE_RETAIN_HEADER+8*(int64_t)keep.path.depth+s->used;table_retain_put32(record,(uint32_t)bytes);table_retain_put32(record+12,(uint32_t)s->used);keep.retain->used+=bytes;keep.retain->count++;report->retained++;}
static SCHEMA_UNUSED int table_retain_capture(TableReader * r,uint64_t id,uint8_t kind,TableRetainWalk keep)
{
 TableRetainIn s;TableReader source=*r;uint8_t * record;if(!table_reader_skip(r,kind))return 0;if(!keep.retain)return 1;
 record=table_retain_begin(keep,id,kind,&s);if(!record){r->report->retain_lost++;return 1;}source.size=r->offset;s.input=source;
 if(!table_retain_in_payload(&s,kind,0)||s.input.offset!=source.size){r->report->retain_lost++;return 1;}table_retain_commit(keep,record,&s,r->report);return 1;
}

@RETAIN_OUT@
@RETAIN_MESSAGE@
#endif
`
