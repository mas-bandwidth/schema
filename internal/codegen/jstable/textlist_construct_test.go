package jstable

import (
	"encoding/hex"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
 expect(v!==null&&!report.malformed&&!report.kindMismatch,"JSON "+c.root+" "+JSON.stringify(report));
 expect(m[c.root+"ToJson"](v)===c.text,"JSON pin "+c.root);
 expect(hex(m[c.root+"Save"](v))===c.wire,"wire pin "+c.root);
 expect(m[c.root+"Measure"](v)===c.wire.length/2,"measure "+c.root);
 const rr=new m.TableVariableReport(),back=m[c.root+"Load"](bytes(c.wire),rr);
 expect(back!==null&&!rr.malformed&&!rr.kindMismatch&&m[c.root+"ToJson"](back)===c.text,"wire read "+c.root);
 expect(m[c.root+"LoadMeasure"](bytes(c.wire))===c.region,"region measure "+c.root);
 const size=m[c.root+"CookMeasure"](v),cook=new Uint8Array(Math.max(size,0));
 expect(size>0&&m[c.root+"CookFrom"](v,cook,1)&&hex(cook)===c.cook,"cook pin "+c.root+" got="+hex(cook)+" want="+c.cook);
 const retain=new m.TableVariableRetain(new Uint8Array(4096),new Array(64)),rp=new m.TableVariableReport();
 const rv=m[c.root+"LoadRetain"](bytes(c.wire),retain,rp);
 expect(rv!==null&&hex(m[c.root+"SaveRetain"](rv,retain,rp))===c.wire,"retain "+c.root);
}
const vals=v=>Object.fromEntries(["a","b","c","d"].map(k=>[k,v[k].map(x=>x.value)]));
const expected={a:["éé","a","z"],b:[null,"éé","abc"],c:["éé","a","z"],d:[null,"éé","abc","x"]};
const totalExpected={a:["éé","a"],b:[null,"éé"],c:["éé","a"],d:[null,"éé"]};
for(const mode of ["json","wire"]){
 const r=new m.TableVariableReport(),t=new m.TableVariableReport();
 const plain=mode==="json"?m.ListsFromJson(spec.input,r):m.ListsLoad(bytes(spec.wide),r);
 const limited=mode==="json"?m.TotalsFromJson(spec.input,t):m.TotalsLoad(bytes(spec.wide),t);
 expect(plain!==null&&JSON.stringify(vals(plain))===JSON.stringify(expected)&&r.clamped===6,"element/count clamps "+mode+" "+JSON.stringify(r)+" "+JSON.stringify(plain));
 expect(limited!==null&&JSON.stringify(vals(limited))===JSON.stringify(totalExpected)&&t.clamped===(mode==="wire"?10:8),"total prefix "+mode+" "+JSON.stringify(t)+" "+JSON.stringify(limited));
 expect(t.clampedField==="Totals.a","first named clamp "+mode+" "+t.clampedField);
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
 expect(m.ListsFromJson(JSON.stringify({[field]:["x\u0000y"]}),r)===null&&r.malformed,"JSON zero "+field);
}
for (const field of ["a","b","c","d"]) {
 const source=new m.Lists(); source[field]=[{value:"xyz"}]; const wire=m.ListsSave(source);
 const at=Buffer.from(wire).indexOf(Buffer.from("xyz")); expect(at>=0,"mutation marker");
 for (const byte of [0,255]) { const bad=wire.slice();bad[at]=byte;const r=new m.TableVariableReport();
  expect(m.ListsLoad(bad,r)===null&&r.malformed,"malformed wire text "+field+" "+byte);
 }
}
const req=new m.Required();expect(m.RequiredSave(req)!==null,"required empty text list writer");
expect(m.RequiredFromJson("{}",new m.TableVariableReport())===null,"required list missing");
const emptyReq=m.RequiredFromJson('{"a":[]}',new m.TableVariableReport());
expect(emptyReq!==null&&hex(m.RequiredSave(emptyReq))===hex(m.RequiredSave(req)),"required empty list JSON");
expect(!("ListsATextSave" in m)&&!("ListsATextOpen" in m),"synthetic record exposed as root");
console.log("TEXT LIST OK");
`
