package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// erring is a reader or writer that fails: the path list cannot be read, or
// the selection cannot be written.
type erring struct{}

func (erring) Read([]byte) (int, error)  { return 0, errors.New("planted read failure") }
func (erring) Write([]byte) (int, error) { return 0, errors.New("planted write failure") }

// legsOf runs the command over a tree and a path list on standard input.
func legsOf(t *testing.T, args []string, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

// tree writes a repository root holding the given files and returns it.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// THE SELECTION A WORKFLOW READS. A leg's make file selects its leg, and so
// does a file its recipe reads; a path nothing reads selects no leg and
// prints an empty line, never nothing. -explain names each path's legs on
// standard error and leaves the selection on standard output unchanged.
func TestLegpathsPrintsTheLegsItsTreeSelects(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{
		"make/lua.mk":             "lua-check:\n\tlua testdata/luafix/run.lua\n",
		"testdata/luafix/run.lua": "-- fixture\n",
		"README.md":               "# readme\n",
	})
	for _, tc := range []struct{ name, stdin, want string }{
		{"the leg's make file", "make/lua.mk\n", "lua\n"},
		{"a file the recipe reads", "testdata/luafix/run.lua\n", "lua\n"},
		{"a path no leg reads", "README.md\n", "\n"},
		{"blank lines are no paths", "\n  \nREADME.md\n\n", "\n"},
	} {
		code, out, errs := legsOf(t, []string{"-root", root, "-changed", "-"}, tc.stdin)
		if code != 0 || out != tc.want {
			t.Errorf("%s: exit %d, stdout %q; want exit 0, %q (stderr %q)", tc.name, code, out, tc.want, errs)
		}
	}
	code, out, errs := legsOf(t, []string{"-root", root, "-explain", "-changed", "-"}, "make/lua.mk\nREADME.md\n")
	if code != 0 || out != "lua\n" {
		t.Fatalf("-explain: exit %d, stdout %q; want exit 0, %q", code, out, "lua\n")
	}
	if want := "make/lua.mk: lua\nREADME.md: \n"; errs != want {
		t.Errorf("-explain wrote %q to stderr; want %q", errs, want)
	}

	// a list in a file reads the same as one on standard input
	list := filepath.Join(t.TempDir(), "changed.txt")
	if err := os.WriteFile(list, []byte("testdata/luafix/run.lua\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := legsOf(t, []string{"-root", root, "-changed", list}, ""); code != 0 || out != "lua\n" {
		t.Errorf("-changed <file>: exit %d, stdout %q; want exit 0, %q (stderr %q)", code, out, "lua\n", errs)
	}
}

// EVERY FAILURE IS EXIT 2 WITH A CAUSE. ci-fast.yml plan and ci-full.yml
// pr-select assign the output, so a non-zero exit fails the step; a failure
// that printed an empty line and exited 0 would select no leg and pass.
func TestLegpathsRefusesWithExitTwo(t *testing.T) {
	t.Parallel()
	good := tree(t, map[string]string{"make/lua.mk": "lua-check:\n\tlua x.lua\n"})
	noMake := tree(t, map[string]string{"README.md": "# no make directory\n"})
	emptyMake := tree(t, map[string]string{"make/README": "no .mk file here\n"})
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  string // a phrase stderr must carry
	}{
		{"no -changed", []string{"-root", good}, "", "usage: legpaths"},
		{"a stray argument", []string{"-root", good, "-changed", "-", "extra"}, "", "usage: legpaths"},
		{"an unknown flag", []string{"-root", good, "-bogus", "-changed", "-"}, "", "flag provided but not defined: -bogus"},
		{"a path list that does not exist", []string{"-root", good, "-changed", filepath.Join(good, "missing.txt")}, "", "missing.txt"},
		{"a root with no make directory", []string{"-root", noMake, "-changed", "-"}, "make/lua.mk\n", "legpaths:"},
		{"a make directory with no make file", []string{"-root", emptyMake, "-changed", "-"}, "make/lua.mk\n", "no make file under"},
	} {
		code, out, errs := legsOf(t, tc.args, tc.stdin)
		if code != 2 {
			t.Errorf("%s: exit %d (stdout %q); want 2", tc.name, code, out)
		}
		if out != "" {
			t.Errorf("%s: printed a selection %q on a failure", tc.name, out)
		}
		if !strings.Contains(errs, tc.want) {
			t.Errorf("%s: stderr %q does not name the cause (%q)", tc.name, errs, tc.want)
		}
	}

	// a list that fails mid-read, and a selection that cannot be written
	var out, errb bytes.Buffer
	if code := run([]string{"-root", good, "-changed", "-"}, erring{}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "planted read failure") {
		t.Errorf("unreadable standard input: exit %d, stderr %q; want 2 naming the read failure", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"-root", good, "-changed", "-"}, strings.NewReader("make/lua.mk\n"), erring{}, &errb); code != 2 || !strings.Contains(errb.String(), "planted write failure") {
		t.Errorf("unwritable standard output: exit %d, stderr %q; want 2 naming the write failure", code, errb.String())
	}
}
