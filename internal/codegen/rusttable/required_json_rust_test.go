package rusttable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRustRequiredJSONNative(t *testing.T) {
	slowtest.Gate(t, "rustc (Required JSON named presence and nested guard closure)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/required/rust-json")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	type requiredCase struct {
		Name, Root, Input, Reason string
		KindMismatch, Duplicate   int
	}
	var cases []requiredCase
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(fixture, "../nested-guards")
	nestedSchema, err := os.ReadFile(filepath.Join(nested, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	nestedData, err := os.ReadFile(filepath.Join(nested, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nestedCases []struct{ Root, Input, Missing string }
	if err = json.Unmarshal(nestedData, &nestedCases); err != nil {
		t.Fatal(err)
	}
	for i, tc := range nestedCases {
		reason := ""
		if tc.Missing != "" {
			reason = "missing_required: " + tc.Missing
		}
		cases = append(cases, requiredCase{Name: fmt.Sprintf("shared_nested_guard_%d", i), Root: tc.Root, Input: tc.Input, Reason: reason})
	}
	schema = append(schema, []byte(strings.TrimPrefix(string(nestedSchema), "package probe"))...)
	many := "\ntable Many {\n"
	for i := range 512 {
		many += fmt.Sprintf(" f%d int32\n", i)
	}
	many += " last int32 | required\n}\n"
	cases = append(cases, requiredCase{Name: "slot513_missing", Root: "Many", Input: "{}", Reason: "missing_required: last"},
		requiredCase{Name: "slot513_duplicate", Root: "Many", Input: `{"last":0,"last":7}`, Duplicate: 1})
	u := unitFrom(t, string(schema)+many)
	m := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString("let mut failures=Vec::<String>::new();\n")
	// STRICT-ONLY (docs/plans/strict-only.md): only the verdict is pinned, and
	// the Go engine reading the same text is the oracle. A kind mismatch or a
	// repeated key refuses the whole read like a missing required field.
	for _, tc := range cases {
		v := m.New(u.Tables[tc.Root])
		var rep tabletext.Report
		ok := m.Read(v, []byte(tc.Input), &rep)
		fmt.Fprintf(&body, `{
let mut report=TableReport::default();
let value=%s_from_json(%q,&mut report);
if value.is_some()!=%t || (value.is_none() && !(report.damaged || report.refused)) {
 failures.push(format!("%s: value={} report={:?}",value.is_some(),report));
}
println!("case %s ok");
}
`, ir.RustSnake(tc.Root), tc.Input, ok, tc.Name, tc.Name)
	}
	body.WriteString(`{
let mut report=TableReport::default();
report.missing_required=7;
assert!(required_value_from_json("{}", &mut report).is_none());
assert_eq!(report.missing_required,8);
assert!(required_value_from_json("{\"a\":0,\"b\":0}",&mut report).is_some());
assert_eq!(report.missing_required,8);
assert!(!report.refused && report.missing_required_fields.is_empty() && report.reason==TableReason::None);
for text in ["null","42","[]","\"x\""] {
 assert!(required_value_from_json(text,&mut TableReport::default()).is_none(),"wrong root {}",text);
}
}
`)
	body.WriteString("assert!(failures.is_empty(),\"{}\",failures.join(\"\\n\"));\n")
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("Required JSON native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatalf("missing cases: %s", out)
	}
}
