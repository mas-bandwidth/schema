package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A blob named by two slots (docs/SPEC-TABLES.md §2.5, §16.7): written
// strict, read tolerant, refused by name on the strict read. The seed-24 unit
// and wire are the random-table differential fuzz's (schema#2038, seed 24,
// value 14, xor 0x01 at byte 6 of its wire).
const blobNamedTwiceSeed24Unit = `package probe
enum E0 { Alpha }
fixed table T0 {
 f0 int16
 f1 uint8
 f2 ?int32
 f3 [3]float32
 f4 [..3]int16
}
union U0 {
 ptr *bytes
 label *string
 tally int8 | min = -90, max = 106
}
table Root {
 f0 bool
 f1 bool
 f2 *bytes
 f3 uint32 = 2351968925 | min = 1691408928, max = 2403887662
 f4 string(4)
 f5 E0
 f6 int32 | min = -2147483648, max = 2147483647
 f7 int8 = 93 | min = 69, max = 94
 u0 U0
}
`

const blobNamedTwiceSeed24Wire = "01010101021103030820dad064041e05060401000080070246080f091101030a0c0d020b06c100ff2e7d080b02d11d00382842b5078b9808512d42b5078e98089e2b42b5078d9808042f42b5078f98088be145c2cb04da4f1d3442b5079298086a3242b507919808404667b5077ac408c9e19b56195cf677ffffffffffffffffe44f1c4f47c02e2f0b00000000000000"

// blobNamedTwiceProbeHead is the probes' head: the seed-24 wires, a
// cache-line-aligned caller region, and the blob_shared check.
const blobNamedTwiceProbeHead = `package probe
import("bytes";"encoding/base64";"encoding/hex";"errors";"runtime";"testing";"unsafe")
var _ = bytes.Equal; var _ = base64.StdEncoding; var _ = hex.DecodeString; var _ = runtime.GC; var _ = unsafe.Pointer(nil)
const seed24 = "` + blobNamedTwiceSeed24Wire + `"
func wires(t testing.TB)([]byte,[]byte){d,err:=hex.DecodeString(seed24);if err!=nil{t.Fatal(err)};c:=append([]byte(nil),d...);c[6]^=0x01;return d,c}
func region(size int64)[]byte{raw:=make([]byte,size+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(size)]}
var _ = errors.Is
func shared(t *testing.T,what string,err error){t.Helper();if !errors.Is(err,TableRefuseBlobShared)||err.Error()!="blob_shared"{t.Fatalf("%s: %v, want blob_shared",what,err)}}
`

// TestBlobNamedTwiceSeed24 pins the fuzz's own wire: the strict reads refuse
// it by name, the tolerant Load keeps it and names it in its report, and the
// writers refuse its value by name rather than with a bare -1. The clean wire
// still reads strict and round-trips byte for byte.
func TestBlobNamedTwiceSeed24(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	out, err := runGeneratedResult(t, blobNamedTwiceSeed24Unit, blobNamedTwiceProbeHead+`
func TestSeed24(t *testing.T){
 damaged,clean:=wires(t)
 if v,err:=RootReadStrict(damaged);v!=nil{t.Fatal("ReadStrict answered a value for a blob named twice")}else{shared(t,"ReadStrict",err)}
 var lt TableReport;if v,err:=RootLoadText(base64.StdEncoding.EncodeToString(damaged),&lt);v!=nil{t.Fatal("LoadText answered a value")}else{shared(t,"LoadText",err)};if lt.BlobShared!=1{t.Fatalf("LoadText report %+v",lt)}
 size:=RootLoadMeasure(damaged);if size<=0{t.Fatalf("LoadMeasure %d",size)};var pr TableReport;if pv:=RootLoad(region(size),damaged,&pr);pv!=nil{t.Fatal("the strict plain Load answered a root for a blob named twice")}else{shared(t,"plain Load",pr.Err())}
 var report TableReport;v:=RootLoadTolerant(region(size),damaged,&report)
 if v==nil||report.Malformed||report.BlobShared!=1{t.Fatalf("tolerant Load: %v %+v",v==nil,report)};shared(t,"Load report",report.Err())
 a,b:=TableBytesAt(&v.F2),TableBytesAt(&v.U0.Ptr().Value);if a.Length==0||&a.Data[0]!=&b.Data[0]{t.Fatal("the two slots do not name the one decoded node")}
 if n:=RootToJsonMeasure(v);n!= -1{t.Fatalf("ToJsonMeasure %d",n)};if n,err:=RootToJsonMeasureReason(v);n!= -1{t.Fatalf("ToJsonMeasureReason %d",n)}else{shared(t,"ToJsonMeasureReason",err)}
 if n,err:=RootMeasureReason(v);n!= -1{t.Fatalf("MeasureReason %d",n)}else{shared(t,"MeasureReason",err)};if RootSave(v,make([]byte,4096))!= -1{t.Fatal("Save wrote a blob named twice")}
 c,err:=RootReadStrict(clean);if err!=nil||c==nil{t.Fatalf("clean ReadStrict: %v",err)};n:=RootMeasure(c);buf:=make([]byte,n);if n<=0||RootSave(c,buf)!=n||!bytes.Equal(buf,clean){t.Fatalf("clean round trip %d of %d",n,len(clean))}
}
`)
	if err != nil {
		t.Fatalf("seed-24 probe: %v\n%s", err, out)
	}
}

