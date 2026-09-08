package ctable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const cMessageRead = `
typedef struct TableMessageReader {
 TableBitReader bits; const TableVocabulary * vocabulary; TableReport * report;
 int64_t index_bits; int extent_refused;@NODES@
} TableMessageReader;
static SCHEMA_UNUSED int table_message_ref(TableMessageReader * r,const TableMessageEntry ** entry,int name)
{
 uint64_t ref=0;*entry=NULL;
 if(!table_bit_get(&r->bits,&ref,r->vocabulary->ref_bits))return 0;
 if(ref==0)return 1;
 if(ref>(uint64_t)r->vocabulary->count)return 0;
 *entry=r->vocabulary->entries+ref-1;
 return (*entry)->id<UINT64_C(0xfffffffffffffffd) && (!name || (*entry)->kind==0);
}
static SCHEMA_UNUSED TableMessageEntry table_message_element(const TableMessageEntry * e)
{
 TableMessageEntry v;memset(&v,0,sizeof(v));v.kind=e->elem_kind;
 v.packing=e->elem_packing;v.value_bits=e->elem_value_bits;v.base_lo=e->elem_base_lo;v.base_hi=e->elem_base_hi;
 v.max=e->elem_max;v.qmin=e->elem_qmin;v.qdelta=e->elem_qdelta;v.qcount=e->elem_qcount;return v;
}
static SCHEMA_UNUSED int table_message_skip_run(TableMessageReader * r,uint64_t count,int64_t width)
{
 if(width<0)return 0;
 if(width==0)return 1;
 if(count>(uint64_t)(INT64_MAX/width))return 0;
 return table_bit_skip(&r->bits,(int64_t)(count*(uint64_t)width));
}
static SCHEMA_UNUSED int table_message_count(TableMessageReader * r,const TableMessageEntry * e,uint64_t * n)
{
 uint64_t raw;
 if(!table_bit_get(&r->bits,&raw,table_bits_required(e->min,e->max)))return 0;
 *n=raw+(uint64_t)e->min;
 return !(e->kind==14 && e->elem_kind==6) || table_bit_align_read(&r->bits);
}
static SCHEMA_UNUSED int table_message_skip(TableMessageReader *,const TableMessageEntry *);
static SCHEMA_UNUSED int table_message_skip_body(TableMessageReader * r)
{
 for(;;) {const TableMessageEntry * entry;
  if(!table_message_ref(r,&entry,0))return 0;
  if(entry==NULL)return 1;
  if(!table_message_skip(r,entry))return 0;
 }
}
static SCHEMA_UNUSED int table_message_skip(TableMessageReader * r,const TableMessageEntry * e)
{
 uint64_t n,i;const TableMessageEntry * entry;
 switch(e->kind) {
 case 0:case 32:return 1;
 case 30:return table_message_ref(r,&entry,1);
 case 13:return table_message_skip_body(r);
 case 17:return r->index_bits>0 && table_bit_skip(&r->bits,r->index_bits);
 case 15:
  if(!table_message_ref(r,&entry,0))return 0;
  return entry==NULL || (entry->kind!=0 && table_message_skip(r,entry));
 case 12:case 33:
  if(!table_bit_get(&r->bits,&n,table_bits_required(0,e->max)))return 0;
  if(e->kind==12 && !table_bit_align_read(&r->bits))return 0;
  return table_message_skip_run(r,n,e->kind==12?8:16);
 case 31:
  if(!table_bit_align_read(&r->bits)||!table_bit_get(&r->bits,&n,32))return 0;
  return table_message_skip_run(r,n,8);
 case 14:case 16: {
  TableMessageEntry elem=table_message_element(e);
  if(!table_message_count(r,e,&n))return 0;
  if(e->kind==14 && (elem.kind==0||elem.kind==32))return 1;
  if(e->kind==14 && elem.value_bits>=0)return table_message_skip_run(r,n,elem.value_bits);
  for(i=0;i<n;i++) {
   if(e->kind==16 && !table_message_ref(r,&entry,1))return 0;
   if(!table_message_skip(r,&elem))return 0;
  }return 1;
 }
 default:return e->value_bits>=0 && table_bit_skip(&r->bits,e->value_bits);
 }
}
static SCHEMA_UNUSED int table_message_number(TableMessageReader * r,const TableMessageEntry * e,uint64_t * lo,uint64_t * hi)
{
 int64_t width=e->value_bits;uint64_t before;
 *lo=*hi=0;
 if(width<0 || width>128 || !table_bit_get(&r->bits,lo,width<64?width:64))return 0;
 if(width>64 && !table_bit_get(&r->bits,hi,width-64))return 0;
 if(e->packing==1) {
  before=*lo;*lo+=(uint64_t)e->base_lo;
  if(table_message_kind_bits(e->kind)>64)*hi+=(uint64_t)e->base_hi+(*lo<before);
 } else if(((e->kind>=2 && e->kind<=5)||(e->kind>=20 && e->kind<=23)) && width>0 && width<64 && (*lo&(UINT64_C(1)<<(width-1)))) {
  *lo|=UINT64_MAX<<width;
 }
 if((e->kind>=2 && e->kind<=5)||(e->kind>=20 && e->kind<=23))*hi=(int64_t)*lo<0?UINT64_MAX:0;
 return 1;
}
static SCHEMA_UNUSED int table_message_float(TableMessageReader * r,const TableMessageEntry * e,float * value)
{
 uint64_t raw;
 if(!table_bit_get(&r->bits,&raw,e->value_bits))return 0;
 if(e->packing==2) {
  if(raw>e->qcount)return 0;
  *value=table_message_dequantize((uint32_t)raw,e->qmin,e->qdelta,e->qcount);
 }else *value=table_bits_to_float((uint32_t)raw);
 return 1;
}
static SCHEMA_UNUSED int64_t table_message_open(TableMessageReader * r,const TableVocabulary * v,const uint8_t * buffer,int64_t bytes,TableReport * report)
{
 uint64_t count;
 memset(r,0,sizeof(*r));r->report=report;r->vocabulary=v;
 if(bytes<1 || buffer==NULL){report->malformed=1;return -1;}
 if(buffer[0]!=2){report->refused=1;report->reason=SCHEMA_TABLE_NEWER_FORM;return -1;}
 if(v==NULL || !v->announced){report->refused=1;report->reason=SCHEMA_TABLE_NO_VOCABULARY;return -1;}
 r->bits=table_bit_reader(buffer+1,bytes-1);
 if(!table_bit_get(&r->bits,&count,8)){report->malformed=1;return -1;}
 return (int64_t)count+1;
}
static SCHEMA_UNUSED int table_message_close(TableMessageReader * r)
{
 if(!table_bit_align_read(&r->bits)||r->bits.offset!=r->bits.bits){r->report->malformed=1;return 0;}return 1;
}
`

