package ctable

const cRetainMessage = `
typedef struct TableRetainMessage {TableRetainIn output;TableMessageReader input;} TableRetainMessage;
static SCHEMA_UNUSED int table_retain_message_payload(TableRetainMessage *,const TableMessageEntry *,int32_t);
static SCHEMA_UNUSED int table_retain_message_content(TableRetainMessage *,const TableMessageEntry *,int32_t);
static SCHEMA_UNUSED int table_retain_message_frame(TableRetainMessage * s,const TableMessageEntry * e,int32_t depth)
{uint8_t zero[8]={0};int64_t mark=s->output.used;if(!table_retain_raw(&s->output,zero,8)||!table_retain_message_content(s,e,depth))return 0;if(s->output.used-mark-8>UINT32_MAX)return 0;table_retain_put32(s->output.out+mark,(uint32_t)(s->output.used-mark-8));return 1;}
static SCHEMA_UNUSED int table_retain_message_scalar(TableRetainMessage * s,const TableMessageEntry * e)
{
 uint64_t lo=0,hi=0;uint8_t bytes[16];int width=table_retain_width(e->kind);if(!width)return 0;
 if(e->kind==10&&e->packing==2){float value;if(!table_message_float(&s->input,e,&value))return 0;lo=table_float_to_bits(value);}
 else if(!table_message_number(&s->input,e,&lo,&hi))return 0;
 table_retain_put64(bytes,lo);table_retain_put64(bytes+8,hi);return table_retain_raw(&s->output,bytes,width);
}
static SCHEMA_UNUSED int table_retain_message_opaque(TableRetainMessage * s,const TableMessageEntry * e,int framed)
{
 uint64_t n,i;TableMessageReader * r=&s->input;
 if(e->kind==12||e->kind==33){if(!table_bit_get(&r->bits,&n,table_bits_required(0,e->max)))return 0;if(e->kind==33)n*=2;else if(!table_bit_align_read(&r->bits))return 0;}
 else if(!table_bit_align_read(&r->bits)||!table_bit_get(&r->bits,&n,32))return 0;
 if(n>INT64_MAX/8||!table_bit_has(&r->bits,(int64_t)n*8))return 0;
 if(framed&&!table_retain_leb(&s->output,n))return 0;
 if(n>(uint64_t)(s->output.capacity-s->output.used))return 0;
 for(i=0;i<n;i++){uint64_t v;uint8_t byte;if(!table_bit_get(&r->bits,&v,8))return 0;byte=(uint8_t)v;if(!table_retain_raw(&s->output,&byte,1))return 0;}return 1;
}
static SCHEMA_UNUSED int table_retain_message_content(TableRetainMessage * s,const TableMessageEntry * e,int32_t depth)
{
 if(depth>SCHEMA_TABLE_RETAIN_WALK_MAX)return 0;
 switch(e->kind){
 case 13:for(;;){const TableMessageEntry * f;if(!table_message_ref(&s->input,&f,0))return 0;if(!f)return table_retain_id(&s->output,0);if(!f->id||!table_retain_id(&s->output,f->id)||!table_retain_raw(&s->output,&f->kind,1)||!table_retain_message_payload(s,f,depth))return 0;}
 case 14:case 16:{uint64_t n,i;TableMessageEntry elem=table_message_element(e);if(elem.kind==17||!table_message_count(&s->input,e,&n)||!table_retain_raw(&s->output,&elem.kind,1)||!table_retain_leb(&s->output,n))return 0;
 for(i=0;i<n;i++){if(e->kind==16){const TableMessageEntry * key;if(!table_message_ref(&s->input,&key,1)||!key||!key->id||!table_retain_id(&s->output,key->id)||!table_retain_message_frame(s,&elem,depth+1))return 0;}else if(!table_retain_message_payload(s,&elem,depth))return 0;}return 1;}
 case 15:case 30:return table_retain_message_payload(s,e,depth);
 case 32:return 1;
 case 12:case 31:case 33:return table_retain_message_opaque(s,e,0);
 case 0:case 17:return 0;
 default:return table_retain_message_scalar(s,e);
 }
}
static SCHEMA_UNUSED int table_retain_message_payload(TableRetainMessage * s,const TableMessageEntry * e,int32_t depth)
{
 switch(e->kind){
 case 0:case 17:return 0;
 case 32:return table_retain_leb(&s->output,0);
 case 30:{const TableMessageEntry * entry;if(!table_message_ref(&s->input,&entry,1))return 0;return table_retain_id(&s->output,entry?entry->id:0);}
 case 15:{const TableMessageEntry * arm;if(!table_message_ref(&s->input,&arm,0))return 0;if(!arm)return table_retain_id(&s->output,0);return arm->id&&arm->kind&&table_retain_id(&s->output,arm->id)&&table_retain_raw(&s->output,&arm->kind,1)&&table_retain_message_frame(s,arm,depth+1);}
 case 13:case 14:case 16:return table_retain_message_frame(s,e,depth+1);
 case 12:case 31:case 33:return table_retain_message_opaque(s,e,1);
 default:return table_retain_message_scalar(s,e);
 }
}
static SCHEMA_UNUSED int table_retain_message_capture(TableMessageReader * r,const TableMessageEntry * entry,TableRetainWalk keep)
{
 TableRetainMessage s;uint8_t * record;s.input=*r;if(!table_message_skip(r,entry))return 0;if(!keep.retain)return 1;
 record=table_retain_begin(keep,entry->id,entry->kind,&s.output);
 if(!record||!table_retain_message_payload(&s,entry,0)||s.input.bits.offset!=r->bits.offset){r->report->retain_lost++;return 1;}
 table_retain_commit(keep,record,&s.output,r->report);return 1;
}
`
