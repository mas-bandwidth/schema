package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

func (g *tableGen) emitRetainMessageRuntime() {
	g.pf("%s", goRetainMessageRuntime)
	source := tableMessageRegionRuntime(g.unit)
	start := strings.Index(source, "func tableMessageLoadInto(")
	end := strings.Index(source, "func tableMessageHasExtent(")
	source = source[start:end]
	source = strings.NewReplacer("tableMessageLoadInto(", "tableMessageLoadIntoRetain(", "tableMessageRegionInto(", "tableMessageRegionIntoRetain(", "TableMessageReader", "TableRetainMessageReader", "t.LoadMessageBody(", "t.LoadMessageBodyRetain(").Replace(source)
	source = strings.ReplaceAll(source, "tableMessageNodeOpen(r)", "tableMessageNodeOpen(&r.tableMessagePlainReader)")
	source = strings.ReplaceAll(source, "tableMessageRecordScan(r,", "tableMessageRecordScan(&r.tableMessagePlainReader,")
	source = strings.ReplaceAll(source, "tableMessageExtent(&walk,", "tableMessageExtent(&walk.tableMessagePlainReader,")
	source = strings.Replace(source, "count,ok:=tableMessageNodeOpen(", "r.Retain.reset(nil,nil);count,ok:=tableMessageNodeOpen(", 1)
	source = strings.Replace(source, "nodes.Good=true;", "nodes.Good=true;r.Retain.reset(base,directory);if r.Retain!=nil{r.Report.RetainLost+=unknown};", 1)
	source = strings.Replace(source, "*r,ok=tableMessageLoadIntoRetain(*r,t,", "r.Path=tableRetainPath{at:unsafe.Add(base,entry.Offset),node:uint32(k+2)};*r,ok=tableMessageLoadIntoRetain(*r,t,", 1)
	source = strings.Replace(source, "*r,ok=tableMessageLoadIntoRetain(*r,root,", "r.Path=tableRetainPath{at:out,node:1};*r,ok=tableMessageLoadIntoRetain(*r,root,", 1)
	g.pf("%s", source)
}
func (g *tableGen) emitRetainMessageSurface(st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() || !ir.VariableTables(g.unit)[st.Name] {
		return
	}
	bytes, str := ir.PointerReachableBlobs(st)
	mask := 0
	if bytes {
		mask |= 1
	}
	if str {
		mask |= 2
	}
	n, typ := st.Name, g.storageName(st.Name)
	g.pf("func %sLoadRetainMessages(values []*%s,region []byte,vocabulary *TableVocabulary,data []byte,retains []TableRetain,report *TableReport)(int64,bool){if report==nil{var ignored TableReport;report=&ignored};plain,count:=tableMessageBatchOpen(vocabulary,data,report);r:=TableRetainMessageReader{tableMessagePlainReader:plain};defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values))||count>int64(len(retains)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};if len(region)==0||uintptr(unsafe.Pointer(&region[0]))%%uintptr(tableRegionAlign)!=0{r.Report.Malformed=true;return 0,false};clear(region);used:=int64(0);for i:=int64(0);i<count;i++{r.Retain=&retains[i];p,ok:=tableMessageRegionIntoRetain(&r,%sTableType(),%d,region,&used);values[i]=(*%s)(p);if !ok{return i,false}};return count,tableMessageBatchClose(&r.tableMessagePlainReader)}\n", n, typ, n, mask, typ)
}
func (g *tableGen) retainMessagePush(index string) string {
	if index == "" {
		index = "0"
	}
	name := g.nextWireWriter()
	g.pf("{%s:=r.Path;r.Path=r.Path.step(%d,uint32(%s))\n", name, g.retainOrdinal, index)
	return name
}
func (g *tableGen) retainMessagePop(name string) { g.pf("r.Path=%s}\n", name) }

