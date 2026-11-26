package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// MAKE CARDS-TEST TESTS THE CARDS, AND ONLY THE CARDS. Its recipe once ended
// with go test over the whole of ./internal/ci and ./tools/agentsmap under a
// 600 s limit: every test in both packages is already a registry row run by
// name in its own tier, so the line ran ~400 CPU-s twice, nightly-only rows
// among them without the builds they need, and went red at 600 s on a loaded
// bench (harness-run.make-go.cards-test). A go test the recipe runs from the
// repository root names its tests with -run, or does not run there at all.
func TestCardsTestRunsNoWholeRepoPackage(t *testing.T) {
	t.Parallel()
	mk, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	cmds, ok := extractMakeRecipe(string(mk), "\ncards-test:")
	if !ok || len(cmds) == 0 {
		t.Fatal("make/go.mk has no cards-test recipe to judge")
	}
	if bad := wholeRepoPackageRuns(cmds); len(bad) > 0 {
		for _, c := range bad {
			t.Errorf("make/go.mk cards-test runs whole repository packages from the root: %s", c)
		}
	}
}

// The judge on the recipe as it stood, so the check above cannot pass by
// judging nothing.
func TestCardsTestWholeRepoPackageJudgeCatchesThePlant(t *testing.T) {
	t.Parallel()
	planted := []string{
		"./bin/schema check tables/cards",
		"cd build/tables-generated-go/cards && go vet ./... && $(GO_TEST) -v -count=1 -timeout 600s ./...",
		"$(GO_TEST) -count=1 -timeout 600s ./internal/ci/ ./tools/agentsmap/",
	}
	bad := wholeRepoPackageRuns(planted)
	if len(bad) != 1 || bad[0] != planted[2] {
		t.Fatalf("judged %q, want only the whole-package line %q", bad, planted[2])
	}
	if got := wholeRepoPackageRuns([]string{"$(GO_TEST) -count=1 -run '^TestX$$' ./internal/ci/"}); len(got) != 0 {
		t.Fatalf("a named -run selection judged whole: %q", got)
	}
	for _, c := range []string{"$(GO_TEST) -run . ./internal/ci/", "go test -count=1 ./..."} {
		if got := wholeRepoPackageRuns([]string{c}); len(got) != 1 {
			t.Errorf("%q judged %q, want it named: an unanchored -run or ./... is the whole package", c, got)
		}
	}
}

var (
	goTestCmdRe  = regexp.MustCompile(`\$\(GO_TEST\)|\bgo test\b`)
	repoPkgArgRe = regexp.MustCompile(`(?:^|\s)\./(?:(?:internal|tools|cmd|compiler)\b[^\s]*|\.\.\.)`)
	namedRunRe   = regexp.MustCompile(`-run[= ]'?\^`)
)

// wholeRepoPackageRuns is each command that runs go test, from the
// repository root (no cd into another module first), over a repository
// package with no -run selection anchored at ^ (a test named, not ".").
func wholeRepoPackageRuns(cmds []string) []string {
	var bad []string
	for _, c := range cmds {
		if strings.HasPrefix(c, "cd ") || !goTestCmdRe.MatchString(c) || namedRunRe.MatchString(c) {
			continue
		}
		if repoPkgArgRe.MatchString(c) {
			bad = append(bad, c)
		}
	}
	return bad
}
