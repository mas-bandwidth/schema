package jstable

// THE GUARD ON THE JAVASCRIPT ROADMAP TESTS' GATES. jsNode SKIPS a test when no
// node is on PATH (make/js.mk says so above tables-js-versioning), so `go test
// ./...` asserts nothing for the message-form and fixed-roadmap tests: only a
// make target that sets SCHEMA_REQUIRE_CORPUS=1 and refuses a SKIP line makes
// them run. A ledger row cannot honestly be :done on a test no target runs, and
// the next roadmap test added without a target would ride into CI unrun.
//
// This test reads files only (no node, no clock, no network): every top-level
// Test function in this package whose name starts TestFixedRoadmap or
// TestMessageForm, plus the two small message-form regressions, must be named in
// a -run pattern of make/js.mk.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gateRequiredExtra are message-form regressions whose names do not carry the
// TestMessageForm prefix.
var gateRequiredExtra = []string{
	"TestJsMessageSkipOfZeroWidthElementsIsBounded",
	"TestJsAnnounceKeepsTwoDeclaredShapesThatDeriveOneQuantization",
}

func TestEveryJavaScriptRoadmapTestIsRunByAMakeTarget(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no *_test.go files read (%v)", err)
	}
	need := map[string]string{}
	for _, name := range gateRequiredExtra {
		need[name] = "named by this guard"
	}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			n := fn.Name.Name
			if strings.HasPrefix(n, "TestFixedRoadmap") || strings.HasPrefix(n, "TestMessageForm") {
				need[n] = f
			}
		}
	}

	mk, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "js.mk"))
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, line := range strings.Split(string(mk), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if i := strings.Index(line, "-run "); i >= 0 {
			patterns = append(patterns, line[i+len("-run "):])
		}
	}
	run := strings.Join(patterns, "\n")

	var missing []string
	for n, f := range need {
		if !gateContainsWord(run, n) {
			missing = append(missing, n+" ("+f+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d test(s) are named by no -run pattern in make/js.mk, so go test ./... skips them with no node and nothing makes them run:\n  %s\nname each in a target that sets SCHEMA_REQUIRE_CORPUS=1 and refuses a SKIP line, and wire it into test-js", len(missing), strings.Join(missing, "\n  "))
	}
}

// gateContainsWord reports whether name occurs in s as a whole identifier, so
// a shorter test name is not satisfied by a longer one.
func gateContainsWord(s, name string) bool {
	isID := func(b byte) bool {
		return b == '_' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
	}
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], name)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(name)
		if (i == 0 || !isID(s[i-1])) && (end == len(s) || !isID(s[end])) {
			return true
		}
		from = i + 1
	}
	return false
}
