package jstable

import (
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

func TestJavaScriptUnboundedTextConstruct(t *testing.T) {
	slowtest.Gate(t, "native Go and Node (unbounded text wire, cook, region and accessors)")
	schema := `package probe
 table Where {
 rules []string
 first [..4]?string
 capped [..2]string
 running []?string
 byname map[string]int32
 }
 table Bounded {
 rules []string(8)
 first [..4]?string(8)
 capped [..2]string(8)
 running []?string(8)
 byname map[string(8)]int32
 }
 `
	u := unitFrom(t, schema)
	long := strings.Repeat("é", 35000)
	inputs := []string{`{}`, `{"rules":["","a","b"],"first":["",null,"x"],"capped":["c"],"running":[null,"","d"],"byname":{"":0,"k1":1,"k2":2}}`}
	raw, err := json.Marshal(map[string]any{"rules": []string{long, "tail"}, "first": []any{nil, long, ""}, "capped": []string{long}, "running": []any{long, nil, ""}, "byname": map[string]int{long + "a": 1, long + "b": 2}})
	if err != nil {
		t.Fatal(err)
	}
	inputs = append(inputs, string(raw))
	// The compiler-side neutral extent/cook did not carry text blobs when this
	// regression was introduced. Execute the independent generated Go backend;
	// its arena builder, region measure and cook supply populated byte pins.
	gf, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	gt, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(gf, gt)
	dir := t.TempDir()
	rt := slowtest.SerializeGo(t, "../../../")
	gf["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", rt))
	gf["oracle_test.go"] = []byte(jsUnboundedGoOracle)
	inputBytes, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	gf["inputs.json"] = inputBytes
	for n, data := range gf {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout=0", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsUnboundedDriver, spec)
	if !strings.Contains(out, "UNBOUNDED TEXT OK\n") {
		t.Fatal(out)
	}
}

const jsUnboundedGoOracle = `package probe
import("encoding/hex";"encoding/json";"os";"testing";"unsafe")
func TestOracle(t *testing.T){
 raw,err:=os.ReadFile("inputs.json");if err!=nil{t.Fatal(err)};var inputs []string;if err=json.Unmarshal(raw,&inputs);err!=nil{t.Fatal(err)}
 type row struct {Input string;Wire string;Cook string;Text string;Region int64}
 var rows []row
 for _,input:=range inputs{
  var b WhereBuilder;if !b.Init(){t.Fatal("builder init")};var report TableReport
  if !WhereFromJSON(&b,[]byte(input),&report)||report!=(TableReport{}){t.Fatalf("JSON: %+v",report)}
  v:=b.GetRoot();n:=WhereMeasure(v,&b.Arena);if n<0{t.Fatal("measure")};wire:=make([]byte,n)
  if WhereSave(v,wire,&b.Arena)!=n{t.Fatal("save")}
  cn:=WhereCookMeasure(v,&b.Arena);if cn<0{t.Fatal("cook measure")};rawCook:=make([]byte,cn+64);at:=0
  for uintptr(unsafe.Pointer(&rawCook[at]))%64!=0{at++};cook:=rawCook[at:at+int(cn)]
  if !WhereCookFrom(v,cook,TableByteOrderLittle,&b.Arena){t.Fatal("cook")}
  var handle WhereCook;if !WhereOpen(&handle,unsafe.Pointer(&cook[0]),cn){t.Fatal("cook open")};cv:=(*Where)(handle.Region)
  tn:=WhereToJSONMeasure(cv);text:=make([]byte,tn);if WhereToJSON(cv,text)!=tn{t.Fatal("JSON write")}
  rows=append(rows,row{input,hex.EncodeToString(wire),hex.EncodeToString(cook),string(text),WhereLoadMeasure(wire)})
  b.Shutdown()
 }
 out,err:=json.Marshal(rows);if err!=nil{t.Fatal(err)};if err=os.WriteFile("oracle.json",out,0600);err!=nil{t.Fatal(err)}
}
`

const jsUnboundedDriver = `
import {readFileSync} from "node:fs";import {pathToFileURL} from "node:url";
const spec=JSON.parse(readFileSync(process.argv[2],"utf8")),base=pathToFileURL(process.argv[3]),m=await import(base.href);
const bytes=s=>new Uint8Array(Buffer.from(s,"hex")),hex=b=>b===null?"null":Buffer.from(b).toString("hex");
const expect=(ok,msg)=>{if(!ok)throw new Error(msg);};
const cooked=await import(new URL("ProbeCook.js",base).href);
const decode=b=>Buffer.from(b).toString("utf8");
for(const c of spec){
 const report=new m.TableVariableReport(),value=m.WhereFromJson(c.Input,report);
 expect(value!==null&&!report.damaged&&report.oor===0,"JSON read "+JSON.stringify(report));
 expect(m.WhereToJson(value).trim()===c.Text.trim(),"Go JSON pin");
 expect(hex(m.WhereSave(value))===c.Wire&&m.WhereMeasure(value)===c.Wire.length/2,"Go wire pin");
 const rr=new m.TableVariableReport(),back=m.WhereLoad(bytes(c.Wire),rr);
 expect(back!==null&&!rr.damaged&&rr.oor===0&&m.WhereToJson(back)===m.WhereToJson(value),"wire read "+JSON.stringify(rr));
 expect(m.WhereLoadMeasure(bytes(c.Wire))===c.Region,"Go region pin got="+m.WhereLoadMeasure(bytes(c.Wire))+" want="+c.Region);
 const n=m.WhereCookMeasure(value),cook=new Uint8Array(Math.max(n,0));
 expect(n>0&&m.WhereCookFrom(value,cook,1)&&hex(cook)===c.Cook,"Go cook pin gotSize="+n+" wantSize="+c.Cook.length/2);
 const handle=cooked.WhereCook.Open(cook);expect(handle!==null,"cook open");
 const row=cooked.WhereCook.Type.Row,view=handle.View,at=handle.Region;
 for(const [field,name] of [["rules","Rules"],["first","First"],["capped","Capped"],["running","Running"]]){
  const info=cooked.WhereCook.Type.Fields.find(x=>x.Name===field).RecordRef(),element=info.Row;
  const count=row[name+"Count"](view,at);expect(count===value[field].length,"cooked count "+field);
  for(let i=0;i<count;i++){
   const offset=field==="rules"||field==="running"?row[name+"At"](view,at,i):row[name+"At"](at,i);
   const present=element.ValuePresent?element.ValuePresent(view,offset):true;
   expect(present===(value[field][i].value!==null),"cooked presence "+field);
   if(present){expect(decode(element.Value(cook,view,offset))===value[field][i].value,"cooked text "+field);}
  }
 }
 const entry=cooked.WhereCook.Type.Fields.find(x=>x.Name==="byname").RecordRef().Row;
 for(const [key,num] of value.byname){
  const offset=row.BynameFind(cook,view,at,Buffer.from(key));expect(offset>=0,"cooked find");
  expect(decode(entry.Key(cook,view,offset))===key&&entry.Value(view,offset)===num,"cooked map value");
 }
 expect(row.BynameFind(cook,view,at,Buffer.from("not a key"))===-1,"cooked missing key");
 const retain=new m.TableVariableRetain(new Uint8Array(1024*1024),new Array(64)),rp=new m.TableVariableReport();
 const rv=m.WhereLoadRetain(bytes(c.Wire),retain,rp);expect(rv!==null&&hex(m.WhereSaveRetain(rv,retain,rp))===c.Wire,"retain");
}
const short=m.WhereFromJson(spec[1].Input,new m.TableVariableReport());
const bound=m.BoundedLoad(bytes(spec[1].Wire),new m.TableVariableReport());
expect(hex(m.BoundedSave(bound))===spec[1].Wire,"bounded twin bytes");
for(const field of ["rules","first","capped","running"]){
 for(const bad of ["x\u0000y","\ud800","\udc00"]){
  const value=new m.Where();value[field]=[{value:bad}];
  expect(m.WhereSave(value)===null&&m.WhereMeasure(value)===-1&&m.WhereToJson(value)===null,"bad writer "+field);
 }
 const source=new m.Where();source[field]=[{value:"xyz"}];const wire=m.WhereSave(source),at=Buffer.from(wire).indexOf(Buffer.from("xyz"));
 for(const byte of [0,255]){const damaged=wire.slice();damaged[at]=byte;const r=new m.TableVariableReport();expect(m.WhereLoad(damaged,r)===null&&r.damaged,"bad wire "+field);}
}
for(const bad of ["x\u0000y","\ud800"]){
 const value=new m.Where();value.byname.set(bad,1);
 expect(m.WhereSave(value)===null&&m.WhereMeasure(value)===-1&&m.WhereToJson(value)===null,"bad map key");
}
for(const mode of ["wire","json"]){
 const r=new m.TableVariableReport(),v=mode==="wire"?m.BoundedLoad(bytes(spec[2].Wire),r):m.BoundedFromJson(spec[2].Input,r);
 expect(v!==null&&!r.damaged&&r.oor===6&&v.byname.size===0,"bounded read of long text "+mode+" "+JSON.stringify(r));
 expect(v.rules[0].value==="éééé"&&v.first[1].value==="éééé"&&v.capped[0].value==="éééé"&&v.running[0].value==="éééé","codepoint clamp");
}
const mapSource=new m.Where();mapSource.byname.set("xyz",1);const mapWire=m.WhereSave(mapSource),keyAt=Buffer.from(mapWire).indexOf(Buffer.from("xyz"));
for(const byte of [0,255]){const damaged=mapWire.slice();damaged[keyAt]=byte;const r=new m.TableVariableReport();expect(m.WhereLoad(damaged,r)===null&&r.damaged,"malformed map key");}
const longWire=bytes(spec[2].Wire);expect(m.WhereLoad(longWire.subarray(0,100),new m.TableVariableReport())===null,"truncated long text");
console.log("UNBOUNDED TEXT OK");
`
