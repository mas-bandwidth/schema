package ci

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/modpath"
)

// modpathFloor is the fewest files the guard accepts from a walk of the real
// tree (about 11,500 today): a symlink, an empty directory or a wrong root
// must fail, never pass as clean.
const modpathFloor = 1000

// modpathErrors runs the module-path check over root and answers one line per
// offending file:line, each naming the command that fixes it, and the run's
// refusal when root is not the repository.
func modpathErrors(t *testing.T, root string, floor int) []string {
	t.Helper()
	r, err := modpath.New(modpath.DefaultFrom, modpath.DefaultTo)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := r.Run(root, true, floor)
	if err != nil {
		return []string{"the module-path check refused the root: " + err.Error()}
	}
	var errs []string
	for _, c := range rep.Changes {
		for _, n := range c.Lines {
			errs = append(errs, c.File+":"+strconv.Itoa(n)+": spells the old module path "+modpath.DefaultFrom+" (or writes a version that does not fit "+modpath.DefaultTo+"); run: "+modpath.Command)
		}
		if len(c.Lines) == 0 {
			errs = append(errs, c.File+": would change under the module-path move; run: "+modpath.Command)
		}
	}
	for _, p := range rep.Problems {
		errs = append(errs, p.String())
	}
	return errs
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestModulePathIsMoved holds the owner's ruling of 2026-11-21 ("v3 module
// path yes"): the module is modpath.DefaultTo, and no spelling of the old path
// modpath.DefaultFrom is left anywhere but the records internal/modpath
// allowlists and the lines it excepts. A branch that brings back an import, a
// go.mod line, a written go.mod, a doc line or a version stamp with the old
// path fails here, and the message names the one command that moves it.
func TestModulePathIsMoved(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if errs := modpathErrors(t, root, modpathFloor); len(errs) > 0 {
		if len(errs) > 40 {
			errs = append(errs[:40], strconv.Itoa(len(errs)-40)+" more")
		}
		t.Fatalf("the old module path is spelled outside the allowlist:\n%s", strings.Join(errs, "\n"))
	}

	// NEGATIVE CONTROLS: copies of real lines with the old path put back, in
	// a scratch tree, must each go red.
	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	short := strings.TrimPrefix(modpath.DefaultFrom, "github.com/")
	tail := short[strings.Index(short, "/")+1:]
	newMod := "module " + modpath.DefaultTo + "\n"
	for _, tc := range []struct {
		name, rel, content, want string
	}{
		{"the root go.mod says the old path", "go.mod", strings.Replace(string(rootMod), modpath.DefaultTo, modpath.DefaultFrom, 1), "go.mod:1:"},
		{"an import of the old path behind a build tag", "internal/x/leg.go", "//go:build schema_leg_moo\n\npackage x\n\nimport _ \"" + modpath.DefaultFrom + "/compiler\"\n", "internal/x/leg.go:5:"},
		{"a written go.mod requires the old path", "make/x.mk", "\t@printf 'require (\\n\\t" + modpath.DefaultFrom + " v2.0.0\\n)\\nreplace " + modpath.DefaultFrom + " => ../..\\n' > go.mod\n", "make/x.mk:1:"},
		{"a nested go.mod keeps a v2 version on the new path", "test/x/go.mod", "module x\n\nrequire " + modpath.DefaultTo + " v2.0.0\n\nreplace " + modpath.DefaultTo + " => ../..\n", "test/x/go.mod:3:"},
		{"the version stamp", "Makefile", "LDFLAGS := -X " + modpath.DefaultFrom + "/internal/version.version=$(VERSION)\n", "Makefile:1:"},
		{"a go install at main in a doc", "README.md", "go install " + modpath.DefaultFrom + "/cmd/schema@main\n", "README.md:1:"},
		{"the short spelling", "docs/X.md", "the module " + short + " is\n", "docs/X.md:1:"},
		{"the two-element spelling", "compiler/x_test.go", "var s = []byte(\"" + tail + "/tables/goruntime\")\n", "compiler/x_test.go:1:"},
		{"the excepted VERSIONING substring in another page", "docs/USAGE.md", "a pin resolves " + modpath.Exceptions[0].Substr + ".\n", "docs/USAGE.md:1:"},
	} {
		files := map[string]string{"go.mod": newMod}
		files[tc.rel] = tc.content
		errs := strings.Join(modpathErrors(t, writeTree(t, files), 1), "\n")
		if !strings.Contains(errs, tc.want) || !strings.Contains(errs, "run: "+modpath.Command) {
			t.Errorf("NEGATIVE CONTROL FAILED: %s: got %q, want an error at %s naming %q", tc.name, errs, tc.want, modpath.Command)
		}
	}

	// a pinned version with no replace is a problem, not a rewrite
	pinned := writeTree(t, map[string]string{"go.mod": newMod, "use/go.mod": "module use\n\nrequire " + modpath.DefaultFrom + " v2.4.0\n"})
	if errs := strings.Join(modpathErrors(t, pinned, 1), "\n"); !strings.Contains(errs, "use/go.mod:3: "+modpath.PinProblem) {
		t.Errorf("NEGATIVE CONTROL FAILED: a pinned require with no replace: got %q", errs)
	}

	// a root that is not the repository is refused, never clean: an empty
	// directory, and a symlink to a small tree under the real floor
	for name, dir := range map[string]string{
		"an empty root":          t.TempDir(),
		"a tree under the floor": writeTree(t, map[string]string{"go.mod": newMod}),
		"a root with no go.mod":  writeTree(t, map[string]string{"a.go": "package a\n"}),
		"a symlink to a small tree": func() string {
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(writeTree(t, map[string]string{"go.mod": newMod}), link); err != nil {
				t.Fatal(err)
			}
			return link
		}(),
	} {
		if errs := strings.Join(modpathErrors(t, dir, modpathFloor), "\n"); !strings.Contains(errs, "refused the root") {
			t.Errorf("NEGATIVE CONTROL FAILED: %s passed: %q", name, errs)
		}
	}

	// and an allowlisted record, and the excepted line in its own page, keep
	// their text without going red
	kept := writeTree(t, map[string]string{
		"go.mod":                   newMod,
		"bench/results/run.md":     "ok " + modpath.DefaultFrom + "/bench 1s\n",
		modpath.Exceptions[0].File: "a pin resolves " + modpath.Exceptions[0].Substr + ".\n",
	})
	if errs := modpathErrors(t, kept, 1); len(errs) > 0 {
		t.Errorf("an allowlisted record or the excepted line went red: %v", errs)
	}
}
