package gotable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
)

// TestStrictOnlyGoNoTolerantEntry is the Go leg's strict-only grep gate
// (docs/plans/strict-only.md layer 4): the generator's output for the leg's
// example schema (tables/examples), the shared runtime, internal/tablenames/go.go,
// and the Go conformance driver under test/conformance/go carry none of the
// retired tolerant names — no Tolerant entry point, no Clamped or KindMismatch
// or Duplicate counter, and no Malformed binary verdict on the read report.
// The one read returns the whole value or refuses the whole load; the surviving
// counters are the evolution diagnostics only.
func TestStrictOnlyGoNoTolerantEntry(t *testing.T) {
	t.Parallel()

	retired := []string{
		"Tolerant",
		"KindMismatch int32",
		"kindMismatch",
		".KindMismatch",
		`"KindMismatch"`,
		"Clamped",
		"Duplicate int32",
		".Duplicate",
		`"Duplicate"`,
		"unclamped",
		"Malformed bool",
		"malformed bool",
		".Malformed",
		".malformed",
		"malformed, refused bool",
		"rep.duplicate",
		"duplicate int32",
		"clamped int32",
		"rep.clamped",
	}

	check := func(where, text string) {
		for _, word := range retired {
			if strings.Contains(text, word) {
				t.Errorf("%s retains retired tolerant name %q", where, word)
			}
		}
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}

	standalone, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate standalone go tables/examples: %v", err)
	}
	if len(standalone) == 0 {
		t.Fatal("generate standalone go tables/examples emitted nothing")
	}
	names := make([]string, 0, len(standalone))
	for name := range standalone {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check("standalone/"+name, string(standalone[name]))
	}

	shared, err := gotable.SharedRuntimeFiles()
	if err != nil {
		t.Fatalf("generate shared go runtime: %v", err)
	}
	sharedNames := make([]string, 0, len(shared))
	for name := range shared {
		sharedNames = append(sharedNames, name)
	}
	sort.Strings(sharedNames)
	for _, name := range sharedNames {
		check("shared/"+name, string(shared[name]))
	}

	tablenamesPath := filepath.Join("..", "..", "tablenames", "go.go")
	tablenamesData, err := os.ReadFile(tablenamesPath)
	if err != nil {
		t.Fatalf("read %s: %v", tablenamesPath, err)
	}
	check("internal/tablenames/go.go", string(tablenamesData))

	conformanceRoot := filepath.Join("..", "..", "..", "test", "conformance", "go")
	scanned := 0
	err = filepath.WalkDir(conformanceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		check(path, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("scan test/conformance/go: %v", err)
	}
	if scanned == 0 {
		t.Fatal("test/conformance/go scan found no .go files")
	}
}
