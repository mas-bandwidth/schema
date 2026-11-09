package ci

import (
	"regexp"
	"strings"
	"testing"
)

// TestRunExceptionsNameExistingTests holds every -run exception anchored in
// make/go.mk or the Makefile to the test it excuses: the exception's where
// must still name a `go test -run` recipe in that file, and every test-name
// token in that recipe's pattern must name a test (or fuzz target) declared
// in a _test.go file in the repo. An exception whose line has drifted, or
// whose named test has been renamed or deleted, is stale and fails here
// rather than silently exempting a pattern that selects nothing. Patterns
// carrying a make variable or template ($( ... )) and intentional empty
// patterns ('^$', paired with -fuzz) name no literal test and are checked
// only for their anchor.
func TestRunExceptionsNameExistingTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := makefileFiles(t, root)

	recipesByWhere := map[string]runRecipe{}
	for _, name := range []string{"make/go.mk", "Makefile"} {
		text, ok := files[name]
		if !ok {
			t.Fatalf("makefile set does not include %s", name)
		}
		for _, r := range runRecipes(name, text) {
			recipesByWhere[r.where] = r
		}
	}
	if len(recipesByWhere) == 0 {
		t.Fatal("no go test -run recipes found in make/go.mk or Makefile")
	}

	testNames := testFunctionNames(t, root)
	if len(testNames) == 0 {
		t.Fatal("no test functions found in the repo")
	}

	tokenRe := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	checked := 0
	for _, e := range runPatternExceptions {
		file, _, _ := strings.Cut(e.where, ":")
		if file != "make/go.mk" && file != "Makefile" {
			continue
		}
		checked++
		if strings.TrimSpace(e.why) == "" {
			t.Errorf("%s: -run exception carries no reason", e.where)
			continue
		}
		r, ok := recipesByWhere[e.where]
		if !ok {
			t.Errorf("%s: stale -run exception: no go test -run recipe at this line in %s; delete the exception or re-anchor it", e.where, file)
			continue
		}
		if strings.Contains(r.pattern, "$(") {
			continue
		}
		for _, tok := range tokenRe.FindAllString(r.pattern, -1) {
			found := false
			for name := range testNames {
				if strings.Contains(name, tok) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: -run exception pattern %q names %q, which no test or fuzz function in any _test.go declares; delete the exception or fix the pattern", e.where, r.pattern, tok)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no -run exceptions in make/go.mk or Makefile: this gate is looking at the wrong list")
	}
	t.Logf("checked %d -run exceptions in make/go.mk and Makefile against %d declared test functions", checked, len(testNames))
}
