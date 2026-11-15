package cstable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const textTotalSchema = `package probe
table Root {
 a [..3]string(5) | total=5
 b [..3]?string(5) | total=5
 c []string(5) | total=5
 d []?string(5) | total=5
}
table Wrap { child Root }
`
const textTotalInput = `{"a":["ééé","a","z","tail"],"b":[null,"ééé","abc","tail"],"c":["ééé","a","z"],"d":[null,"ééé","abc","x"]}`

func TestTextTotalCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	wide := unitFrom(t, strings.NewReplacer("string(5)", "string(20)", "[..3]", "[..8]", " | total=5", "").Replace(textTotalSchema))
	model := tabletext.NewModel(wide)
	value := model.New(wide.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(textTotalInput), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	// THE STRICT READ (docs/SPEC-TABLES.md §4, §6.5): text past its total is a
	// refusal of the whole load, never a kept prefix, in generated Go as here.
	textTotalGoRefuses(t, wire)
	extra := strings.NewReplacer("SOURCE_WIRE", hex.EncodeToString(wire)).Replace(textTotalNativeProbe)
	runCarryCS(t, textTotalSchema, []string{`{}`, textTotalInput,
		`{"a":["ab","cde"],"b":[null,"","abc"],"c":["a","b","c","d","e"],"d":[null,"","12345"]}`,
		`{"a":["abc","def","123456789\u0000"],"b":["abc","def","123456789\u0000"],"c":["abc","def","123456789\u0000"],"d":["abc","def","123456789\u0000"]}`,
	}, extra)
}

const textTotalNativeProbe = `unsafe {
 byte[] source=Convert.FromHexString("SOURCE_WIRE");
 var root=new Root();var report=new TableReport();
 if(Schema.RootLoad(root,source,report)||report.Reason==null)throw new Exception("wire past its text total must refuse");
 var type=Schema.RootTableType();long size=Schema.RootLoadMeasure(source);
 if(size>=0){
  void* memory=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((size+15)&~15L),16);
  try{
   var nativeReport=new TableReport();
   if(Schema.TableWire.LoadRegion(type,(IntPtr)memory,size,source,nativeReport)!=IntPtr.Zero||nativeReport.Reason==null)throw new Exception("native load past its text total must refuse");
   TableRetain retain=default;var retainedReport=new TableReport();
   if(Schema.TableWire.LoadRetainRegion(type,(IntPtr)memory,size,source,ref retain,retainedReport)!=IntPtr.Zero||retainedReport.Reason==null)throw new Exception("retained load past its text total must refuse");
  }finally{System.Runtime.InteropServices.NativeMemory.AlignedFree(memory);}
 }
 using(var builder=new RootBuilder()){
  var r=new TableReport();if(builder.Load(source,r)||r.Reason==null)throw new Exception("builder load past its text total must refuse");
 }
 var appended=new Root();
 if(!Schema.RootAAppend(appended,Encoding.UTF8.GetBytes("éé"))||!Schema.RootAAppend(appended,Encoding.UTF8.GetBytes("a"))||Schema.RootAAppend(appended,Encoding.UTF8.GetBytes("b"))||appended.ACount!=2)throw new Exception("managed total append transaction");
 if(!Schema.RootDAppendAbsent(appended)||!Schema.RootDAppend(appended,Encoding.UTF8.GetBytes("12345"))||Schema.RootDAppend(appended,Encoding.UTF8.GetBytes("x"))||appended.DCount!=2)throw new Exception("managed optional total append");
 using(var builder=new RootBuilder()){
  var row=builder.GetRoot();var worker=builder.CreateWorker();
  if(!Schema.RootAAppend(row,Encoding.UTF8.GetBytes("éé"))||!Schema.RootAAppend(row,Encoding.UTF8.GetBytes("a"))||Schema.RootAAppend(row,Encoding.UTF8.GetBytes("b"))||row->ACount!=2)throw new Exception("native total append transaction");
  if(!Schema.RootDAppendAbsent(worker,row)||!Schema.RootDAppend(worker,row,Encoding.UTF8.GetBytes("12345"))||Schema.RootDAppend(worker,row,Encoding.UTF8.GetBytes("x"))||row->D.Count!=2)throw new Exception("native optional total append");
 }
 appended.A[1].Value[0]=(byte)'a';appended.A[1].Value[1]=(byte)'b';appended.A[1].ValueLength=2;
 byte[] untouched=new byte[512];Array.Fill(untouched,(byte)0x5a);
 if(Schema.RootMeasure(appended)!=-1||Schema.RootSave(appended,untouched)!=-1||Schema.RootToJsonMeasure(appended)!=-1||Array.Exists(untouched,b=>b!=0x5a))throw new Exception("over-total managed writer preflight");
 if(Schema.WrapMeasure(new Wrap{Child=appended})!=-1)throw new Exception("nested total writer preflight");
}`

func textTotalGoRefuses(t *testing.T, wire []byte) {
	t.Helper()
	u := unitFrom(t, textTotalSchema)
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
	output := filepath.Join(dir, "refused")
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import("encoding/hex";"os";"testing")
func TestTotalOracle(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var r TableReport
 if RootFromJSON(&b,[]byte(%q),&r)||!r.Refused(){t.Fatal("JSON past its total must refuse",r)}
 wire,_:=hex.DecodeString(%q);r=TableReport{};if RootLoadBuilder(&b,wire,&r)||!r.Refused(){t.Fatal("wire past its total must refuse",r)}
 if err:=os.WriteFile(%q,[]byte("ok"),0600);err!=nil{t.Fatal(err)}
}
`, textTotalInput, hex.EncodeToString(wire), output))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestTotalOracle$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go total oracle: %v\n%s", err, out)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}
