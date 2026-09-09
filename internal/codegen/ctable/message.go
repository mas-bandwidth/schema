package ctable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func tableMessageForm(u *ir.Unit, variable, hasProbes bool) string {
	var b strings.Builder
	b.WriteString("#ifndef SCHEMA_" + strings.ToUpper(u.Package) + "_TABLE_MESSAGE\n#define SCHEMA_" + strings.ToUpper(u.Package) + "_TABLE_MESSAGE\n")
	entries, announcement := ir.TableVocabulary(u), ir.TableAnnouncement(u)
	fmt.Fprintf(&b, "enum { kTableMessageRefBitsHere=%d, kTableMessageEntriesHere=%d, kTableAnnounceBytes=%d, kTableMessageBatchMax=256 };\n", ir.TableMessageRefBits(len(entries)), len(entries), len(announcement))
	b.WriteString("static const uint8_t kTableAnnounce[kTableAnnounceBytes]={")
	for _, v := range announcement {
		fmt.Fprintf(&b, "0x%02x,", v)
	}
	b.WriteString("};\n")
	nodes := ""
	if variable {
		nodes = "TableNumbering * nodes; int64_t index_bits;"
	}
	checkDefault := ""
	if hasProbes {
		checkDefault = ",check_default"
	}
	bits := strings.ReplaceAll(cMessageBits, "@CHECK_DEFAULT@", checkDefault)
	b.WriteString(strings.ReplaceAll(bits, "@NODES@", nodes))
	b.WriteString(cMessageNumbers)
	b.WriteString(cMessageShape)
	b.WriteString(cMessageAnnounce)
	readNodes := ""
	if variable {
		readNodes = "TableNodeMap * nodes;"
	}
	b.WriteString(strings.ReplaceAll(cMessageRead, "@NODES@", readNodes))
	b.WriteString(cMessageNodes)
	b.WriteString("\n#endif\n")
	return b.String()
}

// A null output performs exactly the same walk as a save, counting bits.
// Reads and writes touch only the bytes occupied by the requested value.
const cMessageBits = `
typedef struct TableBitWriter { uint8_t * buffer; int64_t capacity,bits; int overflow@CHECK_DEFAULT@; @NODES@ } TableBitWriter;
typedef struct TableBitReader { const uint8_t * buffer; int64_t bits,offset; } TableBitReader;
static SCHEMA_UNUSED TableBitWriter table_bit_writer(uint8_t * buffer,int64_t capacity)
{ TableBitWriter w;memset(&w,0,sizeof(w));w.buffer=buffer;w.capacity=capacity;return w; }
static SCHEMA_UNUSED TableBitReader table_bit_reader(const uint8_t * buffer,int64_t bytes)
{ TableBitReader r={buffer,bytes>=0 && bytes<=INT64_MAX/8 ? bytes*8 : 0,0}; return r; }
static SCHEMA_UNUSED int table_bit_has(const TableBitReader * r,int64_t n)
{ return n>=0 && r->offset>=0 && r->offset<=r->bits && n<=r->bits-r->offset; }
static SCHEMA_UNUSED void table_bit_put(TableBitWriter * w,uint64_t value,int64_t n)
{
 int64_t index,bit,need,i; uint64_t word,head;
 if(n<0 || n>64 || w->bits>INT64_MAX-n-7) { w->overflow=1;return; }
 if(n==0) return;
 if(w->capacity<0 || (w->bits+n+7)/8>w->capacity) {w->overflow=1;w->bits+=n;return;}
 if(w->buffer==NULL) {w->bits+=n;return;}
 if(n<64) value&=(UINT64_C(1)<<n)-1;
 index=w->bits>>3;bit=w->bits&7;need=(bit+n+7)>>3;
 head=bit ? (uint64_t)w->buffer[index]&((UINT64_C(1)<<bit)-1) : 0;
 word=head|(value<<bit);
 for(i=0;i<need && i<8;i++) w->buffer[index+i]=(uint8_t)(word>>(8*i));
 if(need>8) w->buffer[index+8]=(uint8_t)(value>>(64-bit));
 w->bits+=n;
}
static SCHEMA_UNUSED int table_bit_get(TableBitReader * r,uint64_t * value,int64_t n)
{
 int64_t got=0;
 if(n>64 || !table_bit_has(r,n)) return 0;
 *value=0;
 while(got<n) {
  int64_t bit=r->offset&7,take=8-bit;
  if(take>n-got) take=n-got;
  *value|=(((uint64_t)r->buffer[r->offset>>3]>>bit)&((UINT64_C(1)<<take)-1))<<got;
  r->offset+=take;got+=take;
 }
 return 1;
}
static SCHEMA_UNUSED int table_bit_skip(TableBitReader * r,int64_t n)
{ if(!table_bit_has(r,n))return 0;r->offset+=n;return 1; }
static SCHEMA_UNUSED void table_bit_align_write(TableBitWriter * w)
{table_bit_put(w,0,(8-(w->bits&7))&7);}
static SCHEMA_UNUSED int table_bit_align_read(TableBitReader * r)
{uint64_t pad;return table_bit_get(r,&pad,(8-(r->offset&7))&7) && pad==0;}
static SCHEMA_UNUSED void table_bit_putbytes(TableBitWriter * w,const void * source,int64_t n)
{
 int64_t i;
 if(n<0 || n>(INT64_MAX-w->bits)/8) {w->overflow=1;return;}
 if(w->buffer==NULL || (w->bits&7)==0) {
  if(w->bits/8+n>w->capacity)w->overflow=1;
  else if(w->buffer!=NULL && n>0)memcpy(w->buffer+w->bits/8,source,(size_t)n);
  w->bits+=n*8;return;
 }
 for(i=0;i<n;i++)table_bit_put(w,((const uint8_t *)source)[i],8);
}
static SCHEMA_UNUSED int table_bit_getbytes(TableBitReader * r,uint8_t * target,int64_t n)
{
 int64_t i;uint64_t by;
 if(n<0 || n>INT64_MAX/8 || !table_bit_has(r,n*8))return 0;
 if((r->offset&7)==0){if(n>0)memcpy(target,r->buffer+r->offset/8,(size_t)n);r->offset+=n*8;return 1;}
 for(i=0;i<n;i++){if(!table_bit_get(r,&by,8))return 0;target[i]=(uint8_t)by;}return 1;
}
static SCHEMA_UNUSED int64_t announce_measure(void) {return kTableAnnounceBytes;}
static SCHEMA_UNUSED int64_t announce(uint8_t * buffer,int64_t capacity)
{if(buffer==NULL || capacity<kTableAnnounceBytes)return -1;memcpy(buffer,kTableAnnounce,kTableAnnounceBytes);return kTableAnnounceBytes;}
`

