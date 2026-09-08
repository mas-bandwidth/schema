package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strings"
	"testing"
)

func TestRetainMatchesIndependentRewrite(t *testing.T) {
	old := `package probe
enum Key { A, B }
table Child { n int32 }
union Inner { child Child }
union Choice { child Child
 nested [..2]Inner
 empty }
table Root { link *Child
 direct Inner
 child Child
 optional ?Child
 array [..3]Child
 list []Child
 choices [..2]Choice
 keyed [Key]Child
 mapping map[int32]Child
 choice Choice }
`
	newer := strings.Replace(old, "table Child { n int32 }", "table Child { n int32\n extra string(16) }", 1)
	newer = strings.Replace(newer, " choice Choice }", " choice Choice\n future Choice }", 1)
	model := tabletext.NewModel(unitFrom(t, newer))
	v := model.New(model.Lookup("Root"))
	var report tabletext.Report
	text := `{"link":{"extra":"node"},"child":{"extra":"child"},"optional":{"extra":"optional"},"array":[{"extra":"array0"},{"n":3,"extra":"array1"}],"list":[{"extra":"list"}],"choices":[{"child":{"extra":"arm0"}},{"nested":[{"child":{"extra":"nested0"}},{"child":{"extra":"nested1"}}]}],"keyed":{"A":{"extra":"keyA"},"B":{"extra":"keyB"}},"mapping":{"-2":{"extra":"map"}},"choice":{"child":{"extra":"arm"}},"future":{"child":{"extra":"unknown union"}}}`
	if !model.Read(v, []byte(text), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}
	reader := tabletext.NewModel(unitFrom(t, old))
	value := reader.New(reader.Lookup("Root"))
	store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	report = tabletext.Report{}
	ok, err := tablewire.DecodeRetain(reader, value, wire, store, &report)
	if !ok || err != nil || report.Malformed {
		t.Fatalf("oracle read %v %v %+v", ok, err, report)
	}
	want, err := tablewire.EncodeRetain(reader, value, store, &report)
	if err != nil {
		t.Fatal(err)
	}
	message, err := tablewire.EncodeMessages(model, []*tabletext.Instance{v})
	if err != nil {
		t.Fatal(err)
	}
	announcement := tablewire.Announce(model.Unit)
	vocabulary := &tablewire.Vocabulary{}
	if err := vocabulary.AnnounceRead(announcement, &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	var messageReport tabletext.Report
	if count, ok, err := tablewire.DecodeRetainMessages(reader, []*tabletext.Instance{value}, message, vocabulary, []*tablewire.Retain{store}, &messageReport); count != 1 || !ok || err != nil || messageReport.Malformed {
		t.Fatalf("message oracle %d %v %v %+v", count, ok, err, messageReport)
	}
	messageWant, err := tablewire.EncodeRetain(reader, value, store, &messageReport)
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`package probe
import("testing";"bytes";"unsafe")
func TestRetain(t *testing.T){
 wire:=%#v;want:=%#v
 n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainId,1024)};var report TableReport
 v:=RootLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed||report.Unknown!=%d||report.Retained!=%d||report.RetainLost!=%d{t.Fatalf("report: %%+v stored %%d",report,retain.Count)}
 size:=RootMeasureRetain(v,&retain);if size!=int64(len(want)){t.Fatalf("measure %%d want %%d",size,len(want))};out:=make([]byte,size)
 if RootSaveRetain(v,&retain,out,nil)!=-1{t.Fatal("nil report")}
 report=TableReport{};if RootSaveRetain(v,&retain,out,&report)!=size||!bytes.Equal(out,want)||report.RetainLost!=0{for i:=range out{if i>=len(want)||out[i]!=want[i]{t.Fatalf("rewrite byte %%d, report %%+v",i,report)}};t.Fatalf("rewrite %%+v",report)}
 report=TableReport{};if RootSaveRetain(v,&retain,out,&report)!=size||!bytes.Equal(out,want)||report.RetainLost!=0{t.Fatalf("second save %%+v",report)}
 if allocations:=testing.AllocsPerRun(20,func(){report=TableReport{};if RootLoadRetain(region,wire,&retain,&report)==nil{panic("load")}});allocations!=0{t.Fatalf("retaining load allocated %%v",allocations)}
 retain.Bytes=nil;report=TableReport{};v=RootLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed||report.Retained!=0||report.RetainLost!=%d{t.Fatalf("zero capacity: %%+v",report)}
}
func TestRetainMessage(t *testing.T){
 announcement:=%#v;message:=%#v;want:=%#v
 vocabulary:=TableVocabulary{};vocabulary.Init(make([]TableMessageEntry,1024));var report TableReport
 if !AnnounceRead(&vocabulary,announcement,&report){t.Fatal(report)}
 size:=RootLoadMessagesMeasure(&vocabulary,message);if size<0{t.Fatal("message measure")};raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)]
 plainReport:=TableReport{};var plainRoots [1]*Root
 if count,ok:=RootLoadMessages(plainRoots[:],region,&vocabulary,message,&plainReport);count!=1||!ok{t.Fatalf("ordinary message read %%d %%v %%+v",count,ok,plainReport)}
 stores:=[]TableRetain{{Bytes:make([]byte,1<<20),Ids:make([]TableRetainId,1024)}};roots:=make([]*Root,1)
 if count,ok:=RootLoadRetainMessages(roots,region,&vocabulary,message,stores,&report);count!=1||!ok||report.Malformed||report.RetainLost!=0{t.Fatalf("message read %%d %%v %%+v",count,ok,report)}
 n:=RootMeasureRetain(roots[0],&stores[0]);if n<0{t.Fatal("message retaining measure")};out:=make([]byte,n);report=TableReport{}
 if RootSaveRetain(roots[0],&stores[0],out,&report)!=n||report.RetainLost!=0||!bytes.Equal(out,want){t.Fatalf("message rewrite %%+v got %%x want %%x",report,out,want)}
 if allocations:=testing.AllocsPerRun(20,func(){RootLoadRetainMessages(roots,region,&vocabulary,message,stores,&report)});allocations!=0{t.Fatalf("message read allocated %%v",allocations)}
}
`, wire, want, report.Unknown, report.Retained, report.RetainLost, report.Retained+report.RetainLost, announcement, message, messageWant)
	runGenerated(t, old, source)
}

