package rusttable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Map[K]?T presence is distinct from ?map[K]T field presence: only null is
// absent; every other JSON shape supplies a present value (§16.2).
func TestRustMapValueShapeNative(t *testing.T) {
	slowtest.Gate(t, "rustc (map value shape and optional presence)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/map-value-shape")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input, Expected string
		Mismatch, Duplicate   int
		Malformed             bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 14 {
		t.Fatalf("map value shape corpus has %d cases, want 14", len(cases))
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	var body strings.Builder
	for i, tc := range cases {
		fmt.Fprintf(&body, `{
let mut report=TableReport::default();
let result=maps_from_json(%q,&mut report);
assert_eq!((result.is_some(),report.mismatch,report.repeated,report.damaged),(%t,%d,%d,%t),"map value %s");
`, tc.Input, !tc.Malformed, tc.Mismatch, tc.Duplicate, tc.Malformed, tc.Name)
		if !tc.Malformed {
			value := m.New(u.Tables["Maps"])
			var report tabletext.Report
			if !m.Read(value, []byte(tc.Expected), &report) || !report.Silent() {
				t.Fatalf("expected %s: %+v", tc.Name, report)
			}
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "assert_eq!(maps_save(&result.unwrap()),vec!%s,\"map value bytes %s\");\n", rustByteVector(wire), tc.Name)
		}
		fmt.Fprintf(&body, "println!(\"case %d ok\");}\n", i)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("map value shape: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatalf("missing map value cases: %s", out)
	}
}

func rustByteVector(data []byte) string {
	values := make([]string, len(data))
	for i, b := range data {
		values[i] = fmt.Sprint(b)
	}
	return "[" + strings.Join(values, ",") + "]"
}
