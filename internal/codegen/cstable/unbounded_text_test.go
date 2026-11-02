package cstable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unboundedTextSchema = `package probe
table Root {
 names []string
 maybe [..3]?string
 bounded [..3]string
 optional []?string
 by_name map[string]int32
}
`

func TestUnboundedTextCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	long := strings.Repeat("x", 5000)
	independentInput := fmt.Sprintf(`{"names":["","%s","é😊"],"maybe":[null,"","%s"],"by_name":{"%sa":1,"%sb":2}}`, long, long, long, long)
	pin := unboundedGoWitness(t, independentInput)
	runCarryCS(t, unboundedTextSchema, []string{
		`{}`,
		fmt.Sprintf(`{"names":["","%s","é😊"],"maybe":[null,"","%s"],"by_name":{"%sa":1,"%sb":2}}`, long, long, long, long),
		fmt.Sprintf(`{"by_name":{"%sa":1,"%sa":3,"%sb":2}}`, long, long, long),
		`{"names":["x\u0000y"]}`, `{"by_name":{"x\u0000y":1}}`,
	}, fmt.Sprintf(`{var independent=new Root();if(!Schema.RootFromJson(independent,Encoding.UTF8.GetBytes(%s),new TableReport()))throw new Exception("Go witness JSON");byte[] output=new byte[Schema.RootMeasure(independent)];Schema.RootSave(independent,output);if(!output.AsSpan().SequenceEqual(Convert.FromHexString("%s")))throw new Exception("independent generated Go wire");}`, strconvQuoteCS(independentInput), pin), unboundedTextNativeProbe)
}

