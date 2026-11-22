// A BATCH UNPACKS INTO TREES OF ITS OWN, EVERY TREE OR NONE.
//
// Review findings F-012 and F-013 (2026-11-21). Two destinations of `unpack
// --batch` that were one directory left both messages reading the second
// value, and a repack silently changed the batch; nested destinations
// unpacked and then refused to pack. And a batch whose second tree could not
// be written had already replaced the first.
package tablepack_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const batchSchema = "package unpackbatch\n\nfixed table Player {\n\tlevel int32 | min = 0, max = 1000\n}\n"

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// listing is every entry under dir, so "nothing was written" is checked
// against the whole tree and a leftover temporary shows.
func listing(t *testing.T, dir string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != dir {
			rel, _ := filepath.Rel(dir, path)
			got = append(got, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

// batch is the reviewer's: two Player messages, levels 7 and 33, the
// announcement they ride under, and a directory to unpack into.
type batch struct {
	c     *compiler.Compiler
	u     *ir.Unit
	ann   []byte
	wire  []byte
	work  string
	trees func(a, b string) []compiler.MessageTree
}

func newBatch(t *testing.T) batch {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(base, "unit", "Player.schema"), batchSchema)
	put(t, filepath.Join(base, "in", "a", "level.json"), "7\n")
	put(t, filepath.Join(base, "in", "b", "level.json"), "33\n")
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(base, "unit")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	trees := func(a, b string) []compiler.MessageTree {
		return []compiler.MessageTree{{Root: "Player", Dir: a}, {Root: "Player", Dir: b}}
	}
	wire, _, _, err := c.PackMessages(u, trees(filepath.Join(base, "in", "a"), filepath.Join(base, "in", "b")))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	return batch{c: c, u: u, ann: c.Announce(u), wire: wire, work: work, trees: trees}
}

func (b batch) unpack(x, y string) error {
	_, err := b.c.UnpackMessages(b.u, b.trees(x, y), b.ann, b.wire, false)
	return err
}

// repack packs the two trees back and reports whether the batch is the one
// that was unpacked.
func (b batch) wantRoundTrip(t *testing.T, x, y string) {
	t.Helper()
	if get(t, filepath.Join(x, "level.json")) != "7" || get(t, filepath.Join(y, "level.json")) != "33" {
		t.Fatalf("the trees read %s and %s; want 7 and 33", get(t, filepath.Join(x, "level.json")), get(t, filepath.Join(y, "level.json")))
	}
	again, _, _, err := b.c.PackMessages(b.u, b.trees(x, y))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, b.wire) {
		t.Fatalf("unpack then pack moved the batch: %d bytes then %d", len(b.wire), len(again))
	}
}

// F-012: destinations that are one directory, or one inside the other, are
// refused naming both, whether or not they exist yet, and nothing is written
// or created. The control is two disjoint destinations, which reproduce the
// batch byte for byte. Names a volume folds are held on the volume itself,
// below.
func TestUnpackBatchRefusesOverlappingDestinations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, work string) (x, y string)
	}{
		{"one existing directory", func(t *testing.T, work string) (string, string) {
			put(t, filepath.Join(work, "out", "keep.txt"), "keep")
			return filepath.Join(work, "out"), filepath.Join(work, "out")
		}},
		{"a symlinked spelling of the directory", func(t *testing.T, work string) (string, string) {
			put(t, filepath.Join(work, "out", "keep.txt"), "keep")
			if err := os.Symlink("out", filepath.Join(work, "alias")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return filepath.Join(work, "out"), filepath.Join(work, "alias")
		}},
		{"one fresh directory named twice", func(t *testing.T, work string) (string, string) {
			return filepath.Join(work, "fresh"), filepath.Join(work, "fresh")
		}},
		{"a fresh directory and a dotted spelling of it", func(t *testing.T, work string) (string, string) {
			return filepath.Join(work, "fresh"), work + "/sub/../fresh"
		}},
		{"a fresh parent, then its fresh child", func(t *testing.T, work string) (string, string) {
			return filepath.Join(work, "parent"), filepath.Join(work, "parent", "child")
		}},
		{"a fresh child, then its fresh parent", func(t *testing.T, work string) (string, string) {
			return filepath.Join(work, "parent", "child"), filepath.Join(work, "parent")
		}},
		{"an existing parent and a fresh child", func(t *testing.T, work string) (string, string) {
			put(t, filepath.Join(work, "parent", "keep.txt"), "keep")
			return filepath.Join(work, "parent"), filepath.Join(work, "parent", "child")
		}},
		{"an existing child, then its existing parent", func(t *testing.T, work string) (string, string) {
			put(t, filepath.Join(work, "parent", "child", "keep.txt"), "keep")
			return filepath.Join(work, "parent", "child"), filepath.Join(work, "parent")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBatch(t)
			x, y := tc.setup(t, b.work)
			before := listing(t, b.work)
			err := b.unpack(x, y)
			if err == nil {
				t.Fatalf("unpacked two messages into %s and %s", x, y)
			}
			for _, want := range []string{x, y, "message 1", "message 2", "nothing was written"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not say %q: %v", want, err)
				}
			}
			if after := listing(t, b.work); !slices.Equal(before, after) {
				t.Errorf("the refused unpack changed the directory:\nbefore %v\nafter  %v", before, after)
			}
		})
	}

	t.Run("control: disjoint destinations, fresh and existing", func(t *testing.T) {
		b := newBatch(t)
		x, y := filepath.Join(b.work, "fresh", "a"), filepath.Join(b.work, "existing")
		put(t, filepath.Join(y, "level.json"), "999\n")
		if err := b.unpack(x, y); err != nil {
			t.Fatal(err)
		}
		b.wantRoundTrip(t, x, y)
		if got := listing(t, b.work); !slices.Equal(got, []string{"existing", "existing/level.json", "fresh", "fresh/a", "fresh/a/level.json"}) {
			t.Errorf("the unpack left %v", got)
		}
	})

	t.Run("control: names differing only by case, where the volume keeps them apart", func(t *testing.T) {
		b := newBatch(t)
		x, y := filepath.Join(b.work, "Out"), filepath.Join(b.work, "out")
		if err := os.Mkdir(x, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(y); err == nil {
			t.Skip("this volume takes Out and out for one directory, so they are one destination here: the control runs on Linux")
		}
		if err := os.Remove(x); err != nil {
			t.Fatal(err)
		}
		if err := b.unpack(x, y); err != nil {
			t.Fatal(err)
		}
		b.wantRoundTrip(t, x, y)
	})
}

