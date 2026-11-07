package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaScriptNestedRequiredGuards(t *testing.T) {
	slowtest.Gate(t, "node nested required guards and canonical field presence")
	const base = "../../../testdata/conformance/feature-witnesses/required/nested-guards/"
	source, e := os.ReadFile(base + "Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(source))
	pin := func(root string, names []string, value uint32) string {
		wire := []byte{1}
		var ids []uint64
		for i, name := range names {
			var field *ir.Field
			for _, f := range u.Tables[root].Fields {
				if f.Name == name {
					field = f
					break
				}
			}
			if field == nil {
				t.Fatal(name)
			}
			wire = append(wire, byte(i+1), byte(ir.TableWireFieldKind(field)))
			if field.Type.Kind == ir.TBool {
				wire = append(wire, 1)
			} else {
				wire = binary.LittleEndian.AppendUint32(wire, value)
			}
			ids = append(ids, ir.TableFieldWireId(field))
		}
		wire = append(wire, 0)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		return hex.EncodeToString(wire)
	}
	var cases []struct {
		Root    string   `json:"root"`
		Input   string   `json:"input"`
		Wire    string   `json:"wire"`
		Fields  []string `json:"fields"`
		Value   uint32   `json:"value"`
		Missing string   `json:"missing,omitempty"`
	}
	raw, e := os.ReadFile(base + "cases.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	if len(cases) != 12 {
		t.Fatalf("got %d corpus rows", len(cases))
	}
	for i := range cases {
		c := &cases[i]
		if c.Missing == "" {
			c.Wire = pin(c.Root, c.Fields, c.Value)
		}
	}
	files, e := generateVariableFiles(u, nil)
	if e != nil {
		t.Fatal(e)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsNestedGuards, cases)
	if !strings.Contains(out, "NESTED_GUARDS_OK") {
		t.Fatal(out)
	}
}

const jsNestedGuards = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const check=(ok,why)=>{if(!ok)throw new Error(why);};
const hex=b=>b===null?"null":Buffer.from(b).toString("hex");
for(const c of cases){
 const r=new m.TableVariableReport();const v=m[c.root+"FromJson"](c.input,r);
 if(c.missing){check(v===null && r.missingRequired===1 && r.reason==="missing_required: "+c.missing,"nested missing "+JSON.stringify(c)+" "+JSON.stringify(r));continue;}
 check(v!==null,"valid guarded JSON "+JSON.stringify(r));
 check(hex(m[c.root+"Save"](v))===c.wire,"guarded canonical wire "+c.input+" got "+hex(m[c.root+"Save"](v))+" want "+c.wire);
 const back=m[c.root+"Load"](new Uint8Array(Buffer.from(c.wire,"hex")),new m.TableVariableReport());
 check(back!==null && hex(m[c.root+"Save"](back))===c.wire,"guarded wire roundtrip");
 check(JSON.stringify(JSON.parse(m[c.root+"ToJson"](v)))===JSON.stringify(JSON.parse(c.input)),"inactive JSON fields");
}
const stale=new m.P();stale.outer=false;stale.inner=true;stale.child=new m.Leaf();stale.child.x=7;
check(hex(m.PSave(stale))==="01000000000000000000","inactive branch must not number stale pointer node");
const pr=new m.TableVariableReport();check(m.PFromJson('{"outer":true,"inner":true}',pr)===null && pr.reason==="missing_required: child","nested pointer required");
console.log("NESTED_GUARDS_OK");
`
