package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinPublishTree is two packages at v2.5.0 and the changes that move both to
// v2.6.0, as runPin would hand them to publishPins.
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

// TestPinPublishAsideFailure is the one failure point of publishPins the
// committed corpus has no case for (its cases fail a temporary's creation or
// a rename ONTO a path; TestPinCorpusTrees runs them): the rename that moves
// the second original aside fails, after the first file is published, and
// the tree ends all old with nothing staged or kept beside the files.
func TestPinPublishAsideFailure(t *testing.T) {
	t.Parallel()
	errDisk := errors.New("permission denied (injected)")
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
	for i, ch := range changes {
		if got := mustRead(t, ch.path); got != string(ch.orig) {
			t.Errorf("after a failure moving an original aside %s holds\n%q", ch.path, got)
		}
		if entries, err := os.ReadDir(dirs[i]); err != nil || len(entries) != 1 {
			t.Errorf("%s holds %v (err %v); want one entry, nothing staged or kept beside it", dirs[i], entries, err)
		}
	}
}
