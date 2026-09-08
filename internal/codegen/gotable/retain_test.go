package gotable

import (
	"bytes"
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

func TestRetainFileCapacityBeforeExpansion(t *testing.T) {
	runGenerated(t, `package probe
 table Child { n int32 }
 table Root { child *Child }
 `, `package probe
 import("testing";"unsafe")
 func TestFileCapacity(t *testing.T){
  body:=append([]byte{6,100},make([]byte,100)...)
  probe:=tableRetainIn{r:TableReader{Buffer:body},w:TableWriter{Measuring:true},limit:16}
  if probe.content(14,int64(len(body)),0)||probe.r.Offset!=2{t.Fatalf("count expanded past available resolved capacity at %d",probe.r.Offset)}
  var root Root;store:=TableRetain{Bytes:make([]byte,42),Ids:make([]TableRetainId,8)};store.reset(unsafe.Pointer(&root),[]TableNodeDirEntry{{TypeId:RootTableType().Id}})
  wire:=append([]byte{byte(len(body))},body...);var report TableReport;r:=TableRetainReader{tableRetainPlainReader:TableReader{Buffer:wire,Report:&report},Retain:&store,Path:tableRetainPath{at:unsafe.Pointer(&root),node:1}}
  if !r.capture(123,14)||report.Malformed||report.RetainLost!=1||report.Retained!=0||store.Used!=0{t.Fatalf("whole-record capacity %+v",report)}
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
 func TestUnknownNode(t *testing.T){wire:=[]byte{%s};size:=NodeLoadMeasure(wire);if size<0{t.Fatal("measure")};raw:=make([]byte,size+16);offset:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[offset:offset+uintptr(size)];store:=TableRetain{Bytes:make([]byte,4096),Ids:make([]TableRetainId,128)};report:=TableReport{Clamped:3};value:=NodeLoadRetain(region,wire,&store,&report);if value==nil||value.Head!=0||report.RetainLost!=1||report.Unknown!=1||report.Malformed||report.Clamped!=3{t.Fatalf("unplaceable node retention: %%+v",report)}}
 `, byteLiterals(wire)))
}

// Expected rewrites are constructed directly, independently of the message
// decoder: dropping an oversized key must not expose or retain its value.
func TestRetainMessageMapKeyProbe(t *testing.T) {
	const old = `package probe
 table Item { number int32 }
 table Root { names map[string(2)]Item }
 `
	newer := strings.Replace(strings.Replace(old, "string(2)", "string(8)", 1), "number int32", "number int32\nextra int32", 1)
	sender, reader := tabletext.NewModel(unitFrom(t, newer)), tabletext.NewModel(unitFrom(t, old))
	var cases strings.Builder
	for _, tc := range []struct {
		name, input, output string
		replace, malformed  bool
	}{
		{"dropped", `{"names":{"long":{"number":1,"extra":7}}}`, `{}`, false, false},
		{"replacement", `{"names":{"long":{"number":1,"extra":7}}}`, `{"names":{"ok":{"number":2}}}`, true, false},
		{"no alias", `{"names":{"lo":{"number":3},"long":{"number":1,"extra":7}}}`, `{"names":{"lo":{"number":3}}}`, false, false},
		{"invalid oversized key", `{"names":{"long":{"number":1,"extra":7}}}`, `{}`, false, true},
	} {
		source, expected := sender.New(sender.Lookup("Root")), reader.New(reader.Lookup("Root"))
		var report tabletext.Report
		if !sender.Read(source, []byte(tc.input), &report) || !reader.Read(expected, []byte(tc.output), &report) || !report.Silent() {
			t.Fatal(report)
		}
		if tc.replace {
			next := sender.New(sender.Lookup("Root"))
			if !sender.Read(next, []byte(tc.output), &report) || !report.Silent() {
				t.Fatal(report)
			}
			source.Fields = append(source.Fields, next.Fields[0])
		}
		batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
		if err != nil {
			t.Fatal(err)
		}
		if tc.malformed {
			if bytes.Count(batch, []byte("long")) != 1 {
				t.Fatal("key bytes not found")
			}
			batch = bytes.Replace(batch, []byte("long"), []byte{0xff, 'o', 'n', 'g'}, 1)
		}
		want, err := tablewire.Encode(reader, expected)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "{name:%q, message:%#v, want:%#v, malformed:%v},\n", tc.name, batch, want, tc.malformed)
	}
	runGenerated(t, old, fmt.Sprintf(`package probe
import("bytes";"testing";"unsafe")
func TestMapKeys(t *testing.T){
 announcement:=%#v
 for _,tc:=range []struct{name string;message,want []byte;malformed bool}{%s}{t.Run(tc.name,func(t *testing.T){
  vocabulary:=TableVocabulary{};vocabulary.Init(make([]TableMessageEntry,128));var report TableReport
  if !AnnounceRead(&vocabulary,announcement,&report){t.Fatal(report)}
  size:=RootLoadMessagesMeasure(&vocabulary,tc.message);if size<0{t.Fatal("measure")}
  raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)]
  for _,retaining:=range []bool{false,true}{
   roots:=make([]*Root,1);stores:=[]TableRetain{{Bytes:make([]byte,8192),Ids:make([]TableRetainId,128)}};report=TableReport{}
   var count int64;var ok bool
   if retaining{count,ok=RootLoadRetainMessages(roots,region,&vocabulary,tc.message,stores,&report)}else{count,ok=RootLoadMessages(roots,region,&vocabulary,tc.message,&report)}
   if tc.malformed{if count!=0||ok||!report.Malformed{t.Fatalf("accepted invalid key: %%d %%v %%+v",count,ok,report)};continue}
   if count!=1||!ok||report.Malformed||report.Clamped!=1||report.Unknown!=0||report.Retained!=0||report.RetainLost!=0||report.Duplicate!=0{t.Fatalf("load: %%d %%v %%+v",count,ok,report)}
   out:=make([]byte,8192);var n int64
   if retaining{n=RootSaveRetain(roots[0],&stores[0],out,&report)}else{n=RootSave(roots[0],out)}
   if n!=int64(len(tc.want))||!bytes.Equal(out[:n],tc.want)||report.RetainLost!=0{t.Fatalf("rewrite: %%d %%+v",n,report)}
  }
 })}
}
`, tablewire.Announce(sender.Unit), cases.String()))
}

