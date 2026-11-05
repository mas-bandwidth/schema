package cstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"testing"
)

func TestTextListCSharp(t *testing.T) {
	t.Skip("layer 4 (cs): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "the .NET SDK")
	runCarryCS(t, `package probe
table Root {
 names [..3]string(8)
 maybe []?string(8)
 loose []string(8)
 bounded [..3]?string(8)
}
`, []string{
		`{}`, `{"names":["a","é",""],"maybe":["x",null,""],"loose":["u",""],"bounded":[null,"v",""]}`,
		`{"names":["123456789","ééééé","z","tail"],"maybe":[null,"123456789"]}`,
		`{"names":["a",42,null],"maybe":[false,{},[]]}`,
		`{"names":["x\u0000y"]}`, `{"maybe":["\ud800"]}`,
	}, textListNativeProbe)
}

const textListNativeProbe = `unsafe {
 var original=new Root();var rr=new TableReport();
 if(!Schema.RootFromJson(original,Encoding.UTF8.GetBytes("""{"names":["a","é",""],"maybe":["x",null,""],"loose":["u",""],"bounded":[null,"v",""]}"""),rr))throw new Exception("text-list native setup");
 byte[] wire=new byte[Schema.RootMeasure(original)];Schema.RootSave(original,wire);
 var authored=new Root();
 if(!Schema.RootNamesAppend(authored,Encoding.UTF8.GetBytes("a"))||!Schema.RootNamesAppend(authored,Encoding.UTF8.GetBytes("é"))||!Schema.RootNamesAppend(authored,ReadOnlySpan<byte>.Empty)||Schema.RootNamesAppend(authored,Encoding.UTF8.GetBytes("overflow")))throw new Exception("managed text append bounds");
 if(!Schema.RootMaybeAppend(authored,Encoding.UTF8.GetBytes("x"))||!Schema.RootMaybeAppendAbsent(authored)||!Schema.RootMaybeAppend(authored,ReadOnlySpan<byte>.Empty))throw new Exception("managed optional text append");
 if(!Schema.RootLooseAppend(authored,Encoding.UTF8.GetBytes("u"))||!Schema.RootLooseAppend(authored,ReadOnlySpan<byte>.Empty)||!Schema.RootBoundedAppendAbsent(authored)||!Schema.RootBoundedAppend(authored,Encoding.UTF8.GetBytes("v"))||!Schema.RootBoundedAppend(authored,ReadOnlySpan<byte>.Empty))throw new Exception("managed remaining list shapes");
 if(Schema.RootMaybeTextAt(authored,1,out bool present).Length!=0||present||Schema.RootMaybeTextAt(authored,2,out present).Length!=0||!present)throw new Exception("optional text presence accessor");
 byte[] authoredWire=new byte[Schema.RootMeasure(authored)];Schema.RootSave(authored,authoredWire);
 if(!authoredWire.AsSpan().SequenceEqual(wire))throw new Exception("managed append wire");
 using(var fresh=new RootBuilder()){
  var row=fresh.GetRoot();var worker=fresh.CreateWorker();
  if(!Schema.RootNamesAppend(row,Encoding.UTF8.GetBytes("a"))||!Schema.RootNamesAppend(row,Encoding.UTF8.GetBytes("é"))||!Schema.RootNamesAppend(row,ReadOnlySpan<byte>.Empty)||Schema.RootNamesAppend(row,Encoding.UTF8.GetBytes("overflow")))throw new Exception("native text append bounds");
  if(!Schema.RootMaybeAppend(worker,row,Encoding.UTF8.GetBytes("x"))||!Schema.RootMaybeAppendAbsent(worker,row)||!Schema.RootMaybeAppend(worker,row,ReadOnlySpan<byte>.Empty))throw new Exception("native optional text append");
  if(!Schema.RootLooseAppend(worker,row,Encoding.UTF8.GetBytes("u"))||!Schema.RootLooseAppend(worker,row,ReadOnlySpan<byte>.Empty)||!Schema.RootBoundedAppendAbsent(row)||!Schema.RootBoundedAppend(row,Encoding.UTF8.GetBytes("v"))||!Schema.RootBoundedAppend(row,ReadOnlySpan<byte>.Empty))throw new Exception("native remaining list shapes");
  if(Schema.RootMaybeTextAt(row,1,out present).Length!=0||present||Schema.RootMaybeTextAt(row,2,out present).Length!=0||!present)throw new Exception("native optional presence accessor");
  byte[] authoredNative=new byte[fresh.Measure()];if(fresh.Save(authoredNative)!=wire.Length||!authoredNative.AsSpan().SequenceEqual(wire))throw new Exception("native authored wire");
 }
 var clamped=new Root();var clampReport=new TableReport();
 if(!Schema.RootFromJson(clamped,Encoding.UTF8.GetBytes("""{"names":["123456789"]}"""),clampReport)||clampReport.Clamped!=1||clampReport.ClampedField!="Root.names")throw new Exception("named text clamp");
 var refusedAppend=new Root();
 if(Schema.RootNamesAppend(refusedAppend,Encoding.UTF8.GetBytes("123456789"))||Schema.RootNamesAppend(refusedAppend,new byte[]{0})||Schema.RootNamesAppend(refusedAppend,new byte[]{0xff})||refusedAppend.NamesCount!=0)throw new Exception("invalid append mutated managed list");
 var type=Schema.RootTableType();long size=Schema.RootLoadMeasure(wire);
 if(size<=0)throw new Exception("text-list native measure");
 void* memory=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((size+15)&~15L),16);
 try{
  var report=new TableReport();IntPtr ptr=Schema.TableWire.LoadRegion(type,(IntPtr)memory,size,wire,report);
  if(ptr==IntPtr.Zero||report.Malformed)throw new Exception("text-list native load");
  Span<ulong> ids=stackalloc ulong[4096];Span<int> slots=stackalloc int[4096];
  byte[] saved=new byte[Schema.TableWire.SaveRegion(ptr,type,Span<byte>.Empty,ids,true)];
  if(Schema.TableWire.SaveRegion(ptr,type,saved,ids,false)!=wire.Length||!saved.AsSpan().SequenceEqual(wire))throw new Exception("text-list native roundtrip");
  TableRetain retain=default;
  ptr=Schema.TableWire.LoadRetainRegion(type,(IntPtr)memory,size,wire,ref retain,new TableReport());
  if(ptr==IntPtr.Zero)throw new Exception("text-list retained load");
  saved=new byte[Schema.TableWire.SaveRetainRegion(ptr,type,Span<byte>.Empty,ids,slots,ref retain,null,true)];
  if(Schema.TableWire.SaveRetainRegion(ptr,type,saved,ids,slots,ref retain,new TableReport(),false)!=wire.Length||!saved.AsSpan().SequenceEqual(wire))throw new Exception("text-list retained roundtrip");
 }finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(memory);}
 byte[] managedCook=new byte[Schema.RootCookMeasure(original)];
 if(!Schema.RootCook(original,managedCook))throw new Exception("text-list managed cook");
 using(var builder=new RootBuilder()){
  if(!builder.Load(wire,new TableReport()))throw new Exception("text-list builder load");
  byte[] built=new byte[builder.Measure()];if(builder.Save(built)!=wire.Length||!built.AsSpan().SequenceEqual(wire))throw new Exception("text-list builder save");
  byte[] cooked=new byte[builder.CookMeasure()];if(!builder.Cook(cooked)||!cooked.AsSpan().SequenceEqual(managedCook))throw new Exception("text-list independent storage cooks disagree");
  fixed(byte* p=cooked){if(!RootCook.Open(out var opened,(IntPtr)p,cooked.Length)||opened.Root.NamesCount!=3||opened.Root.Maybe.Count!=3)throw new Exception("text-list cook open");}
 }

 byte[] TextWire(byte[] payload,ulong fieldId){
  var frame=new System.Collections.Generic.List<byte>{1,1,14,(byte)payload.Length};frame.AddRange(payload);frame.Add(0);
  byte[] idBytes=new byte[8];System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(idBytes,fieldId);frame.AddRange(idBytes);
  frame.AddRange(new byte[]{1,0,0,0,0,0,0,0});return frame.ToArray();
 }
 foreach(byte[] element in new[]{Encoding.UTF8.GetBytes("123456789"),Encoding.UTF8.GetBytes("ééééé"),new byte[]{0},new byte[]{0xff}}){
  byte[] payload=new byte[element.Length+3];payload[0]=12;payload[1]=1;payload[2]=(byte)element.Length;element.CopyTo(payload,3);
  byte[] source=TextWire(payload,type.Fields[0].Id);bool damaged=element[0]==0||element[0]==0xff;
  var read=new Root();var r=new TableReport();bool accepted=Schema.RootLoad(read,source,r);
  if(accepted==damaged||r.Malformed!=damaged||!damaged&&(r.Clamped!=1||r.ClampedField!="Root.names"||read.NamesCount!=1||read.Names[0].ValueLength!=8))throw new Exception("text-list wire text validation");
  long need=Schema.RootLoadMeasure(source);void* mem=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((need+15)&~15L),16);
  try{var nr=new TableReport();IntPtr loaded=Schema.TableWire.LoadRegion(type,(IntPtr)mem,need,source,nr);
   if((loaded==IntPtr.Zero)!=damaged||nr.Malformed!=damaged||!damaged&&(nr.Clamped!=1||nr.ClampedField!="Root.names"))throw new Exception("native text-list wire validation");
  }finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(mem);}
 }
 byte[] forgedCount=TextWire(new byte[]{12,127},type.Fields[2].Id);
 if(Schema.RootLoadMeasure(forgedCount,out TableRefuseReason countReason)!=-1||countReason!=TableRefuseReason.count_over_length)throw new Exception("plain string list count allocated beyond framing");
 original.Names[0].Value[0]=0;
 byte[] untouched=new byte[wire.Length];Array.Fill(untouched,(byte)0xa5);
 if(Schema.RootMeasure(original)!=-1||Schema.RootSave(original,untouched)!=-1||Array.Exists(untouched,b=>b!=0xa5))throw new Exception("stored NUL text-list writer refusal");
}`