const cMessageShape = `
typedef struct TableMessageEntry {
 uint64_t id;
 int64_t min,max,base_lo,base_hi,elem_max,elem_base_lo,elem_base_hi;
 float qmin,qdelta;uint32_t qcount;float elem_qmin,elem_qdelta;uint32_t elem_qcount;
 int16_t value_bits,elem_value_bits;
 uint8_t kind,packing,elem_kind,elem_packing;
} TableMessageEntry;
typedef struct TableMessageShapeFacts {
 uint8_t packing,elem_kind; int64_t value_bits,base_lo,base_hi,min,max;
 float qmin,qmax,qres,qdelta;uint32_t qcount;
} TableMessageShapeFacts;
typedef struct TableVocabulary {
 TableMessageEntry * entries;int64_t max_entries,count,ref_bits;
 uint64_t build_version;int announced,refused;int64_t max_bytes;
} TableVocabulary;
enum { SCHEMA_TABLE_NO_VOCABULARY=3,SCHEMA_TABLE_SECOND_ANNOUNCEMENT=4,SCHEMA_TABLE_VOCABULARY_TOO_LARGE=5,SCHEMA_TABLE_BATCH_TOO_LARGE=6 };
static SCHEMA_UNUSED TableVocabulary table_vocabulary(TableMessageEntry * storage,int64_t capacity)
{TableVocabulary v;memset(&v,0,sizeof(v));v.entries=storage;v.max_entries=capacity;v.max_bytes=65536;return v;}
static SCHEMA_UNUSED int table_message_leb(const uint8_t * in,int64_t size,int64_t * at,uint64_t * value)
{
 TableReader r=table_reader_make(in,size,NULL);r.offset=*at;
 if(!table_reader_leb(&r,value))return 0;*at=r.offset;return 1;
}
static SCHEMA_UNUSED int64_t table_message_value_bits(uint8_t kind,uint8_t packing,int64_t bits)
{
 if(kind==1)return 1;if(kind==11)return 64;if(kind==10)return packing==2?bits:32;
 if(table_message_integer_kind(kind)||table_message_fixed_kind(kind))return packing==1?bits:table_message_kind_bits(kind);
 return -1;
}
static SCHEMA_UNUSED int table_message_shape_read(const uint8_t * in,int64_t size,int64_t * at,uint8_t kind,TableMessageShapeFacts * f)
{
 uint64_t v=0;int i,k;
 if(table_message_integer_kind(kind)||table_message_fixed_kind(kind)||kind==10){
  if(*at>=size)return 0;f->packing=in[(*at)++];if(f->packing==0)return 1;
  if(f->packing==1 && kind!=10){
   if(!table_message_leb(in,size,at,&v)||v>(uint64_t)table_message_kind_bits(kind))return 0;
   f->value_bits=(int64_t)v;
   if(kind==18||kind==19||table_message_fixed_kind(kind)){
    uint64_t lo=0,hi=0;if(*at>size-16)return 0;
    for(i=0;i<8;i++){lo|=(uint64_t)in[*at+i]<<(8*i);hi|=(uint64_t)in[*at+8+i]<<(8*i);}
    f->base_lo=(int64_t)lo;f->base_hi=(int64_t)hi;*at+=16;return 1;
   }
   if(!table_message_leb(in,size,at,&v))return 0;
   f->base_lo=kind>=2&&kind<=5?(int64_t)(v>>1)^-(int64_t)(v&1):(int64_t)v;return 1;
  }
  if(f->packing==2 && kind==10){
   uint32_t raw[3]={0,0,0};if(*at>size-12)return 0;
   for(k=0;k<3;k++)for(i=0;i<4;i++)raw[k]|=(uint32_t)in[*at+4*k+i]<<(8*i);
   *at+=12;memcpy(&f->qmin,raw,4);memcpy(&f->qmax,raw+1,4);memcpy(&f->qres,raw+2,4);
   return table_message_quantization(f->qmin,f->qmax,f->qres,&f->qdelta,&f->qcount,&f->value_bits);
  }return 0;
 }
 if(kind==12||kind==33){if(!table_message_leb(in,size,at,&v)||v>INT32_MAX)return 0;f->max=(int64_t)v;return 1;}
 if(kind==14||kind==16){
  if(kind==14){if(!table_message_leb(in,size,at,&v)||v>UINT32_MAX)return 0;f->min=(int64_t)v;}
  if(!table_message_leb(in,size,at,&v)||v>UINT32_MAX||v<(uint64_t)f->min)return 0;f->max=(int64_t)v;
  if(*at>=size)return 0;f->elem_kind=in[(*at)++];
  return table_message_known_kind(f->elem_kind)&&f->elem_kind!=12&&f->elem_kind!=33;
 }return 1;
}
static SCHEMA_UNUSED int table_message_entry_read(const uint8_t * in,int64_t size,int64_t * at,TableMessageEntry * e)
{
 TableMessageShapeFacts own={0},elem={0};int i;
 if(*at<0||*at>size-9)return 0;memset(e,0,sizeof(*e));e->value_bits=e->elem_value_bits=-1;
 for(i=0;i<8;i++)e->id|=(uint64_t)in[*at+i]<<(8*i);e->kind=in[*at+8];*at+=9;
 if(!table_message_known_kind(e->kind)||!table_message_shape_read(in,size,at,e->kind,&own))return 0;
 e->packing=own.packing;e->value_bits=(int16_t)table_message_value_bits(e->kind,own.packing,own.value_bits);
 e->min=own.min;e->max=own.max;e->base_lo=own.base_lo;e->base_hi=own.base_hi;e->qmin=own.qmin;e->qdelta=own.qdelta;e->qcount=own.qcount;e->elem_kind=own.elem_kind;
 if(e->kind==14||e->kind==16){
  if(!table_message_shape_read(in,size,at,e->elem_kind,&elem))return 0;
  e->elem_packing=elem.packing;e->elem_value_bits=(int16_t)table_message_value_bits(e->elem_kind,elem.packing,elem.value_bits);
  e->elem_max=elem.max;e->elem_base_lo=elem.base_lo;e->elem_base_hi=elem.base_hi;e->elem_qmin=elem.qmin;e->elem_qdelta=elem.qdelta;e->elem_qcount=elem.qcount;
 }return 1;
}

`

