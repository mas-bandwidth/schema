package rusttable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Array-root text is an array in every by-value position, while wire and cook
// remain unchanged. Wrong top-level shapes are named damage; nested ones are
// recoverable kind mismatches (§16.10).
func TestRustArrayRootNative(t *testing.T) {
	t.Skip("layer 4 (rust): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "rustc (array-root JSON closure and cook)")
	keyedRequireRustc(t)
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
		Root, Text, Reason string
		Mismatch, Clamped  int
		Malformed          bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatalf("array-root corpus has %d cases, want 12", len(cases))
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	work := t.TempDir()
	var body strings.Builder
	for i, tc := range cases {
		value := m.New(u.Tables[tc.Root])
		var report tabletext.Report
		accepted := m.Read(value, []byte(tc.Text), &report)
		if accepted == tc.Malformed || accepted == report.Refused {
			t.Fatalf("neutral corpus case %d disagrees with explicit SPEC outcome: accepted=%t report=%+v", i, accepted, report)
		}
		reason := "none"
		if strings.HasPrefix(report.Reason, "json_root_not_array:") {
			reason = "json_root_not_array"
		}
		fmt.Fprintf(&body, `{
let mut report=TableReport::default();
let result=%s(%q,&mut report);
assert_eq!((result.is_some(),report.kind_mismatch,report.clamped,report.malformed),(%t,%d,%d,%t),"array-root case %d");
assert_eq!(report.reason.name(),%q,"array-root reason %d");
`, fn(tc.Root, "from_json"), tc.Text, accepted, tc.Mismatch, tc.Clamped, tc.Malformed, i, reason, i)
		fmt.Fprintf(&body, "assert_eq!(report.text_error,%q,\"array-root diagnostic %d\");\n", report.Reason, i)
		if accepted {
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, `let v=result.unwrap();
assert_eq!(%s(&v),vec!%s,"array-root wire %d");
let text=%s(&v);
let again=%s(&text,&mut TableReport::default()).expect("array-root read own text");
assert_eq!(%s(&again),vec!%s,"array-root rendered value %d");
`, fn(tc.Root, "save"), rustByteVector(wire), i, fn(tc.Root, "to_json"), fn(tc.Root, "from_json"), fn(tc.Root, "save"), rustByteVector(wire), i)
			if tc.Root != "Nested" {
				fmt.Fprintf(&body, "assert!(text.trim_start().starts_with('['),\"array root wrote an object\");\n")
			}
			for _, big := range []bool{false, true} {
				cooked, err := tablecook.Cook(m, value, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				order := 1
				if big {
					order = 2
				}
				path := filepath.Join(work, fmt.Sprintf("%d-%d.cook", i, order))
				if err := os.WriteFile(path, cooked, 0600); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&body, "assert_eq!(%s(&v,%d).unwrap(),std::fs::read(%q).unwrap(),\"array-root cook %d\");\n", fn(tc.Root, "cook_write"), order, path, i)
			}
		}
		fmt.Fprintf(&body, "println!(\"case %d ok\");}\n", i)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("array-root native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatalf("missing array-root cases: %s", out)
	}
}