func TestRetainMessageBoundsBeforeExpansion(t *testing.T) {
	runGenerated(t, `package probe
table Child { n int32 }
table Root { child *Child }
`, `package probe
import("testing";"unsafe")
func TestCaptureBounds(t *testing.T){
 var root Root;store:=TableRetain{Bytes:make([]byte,8192),Ids:make([]TableRetainId,8)};store.reset(unsafe.Pointer(&root),[]TableNodeDirEntry{{TypeId:RootTableType().Id}})
 entry:=TableMessageEntry{Id:123,Shape:TableMessageShape{Kind:13,Bits:-1}};v:=TableVocabulary{Entries:[]TableMessageEntry{entry},Count:1,RefBits:1,Announced:true}
 for _,depth:=range []int{64,65}{
  bits:=make([]byte,32);w:=TableBitWriter{Buffer:bits};for i:=1;i<depth;i++{w.Put(1,1)};for i:=0;i<depth;i++{w.Put(0,1)}
  store.Used=0;store.Count=0;r:=TableRetainMessageReader{tableMessagePlainReader:TableMessageReader{Bits:TableBitReader{Buffer:bits},Vocabulary:&v},Retain:&store,Path:tableRetainPath{at:unsafe.Pointer(&root),node:1}}
  if !r.capture(entry)||r.Report.Malformed||r.Bits.Offset!=w.Bits{t.Fatalf("depth %d changed ordinary framing %+v",depth,r.Report)}
  if depth==64&&(r.Report.Retained!=1||r.Report.RetainLost!=0)||depth==65&&(r.Report.Retained!=0||r.Report.RetainLost!=1){t.Fatalf("depth %d %+v",depth,r.Report)}
 }
 store.Used=0;store.Count=0;store.Bytes=store.Bytes[:64]
 huge:=TableMessageEntry{Id:124,Shape:TableMessageShape{Kind:14,Min:1<<31,Max:1<<31,Bits:-1},Element:TableMessageShape{Kind:4,Packing:1,Bits:0}}
 r:=TableRetainMessageReader{tableMessagePlainReader:TableMessageReader{Vocabulary:&v},Retain:&store,Path:tableRetainPath{at:unsafe.Pointer(&root),node:1}}
 if !r.capture(huge)||r.Report.RetainLost!=1||r.Report.Retained!=0||r.Report.Malformed||store.Used!=0{t.Fatalf("zero-bit count %+v",r.Report)}
 quantized:=TableMessageEntry{Id:125,Shape:TableMessageShape{Kind:10,Packing:2,Bits:2,QCount:2,QMin:0,QDelta:1}}
 r.Report=TableReport{};r.Bits=TableBitReader{Buffer:[]byte{3}}
 if !r.capture(quantized)||r.Report.RetainLost!=1||r.Report.Malformed{t.Fatalf("invalid quantized index %+v",r.Report)}
}
`)
}

