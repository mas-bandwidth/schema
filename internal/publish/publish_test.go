package publish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func names(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	return strings.Join(got, " ")
}

// fixture is a directory holding one entry of each kind a publish meets: a
// file to replace, a directory to replace, a file to remove, and nothing
// where a new file goes. The staged content sits in a directory beside it.
func fixture(t *testing.T) (dir string, entries []*Entry) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "dest")
	stage := filepath.Join(root, "stage")
	write(t, filepath.Join(dir, "file"), "old file")
	write(t, filepath.Join(dir, "tree", "inner"), "old inner")
	write(t, filepath.Join(dir, "stale"), "old stale")
	write(t, filepath.Join(stage, "file"), "new file")
	write(t, filepath.Join(stage, "tree", "inner"), "new inner")
	write(t, filepath.Join(stage, "fresh"), "new fresh")
	for _, e := range []*Entry{
		{Name: "stale", Dest: filepath.Join(dir, "stale")},
		{Name: "file", Dest: filepath.Join(dir, "file"), Next: filepath.Join(stage, "file")},
		{Name: "tree", Dest: filepath.Join(dir, "tree"), Next: filepath.Join(stage, "tree"), Tree: true},
		{Name: "fresh", Dest: filepath.Join(dir, "fresh"), Next: filepath.Join(stage, "fresh")},
	} {
		kept, err := OS.Stage(e.Dest, "keep", nil, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		e.Kept = kept
		entries = append(entries, e)
	}
	return dir, entries
}

func TestPublishMovesEveryEntry(t *testing.T) {
	dir, entries := fixture(t)
	steps := 0
	if fail := OS.Publish(entries, nil, func() { steps++ }); fail != nil {
		t.Fatal(fail.Cause)
	}
	if left := RemoveKept(entries); len(left) > 0 {
		t.Fatal(left)
	}
	if steps != len(entries) {
		t.Errorf("%d steps for %d entries", steps, len(entries))
	}
	if got := names(t, dir); got != "file fresh tree" {
		t.Errorf("the directory holds %q; want file fresh tree, nothing kept or staged", got)
	}
	if read(t, filepath.Join(dir, "file")) != "new file" || read(t, filepath.Join(dir, "tree", "inner")) != "new inner" || read(t, filepath.Join(dir, "fresh")) != "new fresh" {
		t.Error("an entry does not hold its new content")
	}
}

