package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"strings"
	"testing"
)

func TestRetainMatchesIndependentRewrite(t *testing.T) {
	old := `package probe
enum Key { A, B }
fixed table Child { n int32 }
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
	source := fmt.Sprintf(`package probe
import("testing";"bytes";"unsafe")
func TestRetain(t *testing.T){
 wire:=%#v;want:=%#v
 n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainID,1024)};var report TableReport
 v:=RootLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed||report.Unknown!=%d||report.Retained!=%d||report.RetainLost!=%d{t.Fatalf("report: %%+v stored %%d",report,retain.Count)}
 size:=RootMeasureRetain(v,&retain);if size!=int64(len(want)){t.Fatalf("measure %%d want %%d",size,len(want))};out:=make([]byte,size)
 if RootSaveRetain(v,&retain,out,nil)!=-1{t.Fatal("nil report")}
 report=TableReport{};if RootSaveRetain(v,&retain,out,&report)!=size||!bytes.Equal(out,want)||report.RetainLost!=0{for i:=range out{if i>=len(want)||out[i]!=want[i]{t.Fatalf("rewrite byte %%d, report %%+v",i,report)}};t.Fatalf("rewrite %%+v",report)}
 report=TableReport{};if RootSaveRetain(v,&retain,out,&report)!=size||!bytes.Equal(out,want)||report.RetainLost!=0{t.Fatalf("second save %%+v",report)}
 if allocations:=testing.AllocsPerRun(20,func(){report=TableReport{};if RootLoadRetain(region,wire,&retain,&report)==nil{panic("load")}});allocations!=0{t.Fatalf("retaining load allocated %%v",allocations)}
 retain.Bytes=nil;report=TableReport{};v=RootLoadRetain(region,wire,&retain,&report);if v==nil||report.Malformed||report.Retained!=0||report.RetainLost!=%d{t.Fatalf("zero capacity: %%+v",report)}
}
`, wire, want, report.Unknown, report.Retained, report.RetainLost, report.Retained+report.RetainLost)
	runGenerated(t, old, source)
}

func TestRetainReplacementAndShapeChanges(t *testing.T) {
	runGenerated(t, `package probe
