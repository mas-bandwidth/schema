package ci

import (
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/execref"
)

// boundedChildDirs are the packages whose tests start an interpreter or a
// compiled leg's toolchain and binaries, and the helper packages that start
// them for them. Every child process they start goes
// through internal/testproc, which kills it with its process group when the
// test ends, before the test binary's deadline, and (on Linux) when the binary
// dies (review-owed/ci-tools/test-children-bounded:
// TestLuauVariableSaveRefusesOffDeclaration once left a luau process spinning
// for six days on a bench).
var boundedChildDirs = []string{
	"internal/codegen/dart", "internal/codegen/darttable",
	"internal/codegen/elixir", "internal/codegen/elixirtable",
	"internal/codegen/js", "internal/codegen/jstable",
	"internal/codegen/lobster", "internal/codegen/lobstertable", "internal/codegen/lobster/lobsterrun",
	"internal/codegen/lua", "internal/codegen/luatable", "internal/codegen/lua/luarun",
	"internal/codegen/luau", "internal/codegen/luautable", "internal/codegen/luau/luaurun",
	"internal/codegen/moo", "internal/codegen/mootable", "internal/codegen/moo/moorun",
	"internal/codegen/python", "internal/codegen/pythontable", "internal/codegen/python/pyrun",
	"internal/codegen/ruby", "internal/codegen/rubytable", "internal/codegen/ruby/rubyrun",
	"internal/codegen/squirrel", "internal/codegen/squirreltable", "internal/codegen/squirrel/sqrun",
	"internal/codegen/c", "internal/codegen/ctable",
	"internal/codegen/cpp", "internal/codegen/cpptable",
	"internal/codegen/csharp", "internal/codegen/cstable", "internal/codegen/unitytable",
	"internal/codegen/golang", "internal/codegen/gotable", "internal/codegen/gotable/fuzzunit",
	"internal/codegen/java", "internal/codegen/javatable",
	"internal/codegen/rust", "internal/codegen/rusttable",
	"compiler",
	"tools/namebuild",
}

// helperDir reports whether dir is a run-helper package, whose every file
// (not only its tests) starts children for tests.
func helperDir(dir string) bool { return strings.HasSuffix(dir, "run") }

// unboundedChildren returns file:line for every reference to os/exec's
// Command or CommandContext in src, called or taken as a value, with each name
// resolved to what it binds to (internal/execref): through a renamed or a dot
// import it is found, and a local value or function that shadows the name is
// not one (the reviewer's F-230: read by spelling, a dot import passed and a
// local method was refused).
func unboundedChildren(fset *token.FileSet, name string, src []byte) ([]string, error) {
	refs, err := execref.Find(fset, name, src, "os/exec", "Command", "CommandContext")
	if err != nil {
		return nil, err
	}
	var at []string
	for _, r := range refs {
		at = append(at, r.Pos.Filename+":"+strconv.Itoa(r.Pos.Line))
	}
	return at, nil
}

// TestEveryTestChildIsBounded holds every child a covered test starts to
// internal/testproc: no _test.go file in a covered package, and no file in a
// run-helper package, calls exec.Command or exec.CommandContext.
func TestEveryTestChildIsBounded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fset := token.NewFileSet()
	var bad []string
	for _, dir := range boundedChildDirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("covered package %s: %v", dir, err)
		}
		read := 0
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !judgedFile(dir, n) {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, n))
			src, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			at, err := unboundedChildren(fset, rel, src)
			if err != nil {
				t.Fatal(err)
			}
			read++
			bad = append(bad, at...)
		}
		if read == 0 {
			t.Fatalf("covered package %s: no source was read, so nothing in it is held to testproc — the package moved, or the list is stale", dir)
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s starts a child with os/exec directly; use testproc.Command(t, ...) so it cannot outlive its test", b)
	}
}

// TestEveryTestChildIsBoundedNegativeControl plants each way a file can start
// an unbounded child and checks the gate's reader sees it, and that the
// bounded form passes.
func TestEveryTestChildIsBoundedNegativeControl(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      int
	}{
		{"exec.Command", "package p\nimport \"os/exec\"\nfunc f() { exec.Command(\"luau\") }\n", 1},
		{"exec.CommandContext", "package p\nimport (\"context\"; \"os/exec\")\nfunc f() { exec.CommandContext(context.Background(), \"luau\") }\n", 1},
		{"renamed import", "package p\nimport osexec \"os/exec\"\nfunc f() { osexec.Command(\"luau\") }\n", 1},
		{"taken as a value", "package p\nimport \"os/exec\"\nvar start = exec.Command\n", 1},
		{"bounded", "package p\nimport \"github.com/mas-bandwidth/schema/v2/internal/testproc\"\nfunc f(t testing.TB) { testproc.Command(t, \"luau\") }\n", 0},
		{"exec for its types only", "package p\nimport \"os/exec\"\nfunc f(c *exec.Cmd) error { var e *exec.ExitError; _ = e; return c.Run() }\n", 0},
	}
	planted := 0
	for _, c := range cases {
		at, err := unboundedChildren(token.NewFileSet(), c.name+".go", []byte(c.src))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(at) != c.want {
			t.Errorf("%s: the gate saw %d unbounded child(ren) %v, want %d", c.name, len(at), at, c.want)
		}
		if c.want > 0 {
			planted++
			if len(at) == 0 {
				t.Errorf("%s: the gate accepted a planted unbounded child", c.name)
			}
		}
	}
	if planted != 4 {
		t.Fatalf("the control planted %d unbounded children, want 4: it would pass having refused nothing", planted)
	}
}

// TestEveryTestChildIsBoundedPolicyCorpus runs this gate's reader over the
// committed child-process policy corpus (internal/execref/testdata/policy),
// which goosetable's reader runs too (the reviewer's F-230): a dot import and
// a constructor taken as a value start a child, a local value whose method is
// named Command starts none, beside the direct, renamed, type-only and
// bounded controls.
func TestEveryTestChildIsBoundedPolicyCorpus(t *testing.T) {
	t.Parallel()
	cases, err := execref.ReadPolicyCorpus(filepath.Join(repoRoot(t), filepath.FromSlash(execref.PolicyCorpusDir)))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		at, err := unboundedChildren(token.NewFileSet(), c.Fixture, c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Fixture, err)
		}
		if len(at) != c.Starts {
			t.Errorf("%s (%s): the gate saw %d unbounded child(ren) %v, want %d", c.Fixture, c.Form, len(at), at, c.Starts)
		}
	}
}