func (g *tableGen) messageReadEnum(e *ir.Enum, dst, ind string) {
	g.pf("%s{ const TableMessageEntry * named; if(!table_message_ref(r,&named,1)) goto malformed;\n%s if(named==NULL) %s=%s; else switch(named->id) {\n", ind, ind, dst, enumNoneConst(e.Name))
	for i, v := range e.Variants {
		g.pf("%s case UINT64_C(0x%016x): %s=%s;break;\n", ind, ir.TableWireId(e.VariantWireName(i)), dst, enumConst(e.Name, v))
	}
	g.pf("%s default:%s=%s;r->report->unknown++;break;\n%s } }\n", ind, dst, enumNoneConst(e.Name), ind)
}

func (g *tableGen) messageReadScalar(f *ir.Field, dst, e, ind string) {
	if en := enumRef(f); en != nil {
		g.messageReadEnum(en, dst, ind)
		return
	}
	kind := ir.TableWireScalarKind(f)
	if kind == tkF32 || kind == tkF64 {
		typ := "float"
		if kind == tkF64 {
			typ = "double"
		}
		g.pf("%s{ %s decoded; if(%s->kind==10) {float narrow;if(!table_message_float(r,%s,&narrow))goto malformed;", ind, typ, e, e)
		if kind == tkF64 {
			g.pf("decoded=table_wire_widen_f32(table_float_to_bits(narrow));")
		} else {
			g.pf("decoded=narrow;")
		}
		g.pf("} else {uint64_t raw;if(!table_bit_get(&r->bits,&raw,64))goto malformed;decoded=(%s)table_bits_to_double(raw);}\n", typ)
		if f.HasFloatRange {
			g.pf("%s if(decoded<%s){decoded=%s;r->report->clamped++;}else if(decoded>%s){decoded=%s;r->report->clamped++;}\n", ind, formatFloat(f.FMin, kind == tkF32), formatFloat(f.FMin, kind == tkF32), formatFloat(f.FMax, kind == tkF32), formatFloat(f.FMax, kind == tkF32))
		}
		g.pf("%s %s=decoded; }\n", ind, dst)
		return
	}
	g.pf("%s{ uint64_t lo,hi;if(!table_message_number(r,%s,&lo,&hi))goto malformed;\n", ind, e)
	if f.Type.Width == 128 {
		typ := g.cFieldType(f.Type)
		g.pf("%s %s decoded;decoded.lo=lo;decoded.hi=hi;\n", ind, typ)
		if low, high, ok := ir.TableRawRange(f); ok {
			for i, b := range []*big.Int{low, high} {
				op := "<"
				if i == 1 {
					op = ">"
				}
				cmp := fmt.Sprintf("decoded.hi%sbound.hi || (decoded.hi==bound.hi && decoded.lo%sbound.lo)", op, op)
				if f.Type.Signed {
					cmp = "serialize_int128_compare(decoded,bound)" + op + "0"
				}
				g.pf("%s { %s bound=%s;if(%s){decoded=bound;r->report->clamped++;} }\n", ind, typ, tableWideLit(b, f.Type.Signed), cmp)
			}
		}
		g.pf("%s %s=decoded; }\n", ind, dst)
		return
	}
	if kind == tkBool {
		g.pf("%s %s=lo!=0; }\n", ind, dst)
		return
	}
	signed := ir.TableKindSigned(kind)
	typ := "uint64_t"
	if signed {
		typ = "int64_t"
	}
	g.pf("%s %s decoded=(%s)lo;(void)hi;\n", ind, typ, typ)
	bits := tableKindWidth(kind) * 8
	low, high := messageRange(signed, bits)
	if a, b, ok := ir.TableRawRange(f); ok {
		if a.Cmp(low) > 0 {
			low = a
		}
		if b.Cmp(high) < 0 {
			high = b
		}
	}
	if f.Type.Kind == ir.TBits {
		high = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
	}
	floor, ceiling := messageRange(signed, 64)
	if low.Cmp(floor) > 0 {
		s := tableIntLit(low, signed, 8)
		g.pf("%s if(decoded<%s){decoded=%s;r->report->clamped++;}\n", ind, s, s)
	}
	if high.Cmp(ceiling) < 0 {
		s := tableIntLit(high, signed, 8)
		g.pf("%s if(decoded>%s){decoded=%s;r->report->clamped++;}\n", ind, s, s)
	}
	g.pf("%s %s=(%s)decoded; }\n", ind, dst, g.cFieldType(f.Type))
}

