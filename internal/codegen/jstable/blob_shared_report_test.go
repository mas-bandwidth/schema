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
		OK                   bool
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 {
		t.Fatalf("blob report corpus has %d rows, want 9", len(rows))
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	for i := range rows {
		row := &rows[i]
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			t.Fatal(err)
		}
		expectRead := row.Name == "distinct_empty" || row.Name == "distinct_equal" || row.Name == "all_null"
		for _, retaining := range []bool{false, true} {
			value := m.New(u.Tables["Root"])
			var report tabletext.Report
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			var ok bool
			if retaining {
				ok, err = tablewire.DecodeRetain(m, value, wire, store, &report)
			} else {
				ok, err = tablewire.Decode(m, value, wire, &report)
			}
			if ok != expectRead || (ok && err != nil) || (!ok && !tablewire.ReadRefusal(err)) {
				t.Fatalf("%s retain=%v: ok=%v err=%v report=%+v", row.Name, retaining, ok, err, report)
			}
			if !ok && retaining && store.Used() != 0 {
				t.Fatalf("%s retained a refused input", row.Name)
			}
			if ok {
				saved, err := tablewire.Encode(m, value)
				if err != nil || hex.EncodeToString(saved) != row.Saved {
					t.Fatalf("%s save: %x %v", row.Name, saved, err)
				}
			}
		}
		row.OK = expectRead
	}

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsBlobSharedReportProbe, rows)
	if !strings.Contains(out, "BLOB REPORT OK 36\n") {
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
let checks=0;
for(const c of cases)for(const retaining of [false,true]){
 const r=new m.TableVariableReport(),store=new m.TableVariableRetain(new Uint8Array(65536),new Array(1024));
 for(let repeat=0;repeat<2;repeat++){
  const v=retaining?m.RootLoadRetain(bytes(c.Wire),store,r):m.RootLoad(bytes(c.Wire),r);
  if((v!==null)!==c.OK)throw new Error(c.Name+" wrong verdict retain="+retaining);
  if(v!==null){
   if(hex(m.RootSave(v))!==c.Saved)throw new Error(c.Name+" wrong save");
   if(retaining && hex(m.RootSaveRetain(v,store,r))!==c.Saved)throw new Error(c.Name+" wrong retained save");
   if(c.Name==="distinct_equal"){
    if(typeof v.a!=="string" || typeof v.b!=="string" || v.c===v.d)throw new Error("distinct blob identities collapsed");
    v.d=v.c;
    if(m.RootSave(v)!==null || m.RootToJson(v)!==null)throw new Error("shared blob writer accepted");
    if(retaining && m.RootSaveRetain(v,store,r)!==null)throw new Error("shared blob retained writer accepted");
   }
   if(c.Name==="all_null" && (v.a!==null||v.b!==null||v.c!==null||v.d!==null))throw new Error("null slots changed");
  } else if(retaining && (store.used!==0||store.count!==0))throw new Error(c.Name+" retained refused data");
  checks++;
 }
}
console.log("BLOB REPORT OK "+checks);
`
