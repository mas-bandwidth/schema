package baseline_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

func TestBitsWidthSavedValueCorpus(t *testing.T) {
	t.Parallel()
	const root = "testdata/bits-width"
	read := func(t *testing.T, rel string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := string(read(t, "before/Save.schema"))
	old := unit(t, before)
	live := unit(t, string(read(t, "after/Save.schema")))
	got := baseline.Diff(committed(t, before), baseline.Render(live), baseline.DefaultTokenPolicy)
	if len(got) != 1 || !find(got, baseline.Warn, "Save.flags", "bit width 7 -> 5") || !strings.Contains(got[0].What, "out_of_range") {
		t.Errorf("want one bit-width warning naming Save.flags and out_of_range, got:%s", summary(got))
	}

	c := compiler.New()
	for _, name := range []string{"historical", "retained"} {
		t.Run(name, func(t *testing.T) {
			wire := read(t, name+".bin")
			packed, _, report, err := c.Pack(old, "Save", filepath.Join(root, name))
			if err != nil || !report.Silent() || !bytes.Equal(packed, wire) {
				t.Fatalf("7-bit writer: got %x, want saved %x, report %+v, error %v", packed, wire, report, err)
			}
			if report, err := c.ReadReport(old, "Save", wire); err != nil || !report.Silent() {
				t.Fatalf("7-bit reader: report %+v, error %v", report, err)
			}
			dir := t.TempDir()
			report, err = c.UnpackOneFile(live, "Save", wire, dir)
			if name == "historical" {
				if err == nil || !strings.Contains(err.Error(), "out_of_range") {
					t.Errorf("5-bit reader: want out_of_range for 95, got %v", err)
				}
				return
			}
			if err != nil || !report.Silent() {
				t.Fatalf("5-bit reader: report %+v, error %v", report, err)
			}
			resaved, _, report, err := c.Pack(live, "Save", dir)
			if err != nil || !report.Silent() || !bytes.Equal(resaved, wire) {
				t.Errorf("retained 25 did not round-trip: got %x, want %x, report %+v, error %v", resaved, wire, report, err)
			}
		})
	}
}
