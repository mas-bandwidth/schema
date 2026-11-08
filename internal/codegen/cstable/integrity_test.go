package cstable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"hash/crc32"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const integrityCSSchema = `package probe
table Child | integrity = "crc32c" { value int32 }
table Root | integrity = "crc32c" {
 value int32
 child Child
}
table Plain { child Child }
fixed table FixedRow | integrity = "crc32c" { value int32 }
`

func TestIntegrityCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	witnesses := integrityGoWitnesses(t)
	// The Go optimized fixed-table form-1 surface currently omits its integrity
	// trailer. Do not reproduce that defect: a neutral undeclared-table body and
	// the standard library Castagnoli implementation independently pin this form.
	plain := unitFrom(t, strings.ReplaceAll(integrityCSSchema, " | integrity = \"crc32c\"", ""))
	model := tabletext.NewModel(plain)
	value := model.New(plain.Tables["FixedRow"])
	var report tabletext.Report
	if !model.Read(value, []byte("{\"value\":17}"), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	wire = binary.LittleEndian.AppendUint32(wire, crc32.Checksum(wire, crc32.MakeTable(crc32.Castagnoli)))
	witnesses["typed"] = hex.EncodeToString(wire)
	t.Run("wire-fixture", func(t *testing.T) {
		t15RunCS(t, integrityCSSchema, fmt.Sprintf("var root = new Probe.Root {Value=7,Child=new Probe.Child {Value=11}}; byte[] expected = Convert.FromHexString(%q); byte[] saved = new byte[Probe.Schema.RootMeasure(root)]; if (Probe.Schema.RootSave(root,saved)!=saved.Length || !saved.AsSpan().SequenceEqual(expected)) throw new Exception(\"independent Go integrity wire fixture\");", witnesses["root"]))
	})

	body := strings.NewReplacer("WIRE_ROOT", witnesses["root"], "WIRE_PLAIN", witnesses["plain"], "WIRE_FIXED", witnesses["fixed"], "WIRE_MESSAGE", witnesses["message"], "WIRE_TYPED", witnesses["typed"]).Replace(`unsafe {
 if (Probe.Schema.TableWire.IntegrityCrc(System.Text.Encoding.ASCII.GetBytes("123456789"))!=0xe3069283u) throw new Exception("CRC32C published check vector");
 byte[] expected = Convert.FromHexString("WIRE_ROOT");
 var root = new Probe.Root {Value=7,Child=new Probe.Child {Value=11}};
 byte[] saved = new byte[Probe.Schema.RootMeasure(root)];
 if (Probe.Schema.RootSave(root,saved)!=saved.Length || !saved.AsSpan().SequenceEqual(expected)) throw new Exception("independent Go variable integrity bytes");
 var plain = new Probe.Plain {Child=new Probe.Child{Value=11}};
 byte[] nested=new byte[Probe.Schema.PlainMeasure(plain)];
 if(Probe.Schema.PlainSave(plain,nested)!=nested.Length || !nested.AsSpan().SequenceEqual(Convert.FromHexString("WIRE_PLAIN"))) throw new Exception("nested child wrongly adds trailer");
 var type=Probe.Schema.RootTableType();
 byte[] shortOutput=new byte[saved.Length-1];Array.Fill(shortOutput,(byte)0x5a);
 if(Probe.Schema.RootSave(root,shortOutput)!=-1||Array.Exists(shortOutput,b=>b!=0x5a))throw new Exception("short integrity output was touched");
 byte[] base64Roundtrip=Convert.FromBase64String(Convert.ToBase64String(saved));
 if(!Probe.Schema.RootLoad(new Probe.Root(),base64Roundtrip,new Probe.TableReport()))throw new Exception("base64 lost CRC trailer");
 void Damage(Probe.TableReport r) { if (!r.Malformed || r.Refused || r.Reason!="integrity_mismatch" || r.Verdict!=Probe.Schema.TableWire.Verdict.Damaged || r.Unknown!=0 || r.KindMismatch!=0 || r.Clamped!=0) throw new Exception("integrity report "+r.Reason); }
 void CheckBad(byte[] bytes) {
  var value=new Probe.Root{Value=123};var report=new Probe.TableReport();
  if(Probe.Schema.RootLoad(value,bytes,report)||value.Value!=0) throw new Exception("damaged managed file decoded");
  Damage(report);
  if(Probe.Schema.RootLoadMeasure(bytes,out Probe.TableRefuseReason why)!=-1 || why!=Probe.TableRefuseReason.integrity_mismatch)throw new Exception("damaged measure reason "+why);
 }
 for(int at=1;at<saved.Length;at++)for(int bit=0;bit<8;bit++){byte[] corrupt=(byte[])saved.Clone();corrupt[at]^=(byte)(1<<bit);CheckBad(corrupt);}
 for(int n=1;n<saved.Length;n++)CheckBad(saved.AsSpan(0,n).ToArray());
 CheckBad(saved.AsSpan(0,saved.Length-4).ToArray());
 var wrong=(byte[])saved.Clone();wrong[0]=99;
 var formReport=new Probe.TableReport();
 if(Probe.Schema.RootLoad(new Probe.Root(),wrong,formReport)||!formReport.Refused||formReport.Malformed||formReport.Reason!="newer_form")throw new Exception("form refusal must precede CRC");
 byte[] forged=(byte[])saved.Clone();Array.Fill(forged,(byte)0xff,forged.Length-12,8);
 System.Buffers.Binary.BinaryPrimitives.WriteUInt32LittleEndian(forged.AsSpan(forged.Length-4),Probe.Schema.TableWire.IntegrityCrc(forged.AsSpan(0,forged.Length-4)));
 var framing=new Probe.TableReport();
 if(Probe.Schema.RootLoad(new Probe.Root(),forged,framing)||!framing.Malformed||framing.Reason=="integrity_mismatch")throw new Exception("valid checksum bypassed hostile count framing");
 type.Integrity=false;
 try{var oldReader=new Probe.TableReport();if(Probe.Schema.RootLoad(new Probe.Root(),saved,oldReader)||!oldReader.Malformed)throw new Exception("unmarked reader accepted trailer");}
 finally{type.Integrity=true;}
 var managed=new Probe.Root();if(!Probe.Schema.RootLoad(managed,saved,new Probe.TableReport())||managed.Child.Value!=11)throw new Exception("valid managed load");
 long size=Probe.Schema.RootLoadMeasure(saved,out long dataBytes,out long attributionBytes,out Probe.TableRefuseReason reason);
 if(size<=0||reason!=Probe.TableRefuseReason.ok||dataBytes+attributionBytes!=size)throw new Exception("valid native parts measure");
 void* memory=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((size+15)&~15L),16);
 try{
  Span<ulong> ids=stackalloc ulong[4096];Span<int> slots=stackalloc int[4096];
  var report=new Probe.TableReport();
  IntPtr ptr=Probe.Schema.TableWire.LoadRegion(type,(IntPtr)memory,size,saved,report);
  if(ptr==IntPtr.Zero||report.Malformed)throw new Exception("native integrity load");
  byte[] native=new byte[Probe.Schema.TableWire.SaveRegion(ptr,type,Span<byte>.Empty,ids,true)];
  if(Probe.Schema.TableWire.SaveRegion(ptr,type,native,ids,false)!=saved.Length||!native.AsSpan().SequenceEqual(saved))throw new Exception("native integrity save");
  Probe.TableRetain retain=default;
  ptr=Probe.Schema.TableWire.LoadRetainRegion(type,(IntPtr)memory,size,saved,ref retain,new Probe.TableReport());
  if(ptr==IntPtr.Zero)throw new Exception("retained integrity load");
  byte[] retained=new byte[Probe.Schema.TableWire.SaveRetainRegion(ptr,type,Span<byte>.Empty,ids,slots,ref retain,null,true)];
  if(Probe.Schema.TableWire.SaveRetainRegion(ptr,type,retained,ids,slots,ref retain,new Probe.TableReport(),false)!=saved.Length||!retained.AsSpan().SequenceEqual(saved))throw new Exception("retained integrity save");
  byte[] corrupt=(byte[])saved.Clone();corrupt[1]^=1;
  report=new Probe.TableReport();
  if(Probe.Schema.TableWire.LoadRegion(type,(IntPtr)memory,size,corrupt,report)!=IntPtr.Zero)throw new Exception("corrupt native accepted");Damage(report);
  report=new Probe.TableReport();
  if(Probe.Schema.TableWire.LoadRetainRegion(type,(IntPtr)memory,size,corrupt,ref retain,report)!=IntPtr.Zero)throw new Exception("corrupt retained accepted");Damage(report);
 }finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(memory);}
 using(var builder=new Probe.RootBuilder()){
  if(!builder.Load(saved,new Probe.TableReport()))throw new Exception("builder integrity load");
  byte[] built=new byte[builder.Measure()];
  if(builder.Save(built)!=saved.Length||!built.AsSpan().SequenceEqual(saved))throw new Exception("builder integrity save");
  byte[] corrupt=(byte[])saved.Clone();corrupt[1]^=1;var r=new Probe.TableReport();
  if(builder.Load(corrupt,r)||builder.GetRoot()->Value!=0)throw new Exception("builder integrity damage reset");Damage(r);
 }
 var fixedValues=new[]{new Probe.FixedRow{Value=17},new Probe.FixedRow{Value=-19}};
 byte[] typedWire=new byte[Probe.Schema.FixedRowMeasure(fixedValues[0])];
 if(Probe.Schema.FixedRowSave(fixedValues[0],typedWire)!=typedWire.Length||!typedWire.AsSpan().SequenceEqual(Convert.FromHexString("WIRE_TYPED")))throw new Exception("independent Go typed writer integrity bytes got="+Convert.ToHexString(typedWire)+" want=WIRE_TYPED");
 byte[] fixedBytes=new byte[Probe.Schema.FixedRowFixedMeasure(2)];
 if(Probe.Schema.FixedRowFixedSave(fixedValues,fixedBytes)!=fixedBytes.Length||!fixedBytes.AsSpan().SequenceEqual(Convert.FromHexString("WIRE_FIXED")))throw new Exception("independent Go fixed integrity bytes");
 var fixedBack=new[]{new Probe.FixedRow(),new Probe.FixedRow()};
 if(Probe.Schema.FixedRowFixedLoad(fixedBack,fixedBytes,new Probe.TableFixedEntry[8],new Probe.TableReport())!=2||fixedBack[0].Value!=17||fixedBack[1].Value!=-19)throw new Exception("fixed integrity load");
 for(int at=1;at<fixedBytes.Length;at++)for(int bit=0;bit<8;bit++){
  byte[] corrupt=(byte[])fixedBytes.Clone();corrupt[at]^=(byte)(1<<bit);
  fixedBack[0].Value=77;fixedBack[1].Value=88;var r=new Probe.TableReport();
  if(Probe.Schema.FixedRowFixedLoad(fixedBack,corrupt,new Probe.TableFixedEntry[8],r)!=-1||fixedBack[0].Value!=77||fixedBack[1].Value!=88)throw new Exception("damaged fixed file touched output");Damage(r);
 }
 for(int n=1;n<5;n++){var r=new Probe.TableReport();byte[] shortFixed=new byte[n];shortFixed[0]=3;
  if(Probe.Schema.FixedRowFixedLoad(fixedBack,shortFixed,new Probe.TableFixedEntry[8],r)!=-1)throw new Exception("short fixed accepted");Damage(r);}
 var fixedWrong=(byte[])fixedBytes.Clone();fixedWrong[0]=1;var fixedFormReport=new Probe.TableReport();
 if(Probe.Schema.FixedRowFixedLoad(fixedBack,fixedWrong,new Probe.TableFixedEntry[8],fixedFormReport)!=-1||!fixedFormReport.Refused||fixedFormReport.Malformed||fixedFormReport.Reason!="previous_form")throw new Exception("fixed form precedence");
 byte[] message=new byte[Probe.Schema.FixedRowMeasureMessages(fixedValues)];
 if(Probe.Schema.FixedRowSaveMessages(fixedValues,message)!=message.Length||!message.AsSpan().SequenceEqual(Convert.FromHexString("WIRE_MESSAGE")))throw new Exception("independent Go message integrity bytes");
 var vocabulary=Probe.Schema.TableWire.OwnVocabulary(Probe.Schema.Announce());
 if(Probe.Schema.FixedRowLoadMessages(fixedBack,message,vocabulary,new Probe.TableReport(),out int count)!=Probe.Schema.TableWire.Verdict.Ok||count!=2||fixedBack[0].Value!=17||fixedBack[1].Value!=-19)throw new Exception("message integrity load");
 for(int at=1;at<message.Length;at++)for(int bit=0;bit<8;bit++){
  byte[] corrupt=(byte[])message.Clone();corrupt[at]^=(byte)(1<<bit);
  fixedBack[0].Value=77;fixedBack[1].Value=88;var r=new Probe.TableReport();
  if(Probe.Schema.FixedRowLoadMessages(fixedBack,corrupt,vocabulary,r,out count)!=Probe.Schema.TableWire.Verdict.Damaged||count!=0||fixedBack[0].Value!=77||fixedBack[1].Value!=88)throw new Exception("damaged message touched output");Damage(r);
 }
 var messageWrong=(byte[])message.Clone();messageWrong[0]=1;var messageFormReport=new Probe.TableReport();
 if(Probe.Schema.FixedRowLoadMessages(fixedBack,messageWrong,vocabulary,messageFormReport,out count)!=Probe.Schema.TableWire.Verdict.Refused||messageFormReport.Malformed||messageFormReport.Reason!="newer_form")throw new Exception("message form precedence");
 for(int n=1;n<6;n++){
  var r=new Probe.TableReport();byte[] shortBatch=new byte[n];shortBatch[0]=2;
  if(Probe.Schema.FixedRowLoadMessages(fixedBack,shortBatch,vocabulary,r,out count)!=Probe.Schema.TableWire.Verdict.Damaged)throw new Exception("short message integrity");Damage(r);
 }
}`)
	t.Run("native", func(t *testing.T) { t15RunCS(t, integrityCSSchema, body) })
}

