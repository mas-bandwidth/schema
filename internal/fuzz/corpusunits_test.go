package fuzz_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorpusUnitsRefuseAMissingCorpus is F-048's regression test. The property
// tests (TestGeneratedOutputIsDeterministic and its neighbour) run over two
// corpora, examples and examples128. corpusUnits used to skip a directory it
// could not read and a file it could not read, so with examples gone the tests
// passed over examples128 alone; only both gone was red. Each committed layout
// under testdata/corpuslayout is one case, beside the complete control.
func TestCorpusUnitsRefuseAMissingCorpus(t *testing.T) {
	layouts := []struct {
		name    string
		refused string // "" for the control: both corpora read
	}{
		{"complete", ""},
		{"missing-primary", filepath.Join("missing-primary", "examples") + ": open"},
		{"no-schema-primary", "holds no .schema file"},
	}
	for _, l := range layouts {
		root := filepath.Join("testdata", "corpuslayout", l.name)
		dirs := []string{filepath.Join(root, "examples"), filepath.Join(root, "examples128")}
		units, err := corpusUnitsFrom(dirs)
		if l.refused == "" {
			if err != nil {
				t.Fatalf("%s: the control was refused: %v", l.name, err)
			}
			if len(units) != 2 {
				t.Fatalf("%s: read %d corpus units, want 2", l.name, len(units))
			}
			for _, dir := range dirs {
				if len(units[dir]) != 1 {
					t.Fatalf("%s: %s gave %d files, want 1", l.name, dir, len(units[dir]))
				}
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: read %d corpus units; a missing corpus must be refused, not skipped", l.name, len(units))
		}
		if !strings.Contains(err.Error(), l.refused) {
			t.Fatalf("%s: refused with %q, want it to name %q", l.name, err, l.refused)
		}
	}
}

// TestCorpusUnitsRefuseAnUnreadableFile: a .schema file the loader cannot
// read used to be skipped (F-048's second half). A dangling link is one.
func TestCorpusUnitsRefuseAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	good, err := os.ReadFile(filepath.Join("testdata", "corpuslayout", "complete", "examples", "Point.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Point.schema"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	// the control: the directory reads while every file in it does
	if _, err := corpusUnitsFrom([]string{dir}); err != nil {
		t.Fatalf("the readable control was refused: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "absent"), filepath.Join(dir, "Gone.schema")); err != nil {
		t.Fatal(err)
	}
	units, err := corpusUnitsFrom([]string{dir})
	if err == nil {
		t.Fatalf("read %d corpus units past an unreadable file; it must be refused", len(units))
	}
	if !strings.Contains(err.Error(), "Gone.schema") {
		t.Fatalf("refused with %q, want it to name Gone.schema", err)
	}
}

// TestTheRealCorporaAreBothRead holds the repository to the same rule the
// layouts check: both corpora read, each with at least one file.
func TestTheRealCorporaAreBothRead(t *testing.T) {
	units, err := corpusUnitsFrom(corpusDirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != len(corpusDirs) {
		t.Fatalf("read %d corpus units, want %d", len(units), len(corpusDirs))
	}
}
