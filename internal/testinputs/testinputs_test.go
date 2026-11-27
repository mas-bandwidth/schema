package testinputs

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeTB records a Fatal instead of stopping the goroutine of a real test.
type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Helper()               {}
func (f *fakeTB) Fatal(...any)          { f.failed = true; panic(f) }
func (f *fakeTB) Fatalf(string, ...any) { f.failed = true; panic(f) }

func fails(t *testing.T, run func(tb testing.TB)) bool {
	t.Helper()
	f := &fakeTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil && r != any(f) {
				panic(r)
			}
		}()
		run(f)
	}()
	return f.failed
}

func TestFilesRecordsFilesAndTrees(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"x.sh", "a/y.mk", "a/b/z.py"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if fails(t, func(tb testing.TB) { Files(tb, filepath.Join(dir, "x.sh"), filepath.Join(dir, "a")) }) {
		t.Fatal("Files refused existing files and trees")
	}
}

func TestFilesRefusesAMissingPathAndNoPath(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "absent.sh")
	if !fails(t, func(tb testing.TB) { Files(tb, missing) }) {
		t.Error("Files accepted a missing file: a child pointed at nothing would go unrecorded")
	}
	if !fails(t, func(tb testing.TB) { Files(tb) }) {
		t.Error("Files accepted no path at all")
	}
}

func TestMakeRecordsTheMakeInputs(t *testing.T) {
	t.Parallel()
	root := Root(t)
	for _, p := range MakeInputs {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("make input %s: %v", p, err)
		}
	}
	if fails(t, func(tb testing.TB) { Make(tb) }) {
		t.Fatal("Make failed on this repository")
	}
}