enum Key { A, B }
fixed table Child { n int32 }
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
  n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)];retain:=TableRetain{Bytes:make([]byte,2048),Ids:make([]TableRetainID,32)};var r TableReport
  root:=RootLoadRetain(region,wire,&retain,&r);if root==nil||r.Malformed||r.Retained!=tc.retained||int32(retain.Count)!=tc.count||r.RetainLost!=0{t.Fatalf("load %+v count %d",r,retain.Count)}
  if tc.edit{root.ArrayCount=1};out:=make([]byte,RootMeasureRetain(root,&retain));r=TableReport{};if RootSaveRetain(root,&retain,out,&r)!=int64(len(out))||r.RetainLost!=tc.lost{t.Fatalf("save %+v",r)}
  size:=RootLoadMeasure(out);raw2:=make([]byte,size+63);off2:=(-uintptr(unsafe.Pointer(&raw2[0])))&63;r=TableReport{};if RootLoad(raw2[off2:off2+uintptr(size)],out,&r)==nil||r.Malformed||r.Unknown!=tc.count-tc.lost{t.Fatalf("retained contents %+v",r)}
 })}
}
`)
}

func TestRetainFileCapacityBeforeExpansion(t *testing.T) {
	runGenerated(t, `package probe
 fixed table Child { n int32 }
 table Root { child *Child }
 `, `package probe
 import("testing";"unsafe")
 func TestFileCapacity(t *testing.T){
  body:=append([]byte{6,100},make([]byte,100)...)
  probe:=tableRetainIn{r:TableReader{Buffer:body},w:TableWriter{Measuring:true},limit:16}
  if probe.content(14,int64(len(body)),0)||probe.r.Offset!=2{t.Fatalf("count expanded past available resolved capacity at %d",probe.r.Offset)}
  var root Root;store:=TableRetain{Bytes:make([]byte,42),Ids:make([]TableRetainID,8)};store.reset(unsafe.Pointer(&root),[]TableNodeDirEntry{{TypeID:RootTableType().ID}})
  wire:=append([]byte{byte(len(body))},body...);var report TableReport;r:=TableRetainReader{tableRetainPlainReader:TableReader{Buffer:wire,Report:&report},Retain:&store,Path:tableRetainPath{at:unsafe.Pointer(&root),node:1}}
  if !r.capture(123,14,"T")||report.Malformed||report.RetainLost!=1||report.Retained!=0||store.Used!=0{t.Fatalf("whole-record capacity %+v",report)}
 }
 `)
}

// TestRetainedFloatIsTestedLikeAKnownOne is ruling 3 of 2026-10-27
// (docs/SPEC-TABLES.md §3.3, §6.6): a retaining read meets an unknown entry of
// a float kind and tests its raw bits like a known field's. A NaN or an
// infinity refuses the read as non_finite_float, named <Table>.0x<id>, and is
// not retained; a finite one is retained as before.
func TestRetainedFloatIsTestedLikeAKnownOne(t *testing.T) {
	runGenerated(t, `package probe
 fixed table Child { n int32 }
 table Root { child *Child }
 `, `package probe
 import("encoding/binary";"testing";"unsafe")
 func TestRetainedFloat(t *testing.T){
  for _,c:=range []struct{kind uint8;payload []byte;finite bool}{
   {10,binary.LittleEndian.AppendUint32(nil,0x3fc00000),true},
   {10,binary.LittleEndian.AppendUint32(nil,0x7f800001),false},
   {11,binary.LittleEndian.AppendUint64(nil,0x8000000000000000),true},
   {11,binary.LittleEndian.AppendUint64(nil,0xfff0000000000000),false},
  }{
   var root Root;store:=TableRetain{Bytes:make([]byte,256),Ids:make([]TableRetainID,8)};store.reset(unsafe.Pointer(&root),[]TableNodeDirEntry{{TypeID:RootTableType().ID}})
   var report TableReport;r:=TableRetainReader{tableRetainPlainReader:TableReader{Buffer:c.payload,Report:&report},Retain:&store,Path:tableRetainPath{at:unsafe.Pointer(&root),node:1}}
   if !r.capture(0x123,c.kind,"Root")||report.Malformed{t.Fatalf("kind %d: the capture failed framing %+v",c.kind,report)}
   if c.finite&&(report.NonFiniteFloat!=""||report.Retained!=1){t.Errorf("kind %d finite: %+v",c.kind,report)}
   if !c.finite&&(report.NonFiniteFloat!="Root.0x0000000000000123"||report.Retained!=0||store.Used!=0){t.Errorf("kind %d non-finite: %+v",c.kind,report)}
  }
 }
 `)
}

func TestRetainSourceSeamsFailClosed(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"missing", func() { tableSourceReplace("one", "missing", "other", 1) }},
		{"duplicate", func() { tableSourceReplace("one one", "one", "other", 1) }},
		{"span", func() { tableSourceSpan("end start", "start", "end") }},
		{"function", func() { tableSourceFunction("func missing() {", "func missing(") }},
		{"rewrite", func() { tableSourceRewriter("one", "absent", "two") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("source drift did not panic")
				}
			}()
			tc.call()
		})
	}
}

func TestRetainUnknownNodeRecord(t *testing.T) {
	schema, err := os.ReadFile("../../../test/tables/RT1.schema")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile("../../../testdata/wire/tables/retain_rt3.bin")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), fmt.Sprintf(`package tblrt1
 import("testing";"unsafe")
 func TestUnknownNode(t *testing.T){wire:=[]byte{%s};size:=NodeLoadMeasure(wire);if size<0{t.Fatal("measure")};raw:=make([]byte,size+16);offset:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[offset:offset+uintptr(size)];store:=TableRetain{Bytes:make([]byte,4096),Ids:make([]TableRetainID,128)};report:=TableReport{Clamped:3};value:=NodeLoadRetain(region,wire,&store,&report);if value==nil||value.Head!=0||report.RetainLost!=1||report.Unknown!=1||report.Malformed||report.Clamped!=3{t.Fatalf("unplaceable node retention: %%+v",report)}}
 `, byteLiterals(wire)))
}
