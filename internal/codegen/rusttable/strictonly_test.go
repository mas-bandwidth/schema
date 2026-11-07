package rusttable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
)

// TestStrictOnlyRustNoTolerantEntry is the Rust leg's strict-only grep gate
// (docs/plans/strict-only.md layer 4): the generator's output for the leg's
// example schema (tables/examples) and the Rust table names carry none of the
// retired tolerant names — no Tolerant entry point, no clamped (renamed oor /
// out_of_range), no kind_mismatch counter, no duplicate counter, no malformed
// binary verdict field, and no blob_shared counter on any read report.
// kind_mismatch, malformed, and blob_shared survive only as refusal reasons.
func TestStrictOnlyRustNoTolerantEntry(t *testing.T) {
	t.Parallel()

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := rusttable.Generate(u)
	if err != nil {
		t.Fatalf("generate rust tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate rust tables/examples emitted nothing")
	}

	tablenamesPath := filepath.Join("..", "..", "tablenames", "rust.go")
	if b, err := os.ReadFile(tablenamesPath); err == nil {
		files[tablenamesPath] = b
	} else {
		t.Fatalf("read %s: %v", tablenamesPath, err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	retired := []string{
		"Tolerant",
		"tolerant",
		"clamped",
		"Clamped",
		"duplicate",
		"Duplicate",
		"pub kind_mismatch",
		"kind_mismatch:",
		".kind_mismatch",
		"kind_mismatch +=",
		"pub malformed",
		"malformed:",
		".malformed",
		"malformed =",
		"pub blob_shared",
		".blob_shared",
		"blob_shared +=",
	}

	for _, name := range names {
		text := string(files[name])
		for _, bad := range retired {
			if strings.Contains(text, bad) {
				t.Errorf("%s retains retired tolerant name %q", name, bad)
			}
		}
	}
}
