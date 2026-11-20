package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

const (
	goJSONWalkBegin = "// ---- json walk: begin ----"
	goJSONWalkEnd   = "// ---- json walk: end ----"
)

var requiredMapBodyRe = regexp.MustCompile(`(?s)var tableJSONRequiredByTable = map\[string\]\[\]string\{\n.*?\n\}`)

func extractGoJSONWalk(src []byte) string {
	text := string(src)
	i := strings.Index(text, goJSONWalkBegin)
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], goJSONWalkEnd)
	if j < 0 {
		return ""
	}
	end := i + j + len(goJSONWalkEnd)
	if end < len(text) && text[end] == '\n' {
		end++
	}
	return text[i:end]
}

// normalizeGoJSONWalk replaces only the per-unit required-field name map literal
// (emitted when unitHasRequired(u) holds) so the rest of the 3,800-line walker
// is compared byte for byte against schema-independent reference probes.
func normalizeGoJSONWalk(walk string) string {
	return requiredMapBodyRe.ReplaceAllString(walk, "var tableJSONRequiredByTable = map[string][]string{/* per-table required names */}")
}

type walkProfile struct {
	name string
	walk string
}

func parseProbeWalk(name, schema string) (walkProfile, error) {
	f, perrs := parser.Parse("Probe.schema", []byte(schema))
	if len(perrs) > 0 {
		return walkProfile{}, fmt.Errorf("probe %s parse: %w", name, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(schema), AST: f,
	}})
	if len(cerrs) > 0 {
		return walkProfile{}, fmt.Errorf("probe %s check: %w", name, cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		return walkProfile{}, fmt.Errorf("probe %s generate: %w", name, err)
	}
	var walk string
	for fname, content := range files {
		if strings.HasSuffix(fname, "TableJson.go") {
			if w := extractGoJSONWalk(content); w != "" {
				walk = normalizeGoJSONWalk(w)
			}
		}
	}
	if walk == "" {
		return walkProfile{}, fmt.Errorf("probe %s emitted no JSON walker", name)
	}
	return walkProfile{name: name, walk: walk}, nil
}

