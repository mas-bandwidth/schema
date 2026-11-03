package jstable

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJavaScriptBlobSharedReportCensus(t *testing.T) {
	slowtest.Gate(t, "native JavaScript blob sharing report")
	dir := "../../../testdata/conformance/feature-witnesses/blob-shared-report"
	source, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Wire, Saved    string
		Shared, KindMismatch int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 {
		t.Fatalf("blob report corpus has %d rows, want 9", len(rows))
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	for _, row := range rows {
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			t.Fatal(err)
		}
		v := m.New(u.Tables["Root"])
		for _, retaining := range []bool{false, true} {
			r := tabletext.Report{}
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			for repeat := 1; repeat <= 2; repeat++ {
				var ok bool
				var err error
				if retaining {
					ok, err = tablewire.DecodeRetain(m, v, wire, store, &r)
				} else {
					ok, err = tablewire.Decode(m, v, wire, &r)
				}
				if !ok || err != nil || r.Malformed || r.BlobShared != row.Shared*repeat || r.KindMismatch != row.KindMismatch*repeat {
					t.Fatalf("%s neutral retain=%v: %+v %v", row.Name, retaining, r, err)
				}
			}
		}
		saved, err := tablewire.Encode(m, v)
		if row.Shared == 0 && (err != nil || hex.EncodeToString(saved) != row.Saved) {
			t.Fatalf("%s canonical save: %x %v", row.Name, saved, err)
		}
		if row.Shared != 0 && (err == nil || !strings.Contains(err.Error(), "blob_shared")) {
			t.Fatalf("%s writer: %v", row.Name, err)
		}
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsBlobSharedReportProbe, rows)
	if !strings.Contains(out, "BLOB REPORT OK 72\n") {
		t.Fatal(out)
	}
}

const jsBlobSharedReportProbe = `
import fs from "node:fs";
import { pathToFileURL } from "node:url";
const cases=JSON.parse(fs.readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const bytes=s=>new Uint8Array(Buffer.from(s,"hex"));
const hex=b=>Buffer.from(b).toString("hex");
const failures=[];
let checks=0;
function check(ok,why){if(!ok)failures.push(why);}
for(const c of cases)for(const retaining of [false,true])for(const history of [0,7]){
 const r=new m.TableVariableReport(),store=new m.TableVariableRetain(new Uint8Array(65536),new Array(1024));
 check(r.blobShared===0,"new report counter "+c.Name);
 r.blobShared=history;r.unknown=11;r.widened=13;r.clamped=17;
 for(let repeat=1;repeat<=2;repeat++){
  const wire=bytes(c.Wire);
  const v=retaining?m.RootLoadRetain(wire,store,r):m.RootLoad(wire,r);
  const why=c.Name+" retain="+retaining+" history="+history+" repeat="+repeat;
  check(v!==null&&!r.malformed&&!r.refused&&r.blobShared===history+c.Shared*repeat&&r.kindMismatch===c.KindMismatch*repeat,"census "+why+" "+JSON.stringify(r));
  check(r.unknown===11&&r.widened===13&&r.clamped===17,"history preserved "+why);
  if(v!==null){
   if(["shared_empty","shared_equal","repeated_field_naming"].includes(c.Name)){
    check(v.a instanceof String&&v.a===v.b&&v.c===v.d,"one shared object per blob "+why);
   }
   if(c.Name.startsWith("distinct_")){
    check(typeof v.a==="string"&&typeof v.b==="string"&&v.c!==v.d,"distinct equal blob identities "+why);
   }
   if(c.Name==="all_null")check(v.a===null&&v.b===null&&v.c===null&&v.d===null,"null slots "+why);
   if(c.Shared===0){
    check(hex(m.RootSave(v))===c.Saved,"distinct/null save "+why);
    if(retaining)check(hex(m.RootSaveRetain(v,store,r))===c.Saved,"distinct/null retained save "+why);
   }else{
    check(m.RootSave(v)===null&&m.RootToJson(v)===null,"shared writer refusal "+why);
    check(JSON.stringify(m.RootMeasureReason(v))===JSON.stringify([-1,"blob_shared"]),"shared named reason "+why);
    if(retaining)check(m.RootSaveRetain(v,store,r)===null,"shared retained writer refusal "+why);
   }
  }
  checks++;
 }
 check(m.RootLoad(bytes(c.Wire),null)!==null,"nil report remains tolerant "+c.Name);
}
if(failures.length)throw new Error(failures.join("\n"));
console.log("BLOB REPORT OK "+checks);
`