// TestBlobNamedTwiceWriters pins every writer's refusal of a graph whose two
// slots (Root.f2 and the u0 ptr arm) name one blob, and that the same value
// with two blobs saves and round-trips.
func TestBlobNamedTwiceWriters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	out, err := runGeneratedResult(t, blobNamedTwiceSeed24Unit, blobNamedTwiceProbeHead+`
func TestWriters(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();r:=b.GetRoot();data,ref:=b.Main.AllocBytes(3);copy(data,"abc");r.F2=ref;r.U0.Type=U0TypePtr;r.U0.Ptr().Value=ref
 if n,err:=RootMeasureReason(r,&b.Arena);n!= -1{t.Fatalf("MeasureReason %d",n)}else{shared(t,"MeasureReason",err)}
 if RootSave(r,make([]byte,4096),&b.Arena)!= -1{t.Fatal("Save wrote a blob named twice")}
 if s,err:=RootSaveText(r,&b.Arena);s!=""{t.Fatal("SaveText wrote a blob named twice")}else{shared(t,"SaveText",err)}
 if !b.Lock(){t.Fatal("Lock refused: a region holds the one node")};c:=b.AsConst()
 if n:=RootToJsonMeasure(c);n!= -1{t.Fatalf("ToJsonMeasure %d",n)};if n:=RootToJson(c,make([]byte,4096));n!= -1{t.Fatalf("ToJson %d",n)}
 if n,err:=RootToJsonMeasureReason(c);n!= -1{t.Fatalf("ToJsonMeasureReason %d",n)}else{shared(t,"ToJsonMeasureReason",err)}
 var s RootBuilder;if !s.Init(){t.Fatal("init")};defer s.Shutdown();q:=s.GetRoot();d1,r1:=s.Main.AllocBytes(3);copy(d1,"abc");d2,r2:=s.Main.AllocBytes(3);copy(d2,"abc");q.F2=r1;q.U0.Type=U0TypePtr;q.U0.Ptr().Value=r2
 n,err:=RootMeasureReason(q,&s.Arena);if err!=nil||n<=0{t.Fatalf("two blobs MeasureReason %d %v",n,err)};wire:=make([]byte,n);if RootSave(q,wire,&s.Arena)!=n{t.Fatal("two blobs save")}
 text,err:=RootSaveText(q,&s.Arena);if err!=nil||text!=base64.StdEncoding.EncodeToString(wire){t.Fatalf("two blobs SaveText %v",err)}
 v,err:=RootReadStrict(wire);if err!=nil||v==nil{t.Fatalf("two blobs ReadStrict %v",err)};back:=make([]byte,n);if RootSave(v,back)!=n||!bytes.Equal(back,wire){t.Fatal("two blobs wire round trip")}
 j:=RootToJsonMeasure(v);js:=make([]byte,j);if j<=0||RootToJson(v,js)!=j{t.Fatalf("two blobs ToJson %d",j)};if m,err:=RootToJsonMeasureReason(v);m!=j||err!=nil{t.Fatalf("two blobs ToJsonMeasureReason %d %v",m,err)}
 var f RootBuilder;if !f.Init(){t.Fatal("init")};defer f.Shutdown();var fr TableReport;if !RootFromJson(&f,js,&fr)||fr.Err()!=nil||!f.Lock(){t.Fatalf("two blobs FromJson %+v",fr)};js2:=make([]byte,j);if RootToJson(f.AsConst(),js2)!=j||!bytes.Equal(js,js2){t.Fatal("two blobs text round trip")}
}
`)
	if err != nil {
		t.Fatalf("writers probe: %v\n%s", err, out)
	}
}

