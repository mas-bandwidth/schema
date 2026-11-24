package cpptable

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestEveryGeneratedTableHeaderCompilesStandalone holds docs/SPEC-TABLES.md
// section 2 (the zero-cost rule) over the shared corpus: every unit the
// conformance manifest names generates C++ table headers, and each header
// compiles when it is the first and only thing a translation unit includes. A
// header that calls schema_assert without carrying the hook (an
// `#ifndef schema_assert` fallback) is refused by the compiler here, whichever
// of the write-contract checks emitted the call: a variable table with a
// ranged fixed(48,16) field once did, and the unit-specific conditions that
// decided the hook missed it.
//
// Functional (a C++ compiler): run with SCHEMA_SLOW=1.
func TestEveryGeneratedTableHeaderCompilesStandalone(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (every manifest unit header compiled on its own)")
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type unitRow struct {
		name  string
		paths []string
	}
	var units []unitRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "unit" {
			units = append(units, unitRow{f[1], f[2:]})
		}
	}
	// the shapes the manifest units do not reach: each is one section 5.1 write
	// contract check and nothing else (testdata/cpp-standalone)
	extra, err := os.ReadDir(filepath.Join(repo, "testdata", "cpp-standalone"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range extra {
		if e.IsDir() {
			units = append(units, unitRow{"standalone_" + e.Name(), []string{filepath.Join("testdata", "cpp-standalone", e.Name())}})
		}
	}
	if len(units) < 40 {
		t.Fatalf("the manifest names %d units: the corpus this test sweeps has gone missing", len(units))
	}
	cxx := cppReasonCompiler(t, "g++", "clang++")
	// the packet headers the table headers include take the serialize runtime
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize"))
	if err != nil {
		t.Fatal(err)
	}
	if v := os.Getenv("SERIALIZE"); v != "" {
		serialize = v
	}
	if _, err := os.Stat(filepath.Join(serialize, "serialize.h")); err != nil {
		t.Fatalf("the serialize runtime is not at %s (clone the pinned sibling or set SERIALIZE): %v", serialize, err)
	}
	headers := 0
	for _, row := range units {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			u := loadHeldUnitPaths(t, repo, row.paths)
			files, err := cpp.Generate(u)
			if err != nil {
				t.Fatalf("unit %s: the C++ packet backend refused it: %v", row.name, err)
			}
			tables, err := GenerateLineage(u, nil)
			if err != nil {
				t.Fatalf("unit %s: the C++ table backend refused it: %v", row.name, err)
			}
			maps.Copy(files, tables)
			var names []string
			for name := range files {
				if !strings.Contains(name, "/") && strings.HasSuffix(name, ".h") {
					names = append(names, name)
				}
			}
			slices.Sort(names)
			if len(names) == 0 {
				t.Fatalf("unit %s generated no header", row.name)
			}
			dir := t.TempDir()
			for name, body := range files {
				if strings.Contains(name, "/") {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, header := range names {
				tu := filepath.Join(dir, "tu_"+strings.TrimSuffix(header, ".h")+".cpp")
				if err := os.WriteFile(tu, []byte("#include \""+header+"\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := testproc.Command(t, cxx, "-std=c++17", "-fsyntax-only", "-I"+dir, "-I"+serialize, "-I"+filepath.Join(repo, "test", "tables"), tu).CombinedOutput()
				if err != nil {
					t.Errorf("unit %s: %s does not compile standalone: %v\n%.1500s", row.name, header, err, out)
				}
			}
		})
		headers++
	}
	if headers != len(units) {
		t.Fatalf("swept %d of %d units", headers, len(units))
	}
}
