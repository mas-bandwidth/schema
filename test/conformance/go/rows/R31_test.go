// R31_test.go — tests the full-width lanes law (docs/FIXED-FORM-ALGORITHM.md:1125)
// Law: arg and arg2 are uint64_t on both twins; the emitted static plan carries
// them full width; the compare is at the tag's own width; a 64-bit ordinal
// temporary. This test verifies the guard comparison at full width.

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestRowR31 verifies that guards compare at full width (not byte lane) and
// that ordinals can be 64-bit (no byte lane truncation).
//
// The spec says (docs/FIXED-FORM-ALGORITHM.md:1125):
//
//	"arg and arg2 are uint64_t on both twins, the emitted static plan carries
//	 them full width, and the compare is at the tag's own width"
//
// This test verifies:
// 1. The generated code uses full-width comparison at tag's own width
// 2. The tableFixedTagAt function loads guards at full width into uint64
func TestRowR31(t *testing.T) {
	// Read the generated code from M1 which has union tags
	root := findRepoRoot(t)
	codePath := filepath.Join(root, "build", "tables-generated-go", "m1", "M1Table.go")
	code, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatalf("failed to read M1Table.go: %v", err)
	}
	codeStr := string(code)

	// Verify tableFixedTagAt loads at full width into uint64
	// The function should return uint64 and read the full width, not just first byte
	tagAtRegex := regexp.MustCompile(`func tableFixedTagAt\([^)]*\)\s+uint64\s*\{[^}]*var\s+v\s+uint64[^}]*}`)
	if !tagAtRegex.MatchString(codeStr) {
		t.Error("tableFixedTagAt should return uint64 and use uint64 variable for full-width read")
	}

	// Verify the guard comparison happens at full width (not byte lane)
	// In tableFixedRun, we should see: tableFixedTagAt(...) != uint64(p.Arg)
	// This shows the comparison uses full-width tag against full-width arg
	guardCompare := regexp.MustCompile(`tableFixedTagAt\s*\([^)]+\)\s*!=\s*uint64\s*\(\s*p\.Arg\s*\)`)
	if !guardCompare.MatchString(codeStr) {
		t.Error("guard comparison should be at full width using uint64(p.Arg)")
	}

	// Verify ArgW is used for the tag width (not hardcoded to byte)
	// This proves the compare is at the tag's own width
	argWRegex := regexp.MustCompile(`tableFixedTagAt\s*\([^)]+,.*,\s*p\.ArgW\s*\)`)
	if !argWRegex.MatchString(codeStr) {
		t.Error("compare should use p.ArgW for tag's own width")
	}

	// Verify the comment explains full-width behavior (not byte lane)
	commentRegex := regexp.MustCompile(`(?s)Arg\s+is.*(?i)no\s+byte\s+lane`)
	if !commentRegex.MatchString(codeStr) {
		t.Error("should have comment explaining arg has no byte lane")
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "schema")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
