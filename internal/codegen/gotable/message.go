package gotable

// Message form is a separate bit codec. Its framing comes from the peer's
// announcement; it shares the file codec's value clamps, never its headers.
import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func messageShapeLiteral(kind uint8, s ir.TableMessageShape) string {
	lo, hi := uint64(0), uint64(0)
	if s.Base != nil {
		v := new(big.Int).Set(s.Base)
		v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
		lo = v.Uint64()
		hi = new(big.Int).Rsh(v, 64).Uint64()
	}
	if kind < 18 {
		hi = 0
	}
	count, delta, _ := ir.TableMessageQuantization(s)
	return fmt.Sprintf("TableMessageShape{Kind:%d,Packing:%d,Bits:%d,Base:[2]uint64{0x%x,0x%x},Min:%d,Max:%d,QMin:math.Float32frombits(0x%x),QDelta:math.Float32frombits(0x%x),QCount:%d}", kind, s.Packing, ir.TableMessageValueBits(kind, s), lo, hi, s.Min, s.Max, math.Float32bits(s.QMin), math.Float32bits(delta), count)
}
func messageEntryLiteral(e ir.TableVocabularyEntry) string {
	inner := "TableMessageShape{}"
	if e.Shape.Inner != nil {
		inner = messageShapeLiteral(e.Shape.Elem, *e.Shape.Inner)
	}
	return fmt.Sprintf("TableMessageEntry{Id:0x%016x,Shape:%s,Element:%s}", e.Id, messageShapeLiteral(e.Kind, e.Shape), inner)
}
func tableMessageRuntime(u *ir.Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "const TableMessageBatchMax = 256\nconst TableMessageRefBitsHere = %d\nconst TableMessageEntriesHere = %d\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))), len(ir.TableVocabulary(u)))
	fmt.Fprintf(&b, "var tableAnnouncement = [...]byte{%s}\n", byteLiterals(ir.TableAnnouncement(u)))
	b.WriteString("var tableMessageEntries = [...]TableMessageEntry{\n")
	for _, e := range ir.TableVocabulary(u) {
		fmt.Fprintf(&b, "%s,\n", messageEntryLiteral(e))
	}
	b.WriteString("}\n")
	runtime := goMessageRuntime
	if len(variableTableNames(u)) > 0 {
		runtime = strings.Replace(runtime, "type TableMessageReader struct {", "type TableMessageReader struct { Nodes TableNodeMap;", 1)
		runtime += "\ntype TableMessageWriter struct { TableBitWriter; IndexBits int64; Numbering *TableNumbering }\n"
		runtime += tableMessageRegionRuntime(u)
	} else {
		runtime += "\ntype TableMessageWriter struct { TableBitWriter; IndexBits int64 }\n"
	}
	b.WriteString(runtime)
	return b.String()
}
func byteLiterals(b []byte) string {
	var s strings.Builder
	for _, v := range b {
		fmt.Fprintf(&s, "0x%02x,", v)
	}
	return s.String()
}
func (g *tableGen) messageSlot(e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(g.unit) {
		if e.Key() == v.Key() {
			return i + 1
		}
	}
	panic("message entry outside vocabulary")
}

