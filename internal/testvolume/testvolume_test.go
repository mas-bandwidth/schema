package testvolume

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// Where the folding directory comes from: the variable first, then the
// fleet's mounted scratch, then the test's own temporary directory. The fleet
// benches mount the scratch and set no variable, so a lookup that read only
// the variable failed every folding test on them.
func TestFoldingBaseOrder(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == EnvVar {
				return v
			}
			return ""
		}
	}
	dirs := func(ds ...string) func(string) bool {
		return func(p string) bool {
			for _, d := range ds {
				if p == d {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		name   string
		getenv func(string) string
		isDir  func(string) bool
		want   string
	}{
		{"variable wins over the scratch", env("/elsewhere"), dirs(FleetScratch), "/elsewhere"},
		{"the fleet scratch when no variable", env(""), dirs(FleetScratch), FleetScratch},
		{"the temporary directory when neither", env(""), dirs(), ""},
	} {
		if got, _ := base(c.getenv, c.isDir); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Folds leaves the directory it probes as it was, folding or not.
func TestFoldsLeavesTheDirectoryAsItWas(t *testing.T) {
	dir := t.TempDir()
	if err := Folds(dir); err != nil && !errors.Is(err, errApart) {
		t.Fatalf("Folds on a fresh directory: %v", err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("Folds left %d entries in %s", len(left), dir)
	}
}

// TestProbeNamesAMachineWithNoFoldingVolume: a machine with neither the
// variable nor the fleet scratch has no folding volume, and Probe says so;
// with the variable set, Probe's verdict is Folds' on that volume (a Linux
// bench's own temporary directory keeps the two spellings apart, an APFS one
// folds them). tools/fullrun refuses a bench's casefold declaration on this.
// Planted: a probe that answered nil with no volume would send a casefold
// test to captain.
func TestProbeNamesAMachineWithNoFoldingVolume(t *testing.T) {
	none := func(string) string { return "" }
	noDir := func(string) bool { return false }
	if err := probe(none, noDir); err == nil || !strings.Contains(err.Error(), "no case-folding volume") {
		t.Fatalf("a machine with no folding volume: %v", err)
	}
	dir := t.TempDir()
	env := func(k string) string {
		if k == EnvVar {
			return dir
		}
		return ""
	}
	want := Folds(dir)
	got := probe(env, noDir)
	if (got == nil) != (want == nil) {
		t.Fatalf("probe of %s: %v; Folds there: %v", dir, got, want)
	}
	if got != nil && !strings.Contains(got.Error(), "does not fold names") {
		t.Fatalf("a volume that keeps the spellings apart: %v", got)
	}
	if left, err := os.ReadDir(dir); err != nil || len(left) != 0 {
		t.Fatalf("the probe left %v behind (%v)", left, err)
	}
}