const cMessageAnnounce = `
static SCHEMA_UNUSED int table_announce_once(TableVocabulary * v,const uint8_t * buffer,int64_t bytes,TableReport * report)
{
 TableReader r;int seen_version=0,seen_words=0;const uint8_t * words=NULL;int64_t words_bytes=0,at=0,count=0,nodes=0,touched=0;
 if(!table_wire_open(&r,buffer,bytes,report))return 0;
 for(;;){
  uint64_t ref,id;uint8_t kind;
  if(!table_reader_leb(&r,&ref))goto malformed;
  if(ref==0)break;
  if(ref>r.id_count||!table_reader_has(&r,1))goto malformed;
  id=table_reader_id_at(&r,ref);kind=table_reader_get8(&r);
  if(id==UINT64_C(0xfffffffffffffffe)){
   if(kind!=9||!table_reader_has(&r,8))goto malformed;
   v->build_version=table_reader_get64(&r);seen_version++;continue;
  }
  if(id==UINT64_C(0xfffffffffffffffd)){
   TableReader field;uint64_t n;
   if(kind!=14||!table_reader_span(&r,&field)||!table_reader_has(&field,1)||table_reader_get8(&field)!=6)goto malformed;
   if(!table_reader_leb(&field,&n)||!table_reader_room(&field,n)||n!=(uint64_t)(field.size-field.offset))goto malformed;
   if(v->max_bytes<0 || n>(uint64_t)v->max_bytes){report->refused=1;report->reason=SCHEMA_TABLE_VOCABULARY_TOO_LARGE;return 0;}
   words=field.buffer+field.offset;words_bytes=(int64_t)n;seen_words++;continue;
  }
  report->unknown++;if(!table_reader_skip(&r,kind))goto malformed;
 }
 if(seen_version!=1||seen_words!=1||r.offset!=r.size)goto malformed;
 while(at<words_bytes){
  TableMessageEntry * entry;int64_t i,began=at;
  if(v->entries==NULL){report->refused=1;report->reason=SCHEMA_TABLE_NO_VOCABULARY;goto refused;}
  if(count>=v->max_entries){report->refused=1;report->reason=SCHEMA_TABLE_VOCABULARY_TOO_LARGE;goto refused;}
  entry=&v->entries[count];touched=count+1;
  if(!table_message_entry_read(words,words_bytes,&at,entry))goto malformed;
  if(entry->id==UINT64_C(0xfffffffffffffffe)||entry->id==UINT64_C(0xfffffffffffffffd))goto malformed;
  if(entry->id==UINT64_MAX && nodes++>0)goto malformed;
  /* Declared canonical bytes identify a shape. Resolved quantization facts
     alone can collapse different triples (#722). Until validation finishes,
     min/max temporarily hold the source span; no announcement pointer escapes. */
  for(i=0;i<count;i++){int64_t start=v->entries[i].min,length=v->entries[i].max-start;
   if(length==at-began && !memcmp(words+start,words+began,(size_t)length))goto malformed;}
  entry->min=began;entry->max=at;
  count++;
 }
 /* Resolve the accepted spans into the caller's final 96-byte entries. */
 at=0;{int64_t i;for(i=0;i<count;i++)if(!table_message_entry_read(words,words_bytes,&at,v->entries+i))goto malformed;}
 v->count=count;v->ref_bits=table_bits_required(0,count);v->announced=1;return 1;
malformed:
 report->malformed=1;
refused:
 if(touched)memset(v->entries,0,(size_t)touched*sizeof(*v->entries));return 0;
}
static SCHEMA_UNUSED int announce_read(TableVocabulary * v,const uint8_t * buffer,int64_t bytes,TableReport * report)
{
 TableReport ignored={0};int ok;
 if(report==NULL)report=&ignored;
 if(v->announced||v->refused){report->refused=1;report->reason=SCHEMA_TABLE_SECOND_ANNOUNCEMENT;return 0;}
 ok=table_announce_once(v,buffer,bytes,report);if(!ok)v->refused=1;return ok;
}
`

