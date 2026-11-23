package ci

// A REWRITE RUN COMPARES NOTHING (2026-11-22, "Nothing is allowed to fail
// silently"). A golden test that reads NAME=1 as "write the golden from this
// run" passes over no comparison, and a variable exported in a shell or a
// workflow does that to every such test at once. internal/slowtest.Rewriting
// is the one way a test of this module reads such a variable: it fails under
// CI and on any value but 1, and says in the log that a local rewrite
// compared nothing. This gate refuses a test that reads a *_UPDATE* or
// *_WRITE* variable any other way. A directory holding its own go.mod is
// another module, which cannot import internal/slowtest; its tests are read
// by their own module.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var directRewriteRead = regexp.MustCompile(`os\.(Getenv|LookupEnv)\("([A-Z0-9_]*(UPDATE|WRITE)[A-Z0-9_]*)"\)`)

// directRewriteReads is every test-file line under root that reads a rewrite
// variable without slowtest.Rewriting, and the number of test files read.
func directRewriteReads(root string) ([]string, int, error) {
	var out []string
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			switch d.Name() {
			case "testdata", ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := directRewriteRead.FindStringSubmatch(line); m != nil {
				out = append(out, fmt.Sprintf("%s:%d: reads %s directly", filepath.ToSlash(rel), i+1, m[2]))
			}
		}
		return nil
	})
	return out, files, err
}

func TestGoldenRewritesGoThroughRewriting(t *testing.T) {
	t.Parallel()
	reads, files, err := directRewriteReads(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if files < 1000 {
		t.Fatalf("the scan read %d test files: it is looking at the wrong tree", files)
	}
	for _, r := range reads {
		t.Errorf("%s: a rewrite variable read this way passes over no comparison wherever it is set; read it with slowtest.Rewriting(t, name), which fails under CI and on any value but 1", r)
	}
}

func TestGoldenRewriteGateOverAFixtureTree(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"a/a_test.go":          "package a\nfunc f() bool { return os.Getenv(\"SCHEMA_UPDATE_X_GOLDEN\") == \"1\" }\n",
		"a/b_test.go":          "package a\nfunc g() { _, _ = os.LookupEnv(\"FRAMING_WRITE\") }\n",
		"a/c_test.go":          "package a\nfunc h(t T) bool { return slowtest.Rewriting(t, \"FRAMING_WRITE\") }\n",
		"a/d_test.go":          "package a\nfunc k() string { return os.Getenv(\"SCHEMA_SLOW\") }\n",
		"a/testdata/e_test.go": "package e\nvar _ = os.Getenv(\"SCHEMA_UPDATE_Y\")\n",
		"m/go.mod":             "module m\n",
		"m/m_test.go":          "package m\nvar _ = os.Getenv(\"CARDS_UPDATE_FIXTURE\")\n",
	})
	reads, files, err := directRewriteReads(root)
	if err != nil {
		t.Fatal(err)
	}
	want := "a/a_test.go:2: reads SCHEMA_UPDATE_X_GOLDEN directly; a/b_test.go:2: reads FRAMING_WRITE directly"
	if got := strings.Join(reads, "; "); got != want || files != 4 {
		t.Errorf("read %d files, refused:\n got %s\nwant %s (4 files)", files, got, want)
	}
}
