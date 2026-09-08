package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// Message batches use one caller-owned region. Every body's directory comes
// first, then its records, then its root, matching the C++ message layout.
func (g *tableGen) emitRegionMessage(st *ir.Struct) {
	if st.IsMapEntry() || !ir.VariableTables(g.unit)[st.Name] {
		return
	}
	n, typ := st.Name, g.storageName(st.Name)
	bytes, str := ir.PointerReachableBlobs(st)
	mask := 0
	if bytes {
		mask |= 1
	}
	if str {
		mask |= 2
	}
	g.pf("func %sMeasureMessages(values []*%s,report *TableReport,context ...TableWriteContext)int64 {arena,allocator:=tableWriteOptions(context);if len(values)<1{return -1};if len(values)>TableMessageBatchMax {if report!=nil{tableMessageRefuse(report,\"batch_too_large\")};return -1};w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};for _,value:=range values{if !tableMessageRegionWrite(&w,unsafe.Pointer(value),%sTableType(),arena,allocator){return -1}};w.Align();if w.Overflow{return -1};return 2+w.Bits/8}\n", n, typ, n)
	g.pf("func %sSaveMessages(values []*%s,buffer []byte,report *TableReport,context ...TableWriteContext)int64 {arena,allocator:=tableWriteOptions(context);if len(values)<1{return -1};if len(values)>TableMessageBatchMax {if report!=nil{tableMessageRefuse(report,\"batch_too_large\")};return -1};if len(buffer)<2{return -1};buffer[0]=2;buffer[1]=byte(len(values)-1);w:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:buffer[2:]}};for _,value:=range values{if !tableMessageRegionWrite(&w,unsafe.Pointer(value),%sTableType(),arena,allocator){return -1}};w.Align();if w.Overflow{return -1};return 2+w.Bits/8}\n", n, typ, n)
	g.pf("func %sLoadMessagesMeasure(vocabulary *TableVocabulary,data []byte)int64 {return tableMessageRegionMeasure(vocabulary,data,%sTableType(),%d)}\n", n, n, mask)
	g.pf("func %sLoadMessages(values []*%s,region []byte,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool){if report==nil {var ignored TableReport;report=&ignored};r,count:=tableMessageBatchOpen(vocabulary,data,report);defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};if len(region)==0||uintptr(unsafe.Pointer(&region[0]))%%uintptr(tableRegionAlign)!=0{r.Report.Malformed=true;return 0,false};clear(region);used:=int64(0);for i:=int64(0);i<count;i++ {p,ok:=tableMessageRegionInto(&r,%sTableType(),%d,region,&used);values[i]=(*%s)(p);if !ok{return i,false}};return count,tableMessageBatchClose(&r)}\n", n, typ, n, mask, typ)
}

func tableMessageRegionRuntime(u *ir.Unit) string {
	s := goMessageRegionRuntime
	if unitHasContainers(u) {
		s = strings.Replace(s, "return tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0)", "return tableMessageExtentBody(r,t,at)", 1)
		s = strings.Replace(s, "return t.LoadMessageBody(r,p)", "r.Nodes.Carve=TableExtentCarve{Base:p,At:tableRegionRound(int64(t.Size)),Limit:size};return t.LoadMessageBody(r,p)", 1)
		s += goMessageExtentRuntime + goMessageMapRuntime
	}
	return s
}