func messageRange(signed bool, bits int) (*big.Int, *big.Int) {
	if signed {
		n := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
		return new(big.Int).Neg(n), new(big.Int).Sub(n, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
}

func (g *tableGen) messageReadElement(f *ir.Field, dst, e, ind string) {
	switch {
	case f.Type.Pointer:
		target := ir.TableWireId(ir.PointeeWireName(f))
		if f.Type.Blob() {
			target = ir.BlobWireTypeId(f)
		}
		g.pf("%s{uint64_t index;if(r->index_bits<1 || !table_bit_get(&r->bits,&index,r->index_bits))goto malformed;table_node_resolve(r->nodes,&%s,index,UINT64_C(0x%016x),r->report);}\n", ind, dst, target)
	case ir.TableWireScalarKind(f) == tkTable:
		g.pf("%sif(!%s(r,&%s))return 0;\n", ind, g.api(f.Type.Name, "load_message_body"), dst)
	case ir.TableWireScalarKind(f) == tkUnion:
		g.pf("%sif(!%s(r,&%s))return 0;\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "message_load"), dst)
	default:
		g.messageReadScalar(f, dst, e, ind)
	}
}

func (g *tableGen) messageReadValue(f *ir.Field, dst, count, e, ind string) {
	switch {
	case f.IsList() || f.IsMap():
		g.emitMessageSequenceRead(f, dst, e, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.messageReadElement(f, dst, e, ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		width := "table_bits_required(0," + e + "->max)"
		bits := 8
		if f.Type.Kind == ir.TBytes {
			width = "table_bits_required(" + e + "->min," + e + "->max)"
		}
		if f.Type.Kind == ir.TWString {
			bits = 16
		}
		g.pf("%s{uint64_t n,kept;if(!table_bit_get(&r->bits,&n,%s))goto malformed;\n", ind, width)
		if bits == 8 {
			g.pf("%s if(!table_bit_align_read(&r->bits))goto malformed;\n", ind)
		}
		g.pf("%s if(n>INT64_MAX/%d || !table_bit_has(&r->bits,(int64_t)n*%d))goto malformed;\n", ind, bits, bits)
		if f.Type.Kind == ir.TString {
			g.pf("%s {const uint8_t * text=r->bits.buffer+(r->bits.offset>>3);if(!table_wire_utf8(text,n))goto malformed;kept=table_wire_utf8_clamp(text,n,%d);memcpy(%s,text,(size_t)kept);%s[kept]=0;}\n", ind, f.Type.Size, dst, dst)
			g.pf("%s r->bits.offset+=(int64_t)n*8;\n", ind)
		} else {
			g.pf("%s kept=n>%d ? %d : n;\n", ind, f.Type.Size, f.Type.Size)
			if bits == 8 {
				g.pf("%s if(!table_bit_getbytes(&r->bits,%s,(int64_t)kept)||!table_message_skip_run(r,n-kept,8))goto malformed;\n", ind, dst)
			} else {
				g.pf("%s {uint64_t i,unit;int high=0,ill=0;for(i=0;i<n;i++){if(!table_bit_get(&r->bits,&unit,16))goto malformed;\n", ind)
				g.pf("%s if(unit==0)ill=1;if(high){if(unit<0xdc00||unit>0xdfff)ill=1;high=0;}else if(unit>=0xd800&&unit<=0xdbff)high=1;else if(unit>=0xdc00&&unit<=0xdfff)ill=1;\n", ind)
				g.pf("%s if(i<kept)%s[i]=(uint16_t)unit; } if(high||ill)goto malformed;\n", ind, dst)
				g.pf("%s if(kept>0 && kept<n && %s[kept-1]>=0xd800 && %s[kept-1]<=0xdbff)kept--; %s[kept]=0; }\n", ind, dst, dst, dst)
			}
		}
		g.pf("%s if(kept<n)r->report->clamped++;%s=(int32_t)kept; }\n", ind, count)
	case f.KeyEnum != "":
		g.pf("%s{uint64_t n,i;TableMessageEntry element_shape=table_message_element(%s);const TableMessageEntry * element_entry=&element_shape;(void)element_entry;\n%s if(!table_message_count(r,%s,&n))goto malformed;for(i=0;i<n;i++){const TableMessageEntry * key;int32_t slot=-1;\n%s if(!table_message_ref(r,&key,1)||key==NULL)goto malformed;switch(key->id){\n", ind, e, ind, e, ind)
		for i, v := range f.KeyEnumRef.Variants {
			g.pf("%s case UINT64_C(0x%016x):slot=%d;break;\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
			_ = v
		}
		g.pf("%s default:break;} if(slot<0){r->report->unknown++;if(!table_message_skip(r,element_entry))goto malformed;continue;}\n", ind)
		g.messageReadElement(f, dst+"[slot]", "element_entry", ind+" ")
		g.pf("%s} }\n", ind)
	case f.Array != ir.ArrayNone:
		g.pf("%s{uint64_t n,kept,walk,i;TableMessageEntry element_shape=table_message_element(%s);const TableMessageEntry * element_entry=&element_shape;(void)element_entry;\n", ind, e)
		g.pf("%s if(!table_message_count(r,%s,&n))goto malformed;kept=n>%d?%d:n;if(kept<n)r->report->clamped++;\n", ind, e, f.ArrayBound, f.ArrayBound)
		g.pf("%s walk=element_entry->value_bits>=0?kept:n;for(i=0;i<walk;i++){%s scratch; %s * item=i<kept ? &%s[i] : &scratch;\n", ind, g.sequenceType(f), g.sequenceType(f), dst)
		g.messageReadElement(f, "(*item)", "element_entry", ind+" ")
		g.pf("%s }if(walk<n && !table_message_skip_run(r,n-walk,element_entry->value_bits))goto malformed;\n", ind)
		if f.Array == ir.ArrayCounted {
			g.pf("%s %s=(int32_t)kept;\n", ind, count)
		}
		g.pf("%s}\n", ind)
	default:
		g.messageReadElement(f, dst, e, ind)
	}
}

// A sender's shape may change while its kind remains compatible. The decode
// always uses that shape, then applies this declaration's bounds.
func (g *tableGen) messageAccept(f *ir.Field, e ir.TableVocabularyEntry, ind string) {
	condition := fmt.Sprintf("entry->kind==%d && entry->elem_kind==%d", e.Kind, e.Shape.Elem)
	widen := "0"
	if f != nil && !f.Type.Pointer {
		if f.Array != ir.ArrayNone && f.KeyEnum == "" && !f.IsMap() && enumRef(f) == nil {
			widen = fmt.Sprintf("entry->kind==%d && table_kind_widens(entry->elem_kind,%d)", e.Kind, e.Shape.Elem)
		} else if f.Array == ir.ArrayNone && !f.IsMap() && enumRef(f) == nil {
			widen = fmt.Sprintf("entry->elem_kind==0 && table_kind_widens(entry->kind,%d)", e.Kind)
		}
	}
	g.pf("%sif(!(%s)){if(%s){r->report->widened++;}else{r->report->kind_mismatch++;if(!table_message_skip(r,entry))goto malformed;break;}}\n", ind, condition, widen)
}

func (g *tableGen) emitMessageReadDeclarations(members []*ir.Struct, unions []*ir.Union) {
	for _, st := range members {
		g.pf("static SCHEMA_UNUSED int %s(TableMessageReader *,%s *);\n", g.api(st.Name, "load_message_body"), st.Name)
	}
	for _, un := range unions {
		g.pf("static SCHEMA_UNUSED int %s(TableMessageReader *,%s *);\n", g.unionWireName(un, "message_load"), un.Name)
	}
}

func (g *tableGen) emitMessageUnionRead(un *ir.Union) {
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,%s * value)\n{\n const TableMessageEntry * entry;\n if(!table_message_ref(r,&entry,0))goto malformed;value->type=0;if(entry==NULL)return 1;if(entry->kind==0)goto malformed;\n switch(entry->id){\n", g.unionWireName(un, "message_load"), un.Name)
	for _, v := range un.Variants {
		g.pf(" case UINT64_C(0x%016x): {\n", ir.TableWireId(v.WireName()))
		g.messageAccept(v.F, ir.TableArmEntry(v), " ")
		if !v.Void() {
			dst := armValue("(*value)", v)
			count := dst + "_count"
			if v.F.Array == ir.ArrayNone {
				count = dst + "_length"
			}
			g.pf(" memset(&%s,0,sizeof(%s));\n", dst, dst)
			g.messageReadValue(v.F, dst, count, "entry", " ")
		}
		g.pf(" value->type=%s;break;}\n", enumConst(un.Name+"Type", v.Name))
	}
	g.pf(" default:r->report->unknown++;if(!table_message_skip(r,entry))goto malformed;break;\n }return 1;\n malformed:r->report->malformed=1;return 0;\n}\n")
}

func (g *tableGen) emitMessageRead(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,%s * value)\n{\n %s(value);for(;;){const TableMessageEntry * entry;\n if(!table_message_ref(r,&entry,0))goto malformed;if(entry==NULL)return 1;switch(entry->id){\n", g.api(st.Name, "load_message_body"), st.Name, g.api(st.Name, "reset"))
	for _, f := range st.Fields {
		g.pf(" case UINT64_C(0x%016x): {\n", ir.TableFieldWireId(f))
		g.messageAccept(f, ir.TableFieldEntry(f), " ")
		dst, count := "value->"+f.Name, "value->"+f.Name+"_count"
		if f.Array == ir.ArrayNone {
			count = "value->" + f.Name + "_length"
		}
		g.messageReadValue(f, dst, count, "entry", " ")
		if f.Type.Optional {
			g.pf(" %s_present=1;\n", dst)
		}
		g.pf(" break;}\n")
	}
	g.pf(" default:r->report->unknown++;if(!table_message_skip(r,entry))goto malformed;break;\n } }\n malformed:r->report->malformed=1;return 0;\n}\n")
	if !g.isVar(st.Name) && !st.IsMapEntry() {
		g.emitFixedMessageRead(st)
	}
}

func (g *tableGen) emitFixedMessageRead(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s(%s * values,int64_t * count,const TableVocabulary * vocabulary,const uint8_t * buffer,int64_t bytes,TableReport * report)\n{\n", g.api(st.Name, "load_messages"), st.Name)
	g.pf(" TableReport ignored={0};TableMessageReader r;int64_t capacity,bodies,i;if(report==NULL)report=&ignored;\n if(values==NULL||count==NULL){report->malformed=1;return 0;}capacity=*count;*count=0;\n bodies=table_message_open(&r,vocabulary,buffer,bytes,report);if(bodies<0)return 0;\n if(bodies>capacity){*count=bodies;report->refused=1;report->reason=SCHEMA_TABLE_BATCH_TOO_LARGE;return 0;}\n for(i=0;i<bodies;i++){if(!%s(&r,values+i))return 0;(*count)++;}return table_message_close(&r);\n}\n", g.api(st.Name, "load_message_body"))
}

func (g *tableGen) emitMessageMapKeyRead(f *ir.Field) {
	n, key := f.MapEntry.Name, ir.MapKeyField(f)
	readType := g.sym(n, "key_read")
	kind := ir.TableWireScalarKind(key)
	g.pf("static SCHEMA_UNUSED %s %s(TableMessageReader * r)\n{\n %s out;memset(&out,0,sizeof(out));for(;;){const TableMessageEntry * entry;\n if(!table_message_ref(r,&entry,0))goto malformed;if(entry==NULL)return out;\n if(entry->id==UINT64_C(0x%016x)){\n", readType, g.sym(n, "message_key"), readType, ir.MapKeyWireId)
	g.pf(" out.kind_bad=entry->kind!=%d && !table_kind_widens(entry->kind,%d);if(entry->kind!=%d && !out.kind_bad)out.widened=1;\n if(!out.kind_bad){\n", kind, kind, kind)
	if key.Type.Kind == ir.TString {
		g.pf(" uint64_t count;if(!table_bit_get(&r->bits,&count,table_bits_required(0,entry->max))||!table_bit_align_read(&r->bits)||!table_bit_has(&r->bits,(int64_t)count*8))goto malformed;\n out.key.data=(const char *)(r->bits.buffer+(r->bits.offset>>3));out.key.length=(int32_t)count;out.over=count>%d;\n if(!table_wire_utf8((const uint8_t *)out.key.data,count))goto malformed;r->bits.offset+=(int64_t)count*8;\n", key.Type.Size)
	} else {
		g.pf(" uint64_t low,high;if(!table_message_number(r,entry,&low,&high))goto malformed;(void)high;out.over=0;\n")
		bits := tableKindWidth(kind) * 8
		if bits < 64 {
			lo, hi := messageRange(ir.TableKindSigned(kind), bits)
			cast := "uint64_t"
			if ir.TableKindSigned(kind) {
				cast = "int64_t"
			}
			g.pf(" if((%s)low<%s || (%s)low>%s)out.over=1;\n", cast, tableIntLit(lo, ir.TableKindSigned(kind), 8), cast, tableIntLit(hi, ir.TableKindSigned(kind), 8))
		}
		g.pf(" out.key=(%s)low;\n", g.mapKeyType(f))
	}
	g.pf(" continue;} }\n if(!table_message_skip(r,entry))goto malformed;\n }malformed:out.malformed=1;return out;\n}\n")
}

func (g *tableGen) emitMessageSequenceRead(f *ir.Field, dst, e, ind string) {
	n := g.sequenceType(f)
	elem := sequenceElement(f)
	g.pf("%s{uint64_t count,i;TableSequenceFill fill;TableMessageEntry element_shape=table_message_element(%s);const TableMessageEntry * element_entry=&element_shape;(void)element_entry;\n", ind, e)
	g.pf("%s if(!table_message_count(r,%s,&count))goto malformed;fill=table_sequence_fill(r->nodes?r->nodes->sink:NULL,&%s,count,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s));\n", ind, e, dst, n, n)
	g.pf("%s if(fill.refused){if(r->nodes)r->nodes->refused=1;return 0;}if(!fill.ok)goto malformed;\n", ind)
	if f.IsMap() {
		g.pf("%s { %s previous; %s * last=NULL;int widened=0;memset(&previous,0,sizeof(previous));(void)element_entry;\n", ind, g.sym(n, "key_read"), n)
	}
	g.pf("%s for(i=0;i<count;i++){%s * item;\n", ind, n)
	if f.IsMap() {
		g.pf("%s TableMessageReader scan=*r;%s key=%s(&scan);int order;\n", ind, g.sym(n, "key_read"), g.sym(n, "message_key"))
		g.pf("%s if(key.widened&&!widened){widened=1;r->report->widened++;}\n", ind)
		g.pf("%s if(key.malformed)goto malformed;if(key.kind_bad){r->report->kind_mismatch++;r->bits=scan.bits;for(i++;i<count;i++)if(!table_message_skip_body(r))goto malformed;memset(&%s,0,sizeof(%s));break;}\n", ind, dst, dst)
		g.pf("%s if(key.over){r->report->clamped++;r->bits=scan.bits;continue;}order=last?%s(previous.key,key.key):-1;if(order>0)goto malformed;\n", ind, g.sym(n, "key_compare"))
		g.pf("%s if(order==0){item=last;r->report->duplicate++;}else item=(%s *)table_sequence_fill_next(&fill);if(item==NULL)goto malformed;\n", ind, n)
		g.messageReadElement(elem, "(*item)", "element_entry", ind+" ")
		g.pf("%s last=item;previous=key;\n", ind)
	} else {
		g.pf("%s item=(%s *)table_sequence_fill_next(&fill);if(item==NULL)goto malformed;\n", ind, n)
		g.messageReadElement(elem, "(*item)", "element_entry", ind+" ")
	}
	g.pf("%s }\n", ind)
	if f.IsMap() {
		g.pf("%s }\n", ind)
	}
	g.pf("%s table_sequence_fill_end(&fill); }\n", ind)
}
