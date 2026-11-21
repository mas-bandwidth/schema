package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinPublishTree is two packages at v2.5.0 and the changes that move both to
// v2.6.0, as cmdPin would hand them to publishPins.
func pinPublishTree(t *testing.T) (dirs []string, changes []pinChange) {
	t.Helper()
	root := t.TempDir()
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
// generate.go alone, no temporary left beside it.
func assertPinTree(t *testing.T, what string, dirs []string, changes []pinChange, want func(pinChange) []byte) {
	t.Helper()
	for i, ch := range changes {
		if got := mustRead(t, ch.path); got != string(want(ch)) {
			t.Errorf("%s: %s holds\n%q\nwant\n%q", what, ch.path, got, want(ch))
		}
		if entries, err := os.ReadDir(dirs[i]); err != nil || len(entries) != 1 {
			t.Errorf("%s: %s holds %v (err %v); want generate.go alone", what, dirs[i], entries, err)
		}
	}
}

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
			t.Errorf("a file was published (%d renames) before every file had staged", renames)
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
		f := pinFiles{
			createTemp: os.CreateTemp,
			rename: func(oldpath, newpath string) error {
				if newpath == changes[1].path {
					return errDisk
				}
				return os.Rename(oldpath, newpath)
			},
		}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("publishPins = %v; want the publish failure, saying nothing was changed", err)
		}
		assertPinTree(t, "after a publish failure", dirs, changes, orig)
	})

	t.Run("publishing fails and the put-back cannot rename either", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		first := 0
		f := pinFiles{
			createTemp: os.CreateTemp,
			rename: func(oldpath, newpath string) error {
				if newpath == changes[1].path {
					return errDisk
				}
				if first++; first > 1 {
					return errDisk // the put-back of the first file
				}
				return os.Rename(oldpath, newpath)
			},
		}
		if err := publishPins(f, changes); !errors.Is(err, errDisk) {
			t.Errorf("publishPins = %v; want the publish failure", err)
		}
		// the original bytes are written in place instead
		assertPinTree(t, "after a publish failure and an in-place put-back", dirs, changes, orig)
	})

	t.Run("a put-back that cannot be done is named", func(t *testing.T) {
		t.Parallel()
		dirs, changes := pinPublishTree(t)
		f := pinFiles{
			createTemp: os.CreateTemp,
			rename: func(oldpath, newpath string) error {
				if newpath == changes[1].path {
					// the first file is published by now: make it a file
					// nothing can be put back over
					if err := os.Remove(changes[0].path); err != nil {
						t.Error(err)
					}
					if err := os.Mkdir(changes[0].path, 0o755); err != nil {
						t.Error(err)
					}
					return errDisk
				}
				return os.Rename(oldpath, newpath)
			},
		}
		err := publishPins(f, changes)
		if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "HALF MOVED") || !strings.Contains(err.Error(), changes[0].path) {
			t.Errorf("publishPins = %v; want the publish failure naming %s as half moved", err, changes[0].path)
		}
		if strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("a half-moved pin claims nothing was changed: %v", err)
		}
		if got := mustRead(t, changes[1].path); got != string(changes[1].orig) {
			t.Errorf("the second file changed: %q", got)
		}
		if entries, rerr := os.ReadDir(dirs[1]); rerr != nil || len(entries) != 1 {
			t.Errorf("%s holds %v (err %v); want generate.go alone", dirs[1], entries, rerr)
		}
		if entries, rerr := os.ReadDir(dirs[0]); rerr != nil || len(entries) != 1 {
			t.Errorf("%s holds %v (err %v); want one entry, no temporary", dirs[0], entries, rerr)
		}
	})
}