const goMessageRegionRuntime = `
func tableMessageRegionWrite(w *TableMessageWriter,p unsafe.Pointer,t *TableTypeInfo,a *TableArena,allocator ...TableAllocator)bool {
 var ok bool;w.numbering,ok=tableNumber(p,t,a,allocator...);if !ok{return false};n:=&w.numbering;defer n.Release();defer func(){w.Numbering=nil}();w.Numbering=n;w.IndexBits=tableMessageBits(uint64(len(n.entries)));if w.IndexBits<1{w.IndexBits=1}
 if len(n.entries)>1 {slot:=uint64(0);for i:=range tableMessageEntries {if tableMessageEntries[i].Id==0xffffffffffffffff {slot=uint64(i+1);break}};if slot==0||uint64(len(n.entries)-1)>math.MaxUint32{return false};w.Put(slot,TableMessageRefBitsHere);w.Put(uint64(len(n.entries)-1),32)
 for i:=1;i<len(n.entries);i++ {node:=&n.entries[i];slot:=tableMessageNameSlot(node.id);if slot==0{return false};w.Put(slot,TableMessageRefBitsHere);if node.info!=nil {if !node.info.SaveMessageBody(w,node.node){return false}}else {length:=uint64(*(*uint32)(node.node));if length>math.MaxUint32{return false};w.Put(length,32);w.Raw(unsafe.Slice((*byte)(unsafe.Add(node.node,8)),int(length)))}}
 };return t.SaveMessageBody(w,p)
}
func tableMessageNodeOpen(r *TableMessageReader)(int64,bool){start:=r.Bits.Offset;ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return 0,false};if ref==0{r.Bits.Offset=start;return 0,true};e,ok:=r.Vocabulary.Entry(ref);if !ok{return 0,false};if e.Id!=0xffffffffffffffff{r.Bits.Offset=start;return 0,true};n,ok:=r.Bits.Get(32);return int64(n),ok}
func tableMessageExtent(r *TableMessageReader,t *TableTypeInfo,at *int64)bool {return tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0)}
type tableMessageRecord struct { id uint64; size,length int64 }
func tableMessageRecordScan(r *TableMessageReader,root *TableTypeInfo,blobs int)(record tableMessageRecord,ok bool){ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return record,false};record.id,ok=r.Vocabulary.Name(ref);if !ok{return record,false};if record.id==tableBytesTypeId||record.id==tableStringTypeId {n,ok:=r.Bits.Get(32);if !ok||!r.Bits.Align()||!r.Bits.Skip(int64(n)*8){return record,false};record.length=int64(n);if record.id==tableBytesTypeId&&blobs&1!=0||record.id==tableStringTypeId&&blobs&2!=0 {extra:=int64(0);if record.id==tableStringTypeId{extra=1};record.size=tableRegionRound(8+record.length+extra)};return record,true};t:=root.NodeType(record.id);if t==nil{return record,tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0)};extent:=int64(0);if !tableMessageExtent(r,t,&extent){return record,false};record.size=tableRegionRound(tableRegionRound(int64(t.Size))+extent);return record,true}
func tableMessageBodyStorage(r *TableMessageReader,root *TableTypeInfo,blobs int)(size int64,complete,ok bool){count,ok:=tableMessageNodeOpen(r);if !ok{return 0,false,false};r.IndexBits=tableMessageBits(uint64(count+1));size=(count+1)*16;for k:=int64(0);k<count;k++ {record,ok:=tableMessageRecordScan(r,root,blobs);if !ok{return 0,false,false};size+=record.size};extent:=int64(0);complete=tableMessageExtent(r,root,&extent);size+=tableRegionRound(tableRegionRound(int64(root.Size))+extent);return size,complete,true}
func tableMessageRegionMeasure(v *TableVocabulary,data []byte,root *TableTypeInfo,blobs int)int64 {var ignored TableReport;r,count:=tableMessageBatchOpen(v,data,&ignored);if count<0{return -1};total:=int64(0);for i:=int64(0);i<count;i++ {size,complete,ok:=tableMessageBodyStorage(&r,root,blobs);if !ok{return -1};total+=size;if !complete{break}};return total}
func tableMessageLoadInto(r TableMessageReader,t *TableTypeInfo,p unsafe.Pointer,size int64)(TableMessageReader,bool){return t.LoadMessageBody(r,p)}
func tableMessageRegionInto(r *TableMessageReader,root *TableTypeInfo,blobs int,region []byte,used *int64)(out unsafe.Pointer,ok bool){
 count,ok:=tableMessageNodeOpen(r);if !ok{r.Report.Malformed=true;return nil,false};directoryBytes:=(count+1)*16;limit:=int64(len(region));if *used>limit||directoryBytes>limit-*used {r.Report.Malformed=true;return nil,false};base:=unsafe.Pointer(&region[0]);directory:=unsafe.Slice((*TableNodeDirEntry)(unsafe.Add(base,*used)),int(count+1));*used+=directoryBytes;r.IndexBits=tableMessageBits(uint64(count+1));nodes:=TableNodeMap{Base:base,Entries:directory};recordsStart:=r.Bits.Offset;unknown:=int32(0)
 for k:=int64(0);k<count;k++ {record,good:=tableMessageRecordScan(r,root,blobs);if !good{r.Report.Malformed=true;return nil,false};entry:=&directory[k+1];entry.TypeId=record.id;if record.size<=0 {entry.Offset=^uint64(0);unknown++;continue};if record.size>limit-*used{r.Report.Malformed=true;return nil,false};entry.Offset=uint64(*used);p:=unsafe.Add(base,*used);if t:=root.NodeType(record.id);t!=nil{t.Reset(p)}else{*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(record.length))}};*used+=record.size}
 fieldsStart:=r.Bits.Offset;walk:=*r;extent:=int64(0);complete:=tableMessageExtent(&walk,root,&extent);if !complete&&tableMessageHasExtent(root){r.Report.Malformed=true;return nil,false};rootSize:=tableRegionRound(tableRegionRound(int64(root.Size))+extent);if rootSize>limit-*used {r.Report.Malformed=true;return nil,false};directory[0]=TableNodeDirEntry{Offset:uint64(*used),TypeId:root.Id};out=unsafe.Add(base,*used);root.Reset(out);*used+=rootSize;nodes.Good=true;r.Report.Unknown+=unknown;r.Nodes=nodes;r.Bits.Offset=recordsStart
 for k:=int64(0);k<count;k++ {if _,good:=r.Bits.Get(r.Vocabulary.RefBits);!good{r.Report.Malformed=true;return out,false};entry:=directory[k+1];if entry.TypeId==tableBytesTypeId||entry.TypeId==tableStringTypeId {n,good:=r.Bits.Get(32);if !good||!r.Bits.Align(){r.Report.Malformed=true;return out,false};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){r.Report.Malformed=true;return out,false};payload:=r.Bits.Buffer[start:start+int64(n)];if entry.TypeId==tableStringTypeId&&blobs&2!=0&&!tableUtf8Valid(payload){r.Report.Malformed=true;return out,false};if entry.Offset!=^uint64(0){copy(unsafe.Slice((*byte)(unsafe.Add(base,entry.Offset+8)),int(n)),payload)};continue};if entry.Offset==^uint64(0){if !tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0){r.Report.Malformed=true;return out,false};continue};t:=root.NodeType(entry.TypeId);walk:=*r;extent:=int64(0);if !tableMessageExtent(&walk,t,&extent){r.Report.Malformed=true;return out,false};*r,ok=tableMessageLoadInto(*r,t,unsafe.Add(base,entry.Offset),tableRegionRound(int64(t.Size))+extent);if !ok{return out,false}}
 if r.Bits.Offset!=fieldsStart{r.Report.Malformed=true;return out,false};*r,ok=tableMessageLoadInto(*r,root,out,rootSize);return out,ok
}
func tableMessageHasExtent(t *TableTypeInfo)bool {for i:=range t.Fields{if tableMessageFieldHasExtent(&t.Fields[i]){return true}};return false}
func tableMessageFieldHasExtent(f *TableFieldInfo)bool {if f.List||f.Map{return true};if f.Pointer{return false};if f.Kind==13&&f.Table!=nil{return tableMessageHasExtent(f.Table())};if f.Kind==15&&f.Arms!=nil{u:=f.Arms();for i:=1;i<len(u.Arms);i++{if !u.Arms[i].Void&&tableMessageFieldHasExtent(&u.Arms[i].Field){return true}}};return false}
`