// The reviewer's fresh case-only alias on the volume itself: no seam. Runs
// where the volume folds names and is skipped, named, where it does not.
func TestUnpackBatchRefusesFreshCaseAliasOnAFoldingVolume(t *testing.T) {
	b := newBatch(t)
	probe := filepath.Join(b.work, "Probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := os.Lstat(filepath.Join(b.work, "probe"))
	if rerr := os.Remove(probe); rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Skipf("this volume keeps Probe and probe apart, so two names differing by case are two directories here: %v", err)
	}
	x, y := filepath.Join(b.work, "Out"), filepath.Join(b.work, "out")
	if err := b.unpack(x, y); err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("unpacked two messages into %s and %s: %v", x, y, err)
	}
	if got := listing(t, b.work); len(got) != 0 {
		t.Errorf("the refused unpack left %v", got)
	}
}

// F-013: the second tree cannot be written (a directory stands where its
// value file goes). The unpack is refused naming it, and the first tree is the
// tree it was: where it held a prior value, the same file, holding 999; where
// it did not exist, it still does not (the reviewer found it made and left
// empty). The control is the same two trees unobstructed.
func TestUnpackBatchWritesEveryTreeOrNone(t *testing.T) {
	for _, firstExists := range []bool{true, false} {
		name := map[bool]string{true: "first tree holds a prior value", false: "first tree absent"}[firstExists]
		t.Run(name, func(t *testing.T) {
			b := newBatch(t)
			x, y := filepath.Join(b.work, "first"), filepath.Join(b.work, "second")
			if firstExists {
				put(t, filepath.Join(x, "level.json"), "999\n")
				put(t, filepath.Join(x, ".notes"), "mine")
			}
			put(t, filepath.Join(y, "level.json", "inside.txt"), "in the way")
			var was os.FileInfo
			if firstExists {
				var err error
				if was, err = os.Lstat(filepath.Join(x, "level.json")); err != nil {
					t.Fatal(err)
				}
			}
			before := listing(t, b.work)
			err := b.unpack(x, y)
			if err == nil || !strings.Contains(err.Error(), y) || !strings.Contains(err.Error(), "level.json") {
				t.Fatalf("the obstructed second tree was not diagnosed by name: %v", err)
			}
			if firstExists {
				if got := get(t, filepath.Join(x, "level.json")); got != "999" {
					t.Errorf("the first tree reads %s after a refused batch; want 999, its prior value", got)
				}
				if now, err := os.Lstat(filepath.Join(x, "level.json")); err != nil || !os.SameFile(was, now) || !now.ModTime().Equal(was.ModTime()) {
					t.Errorf("the first tree's value file is not the file it was (%v)", err)
				}
			}
			if after := listing(t, b.work); !slices.Equal(before, after) {
				t.Errorf("the refused unpack changed the directory:\nbefore %v\nafter  %v", before, after)
			}

			// the control: move the obstruction and both trees are written
			if err := os.RemoveAll(filepath.Join(y, "level.json")); err != nil {
				t.Fatal(err)
			}
			if err := b.unpack(x, y); err != nil {
				t.Fatal(err)
			}
			b.wantRoundTrip(t, x, y)
			if firstExists && get(t, filepath.Join(x, ".notes")) != "mine" {
				t.Error("an entry no schema owns was touched")
			}
		})
	}
}

