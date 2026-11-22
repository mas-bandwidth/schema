package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// A bare `go test -run X` that selects no test prints "ok ... [no tests to run]"
// and exits 0, so a recipe built on one can silently run nothing. Every make
// recipe that runs `go test -run` goes through tools/gotestrun ($(GOTESTRUN),
// or $(GOTESTRUN_ELSEWHERE) after a cd into another module), which fails a run
// that selected no test or in which every test skipped. This test fails on any
// recipe that still calls bare `go test ... -run`.
//
// Exempt: a -fuzz run (its -run '^$' selects no test on purpose), a -json run
// (the slow-gate coverage runs: the wrapper refuses -json, and slowgatescan
// -coverage accounts for every test in that transcript itself), comments, and
// text inside an echo or printf.

var bareGoTest = regexp.MustCompile(`(^|[^A-Za-z0-9_./$-])go test([ \t]|$)`)

var runFlag = regexp.MustCompile(`(^|\s)-run(\s|=)`)

// bareGoTestRuns returns "line: text" for each bare `go test ... -run` in a
// make file's text. Backslash continuations are joined first, so a -run on a
// continuation line counts.
func bareGoTestRuns(text string) []string {
	var found []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		start := i
		logical := lines[i]
		for strings.HasSuffix(logical, "\\") && i+1 < len(lines) {
			i++
			logical = strings.TrimSuffix(logical, "\\") + " " + lines[i]
		}
		trim := strings.TrimLeft(logical, " \t@-+")
		if strings.HasPrefix(trim, "#") {
			continue
		}
		for _, m := range bareGoTest.FindAllStringIndex(logical, -1) {
			pre := logical[:m[0]]
			if strings.Contains(pre, "echo ") || strings.Contains(pre, "printf ") ||
				strings.Count(pre, "'")%2 == 1 || strings.Count(pre, "\"")%2 == 1 {
				continue
			}
			rest := logical[m[1]-1:]
			if end := strings.IndexAny(rest, ";&|><"); end >= 0 {
				rest = rest[:end]
			}
			if !runFlag.MatchString(rest) || strings.Contains(rest, "-fuzz") || strings.Contains(rest, " -json") {
				continue
			}
			found = append(found, fmt.Sprintf("line %d: %s", start+1, strings.TrimSpace(logical)))
		}
	}
	return found
}

func TestUnitNoMakeRecipeCallsBareGoTestRun(t *testing.T) {
	root := repoRoot(t)
	files, err := provesran.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no make/*.mk found: this gate would pass over an empty set")
	}
	files = append(files, filepath.Join(root, "Makefile"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range bareGoTestRuns(string(b)) {
			t.Errorf("%s %s\n\tuse $(GOTESTRUN) (or $(GOTESTRUN_ELSEWHERE) after a cd into another module), not bare go test -run: a pattern that selects nothing must be red", filepath.Base(f), hit)
		}
	}
}

// The lint must go red on a bare call in every shape a recipe uses, and stay
// quiet on the wrapper, comments, echo text and fuzz runs.
func TestUnitBareGoTestRunLintMatchesTheShapes(t *testing.T) {
	red := []string{
		"\tgo test -run TestX ./pkg",
		"\tSCHEMA_SLOW=1 go test -count=1 ./pkg -run '^TestX$$' -v",
		"\t@if SCHEMA_SLOW=1 go test -overlay a.json ./pkg \\\n\t\t-run TestX > log 2>&1; then exit 1; fi",
		"\tcd test/go-tables && go test -race -run=Fuzz -count 1 .",
		"\tnice -n 19 go test ./pkg -run X",
	}
	for _, r := range red {
		if len(bareGoTestRuns(r)) != 1 {
			t.Errorf("lint missed a bare call: %q", r)
		}
	}
	green := []string{
		"\t$(GOTESTRUN) ./pkg -run TestX",
		"\t$(GOTESTRUN) -each ./a ./b -run TestX",
		"\tcd m && $(GOTESTRUN_ELSEWHERE) -run TestX .",
		"\tgo test ./...",
		"\tgo test -count=1 ./pkg",
		"# a bare `go test -run X` in a comment",
		"\t@printf 'go test -run X\\n'",
		"\tgo test ./pkg/ -run '^$$' -fuzz FuzzX -fuzztime 10s",
		"\tgo test -json -run \"$$(x)\" ./... > log",
		"\tgo test -count=1 ./pkg > out; other -run x",
	}
	for _, g := range green {
		if hits := bareGoTestRuns(g); len(hits) != 0 {
			t.Errorf("lint flagged %q: %v", g, hits)
		}
	}
}
