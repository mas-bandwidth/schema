package jstable

import (
	"encoding/hex"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"strings"
	"testing"
)

func TestJavaScriptTextListConstruct(t *testing.T) {
	slowtest.Gate(t, "node (text lists and total bounds)")
	source := `package probe
 table Lists {
 a [..3]string(5)
 b [..3]?string(5)
 c []string(5)
 d []?string(5)
 }
 table Totals {
 a [..3]string(5) | total=5
 b [..3]?string(5) | total=5
 c []string(5) | total=5
 d []?string(5) | total=5
 }
 table Required { a []string(5) | required }
 table Nested { lists Lists
 ptr *Totals
 }
 table ArrayRoot | json="array" { values []string(5) }
 `
	u := unitFrom(t, source)
	model := tabletext.NewModel(u)
	type row struct {
		Root   string `json:"root"`
		Input  string `json:"input"`
		Wire   string `json:"wire"`
		Text   string `json:"text"`
		Cook   string `json:"cook"`
		Region int64  `json:"region"`
	}
	var rows []row
	for _, tc := range []struct{ root, input string }{
		{"Lists", `{}`},
		{"Lists", `{"a":["","é","abc"],"b":[null,"","abc"],"c":["é","x"],"d":["",null,"abc"]}`},
		{"Totals", `{"a":["é","abc"],"b":[null,"é","abc"],"c":["","é","abc"],"d":["",null,"abc"]}`},
		{"Nested", `{"lists":{"a":["é"]},"ptr":{"d":[null,"abc"]}}`},
		{"ArrayRoot", `["","é","abc"]`},
	} {
		v := model.New(u.Tables[tc.root])
		var rep tabletext.Report
		if !model.Read(v, []byte(tc.input), &rep) || !rep.Silent() {
			t.Fatal(rep)
		}
		wire, err := tablewire.Encode(model, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		cook, err := tablecook.Cook(model, v, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		region, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[tc.root], wire)
		if !whole || refused {
			t.Fatal("region measure refused")
		}
		rows = append(rows, row{tc.root, tc.input, hex.EncodeToString(wire), string(text) + "\n", hex.EncodeToString(cook), region})
	}
	// Source is deliberately wider and has no total, so its bytes independently
	// exercise the target declaration's count, element, and prefix-total clamps.
	wide := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(source, "string(5)", "string(16)"), "[..3]", "[..8]"))
	wm := tabletext.NewModel(wide)
	wv := wm.New(wide.Tables["Lists"])
	var wr tabletext.Report
	input := `{"a":["ééé","a","z","tail"],"b":[null,"ééé","abc","tail"],"c":["ééé","a","z"],"d":[null,"ééé","abc","x"]}`
	if !wm.Read(wv, []byte(input), &wr) {
		t.Fatal(wr)
	}
	wire, err := tablewire.Encode(wm, wv)
	if err != nil {
		t.Fatal(err)
	}
	// Both paths refuse the complete value when the reader's count, element
	// or total bound is exceeded. The Go oracle is independent of the JS leg.
	for _, root := range []string{"Lists", "Totals"} {
		v := model.New(u.Tables[root])
		var textReport tabletext.Report
		if model.Read(v, []byte(input), &textReport) || !textReport.Refused {
			t.Fatalf("%s Go JSON must refuse over-bound input: %+v", root, textReport)
		}
		var wireReport tabletext.Report
		ok, err := tablewire.Decode(model, model.New(u.Tables[root]), wire, &wireReport)
		if ok || err == nil || !wireReport.Refused {
			t.Fatalf("%s Go wire must refuse over-bound input: %v %+v", root, err, wireReport)
		}
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := struct {
		Rows  []row  `json:"rows"`
		Wide  string `json:"wide"`
		Input string `json:"input"`
	}{rows, hex.EncodeToString(wire), input}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsTextListDriver, spec)
	if !strings.Contains(out, "TEXT LIST OK\n") {
		t.Fatal(out)
	}
}

