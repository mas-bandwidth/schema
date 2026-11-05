package main

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// THE HARNESS'S TESTS LEAVE THE WORKING TREE AS THEY FOUND IT. A test that
// runs `generate` writes into its own t.TempDir() (outPath), and this check
// holds every test of the package to that: testdata/ is fingerprinted, file by
// file, before the tests run and again after, and any file added, removed or
// changed fails the run by name.
func TestMain(m *testing.M) {
	if os.Getenv("SCHEMA_HARNESS_SKIP_TREE_CHECK") == "1" {
		os.Exit(m.Run())
	}
	root, err := treeRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	before, err := treeFingerprint(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	after, err := treeFingerprint(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if moved := treeMoved(before, after); len(moved) > 0 {
		fmt.Fprintf(os.Stderr, "the harness tests changed the working tree (every test writes under t.TempDir()):\n  %s\n", strings.Join(moved, "\n  "))
		code = 1
	}
	os.Exit(code)
}

// treeRoot is the testdata/ tree the check fingerprints. It is found from
// this file, not from the working directory, so a test that re-runs this
// binary from elsewhere still checks the one tree: a build that keeps the
// source path gives it directly. A build that TRIMS the path (-trimpath)
// gives a package-relative one, so the tree is then found by walking up from
// the working directory, which `go test` sets to this package's own
// directory.
func treeRoot() (string, error) {
	if _, here, _, ok := runtime.Caller(0); ok && filepath.IsAbs(here) {
		root := filepath.Join(filepath.Dir(here), "..", "..", "..", "testdata")
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root, nil
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		root := filepath.Join(dir, "testdata")
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no testdata tree found from %s", dir)
		}
		dir = parent
	}
}

// treeFingerprint is every regular file under root, by its path, to the
// SHA-256 of its bytes.
func treeFingerprint(root string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(data)
		return nil
	})
	return out, err
}

// treeMoved names every file added, removed or changed between two
// fingerprints, sorted.
func treeMoved(before, after map[string][32]byte) []string {
	var moved []string
	for path, sum := range before {
		if now, ok := after[path]; !ok {
			moved = append(moved, "removed "+path)
		} else if now != sum {
			moved = append(moved, "changed "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			moved = append(moved, "added "+path)
		}
	}
	sort.Strings(moved)
	return moved
}

// The check's own negative control: a removed file, a changed one and an added
// one are each named.
func TestTreeMovedNamesEveryChange(t *testing.T) {
	t.Parallel()
	a, b := [32]byte{1}, [32]byte{2}
	got := treeMoved(map[string][32]byte{"kept": a, "gone": a, "edited": a}, map[string][32]byte{"kept": a, "edited": b, "new": a})
	want := []string{"added new", "changed edited", "removed gone"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("treeMoved = %v, want %v", got, want)
	}
}
