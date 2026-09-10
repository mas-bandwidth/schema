// The committed baselines are pinned like any other golden: the tables
// corpora carry a tables.baseline, and regenerating it must reproduce the
// committed bytes exactly. A drift here means the projection moved under an
// unchanged corpus, which is a review question, not a re-pin.
package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// the tables corpora, from this package's directory. tables/messages is where
// an arm of every shape lives (§2.6), so its file pins the ARM LINE's three
// spellings (§18.1) as the others pin the field line's; tables/maps is where a
// generated ENTRY lives, and its file pins the ANONYMOUS key §2.8 gives one —
// the holder's wire id and the map field's, chaining through a nested map, and
// never the generated name, so a `was` rename moves no line.
var corpora = []string{"../../tables/examples", "../../tables/pointers", "../../tables/messages", "../../tables/maps"}

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

// THE FIXED-FORM SIZE ADVISORIES THE CORPUS IS EXPECTED TO CARRY
// (docs/SPEC-TABLES.md §3.4), by table name. They are NOT baseline findings:
// they say that a fixed table's record body is large, which is a design
// remark about a declaration and not a lossy edit. The corpus carries two on
// purpose — the wide-text table of §2.8 and the arms corpus's ToolMessage —
// and pinning them BY NAME rather than tolerating a count is what keeps a
// THIRD one, or a baseline warning, from arriving unnoticed.
var expectedFixedSizeNotes = map[string]string{
	"../../tables/examples": "WideBlob",
	"../../tables/messages": "ToolMessage",
}

// TestCorpusPassesItsOwnCheck is the whole feature end to end over real
// schemas: the driver's baseline policy on, the committed file diffed, and no
// warning the corpus is not expected to carry.
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
			want, expected := expectedFixedSizeNotes[dir], 0
			for _, w := range warns {
				if want != "" && strings.Contains(w, "table "+want+":") && strings.Contains(w, "§3.4") {
					expected++
					continue
				}
				t.Errorf("the corpus must warn about nothing but its known §3.4 size notes: %s", w)
			}
			if want != "" && expected != 1 {
				t.Errorf("the corpus's known §3.4 size note on %s is gone — if the declaration shrank, drop it from expectedFixedSizeNotes", want)
			}
		})
	}
}
