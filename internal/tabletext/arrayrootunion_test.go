package tabletext_test

import (
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"path/filepath"
	"testing"
)

// §16.10 explicitly gives array roots their array shape in union arms too.
// All nested wrong shapes are recoverable; top-level wrong shapes are named.
func TestArrayRootClosureShapes(t *testing.T) {
	t.Parallel()
	fixture := "../../testdata/conformance/feature-witnesses/array-root/closure"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), schema, 0600); err != nil {
		t.Fatal(err)
	}
	_, u := load(t, dir)
	m := tabletext.NewModel(u)
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
		t.Fatalf("array-root closure corpus has %d cases, want 12", len(cases))
	}
	for _, tc := range cases {
		value := m.New(u.Tables[tc.Root])
		var report tabletext.Report
		ok := m.Read(value, []byte(tc.Text), &report)
		want := tabletext.Report{KindMismatch: tc.Mismatch, Clamped: tc.Clamped, Malformed: tc.Refused, Reason: tc.Reason}
		if ok == tc.Refused || report != want {
			t.Errorf("%s %s: ok=%t report=%+v want ok=%t report=%+v", tc.Root, tc.Text, ok, report, !tc.Refused, want)
			continue
		}
		if tc.Root == "Nested" && tc.Mismatch == 0 {
			arm := field(t, value, "choice").Cell
			if arm.U != 1 || arm.Tab == nil || len(arm.Tab.Fields[0].Elems) != 1 || arm.Tab.Fields[0].Elems[0].I != 5 {
				t.Errorf("valid array-root arm was dropped: %+v", arm)
			}
		}
	}
}
