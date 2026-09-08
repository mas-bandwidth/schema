package ctable

// A postorder measure caches each resolved frame's encoded length. Emission
// then reads that length once; hostile nesting cannot cause repeated measuring.
const cRetainOut = `
typedef struct TableRetainOut {
 uint8_t * input;int64_t size,at,bytes;TableRetainIds * ids;TableWriter * writer;int overflow;
} TableRetainOut;
static SCHEMA_UNUSED int table_retain_out_raw(TableRetainOut * s,const void * data,int64_t bytes)
{if(bytes<0||bytes>INT64_MAX-s->bytes)return 0;if(s->writer)table_writer_raw(s->writer,data,bytes);s->bytes+=bytes;return !s->writer||!s->writer->overflow;}
static SCHEMA_UNUSED int table_retain_out_leb(TableRetainOut * s,uint64_t v)
{if(s->writer)table_writer_leb(s->writer,v);s->bytes+=table_retain_leb_bytes(v);return !s->writer||!s->writer->overflow;}
static SCHEMA_UNUSED int table_retain_out_leb_read(TableRetainOut * s,uint64_t * v)
{TableReader r=table_reader_make(s->input,s->size,NULL);r.offset=s->at;if(!table_reader_leb(&r,v))return 0;s->at=r.offset;return 1;}
static SCHEMA_UNUSED int table_retain_out_ref(TableRetainOut * s)
{uint64_t id,ref;if(s->size-s->at<8)return 0;id=table_retain_u64(s->input+s->at);s->at+=8;if(!id)return table_retain_out_leb(s,0);ref=table_retain_ref(s->ids,id,1,&s->overflow);return !s->ids->lost&&!s->overflow&&table_retain_out_leb(s,ref);}
static SCHEMA_UNUSED int table_retain_out_payload(TableRetainOut *,uint8_t,int32_t);
static SCHEMA_UNUSED int table_retain_out_content(TableRetainOut *,uint8_t,int64_t,int32_t);
static SCHEMA_UNUSED int table_retain_out_frame(TableRetainOut * s,uint8_t kind,int32_t depth)
{
 uint8_t * slot;int64_t resolved,wire,begin;if(s->size-s->at<8)return 0;slot=s->input+s->at;resolved=table_retain_u32(slot);s->at+=8;
 if(!s->writer){begin=s->bytes;if(!table_retain_out_content(s,kind,resolved,depth))return 0;wire=s->bytes-begin;if(wire>UINT32_MAX)return 0;table_retain_put32(slot+4,(uint32_t)wire);s->bytes+=table_retain_leb_bytes((uint64_t)wire);return 1;}
 wire=table_retain_u32(slot+4);if(!table_retain_out_leb(s,(uint64_t)wire))return 0;begin=s->bytes;return table_retain_out_content(s,kind,resolved,depth)&&s->bytes-begin==wire;
}
static SCHEMA_UNUSED int table_retain_out_content(TableRetainOut * s,uint8_t kind,int64_t length,int32_t depth)
{
 int64_t end;if(depth>SCHEMA_TABLE_RETAIN_WALK_MAX||length<0||length>s->size-s->at)return 0;end=s->at+length;
 switch(kind){
 case 13:for(;;){uint64_t id;uint8_t k;if(end-s->at<8)return 0;id=table_retain_u64(s->input+s->at);if(!table_retain_out_ref(s))return 0;if(!id)break;if(s->at>=end)return 0;k=s->input[s->at++];if(!table_retain_out_raw(s,&k,1)||!table_retain_out_payload(s,k,depth))return 0;}break;
 case 14:case 16:{uint64_t n,i;uint8_t elem;if(s->at>=end)return 0;elem=s->input[s->at++];if(!table_retain_out_raw(s,&elem,1)||!table_retain_out_leb_read(s,&n)||!table_retain_out_leb(s,n))return 0;
 for(i=0;i<n;i++){if(kind==16){if(!table_retain_out_ref(s)||!table_retain_out_frame(s,elem,depth+1))return 0;}else if(!table_retain_out_payload(s,elem,depth))return 0;}break;}
 case 15:case 30:if(!table_retain_out_payload(s,kind,depth))return 0;break;
 case 17:return 0;
 default:if(!table_retain_out_raw(s,s->input+s->at,length))return 0;s->at+=length;break;
 }return s->at==end;
}
static SCHEMA_UNUSED int table_retain_out_payload(TableRetainOut * s,uint8_t kind,int32_t depth)
{
 int width=table_retain_width(kind);uint64_t length;
 if(width){if(width>s->size-s->at||!table_retain_out_raw(s,s->input+s->at,width))return 0;s->at+=width;return 1;}
 switch(kind){
 case 12:case 31:case 32:case 33:if(!table_retain_out_leb_read(s,&length)||length>(uint64_t)(s->size-s->at)||!table_retain_out_leb(s,length)||!table_retain_out_raw(s,s->input+s->at,(int64_t)length))return 0;s->at+=(int64_t)length;return 1;
 case 13:case 14:case 16:return table_retain_out_frame(s,kind,depth+1);
 case 15:{uint64_t arm;uint8_t k;if(s->size-s->at<8)return 0;arm=table_retain_u64(s->input+s->at);if(!table_retain_out_ref(s))return 0;if(!arm)return 1;if(s->at>=s->size)return 0;k=s->input[s->at++];return table_retain_out_raw(s,&k,1)&&table_retain_out_frame(s,k,depth+1);}
 case 30:return table_retain_out_ref(s);
 default:return 0;
 }
}
static SCHEMA_UNUSED int64_t table_retain_record_measure(uint8_t * record,TableRetainIds * ids,uint64_t * ref)
{
 int32_t mark=ids->count;TableRetainOut s;int overflow=0;ids->lost=0;*ref=table_retain_ref(ids,table_retain_u64(record+16),1,&overflow);
 if(!ids->lost&&!overflow){memset(&s,0,sizeof(s));s.input=table_retain_payload(record);s.size=table_retain_u32(record+12);s.ids=ids;if(table_retain_out_payload(&s,record[24],0)&&s.at==s.size)return table_retain_leb_bytes(*ref)+1+s.bytes;}
 table_retain_truncate(ids,mark);ids->lost=0;return -1;
}
static SCHEMA_UNUSED int table_retain_tail(TableWriter * w,TableRetainWalk keep)
{
 TableRetain * r=keep.retain;int32_t i;int64_t at=0;if(!r||!r->bytes)return 1;
 for(i=0;i<r->count;i++){uint8_t * record=r->bytes+at;uint64_t ref;int64_t bytes;TableRetainOut s;at+=table_retain_u32(record);
 if(!table_retain_here(r,record,&keep.path))continue;
 if(w->check_default){w->offset=2;return 1;}
 bytes=table_retain_record_measure(record,keep.ids,&ref);if(bytes<0)continue;
 if(!w->buffer){if(bytes>INT64_MAX-w->offset){w->overflow=1;return 0;}w->offset+=bytes;continue;}
 table_writer_leb(w,ref);table_writer_put8(w,record[24]);memset(&s,0,sizeof(s));s.input=table_retain_payload(record);s.size=table_retain_u32(record+12);s.ids=keep.ids;s.writer=w;
 if(!table_retain_out_payload(&s,record[24],0)||s.at!=s.size)return 0;record[25]=1;
 }return !w->overflow;
}
static SCHEMA_UNUSED void table_retain_clear_placed(TableRetain * r)
{int32_t i;int64_t at=0;if(!r||!r->bytes)return;for(i=0;i<r->count;i++){r->bytes[at+25]=0;at+=table_retain_u32(r->bytes+at);}}
static SCHEMA_UNUSED void table_retain_count_lost(TableRetain * r,TableReport * report)
{int32_t i;int64_t at=0;if(!r||!r->bytes)return;for(i=0;i<r->count;i++){if(!r->bytes[at+25])report->retain_lost++;at+=table_retain_u32(r->bytes+at);}}
`
