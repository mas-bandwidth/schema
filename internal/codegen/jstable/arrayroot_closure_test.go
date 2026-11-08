package jstable

import (
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"strings"
	"testing"
)

func TestJavaScriptArrayRootClosureCorpus(t *testing.T) {
	slowtest.Gate(t, "node (array-root closure corpus)")
	base := "../../../testdata/conformance/feature-witnesses/array-root/closure/"
	source, err := os.ReadFile(base + "Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(source))
	data, err := os.ReadFile(base + "cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases any
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsArrayClosureDriver, cases)
	if !strings.Contains(out, "ARRAY_CLOSURE_OK 12") {
		t.Fatalf("closure corpus: %s", out)
	}
}

const jsArrayClosureDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const check=(p,s)=>{if(!p)throw new Error(s)};
for(const c of cases){
 const r=new m.TableVariableReport(),v=m[c.root+"FromJson"](c.text,r);
 check((v===null)===c.malformed, "read verdict "+c.text);
 check(r.mismatch===c.mismatch&&r.oor===c.clamped&&r.damaged===c.malformed,"report "+c.text+" "+JSON.stringify(r));
 if(c.reason)check(r.reason===c.reason,"reason "+c.text);
 if(v!==null){
  const text=m[c.root+"ToJson"](v),obj=JSON.parse(text);
  if(c.root==="Nested"){
   check(c.mismatch===0?JSON.stringify(obj.choice.items)==="[5]":JSON.stringify(obj.choice)==="{}","union arm shape "+text);
  }
  const wire=m[c.root+"Save"](v),loaded=m[c.root+"Load"](wire,new m.TableVariableReport());
  check(loaded!==null&&m[c.root+"ToJson"](loaded)===text,"wire preserved nested array "+c.text);
 }
}
console.log("ARRAY_CLOSURE_OK "+cases.length);
`

func TestJavaScriptMapValueShapeCorpus(t *testing.T) {
	slowtest.Gate(t, "node (map value shape corpus)")
	base := "../../../testdata/conformance/feature-witnesses/map-value-shape/"
	source, err := os.ReadFile(base + "Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(source))
	data, err := os.ReadFile(base + "cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases any
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsMapShapeDriver, cases)
	if !strings.Contains(out, "MAP_SHAPE_OK 14") {
		t.Fatalf("map shape corpus: %s", out)
	}
}

const jsMapShapeDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const check=(p,s)=>{if(!p)throw new Error(s)};
for(const c of cases){
 const r=new m.TableVariableReport(),v=m.MapsFromJson(c.input,r);
 check((v===null)===!!c.malformed,"read verdict "+c.name);
 check(r.mismatch===(c.mismatch||0)&&r.repeated===(c.duplicate||0)&&r.damaged===!!c.malformed,"report "+c.name+" "+JSON.stringify(r));
 if(v!==null){
  const er=new m.TableVariableReport(),expected=m.MapsFromJson(c.expected,er);
  check(expected!==null&&er.mismatch===0,"expected input "+c.name);
  const text=m.MapsToJson(v),want=m.MapsToJson(expected);
  check(text===want,"present default "+c.name+" "+text+" != "+want);
  const wire=m.MapsSave(v),loaded=m.MapsLoad(wire,new m.TableVariableReport());
  check(loaded!==null&&m.MapsToJson(loaded)===text,"wire presence "+c.name);
 }
}
console.log("MAP_SHAPE_OK "+cases.length);
`
