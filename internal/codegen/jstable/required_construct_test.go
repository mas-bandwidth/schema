package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// These pins are assembled from field identities and the specified wire framing,
// independently of the neutral encoder, which historically elided required zeroes.
func TestJavaScriptRequiredConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes required-field presence and refusal)")
	const src = `package probe
 enum Choice { A, B }
 table Leaf { x int32 }
 union U { Empty }
 table All {
  b bool | required
  i int32 | required
  f float32 | required
  s string(8) | required
  w wstring(8) | required
  buf bytes(8) | required
  choice Choice | required
  u U | required
  ptr *Leaf | required
  text *string | required
  blob *bytes | required
  leaf Leaf | required
  list []int32 | required
  slots [2]int32 | required
  tags map[uint8]int32 | required
  keyed [Choice]int32 | required
 }
 table Kid { x int32 | required }
 table Nest { child Kid
 items []Kid
 tags map[uint8]Kid
 ptr *Kid }
 table Guard { on bool
 if on { needed int32 | required } }
 table Range { n int32 | min=5, required }
 `
	wide := "\ntable Wide {\n"
	for i := range 513 {
		wide += "v" + strconv.Itoa(i) + " int32"
		if i == 512 {
			wide += " | required"
		}
		wide += "\n"
	}
	wide += "}\n"
	u := unitFrom(t, src+wide)
	payloads := []string{"00", "00000000", "00000000", "00", "00", "020600", "00", "00", "00", "00", "00", "0100", "020400", "0a04020000000000000000", "020d00", "020400"}
	var body []byte
	for i, f := range u.Tables["All"].Fields {
		p, err := hex.DecodeString(payloads[i])
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, byte(i+1), byte(ir.TableWireFieldKind(f)))
		body = append(body, p...)
	}
	pack := func(body []byte) string {
		wire := append([]byte{1}, body...)
		wire = append(wire, 0)
		for _, f := range u.Tables["All"].Fields {
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(u.Tables["All"].Fields)))
		return hex.EncodeToString(wire)
	}
	type row struct {
		Name    string `json:"name"`
		Root    string `json:"root"`
		Wire    string `json:"wire"`
		Missing string `json:"missing"`
		Input   string `json:"input"`
	}
	cases := []row{{Name: "all-defaults", Root: "All", Wire: pack(body)}}
	populated := `{"b":true,"i":7,"f":1.25,"s":"hello","w":"wide","buf":"AQI=","choice":"A","u":{"Empty":null},"ptr":{"x":3},"text":"text","blob":"Aw==","leaf":{"x":9},"list":[1,-2],"slots":[2,3],"tags":{"1":4},"keyed":{"A":5}}`
	model := tabletext.NewModel(u)
	pop := model.New(u.Tables["All"])
	var popReport tabletext.Report
	if !model.Read(pop, []byte(populated), &popReport) {
		t.Fatalf("populated JSON: %+v", popReport)
	}
	popWire, err := tablewire.Encode(model, pop)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, row{Name: "all-populated", Root: "All", Wire: hex.EncodeToString(popWire), Input: populated})
	at := 0
	for i, f := range u.Tables["All"].Fields {
		n := 2 + len(payloads[i])/2
		cut := append(append([]byte{}, body[:at]...), body[at+n:]...)
		cases = append(cases, row{Name: "missing-" + f.Name, Root: "All", Wire: pack(cut), Missing: f.Name})
		if f.Name == "buf" || f.Name == "list" || f.Name == "slots" || f.Name == "tags" || f.Name == "keyed" {
			foreign := append([]byte{}, body...)
			foreign[at+3] = 1
			cases = append(cases, row{Name: "foreign-element-" + f.Name, Root: "All", Wire: pack(foreign), Missing: f.Name})
		}
		at += n
	}
	// A wrong-kind field is missing on the binary form, while a widening satisfies it.
	wrong := append([]byte{}, body...)
	wrong[1] = 6
	cases = append(cases, row{Name: "wrong-kind", Root: "All", Wire: pack(wrong), Missing: "b"})
	// The old, non-required schema writes nested records missing x. Keep x absent
	// while giving each record another field so its body actually rides.
	old := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(src, " | required", ""), "table Kid { x int32 }", "table Kid { x int32\n keep int32 }"), "min=5", "min=0"))
	om := tabletext.NewModel(old)
	for _, input := range []string{`{"child":{"keep":1}}`, `{"items":[{"keep":1}]}`, `{"tags":{"1":{"keep":1}}}`, `{"ptr":{"keep":1}}`} {
		v := om.New(old.Tables["Nest"])
		var r tabletext.Report
		if !om.Read(v, []byte(input), &r) {
			t.Fatalf("old JSON: %+v", r)
		}
		wire, err := tablewire.Encode(om, v)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, row{Name: "nested-" + input, Root: "Nest", Wire: hex.EncodeToString(wire), Missing: "x", Input: strings.ReplaceAll(input, `"keep":1`, ``)})
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsRequiredDriver, cases)
	for _, c := range cases {
		if !strings.Contains(out, c.Name+" OK\n") {
			t.Errorf("missing required probe %s\n%s", c.Name, out)
		}
	}
}

const jsRequiredDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const hex=b=>b===null?"null":Buffer.from(b).toString("hex");
const expect=(ok,msg)=>{if(!ok){throw new Error(msg);}};
const missing=(v,r,name)=>expect(v===null && r.missingRequired===1 && r.reason==="missing_required: "+name && !r.malformed,"missing "+name+": "+JSON.stringify(r));
for(const c of cases){
 const r=new m.TableVariableReport();const v=m[c.root+"Load"](new Uint8Array(Buffer.from(c.wire,"hex")),r);
 if(c.missing){missing(v,r,c.missing);}
 else {
  expect(v!==null && !r.malformed && (r.missingRequired||0)===0,"positive read "+JSON.stringify(r));
  expect(hex(m.AllSave(v))===c.wire,"required default writer");
  expect(m.AllMeasure(v)===c.wire.length/2,"required measure");
  const text=m.AllToJson(v);const jr=new m.TableVariableReport();const back=m.AllFromJson(text,jr);
  expect(back!==null && hex(m.AllSave(back))===c.wire,"required JSON roundtrip "+JSON.stringify(jr));
  const n=m.AllCookMeasure(v);expect(n>0 && m.AllCookFrom(v,new Uint8Array(n),1),"required cook");
  const obj=JSON.parse(text);
  for(const key of Object.keys(obj)) {const copy={...obj};delete copy[key];const wr=new m.TableVariableReport();missing(m.WideFromJson('{}',wr),wr,"v512");
expect(m.WideFromJson('{"v512":0}',new m.TableVariableReport())!==null,"required beyond 512 present");
const range=new m.Range();expect(m.RangeSave(range)===null && m.RangeMeasure(range)===-1,"required zero outside range must refuse writer");
expect(m.RangeToJson(range)===null,"required zero outside range must refuse JSON");
range.n=5;expect(m.RangeSave(range)!==null && m.RangeToJson(range)!==null,"required explicit valid value");
const rr=new m.TableVariableReport();missing(m.AllFromJson(JSON.stringify(copy),rr),rr,key);}
 }
 if(c.input && c.missing){const jr=new m.TableVariableReport();missing(m[c.root+"FromJson"](c.input,jr),jr,c.missing);}
 console.log(c.name+" OK");
}
for(const input of ['{}','{"on":false}','{"on":true,"needed":0}']){
 const r=new m.TableVariableReport();const v=m.GuardFromJson(input,r);expect(v!==null,"guard positive");
 const wire=m.GuardSave(v);expect(wire!==null && m.GuardLoad(wire,new m.TableVariableReport())!==null,"guard wire");
}
const r=new m.TableVariableReport();missing(m.GuardFromJson('{"on":true}',r),r,"needed");
// A reused report records past events but a clean read must still succeed.
expect(m.GuardFromJson('{}',r)!==null,"reused JSON report");
expect(m.AllLoad(new Uint8Array(Buffer.from(cases[0].wire,"hex")),r)!==null,"reused wire report");
const rr=new m.TableVariableReport();const wrongShape=m.GuardFromJson('{"on":true,"needed":"wrong"}',rr);
expect(wrongShape!==null && rr.kindMismatch===1 && rr.missingRequired===0,"JSON key presence");
`

// Required is a variable-table declaration. Its fixed positional accelerator
// remains valid, but it must not acquire message APIs (message roots are explicit
// fixed tables, where Required is forbidden by the checker).
func TestJavaScriptRequiredFixedConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes required no-default fixed storage)")
	const src = `package probe
table T { n int32 | min=5, required
 wide uint64 | min=9007199254740993, required
}
table Guarded { active bool
 if active { n int32 | min=5, required }
}
fixed table Other { x int32 }
`
	const probe = `
const exports=await import("./ProbeTable.js");
if(exports.GuardedFixedSave!==undefined){fail("guarded ordinary table acquired fixed positional API");}
for(const name of Object.keys(exports)){
 if(name.startsWith("T") && name.includes("Message") && !name.startsWith("Table")) {fail("required table leaked message surface "+name);}
}
const value=new T();InitT(value);
const wire=new Uint8Array(TMeasure(1));
if(TSave([value],1,wire)!==-1){fail("required storage zero outside range accepted by fixed writer");}
value.n=5;
value.wide=9007199254740992n;
if(TSave([value],1,wire)!==-1){fail("required uint64 preflight rounded its minimum");}
value.wide=9007199254740993n;
if(TSave([value],1,wire)!==wire.length){fail("required initialized fixed value rejected");}
const back=[new T()];InitT(back[0]);const report=new TableFixedReport();
if(TLoad(back,1,wire,wire.length,TNewPlan(4096,4096),report)!==1 || back[0].n!==5 || back[0].wide!==9007199254740993n || report.malformed || report.refused!==0){fail("required fixed roundtrip");}
`
	if out, err := jsRunVersionProbe(t, jsNode(t), src, nil, 0, "T", probe); err != nil {
		t.Fatalf("required fixed: %v\n%s", err, out)
	}
}
