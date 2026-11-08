package cstable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
)

// TestStrictOnlyCsNoTolerantEntry is the cs leg's strict-only grep gate
// (docs/plans/strict-only.md layer 4): the generator's output for the leg's
// example schema (tables/examples), the committed bench output under
// generated/bench (w2: the tables and paired bench codecs were still
// carrying the retired counters), and internal/tablenames/cs.go carry none
// of the retired tolerant names — no Tolerant entry point, no Clamped or
// KindMismatch or Duplicate counter, and no Malformed binary verdict on the
// read report. The one read returns the whole value or refuses the whole
// load; the surviving counters are the evolution diagnostics only.
func TestStrictOnlyCsNoTolerantEntry(t *testing.T) {
	t.Parallel()

	retired := []string{"Tolerant", "KindMismatch", "Clamped", "Duplicate", "Malformed"}

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := cstable.Generate(u)
	if err != nil {
		t.Fatalf("generate cs tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate cs tables/examples emitted nothing")
	}
	for name, data := range files {
		for _, word := range retired {
			if strings.Contains(string(data), word) {
				t.Errorf("generated %s retains retired tolerant name %q", name, word)
			}
		}
	}

	// The committed bench codecs are generated output too: scan every .cs
	// file under generated/bench so a stale regeneration cannot reintroduce
	// a retired name unnoticed.
	benchRoot := filepath.Join("..", "..", "..", "generated", "bench")
	scanned := 0
	err = filepath.WalkDir(benchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".cs") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, word := range retired {
			if strings.Contains(string(data), word) {
				t.Errorf("%s retains retired tolerant name %q", path, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan generated/bench: %v", err)
	}
	if scanned == 0 {
		t.Fatal("generated/bench scan found no .cs files")
	}

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "cs.go"))
	if err != nil {
		t.Fatalf("read internal/tablenames/cs.go: %v", err)
	}
	for _, word := range retired {
		if strings.Contains(string(names), word) {
			t.Errorf("internal/tablenames/cs.go retains retired tolerant name %q", word)
		}
	}
}
