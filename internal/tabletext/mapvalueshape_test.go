package tabletext_test

import (
	"bytes"
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"path/filepath"
	"testing"
)

// A valid map key supplies the slot. A well-formed wrong-shape value is
// skipped, leaves the declared default, and counts one kind mismatch (§16.2).
func TestMapValueWrongShapeUsesDefault(t *testing.T) {
	t.Parallel()
	fixture := "../../testdata/conformance/feature-witnesses/map-value-shape"
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
		Name, Input, Expected string
		Mismatch, Duplicate   int
		Malformed             bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 14 {
		t.Fatalf("wrong-shape corpus has %d cases, want 14", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			value := m.New(m.Lookup("Maps"))
			var report tabletext.Report
			ok := m.Read(value, []byte(tc.Input), &report)
			want := tabletext.Report{KindMismatch: tc.Mismatch, Duplicate: tc.Duplicate, Malformed: tc.Refused}
			if ok == tc.Refused || report != want {
				t.Fatalf("read ok=%t report=%+v, want ok=%t report=%+v", ok, report, !tc.Refused, want)
			}
			if tc.Refused {
				return
			}
			expected := m.New(m.Lookup("Maps"))
			var expectedReport tabletext.Report
			if !m.Read(expected, []byte(tc.Expected), &expectedReport) || !expectedReport.Silent() {
				t.Fatalf("expected value is not clean: %+v", expectedReport)
			}
			got, err := m.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			wantText, err := m.Write(expected)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wantText) {
				t.Fatalf("map entry/default/presence changed: got %s want %s", got, wantText)
			}
		})
	}
}