const unboundedTextNativeProbe = `unsafe {
 byte[] text=Encoding.UTF8.GetBytes(new string('x',5000)+"é😊");
 byte[] ka=Encoding.UTF8.GetBytes(new string('x',5000)+"a"), kb=Encoding.UTF8.GetBytes(new string('x',5000)+"b");
 var original=new Root();
 if(!Schema.RootNamesAppend(original,text)||!Schema.RootNamesAppend(original,ReadOnlySpan<byte>.Empty)||!Schema.RootMaybeAppendAbsent(original)||!Schema.RootMaybeAppend(original,ReadOnlySpan<byte>.Empty)||!Schema.RootMaybeAppend(original,text)||!Schema.RootBoundedAppend(original,text)||!Schema.RootOptionalAppendAbsent(original)||!Schema.RootOptionalAppend(original,text))throw new Exception("unbounded managed append");
 var maps=Schema.RootTableType().Fields[4];maps.EnsureCount(original,2);maps.SetCount(original,2);
 for(int i=0;i<2;i++){object entry=maps.GetChild(original,i);var key=maps.Table.Fields[0];byte[] bytes=i==0?ka:kb;key.EnsureCount(entry,bytes.Length);bytes.CopyTo(key.GetBuffer(entry),0);key.SetCount(entry,bytes.Length);maps.Table.Fields[1].SetRaw(entry,0,(ulong)i+1);}
 byte[] wire=new byte[Schema.RootMeasure(original)];if(Schema.RootSave(original,wire)!=wire.Length)throw new Exception("unbounded managed save");
 using(var fresh=new RootBuilder()){
  var row=fresh.GetRoot();var worker=fresh.CreateWorker();
  if(!Schema.RootNamesAppend(worker,row,text)||!Schema.RootNamesAppend(worker,row,ReadOnlySpan<byte>.Empty)||!Schema.RootMaybeAppendAbsent(worker,row)||!Schema.RootMaybeAppend(worker,row,ReadOnlySpan<byte>.Empty)||!Schema.RootMaybeAppend(worker,row,text)||!Schema.RootBoundedAppend(worker,row,text)||!Schema.RootOptionalAppendAbsent(worker,row)||!Schema.RootOptionalAppend(worker,row,text))throw new Exception("unbounded native append");
  var map=worker.Map<RootByNameEntryRow>(ref row->ByName,maps);
  var b=map.Insert(kb);var a=map.Insert(ka);if(a==null||b==null||map.Count!=2)throw new Exception("unbounded native map insert");a->Value=1;b->Value=2;
  if(map.Find(ka)!=a||map.Find(kb)!=b||map.Insert(ka)!=a||map.Count!=2)throw new Exception("unbounded full key compare");
  if(map.Insert(new byte[]{0})!=null||map.Insert(new byte[]{0xff})!=null||map.Count!=2)throw new Exception("invalid map key mutated count");
  if(Schema.RootMaybeAppend(worker,row,text)||Schema.RootNamesAppend(worker,row,new byte[]{0})||Schema.RootNamesAppend(worker,row,new byte[]{0xff})||row->Names.Count!=2)throw new Exception("invalid native append mutated count");
  if(Schema.RootMaybeTextAt(row,0,out bool present).Length!=0||present||Schema.RootMaybeTextAt(row,1,out present).Length!=0||!present||!Schema.RootMaybeTextAt(row,2,out present).SequenceEqual(text)||!present)throw new Exception("native unbounded optional distinction");
  byte[] authored=new byte[fresh.Measure()];if(fresh.Save(authored)!=wire.Length||!authored.AsSpan().SequenceEqual(wire))throw new Exception("unbounded authored bytes");
 }
 var type=Schema.RootTableType();long size=Schema.RootLoadMeasure(wire);
 if(size<=0)throw new Exception("unbounded load measure");
 void* memory=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((size+15)&~15L),16);
 try{
  var report=new TableReport();IntPtr ptr=Schema.TableWire.LoadRegion(type,(IntPtr)memory,size,wire,report);
  if(ptr==IntPtr.Zero||report.Malformed)throw new Exception("unbounded native load");
  Span<ulong> ids=stackalloc ulong[4096];Span<int> slots=stackalloc int[4096];
  byte[] saved=new byte[Schema.TableWire.SaveRegion(ptr,type,Span<byte>.Empty,ids,true)];
  if(Schema.TableWire.SaveRegion(ptr,type,saved,ids,false)!=wire.Length||!saved.AsSpan().SequenceEqual(wire))throw new Exception("unbounded native roundtrip");
  TableRetain retain=default;ptr=Schema.TableWire.LoadRetainRegion(type,(IntPtr)memory,size,wire,ref retain,new TableReport());
  if(ptr==IntPtr.Zero)throw new Exception("unbounded retained load");
  saved=new byte[Schema.TableWire.SaveRetainRegion(ptr,type,Span<byte>.Empty,ids,slots,ref retain,null,true)];
  if(Schema.TableWire.SaveRetainRegion(ptr,type,saved,ids,slots,ref retain,new TableReport(),false)!=wire.Length||!saved.AsSpan().SequenceEqual(wire))throw new Exception("unbounded retained roundtrip");
 }finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(memory);}
 byte[] managedCook=new byte[Schema.RootCookMeasure(original)];if(!Schema.RootCook(original,managedCook))throw new Exception("unbounded managed cook");
 using(var builder=new RootBuilder()){
  if(!builder.Load(wire,new TableReport()))throw new Exception("unbounded builder load");
  byte[] built=new byte[builder.Measure()];if(builder.Save(built)!=wire.Length||!built.AsSpan().SequenceEqual(wire))throw new Exception("unbounded builder save");
  byte[] cooked=new byte[builder.CookMeasure()];if(!builder.Cook(cooked)||!cooked.AsSpan().SequenceEqual(managedCook))throw new Exception("unbounded independent cooks");
  fixed(byte* p=cooked){if(!RootCook.Open(out var opened,(IntPtr)p,cooked.Length)||!Schema.RootNamesTextAt((RootRow*)(p+64),0,out bool present).SequenceEqual(text)||!present)throw new Exception("unbounded cook open");}
 }
 byte[] TextWire(byte[] payload,ulong fieldId){
  var frame=new System.Collections.Generic.List<byte>{1,1,14,(byte)payload.Length};frame.AddRange(payload);frame.Add(0);
  byte[] idBytes=new byte[8];System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(idBytes,fieldId);frame.AddRange(idBytes);frame.AddRange(new byte[]{1,0,0,0,0,0,0,0});return frame.ToArray();
 }
 foreach(byte[] element in new[]{new byte[]{0},new byte[]{0xff},new byte[]{0xc0,0x80}}){
  byte[] payload=new byte[element.Length+3];payload[0]=12;payload[1]=1;payload[2]=(byte)element.Length;element.CopyTo(payload,3);byte[] source=TextWire(payload,type.Fields[0].Id);
  var read=new Root();var r=new TableReport();if(Schema.RootLoad(read,source,r)||!r.Malformed)throw new Exception("unbounded wire invalid text accepted");
  long need=Schema.RootLoadMeasure(source);void* mem=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((need+15)&~15L),16);
  try{var nr=new TableReport();if(Schema.TableWire.LoadRegion(type,(IntPtr)mem,need,source,nr)!=IntPtr.Zero||!nr.Malformed)throw new Exception("unbounded native invalid text accepted");}finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(mem);}
 }
 foreach(string json in new[]{"{\"names\":[\"!\"]}","{\"by_name\":{\"!\":1}}"}){
  byte[] input=Encoding.UTF8.GetBytes(json);input[Array.IndexOf(input,(byte)'!')]=0xff;var badText=new Root();var report=new TableReport();
  string field=json.Contains("names")?"Root.names":"Root.by_name";
  if(Schema.RootFromJson(badText,input,report)||!report.Malformed||report.Reason!="string_not_utf8: "+field)throw new Exception("unbounded JSON invalid UTF-8 named refusal");
 }
 byte[] forgedCount=TextWire(new byte[]{12,127},type.Fields[0].Id);
 if(Schema.RootLoadMeasure(forgedCount,out TableRefuseReason countReason)!=-1||countReason!=TableRefuseReason.count_over_length)throw new Exception("unbounded allocation beyond framing");
 original.Names[0].Value[0]=0;byte[] untouched=new byte[wire.Length];Array.Fill(untouched,(byte)0xa5);
 if(Schema.RootMeasure(original)!=-1||Schema.RootSave(original,untouched)!=-1||Array.Exists(untouched,b=>b!=0xa5))throw new Exception("unbounded writer invalid text");
}`

func strconvQuoteCS(s string) string { b, _ := json.Marshal(s); return string(b) }
func unboundedGoWitness(t *testing.T, input string) string {
	t.Helper()
	u := unitFrom(t, unboundedTextSchema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	output := filepath.Join(dir, "wire.bin")
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import("os";"testing")
func TestUnboundedOracle(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var r TableReport
 if !RootFromJSONTolerant(&b,[]byte(%q),&r)||r.Clamped!=0||r.Malformed{t.Fatal("unbounded JSON",r)}
 saved:=make([]byte,RootMeasure(b.GetRoot(),&b.Arena));if RootSave(b.GetRoot(),saved,&b.Arena)!=int64(len(saved)){t.Fatal("save")}
 if err:=os.WriteFile(%q,saved,0600);err!=nil{t.Fatal(err)}
}
`, input, output))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestUnboundedOracle$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go unbounded oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data)
}
