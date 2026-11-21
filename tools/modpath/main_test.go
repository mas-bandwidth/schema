package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/modpath"
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
		if got := run(append(append([]string{}, flags...), args...), &out, &errb, testBuilt); got != want {
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
	if got := run([]string{"-check"}, &out, &errb, testBuilt); got != 2 {
		t.Errorf("no root: exit %d, want 2", got)
	}
	if got := run([]string{"-from", "x", root}, &out, &errb, testBuilt); got != 2 {
		t.Errorf("bad -from: exit %d, want 2", got)
	}
	// the default floor refuses this one-file tree, and an empty directory
	// fails at any floor: neither may pass as clean
	if got := run([]string{"-check", root}, &out, &errb, testBuilt); got != 2 {
		t.Errorf("tiny tree under the default floor: exit %d, want 2", got)
	}
	errb.Reset()
	if got := run(append(append([]string{}, flags...), "-check", t.TempDir()), &out, &errb, testBuilt); got != 2 || !strings.Contains(errb.String(), "not the repository root") {
		t.Errorf("empty root: exit %d, stderr %q; want 2 and a refusal", got, errb.String())
	}
	// a symlink to the tree walks the tree
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if got := run(append(append([]string{}, flags...), "-check", link), &out, &errb, testBuilt); got != 0 || !strings.Contains(out.String(), "1 files read") {
		t.Errorf("symlinked root: exit %d, stdout %q", got, out.String())
	}
}

// testBuilt stands for the FirstV3ModuleTag a binary is built with.
const testBuilt = "v3.0.7"

// TestFirstV3Tag: the default move needs the first /v3 tag, from the flag or
// the build; a tag that is not a v3 release and a tag for another move are
// refused; the tag is written into go.mod's boundary and the pages; and a
// writing run whose page edit finds neither its old nor its new text fails.
func TestFirstV3Tag(t *testing.T) {
	t.Parallel()
	tree := func(files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for rel, body := range files {
			p := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	gomod := "module " + modpath.DefaultFrom + "\n"
	exit := func(built string, args ...string) (int, string, string) {
		t.Helper()
		var out, errb strings.Builder
		got := run(append([]string{"-min-files", "1"}, args...), &out, &errb, built)
		return got, out.String(), errb.String()
	}

	root := tree(map[string]string{"go.mod": gomod})
	if got, _, errs := exit("", root); got != 2 || !strings.Contains(errs, "-first-v3-tag") {
		t.Errorf("default move, no tag built or given: exit %d, stderr %q; want 2 naming -first-v3-tag", got, errs)
	}
	for _, bad := range []string{"v3.0.7-rc.1", "v3.0.7+meta", "v2.9.0", "3.0.7", "v3.0", "v3.00.7"} {
		if got, _, errs := exit("", "-first-v3-tag", bad, root); got != 2 || !strings.Contains(errs, "v3 release tag") {
			t.Errorf("-first-v3-tag %s: exit %d, stderr %q; want 2 and a refusal", bad, got, errs)
		}
	}
	if got, _, errs := exit("", "-from", "example.com/acme/widget/v2", "-to", "example.com/acme/widget/v3", "-first-v3-tag", "v3.0.7", root); got != 2 || !strings.Contains(errs, "default move") {
		t.Errorf("a first tag for another move: exit %d, stderr %q; want 2", got, errs)
	}
	if got, out, _ := exit("", "-check", root); got != 2 || out != "" {
		t.Errorf("-check of an unmoved tree with no tag: exit %d stdout %q; want 2 before reading", got, out)
	}

	usage := "`v2.5.0` is the newest tag at this writing.\n"
	root = tree(map[string]string{"go.mod": gomod, "docs/USAGE.md": usage, "internal/modulemove/modulemove.go": fixupOld(t, "internal/modulemove/modulemove.go")})
	if got, out, errs := exit("", "-first-v3-tag", "v3.0.7", root); got != 0 {
		t.Fatalf("move with the flag: exit %d\n%s%s", got, out, errs)
	}
	src, err := os.ReadFile(filepath.Join(root, "internal/modulemove/modulemove.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const FirstV3ModuleTag = "v3.0.7"`) || !strings.Contains(string(src), "(tags v3.0.0 to v3.0.6)") {
		t.Errorf("the boundary was not set to the tag:\n%s", src)
	}
	// on the moved tree, the built tag is enough for -check
	if got, out, _ := exit(testBuilt, "-check", root); got != 0 {
		t.Errorf("-check with the built tag: exit %d\n%s", got, out)
	}

	root = tree(map[string]string{"go.mod": gomod, "docs/USAGE.md": "a reworded page\n"})
	if got, out, _ := exit("", "-first-v3-tag", "v3.0.7", root); got != 1 || !strings.Contains(out, "NOTE docs/USAGE.md") {
		t.Errorf("a page edit that finds neither text: exit %d\n%s; want 1 and the note", got, out)
	}
}

func fixupOld(t *testing.T, file string) string {
	t.Helper()
	for _, f := range modpath.Fixups(testBuilt) {
		if f.File == file {
			return f.Old
		}
	}
	t.Fatalf("no edit for %s", file)
	return ""
}