// TestBlobNamedTwiceAmplification is the hostile case: 4096 slots naming one
// 64 KiB blob. The builder's Save refuses it; a wire built by hand with the
// runtime's TableWriter (the numbering and node table Save would write) is
// refused by the strict read, accepted by the tolerant Load into a region that
// counts the blob ONCE, and refused by every writer. Nothing copies the blob
// per slot: the calls allocate far less than one copy per slot would.
func TestBlobNamedTwiceAmplification(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	out, err := runGeneratedResult(t, `package probe
table Item { b *bytes }
table Root { items [..4096]Item }
`, blobNamedTwiceProbeHead+`
func hostile(t *testing.T,root *Root,a *TableArena)[]byte{
 n,ok:=tableNumber(unsafe.Pointer(root),RootTableType(),a);if !ok{t.Fatal("numbering")};defer n.Release()
 write:=func(buf []byte,measure bool)int64{ids:=TableIds{Numbering:&n};var m TableWriter;w:=TableWriter{Buffer:buf,Measuring:measure,Ids:&ids};w.Put8(1);if !RootTableType().SaveBody(&w,unsafe.Pointer(root)){t.Fatal("root body")};w.Offset--
  w.Id(0xffffffffffffffff);w.Put8(12);payload:=tableLebBytes(uint64(len(n.entries)-1));for i:=1;i<len(n.entries);i++{e:=&n.entries[i];payload+=tableLebBytes(ids.Ref(e.id));size:=tableNodeMeasure(&m,&ids,e);payload+=tableLebBytes(uint64(size))+size}
  w.PutLeb(uint64(payload));w.PutLeb(uint64(len(n.entries)-1));for i:=1;i<len(n.entries);i++{e:=&n.entries[i];w.Id(e.id);size:=tableNodeMeasure(&m,&ids,e);w.PutLeb(uint64(size));if measure{w.Advance(size)}else if !tableNodeWrite(&w,e){t.Fatal("node")}}
  w.Put8(0);w.Trailer();if w.Overflow{t.Fatal("overflow")};return w.Offset}
 size:=write(nil,true);buf:=make([]byte,size);if write(buf,false)!=size{t.Fatal("hand-built wire")};return buf
}
func TestAmplification(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();r:=b.GetRoot();data,ref:=b.Main.AllocBytes(65536);for i:=range data{data[i]=byte(i*7)};r.ItemsCount=4096;for i:=range 4096{r.Items[i].B=ref}
 if n,err:=RootMeasureReason(r,&b.Arena);n!= -1{t.Fatalf("builder MeasureReason %d",n)}else{shared(t,"builder MeasureReason",err)};if RootSave(r,make([]byte,1<<17),&b.Arena)!= -1{t.Fatal("builder Save wrote the shared blob")}
 wire:=hostile(t,r,&b.Arena);if len(wire)>1<<17{t.Fatalf("hand-built wire is %d bytes",len(wire))};t.Logf("hostile wire %d bytes",len(wire))
 if v,err:=RootReadStrict(wire);v!=nil{t.Fatal("ReadStrict accepted it")}else{shared(t,"ReadStrict",err)}
 size:=RootLoadMeasure(wire);if size<=0||size>=1<<20{t.Fatalf("LoadMeasure %d: the blob must count once",size)};reg:=region(size);js:=make([]byte,1<<20)
 var before,after runtime.MemStats;runtime.GC();runtime.ReadMemStats(&before)
 var report TableReport;v:=RootLoadTolerant(reg,wire,&report);jm:=RootToJsonMeasure(v);jr,jerr:=RootToJsonMeasureReason(v);jw:=RootToJson(v,js);mn,merr:=RootMeasureReason(v)
 runtime.ReadMemStats(&after);if grew:=after.TotalAlloc-before.TotalAlloc;grew>1<<20{t.Fatalf("Load, ToJson and Measure allocated %d bytes: a per-slot copy",grew)};t.Logf("Load, ToJson and Measure allocated %d bytes",after.TotalAlloc-before.TotalAlloc)
 runtime.GC();runtime.ReadMemStats(&before);var lb RootBuilder;lbok:=lb.Init();var lr TableReport;lbload:=RootLoadBuilderTolerant(&lb,wire,&lr);runtime.ReadMemStats(&after);defer lb.Shutdown()
 if grew:=after.TotalAlloc-before.TotalAlloc;grew>8<<20{t.Fatalf("LoadBuilder allocated %d bytes: a per-slot copy is 256 MiB",grew)};t.Logf("LoadBuilder allocated %d bytes",after.TotalAlloc-before.TotalAlloc)
 if v==nil||report.Malformed||report.BlobShared!=4095{t.Fatalf("tolerant Load: %v %+v",v==nil,report)};shared(t,"Load report",report.Err())
 first:=TableBytesAt(&v.Items[0].B);if first.Length!=65536||!bytes.Equal(first.Data,data){t.Fatal("blob value")};for i:=range 4096{if x:=TableBytesAt(&v.Items[i].B);x.Length!=65536||&x.Data[0]!=&first.Data[0]{t.Fatalf("slot %d does not name the one node",i)}}
 if jm!= -1||jr!= -1||jw!= -1{t.Fatalf("ToJson %d %d %d",jm,jr,jw)};shared(t,"ToJsonMeasureReason",jerr);if mn!= -1{t.Fatalf("MeasureReason %d",mn)};shared(t,"MeasureReason",merr)
 if !lbok||!lbload||lr.BlobShared!=4095{t.Fatalf("tolerant LoadBuilder: %v %v %+v",lbok,lbload,lr)};shared(t,"LoadBuilder report",lr.Err())
 lroot:=lb.GetRoot();lf:=TableBytesAt(&lroot.Items[0].B,&lb.Arena);if lf.Length!=65536{t.Fatal("builder blob")};for i:=range 4096{if x:=TableBytesAt(&lroot.Items[i].B,&lb.Arena);&x.Data[0]!=&lf.Data[0]{t.Fatalf("builder slot %d copied the blob",i)}}
}
`, "-v")
	if err != nil {
		t.Fatalf("amplification probe: %v\n%s", err, out)
	}
	if testing.Verbose() {
		t.Logf("%s", out)
	}
}

