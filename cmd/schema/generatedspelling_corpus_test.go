package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// THE GENERATED-SPELLING CORPUS (F-014): a committed unit and the case-only
// renames that made one regeneration delete this run's own files on a volume
// that folds names, and leave two spellings of every file, which a Go build
// refuses, on one that does not. Through the built binary, on whatever volume
// the test runs on.
func TestGeneratedSpellingCorpus(t *testing.T) {
	bin := buildCLI(t)
	corpus := filepath.Join("testdata", "generated_spelling")
	var c struct {
		Cases []struct {
			ID       string
			RenameIn string `json:"rename_in"`
			From, To string
		}
	}
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(corpus, "cases.json"))), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) < 3 {
		t.Fatalf("the corpus holds %d cases; it was written with 3", len(c.Cases))
	}
	source := mustRead(t, filepath.Join(corpus, "unit", "Player.schema"))
	for _, tc := range c.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			work, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			unit, out, fresh := filepath.Join(work, "unit"), filepath.Join(work, "out"), filepath.Join(work, "fresh")
			writeTestFile(t, filepath.Join(unit, "Player.schema"), source)
			run(t, bin, "generate", "--lang", "go", "--out", out, unit)
			writeTestFile(t, filepath.Join(out, "hand.txt"), "nobody's")
			if tc.RenameIn != "" {
				dir := map[string]string{"unit": unit, "out": out}[tc.RenameIn]
				if dir == "" {
					t.Fatalf("rename_in %q names no directory of the case", tc.RenameIn)
				}
				if _, err := os.Lstat(filepath.Join(dir, tc.From)); err != nil {
					t.Fatalf("%s holds no %s to rename: %v", tc.RenameIn, tc.From, dirNames(t, dir))
				}
				// through a third name, so a volume that folds names takes it
				if err := os.Rename(filepath.Join(dir, tc.From), filepath.Join(dir, "rename.tmp")); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(dir, "rename.tmp"), filepath.Join(dir, tc.To)); err != nil {
					t.Fatal(err)
				}
			}
			run(t, bin, "generate", "--lang", "go", "--out", out, unit)
			run(t, bin, "generate", "--lang", "go", "--out", fresh, unit)
			want := append(dirNames(t, fresh), "hand.txt")
			slices.Sort(want)
			if got := dirNames(t, out); !slices.Equal(got, want) {
				t.Fatalf("after one regeneration the directory holds %d entries\n%v\nwant what a first generation writes, and the file that is nobody's (%d)\n%v", len(got), got, len(want), want)
			}
			for _, name := range dirNames(t, fresh) {
				if got, want := mustRead(t, filepath.Join(out, name)), mustRead(t, filepath.Join(fresh, name)); got != want {
					t.Errorf("%s does not hold this run's bytes", name)
				}
			}
			if got := mustRead(t, filepath.Join(out, "hand.txt")); got != "nobody's" {
				t.Errorf("hand.txt was touched: %q", got)
			}
		})
	}
}
