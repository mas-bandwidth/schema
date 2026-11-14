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

// A directory with its own go.mod is another module: its import path does not
// resolve from root, so a shard that named it would fail setup and print no
// skip. Its gated tests are that module's own targets' business.
func TestGatedTestPackagesSkipsNestedModules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gated := `package pkg

import "testing"

func TestGated(t *testing.T) { slowtest.Gate(t, "x") }
`
	for _, rel := range []string{"pkg", filepath.Join("nested", "rows")} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte(gated), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "go.mod"), []byte("module nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	byDir, err := gatedTestPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDir["pkg"]) != 1 {
		t.Errorf("the root module's gated package is missing: %v", byDir)
	}
	if _, ok := byDir["nested/rows"]; ok {
		t.Errorf("a nested module's package was scanned as root's: %v", byDir)
	}
}
