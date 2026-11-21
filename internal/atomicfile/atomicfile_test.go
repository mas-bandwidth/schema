package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// old is the time the fixtures' files were last written, long before any
// test runs, so a file that was rewritten shows.
var old = time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func put(t *testing.T, path, data string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, old, old); err != nil {
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

// names lists every entry under dir, directories with a trailing slash.
func names(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// same holds a file to the entry it was: the same inode, mode and time.
func same(t *testing.T, what, path string, before fs.FileInfo) {
	t.Helper()
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("%s: %s is a different file", what, path)
	}
	if after.Mode() != before.Mode() {
		t.Errorf("%s: %s has mode %v; want %v", what, path, after.Mode(), before.Mode())
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("%s: %s was last written %v; want %v", what, path, after.ModTime(), before.ModTime())
	}
}

func lstat(t *testing.T, path string) fs.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestWriteFileCreatesAndReplaces(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	path := filepath.Join(dir, "a.txt")
	if err := WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "one" {
		t.Errorf("a new file holds %q", got)
	}
	// an existing file keeps its mode, whatever mode the write names
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("two, and longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "two, and longer" {
		t.Errorf("the replaced file holds %q", got)
	}
	if mode := lstat(t, path).Mode().Perm(); mode != 0o600 {
		t.Errorf("the replaced file has mode %v; want the 0600 it had", mode)
	}
	if got := names(t, dir); got != "a.txt" {
		t.Errorf("the directory holds %q; want the file alone", got)
	}
}

// A write that fails part way leaves the file it was replacing exactly as it
// was: the property the whole package is for.
func TestAFailedWriteLeavesTheOriginal(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{0, 5, 24} {
		dir := tempDir(t)
		path := filepath.Join(dir, "a.txt")
		put(t, path, "the original content, longer than any limit here", 0o640)
		before := lstat(t, path)
		fault := FailWritesAfter(dir, limit)
		err := WriteFile(path, []byte("the replacement, also longer than any limit"), 0o644)
		fault.Remove()
		if !errors.Is(err, ErrInjected) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("limit %d: WriteFile = %v; want the injected failure, saying nothing was changed", limit, err)
		}
		if err != nil && !strings.Contains(err.Error(), path) {
			t.Errorf("limit %d: the error names a temporary, not the file the caller named: %v", limit, err)
		}
		if got := read(t, path); got != "the original content, longer than any limit here" {
			t.Errorf("limit %d: the file holds %q", limit, got)
		}
		same(t, "after a failed write", path, before)
		if got := names(t, dir); got != "a.txt" {
			t.Errorf("limit %d: the directory holds %q; want the file alone", limit, got)
		}
	}
}

func TestAFailedRenameLeavesTheOriginal(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	path := filepath.Join(dir, "a.txt")
	put(t, path, "original", 0o644)
	before := lstat(t, path)
	fault := FailNthRename(dir, 1)
	err := WriteFile(path, []byte("replacement"), 0o644)
	fault.Remove()
	if !errors.Is(err, ErrInjected) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("WriteFile = %v; want the injected failure, saying nothing was changed", err)
	}
	same(t, "after a failed rename", path, before)
	if got := names(t, dir); got != "a.txt" || read(t, path) != "original" {
		t.Errorf("the directory holds %q and the file %q", got, read(t, path))
	}
}

// A file its owner made read-only is not replaced, alone or among others:
// the commit is refused and every file is as it was.
func TestAReadOnlyFileRefusesTheWholeCommit(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	dir := tempDir(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	put(t, a, "a, old", 0o644)
	put(t, b, "b, old", 0o444)
	beforeA, beforeB := lstat(t, a), lstat(t, b)
	txn := New()
	txn.Write(a, []byte("a, new"), 0o644)
	txn.Write(b, []byte("b, new"), 0o644)
	txn.Write(filepath.Join(dir, "c.txt"), []byte("c, new"), 0o644)
	err := txn.Commit()
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("Commit = %v; want permission denied, saying nothing was changed", err)
	}
	same(t, "after the refusal", a, beforeA)
	same(t, "after the refusal", b, beforeB)
	if got := names(t, dir); got != "a.txt b.txt" || read(t, a) != "a, old" || read(t, b) != "b, old" {
		t.Errorf("the directory holds %q; a %q, b %q", got, read(t, a), read(t, b))
	}
}

