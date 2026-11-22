package goosetable_test

// EVERY PROCESS A GOOSE TEST STARTS GOES THROUGH gooserun.Command
// (internal/codegen/goose/gooserun). gooserun.Command bounds the wall time and,
// on Linux, kills the process when the test binary dies, so no Goose process
// outlives the test that started it. This test reads every _test.go file of the
// package and refuses a bare exec.Command or exec.CommandContext outside the one
// deliberate re-exec that proves the kill: the helper in diewithparent_test.go,
// allowed by its enclosing function rather than by line. Its negative control
// holds a bare exec.Command in in-memory source and requires it refused by name.

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/execref"
)

// gooserunUseExempt names the calls a bare exec.Command is right for, by the
// file and enclosing function that hold them. TestGooseDieWithParent re-executes
// this test binary on purpose to show its driver dies with it.
var gooserunUseExempt = map[string]map[string]bool{
	"diewithparent_test.go": {"TestGooseDieWithParent": true},
}

// checkGooserunUse returns one line per reference to os/exec's Command or
// CommandContext in files that is not exempt, naming file, line and function.
// Each name is resolved to what it binds to (internal/execref), and a
// reference counts whether it is called or taken as a value: read by spelling,
// a dot import, and a constructor assigned to a variable and called through
// it, passed, while a local value whose method is named Command was refused
// (the reviewer's F-230).
func checkGooserunUse(files map[string][]byte) []string {
	var out []string
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		refs, err := execref.Find(token.NewFileSet(), name, files[name], "os/exec", "Command", "CommandContext")
		if err != nil {
			out = append(out, fmt.Sprintf("%s: parse: %v", name, err))
			continue
		}
		for _, r := range refs {
			if gooserunUseExempt[name][r.Func] {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d: exec.%s in %s must use gooserun.Command", name, r.Pos.Line, r.Name, r.Func))
		}
	}
	slices.Sort(out)
	return out
}

func TestGooseTestsStartProcessesThroughGooserun(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	if len(files) == 0 {
		t.Fatal("no _test.go files to check")
	}
	if bad := checkGooserunUse(files); len(bad) > 0 {
		t.Fatalf("every process a Goose test starts must go through gooserun.Command:\n%s", strings.Join(bad, "\n"))
	}
}

func TestGooseTestsStartProcessesThroughGooserunNegativeControl(t *testing.T) {
	t.Parallel()
	// The call is split across two literals: this is source text held in
	// memory for the check to refuse, and it starts no process.
	const src = `package goosetable_test

import "os/exec"

func startIt() {
	_ = exec.` + `Command("goose", "run", "main.goose")
}
`
	bad := checkGooserunUse(map[string][]byte{"control_test.go": []byte(src)})
	if len(bad) != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the check refused %d call(s), want 1: %v", len(bad), bad)
	}
	if !strings.Contains(bad[0], "control_test.go") || !strings.Contains(bad[0], "exec.Command") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the check did not refuse the bare exec.Command by name: %v", bad[0])
	}
}

// TestGooseTestsStartProcessesThroughGooserunPolicyCorpus runs this check
// over the committed child-process policy corpus
// (internal/execref/testdata/policy), which internal/ci's
// TestEveryTestChildIsBounded reader runs too (the reviewer's F-230): a dot
// import and a constructor taken as a value are refused, a local value whose
// method is named Command is not, beside the direct, renamed, type-only and
// bounded controls.
func TestGooseTestsStartProcessesThroughGooserunPolicyCorpus(t *testing.T) {
	t.Parallel()
	cases, err := execref.ReadPolicyCorpus(filepath.Join("..", "..", "..", filepath.FromSlash(execref.PolicyCorpusDir)))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		bad := checkGooserunUse(map[string][]byte{c.Fixture: c.Src})
		if len(bad) != c.Starts {
			t.Errorf("%s (%s): the check refused %d reference(s) %v, want %d", c.Fixture, c.Form, len(bad), bad, c.Starts)
		}
	}
}
