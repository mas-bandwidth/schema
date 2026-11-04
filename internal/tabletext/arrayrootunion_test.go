package tabletext_test

import (
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"path/filepath"
	"testing"
)

// §16.10 explicitly gives array roots their array shape in union arms too.
// Each corpus row's columns name the condition the case carries; under the one
// read (§6.5) any condition refuses the whole read, a wrong top-level shape
// with its named reason, and a case with none reads and writes back.
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
		refused := tc.Malformed || tc.Mismatch > 0 || tc.Clamped > 0
		if ok == refused || report.Refused != refused || (tc.Reason != "" && report.Reason != tc.Reason) {
			t.Errorf("%s %s: ok=%t report=%+v, want refused=%t reason %q", tc.Root, tc.Text, ok, report, refused, tc.Reason)
			continue
		}
		if refused {
			continue
		}
		if !report.Silent() {
			t.Errorf("%s %s: an accepted case counted %+v", tc.Root, tc.Text, report)
		}
		if _, err := m.Write(value); err != nil {
			t.Errorf("%s %s: an accepted value does not write: %v", tc.Root, tc.Text, err)
		}
		if tc.Root == "Nested" {
			arm := field(t, value, "choice").Cell
			if arm.U != 1 || arm.Tab == nil || len(arm.Tab.Fields[0].Elems) != 1 || arm.Tab.Fields[0].Elems[0].I != 5 {
				t.Errorf("valid array-root arm was dropped: %+v", arm)
			}
		}
	}
}
