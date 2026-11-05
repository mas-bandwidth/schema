package lobstertable_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	mapKeyWidenMarker  = "if kind != want and widens(kind, want):\n"
	mapKeyWidenScalar  = "\n                if not scalar_at(scan, cell, k, want, kind):"
	mapKeyKindBadReset = "\n                kind_bad = false"
)

func mapKeyWidenBranch(src string) (string, error) {
	if strings.Count(src, mapKeyWidenMarker) != 1 {
		return "", errors.New("map_key widening branch is absent or does not clear kind_bad once")
	}
	start := strings.Index(src, mapKeyWidenMarker)
	end := strings.Index(src[start:], mapKeyWidenScalar)
	if end < 0 {
		return "", errors.New("map_key widening branch is absent or does not clear kind_bad once")
	}
	branch := src[start : start+end]
	if strings.Count(branch, mapKeyKindBadReset) != 1 || strings.Count(branch, "widened = true") != 1 {
		return "", errors.New("map_key widening branch is absent or does not clear kind_bad once")
	}
	return branch, nil
}

// TestLobsterMapKeyWidenClearsKindBad keeps the embedded runtime and its
// generated benchmark copy on the same last-widened-key branch. Without the
// reset, an earlier incompatible key kind incorrectly rejects the entry
// (docs/SPEC-TABLES.md §2.8; PR #2811).
func TestLobsterMapKeyWidenClearsKindBad(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	source := read("internal/codegen/lobstertable/runtime/load.lobster")
	generated := read("generated/bench/tables/lobster/load.lobster")
	sourceBranch, err := mapKeyWidenBranch(source)
	if err != nil {
		t.Fatal("runtime source map_key widening branch must clear kind_bad exactly once")
	}
	generatedBranch, err := mapKeyWidenBranch(generated)
	if err != nil {
		t.Fatal("generated map_key widening branch must clear kind_bad exactly once")
	}
	if sourceBranch != generatedBranch {
		t.Fatal("generated map_key widening branch differs from runtime source")
	}

	// Negative control: removing the reset from either copy must make this
	// contract check fail, as it did against the pre-fix runtime.
	for name, damaged := range map[string]string{
		"runtime":   strings.Replace(source, mapKeyKindBadReset, "", 1),
		"generated": strings.Replace(generated, mapKeyKindBadReset, "", 1),
	} {
		if _, err := mapKeyWidenBranch(damaged); err == nil {
			t.Fatalf("negative control did not reject the missing kind_bad reset in %s", name)
		}
	}
}