func integrityGoWitnesses(t *testing.T) map[string]string {
	t.Helper()
	u := unitFrom(t, integrityCSSchema)
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
	output := filepath.Join(dir, "witnesses.json")
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import("encoding/hex";"encoding/json";"os";"testing")
func TestIntegrityOracle(t *testing.T){
 out:=map[string]string{}
 root:=Root{Value:7,Child:Child{Value:11}};b:=make([]byte,RootMeasure(&root));if RootSave(&root,b)!=int64(len(b)){t.Fatal("root")};out["root"]=hex.EncodeToString(b)
 plain:=Plain{Child:Child{Value:11}};b=make([]byte,PlainMeasure(&plain));if PlainSave(&plain,b)!=int64(len(b)){t.Fatal("plain")};out["plain"]=hex.EncodeToString(b)
 rows:=[]FixedRow{{Value:17},{Value:-19}}
 b=make([]byte,FixedRowMeasure(&rows[0]));if FixedRowSave(&rows[0],b)!=int64(len(b)){t.Fatal("typed")};out["typed"]=hex.EncodeToString(b)
 b=make([]byte,FixedRowFixedMeasure(2));if FixedRowFixedSave(rows,b)!=int64(len(b)){t.Fatal("fixed")};out["fixed"]=hex.EncodeToString(b)
 messages:=[]*FixedRow{&rows[0],&rows[1]};b=make([]byte,FixedRowMeasureMessages(messages,nil));if FixedRowSaveMessages(messages,b,nil)!=int64(len(b)){t.Fatal("message")};out["message"]=hex.EncodeToString(b)
 data,err:=json.Marshal(out);if err!=nil{t.Fatal(err)};if err=os.WriteFile(%q,data,0600);err!=nil{t.Fatal(err)}
}
`, output))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestIntegrityOracle$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent Go integrity oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	return values
}
