package gotable

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Generated Go independently checks the explicit §16.10 closure outcomes.
func TestArrayRootClosureNativeGo(t *testing.T) {
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/array-root/closure")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Root, Text        string
		Mismatch, Clamped int
		Malformed         bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("package probe\nimport(\"strings\";\"testing\")\nfunc TestCorpus(t *testing.T){\n")
	for i, tc := range cases {
		fmt.Fprintf(&body, `{
var b %sBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown()
var report TableReport;ok:=%sFromJSON(&b,[]byte(%q),&report)
if ok!=%t || report.Refusal!=%d || report.Refusal!=%d || (report.Refusal.Error() == "wire_damaged" || report.Refusal.Error() == "malformed")!=%t {t.Fatalf("case %d: ok=%%v report=%%+v",ok,report)}
`, tc.Root, tc.Root, tc.Text, !tc.Malformed, tc.Mismatch, tc.Clamped, tc.Malformed, i)
		if tc.Root == "Nested" && tc.Mismatch == 0 {
			body.WriteString(`if !b.Lock(){t.Fatal("lock")};out:=make([]byte,NestedToJSONMeasure(b.AsConst()));if NestedToJSON(b.AsConst(),out)!=int64(len(out)){t.Fatal("write")}
if !strings.Contains(strings.Join(strings.Fields(string(out)),""),` + "`" + `"choice":{"items":[5]}` + "`" + `){t.Fatalf("array-root union arm dropped: %s",out)}
`)
		}
		body.WriteString("}\n")
	}
	body.WriteString("}\n")
	out, err := runGeneratedResult(t, string(schema), body.String(), "-timeout=0")
	if err != nil {
		t.Fatalf("generated Go array-root closure: %v\n%s", err, out)
	}
}
