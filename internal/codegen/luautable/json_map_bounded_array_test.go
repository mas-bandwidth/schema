package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// firstDiffOffset names the first offset where two hex strings differ, or
// "equal" when they match, plus each side's length.
func firstDiffOffset(a, b string) string {
	if a == b {
		return "equal"
	}
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return strings.TrimSpace(strings.Join([]string{
				"first differing byte", itoa(i / 2), "of", itoa(len(a) / 2), "vs", itoa(len(b) / 2),
			}, " "))
		}
	}
	return "one output is a prefix of the other"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestLuauJsonMapOfBoundedArrayAgreesWithTheCorpus pins the two corpus
// instances whose map VALUE is a bounded array (docs/SPEC-TABLES.md §4.2):
// map_crews ([..N]*T) and map_runs ([..N]T). Both JSON directions must agree
// byte for byte with the corpus, the way the field path already does for the
// other instances.
func TestLuauJsonMapOfBoundedArrayAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	units, _ := readManifest(t)
	for _, caseName := range []string{"map_crews", "map_runs"} {
		t.Run(caseName, func(t *testing.T) {
			t.Parallel()
			var row jsonRow
			for _, r := range jsonManifestRows(t) {
				if r.caseName == caseName {
					row = r
					break
				}
			}
			if row.caseName == "" {
				t.Fatalf("no manifest row for %s", caseName)
			}
			g := generateUnit(t, units, row.unit)
			if g.err != nil {
				t.Fatalf("%s does not generate: %v", row.unit, g.err)
			}
			text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", row.caseName+".json"))
			if err != nil {
				t.Fatal(err)
			}
			wire, err := os.ReadFile(filepath.Join(root, row.path))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, row.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text, "wire": wire})), "\n")
			parts := strings.Split(out, "\n")
			if len(parts) != 3 {
				t.Fatalf("bad output %q", out)
			}
			if parts[2] != "00" {
				t.Fatalf("a direction reported malformed (%s)", parts[2])
			}
			if parts[0] != hexBytes(wire) {
				t.Errorf("FromJson then Save != the wire: %s", firstDiffOffset(hexBytes(wire), parts[0]))
			}
			if parts[1] != hexBytes(text) {
				t.Errorf("Load then ToJson != the json text: %s", firstDiffOffset(hexBytes(text), parts[1]))
			}
		})
	}
}
