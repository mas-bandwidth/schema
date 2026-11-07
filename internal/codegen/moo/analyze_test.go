package moo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// analyzeCorpora is every packet corpus the leg generates: each module and
// the runtime beside it must pass moo-analyze --!strict with NO output, no
// error and no lint warning (docs/PORT-MOO.md, THE GATES; zero warnings).
var analyzeCorpora = [][]string{
	{"examples"},
	{"examples128"},
	{"test", "packet-defaults"},
	{"test", "packet-finite"},
	{"test", "packet-text"},
	{"test", "packet-void"},
	{"test", "packet-wide"},
}

func analyzeDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mods []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".moo") && e.Name() != "inputs.moo" {
			mods = append(mods, e.Name())
		}
	}
	sort.Strings(mods)
	return moorun.Analyze(t, dir, mods...)
}

func TestMooGeneratedCodeIsAnalyzerClean(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (it analyzes the generated modules)")
	t.Parallel()
	for _, c := range analyzeCorpora {
		t.Run(strings.Join(c, "/"), func(t *testing.T) {
			t.Parallel()
			paths, _ := filepath.Glob(filepath.Join(repo(c...), "*.schema"))
			if len(paths) == 0 {
				t.Fatalf("no schema files under %s", filepath.Join(c...))
			}
			sort.Strings(paths)
			// examples*/ is one unit; a test/packet-* directory holds one
			// unit per file
			units := [][]string{paths}
			if c[0] == "test" {
				units = nil
				for _, p := range paths {
					units = append(units, []string{p})
				}
			}
			for _, unit := range units {
				dir, files := generateInto(t, unitFromFiles(t, unit))
				for name, data := range files {
					if !strings.HasPrefix(string(data), "--!strict\n") {
						t.Errorf("%s is not --!strict", name)
					}
				}
				if out := analyzeDir(t, dir); out != "" {
					t.Fatalf("moo-analyze is not silent on %v:\n%s", unit, out)
				}
			}
		})
	}
}

// TestMooAnalyzerGateNegativeControl plants one unused local into a
// generated module and requires the analyzer's output to name it: the gate
// can go red, and a lint warning (which moo-analyze exits 0 on) counts.
func TestMooAnalyzerGateNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (it analyzes the generated modules)")
	t.Parallel()
	dir, files := generateInto(t, unitFromDir(t, repo("examples")))
	src := string(files["Wire.moo"])
	src = strings.Replace(src, "\nlocal M = {}\n", "\nlocal plantedUnused = 1\nlocal M = {}\n", 1)
	writeFile(t, dir, "Wire.moo", src)
	out := analyzeDir(t, dir)
	if !strings.Contains(out, "LocalUnused") || !strings.Contains(out, "plantedUnused") {
		t.Fatalf("the analyzer gate did not flag the planted unused local:\n%s", out)
	}
	// Exactly its row: every diagnostic is the plant (the analyzer may name a
	// module once per path it reaches it by, never anything else).
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if !strings.Contains(line, "Wire.moo") || !strings.Contains(line, "LocalUnused") || !strings.Contains(line, "plantedUnused") {
			t.Fatalf("the analyzer reported something other than the plant:\n%s", out)
		}
	}
}

// TestMooRuntimeIsTheVendoredPin holds runtime/*.moo to SERIALIZE_PIN's
// sha256 lines: the runtime is serialize.moo v2.1.2 verbatim, never patched
// here (a runtime gap is fixed in serialize.moo).
func TestMooRuntimeIsTheVendoredPin(t *testing.T) {
	pin, err := os.ReadFile(filepath.Join("runtime", "SERIALIZE_PIN"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(pin), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && len(f[0]) == 64 {
			want[f[1]] = f[0]
		}
	}
	got := Runtime()
	if len(got) != 5 {
		t.Fatalf("runtime has %d modules, want serialize.moo's 5", len(got))
	}
	for name, data := range got {
		if want[name] == "" || want[name] != sha256hex(data) {
			t.Errorf("runtime/%s does not match SERIALIZE_PIN", name)
		}
	}
}