// blobNamedTwiceMessageHead is the message-form probes' head: a body written
// by the region writer, or by hand with the writer's own steps and no
// blob_shared check (the hostile body), and a read through the region reader.
const blobNamedTwiceMessageHead = blobNamedTwiceProbeHead + `
func messageBody(t *testing.T,root *Root,a *TableArena,check bool)[]byte{
 write:=func(buf []byte,measure bool)int64{w:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:buf,Measuring:measure}}
  if check {if !tableMessageRegionWrite(&w,unsafe.Pointer(root),RootTableType(),a){t.Fatalf("message writer refused: %v",w.numbering.reason)}} else {
   var ok bool;w.numbering,ok=tableNumber(unsafe.Pointer(root),RootTableType(),a);if !ok{t.Fatal("numbering")};n:=&w.numbering;defer n.Release();w.Numbering=n;w.IndexBits=tableMessageBits(uint64(len(n.entries)));if w.IndexBits<1{w.IndexBits=1}
   slot:=uint64(0);for i:=range tableMessageEntries{if tableMessageEntries[i].Id==0xffffffffffffffff{slot=uint64(i+1);break}};w.Put(slot,TableMessageRefBitsHere);w.Put(uint64(len(n.entries)-1),32)
   for i:=1;i<len(n.entries);i++{node:=&n.entries[i];w.Put(tableMessageNameSlot(node.id),TableMessageRefBitsHere);if node.info!=nil{if !node.info.SaveMessageBody(&w,node.node){t.Fatal("node body")};continue};length:=uint64(*(*uint32)(node.node));w.Put(length,32);w.Raw(unsafe.Slice((*byte)(unsafe.Add(node.node,8)),int(length)))}
   if !RootTableType().SaveMessageBody(&w,unsafe.Pointer(root)){t.Fatal("root body")}}
  w.Align();if w.Overflow{t.Fatal("overflow")};return w.Bits/8}
 size:=write(nil,true);wire:=make([]byte,2+size);wire[0]=2;wire[1]=0;if write(wire[2:],false)!=size{t.Fatal("message body")};return wire
}
func messageRead(t *testing.T,wire []byte)(*Root,TableReport){
 ann:=make([]byte,AnnounceMeasure());Announce(ann);var v TableVocabulary;v.Init(make([]TableMessageEntry,TableMessageEntriesHere));var ar TableReport;if !AnnounceRead(&v,ann,&ar){t.Fatalf("announce %+v",ar)}
 size:=tableMessageRegionMeasure(&v,wire,RootTableType(),3);if size<=0{t.Fatalf("message measure %d",size)};reg:=region(size)
 var report TableReport;r,count:=tableMessageBatchOpen(&v,wire,&report);if count!=1{t.Fatalf("batch %d %+v",count,report)};used:=int64(0);out,ok:=tableMessageRegionInto(&r,RootTableType(),3,reg,&used);if !ok||out==nil||r.Report.Malformed{t.Fatalf("message read %v %+v",ok,r.Report)};return (*Root)(out),r.Report
}
`

