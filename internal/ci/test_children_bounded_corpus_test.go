package ci

import (
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// judgedFile reports whether the gate reads the file name in the covered
// package dir: a Go test file, or any Go file of a run-helper package.
// TestEveryTestChildIsBounded walks the tree with it and the corpus below is
// judged by it, so the two cannot disagree on what is read.
func judgedFile(dir, name string) bool {
	return strings.HasSuffix(name, ".go") && (helperDir(dir) || strings.HasSuffix(name, "_test.go"))
}

// TestEveryTestChildIsBoundedCorpus runs the gate over the committed policy
// corpus (testdata/test-children-bounded): Go source fixtures, each judged as
// if it sat at a named repository path. A fixture that starts a child with
// os/exec is reported wherever the gate covers (an interpreter leg's test, a
// compiled leg's test, the compiler's, a run-helper's any file), its bounded
// twin is not, and neither is a file outside the gate's reach.
func TestEveryTestChildIsBoundedCorpus(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "test-children-bounded")
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	reported, clean := 0, 0
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("cases.tsv line %d: %d fields, want 3", n+1, len(f))
		}
		fixture, placed := f[0], f[1]
		want, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("cases.tsv line %d: %v", n+1, err)
		}
		src, err := os.ReadFile(filepath.Join(dir, fixture))
		if err != nil {
			t.Fatal(err)
		}
		// the gate's own two decisions: does it read a file at this path,
		// and what does it see in the source
		var at []string
		if pkg := path.Dir(placed); slices.Contains(boundedChildDirs, pkg) && judgedFile(pkg, path.Base(placed)) {
			if at, err = unboundedChildren(token.NewFileSet(), placed, src); err != nil {
				t.Fatalf("%s: %v", fixture, err)
			}
		}
		if len(at) != want {
			t.Errorf("%s placed at %s: the gate reports %d unbounded child(ren) %v, want %d", fixture, placed, len(at), at, want)
		}
		if want > 0 {
			reported++
		} else {
			clean++
		}
	}
	if reported == 0 || clean == 0 {
		t.Errorf("the corpus holds %d reported and %d clean rows: a report is only proven beside a clean control", reported, clean)
	}
}
