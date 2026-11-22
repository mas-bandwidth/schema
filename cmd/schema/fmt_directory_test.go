package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
)

// A directory is one formatting request. GatherPaths orders A before B.
func TestFmtDirectoryPreservesEarlierFilesOnParseError(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	put := func(t *testing.T, dir, target, fixture string) string {
		t.Helper()
		p := filepath.Join(dir, target)
		mustWrite(t, p, mustRead(t, filepath.Join("testdata", "fmt_directory", fixture)))
		return p
	}
	for _, args := range [][]string{{"fmt"}, {"fmt", "--migrate"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Run("invalid second file", func(t *testing.T) {
				dir := t.TempDir()
				a := put(t, dir, "A.schema", "A.in")
				b := put(t, dir, "B.schema", "B.invalid")
				first, second := stat(t, a), stat(t, b)
				code, output := runExit(t, bin, append(args, dir)...)
				if code != 1 || !strings.Contains(output, "B.schema") {
					t.Fatalf("expected a refusal naming B.schema, exit %d: %s", code, output)
				}
				first.unchanged(t, a, "formatting a directory with an invalid second source")
				second.unchanged(t, b, "formatting a directory with an invalid second source")
			})
			t.Run("both valid", func(t *testing.T) {
				dir := t.TempDir()
				a := put(t, dir, "A.schema", "A.in")
				b := put(t, dir, "B.schema", "B.in")
				first, second := mustRead(t, a), mustRead(t, b)
				run(t, bin, append(args, dir)...)
				if mustRead(t, a) == first || mustRead(t, b) == second {
					t.Fatal("the successful directory command must format both fixture files")
				}
				run(t, bin, "check", dir)
			})
		})
	}
}

// fmtDirectoryCase is a directory of two sources from the committed corpus
// (testdata/fmt_directory) and fmt, or fmt --migrate, over it in process.
func fmtDirectoryCase(t *testing.T, a, b string, migrate bool) (string, func() error) {
	t.Helper()
	dir := realTempDir(t)
	paths := []string{filepath.Join(dir, "A.schema"), filepath.Join(dir, "B.schema")}
	for i, fixture := range []string{a, b} {
		mustWrite(t, paths[i], mustRead(t, filepath.Join("testdata", "fmt_directory", fixture)))
	}
	return dir, func() error {
		got, err := compiler.GatherPaths([]string{dir})
		if err != nil {
			return err
		}
		rewrote, err := formatSources(atomicfile.OS(), got, migrate)
		if err == nil && len(rewrote) != 2 {
			return fmt.Errorf("fmt over the directory rewrote %v; want both sources", rewrote)
		}
		return err
	}
}

// TestFmtDirectoryParseErrorChangesNoSource is F-220 without the binary: fmt
// and fmt --migrate over a directory whose second source does not parse
// refuse, naming it, with BOTH sources the entries they were (the first used
// to be rewritten before the second was read). The control formats both.
func TestFmtDirectoryParseErrorChangesNoSource(t *testing.T) {
	t.Parallel()
	for _, migrate := range []bool{false, true} {
		t.Run(fmt.Sprintf("migrate=%v", migrate), func(t *testing.T) {
			t.Parallel()
			dir, run := fmtDirectoryCase(t, "A.in", "B.invalid", migrate)
			ageTree(t, dir)
			before := snapshotTree(t, dir)
			err := run()
			if err == nil || !strings.Contains(err.Error(), "B.schema") {
				t.Fatalf("fmt over a directory with an invalid second source = %v; want a refusal naming B.schema", err)
			}
			assertSameTree(t, "after a parse error in the second source", before, snapshotTree(t, dir))

			dir, run = fmtDirectoryCase(t, "A.in", "B.in", migrate)
			before = snapshotTree(t, dir)
			if err := run(); err != nil {
				t.Fatal(err)
			}
			after := snapshotTree(t, dir)
			for _, name := range []string{"A.schema", "B.schema"} {
				if after[name].data == before[name].data {
					t.Errorf("the control did not format %s", name)
				}
			}
		})
	}
}
