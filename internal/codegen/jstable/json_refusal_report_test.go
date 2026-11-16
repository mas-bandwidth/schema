package jstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A semantic failure in an array must not be relabeled as JSON damage merely
// because the element reader stops. Explicit syntax errors remain damage.
func TestJavaScriptJsonArrayRefusalReport(t *testing.T) {
	slowtest.Gate(t, "node (JSON array refusal diagnostics)")
	u := unitFrom(t, `package probe
 table Root | json="array" { values [..3]int8 }
 `)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	const probe = `
import {pathToFileURL} from "node:url";
const m=await import(pathToFileURL(process.argv[3]).href);
const check=(p,s)=>{if(!p)throw new Error(s)};
for(const [text,reason] of [
 ["[128]",m.TableRefuseReason.OutOfRange],
 ['["bad"]',m.TableRefuseReason.KindMismatch],
 ["[1.0]",m.TableRefuseReason.KindMismatch],
 ["[1,2,3,4]",m.TableRefuseReason.OutOfRange],
 ["[}",m.TableRefuseReason.WireDamaged],
 ["[1,",m.TableRefuseReason.WireDamaged],
 ["{}",m.TableRefuseReason.WireDamaged],
]){
 const r=new m.TableVariableReport(),v=m.RootFromJson(text,r);
 check(v===null&&r.err()===reason,"refusal "+text+" "+r.err());
 check(r.damaged===(reason===m.TableRefuseReason.WireDamaged),"damage "+text);
}
const r=new m.TableVariableReport(),v=m.RootFromJson("[-128,0,127]",r);
check(v!==null&&r.err()===null&&!r.damaged,"lawful bounds");
check(JSON.stringify(JSON.parse(m.RootToJson(v)))==="[-128,0,127]","lawful values");
console.log("JSON_ARRAY_REPORT_OK");
`
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", probe, nil)
	if !strings.Contains(out, "JSON_ARRAY_REPORT_OK") {
		t.Fatalf("JSON report probe: %s", out)
	}
}
