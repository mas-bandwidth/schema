// AFTER ONE RUN THE OUTPUT DIRECTORY HOLDS THIS RUN'S FILES, ONE PER OUTPUT,
// UNDER THIS RUN'S SPELLING.
//
// Review finding F-014 (2026-11-21). A source renamed only in case
// (Player.schema to player.schema) regenerated into its existing directory:
// on a volume that folds names the cleanup deleted five of the nine files the
// run had just written, and on one that keeps the names apart it left
// Player.go beside player.go, which a Go build refuses. The rules are held
// here against a tree that folds names and one that does not, on any
// machine, then against the volume the test runs on.
package main

import (
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// fakeTree is an output directory in memory. With folds set it answers as a
// case-insensitive, case-preserving volume does: a name resolves to the entry
// that matches it without regard to case, and a write to an existing entry
// keeps the spelling the entry already has.
type fakeTree struct {
	folds   bool
	entries map[string]*fakeEntry // by path as spelled, slash-separated
}

type fakeEntry struct {
	dir  bool
	data []byte
}

func newFakeTree(folds bool, files map[string]string) *fakeTree {
	t := &fakeTree{folds: folds, entries: map[string]*fakeEntry{}}
	for name, data := range files {
		_ = t.mkdirAll(path.Dir(name))
		t.entries[name] = &fakeEntry{data: []byte(data)}
	}
	return t
}

// resolve returns the path as the tree spells it, and whether it exists.
func (t *fakeTree) resolve(name string) (string, bool) {
	at := "."
	if name == "." {
		return at, true
	}
	for comp := range strings.SplitSeq(name, "/") {
		found := ""
		for key := range t.entries {
			if path.Dir(key) != at {
				continue
			}
			if base := path.Base(key); base == comp || (t.folds && found == "" && strings.EqualFold(base, comp)) {
				found = key
				if base == comp {
					break
				}
			}
		}
		if found == "" {
			return path.Join(at, comp), false
		}
		at = found
	}
	return at, true
}

func (t *fakeTree) names(dir string) ([]string, error) {
	at, ok := t.resolve(dir)
	if !ok {
		return nil, fs.ErrNotExist
	}
	var names []string
	for key := range t.entries {
		if path.Dir(key) == at {
			names = append(names, path.Base(key))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (t *fakeTree) exists(name string) bool {
	_, ok := t.resolve(name)
	return ok
}

func (t *fakeTree) isDir(name string) bool {
	at, ok := t.resolve(name)
	return ok && t.entries[at].dir
}

func (t *fakeTree) same(a, b string) bool {
	ka, oka := t.resolve(a)
	kb, okb := t.resolve(b)
	return oka && okb && ka == kb
}

func (t *fakeTree) rename(from, to string) error {
	src, ok := t.resolve(from)
	if !ok {
		return fs.ErrNotExist
	}
	parent, ok := t.resolve(path.Dir(to))
	if !ok {
		return fs.ErrNotExist
	}
	dst := path.Join(parent, path.Base(to))
	if old, ok := t.resolve(to); ok && old != src {
		delete(t.entries, old)
	}
	moved := map[string]*fakeEntry{}
	for key, e := range t.entries {
		if key == src || strings.HasPrefix(key, src+"/") {
			moved[dst+strings.TrimPrefix(key, src)] = e
			delete(t.entries, key)
		}
	}
	for key, e := range moved {
		t.entries[key] = e
	}
	return nil
}

func (t *fakeTree) remove(name string) error {
	at, ok := t.resolve(name)
	if !ok {
		return fs.ErrNotExist
	}
	delete(t.entries, at)
	return nil
}

func (t *fakeTree) mkdirAll(dir string) error {
	if dir == "." {
		return nil
	}
	at := "."
	for comp := range strings.SplitSeq(dir, "/") {
		next, ok := t.resolve(path.Join(at, comp))
		if !ok {
			t.entries[next] = &fakeEntry{dir: true}
		}
		at = next
	}
	return nil
}

func (t *fakeTree) writeFile(name string, data []byte) error {
	at, ok := t.resolve(name)
	if !ok {
		parent, pok := t.resolve(path.Dir(name))
		if !pok {
			return fs.ErrNotExist
		}
		at = path.Join(parent, path.Base(name))
	}
	t.entries[at] = &fakeEntry{data: data}
	return nil
}

func (t *fakeTree) readFile(name string) ([]byte, error) {
	at, ok := t.resolve(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return t.entries[at].data, nil
}

// The fake is also the run's changes (outChanges), applied as they are
// staged: a volume that folds names keeps the spelling an entry has when a
// write lands on it, which is the behaviour the spelling rule answers.

func (t *fakeTree) Write(name string, data []byte, _ fs.FileMode) {
	_ = t.mkdirAll(path.Dir(name))
	_ = t.writeFile(name, data)
}

func (t *fakeTree) Delete(name string) { _ = t.remove(name) }

func (t *fakeTree) Respell(from, to string) { _ = t.rename(from, to) }

// generateInto is one run of the writer over the fake tree.
func generateInto(t *fakeTree, files map[string][]byte, lang string, removeStale bool, pkg string, schemaBases []string) error {
	_, err := planGenerated(t, t, files, lang, removeStale, pkg, schemaBases)
	return err
}

// cleanStale is the stale cleanup alone over the fake tree.
func cleanStale(t *fakeTree, written []string, pkg string, schemaBases []string) error {
	stale, err := staleGoFiles(t, written, pkg, schemaBases)
	for _, name := range stale {
		t.Delete(name)
	}
	return err
}

// listing is every file the tree holds, as spelled.
func (t *fakeTree) listing() []string {
	var names []string
	for key, e := range t.entries {
		if !e.dir {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

// F-014, both volumes: Player.schema is renamed player.schema and the unit is
// regenerated into the directory the first run wrote. The directory then
// holds exactly this run's files, each under this run's spelling and holding
// this run's bytes; a hand-written file, another unit's generated file and a
// file that only looks generated stay. The control is the same regeneration
// with the source name unchanged.
func TestRegenerationAfterACaseOnlySourceRename(t *testing.T) {
	bystanders := map[string]string{
		"hand.go":       "package demo\n// hand-written\n",
		"OtherTable.go": goFile("Other", "other unit"),
		"notes.txt":     "notes",
	}
	for _, folds := range []bool{true, false} {
		for _, renamed := range []bool{true, false} {
			volume := map[bool]string{true: "a volume that folds names", false: "a volume that keeps names apart"}[folds]
			source := map[bool]string{true: "source renamed in case", false: "control: source name unchanged"}[renamed]
			t.Run(volume+"/"+source, func(t *testing.T) {
				first := playerRun("Player", "first run")
				existing := map[string]string{}
				for name, data := range first {
					existing[name] = string(data)
				}
				for name, data := range bystanders {
					existing[name] = data
				}
				tree := newFakeTree(folds, existing)
				base := "Player"
				if renamed {
					base = "player"
				}
				second := playerRun(base, "second run")
				if err := generateInto(tree, second, "go", true, "demo", []string{base}); err != nil {
					t.Fatal(err)
				}
				want := append(sortedNames(second), "OtherTable.go", "hand.go", "notes.txt")
				sort.Strings(want)
				if got := tree.listing(); !slices.Equal(got, want) {
					t.Fatalf("after one run the directory holds\n%v\nwant exactly this run's files and the bystanders\n%v", got, want)
				}
				for name, data := range second {
					if got := string(tree.entries[name].data); got != string(data) {
						t.Errorf("%s does not hold this run's bytes: %q", name, got)
					}
				}
				for name, data := range bystanders {
					if got := string(tree.entries[name].data); got != data {
						t.Errorf("%s was touched: %q", name, got)
					}
				}
			})
		}
	}
}

// The second reproduction: a generated file renamed only in case by hand
// (DemoView.go to demoview.go) on a volume that folds names. The regeneration
// writes it under the generator's spelling and removes nothing.
func TestRegenerationAfterACaseOnlyOutputRename(t *testing.T) {
	run := playerRun("Demo", "first run")
	existing := map[string]string{}
	for name, data := range run {
		if name == "DemoView.go" {
			name = "demoview.go"
		}
		existing[name] = string(data)
	}
	tree := newFakeTree(true, existing)
	second := playerRun("Demo", "second run")
	if err := generateInto(tree, second, "go", true, "demo", []string{"Demo"}); err != nil {
		t.Fatal(err)
	}
	if got, want := tree.listing(), sortedNames(second); !slices.Equal(got, want) {
		t.Fatalf("after one run the directory holds\n%v\nwant\n%v", got, want)
	}
}

// A FILE THIS RUN WROTE IS NEVER REMOVED, whatever name the volume keeps it
// under: the cleanup alone, handed an entry that is one of this run's outputs
// by identity and is spelled as no output is, leaves it. On a volume that
// keeps the names apart the same entry is another file, an earlier run's, and
// goes.
func TestStaleCleanupNeverRemovesWhatThisRunWrote(t *testing.T) {
	for _, folds := range []bool{true, false} {
		tree := newFakeTree(folds, map[string]string{"Player.go": goFile("player", "this run, where the volume folds")})
		if !folds {
			tree.entries["player.go"] = &fakeEntry{data: []byte(goFile("player", "this run"))}
		}
		if err := cleanStale(tree, []string{"player.go"}, "demo", []string{"player"}); err != nil {
			t.Fatal(err)
		}
		want := []string{"Player.go"}
		if !folds {
			want = []string{"player.go"}
		}
		if got := tree.listing(); !slices.Equal(got, want) {
			t.Errorf("folds=%v: the cleanup left %v, want %v", folds, got, want)
		}
	}
}

// A directory on the way to an output takes this run's spelling too, with
// every child it holds.
func TestGeneratedDirectoriesTakeThisRunsSpelling(t *testing.T) {
	tree := newFakeTree(true, map[string]string{"Sub/old.txt": "old", "Sub/kept.txt": "kept"})
	if err := generateInto(tree, map[string][]byte{"sub/Old.txt": []byte("new")}, "cpp", false, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := tree.listing(), []string{"sub/Old.txt", "sub/kept.txt"}; !slices.Equal(got, want) {
		t.Fatalf("the directory holds %v, want %v", got, want)
	}
	if got := string(tree.entries["sub/Old.txt"].data); got != "new" {
		t.Errorf("sub/Old.txt holds %q", got)
	}
}

// EVERY TARGET writes through the one path: for each registered target, an
// output that an existing entry answers to under another spelling lands under
// this run's spelling with this run's bytes, and nothing else in the
// directory is removed (go is the only target with a cleanup, and here its
// stale file is not this unit's).
func TestEveryTargetWritesThisRunsSpelling(t *testing.T) {
	targets := compiler.New().Targets()
	if !slices.Contains(targets, "go") || len(targets) < 2 {
		t.Fatalf("the compiler registers %v; this test walks every registered target", targets)
	}
	for _, lang := range targets {
		for _, folds := range []bool{true, false} {
			tree := newFakeTree(folds, map[string]string{
				"Player.out":  "first run",
				"Stale.go":    goFile("Elsewhere", "another unit"),
				"stale.other": "an earlier output of any target",
			})
			files := map[string][]byte{"player.out": []byte("second run")}
			if err := generateInto(tree, files, lang, lang == "go", "demo", []string{"player"}); err != nil {
				t.Fatalf("%s: %v", lang, err)
			}
			want := []string{"Stale.go", "player.out", "stale.other"}
			if !folds {
				// two names, two files: the earlier one is not this run's to touch
				want = []string{"Player.out", "Stale.go", "player.out", "stale.other"}
			}
			if got := tree.listing(); !slices.Equal(got, want) {
				t.Errorf("%s, folds=%v: the directory holds %v, want %v", lang, folds, got, want)
			}
			if got := string(tree.entries["player.out"].data); got != "second run" {
				t.Errorf("%s, folds=%v: player.out holds %q", lang, folds, got)
			}
		}
	}
}
