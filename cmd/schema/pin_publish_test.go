package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pinPublishTree is two packages at v2.5.0 and the changes that move both to
// v2.6.0, as cmdPin would hand them to publishPins.
func pinPublishTree(t *testing.T) (dirs []string, changes []pinChange) {
	t.Helper()
	// resolved, so a path compares equal to the one publishPins follows it
	// to where the temporary directory itself sits behind a symbolic link
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, src := range []string{pinPkg1, pinPkg2} {
		dir := writePinPkg(t, root, []string{"p1", "p2"}[i], src)
		dirs = append(dirs, dir)
		changes = append(changes, pinChange{
			path: filepath.Join(dir, "generate.go"),
			orig: []byte(src),
			next: []byte(strings.ReplaceAll(src, "v2.5.0", "v2.6.0")),
			mode: 0o644,
		})
	}
	return dirs, changes
}

// assertPinTree: every file holds want(change) and each directory holds its
// generate.go alone: no staged file and no kept original left beside it.
func assertPinTree(t *testing.T, what string, dirs []string, changes []pinChange, want func(pinChange) []byte) {
	t.Helper()
	for i, ch := range changes {
		if got := mustRead(t, ch.path); got != string(want(ch)) {
			t.Errorf("%s: %s holds\n%q\nwant\n%q", what, ch.path, got, want(ch))
		}
		assertAlone(t, what, dirs[i])
	}
}

func assertAlone(t *testing.T, what, dir string) {
	t.Helper()
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("%s: %s holds %v (err %v); want one entry, nothing staged or kept beside it", what, dir, entries, err)
	}
}

// failOnce fails the first rename onto path, the publication of that file,
// and lets every other rename through, the ones that put entries back
// included.
func failOnce(path string, err error) func(oldpath, newpath string) error {
	failed := false
	return func(oldpath, newpath string) error {
		if newpath == path && !failed {
			failed = true
			return err
		}
		return os.Rename(oldpath, newpath)
	}
}

// pinOld is the time the fixtures' files were last written, long before any
// test runs, so a file that was rewritten shows.
var pinOld = time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)