// A failure after the checks, while the trees are being published, puts back
// every entry already moved: the first tree's value is the file it was, an
// entry no schema owns is the file it was, a destination this run created is
// gone again, and nothing staged or kept is left anywhere.
func TestUnpackBatchFailedPublishPutsEveryTreeBack(t *testing.T) {
	b := newBatch(t)
	x, y := filepath.Join(b.work, "first"), filepath.Join(b.work, "made", "second")
	put(t, filepath.Join(x, "level.json"), "999\n")
	put(t, filepath.Join(x, ".notes"), "mine")
	was := map[string]os.FileInfo{}
	for _, name := range []string{"level.json", ".notes"} {
		info, err := os.Lstat(filepath.Join(x, name))
		if err != nil {
			t.Fatal(err)
		}
		was[name] = info
	}
	before := listing(t, b.work)
	// the first rename under the second tree is the one that publishes its
	// value, after the first tree's has landed
	fault := atomicfile.FailNthRename(y, 1)
	err := b.unpack(x, y)
	fault.Remove()
	if fault.Fired() != 1 || !errors.Is(err, atomicfile.ErrInjected) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("unpack = %v (fault fired %d times); want the publish failure, saying nothing was changed", err, fault.Fired())
	}
	if got := get(t, filepath.Join(x, "level.json")); got != "999" {
		t.Errorf("the first tree reads %s after a failed batch; want 999", got)
	}
	for name, info := range was {
		now, err := os.Lstat(filepath.Join(x, name))
		if err != nil || !os.SameFile(info, now) || !now.ModTime().Equal(info.ModTime()) {
			t.Errorf("%s is not the file it was after a failed batch (%v)", name, err)
		}
	}
	if after := listing(t, b.work); !slices.Equal(before, after) {
		t.Errorf("the failed unpack changed the directory:\nbefore %v\nafter  %v", before, after)
	}

	// the retry, on trees that are as they were, writes both
	if err := b.unpack(x, y); err != nil {
		t.Fatal(err)
	}
	b.wantRoundTrip(t, x, y)
	for name, info := range map[string]os.FileInfo{".notes": was[".notes"]} {
		if now, err := os.Lstat(filepath.Join(x, name)); err != nil || !os.SameFile(info, now) {
			t.Errorf("%s, which no schema owns, is not the file it was after an unpack (%v)", name, err)
		}
	}
	if got := listing(t, b.work); !slices.Equal(got, []string{"first", "first/.notes", "first/level.json", "made", "made/second", "made/second/level.json"}) {
		t.Errorf("the unpack left %v", got)
	}
}

// One tree is held the same way: an unpack over an existing tree leaves the
// entries no schema owns as the files they were, inside an enum-keyed
// directory too, and leaves nothing staged or kept behind.
func TestUnpackLeavesUnownedEntriesAsTheFilesTheyWere(t *testing.T) {
	c, u := corpus(t)
	wire, _, _, err := c.Pack(u, "PackConfig", "../../tables/pack/config")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if _, err := c.Unpack(u, "PackConfig", wire, work); err != nil {
		t.Fatal(err)
	}
	first := listing(t, work)
	var keyed string
	for _, name := range first {
		if info, err := os.Stat(filepath.Join(work, name)); err == nil && info.IsDir() {
			keyed = name
			break
		}
	}
	if keyed == "" {
		t.Fatalf("the corpus tree has no enum-keyed directory: %v", first)
	}
	unowned := []string{".notes", filepath.Join(keyed, ".notes")}
	was := map[string]os.FileInfo{}
	for _, name := range unowned {
		put(t, filepath.Join(work, name), "mine")
		if was[name], err = os.Lstat(filepath.Join(work, name)); err != nil {
			t.Fatal(err)
		}
	}
	dirWas, err := os.Lstat(filepath.Join(work, keyed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Unpack(u, "PackConfig", wire, work); err != nil {
		t.Fatal(err)
	}
	for name, info := range was {
		now, err := os.Lstat(filepath.Join(work, name))
		if err != nil || !os.SameFile(info, now) || !now.ModTime().Equal(info.ModTime()) {
			t.Errorf("%s, which no schema owns, is not the file it was (%v)", name, err)
		}
	}
	if now, err := os.Lstat(filepath.Join(work, keyed)); err != nil || !os.SameFile(dirWas, now) {
		t.Errorf("the directory %s was replaced, not written into (%v)", keyed, err)
	}
	want := append(slices.Clone(first), unowned...)
	slices.Sort(want)
	if got := listing(t, work); !slices.Equal(got, want) {
		t.Errorf("the second unpack left\n%v\nwant\n%v", got, want)
	}
}