func (g *tableGen) retainMessageDiscardCode() string {
	if !g.retain {
		return ""
	}
	return fmt.Sprintf("r.Retain.discard(r.Path,true,%d);", g.retainOrdinal)
}

const goRetainMessageRuntime = `
type tableMessagePlainReader = TableMessageReader
type TableRetainMessageReader struct {tableMessagePlainReader;Retain *TableRetain;Path tableRetainPath}
// Capture starts only after the ordinary skip succeeds. Capacity bounds the
// resolved output before an announced count can expand a zero-bit element.
func(r *TableRetainMessageReader)capture(e TableMessageEntry)bool{
 start:=r.Bits.Offset;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return false};if r.Retain==nil{return true};t,p:=r.Retain,r.Path;header:=int64(26+p.depth*8);limit:=int64(len(t.Bytes))-t.Used-header
 if p.depth>len(p.steps)||limit<0{r.Report.RetainLost++;return true}
 probe:=tableRetainMessageIn{bits:r.Bits,v:r.Vocabulary,w:TableWriter{Measuring:true},limit:limit};probe.bits.Offset=start
 if !probe.payload(e,0)||probe.bits.Offset!=r.Bits.Offset||probe.w.Offset>limit{r.Report.RetainLost++;return true};need:=header+probe.w.Offset;if need>0xffffffff{r.Report.RetainLost++;return true}
 b:=t.Bytes[t.Used:t.Used+need];clear(b[:26]);binary.LittleEndian.PutUint32(b,uint32(need));binary.LittleEndian.PutUint32(b[4:],p.node);binary.LittleEndian.PutUint32(b[8:],uint32(p.depth));binary.LittleEndian.PutUint32(b[12:],uint32(probe.w.Offset));binary.LittleEndian.PutUint64(b[16:],e.Id);b[24]=e.Shape.Kind
 for i:=0;i<p.depth;i++{binary.LittleEndian.PutUint32(b[26+i*8:],p.steps[i].ordinal);binary.LittleEndian.PutUint32(b[30+i*8:],p.steps[i].index)}
 write:=tableRetainMessageIn{bits:r.Bits,v:r.Vocabulary,w:TableWriter{Buffer:b[header:]},limit:limit};write.bits.Offset=start;if !write.payload(e,0)||write.w.Overflow{r.Report.RetainLost++;return true};t.Used+=need;t.Count++;r.Report.Retained++;return true
}
type tableRetainMessageIn struct{bits TableBitReader;v *TableVocabulary;w TableWriter;limit int64}
func(s *tableRetainMessageIn)room(n int64)bool{return n>=0&&n<=s.limit-s.w.Offset}
func(s *tableRetainMessageIn)ref(zero bool)(TableMessageEntry,bool){ref,ok:=s.bits.Get(s.v.RefBits);if !ok{return TableMessageEntry{},false};if ref==0{if !zero||!s.room(8){return TableMessageEntry{},false};s.w.Put64(0);return TableMessageEntry{},true};e,ok:=s.v.Entry(ref);if !ok||e.Id==0||e.Id>=0xfffffffffffffffd||!s.room(8){return e,false};s.w.Put64(e.Id);return e,true}
func(s *tableRetainMessageIn)scalar(e TableMessageEntry)bool{
 width:=tableKindBytes(e.Shape.Kind);if width==0||!s.room(width){return false};shape:=e.Shape
 if shape.Packing==2 {probe:=s.bits;index,ok:=probe.Get(shape.Bits);if !ok||index>uint64(shape.QCount){return false}}
 var raw [16]byte;if !tableMessageReadScalar(&s.bits,shape,&raw){return false}
 if shape.Packing==0&&(shape.Kind>=2&&shape.Kind<=5||shape.Kind>=20&&shape.Kind<=24)&&shape.Bits>0&&shape.Bits<64 {v:=binary.LittleEndian.Uint64(raw[:]);if v&(uint64(1)<<uint(shape.Bits-1))!=0{v|=^uint64(0)<<uint(shape.Bits);binary.LittleEndian.PutUint64(raw[:],v)}}
 s.w.Raw(raw[:width]);return !s.w.Overflow
}
func(s *tableRetainMessageIn)opaque(e TableMessageEntry,framed bool)bool{var n uint64;var ok bool
 if e.Shape.Kind==12||e.Shape.Kind==33 {n,ok=tableMessageCount(&s.bits,e.Shape);if !ok{return false};if e.Shape.Kind==33{n*=2}else if !s.bits.Align(){return false}}else{if !s.bits.Align(){return false};n,ok=s.bits.Get(32);if !ok{return false}}
 extra:=int64(0);if framed{extra=tableLebBytes(n)};if n>0x7fffffff||!s.room(int64(n)+extra)||int64(n)*8>int64(len(s.bits.Buffer))*8-s.bits.Offset{return false};if framed{s.w.PutLeb(n)};for i:=uint64(0);i<n;i++{v,ok:=s.bits.Get(8);if !ok{return false};s.w.Put8(byte(v))};return true
}
func(s *tableRetainMessageIn)framed(e TableMessageEntry,depth int)bool{if depth>64||!s.room(8){return false};at:=s.w.Offset;s.w.Put64(0);before:=s.w.Offset;if !s.content(e,depth)||s.w.Offset-before>0xffffffff{return false};if !s.w.Measuring{binary.LittleEndian.PutUint32(s.w.Buffer[at:],uint32(s.w.Offset-before))};return true}
func(s *tableRetainMessageIn)content(e TableMessageEntry,depth int)bool{if depth>64{return false};switch e.Shape.Kind{
 case 17:return false
 case 13:for{entry,ok:=s.ref(true);if !ok{return false};if entry.Id==0{return true};if !s.room(1){return false};s.w.Put8(entry.Shape.Kind);if !s.payload(entry,depth){return false}}
 case 14,16:return s.elements(e,depth)
 case 15,30:return s.payload(e,depth)
 case 32:return true
 case 12,31,33:return s.opaque(e,false)
 };return s.scalar(e)}
func tableRetainMessageMinimum(kind uint8)int64{if n:=tableKindBytes(kind);n!=0{return n};switch kind{case 13:return 16;case 14,16:return 10;case 15,30:return 8;case 17:return -1;case 12,31,32,33:return 1};return -1}
func(s *tableRetainMessageIn)elements(e TableMessageEntry,depth int)bool{if e.Element.Kind==17{return false};n,ok:=tableMessageCount(&s.bits,e.Shape);if !ok{return false};if e.Shape.Kind==14&&e.Element.Kind==6&&!s.bits.Align(){return false};if !s.room(1+tableLebBytes(n)){return false};s.w.Put8(e.Element.Kind);s.w.PutLeb(n);floor:=tableRetainMessageMinimum(e.Element.Kind);if e.Shape.Kind==16{floor=16};if floor<0||n>uint64((s.limit-s.w.Offset)/floor){return false};element:=TableMessageEntry{Shape:e.Element}
 for i:=uint64(0);i<n;i++{if e.Shape.Kind==16{if _,ok:=s.ref(false);!ok||!s.framed(element,depth+1){return false}}else if !s.payload(element,depth){return false}};return true}
func(s *tableRetainMessageIn)payload(e TableMessageEntry,depth int)bool{switch e.Shape.Kind{
 case 0,17:return false
 case 32:if !s.room(1){return false};s.w.PutLeb(0);return true
 case 30:_,ok:=s.ref(true);return ok
 case 15:arm,ok:=s.ref(true);if !ok{return false};if arm.Id==0{return true};if arm.Shape.Kind==0||!s.room(1){return false};s.w.Put8(arm.Shape.Kind);return s.framed(arm,depth+1)
 case 13,14,16:return s.framed(e,depth+1)
 case 12,31,33:return s.opaque(e,true)
 };return s.scalar(e)}
`
