package jstable

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const jsArrayRootSchema = `package probe

table Goal { x int32 }
table GoalList | json = "array" {
    goals [..2]Goal
}
table Holder {
    group GoalList
    groups []GoalList
    by_name map[string(8)]GoalList
    maybe ?GoalList
}
`

func TestJavaScriptArrayRootConstruct(t *testing.T) {
	t.Skip("layer 4 (js): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "node (it executes array-root JSON values)")
	t.Parallel()
	u := unitFrom(t, jsArrayRootSchema)
	m := tabletext.NewModel(u)
	type row struct {
		Name     string `json:"name"`
		Root     string `json:"root"`
		Input    string `json:"input"`
		Text     string `json:"text"`
		Wire     string `json:"wire"`
		OK       bool   `json:"ok"`
		Clamped  int    `json:"clamped"`
		Mismatch int    `json:"mismatch"`
		Reason   string `json:"reason"`
	}
	var cases []row
	for _, tc := range []struct{ name, root, input string }{
		{"empty", "GoalList", `[]`},
		{"populated", "GoalList", `[{"x":3},{"x":7}]`},
		{"bounded", "GoalList", `[{"x":3},{"x":7},{"x":11}]`},
		{"wrong-root", "GoalList", `{"goals":[{"x":3}]}`},
		{"damaged", "GoalList", `[{"x":3}`},
		{"nested", "Holder", `{"group":[{"x":9}],"groups":[[{"x":2}],[]]}`},
		{"nested-map", "Holder", `{"by_name":{"a":[{"x":5}],"b":[]}}`},
		{"optional-present", "Holder", `{"maybe":[{"x":13}]}`},
		{"optional-absent", "Holder", `{"maybe":null}`},
		{"wrong-nested", "Holder", `{"group":{"goals":[{"x":3}]}}`},
	} {
		value := m.New(u.Tables[tc.root])
		var rep tabletext.Report
		ok := m.Read(value, []byte(tc.input), &rep)
		c := row{Name: tc.name, Root: tc.root, Input: tc.input, OK: ok, Clamped: 0, Mismatch: 0, Reason: rep.Reason}
		if ok {
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			c.Wire = hex.EncodeToString(wire)
			txt, err := m.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			c.Text = string(txt) + "\n"
		}
		cases = append(cases, c)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsArrayRootDriver, cases)
	for _, c := range cases {
		if !strings.Contains(out, c.Name+" OK\n") {
			t.Errorf("missing %s\n%s", c.Name, out)
		}
	}
}

const jsArrayRootDriver = `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const cases=JSON.parse(readFileSync(process.argv[2],"utf8"));
const m=await import(pathToFileURL(process.argv[3]).href);
const hex=b=>b==null ? "null" : Buffer.from(b).toString("hex");
let failed=false;
for(const c of cases){
 const r=new m.TableVariableReport();const v=m[c.root+"FromJson"](c.input,r);
 const ok=v!==null;
 if(ok!==c.ok || r.oor!==c.clamped || r.mismatch!==c.mismatch || (c.reason && r.reason!==c.reason)
   || (ok && (hex(m[c.root+"Save"](v))!==c.wire || m[c.root+"ToJson"](v)!==c.text))){
   console.log(c.name+" DIFF "+JSON.stringify({ok,r,text:ok?m[c.root+"ToJson"](v):null,want:c})); failed=true;
 }else{console.log(c.name+" OK");}
}
if(failed){process.exit(1);}
`
