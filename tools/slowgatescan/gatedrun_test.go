package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestGatedRunPatternSelectsDirectAndHelperGatedTests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package pkg

import "testing"

func TestDirect(t *testing.T) { slowtest.Gate(t, "x") }
func TestViaHelper(t *testing.T) { helper(t) }
func TestPlain(t *testing.T) {}
func helper(t *testing.T) { slowtest.Gate(t, "x") }
`
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	pattern, err := gatedRunPattern(root)
	if err != nil {
		t.Fatal(err)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TestDirect", "TestViaHelper"} {
		if !re.MatchString(name) {
			t.Errorf("pattern %q does not select gated %s", pattern, name)
		}
	}
	if re.MatchString("TestPlain") {
		t.Errorf("pattern %q selects non-gated TestPlain", pattern)
	}
}