const jsTextListDriver = `
import {readFileSync} from "node:fs";import {pathToFileURL} from "node:url";
const spec=JSON.parse(readFileSync(process.argv[2],"utf8")),m=await import(pathToFileURL(process.argv[3]).href);
const bytes=s=>new Uint8Array(Buffer.from(s,"hex")),hex=b=>b===null?"null":Buffer.from(b).toString("hex");
const expect=(ok,msg)=>{if(!ok)throw new Error(msg);};
for(const c of spec.rows){
 const report=new m.TableVariableReport(),v=m[c.root+"FromJson"](c.input,report);
 expect(v!==null&&!report.damaged&&!report.mismatch,"JSON "+c.root+" "+JSON.stringify(report));
 expect(m[c.root+"ToJson"](v)===c.text,"JSON pin "+c.root);
 expect(hex(m[c.root+"Save"](v))===c.wire,"wire pin "+c.root);
 expect(m[c.root+"Measure"](v)===c.wire.length/2,"measure "+c.root);
 const rr=new m.TableVariableReport(),back=m[c.root+"Load"](bytes(c.wire),rr);
 expect(back!==null&&!rr.damaged&&!rr.mismatch&&m[c.root+"ToJson"](back)===c.text,"wire read "+c.root);
 expect(m[c.root+"LoadMeasure"](bytes(c.wire))===c.region,"region measure "+c.root);
 const size=m[c.root+"CookMeasure"](v),cook=new Uint8Array(Math.max(size,0));
 expect(size>0&&m[c.root+"CookFrom"](v,cook,1)&&hex(cook)===c.cook,"cook pin "+c.root+" got="+hex(cook)+" want="+c.cook);
 const retain=new m.TableVariableRetain(new Uint8Array(4096),new Array(64)),rp=new m.TableVariableReport();
 const rv=m[c.root+"LoadRetain"](bytes(c.wire),retain,rp);
 expect(rv!==null&&hex(m[c.root+"SaveRetain"](rv,retain,rp))===c.wire,"retain "+c.root);
}

for(const mode of ["json","wire"]){
 const r=new m.TableVariableReport(),t=new m.TableVariableReport();
 const plain=mode==="json"?m.ListsFromJson(spec.input,r):m.ListsLoad(bytes(spec.wide),r);
 const limited=mode==="json"?m.TotalsFromJson(spec.input,t):m.TotalsLoad(bytes(spec.wide),t);
 expect(plain===null&&!r.damaged&&r.oor>0,"element/count refusal "+mode+" "+JSON.stringify(r));
 expect(limited===null&&!t.damaged&&t.oor>0,"total refusal "+mode+" "+JSON.stringify(t));
 expect(t.oorField==="Totals.a","first named refusal "+mode+" "+t.oorField);
}
for(const root of ["Lists","Totals"]){
 for(const field of ["a","b","c","d"]){
  for(const invalid of [["abcdef"],["x\u0000y"],["\ud800"],["\udc00"]]){
   const v=new m[root]();v[field]=invalid.map(value=>({value}));
   expect(m[root+"Save"](v)===null&&m[root+"Measure"](v)===-1&&m[root+"ToJson"](v)===null,"invalid text "+root+"."+field);
  }
  if(field==="a"||field==="b"){
   const v=new m[root]();v[field]=Array.from({length:4},()=>({value:""}));
   expect(m[root+"Save"](v)===null&&m[root+"Measure"](v)===-1&&m[root+"ToJson"](v)===null,"count write bound "+root+"."+field);
  }
  const v=new m.Totals();v[field]=["abc","abc",""].map(value=>({value}));
  expect(m.TotalsSave(v)===null&&m.TotalsMeasure(v)===-1&&m.TotalsToJson(v)===null,"total write bound "+field);
 }
}
for(const field of ["a","b","c","d"]){
 const r=new m.TableVariableReport();
 expect(m.ListsFromJson(JSON.stringify({[field]:["x\u0000y"]}),r)===null&&r.damaged,"JSON zero "+field);
}
for (const field of ["a","b","c","d"]) {
 const source=new m.Lists(); source[field]=[{value:"xyz"}]; const wire=m.ListsSave(source);
 const at=Buffer.from(wire).indexOf(Buffer.from("xyz")); expect(at>=0,"mutation marker");
 for (const byte of [0,255]) { const bad=wire.slice();bad[at]=byte;const r=new m.TableVariableReport();
  expect(m.ListsLoad(bad,r)===null&&r.damaged,"malformed wire text "+field+" "+byte);
 }
}
const req=new m.Required();expect(m.RequiredSave(req)!==null,"required empty text list writer");
expect(m.RequiredFromJson("{}",new m.TableVariableReport())===null,"required list missing");
const emptyReq=m.RequiredFromJson('{"a":[]}',new m.TableVariableReport());
expect(emptyReq!==null&&hex(m.RequiredSave(emptyReq))===hex(m.RequiredSave(req)),"required empty list JSON");
expect(!("ListsATextSave" in m)&&!("ListsATextOpen" in m),"synthetic record exposed as root");
console.log("TEXT LIST OK");
`

// Prove that the strict read probes detect the old tolerant outcome: erasing
// the out-of-range part of the strict verdict must expose a partial value in
// both the wire and JSON readers.
func TestJavaScriptTextListStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "node (text list strict negative control)")
	u := unitFrom(t, `package probe
 table Wide { values []string(8) }
 table Narrow { values []string(8) | total=5 }
 `)
	model := tabletext.NewModel(u)
	input := `{"values":["abc","abc"]}`
	wide := model.New(u.Tables["Wide"])
	var r tabletext.Report
	if !model.Read(wide, []byte(input), &r) {
		t.Fatal(r)
	}
	wire, err := tablewire.Encode(model, wide)
	if err != nil {
		t.Fatal(err)
	}
	var wr, jr tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(u.Tables["Narrow"]), wire, &wr); ok || err == nil || !wr.Refused {
		t.Fatalf("Go wire did not refuse: %v %+v", err, wr)
	}
	if model.Read(model.New(u.Tables["Narrow"]), []byte(input), &jr) || !jr.Refused {
		t.Fatalf("Go JSON did not refuse: %+v", jr)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u) + "Variable.js"
	spec := map[string]string{"input": input, "wire": hex.EncodeToString(wire)}
	script := `
import {readFileSync} from "node:fs";import {pathToFileURL} from "node:url";
const c=JSON.parse(readFileSync(process.argv[2],"utf8")),m=await import(pathToFileURL(process.argv[3]).href);
const wire=m.NarrowLoad(new Uint8Array(Buffer.from(c.wire,"hex")),new m.TableVariableReport());
const json=m.NarrowFromJson(c.input,new m.TableVariableReport());
console.log("wire="+(wire===null?"refused":"accepted")+" json="+(json===null?"refused":"accepted"));
`
	if out := runJSConstructProbe(t, files, home, script, spec); !strings.Contains(out, "wire=refused json=refused") {
		t.Fatalf("positive: %s", out)
	}
	const anchor = "return report.mismatch + report.oor + report.repeated + report.sharedBlob;"
	src := string(files[home])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("negative control anchor count %d", strings.Count(src, anchor))
	}
	files[home] = []byte(strings.Replace(src, anchor, "return report.mismatch + report.repeated + report.sharedBlob;", 1))
	if out := runJSConstructProbe(t, files, home, script, spec); !strings.Contains(out, "wire=accepted json=accepted") {
		t.Fatalf("negative control did not expose both partial reads: %s", out)
	}
}