func TestRetainMessageMapRepeatedKeyKeepsWidening(t *testing.T) {
	const schema = `package probe
 table Root { names map[uint32]int32
 narrow map[uint8]int32 }
 `
	m := tabletext.NewModel(unitFrom(t, schema))
	source := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(source, []byte(`{"names":{"2":7},"narrow":{"1":0}}`), &report) || !report.Silent() {
		t.Fatal(report)
	}
	entry := source.Fields[0].Entries[0].Tab
	key := source.Fields[1].Entries[0].Tab.Fields[0]
	entry.Fields = append([]tabletext.Field{key}, entry.Fields...)
	source.Fields[1].Entries = nil
	source.Fields[1].Count = 0
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{source})
	if err != nil {
		t.Fatal(err)
	}
	expected := m.New(m.Lookup("Root"))
	if !m.Read(expected, []byte(`{"names":{"2":7}}`), &report) || !report.Silent() {
		t.Fatal(report)
	}
	want, err := tablewire.Encode(m, expected)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, schema, fmt.Sprintf(`package probe
import("bytes";"testing";"unsafe")
func TestRepeatedKey(t *testing.T){
 message:=%#v;announcement:=%#v;want:=%#v
 vocabulary:=TableVocabulary{};vocabulary.Init(make([]TableMessageEntry,128));var report TableReport
 if !AnnounceRead(&vocabulary,announcement,&report){t.Fatal(report)}
 size:=RootLoadMessagesMeasure(&vocabulary,message);if size<0{t.Fatal("measure")}
 raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)]
 for _,retaining:=range []bool{false,true}{
  report=TableReport{};roots:=make([]*Root,1);stores:=[]TableRetain{{Bytes:make([]byte,8192),Ids:make([]TableRetainId,128)}}
  var n int64;var ok bool
  if retaining{n,ok=RootLoadRetainMessages(roots,region,&vocabulary,message,stores,&report)}else{n,ok=RootLoadMessages(roots,region,&vocabulary,message,&report)}
  if n!=1||!ok||report.Malformed||report.Widened!=1{t.Fatalf("load: %%d %%v %%+v",n,ok,report)}
  out:=make([]byte,8192);if retaining{n=RootSaveRetain(roots[0],&stores[0],out,&report)}else{n=RootSave(roots[0],out)}
  if n!=int64(len(want))||!bytes.Equal(out[:n],want){t.Fatalf("rewrite: %%d %%+v",n,report)}
 }
}
`, batch, tablewire.Announce(m.Unit), want))
}