const goMessageExtentRuntime = `
func tableMessageExtentBody(r *TableMessageReader,t *TableTypeInfo,at *int64)bool {for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok||ref==0{return ok};e,ok:=r.Vocabulary.Entry(ref);if !ok||e.Id>=0xfffffffffffffffd{return false};var field *TableFieldInfo;for i:=range t.Fields{if t.Fields[i].Id==e.Id{field=&t.Fields[i];break}};if field==nil||!tableMessageFieldHasExtent(field){if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return false};continue};if !tableMessageExtentField(r,e,field,at){return false}}}
func tableMessageExtentField(r *TableMessageReader,e TableMessageEntry,f *TableFieldInfo,at *int64)bool {
 if f.IsArray {kind:=uint8(14);if f.KeyName!=nil{kind=16};if e.Shape.Kind!=kind||e.Element.Kind!=f.Kind&&!tableKindWidens(e.Element.Kind,f.Kind){return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0)};n,ok:=tableMessageCount(&r.Bits,e.Shape);if !ok{return false};if e.Shape.Kind==14&&e.Element.Kind==6&&!r.Bits.Align(){return false};if f.List||f.Map {if n>math.MaxInt32{return false};*at=(*at+int64(f.ElemAlign)-1)& -int64(f.ElemAlign);*at+=int64(n)*int64(f.ElemSize)};element:=TableMessageEntry{Shape:e.Element};if e.Element.Bits>=0{return r.Bits.Skip(int64(n)*e.Element.Bits)};for i:=uint64(0);i<n;i++ {if kind==16&&!r.Bits.Skip(r.Vocabulary.RefBits){return false};if !tableMessageExtentValue(r,element,f,at){return false}};return true};return tableMessageExtentValue(r,e,f,at)
}
func tableMessageExtentValue(r *TableMessageReader,e TableMessageEntry,f *TableFieldInfo,at *int64)bool {
 if e.Shape.Kind==13&&f.Kind==13&&!f.Pointer {return tableMessageExtentBody(r,f.Table(),at)}
 if e.Shape.Kind==15&&f.Kind==15 {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok||ref==0{return ok};arm,ok:=r.Vocabulary.Entry(ref);if !ok||arm.Id>=0xfffffffffffffffd||arm.Shape.Kind==0{return false};u:=f.Arms();for i:=1;i<len(u.Arms);i++{if f.VariantId(uint64(i))==arm.Id&&!u.Arms[i].Void{return tableMessageExtentField(r,arm,&u.Arms[i].Field,at)}};return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,arm,0)}
 return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0)
}
`

const goMessageMapRuntime = `
func tableMessageMapReadKey(r TableMessageReader,f *TableFieldInfo)(key tableMapKey,end int64,bad,over,wide,whole bool){for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return};if ref==0{end=r.Bits.Offset;whole=true;return};e,ok:=r.Vocabulary.Entry(ref);if !ok||e.Id>=0xfffffffffffffffd{return};if e.Id!=f.Id {if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return};continue};bad=e.Shape.Kind!=f.Kind&&!tableKindWidens(e.Shape.Kind,f.Kind);wide=e.Shape.Kind!=f.Kind;if bad{if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return};continue};if f.Kind==12{n,ok:=tableMessageCount(&r.Bits,e.Shape);if !ok||!r.Bits.Align(){return};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){return};key.text=r.Bits.Buffer[start:start+int64(n)];if !tableUtf8Valid(key.text){return};over=n>uint64(f.ArrayBound)}else{var raw [16]byte;if !tableMessageReadScalar(&r.Bits,e.Shape,&raw){return};value:=TableReader{Buffer:raw[:]};if f.Kind>=2&&f.Kind<=5{key.raw=uint64(value.Signed(e.Shape.Kind))}else{key.raw=value.Unsigned(e.Shape.Kind)}}}}
`
