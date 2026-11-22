package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A lookup that failed for any reason but "no such file" is an error, never
// "no file there": read as absent, an output behind a directory the run may
// not search passed the alias check unexamined, as if it could be no input.
func TestInfoOfRefusesALookupThatFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes: the search bit this test clears")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads through a directory it may not search, so the lookup cannot fail")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "in.schema"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := infoOf(filepath.Join(locked, "in.schema")); err == nil {
		t.Fatal("a path behind a directory the run may not search read as no file")
	}
	err := refuseOutputsIntoInputs("pack", []outputPath{{"--out", filepath.Join(locked, "in.schema")}}, nil)
	if err == nil || !strings.Contains(err.Error(), "--out") {
		t.Fatalf("the refusal does not name the output it could not look up: %v", err)
	}
	// the control: a path that reaches nothing is absent, not an error
	if e, err := infoOf(filepath.Join(dir, "nothing", "here")); err != nil || e.entry != nil || e.target != nil {
		t.Fatalf("a missing path: %+v %v", e, err)
	}
}