// TestBlobNamedTwiceMessageForm pins the message form's region writer and
// reader to the same ruling (docs/SPEC-TABLES.md §2.5, §16.7): the writer
// refuses a graph whose two slots name one blob by name, blob_shared, and the
// reader keeps a hand-built body that does it, both slots naming the one
// decoded node, and counts it in TableReport.BlobShared. The same value with
// two blobs writes and reads with no count.
func TestBlobNamedTwiceMessageForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	out, err := runGeneratedResult(t, blobNamedTwiceSeed24Unit, blobNamedTwiceMessageHead+`
func TestMessage(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();r:=b.GetRoot();data,ref:=b.Main.AllocBytes(3);copy(data,"abc");r.F2=ref;r.U0.Type=U0TypePtr;r.U0.Ptr().Value=ref
 w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};if tableMessageRegionWrite(&w,unsafe.Pointer(r),RootTableType(),&b.Arena){t.Fatal("message writer emitted a blob named twice")};if w.numbering.reason!=TableRefuseBlobShared||w.numbering.reason.Error()!="blob_shared"{t.Fatalf("message writer reason %v, want blob_shared",w.numbering.reason)}
 v,report:=messageRead(t,messageBody(t,r,&b.Arena,false));if report.BlobShared!=1{t.Fatalf("message reader report %+v, want BlobShared 1",report)};shared(t,"message report",report.Err())
 a,c:=TableBytesAt(&v.F2),TableBytesAt(&v.U0.Ptr().Value);if a.Length!=3||string(a.Data)!="abc"||&a.Data[0]!=&c.Data[0]{t.Fatal("the two slots do not name the one decoded node")}
 var s RootBuilder;if !s.Init(){t.Fatal("init")};defer s.Shutdown();q:=s.GetRoot();d1,r1:=s.Main.AllocBytes(3);copy(d1,"abc");d2,r2:=s.Main.AllocBytes(3);copy(d2,"xyz");q.F2=r1;q.U0.Type=U0TypePtr;q.U0.Ptr().Value=r2
 v2,report2:=messageRead(t,messageBody(t,q,&s.Arena,true));if report2.BlobShared!=0||report2.Err()!=nil{t.Fatalf("two blobs message report %+v",report2)};if string(TableBytesAt(&v2.F2).Data)!="abc"||string(TableBytesAt(&v2.U0.Ptr().Value).Data)!="xyz"{t.Fatal("two blobs message values")}
}
`)
	if err != nil {
		t.Fatalf("message-form probe: %v\n%s", err, out)
	}
	// A table node's record is read before the blob record it names, so the
	// reader meets the blob's entry after Resolve has marked it.
	out, err = runGeneratedResult(t, `package probe
table Item { b *bytes }
table Root {
 p *Item
 q *bytes
}
`, blobNamedTwiceMessageHead+`
func TestMessageNodeFirst(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();r:=b.GetRoot();data,ref:=b.Main.AllocBytes(3);copy(data,"abc");ip,iref:=b.Main.Alloc(int64(ItemTableType().Size));if ip==nil{t.Fatal("alloc")};ItemTableType().Reset(ip);item:=(*Item)(ip);item.B=ref;r.P=iref;r.Q=ref
 w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};if tableMessageRegionWrite(&w,unsafe.Pointer(r),RootTableType(),&b.Arena)||w.numbering.reason!=TableRefuseBlobShared{t.Fatalf("message writer reason %v, want blob_shared",w.numbering.reason)}
 v,report:=messageRead(t,messageBody(t,r,&b.Arena,false));if report.BlobShared!=1{t.Fatalf("message reader report %+v, want BlobShared 1",report)}
 a,c:=TableBytesAt(&v.Q),TableBytesAt(&ItemAt(&v.P).B);if a.Length!=3||string(a.Data)!="abc"||&a.Data[0]!=&c.Data[0]{t.Fatal("the two slots do not name the one decoded node")}
}
`)
	if err != nil {
		t.Fatalf("message-form node-first probe: %v\n%s", err, out)
	}
}
