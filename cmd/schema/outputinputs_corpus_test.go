package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// outputInputsCorpus is testdata/output_inputs/cases.json.
type outputInputsCorpus struct {
	Setup [][]string
	Cases []struct {
		ID     string
		Layout struct {
			Files     map[string]string
			Symlinks  map[string]string
			Hardlinks map[string]string
		}
		Args      []string
		Exit      int
		Names     string
		SameEntry []string `json:"same_entry"`
	}
}

// THE OUTPUT-INPUTS CORPUS (F-010, F-011, F-140): committed filesystem
// layouts and the commands run over them, through the built binary. A
// refused case exits nonzero naming its flag, leaves the unit and the tree
// byte for byte, leaves the next check and pack working, and leaves every
// entry it lists the directory entry it was; a control exits 0.
func TestOutputInputsCorpus(t *testing.T) {
	bin := buildCLI(t)
	corpus := filepath.Join("testdata", "output_inputs")
	var c outputInputsCorpus
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(corpus, "cases.json"))), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) < 12 {
		t.Fatalf("the corpus holds %d cases; it was written with 12", len(c.Cases))
	}
	fixture := map[string]string{} // every input file, by its path under {work}
	for _, dir := range []string{"unit", "tree"} {
		err := filepath.WalkDir(filepath.Join(corpus, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(corpus, path)
			fixture[rel] = mustRead(t, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range c.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			work, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			expand := func(args []string) []string {
				out := make([]string, len(args))
				for i, a := range args {
					out[i] = strings.ReplaceAll(a, "{work}", work)
				}
				return out
			}
			for rel, data := range fixture {
				writeTestFile(t, filepath.Join(work, rel), data)
			}
			if err := os.MkdirAll(filepath.Join(work, "out"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, args := range c.Setup {
				run(t, bin, expand(args)...)
			}
			for rel, data := range tc.Layout.Files {
				writeTestFile(t, filepath.Join(work, rel), data)
			}
			for rel, target := range tc.Layout.Symlinks {
				if err := os.Symlink(target, filepath.Join(work, rel)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			for rel, of := range tc.Layout.Hardlinks {
				if err := os.Link(filepath.Join(work, of), filepath.Join(work, rel)); err != nil {
					t.Fatalf("hard link %s -> %s: %v", rel, of, err)
				}
			}
			before := map[string]fs.FileInfo{}
			for _, rel := range tc.SameEntry {
				info, err := os.Lstat(filepath.Join(work, rel))
				if err != nil {
					t.Fatal(err)
				}
				before[rel] = info
			}
			outBefore := treeNames(t, filepath.Join(work, "out"))

			code, text := runExit(t, bin, expand(tc.Args)...)
			if tc.Exit == 0 {
				if code != 0 {
					t.Fatalf("the control exited %d:\n%s", code, text)
				}
			} else {
				if code == 0 {
					t.Fatalf("exited 0, writing over what it reads:\n%s", text)
				}
				if !strings.Contains(text, tc.Names) {
					t.Errorf("the refusal does not name %s:\n%s", tc.Names, text)
				}
				if got := treeNames(t, filepath.Join(work, "out")); !slices.Equal(got, outBefore) {
					t.Errorf("the refused run changed out/: %v, was %v", got, outBefore)
				}
			}
			for rel, data := range fixture {
				if got := mustRead(t, filepath.Join(work, rel)); got != data {
					t.Errorf("%s holds %q after the run; want %q", rel, got, data)
				}
			}
			for rel, was := range before {
				now, err := os.Lstat(filepath.Join(work, rel))
				switch {
				case err != nil:
					t.Errorf("%s is gone: %v", rel, err)
				case !os.SameFile(was, now):
					t.Errorf("%s is another entry: it was replaced, not kept or put back", rel)
				case now.Mode() != was.Mode() || !now.ModTime().Equal(was.ModTime()):
					t.Errorf("%s came back with mode %v and time %v; want %v and %v", rel, now.Mode(), now.ModTime(), was.Mode(), was.ModTime())
				}
			}
			// the next run works
			run(t, bin, "check", filepath.Join(work, "unit"))
			run(t, bin, "pack", "--root", "Player", "--out", filepath.Join(t.TempDir(), "repack.bin"), filepath.Join(work, "tree"), filepath.Join(work, "unit"))
		})
	}
}
