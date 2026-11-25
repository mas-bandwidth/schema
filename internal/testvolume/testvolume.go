// Package testvolume gives a test a working directory on a volume that
// takes two spellings of one name for one entry, as APFS on macOS and NTFS on
// Windows do: Probe and probe, and the composed and decomposed é. A test of
// what a writer does on such a volume needs one, and on Linux only a
// case-folding file system has it.
//
// The directory comes from, in order:
//
//   - $SCHEMA_CASEFOLD_DIR, a directory on such a volume;
//   - FleetScratch, the case-folding scratch every Linux fleet runner and
//     bench mounts (the nova-fleet github-runners play, casefold.yml), when
//     it is a directory;
//   - the test's own temporary directory, where that volume folds.
//
// A directory that does not fold fails the test, naming it. Nothing skips.
package testvolume

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// FleetScratch is the case-folding scratch on every Linux fleet machine: an
// ext4 image made with casefold, mounted at /srv/casefold, whose scratch
// directory any login may write (mode 1777).
const FleetScratch = "/srv/casefold/scratch"

// EnvVar names a directory on a folding volume, overriding FleetScratch.
const EnvVar = "SCHEMA_CASEFOLD_DIR"

// base is where Folding makes its directory: $SCHEMA_CASEFOLD_DIR, else
// FleetScratch when it is a directory, else "" (the test's own temporary
// directory). why says which, for a failure.
func base(getenv func(string) string, isDir func(string) bool) (dir, why string) {
	if d := getenv(EnvVar); d != "" {
		return d, EnvVar + "=" + d
	}
	if isDir(FleetScratch) {
		return FleetScratch, "the fleet's case-folding scratch " + FleetScratch
	}
	return "", ""
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Folding is a fresh, empty directory on a volume that folds names, removed
// after the test. It fails t where there is none, naming what it tried.
func Folding(t testing.TB) string {
	t.Helper()
	b, why := base(os.Getenv, isDir)
	if b != "" {
		work, err := os.MkdirTemp(b, "testvolume-")
		if err != nil {
			t.Fatalf("%s: %v", why, err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(work); err != nil {
				t.Errorf("%s: removing %s: %v", why, work, err)
			}
		})
		if work, err = filepath.EvalSymlinks(work); err != nil {
			t.Fatal(err)
		}
		if err := Folds(work); err != nil {
			t.Fatalf("%s does not fold names: %v", why, err)
		}
		return work
	}
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Folds(work); err != nil {
		t.Fatalf("%s is not set, %s is not a directory, and %s does not fold names (%v): set %s to a directory on a case-folding volume", EnvVar, FleetScratch, work, err, EnvVar)
	}
	return work
}

// errApart is Folds' answer for a volume that keeps the two spellings apart.
var errApart = errors.New("the two spellings are two entries")

// Folds reports why dir does not take Probe and probe for one entry, or nil
// when it does. dir is left as it was.
func Folds(dir string) error {
	probe := filepath.Join(dir, "Probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		return err
	}
	made, err := os.Lstat(probe)
	if err != nil {
		_ = os.Remove(probe)
		return err
	}
	other, lerr := os.Lstat(filepath.Join(dir, "probe"))
	if err := os.Remove(probe); err != nil {
		return err
	}
	if lerr != nil {
		return fmt.Errorf("%w: %w", errApart, lerr)
	}
	if !os.SameFile(made, other) {
		return errApart
	}
	return nil
}

// Probe reports why this machine cannot give a test a folding directory, or
// nil when it can: the directory Folding would use ($SCHEMA_CASEFOLD_DIR,
// else FleetScratch) must be there and fold names. A machine with neither
// cannot: the test's own temporary directory folds only on APFS and NTFS,
// never on a Linux bench. tools/fullrun probes a bench that declares the
// casefold property with this before it sends a casefold test there.
func Probe() error {
	return probe(os.Getenv, isDir)
}

func probe(getenv func(string) string, isDir func(string) bool) error {
	b, why := base(getenv, isDir)
	if b == "" {
		return fmt.Errorf("%s is not set and %s is not a directory: this machine has no case-folding volume", EnvVar, FleetScratch)
	}
	work, err := os.MkdirTemp(b, "testvolume-probe-")
	if err != nil {
		return fmt.Errorf("%s: %w", why, err)
	}
	defer func() {
		_ = os.RemoveAll(work) // errdiscard: a probe directory left behind is the scratch's to clear; the verdict is Folds'
	}()
	if err := Folds(work); err != nil {
		return fmt.Errorf("%s does not fold names: %w", why, err)
	}
	return nil
}