// A failure at any entry puts back every entry before it: the removed file,
// the replaced file and the replaced directory are the entries they were
// (the same inode), and the file that was not there is absent again.
func TestPublishFailurePutsEveryEntryBack(t *testing.T) {
	errDisk := errors.New("permission denied (injected)")
	for failAt := range 4 {
		dir, entries := fixture(t)
		before := map[string]os.FileInfo{}
		for _, name := range []string{"stale", "file", "tree"} {
			info, err := os.Lstat(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			before[name] = info
		}
		fail := OS.Publish(entries, func(i int) error {
			if i == failAt {
				return errDisk
			}
			return nil
		}, nil)
		if fail == nil || !errors.Is(fail.Cause, errDisk) || len(fail.Stuck) > 0 {
			t.Fatalf("fail at %d: %+v", failAt, fail)
		}
		if got := names(t, dir); got != "file stale tree" {
			t.Errorf("fail at %d: the directory holds %q; want file stale tree", failAt, got)
		}
		for name, was := range before {
			now, err := os.Lstat(filepath.Join(dir, name))
			if err != nil || !os.SameFile(was, now) || !now.ModTime().Equal(was.ModTime()) {
				t.Errorf("fail at %d: %s is not the entry it was (%v)", failAt, name, err)
			}
		}
		if read(t, filepath.Join(dir, "file")) != "old file" || read(t, filepath.Join(dir, "tree", "inner")) != "old inner" || read(t, filepath.Join(dir, "stale")) != "old stale" {
			t.Errorf("fail at %d: an entry does not hold its old content", failAt)
		}
	}
}

// An original that will not go home stays at its kept name and is named.
func TestPublishNamesAnEntryItCannotPutBack(t *testing.T) {
	errDisk := errors.New("permission denied (injected)")
	dir, entries := fixture(t)
	file := filepath.Join(dir, "file")
	onto := 0
	f := Files{CreateTemp: os.CreateTemp, Rename: func(oldpath, newpath string) error {
		if newpath == file {
			if onto++; onto > 1 {
				return errDisk // putting the file back
			}
		}
		return os.Rename(oldpath, newpath)
	}}
	fail := f.Publish(entries, func(i int) error {
		if i == 3 {
			return errDisk
		}
		return nil
	}, nil)
	if fail == nil || len(fail.Stuck) != 1 || !strings.Contains(fail.Stuck[0], "file") {
		t.Fatalf("%+v", fail)
	}
	kept := entries[1].Kept
	if kept == "" || !strings.Contains(fail.Stuck[0], kept) || read(t, kept) != "old file" {
		t.Errorf("the failure does not name where the original is kept (%q): %v", kept, fail.Stuck)
	}
}

func TestPublishRefusesAKindMismatch(t *testing.T) {
	dir, entries := fixture(t)
	entries[1].Dest, entries[2].Dest = entries[2].Dest, entries[1].Dest // a file onto the directory
	fail := OS.Publish(entries[1:2], nil, nil)
	if fail == nil || !strings.Contains(fail.Cause.Error(), "is a directory") {
		t.Fatalf("a file replaced a directory: %+v", fail)
	}
	if read(t, filepath.Join(dir, "tree", "inner")) != "old inner" {
		t.Error("the directory was touched")
	}
}

// Overlap by identity for what exists, and by the volume's rule for what
// does not yet. The rule is stated through the Folds seam, so both kinds of
// volume are held on any machine.
func TestOverlap(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a", "b", "a/child"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	saved := Folds
	t.Cleanup(func() { Folds = saved })
	for _, tc := range []struct {
		name  string
		a, b  string
		folds bool
		want  bool
	}{
		{"one existing directory", p("a"), p("a"), false, true},
		{"a symlinked spelling", p("a"), p("alias"), false, true},
		{"a dotted spelling", p("a"), p("b", "..", "a"), false, true},
		{"a child of an existing directory", p("a"), p("a", "child"), false, true},
		{"a child through the alias", p("alias", "child"), p("a"), false, true},
		{"two existing directories", p("a"), p("b"), false, false},
		{"a fresh child of an existing directory", p("a"), p("a", "new"), false, true},
		{"a fresh grandchild through the alias", p("alias", "new", "deeper"), p("a"), false, true},
		{"one fresh name twice", p("new"), p("new"), false, true},
		{"a fresh directory and its fresh child", p("new"), p("new", "child"), false, true},
		{"the fresh child first", p("new", "child"), p("new"), false, true},
		{"two fresh siblings", p("new"), p("other"), false, false},
		{"two fresh cousins", p("new", "x"), p("new", "y"), false, false},
		{"a fresh name beside an existing one", p("a"), p("new"), false, false},
		{"fresh names differing by case, on a volume that keeps them apart", p("New"), p("new"), false, false},
		{"fresh names differing by case, on a volume that folds", p("New"), p("new"), true, true},
		{"nested under a folded spelling", p("New", "child"), p("new"), true, true},
		{"fresh siblings on a volume that folds", p("new"), p("other"), true, false},
		{"two Unicode spellings on a volume that folds", p("café"), p("café"), true, true},
		{"two Unicode spellings on a volume that keeps them apart", p("café"), p("café"), false, false},
	} {
		Folds = func(string) bool { return tc.folds }
		for _, pair := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
			got, err := Overlap(pair[0], pair[1])
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%s: Overlap(%s, %s) = %v, want %v", tc.name, pair[0], pair[1], got, tc.want)
			}
		}
	}
}

// The probe behind Folds agrees with the file system it runs on: it reads
// only, and its answer is what creating the two names would have shown.
func TestFoldsAgreesWithTheVolume(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "Probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	_, lerr := os.Lstat(filepath.Join(dir, "probe"))
	if got, want := Folds(probe), lerr == nil; got != want {
		t.Errorf("Folds = %v on a volume where the other spelling of a name resolves: %v", got, want)
	}
}
