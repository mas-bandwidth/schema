// The committed locks are pinned like any other golden: every corpus unit
// with fixed tables carries a schema.lock, and regenerating it must reproduce
// the committed bytes exactly. A drift here means a fixed table's layout moved
// under an unchanged corpus, which is the defect this file exists to catch.
package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// corpora is every unit in the tree that declares a fixed table, from this
// package's directory. A unit with none carries no lock: there is nothing for
// the file to promise.
var corpora = []string{
	"../../examples-wide",
	"../../tables/arms",
	"../../tables/backend",
	"../../tables/block",
	"../../tables/blockhome",
	"../../tables/examples",
	"../../tables/lists",
	"../../tables/maps",
	"../../tables/messages",
	"../../tables/pointers",
	"../../tables/scalars",
	"../../tables/stream",
	"../../tables/vocab",
	"../../tables/vocab9",
	"../../test/table-base64",
}

// TestCorpusLocksAreCurrent regenerates each committed lock and compares byte
// for byte — the idempotence `schema lock` promises, measured on the corpus
// rather than a fixture.
func TestCorpusLocksAreCurrent(t *testing.T) {
	for _, dir := range corpora {
		t.Run(strings.TrimPrefix(dir, "../../"), func(t *testing.T) {
			path := filepath.Join(dir, lockfile.FileName)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("this unit declares fixed tables and carries a committed lock: %v", err)
			}
			paths, err := compiler.GatherPaths([]string{dir})
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatalf("the corpus does not compile: %v", err)
			}
			// THE LOCK IS CURRENT. This is the question, and `Check` is the
			// thing that answers it for a lock WITH HISTORY as well as one
			// without.
			if errs := lockfile.Check(u, paths); len(errs) > 0 {
				t.Errorf("%s is stale:\n  ./bin/schema lock %s\n%v", path, dir, errs[0])
			}
			// THE FILE IS IN THE COMPILER'S OWN FORM: parsed and written back,
			// it is the same bytes. This is what catches a hand edit, a reflowed
			// line or a stale rendering version.
			locked, err := lockfile.Parse(path, want)
			if err != nil {
				t.Fatalf("%s: the compiler owns this file: %v", path, err)
			}
			if got := locked.Text(); got != string(want) {
				t.Errorf("%s is not in the form `schema lock` writes — parsed and written back it moved:\n  ./bin/schema lock %s", path, dir)
			}
			// AND, FOR A UNIT WITH NO HISTORY, the stricter statement the
			// original test made: a FRESH render of the declaration alone is
			// byte for byte the committed file.
			//
			// It is conditional now because a fresh render has NO LINEAGE and no
			// retired marks — it knows only today's declaration — so a unit whose
			// lock carries a second lineage entry, or an entry an operator
			// RETIRED, can never equal one, and did not before this test was
			// read: `tables/examples` is the first unit in this corpus with
			// history (docs/SPEC-TABLES.md §21.7), and it is what found this.
			// The two assertions above are what carry such a unit.
			if historied(locked) {
				return
			}
			if got := lockfile.Render(u).Text(); got != string(want) {
				t.Errorf("%s is stale — a fixed table's layout moved:\n  ./bin/schema lock %s\n--- committed ---\n%s\n--- current ---\n%s",
					path, dir, want, got)
			}
		})
	}
}

// historied is whether a committed lock holds anything a FRESH RENDER of the
// declaration cannot hold: a lineage of more than one entry, or a retired mark
// on an entry or a table. Such a lock is a RECORD OF THE PAST, and the past is
// exactly what a render of today's schema does not have.
func historied(locked *lockfile.Unit) bool {
	for i := range locked.Tables {
		t := &locked.Tables[i]
		if t.Retired || len(t.Lineage) > 1 {
			return true
		}
		for _, e := range t.Lineage {
			if e.Retired {
				return true
			}
		}
	}
	return false
}

// TestCorpusPassesItsOwnLock is the whole feature end to end over real
// schemas: the driver's lock policy on, the committed file compared, and
// silence.
func TestCorpusPassesItsOwnLock(t *testing.T) {
	for _, dir := range corpora {
		t.Run(strings.TrimPrefix(dir, "../../"), func(t *testing.T) {
			paths, err := compiler.GatherPaths([]string{dir})
			if err != nil {
				t.Fatal(err)
			}
			c := compiler.New()
			c.SchemaLock = true
			if _, err := c.Load(paths); err != nil {
				t.Fatalf("the corpus must pass its own lock: %v", err)
			}
		})
	}
}

// TestEveryUnitWithFixedTablesIsLocked is the register kept honest: a unit
// that grows its first fixed table and commits no lock is unguarded against
// every edit a fixed record cannot report, and nothing else in the tree would
// say so.
func TestEveryUnitWithFixedTablesIsLocked(t *testing.T) {
	locked := map[string]bool{}
	for _, dir := range corpora {
		locked[filepath.Clean(dir)] = true
	}
	// the units that are one directory each; test/tables holds many
	// single-file units in one directory and so has no place for a lock
	// (SPEC §3.2)
	units := []string{
		"../../examples", "../../examples128", "../../examples-wide",
		"../../tables/arms", "../../tables/backend", "../../tables/blobs",
		"../../tables/block", "../../tables/blockhome", "../../tables/examples",
		"../../tables/lists", "../../tables/maps", "../../tables/messages",
		"../../tables/pointers", "../../tables/scalars", "../../tables/stream",
		"../../tables/vocab", "../../tables/vocab9",
		"../../test/packet-text", "../../test/packet-void", "../../test/packet-wide",
		"../../test/table-base64",
	}
	for _, dir := range units {
		paths, err := compiler.GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s does not compile: %v", dir, err)
		}
		fixed := len(lockfile.FixedTables(u)) > 0
		if fixed && !locked[filepath.Clean(dir)] {
			t.Errorf("%s declares fixed tables and carries no %s — lock it with `./bin/schema lock %s` and add it to corpora (docs/SPEC-TABLES.md §2.10)",
				dir, lockfile.FileName, dir)
		}
		if !fixed && locked[filepath.Clean(dir)] {
			t.Errorf("%s carries a %s and declares no fixed table", dir, lockfile.FileName)
		}
	}
}
