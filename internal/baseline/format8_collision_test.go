package baseline_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Two proposed renderings used "schema-tables-baseline 8" for different facts.
// Both fixtures describe before.schema. Only one recorded the count minimum.
// Its disappearance must be diagnosed, or the older file must require migration.
func TestFormat8CannotSilentlyLoseCountMinimum(t *testing.T) {
	fixture := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "format8-collision", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := unit(t, string(fixture("before.schema")))
	after := unit(t, string(fixture("after.schema")))
	saved := fixture("old.save")

	// Reconstruct the old author's value separately from the persisted bytes.
	old := tabletext.NewModel(before)
	value := old.New(old.Lookup("Root"))
	var placed tabletext.Report
	if !old.Read(value, fixture("old-input.json"), &placed) {
		t.Fatalf("old writer cannot place its input: %+v", placed)
	}
	encoded, err := tablewire.Encode(old, value)
	if err != nil || !bytes.Equal(encoded, saved) {
		t.Fatalf("old writer produced %x, want saved bytes %x; err=%v", encoded, saved, err)
	}
	for _, tc := range []struct {
		schema *ir.Unit
		want   string
	}{{before, "old-decoded.json"}, {after, "new-decoded.json"}} {
		model := tabletext.NewModel(tc.schema)
		dst := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, dst, saved, &report); !ok || err != nil {
			t.Fatalf("%s: old file refused: %v, %+v", tc.want, err, report)
		}
		got, err := model.Write(dst)
		if err != nil || compact(string(got)) != compact(string(fixture(tc.want))) {
			t.Fatalf("%s: old file decoded as %s; err=%v", tc.want, got, err)
		}
	}

	current := []byte(baseline.Render(before).Text())
	for _, tc := range []struct {
		name string
		held []byte
	}{
		{"current", current},
		{"format8-bits", fixture("bits-width.baseline")},
		{"format8-count", fixture("count-minimum.baseline")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, baseline.FileName), tc.held, 0o600); err != nil {
				t.Fatal(err)
			}
			paths := []string{filepath.Join(dir, "Root.schema")}
			if tc.name == "current" {
				if warnings, refusals := baseline.Check(before, paths); len(warnings)+len(refusals) != 0 {
					t.Fatalf("current rendering fails its unchanged source: warnings=%v, refusals=%v", warnings, refusals)
				}
			}
			if warnings, refusals := baseline.Check(after, paths); len(refusals) == 0 {
				t.Fatalf("old omitted items decoded as [0,0], now []; expected a compatibility or migration refusal, got warnings %v", warnings)
			}
		})
	}
}