// TestPinPublishAllOrNothing fails a two-file pin at each point it can fail,
// through publishPins' seam, and holds the tree to all old or all new: a
// failure staging the second file (the reviewer's read-only directory) and a
// failure publishing it both leave the FIRST file as it was, where the loop
// over writeAtomic left it moved and the retry refused the mixed tree.
func TestPinPublishAllOrNothing(t *testing.T) {
	t.Parallel()
	errDisk := errors.New("permission denied (injected)")
	orig := func(ch pinChange) []byte { return ch.orig }
	next := func(ch pinChange) []byte { return ch.next }

	t.Run("no failure: both files move", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		if err := publishPins(osPinFiles, changes); err != nil {
			t.Fatal(err)
		}
		assertPinTree(t, "published", dirs, changes, next)
	})

	t.Run("staging the second file fails", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		renames := 0
		f := pinFiles{
			createTemp: func(dir, pattern string) (*os.File, error) {
				if dir == dirs[1] {
					return nil, errDisk
				}
				return os.CreateTemp(dir, pattern)
			},
			rename: func(oldpath, newpath string) error {
				renames++
				return os.Rename(oldpath, newpath)
			},
		}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the staging failure, saying nothing was changed", err)
		}
		if renames != 0 {
			t.Errorf("an entry was moved (%d renames) before every file had staged", renames)
		}
		assertPinTree(t, "after a stage failure", dirs, changes, orig)

		// the retry, on a tree that is still all old, moves both
		if err := publishPins(osPinFiles, changes); err != nil {
			t.Fatal(err)
		}
		assertPinTree(t, "the retry", dirs, changes, next)
	})

	t.Run("publishing the second file fails", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		f := pinFiles{createTemp: os.CreateTemp, rename: failOnce(changes[1].path, errDisk)}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the publish failure, saying nothing was changed", err)
		}
		assertPinTree(t, "after a publish failure", dirs, changes, orig)
	})

	t.Run("moving the second original aside fails", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		f := pinFiles{
			createTemp: os.CreateTemp,
			rename: func(oldpath, newpath string) error {
				if oldpath == changes[1].path {
					return errDisk
				}
				return os.Rename(oldpath, newpath)
			},
		}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the failure, saying nothing was changed", err)
		}
		assertPinTree(t, "after a failure moving an original aside", dirs, changes, orig)
	})

	// "Nothing was changed" has to be true of the directory ENTRY, not only
	// of the bytes: a failed pin puts the original entry back, so a file that
	// shares its inode with another still does, and its mode and the time it
	// was last written are the ones it had. (Writing the original bytes into
	// a new file broke the link and stamped the file with now.)
	t.Run("a failed publish puts the original entry back: hard link, mode, mtime", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		twin := filepath.Join(filepath.Dir(dirs[0]), "twin.go")
		if err := os.Link(changes[0].path, twin); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(changes[0].path, 0o600); err != nil {
			t.Fatal(err)
		}
		changes[0].mode = 0o600
		if err := os.Chtimes(changes[0].path, pinOld, pinOld); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(changes[0].path)
		if err != nil {
			t.Fatal(err)
		}

		f := pinFiles{createTemp: os.CreateTemp, rename: failOnce(changes[1].path, errDisk)}
		err = publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the publish failure, saying nothing was changed", err)
		}
		assertPinTree(t, "after a publish failure", dirs, changes, orig)
		after, err := os.Stat(changes[0].path)
		if err != nil {
			t.Fatal(err)
		}
		twinInfo, err := os.Stat(twin)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || !os.SameFile(after, twinInfo) {
			t.Errorf("after a failed pin %s is a different file: it no longer shares an inode with its hard link %s", changes[0].path, twin)
		}
		if !after.ModTime().Equal(pinOld) {
			t.Errorf("after a failed pin %s was last written %v; want %v, the time it had", changes[0].path, after.ModTime(), pinOld)
		}
		if after.Mode() != before.Mode() {
			t.Errorf("after a failed pin %s has mode %v; want %v", changes[0].path, after.Mode(), before.Mode())
		}
	})

	// A generate.go that is a symbolic link: pin follows it and moves the
	// file it names, so the link is never replaced, on a failure or on a
	// success, and what is staged and kept sits beside the file, not the link.
	t.Run("a symbolic link stays a link to the same file", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		realDir := filepath.Join(filepath.Dir(dirs[0]), "real")
		if err := os.Mkdir(realDir, 0o755); err != nil {
			t.Fatal(err)
		}
		realFile := filepath.Join(realDir, "gen.go")
		if err := os.Rename(changes[0].path, realFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(realFile, pinOld, pinOld); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join("..", "real", "gen.go")
		if err := os.Symlink(target, changes[0].path); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(realFile)
		if err != nil {
			t.Fatal(err)
		}
		assertLink := func(what string) {
			t.Helper()
			info, err := os.Lstat(changes[0].path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("%s: %s is no longer a symbolic link (mode %v)", what, changes[0].path, info.Mode())
			}
			if got, err := os.Readlink(changes[0].path); err != nil || got != target {
				t.Errorf("%s: the link names %q (err %v); want %q", what, got, err, target)
			}
			assertAlone(t, what, realDir)
		}

		f := pinFiles{createTemp: os.CreateTemp, rename: failOnce(changes[1].path, errDisk)}
		err = publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the publish failure, saying nothing was changed", err)
		}
		assertLink("after a publish failure")
		assertPinTree(t, "after a publish failure", dirs, changes, orig)
		after, err := os.Stat(realFile)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || !after.ModTime().Equal(pinOld) {
			t.Errorf("after a failed pin the linked file is not the entry it was (same file %v, last written %v; want %v)", os.SameFile(before, after), after.ModTime(), pinOld)
		}

		if err := publishPins(osPinFiles, changes); err != nil {
			t.Fatal(err)
		}
		assertLink("after a pin")
		assertPinTree(t, "after a pin", dirs, changes, next)
		if got := mustRead(t, realFile); got != string(changes[0].next) {
			t.Errorf("the pin did not move the linked file: %q", got)
		}
	})

	// The one case that cannot end all old or all new: an entry that will
	// not go back. The error must not say nothing changed; it names the file
	// and where its original is kept, and that original is still there.
	t.Run("an entry that cannot be put back is named, its original kept", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		onto := 0
		f := pinFiles{
			createTemp: os.CreateTemp,
			rename: func(oldpath, newpath string) error {
				switch newpath {
				case changes[1].path:
					return errDisk // publishing the second, and putting it back
				case changes[0].path:
					if onto++; onto > 1 {
						return errDisk // putting the first entry back
					}
				}
				return os.Rename(oldpath, newpath)
			},
		}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "HALF MOVED") {
			t.Fatalf("publishPins = %v; want the publish failure, saying the pin is half moved", err)
		}
		if strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("a half-moved pin claims nothing was changed: %v", err)
		}
		for i, ch := range changes {
			if !strings.Contains(err.Error(), ch.path) {
				t.Errorf("the error does not name %s: %v", ch.path, err)
			}
			entries, rerr := os.ReadDir(dirs[i])
			if rerr != nil {
				t.Fatal(rerr)
			}
			// the first directory holds the new file and the kept original,
			// the second the kept original alone; nothing staged is left
			if want := 2 - i; len(entries) != want {
				t.Errorf("%s holds %v; want %d entries", dirs[i], entries, want)
			}
			kept := ""
			for _, e := range entries {
				if e.Name() != "generate.go" {
					kept = filepath.Join(dirs[i], e.Name())
				}
			}
			if kept == "" || !strings.Contains(err.Error(), kept) {
				t.Errorf("the error does not name where the original of %s is kept (%q): %v", ch.path, kept, err)
				continue
			}
			if got := mustRead(t, kept); got != string(ch.orig) {
				t.Errorf("the kept original %s holds %q", kept, got)
			}
		}
	})
}
