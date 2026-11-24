package cpptable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestEveryGeneratedTableHeaderCompilesStandalone holds docs/SPEC-TABLES.md
// section 13.9 (the hooks table) over the shared corpus: every unit the
// conformance manifest names generates C++ table headers, and each Table.h and
// packet header compiles when it is the first and only thing a translation unit
// includes. A header that calls schema_assert without carrying the hook (an
// `#ifndef schema_assert` fallback) is refused by the compiler here, whichever
// of the write-contract checks emitted the call: a variable table with a
// ranged fixed(48,16) field once did, and the unit-specific conditions that
// decided the hook missed it.
//
// COST: the Block and View headers of a manifest unit include its Table.h, so
// only the one-check standalone_* units compile every header; the manifest
// units compile Table.h and the packet headers. Units run in parallel, and
// the ci-full row unit-codegen-cpp-3 gives the test 110 s on 4 CPUs.
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
	var swept atomic.Int64
	// the group returns once every parallel subtest has finished, so the count
	// below is read after the whole sweep
	t.Run("units", func(t *testing.T) {
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
				compiled := 0
				for _, header := range names {
					if !strings.HasPrefix(row.name, "standalone_") && standaloneSkipsHeader(header) {
						continue
					}
					compiled++
					tu := filepath.Join(dir, "tu_"+strings.TrimSuffix(header, ".h")+".cpp")
					if err := os.WriteFile(tu, []byte("#include \""+header+"\"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					out, err := testproc.Command(t, cxx, "-std=c++17", "-fsyntax-only", "-I"+dir, "-I"+serialize, "-I"+filepath.Join(repo, "test", "tables"), tu).CombinedOutput()
					if err != nil {
						t.Errorf("unit %s: %s does not compile standalone: %v\n%.1500s", row.name, header, err, out)
					}
				}
				if compiled == 0 {
					t.Fatalf("unit %s: no header was compiled", row.name)
				}
				swept.Add(1)
			})
		}
	})
	if err := checkSwept(int(swept.Load()), len(units)); err != nil {
		t.Fatal(err)
	}
}

// standaloneSkipsHeader is the cost bound of the sweep: a Block or View header
// includes its unit Table.h, which carries the hook and is compiled alone.
func standaloneSkipsHeader(header string) bool {
	return strings.HasSuffix(header, "Block.h") || strings.HasSuffix(header, "View.h")
}

// checkSwept refuses a sweep that left a unit out: every unit registered must
// have compiled its headers and counted itself.
func checkSwept(swept, want int) error {
	if swept != want {
		return fmt.Errorf("swept %d of %d units: a unit did not finish its headers", swept, want)
	}
	return nil
}

// TestStandaloneSweepCountFires plants a sweep that lost a unit and requires
// the count check to refuse it (the check once compared the registration
// counter with itself and could never fire).
func TestStandaloneSweepCountFires(t *testing.T) {
	t.Parallel()
	if err := checkSwept(51, 52); err == nil {
		t.Fatal("a sweep that lost a unit passed the count check")
	}
	if err := checkSwept(0, 52); err == nil {
		t.Fatal("a sweep that swept nothing passed the count check")
	}
	if err := checkSwept(52, 52); err != nil {
		t.Fatal(err)
	}
}
