package lobstertable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countCapSchema is the measure unit of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv.
const countCapSchema = "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n"

// TestLobsterCountCapRefusal holds the strict read to the pinned answer for a
// `[]` or map count above the int32 storage cap (docs/SPEC-TABLES.md §2.9):
// count_over_extent_cap, the same rows Go and Goose answer, and the good row
// of that unit reads.
func TestLobsterCountCapRefusal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var rows []loadRow
	want := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "measure" || (!strings.Contains(f[0], "_cap3") && f[0] != "good") {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, loadRow{kind: "cap", name: f[0], unit: "probe", root: "Root", data: wire})
		want[f[0]] = f[3]
	}
	if len(rows) != 5 {
		t.Fatalf("%d cap rows and the good row, want 5", len(rows))
	}
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(countCapSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the count-cap unit: %v", err)
	}
	res := runStrictRows(t, g, rows)
	for i, row := range rows {
		if res.reason[i] != want[row.name] {
			t.Errorf("%s: strict read = %s, want %s", row.name, res.reason[i], want[row.name])
		}
	}
}