func TestRetainReplacementAndShapeChanges(t *testing.T) {
	runGenerated(t, `package probe
enum Key { A, B }
table Child { n int32 }
union Choice { branch Child
 empty }
table Root { child Child
 array [..2]Child
 keyed [Key]Child
 choice Choice
 entries map[int32]Child
 link *Child }
`, `package probe
import("testing";"hash/fnv";"encoding/binary";"unsafe")
func TestReplacement(t *testing.T){
 names:=[]string{"child","array","keyed","choice","entries","extra","branch","empty","A","B","key","value"}
 field:=func(ref,kind byte,payload ...byte)[]byte{return append([]byte{ref,kind},payload...)}
 join:=func(parts ...[]byte)[]byte{var out []byte;for _,p:=range parts{out=append(out,p...)};return out}
 unknown:=[]byte{6,6,99,0};empty:=[]byte{0};body:=func(p []byte)[]byte{return append([]byte{byte(len(p))},p...)}
 child:=field(1,13,body(unknown)...);two:=field(2,14,body(join([]byte{13,2},body(unknown),body(unknown)))...)
 keyed:=func(key byte,p []byte)[]byte{return field(3,16,body(join([]byte{13,1,key},body(p)))...)}
 arm:=field(4,15,join([]byte{7,13},body(unknown))...)
 entry:=func(p []byte)[]byte{return join([]byte{11,4,7,0,0,0},field(12,13,body(p)...),[]byte{0})}
 cases:=[]struct{name string;payload []byte;retained,count int32;edit bool;lost int32}{
 {"repeated child",join(child,field(1,13,1,0)),1,0,false,0},
 {"cleared array",join(two,field(2,14,2,13,0)),2,0,false,0},
 {"inert array",join(two,field(2,14,0)),2,2,false,0},
 {"keyed slots",join(keyed(9,unknown),keyed(10,unknown),keyed(9,empty)),2,1,false,0},
 {"switched union",join(arm,field(4,15,8,32,0)),1,0,false,0},
 {"duplicate map key",field(5,14,body(join([]byte{13,2},body(entry(unknown)),body(entry(empty))))...),1,0,false,0},
 {"shape edit",two,2,2,true,1},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  wire:=join([]byte{1},tc.payload,[]byte{0});for _,name:=range names{h:=fnv.New64a();h.Write([]byte(name));wire=binary.LittleEndian.AppendUint64(wire,h.Sum64())};wire=binary.LittleEndian.AppendUint64(wire,uint64(len(names)))
  n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)];retain:=TableRetain{Bytes:make([]byte,2048),Ids:make([]TableRetainId,32)};var r TableReport
  root:=RootLoadRetain(region,wire,&retain,&r);if root==nil||r.Malformed||r.Retained!=tc.retained||int32(retain.Count)!=tc.count||r.RetainLost!=0{t.Fatalf("load %+v count %d",r,retain.Count)}
  if tc.edit{root.ArrayCount=1};out:=make([]byte,RootMeasureRetain(root,&retain));r=TableReport{};if RootSaveRetain(root,&retain,out,&r)!=int64(len(out))||r.RetainLost!=tc.lost{t.Fatalf("save %+v",r)}
  size:=RootLoadMeasure(out);raw2:=make([]byte,size+63);off2:=(-uintptr(unsafe.Pointer(&raw2[0])))&63;r=TableReport{};if RootLoad(raw2[off2:off2+uintptr(size)],out,&r)==nil||r.Malformed||r.Unknown!=tc.count-tc.lost{t.Fatalf("retained contents %+v",r)}
 })}
}
`)
}
