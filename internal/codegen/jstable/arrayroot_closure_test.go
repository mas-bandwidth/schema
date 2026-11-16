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
 // Historical columns name conditions, not a tolerant-read permission (§6.5).
 const refused=c.malformed||c.mismatch>0||c.clamped>0;
 check((v===null)===refused, "read verdict "+c.text);
 if(!refused)check(!r.damaged&&r.err()===null&&r.unknown===0,"accepted report "+c.text);
 if(c.reason)check(r.reason===c.reason,"reason "+c.text);
 if(v!==null){
  const text=m[c.root+"ToJson"](v),obj=JSON.parse(text);
  if(c.root==="Nested"){
   check(JSON.stringify(obj.choice.items)==="[5]","union arm shape "+text);
  }else{
   check(JSON.stringify(obj)===JSON.stringify(JSON.parse(c.text)),"array values "+text);
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
 // Wrong shape or repetition refuses the whole read (§16.2, §6.5).
 const refused=!!c.malformed||c.mismatch>0||c.duplicate>0;
 check((v===null)===refused,"read verdict "+c.name);
 if(!refused)check(!r.damaged&&r.err()===null&&r.unknown===0,"accepted report "+c.name);
 if(c.expected){
  // Keep every declared-default/presence twin live even when input refuses.
  const er=new m.TableVariableReport(),expected=m.MapsFromJson(c.expected,er);
  check(expected!==null&&er.err()===null,"expected input "+c.name);
  const want=m.MapsToJson(expected),obj=JSON.parse(want),declared=JSON.parse(c.expected);
  for(const field of Object.keys(declared))for(const key of Object.keys(declared[field])){
   check(Object.hasOwn(obj[field],key),"present default key "+c.name+" "+field+"."+key);
  }
  if(v!==null)check(m.MapsToJson(v)===want,"present default "+c.name);
  const wire=m.MapsSave(expected),loaded=m.MapsLoad(wire,new m.TableVariableReport());
  check(loaded!==null&&m.MapsToJson(loaded)===want,"wire presence "+c.name);
 }
}
console.log("MAP_SHAPE_OK "+cases.length);
`
