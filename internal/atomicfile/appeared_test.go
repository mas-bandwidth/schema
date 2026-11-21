package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// folding is the file operations of a volume that takes two spellings of a
// name for one entry, over a real directory: every name below root is
// lowercased before it reaches the disk, so Out.bin and out.bin are one file
// on any machine.
func folding(root string) Files {
	fold := func(name string) string {
		if rest, ok := strings.CutPrefix(name, root+string(filepath.Separator)); ok {
			return root + string(filepath.Separator) + strings.ToLower(rest)
		}
		return name
	}
	f := OS()
	create, rename, remove, removeAll, lst, readlink, mkdir, probe := f.Create, f.Rename, f.Remove, f.RemoveAll, f.Lstat, f.Readlink, f.Mkdir, f.Probe
	f.Create = func(name string, mode fs.FileMode) (*os.File, error) { return create(fold(name), mode) }
	f.Rename = func(a, b string) error { return rename(fold(a), fold(b)) }
	f.Remove = func(name string) error { return remove(fold(name)) }
	f.RemoveAll = func(name string) error { return removeAll(fold(name)) }
	f.Lstat = func(name string) (fs.FileInfo, error) { return lst(fold(name)) }
	f.Readlink = func(name string) (string, error) { return readlink(fold(name)) }
	f.Mkdir = func(name string, mode fs.FileMode) error { return mkdir(fold(name), mode) }
	f.Probe = func(name string) error { return probe(fold(name)) }
	return f
}

// TWO NAMES OF ONE COMMIT THAT ARE ONE ENTRY ARE REFUSED. Neither existed when
// the commit began, so nothing could tell them apart by identity then; the
// second finds the first's file under its own name when it publishes, and the
// commit puts the first back and refuses instead of leaving one file where
// the caller asked for two. The control is the same two names on a volume
// that keeps them apart, where both land.
func TestTwoSpellingsOfOneNewEntryAreRefused(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	put(t, filepath.Join(dir, "other.bin"), "an earlier file", 0o644)
	before := lstat(t, filepath.Join(dir, "other.bin"))
	txn := New()
	v := txn.With(folding(dir))
	v.Write(filepath.Join(dir, "Out.bin"), []byte("first"), 0o644)
	v.Write(filepath.Join(dir, "other.bin"), []byte("second"), 0o644)
	v.Write(filepath.Join(dir, "out.bin"), []byte("third"), 0o644)
	err := txn.Commit()
	if !errors.Is(err, ErrAppeared) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("Commit = %v; want ErrAppeared, saying nothing was changed", err)
	}
	if got := names(t, dir); got != "other.bin" {
		t.Errorf("the directory holds %q; want only other.bin, as it was", got)
	}
	same(t, "the earlier file", filepath.Join(dir, "other.bin"), before)
	if got := read(t, filepath.Join(dir, "other.bin")); got != "an earlier file" {
		t.Errorf("other.bin holds %q", got)
	}

	// the control: a volume that keeps the names apart takes both
	plain := tempDir(t)
	txn = New()
	txn.Write(filepath.Join(plain, "Out.bin"), []byte("first"), 0o644)
	txn.Write(filepath.Join(plain, "out.bin"), []byte("third"), 0o644)
	if err := txn.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(plain, "Out.bin")) + "|" + read(t, filepath.Join(plain, "out.bin")); got != "first|third" {
		t.Errorf("the two files hold %q", got)
	}
}

// The same on the volume the test runs on, where it folds names (APFS and
// NTFS by default); skipped, named, where it keeps them apart.
func TestTwoSpellingsOfOneNewEntryOnThisVolume(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	// does this volume take the two spellings for one name? Asked of it with
	// a probe file, which is then removed
	alias := func(a, b string) bool {
		put(t, filepath.Join(dir, a), "probe", 0o644)
		_, err := os.Lstat(filepath.Join(dir, b))
		if rerr := os.Remove(filepath.Join(dir, a)); rerr != nil {
			t.Fatal(rerr)
		}
		return err == nil
	}
	if !alias("Probe", "probe") {
		t.Skip("this volume keeps Probe and probe apart, so they are two files here")
	}
	for _, pair := range [][2]string{{"Out.bin", "out.bin"}, {"café.bin", "café.bin"}} {
		if !alias(pair[0], pair[1]) {
			t.Logf("%q and %q are two names on this volume", pair[0], pair[1])
			continue
		}
		txn := New()
		txn.Write(filepath.Join(dir, pair[0]), []byte("first"), 0o644)
		txn.Write(filepath.Join(dir, pair[1]), []byte("second"), 0o644)
		if err := txn.Commit(); !errors.Is(err, ErrAppeared) {
			t.Errorf("%q and %q: Commit = %v; want ErrAppeared", pair[0], pair[1], err)
		}
		if got := names(t, dir); got != "" {
			t.Errorf("%q and %q: the directory holds %q; want nothing", pair[0], pair[1], got)
		}
	}
}