// A path that is a symbolic link is followed: the file it names is replaced
// and the link stays, through a relative link, a chain, a link in a
// directory that is itself reached through a link, and a link to a file that
// is not there yet.
func TestASymbolicLinkIsFollowed(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	realFile := filepath.Join(dir, "real", "file.txt")
	put(t, realFile, "old", 0o600)
	if err := os.Mkdir(filepath.Join(dir, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	// links/one -> ../real/file.txt; links/two -> one; via -> links
	for link, target := range map[string]string{
		filepath.Join(dir, "links", "one"): filepath.Join("..", "real", "file.txt"),
		filepath.Join(dir, "links", "two"): "one",
		filepath.Join(dir, "via"):          "links",
		filepath.Join(dir, "dangling"):     filepath.Join("real", "new.txt"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for i, path := range []string{
		filepath.Join(dir, "links", "one"),
		filepath.Join(dir, "links", "two"),
		filepath.Join(dir, "via", "two"),
	} {
		want := strings.Repeat("new ", i+1)
		if err := WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := read(t, realFile); got != want {
			t.Errorf("writing %s left the linked file holding %q", path, got)
		}
		if lstat(t, path).Mode()&fs.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symbolic link", path)
		}
		if mode := lstat(t, realFile).Mode().Perm(); mode != 0o600 {
			t.Errorf("the linked file has mode %v; want the 0600 it had", mode)
		}
	}
	if err := WriteFile(filepath.Join(dir, "dangling"), []byte("made"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "real", "new.txt")); got != "made" {
		t.Errorf("a link to a file not there yet: the file holds %q", got)
	}
	if lstat(t, filepath.Join(dir, "dangling")).Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the dangling link was replaced by a file")
	}
	if got := names(t, dir); got != "dangling links/ links/one links/two real/ real/file.txt real/new.txt via" {
		t.Errorf("the tree holds %q: something staged was left, or a link replaced", got)
	}
}

// tree is a small directory a multi-entry commit rewrites: two files
// replaced (one with a hard link beside it), one added in a new directory,
// one removed, one directory with a child removed, one directory respelled.
func tree(t *testing.T) (dir string, commit func() error) {
	t.Helper()
	dir = tempDir(t)
	put(t, filepath.Join(dir, "a.txt"), "a, old", 0o600)
	put(t, filepath.Join(dir, "b.txt"), "b, old", 0o644)
	put(t, filepath.Join(dir, "stale.txt"), "stale", 0o644)
	put(t, filepath.Join(dir, "gone", "child.txt"), "child", 0o644)
	put(t, filepath.Join(dir, "Spelled", "kept.txt"), "kept", 0o644)
	if err := os.Link(filepath.Join(dir, "a.txt"), filepath.Join(dir, "twin.txt")); err != nil {
		t.Fatal(err)
	}
	return dir, func() error {
		txn := New()
		defer txn.Close()
		v, err := txn.OpenRoot(dir)
		if err != nil {
			return err
		}
		v.Write("a.txt", []byte("a, new"), 0o644)
		v.Delete("stale.txt")
		v.Write("b.txt", []byte("b, new"), 0o644)
		v.Under("made/deep").Write("c.txt", []byte("c, new"), 0o644)
		v.Delete("gone")
		v.Delete("never-there")
		v.Respell("Spelled", "respelled")
		return txn.Commit()
	}
}

const treeBefore = "Spelled/ Spelled/kept.txt a.txt b.txt gone/ gone/child.txt stale.txt twin.txt"
const treeAfter = "a.txt b.txt made/ made/deep/ made/deep/c.txt respelled/ respelled/kept.txt twin.txt"

func TestCommitPublishesEveryChange(t *testing.T) {
	t.Parallel()
	dir, commit := tree(t)
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if got := names(t, dir); got != treeAfter {
		t.Errorf("the tree holds\n%s\nwant\n%s", got, treeAfter)
	}
	if a, b := read(t, filepath.Join(dir, "a.txt")), read(t, filepath.Join(dir, "b.txt")); a != "a, new" || b != "b, new" {
		t.Errorf("a holds %q, b holds %q", a, b)
	}
	if mode := lstat(t, filepath.Join(dir, "a.txt")).Mode().Perm(); mode != 0o600 {
		t.Errorf("a.txt has mode %v; want the 0600 it had", mode)
	}
	// the hard link keeps what it held: the file was replaced, not written into
	if got := read(t, filepath.Join(dir, "twin.txt")); got != "a, old" {
		t.Errorf("the hard link beside a.txt holds %q; a replaced file must not write through to it", got)
	}
}

// Every rename of the commit fails in turn, and every staged write is cut
// short: each time the tree is the tree that was there, entry for entry.
func TestCommitIsAllOrNothing(t *testing.T) {
	t.Parallel()
	dir, commit := tree(t)
	watch := FailNthRename(dir, 1<<30)
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	watch.Remove()
	renames := watch.Renames()
	if renames < 8 {
		t.Fatalf("the commit made %d renames; the fixture should need at least 8", renames)
	}
	check := func(what string, plant func(dir string) *Fault) {
		t.Helper()
		dir, commit := tree(t)
		a, twin := filepath.Join(dir, "a.txt"), filepath.Join(dir, "twin.txt")
		before := map[string]fs.FileInfo{}
		for _, name := range strings.Fields(treeBefore) {
			if !strings.HasSuffix(name, "/") {
				before[name] = lstat(t, filepath.Join(dir, name))
			}
		}
		fault := plant(dir)
		err := commit()
		fault.Remove()
		if !errors.Is(err, ErrInjected) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("%s: Commit = %v; want the injected failure, saying nothing was changed", what, err)
		}
		if got := names(t, dir); got != treeBefore {
			t.Errorf("%s: the tree holds\n%s\nwant, as it was,\n%s", what, got, treeBefore)
			return
		}
		for name, info := range before {
			same(t, what, filepath.Join(dir, name), info)
		}
		if !os.SameFile(lstat(t, a), lstat(t, twin)) {
			t.Errorf("%s: a.txt no longer shares an inode with its hard link", what)
		}
		if got := read(t, a) + "|" + read(t, filepath.Join(dir, "b.txt")) + "|" + read(t, filepath.Join(dir, "gone", "child.txt")); got != "a, old|b, old|child" {
			t.Errorf("%s: the files hold %q", what, got)
		}
		// and the retry lands
		if err := commit(); err != nil {
			t.Errorf("%s: the retry: %v", what, err)
		} else if got := names(t, dir); got != treeAfter {
			t.Errorf("%s: after the retry the tree holds %s", what, got)
		}
	}
	for nth := 1; nth <= renames; nth++ {
		check("rename "+string(rune('0'+nth/10))+string(rune('0'+nth%10)), func(dir string) *Fault { return FailNthRename(dir, nth) })
	}
	check("a staged write cut short", func(dir string) *Fault { return FailWritesAfter(dir, 3) })
}

// In a root, no name resolves outside it: a link to an absolute path, a link
// that climbs out, and a name that climbs out are refused with nothing
// written, inside or outside.
func TestARootConfinesEveryName(t *testing.T) {
	t.Parallel()
	parent := tempDir(t)
	dir := filepath.Join(parent, "out")
	outside := filepath.Join(parent, "outside.txt")
	put(t, outside, "original", 0o644)
	put(t, filepath.Join(dir, "inside.txt"), "inside", 0o644)
	for link, target := range map[string]string{
		"absolute": outside,
		"climbs":   filepath.Join("..", "outside.txt"),
		"within":   "inside.txt",
	} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for _, name := range []string{"absolute", "climbs", filepath.Join("..", "outside.txt")} {
		txn := New()
		v, err := txn.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		v.Write("fresh.txt", []byte("new"), 0o644)
		v.Write(name, []byte("new"), 0o644)
		if err := txn.Commit(); err == nil || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("writing %s in a root: Commit = %v; want a refusal, saying nothing was changed", name, err)
		}
		if got := read(t, outside); got != "original" {
			t.Fatalf("writing %s in a root changed the file outside it: %q", name, got)
		}
		if got := names(t, dir); got != "absolute climbs inside.txt within" {
			t.Errorf("after refusing %s the root holds %q", name, got)
		}
	}
	// a link that stays inside is followed
	txn := New()
	v, err := txn.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	v.Write("within", []byte("through the link"), 0o644)
	if err := txn.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "inside.txt")); got != "through the link" {
		t.Errorf("a link inside the root was not followed: %q", got)
	}
}

// A later write to one file replaces an earlier one, by whatever path each
// reached it.
func TestALaterWriteToOneFileWins(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	path := filepath.Join(dir, "a.txt")
	put(t, path, "old", 0o644)
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	txn := New()
	txn.Write(path, []byte("first"), 0o644)
	txn.Write(filepath.Join(dir, "link"), []byte("second"), 0o644)
	if err := txn.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "second" {
		t.Errorf("the file holds %q; want the later write", got)
	}
	if got := names(t, dir); got != "a.txt link" {
		t.Errorf("the directory holds %q", got)
	}
	if err := txn.Commit(); err == nil {
		t.Errorf("a transaction committed twice")
	}
}

// A directory in the way of a file is refused, and nothing is staged.
func TestADirectoryInTheWayIsRefused(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	if err := os.Mkdir(filepath.Join(dir, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(dir, "a.txt"), []byte("new"), 0o644); err == nil {
		t.Errorf("a directory was replaced by a file")
	}
	if got := names(t, dir); got != "a.txt/" {
		t.Errorf("the directory holds %q", got)
	}
}