const goMessageRuntime = `
// TableBitWriter uses the packet wire's low-bit-first order. Measuring walks
// the identical stream, including alignment, without touching a buffer.
type TableBitWriter struct { Buffer []byte; Bits int64; Measuring, Overflow bool }
func (w *TableBitWriter) Put(value uint64,n int64) {
 if n<0||n>64 {w.Overflow=true;return};if n==0{return};at:=w.Bits;w.Bits+=n;if w.Bits<at {w.Overflow=true;return};if w.Measuring{return};if (w.Bits+7)/8>int64(len(w.Buffer)){w.Overflow=true;return}
 if n<64 {value&=(uint64(1)<<n)-1};index:=at>>3;shift:=at&7;head:=uint64(0);if shift!=0 {head=uint64(w.Buffer[index])&((uint64(1)<<shift)-1)};word:=head|value<<shift;need:=(shift+n+7)/8
 if need>=8 {binary.LittleEndian.PutUint64(w.Buffer[index:],word);if need>8 {w.Buffer[index+8]=byte(value>>(64-shift))}}else{for j:=int64(0);j<need;j++ {w.Buffer[index+j]=byte(word>>(j*8))}}
}
func (w *TableBitWriter) Align(){w.Put(0,(-w.Bits)&7)}
func (w *TableBitWriter) Raw(data []byte){w.Align();if w.Measuring {w.Bits+=int64(len(data))*8;return};at:=w.Bits/8;w.Bits+=int64(len(data))*8;if at>int64(len(w.Buffer))-int64(len(data)){w.Overflow=true;return};copy(w.Buffer[at:],data)}
type TableBitReader struct { Buffer []byte; Offset int64 }
func (r *TableBitReader) Get(n int64)(uint64,bool) {if n<0||n>64||r.Offset<0||n>int64(len(r.Buffer))*8-r.Offset{return 0,false};if n==0{return 0,true};at:=r.Offset>>3;shift:=r.Offset&7;need:=(shift+n+7)/8;var value uint64;if need>=8 {value=binary.LittleEndian.Uint64(r.Buffer[at:])>>shift;if need>8 {value|=uint64(r.Buffer[at+8])<<(64-shift)}}else{for j:=int64(0);j<need;j++ {value|=uint64(r.Buffer[at+j])<<(8*j)};value>>=shift};if n<64 {value&=(uint64(1)<<n)-1};r.Offset+=n;return value,true}
func (r *TableBitReader) Skip(n int64)bool {if n<0||n>int64(len(r.Buffer))*8-r.Offset{return false};r.Offset+=n;return true}
func (r *TableBitReader) Align()bool {v,ok:=r.Get((-r.Offset)&7);return ok&&v==0}
func tableMessageBits(n uint64)int64 {var count int64;for n!=0 {count++;n>>=1};return count}

type TableMessageShape struct { Kind, Packing uint8; Bits int64; Base [2]uint64; Min,Max int64; QMin,QDelta float32; QCount uint32 }
type TableMessageEntry struct { Id uint64; Shape,Element TableMessageShape }
// Storage belongs to the caller and is resolved once for one connection direction.
type TableVocabulary struct { Entries []TableMessageEntry; Count,RefBits int64; BuildVersion uint64; Announced,Refused bool; MaxBytes int64 }
func (v *TableVocabulary) Init(storage []TableMessageEntry){*v=TableVocabulary{Entries:storage,MaxBytes:64*1024}}
func (v *TableVocabulary) Entry(ref uint64)(TableMessageEntry,bool){if ref==0||ref>uint64(v.Count){return TableMessageEntry{},false};return v.Entries[ref-1],true}
func (v *TableVocabulary) Name(ref uint64)(uint64,bool){e,ok:=v.Entry(ref);return e.Id,ok&&e.Shape.Kind==0&&e.Id<0xfffffffffffffffd}
func tableMessageNameSlot(id uint64)uint64 {for i:=range tableMessageEntries {e:=&tableMessageEntries[i];if e.Id==id&&e.Shape.Kind==0{return uint64(i+1)}};return 0}
func AnnounceMeasure()int64{return int64(len(tableAnnouncement))}
func Announce(buffer []byte)int64{if len(buffer)<len(tableAnnouncement){return -1};copy(buffer,tableAnnouncement[:]);return int64(len(tableAnnouncement))}
func tableMessageRefuse(r *TableReport,reason string)bool {r.Verdict=TableOpenRefused;r.Reason=reason;return false}
func AnnounceRead(v *TableVocabulary,data []byte,report *TableReport)bool {
 if report==nil {var ignored TableReport;report=&ignored};if v.Announced||v.Refused{return tableMessageRefuse(report,"second_announcement")};ok:=tableAnnounceRead(v,data,report);if !ok{v.Refused=true};return ok
}
func tableAnnounceRead(v *TableVocabulary,data []byte,report *TableReport)bool {
 r,verdict:=tableOpen(data,report);if verdict!=TableOpenOk {if verdict==TableOpenRefused {reason:="newer_form";if len(data)>0&&data[0]==2 {reason="message_form_as_file"};return tableMessageRefuse(report,reason)};report.Malformed=true;return false}
 versionCount,wordsCount:=0,0;var words []byte
 for {ref,ok:=r.Leb();if !ok{report.Malformed=true;return false};if ref==0 {break};id,ok:=r.Resolve(ref);if !ok||!r.Has(1){report.Malformed=true;return false};kind:=r.Get8();switch id {case 0xfffffffffffffffe:if kind!=9||!r.Has(8){report.Malformed=true;return false};v.BuildVersion=r.Get64();versionCount++
 case 0xfffffffffffffffd:if kind!=14{report.Malformed=true;return false};sub,ok:=r.Body();if !ok||!sub.Has(1)||sub.Get8()!=6{report.Malformed=true;return false};n,ok:=sub.Leb();if !ok||n>uint64(len(sub.Buffer))||int64(n)!=int64(len(sub.Buffer))-sub.Offset{report.Malformed=true;return false};limit:=v.MaxBytes;if limit==0{limit=64*1024};if int64(n)>limit{return tableMessageRefuse(report,"vocabulary_too_large")};words=sub.Buffer[sub.Offset:];wordsCount++
 default:report.Unknown++;if !r.Skip(kind){report.Malformed=true;return false}}
 };if versionCount!=1||wordsCount!=1{report.Malformed=true;return false}
 src:=TableReader{Buffer:words,Report:report};count:=int64(0);nodeSeen:=false
 for src.Offset<int64(len(words)){if count>=int64(len(v.Entries)){return tableMessageRefuse(report,"vocabulary_too_large")};e,ok:=tableMessageEntryRead(&src);if !ok||e.Id==0xfffffffffffffffd||e.Id==0xfffffffffffffffe||e.Id==0xffffffffffffffff&&nodeSeen{report.Malformed=true;return false};if e.Id==0xffffffffffffffff{nodeSeen=true};for i:=int64(0);i<count;i++ {if v.Entries[i]==e{report.Malformed=true;return false}};v.Entries[count]=e;count++}
 v.Count=count;v.RefBits=tableMessageBits(uint64(count));if v.RefBits==0{v.RefBits=1};v.Announced=true;return true
}
func tableMessageKnownKind(k uint8)bool {return k<=33}
func tableMessageKindBits(k uint8)int64 {if k==1{return 1};switch k {case 2,6,20,25:return 8;case 3,7,21,26:return 16;case 4,8,10,22,27:return 32;case 5,9,11,23,28:return 64;case 18,19,24,29:return 128};return -1}
func tableMessageShapeRead(r *TableReader,k uint8)(s TableMessageShape,ok bool) {
 s.Kind=k;s.Bits=tableMessageKindBits(k)
 switch {case k>=2&&k<=10||k>=18&&k<=29:
 if !r.Has(1){return s,false};s.Packing=r.Get8();if s.Packing==0{return s,true};if s.Packing==1&&k!=10 {n,ok:=r.Leb();if !ok||n>uint64(s.Bits){return s,false};s.Bits=int64(n);if k>=18 {if !r.Has(16){return s,false};s.Base=[2]uint64{r.Get64(),r.Get64()}}else{base,ok:=r.Leb();if !ok{return s,false};if k>=2&&k<=5 {base=uint64(int64(base>>1)^ -int64(base&1))};s.Base[0]=base};return s,true}
 if s.Packing==2&&k==10 {if !r.Has(12){return s,false};lo:=math.Float32frombits(r.Get32());hi:=math.Float32frombits(r.Get32());res:=math.Float32frombits(r.Get32());if !(lo<hi)||!(res>0){return s,false};delta:=float32(hi-lo);values:=float32(delta/res);if delta-delta!=0||values-values!=0{return s,false};if !(values>=1){values=1}else if values>4294967040.0{values=4294967040.0};s.QMin=lo;s.QDelta=delta;s.QCount=uint32(math.Ceil(float64(values)));s.Bits=tableMessageBits(uint64(s.QCount));return s,true};return s,false
 case k==12||k==33:n,ok:=r.Leb();if !ok||n>math.MaxInt32{return s,false};s.Max=int64(n)
 case k==14||k==16:if k==14 {n,ok:=r.Leb();if !ok||n>math.MaxUint32{return s,false};s.Min=int64(n)};n,ok:=r.Leb();if !ok||n>math.MaxUint32||int64(n)<s.Min{return s,false};s.Max=int64(n)
 };return s,true
}
func tableMessageEntryRead(r *TableReader)(e TableMessageEntry,ok bool){if !r.Has(9){return e,false};e.Id=r.Get64();kind:=r.Get8();if !tableMessageKnownKind(kind){return e,false};e.Shape,ok=tableMessageShapeRead(r,kind);if !ok{return e,false};if kind==14||kind==16 {if !r.Has(1){return e,false};elem:=r.Get8();if !tableMessageKnownKind(elem)||elem==12||elem==33{return e,false};e.Element,ok=tableMessageShapeRead(r,elem);if !ok{return e,false};if elem==14||elem==16 {if !r.Has(1){return e,false};inner:=r.Get8();if !tableMessageKnownKind(inner)||inner==12||inner==33{return e,false}}};return e,true}
func tableMessageCount(r *TableBitReader,s TableMessageShape)(uint64,bool){raw,ok:=r.Get(tableMessageBits(uint64(s.Max-s.Min)));return raw+uint64(s.Min),ok}
func tableMessageSkip(r *TableBitReader,v *TableVocabulary,indexBits int64,e TableMessageEntry,depth int)bool {
 if depth>128{return false};s:=e.Shape;switch s.Kind {
 case 0,32:return true
 case 30:ref,ok:=r.Get(v.RefBits);if !ok||ref==0{return ok};_,ok=v.Name(ref);return ok
 case 13:return tableMessageSkipBody(r,v,indexBits,depth+1)
 case 17:return indexBits>0&&r.Skip(indexBits)
 case 15:ref,ok:=r.Get(v.RefBits);if !ok||ref==0{return ok};arm,ok:=v.Entry(ref);return ok&&arm.Id<0xfffffffffffffffd&&arm.Shape.Kind!=0&&tableMessageSkip(r,v,indexBits,arm,depth+1)
 case 12,33:n,ok:=tableMessageCount(r,s);if !ok{return false};width:=int64(16);if s.Kind==12 {width=8;if !r.Align(){return false}};return r.Skip(int64(n)*width)
 case 31:if !r.Align(){return false};n,ok:=r.Get(32);return ok&&r.Skip(int64(n)*8)
 case 14,16:n,ok:=tableMessageCount(r,s);if !ok{return false};if s.Kind==14&&e.Element.Kind==6&&!r.Align(){return false};if e.Element.Bits>=0 {width:=e.Element.Bits;if s.Kind==16 {width+=v.RefBits};return r.Skip(int64(n)*width)};for i:=uint64(0);i<n;i++ {if s.Kind==16&&!r.Skip(v.RefBits){return false};if !tableMessageSkip(r,v,indexBits,TableMessageEntry{Shape:e.Element},depth+1){return false}};return true
 };return s.Bits>=0&&r.Skip(s.Bits)
}
func tableMessageSkipBody(r *TableBitReader,v *TableVocabulary,indexBits int64,depth int)bool {if depth>128{return false};for {ref,ok:=r.Get(v.RefBits);if !ok||ref==0{return ok};e,ok:=v.Entry(ref);if !ok||e.Id>=0xfffffffffffffffd||!tableMessageSkip(r,v,indexBits,e,depth){return false}}}
// Numeric values are reconstructed at the announced storage width. Typed
// readers then apply the existing exact file-domain clamps to these lanes.
func tableMessageReadScalar(r *TableBitReader,s TableMessageShape,out *[16]byte)bool {lo,ok:=r.Get(min(s.Bits,64));if !ok{return false};hi:=uint64(0);if s.Bits>64 {hi,ok=r.Get(s.Bits-64);if !ok{return false}};if s.Packing==1 {sum:=lo+s.Base[0];carry:=uint64(0);if sum<lo{carry=1};lo=sum;hi+=s.Base[1]+carry};if s.Packing==2 {index:=min(uint32(lo),s.QCount);ratio:=float32(float32(index)/float32(s.QCount));scaled:=float32(ratio*s.QDelta);lo=uint64(math.Float32bits(float32(scaled+s.QMin)))};binary.LittleEndian.PutUint64(out[:],lo);binary.LittleEndian.PutUint64(out[8:],hi);return true}
func tableMessageWriteScalar(w *TableBitWriter,s TableMessageShape,lo,hi uint64) {
 if s.Packing==1 {borrow:=uint64(0);if lo<s.Base[0]{borrow=1};lo-=s.Base[0];hi-=s.Base[1]+borrow};if s.Packing==2 {value:=math.Float32frombits(uint32(lo));ratio:=float32((value-s.QMin)/s.QDelta);if !(ratio>=0){ratio=0}else if !(ratio<=1){ratio=1};scaled:=float32(ratio*float32(s.QCount));lo=uint64(min(uint32(math.Floor(float64(float32(scaled+0.5)))),s.QCount))};w.Put(lo,min(s.Bits,64));if s.Bits>64{w.Put(hi,s.Bits-64)}
}
type TableMessageReader struct { Bits TableBitReader; Vocabulary *TableVocabulary; Report TableReport; IndexBits int64 }
func tableMessageBatchOpen(v *TableVocabulary,data []byte,report *TableReport)(TableMessageReader,int64) {r:=TableMessageReader{Vocabulary:v,Report:*report};defer func(){*report=r.Report}();if len(data)==0{r.Report.Malformed=true;return r,-1};if data[0]!=2{tableMessageRefuse(&r.Report,"newer_form");return r,-1};if v==nil||!v.Announced {tableMessageRefuse(&r.Report,"no_vocabulary");return r,-1};if len(data)<2{r.Report.Malformed=true;return r,-1};r.Bits.Buffer=data[2:];return r,int64(data[1])+1}
func tableMessageBatchClose(r *TableMessageReader)bool {if !r.Bits.Align()||r.Bits.Offset!=int64(len(r.Bits.Buffer))*8 {r.Report.Malformed=true;return false};return true}
`
