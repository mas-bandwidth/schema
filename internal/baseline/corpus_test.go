// The committed baselines are pinned like any other golden: the tables
// corpora carry a tables.baseline, and regenerating it must reproduce the
// committed bytes exactly. A drift here means the projection moved under an
// unchanged corpus, which is a review question, not a re-pin.
package baseline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// the tables corpora, from this package's directory. tables/messages is where
// an arm of every shape lives (§2.6), so its file pins the ARM LINE's three
// spellings (§18.1) as the others pin the field line's.
var corpora = []string{"../../tables/examples", "../../tables/pointers", "../../tables/messages"}

// TestCorpusBaselinesAreCurrent regenerates each committed baseline and
// compares byte for byte — the idempotence the `--update` path promises,
// measured on the corpus rather than a fixture.
func TestCorpusBaselinesAreCurrent(t *testing.T) {
	for _, dir := range corpora {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			path := filepath.Join(dir, baseline.FileName)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("the tables corpus carries a committed baseline: %v", err)
			}
			committedFile, err := baseline.Parse(path, want)
			if err != nil {
				t.Fatal(err)
			}

			paths, err := compiler.GatherPaths([]string{dir})
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatalf("the corpus does not compile: %v", err)
			}
			live := baseline.Render(u)
			live.History = committedFile.History
			if live.Text() != string(want) {
				t.Errorf("%s is stale — regenerate it deliberately:\n  ./bin/schema tables-baseline --update --reason \"...\" %s\n--- committed ---\n%s\n--- current ---\n%s",
					path, dir, want, live.Text())
			}
		})
	}
}

// TestCorpusPassesItsOwnCheck is the whole feature end to end over real
// schemas: the driver's baseline policy on, the committed file diffed, and
// silence.
func TestCorpusPassesItsOwnCheck(t *testing.T) {
	for _, dir := range corpora {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			paths, err := compiler.GatherPaths([]string{dir})
			if err != nil {
				t.Fatal(err)
			}
			c := compiler.New()
			c.TablesBaseline = true
			var warns []string
			c.OnWarn = func(msg string) { warns = append(warns, msg) }
			if _, err := c.Load(paths); err != nil {
				t.Fatalf("the corpus must pass its own baseline: %v", err)
			}
			if len(warns) != 0 {
				t.Errorf("the corpus must warn about nothing: %v", warns)
			}
		})
	}
}
