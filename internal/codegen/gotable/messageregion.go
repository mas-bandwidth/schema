package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM OVER A VARIABLE ROOT (docs/SPEC-TABLES.md §3.3, "The message
// form over a variable root"): a plain `table` rides the form exactly as the C++
// and C emitters carry it (internal/codegen/cpptable/messagevariable.go,
// internal/codegen/ctable/message_graph.go), and the bytes the conformance
// corpus pins for them are normative. A batch of variable roots takes ONE
// caller-owned REGION: every body's node directory comes first, then its
// records, then its root, matching the C++ message layout, and each body keeps
// its own numbering. A fixed table root keeps the non-region batch surface
// emitted by emitMessageWrite and emitMessageRead.
//
// emitRegionMessage emits one variable root's batch verbs: MeasureMessages and
// SaveMessages over an array of root pointers, LoadMessagesMeasure for the
// region the batch needs, and the TOLERANT and the STRICT (§6.5) reads.
func (g *tableGen) emitRegionMessage(st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() || !ir.VariableTables(g.unit)[st.Name] {
		return
	}
	n, typ := st.Name, g.storageName(st.Name)
	mask := regionBlobMask(st)
	// THE MESSAGE FORM CARRIES THE TRAILER PER BATCH (§2.14, §3.3) for a root
	// that declares `integrity`, as the fixed root's batch does; a unit with
	// no declaring table opens and writes the bytes it did before.
	open, measureFrame, bufferEnd, finish := "tableMessageBatchOpen(vocabulary,data,report)", "2", "buffer[2:]", "2+w.Bits/8"
	if len(ir.TableIntegrityTables(g.unit)) > 0 {
		sealed := "false"
		if st.Integrity != "" {
			sealed = "true"
			measureFrame, bufferEnd, finish = "6", "buffer[2:len(buffer)-4]", "tableIntegritySeal(buffer,2+w.Bits/8)"
		}
		open = "tableMessageBatchOpenSealed(vocabulary,data,report," + sealed + ")"
	}
	minimum := measureFrame
	refuse := "if len(values)<1||len(values)>TableMessageBatchMax{if report!=nil{tableMessageRefuse(report,\"batch_too_large\")};return -1}"
	// a blob named from two slots is refused by name, blob_shared, and never
	// written once a slot (§2.5, §16.7)
	shared := "if w.numbering.reason==TableRefuseBlobShared&&report!=nil{tableMessageRefuse(report,\"blob_shared\")};return -1"
	g.pf("// %sMeasureMessages sizes a batch of %s roots on the MESSAGE form (docs/SPEC-TABLES.md §3.3): one\n// count and one bit stream, each body carrying its own numbering with its node table first.\n", n, n)
	g.pf("func %sMeasureMessages(values []*%s,report *TableReport,context ...TableWriteContext)int64 {arena,allocator:=tableWriteOptions(context);%s;w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};for _,value:=range values {if value==nil{return -1};if !tableMessageRegionWrite(&w,unsafe.Pointer(value),%sTableType(),arena,allocator){%s}};w.Align();if w.Overflow{return -1};return %s+w.Bits/8}\n", n, typ, refuse, n, shared, measureFrame)
	g.pf("func %sSaveMessages(values []*%s,buffer []byte,report *TableReport,context ...TableWriteContext)int64 {arena,allocator:=tableWriteOptions(context);%s;if len(buffer)<%s{return -1};buffer[0]=2;buffer[1]=byte(len(values)-1);w:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:%s}};for _,value:=range values {if value==nil{return -1};if !tableMessageRegionWrite(&w,unsafe.Pointer(value),%sTableType(),arena,allocator){%s}};w.Align();if w.Overflow{return -1};if report!=nil{report.Clamped+=w.Clamped};return %s}\n", n, typ, refuse, minimum, bufferEnd, n, shared, finish)
	g.pf("// %sLoadMessagesMeasure is the exact region bytes ONE BATCH needs (§3.3): a scan by the announced\n// shapes that reads no field value, -1 for a wire it cannot size.\n", n)
	g.pf("func %sLoadMessagesMeasure(vocabulary *TableVocabulary,data []byte)int64 {return tableMessageRegionMeasure(vocabulary,data,%sTableType(),%d)}\n", n, n, mask)
	g.pf("// %sLoadMessagesTolerant is the TOLERANT message read (docs/SPEC-TABLES.md §6.5, §3.3): the batch into\n// the caller's region, each body's root written into values; damage inside body k delivers k-1.\n", n)
	g.pf("func %sLoadMessagesTolerant(values []*%s,region []byte,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {if report==nil {var ignored TableReport;report=&ignored};r,count:=%s;defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};if len(region)==0||uintptr(unsafe.Pointer(&region[0]))%%uintptr(tableRegionAlign)!=0{r.Report.Malformed=true;return 0,false};clear(region);used:=int64(0);for i := range count {p,ok:=tableMessageRegionInto(&r,%sTableType(),%d,region,&used);values[i]=(*%s)(p);if !ok||r.Report.NonFiniteFloat!=\"\"{if tableNonFiniteRefused(&r.Report){for j := range i+1 {values[j]=nil};return 0,false};return i,false}};return count,tableMessageBatchClose(&r)}\n", n, typ, open, n, mask, typ)
	g.pf("// %sLoadMessages is the plain message read, and it is STRICT (§6.5): ok is false for whatever\n// %sReadStrict refuses, and a batch refused by the strict rule alone answers a count of 0 with every\n// root it had written cleared. A supplied report receives THIS read's report; report.Err() is the reason.\n", n, n)
	g.pf("func %sLoadMessages(values []*%s,region []byte,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {read:=report;if read==nil{read=&TableReport{}}else{*read=TableReport{}};count,ok:=%sLoadMessagesTolerant(values,region,vocabulary,data,read);if !ok{return count,false};if read.Err()!=nil{for i := range count {values[i]=nil};return 0,false};return count,true}\n", n, typ, n)
}

// regionBlobMask is the blob kinds a root's pointers reach: bit 0 a `*bytes`,
// bit 1 a `*string` (§2.5), which is what a record of each commands storage for.
func regionBlobMask(st *ir.Struct) int {
	bytes, str := ir.PointerReachableBlobs(st)
	mask := 0
	if bytes {
		mask |= 1
	}
	if str {
		mask |= 2
	}
	return mask
}

func tableMessageRegionRuntime(u *ir.Unit) string {
	s := goMessageRegionRuntime
	if unitHasContainers(u) {
		s = tableSourceReplace(s, "return tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,depth)", "return tableMessageExtentBody(r,t,at,depth)", 1)
		s = tableSourceReplace(s, "return t.LoadMessageBody(r,p)", "r.Nodes.Carve=TableExtentCarve{Base:p,At:tableRegionRound(int64(t.Size)),Limit:size};return t.LoadMessageBody(r,p)", 1)
		s += goMessageExtentRuntime + goMessageMapRuntime
	}
	return s
}

// The region writer refuses a blob named from two slots by name, blob_shared
// (the reason is left in w.numbering.reason), and never writes a copy per slot;
// the region reader resolves under Once, so a second slot naming one blob names
// the one decoded node and counts TableReport.BlobShared (docs/SPEC-TABLES.md
// §2.5, §16.7).
const goMessageRegionRuntime = `
func tableMessageRegionWrite(w *TableMessageWriter,p unsafe.Pointer,t *TableTypeInfo,a *TableArena,allocator ...TableAllocator)bool {
 var ok bool;w.numbering,ok=tableNumber(p,t,a,allocator...);if !ok{return false};if w.numbering.shared{w.numbering.Release();w.numbering.reason=TableRefuseBlobShared;return false};n:=&w.numbering;defer n.Release();defer func(){w.Numbering=nil}();w.Numbering=n;w.IndexBits=tableMessageBits(uint64(len(n.entries)));w.IndexBits=max(w.IndexBits,1)
 if len(n.entries)>1 {slot:=uint64(0);for i:=range tableMessageEntries {if tableMessageEntries[i].ID==0xffffffffffffffff {slot=uint64(i+1);break}};if slot==0||uint64(len(n.entries)-1)>math.MaxUint32{return false};w.Put(slot,TableMessageRefBitsHere);w.Put(uint64(len(n.entries)-1),32)
 for i:=1;i<len(n.entries);i++ {node:=&n.entries[i];slot:=tableMessageNameSlot(node.id);if slot==0{return false};w.Put(slot,TableMessageRefBitsHere);if node.info!=nil {if !node.info.SaveMessageBody(w,node.node){return false}}else {length:=uint64(*(*uint32)(node.node));if length>math.MaxUint32{return false};w.Put(length,32);w.Raw(unsafe.Slice((*byte)(unsafe.Add(node.node,8)),int(length)))}}
 };return t.SaveMessageBody(w,p)
}
func tableMessageNodeOpen(r *TableMessageReader)(int64,bool){start:=r.Bits.Offset;ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return 0,false};if ref==0{r.Bits.Offset=start;return 0,true};e,ok:=r.Vocabulary.Entry(ref);if !ok{return 0,false};if e.ID!=0xffffffffffffffff{r.Bits.Offset=start;return 0,true};n,ok:=r.Bits.Get(32);return int64(n),ok}
func tableMessageExtent(r *TableMessageReader,t *TableTypeInfo,at *int64,depth int)bool {return tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,depth)}
type tableMessageRecord struct { id uint64; size,length int64 }
func tableMessageRecordScan(r *TableMessageReader,root *TableTypeInfo,blobs int)(record tableMessageRecord,ok bool){ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return record,false};record.id,ok=r.Vocabulary.Name(ref);if !ok{return record,false};if record.id==tableBytesTypeID||record.id==tableStringTypeID {n,ok:=r.Bits.Get(32);if !ok||!r.Bits.Align()||!r.Bits.Skip(int64(n)*8){return record,false};record.length=int64(n);if record.id==tableBytesTypeID&&blobs&1!=0||record.id==tableStringTypeID&&blobs&2!=0 {extra:=int64(0);if record.id==tableStringTypeID{extra=1};record.size=tableRegionRound(8+record.length+extra)};return record,true};t:=root.NodeType(record.id);if t==nil{return record,tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0)};extent:=int64(0);if !tableMessageExtent(r,t,&extent,0){return record,false};record.size=tableRegionRound(tableRegionRound(int64(t.Size))+extent);return record,true}
func tableMessageBodyStorage(r *TableMessageReader,root *TableTypeInfo,blobs int)(size int64,complete,ok bool){count,ok:=tableMessageNodeOpen(r);if !ok{return 0,false,false};r.IndexBits=tableMessageBits(uint64(count+1));size=(count+1)*16;for range count {record,ok:=tableMessageRecordScan(r,root,blobs);if !ok{return 0,false,false};size+=record.size};extent:=int64(0);complete=tableMessageExtent(r,root,&extent,0);size+=tableRegionRound(tableRegionRound(int64(root.Size))+extent);return size,complete,true}
func tableMessageRegionMeasure(v *TableVocabulary,data []byte,root *TableTypeInfo,blobs int)int64 {var ignored TableReport;r,count:=tableMessageBatchOpen(v,data,&ignored);if count<0{return -1};total:=int64(0);for range count {size,complete,ok:=tableMessageBodyStorage(&r,root,blobs);if !ok{return -1};total+=size;if !complete{break}};return total}
func tableMessageLoadInto(r TableMessageReader,t *TableTypeInfo,p unsafe.Pointer,size int64)(TableMessageReader,bool){return t.LoadMessageBody(r,p)}
func tableMessageRegionInto(r *TableMessageReader,root *TableTypeInfo,blobs int,region []byte,used *int64)(out unsafe.Pointer,ok bool){
 count,ok:=tableMessageNodeOpen(r);if !ok{r.Report.Malformed=true;return nil,false};directoryBytes:=(count+1)*16;limit:=int64(len(region));if *used>limit||directoryBytes>limit-*used {r.Report.Malformed=true;return nil,false};base:=unsafe.Pointer(&region[0]);directory:=unsafe.Slice((*TableNodeDirEntry)(unsafe.Add(base,*used)),int(count+1));*used+=directoryBytes;r.IndexBits=tableMessageBits(uint64(count+1));nodes:=TableNodeMap{Base:base,Entries:directory,Once:true};recordsStart:=r.Bits.Offset;unknown:=int32(0)
 for k := range count {record,good:=tableMessageRecordScan(r,root,blobs);if !good{r.Report.Malformed=true;return nil,false};entry:=&directory[k+1];entry.TypeID=record.id;if record.size<=0 {entry.Offset=^uint64(0);unknown++;continue};if record.size>limit-*used{r.Report.Malformed=true;return nil,false};entry.Offset=uint64(*used);p:=unsafe.Add(base,*used);if t:=root.NodeType(record.id);t!=nil{t.Reset(p)}else{*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(record.length))}};*used+=record.size}
 fieldsStart:=r.Bits.Offset;walk:=*r;extent:=int64(0);complete:=tableMessageExtent(&walk,root,&extent,0);if !complete&&tableMessageHasExtent(root){r.Report.Malformed=true;return nil,false};rootSize:=tableRegionRound(tableRegionRound(int64(root.Size))+extent);if rootSize>limit-*used {r.Report.Malformed=true;return nil,false};directory[0]=TableNodeDirEntry{Offset:uint64(*used),TypeID:root.ID};out=unsafe.Add(base,*used);root.Reset(out);*used+=rootSize;nodes.Good=true;r.Report.Unknown+=unknown;r.Nodes=nodes;r.Bits.Offset=recordsStart
 for k := range count {if _,good:=r.Bits.Get(r.Vocabulary.RefBits);!good{r.Report.Malformed=true;return out,false};entry:=directory[k+1];if entry.TypeID==^tableBytesTypeID||entry.TypeID==^tableStringTypeID{entry.TypeID=^entry.TypeID};if entry.TypeID==tableBytesTypeID||entry.TypeID==tableStringTypeID {n,good:=r.Bits.Get(32);if !good||!r.Bits.Align(){r.Report.Malformed=true;return out,false};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){r.Report.Malformed=true;return out,false};payload:=r.Bits.Buffer[start:start+int64(n)];if entry.TypeID==tableStringTypeID&&blobs&2!=0&&!tableUTF8Valid(payload){r.Report.Malformed=true;return out,false};if entry.Offset!=^uint64(0){copy(unsafe.Slice((*byte)(unsafe.Add(base,entry.Offset+8)),int(n)),payload)};continue};if entry.Offset==^uint64(0){if !tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0){r.Report.Malformed=true;return out,false};continue};t:=root.NodeType(entry.TypeID);walk:=*r;extent:=int64(0);if !tableMessageExtent(&walk,t,&extent,0){r.Report.Malformed=true;return out,false};*r,ok=tableMessageLoadInto(*r,t,unsafe.Add(base,entry.Offset),tableRegionRound(int64(t.Size))+extent);if !ok{return out,false}}
 if r.Bits.Offset!=fieldsStart{r.Report.Malformed=true;return out,false};*r,ok=tableMessageLoadInto(*r,root,out,rootSize);return out,ok
}
func tableMessageHasExtent(t *TableTypeInfo)bool {for i:=range t.Fields{if tableMessageFieldHasExtent(&t.Fields[i]){return true}};return false}
func tableMessageFieldHasExtent(f *TableFieldInfo)bool {if f.List||f.Map{return true};if f.Pointer{return false};if f.Kind==13&&f.Table!=nil{return tableMessageHasExtent(f.Table())};if f.Kind==15&&f.Arms!=nil{u:=f.Arms();for i:=1;i<len(u.Arms);i++{if !u.Arms[i].Void&&tableMessageFieldHasExtent(&u.Arms[i].Field){return true}}};return false}
`

const goMessageExtentRuntime = `
func tableMessageExtentBody(r *TableMessageReader,t *TableTypeInfo,at *int64,depth int)bool {if depth>128{return false};for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok||ref==0{return ok};e,ok:=r.Vocabulary.Entry(ref);if !ok||e.ID>=0xfffffffffffffffd{return false};var field *TableFieldInfo;for i:=range t.Fields{if t.Fields[i].ID==e.ID{field=&t.Fields[i];break}};if field==nil||!tableMessageFieldHasExtent(field){if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,depth){return false};continue};if !tableMessageExtentField(r,e,field,at,depth){return false}}}
func tableMessageExtentField(r *TableMessageReader,e TableMessageEntry,f *TableFieldInfo,at *int64,depth int)bool {if depth>128{return false};
 if f.IsArray {kind:=uint8(14);if f.KeyName!=nil{kind=16};if e.Shape.Kind!=kind||e.Element.Kind!=f.Kind&&!tableKindWidens(e.Element.Kind,f.Kind){return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,depth)};n,ok:=tableMessageCount(&r.Bits,e.Shape);if !ok{return false};if e.Shape.Kind==14&&e.Element.Kind==6&&!r.Bits.Align(){return false};if f.List||f.Map {if n>math.MaxInt32{return false};*at=(*at+int64(f.ElemAlign)-1)& -int64(f.ElemAlign);*at+=int64(n)*int64(f.ElemSize)};element:=TableMessageEntry{Shape:e.Element};if e.Element.Bits>=0{return r.Bits.Skip(int64(n)*e.Element.Bits)};for range n {if kind==16&&!r.Bits.Skip(r.Vocabulary.RefBits){return false};if !tableMessageExtentValue(r,element,f,at,depth+1){return false}};return true};return tableMessageExtentValue(r,e,f,at,depth)
}
func tableMessageExtentValue(r *TableMessageReader,e TableMessageEntry,f *TableFieldInfo,at *int64,depth int)bool {if depth>128{return false};
 if e.Shape.Kind==13&&f.Kind==13&&!f.Pointer {return tableMessageExtentBody(r,f.Table(),at,depth+1)}
 if e.Shape.Kind==15&&f.Kind==15 {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok||ref==0{return ok};arm,ok:=r.Vocabulary.Entry(ref);if !ok||arm.ID>=0xfffffffffffffffd||arm.Shape.Kind==0{return false};u:=f.Arms();for i:=1;i<len(u.Arms);i++{if f.VariantID(uint64(i))==arm.ID&&!u.Arms[i].Void{return tableMessageExtentField(r,arm,&u.Arms[i].Field,at,depth+1)}};return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,arm,depth+1)}
 return tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,depth)
}
`

const goMessageMapRuntime = `
func tableMessageMapReadKey(r TableMessageReader,f *TableFieldInfo)(key tableMapKey,end int64,bad,over,wide,whole bool){for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{return};if ref==0{end=r.Bits.Offset;whole=true;return};e,ok:=r.Vocabulary.Entry(ref);if !ok||e.ID>=0xfffffffffffffffd{return};if e.ID!=f.ID {if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return};continue};bad=e.Shape.Kind!=f.Kind&&!tableKindWidens(e.Shape.Kind,f.Kind);wide=wide||e.Shape.Kind!=f.Kind&&!bad;if bad{if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,e,0){return};continue};if f.Kind==12{n,ok:=tableMessageCount(&r.Bits,e.Shape);if !ok||!r.Bits.Align(){return};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){return};key.text=r.Bits.Buffer[start:start+int64(n)];if !tableUTF8Valid(key.text){return};over=n>uint64(f.ArrayBound)}else{var raw [16]byte;if !tableMessageReadScalar(&r.Bits,e.Shape,&raw){return};value:=TableReader{Buffer:raw[:]};if f.Kind>=2&&f.Kind<=5{key.raw=uint64(value.Signed(e.Shape.Kind))}else{key.raw=value.Unsigned(e.Shape.Kind)}}}}
`