const cMessageNumbers = `static SCHEMA_UNUSED int64_t table_bits_required( int64_t min, int64_t max )
{
    if ( max <= min ) { return 0; }
    uint64_t span = (uint64_t) max - (uint64_t) min;
    int64_t n = 0;
    while ( span > 0 ) { n++; span >>= 1; }
    return n;
}

// table_message_kind_bits is the widest RANGED value a kind can carry, its own
// storage width: a width above it is a hostile width on the announcement.
static SCHEMA_UNUSED int64_t table_message_kind_bits( uint8_t kind )
{
    switch ( kind )
    {
        case 2: case 6: case 20: case 25: return 8;
        case 3: case 7: case 21: case 26: return 16;
        case 4: case 8: case 22: case 27: return 32;
        case 5: case 9: case 23: case 28: return 64;
        default: return 128;
    }
}

// table_message_quantization is SPEC.md §4.3's derivation over an announced
// triple, in float32 and by nothing else: delta, the step count and the
// width. False is a triple SPEC.md calls non-conforming, which on the
// announcement is a hostile width like any other (§3.3).
static SCHEMA_UNUSED int table_message_quantization( float qmin, float qmax, float qres, float * delta, uint32_t * count, int64_t * bits )
{
    if ( !( qmin < qmax ) || !( qres > 0.0f ) ) { return 0; }
    (*delta) = qmax - qmin;
    float values = (*delta) / qres;
    if ( !( (*delta) - (*delta) == 0.0f ) || !( values - values == 0.0f ) ) { return 0; } // Inf - Inf is NaN
    if ( !( values >= 1.0f ) ) { values = 1.0f; }
    else if ( values > 4294967040.0f ) { values = 4294967040.0f; } // the largest float below 2^32
    (*count) = (uint32_t) values;
    if ( (float) (*count) < values ) { (*count)++; } // ceil, on a value the cast holds exactly
    (*bits) = table_bits_required( 0, (int64_t) (*count) );
    return 1;
}

// The two roundings on each side of the rule (SPEC.md §7.2): the product
// rounds to float32 BEFORE the add, which a compiler permitted to contract
// would otherwise fuse into one rounding and move the wire.
#if ( defined( __GNUC__ ) || defined( __clang__ ) ) && ( defined( __aarch64__ ) || defined( _M_ARM64 ) )
#define SCHEMA_TABLE_FLOAT_FORCE_ROUND( x ) __asm__ ( "" : "+w" ( x ) )
#elif ( defined( __GNUC__ ) || defined( __clang__ ) ) && ( defined( __x86_64__ ) || defined( __i386__ ) )
#define SCHEMA_TABLE_FLOAT_FORCE_ROUND( x ) __asm__ ( "" : "+x" ( x ) )
#else
#define SCHEMA_TABLE_FLOAT_FORCE_ROUND( x ) do { volatile float schema_table_float_force_round_slot_ = ( x ); ( x ) = schema_table_float_force_round_slot_; } while ( 0 )
#endif

// table_message_quantize is the writer's half: the index a value takes.
static SCHEMA_UNUSED uint32_t table_message_quantize( float value, float qmin, float delta, uint32_t count )
{
    float normalized = ( value - qmin ) / delta;
    if ( !( normalized >= 0.0f ) ) { normalized = 0.0f; }
    else if ( !( normalized <= 1.0f ) ) { normalized = 1.0f; }
    float scaled = normalized * (float) count;
    SCHEMA_TABLE_FLOAT_FORCE_ROUND( scaled );
    uint32_t index = (uint32_t) ( scaled + 0.5f ); // floor of a non-negative value
    if ( index > count ) { index = count; }
    return index;
}

// table_message_dequantize is the reader's half: the float an index names.
static SCHEMA_UNUSED float table_message_dequantize( uint32_t index, float qmin, float delta, uint32_t count )
{
    if ( index > count ) { index = count; }
    const float normalized = index / (float) count;
    float scaled = normalized * delta;
    SCHEMA_TABLE_FLOAT_FORCE_ROUND( scaled );
    return scaled + qmin;
}

static SCHEMA_UNUSED int table_message_integer_kind( uint8_t kind )
{
    return ( kind >= 2 && kind <= 9 ) || kind == 18 || kind == 19;
}

static SCHEMA_UNUSED int table_message_fixed_kind( uint8_t kind ) { return kind >= 20 && kind <= 29; }

static SCHEMA_UNUSED int table_message_known_kind( uint8_t kind )
{
    return kind == 0 || ( kind >= 1 && kind <= 17 ) || ( kind >= 18 && kind <= 29 ) || ( kind >= 30 && kind <= 33 );
}

`
