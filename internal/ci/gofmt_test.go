package ci

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestTheTreePassesGofmt holds every .go file in the module to gofmt. A
// commit that is not gofmt'd makes every later diff noisier than the change it
// carries, and a reviewer's editor reformats what the author did not: the
// question "is this tree formatted" is answered by go/format, not by a
// contributor's editor settings, so this test asks the library rather than
// running a program.
//
// Build output (.git, dist, build, vendor) and testdata are skipped: none of
// them is source this gate owns, and the first three are generated elsewhere.
func TestTheTreePassesGofmt(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	skip := map[string]bool{".git": true, "dist": true, "build": true, "vendor": true, "testdata": true}
	var unformatted []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want, err := format.Source(src)
		if err != nil {
			return fmt.Errorf("%s: format.Source: %w", path, err)
		}
		if string(want) != string(src) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			unformatted = append(unformatted, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(unformatted)
	if len(unformatted) > 0 {
		t.Errorf("%d file(s) are not gofmt'd — run `gofmt -w` on each:\n%s", len(unformatted), strings.Join(unformatted, "\n"))
	}
}
