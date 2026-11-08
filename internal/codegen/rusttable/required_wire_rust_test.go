package rusttable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRustRequiredWireReadNative(t *testing.T) {
	t.Skip("layer 4 (rust): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "rustc (Required compatible wire presence and whole-record refusal)")
	keyedRequireRustc(t)

	fixture := "../../../testdata/conformance/feature-witnesses/required/wire-presence"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Name, Root, Hex, Missing string
		KindMismatch, Widened    int
		Malformed                bool
	}
	if err = json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 46 {
		t.Fatalf("wire presence corpus has %d cases, want 46", len(records))
	}
	type wireCase struct {
		name, root, missing string
		wire                []byte
		km, widened         int
		malformed           bool
	}
	var cases []wireCase
	for _, r := range records {
		wire, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatal(r.Name, err)
		}
		cases = append(cases, wireCase{r.Name, r.Root, r.Missing, wire, r.KindMismatch, r.Widened, r.Malformed})
		for _, name := range []string{"missing", "missing-b", "zero", "populated"} {
			if r.Name != "shared_"+name {
				continue
			}
			pin, err := os.ReadFile(filepath.Join(fixture, "..", name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pin, wire) {
				t.Fatalf("shared %s pin changed", name)
			}
		}
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString("let mut failures=Vec::<String>::new();\n")
	for _, tc := range cases {
		v := m.New(u.Tables[tc.root])
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, v, tc.wire, &rep)
		reason := ""
		count := 0
		if tc.missing != "" {
			reason = "missing_required: " + tc.missing
			count = 1
		}
		if ok != (count == 0 && !tc.malformed) || ok == rep.Refused || (ok && rep.Widened != tc.widened) {
			t.Fatalf("explicit neutral contract %s: ok=%v error=%v report=%+v wire=%x", tc.name, ok, err, rep, tc.wire)
		}
		fmt.Fprintf(&body, `{
let wire=vec![%s];
for retaining in [false,true] {
let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::new()};
let mut report=TableReport::default();report.missing_required=7;
let value=if retaining {%s_load_retain(&wire,&mut store,&mut report)} else {%s_load(&wire,&mut report)};
if value.is_some()!=%t || report.malformed!=%t || report.refused!=%t || report.missing_required!=%d || report.missing_required_reason().as_deref().unwrap_or("")!=%q || report.kind_mismatch!=%d || report.widened!=%d {
failures.push(format!("%s retaining={}: value={} report={:?}",retaining,value.is_some(),report));
}
if value.is_none() && (!store.bytes.is_empty() || !store.ids.is_empty()) {failures.push(String::from("%s retained partial state"));}
}
println!("case %s ok");
}
`, rustBytes(tc.wire), ir.RustSnake(tc.root), ir.RustSnake(tc.root), count == 0, tc.malformed, count != 0, 7+count, reason, tc.km, tc.widened, tc.name, tc.name, tc.name)
	}
	body.WriteString("assert!(failures.is_empty(),\"{}\",failures.join(\"\\n\"));\n")
	body.WriteString(`{
let mut report=TableReport::default();
assert!(required_value_from_json("{}",&mut report).is_none());
`)
	zero, err := os.ReadFile(filepath.Join(fixture, "..", "zero.bin"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&body, "let value=root_load(&vec![%s],&mut report);assert!(value.is_some());assert!(!report.refused && report.missing_required_fields.is_empty());assert_eq!(report.missing_required,1);}\n", rustBytes(zero))
	body.WriteString("assert!(failures.is_empty(),\"{}\",failures.join(\"\\n\"));\n")
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("Required wire native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatal(out)
	}
}
