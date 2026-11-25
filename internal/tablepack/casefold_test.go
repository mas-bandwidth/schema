package tablepack_test

import (
	"os"
	"path/filepath"
	"testing"
)

// foldingWork is a fresh, empty directory on a volume that takes Probe and
// probe for one entry, as APFS on macOS and NTFS on Windows do.
//
// SCHEMA_CASEFOLD_DIR names a directory on such a volume (on a fleet runner,
// /srv/casefold/scratch: an ext4 image with casefold that the nova-fleet
// github-runners play mounts). The directory is made under it and removed
// after the test, and one that does not fold fails the test. Without it, the
// temporary directory of the test is used where it folds. Where it does not,
// the test fails naming the volume.
func foldingWork(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SCHEMA_CASEFOLD_DIR"); dir != "" {
		work, err := os.MkdirTemp(dir, "tablepack-")
		if err != nil {
			t.Fatalf("SCHEMA_CASEFOLD_DIR=%s: %v", dir, err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(work); err != nil {
				t.Errorf("SCHEMA_CASEFOLD_DIR=%s: removing %s: %v", dir, work, err)
			}
		})
		if work, err = filepath.EvalSymlinks(work); err != nil {
			t.Fatal(err)
		}
		if err := folds(t, work); err != nil {
			t.Fatalf("SCHEMA_CASEFOLD_DIR=%s does not fold names by case: %v", dir, err)
		}
		return work
	}
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = folds(t, work)
	if err == nil {
		return work
	}
	t.Fatalf("SCHEMA_CASEFOLD_DIR is not set and %s does not fold names by case (%v): set SCHEMA_CASEFOLD_DIR to a directory on a case-folding volume (on a fleet runner, /srv/casefold/scratch)", work, err)
	return ""
}

// folds makes dir/Probe and reports why dir/probe is not that entry, or nil
// when it is. dir is left as it was.
func folds(t *testing.T, dir string) error {
	t.Helper()
	probe := filepath.Join(dir, "Probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	made, err := os.Lstat(probe)
	if err != nil {
		t.Fatal(err)
	}
	other, lerr := os.Lstat(filepath.Join(dir, "probe"))
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	if lerr != nil {
		return lerr
	}
	if !os.SameFile(made, other) {
		return os.ErrExist
	}
	return nil
}