// canonicalWalkProfiles derives the closed set of generic Go JSON walker
// profiles from dummy walkprobe0 schemas (zero corpus table or field names).
// Pruning unreachable private helpers (prune.go) and the zero-cost feature
// seams (required, text-list/total, unbounded text, optional map, timestamp)
// partition units into these exact generic profiles without specializing any
// declaration to a unit's tables or fields.
var canonicalWalkProfiles = sync.OnceValues(func() ([]walkProfile, error) {
	specs := []struct {
		name   string
		schema string
	}{
		{
			name: "fixed-only",
			schema: `package walkprobe0
fixed table ProbeFixed { x int32 }
`,
		},
		{
			name: "variable-only",
			schema: `package walkprobe0
table ProbeNode { x int32 }
table ProbeVar { p *ProbeNode }
`,
		},
		{
			name: "fixed-and-variable",
			schema: `package walkprobe0
fixed table ProbeFixed { x int32 }
table ProbeVar { p *ProbeFixed }
`,
		},
		{
			name: "fixed-and-variable-text-forms",
			schema: `package walkprobe0
fixed table ProbeFixed { x int32 }
table ProbeVar { items [..2]string(8) }
`,
		},
		{
			name: "variable-all-seams",
			schema: `package walkprobe0
table ProbeLeaf { x int32 }
table ProbeRoot {
	req int32 | required
	lines [..4]string(8) | total=16
	words []string
	opt ?map[int32]ProbeLeaf
	ts timestamp
}
table ProbeArr | json="array" {
	vals []int32
}
`,
		},
		{
			name: "variable-guarded-required-all-seams",
			schema: `package walkprobe0
table ProbeLeaf { x int32 }
table ProbeRoot {
	armed bool
	if armed {
		req int32 | required
	}
	lines [..4]string(8) | total=16
	words []string
	opt ?map[int32]ProbeLeaf
	ts timestamp
}
table ProbeArr | json="array" {
	vals []int32
}
`,
		},
	}
	out := make([]walkProfile, 0, len(specs))
	for _, sp := range specs {
		pr, err := parseProbeWalk(sp.name, sp.schema)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
})

func checkGoJSONWalkFiles(unit string, files map[string][]byte, profiles []walkProfile) (string, string, error) {
	var jsonFiles []string
	for name := range files {
		if strings.HasSuffix(name, "TableJson.go") {
			jsonFiles = append(jsonFiles, name)
		}
	}
	sort.Strings(jsonFiles)
	var walkers []struct {
		file string
		walk string
	}
	for _, fname := range jsonFiles {
		if w := extractGoJSONWalk(files[fname]); w != "" {
			walkers = append(walkers, struct{ file, walk string }{fname, w})
		}
	}
	if len(jsonFiles) > 0 && len(walkers) != 1 {
		return "", "", fmt.Errorf("GENERIC-WALK GATE FAILED: unit %s carries %d walkers, not one", unit, len(walkers))
	}
	if len(walkers) == 0 {
		return "", "", nil
	}
	norm := normalizeGoJSONWalk(walkers[0].walk)
	for _, pr := range profiles {
		if norm == pr.walk {
			return walkers[0].file, pr.name, nil
		}
	}
	return walkers[0].file, "", fmt.Errorf("GENERIC-WALK GATE FAILED: the walker in %s/%s is not any generic walker profile (specialised to unit)", unit, walkers[0].file)
}

func checkGoJSONWalkDir(genRoot, extractOut string, profiles []walkProfile) (int, map[string]int, error) {
	entries, err := os.ReadDir(genRoot)
	if err != nil {
		return 0, nil, err
	}
	if extractOut != "" {
		if err := os.RemoveAll(extractOut); err != nil {
			return 0, nil, err
		}
		if err := os.MkdirAll(extractOut, 0o755); err != nil {
			return 0, nil, err
		}
	}
	counts := map[string]int{}
	total := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		unit := ent.Name()
		uDir := filepath.Join(genRoot, unit)
		uEntries, err := os.ReadDir(uDir)
		if err != nil {
			return 0, nil, err
		}
		files := map[string][]byte{}
		for _, f := range uEntries {
			if f.IsDir() || !strings.HasSuffix(f.Name(), "TableJson.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(uDir, f.Name()))
			if err != nil {
				return 0, nil, err
			}
			files[f.Name()] = b
		}
		fname, profile, err := checkGoJSONWalkFiles(unit, files, profiles)
		if err != nil {
			return 0, nil, err
		}
		if fname == "" {
			continue
		}
		total++
		counts[profile]++
		if extractOut != "" {
			walk := extractGoJSONWalk(files[fname])
			outPath := filepath.Join(extractOut, unit+"."+fname+".txt")
			if err := os.WriteFile(outPath, []byte(walk), 0o644); err != nil {
				return 0, nil, err
			}
		}
	}
	if total == 0 {
		return 0, nil, fmt.Errorf("GENERIC-WALK GATE FAILED: no walker in any generated .go")
	}
	return total, counts, nil
}

// TestGoJsonWalkIsOneGenericRuntime holds the Go generic-walk property
// (docs/SPEC-TABLES.md §16): every unit emits at most one <Home>TableJson.go
// walker, and after helper pruning (prune.go) and the zero-cost feature seams
// every unit's walker is byte-identical to its schema-independent reference
// profile (zero per-table or per-field specialisation).
func TestGoJsonWalkIsOneGenericRuntime(t *testing.T) {
	t.Parallel()
	profiles, err := canonicalWalkProfiles()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..")
	for _, tc := range []struct {
		unit        string
		paths       []string
		wantProfile string
	}{
		{"a1", []string{"test/tables/A1.schema"}, "fixed-only"},
		{"v1", []string{"test/tables/V1.schema"}, "fixed-only"},
		{"blobs", []string{"tables/blobs"}, "variable-only"},
		{"p2", []string{"test/tables/P2.schema"}, "variable-only"},
		{"pointers", []string{"tables/pointers"}, "fixed-and-variable"},
		{"examples", []string{"tables/examples"}, "fixed-and-variable"},
		{"cooktorture", []string{"test/tables/cooktorture/CookTorture.schema"}, "fixed-and-variable-text-forms"},
		{"featurewitnesses", []string{"testdata/conformance/feature-witnesses/full/Features.schema"}, "variable-all-seams"},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			t.Parallel()
			u := loadUnitPaths(t, root, tc.paths)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("%s generate: %v", tc.unit, err)
			}
			_, gotProfile, err := checkGoJSONWalkFiles(tc.unit, files, profiles)
			if err != nil {
				t.Fatal(err)
			}
			if gotProfile != tc.wantProfile {
				t.Fatalf("%s matched profile %q, want %q", tc.unit, gotProfile, tc.wantProfile)
			}
		})
	}

	if genDir := os.Getenv("SCHEMA_GO_JSON_WALK_DIR"); genDir != "" {
		total, counts, err := checkGoJSONWalkDir(genDir, os.Getenv("SCHEMA_GO_JSON_WALK_OUT"), profiles)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("tables Go generic-walk gate: one walker per unit across %d units (%v)", total, counts)
	}
}

func TestGoJsonWalkNegativeControl(t *testing.T) {
	t.Parallel()
	profiles, err := canonicalWalkProfiles()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..")
	blobsFiles, err := Generate(loadUnitPaths(t, root, []string{"tables/blobs"}))
	if err != nil {
		t.Fatal(err)
	}

	// 1. A per-unit comment/line inside the walker fails the gate.
	sabotaged := map[string][]byte{}
	for k, v := range blobsFiles {
		if strings.HasSuffix(k, "TableJson.go") {
			sabotaged[k] = []byte(strings.Replace(string(v), "tableJSONMaxDepth = 128", "tableJSONMaxDepth = 128 // specialised", 1))
		} else {
			sabotaged[k] = v
		}
	}
	if _, _, err := checkGoJSONWalkFiles("blobs", sabotaged, profiles); err == nil || !strings.Contains(err.Error(), "GENERIC-WALK GATE FAILED: the walker in blobs/") {
		t.Fatalf("specialised walker was not refused: %v", err)
	}

	// 2. A second walker file in one unit fails the gate.
	doubled := map[string][]byte{}
	for k, v := range blobsFiles {
		doubled[k] = v
		if strings.HasSuffix(k, "TableJson.go") {
			doubled["ExtraTableJson.go"] = v
		}
	}
	if _, _, err := checkGoJSONWalkFiles("blobs", doubled, profiles); err == nil || !strings.Contains(err.Error(), "carries 2 walkers, not one") {
		t.Fatalf("second walker in one unit was not refused: %v", err)
	}

	// 3. Empty generated directory fails the gate.
	emptyDir := t.TempDir()
	if _, _, err := checkGoJSONWalkDir(emptyDir, "", profiles); err == nil || !strings.Contains(err.Error(), "no walker in any generated .go") {
		t.Fatalf("empty directory was not refused: %v", err)
	}
}
