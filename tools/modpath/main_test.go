package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/acme/widget/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flags := []string{"-from", "example.com/acme/widget/v2", "-to", "example.com/acme/widget/v3", "-min-files", "1"}
	step := func(name string, want int, wantOut string, args ...string) {
		t.Helper()
		var out, errb strings.Builder
		if got := run(append(append([]string{}, flags...), args...), &out, &errb); got != want {
			t.Fatalf("%s: exit %d, want %d\nstdout %s\nstderr %s", name, got, want, out.String(), errb.String())
		}
		if !strings.Contains(out.String(), wantOut) {
			t.Fatalf("%s: stdout lacks %q:\n%s", name, wantOut, out.String())
		}
	}
	step("check before", 1, "go.mod:1:", "-check", root)
	step("rewrite", 0, "rewrote go.mod (1 lines)", root)
	step("rewrite again", 0, "nothing to rewrite", root)
	step("check after", 0, "clean", "-check", root)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "module example.com/acme/widget/v3\n" {
		t.Fatalf("go.mod is %q", data)
	}

	var out, errb strings.Builder
	if got := run([]string{"-check"}, &out, &errb); got != 2 {
		t.Errorf("no root: exit %d, want 2", got)
	}
	if got := run([]string{"-from", "x", root}, &out, &errb); got != 2 {
		t.Errorf("bad -from: exit %d, want 2", got)
	}
	// the default floor refuses this one-file tree, and an empty directory
	// fails at any floor: neither may pass as clean
	if got := run([]string{"-check", root}, &out, &errb); got != 2 {
		t.Errorf("tiny tree under the default floor: exit %d, want 2", got)
	}
	errb.Reset()
	if got := run(append(append([]string{}, flags...), "-check", t.TempDir()), &out, &errb); got != 2 || !strings.Contains(errb.String(), "not the repository root") {
		t.Errorf("empty root: exit %d, stderr %q; want 2 and a refusal", got, errb.String())
	}
	// a symlink to the tree walks the tree
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if got := run(append(append([]string{}, flags...), "-check", link), &out, &errb); got != 0 || !strings.Contains(out.String(), "1 files read") {
		t.Errorf("symlinked root: exit %d, stdout %q", got, out.String())
	}
}
