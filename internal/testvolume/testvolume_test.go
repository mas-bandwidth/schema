package testvolume

import (
	"errors"
	"os"
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
