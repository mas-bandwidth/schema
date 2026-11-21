package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Nightly and dispatch runs have no time limit (the owner, 2026-11-16 and
// 2026-11-21: "THERE IS NO TIME LIMIT ON NIGHTLY TESTS"; "the whole point of
// the nightly is to NOT have a time limit, so it can finish"). A package that
// runs long is never red for its time. tools/gotestreport once defaulted to a
// 10-minute package budget and the nightly passed -budget 10m, which turned
// gotable (10m16s) and the compiler package (16m37s) red with every test
// passing. This holds that no nightly workflow gives gotestreport a budget.
func TestNightlyGoTestReportHasNoBudget(t *testing.T) {
	root := filepath.Join("..", "..")
	budget := regexp.MustCompile(`gotestreport[^\n]*-budget`)
	for _, wf := range []string{"ci-full.yml", "certify.yml"} {
		b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", wf))
		if err != nil {
			t.Fatal(err)
		}
		if m := budget.Find(b); m != nil {
			t.Errorf("%s gives gotestreport a time budget (%q); nightly runs have no time limit", wf, m)
		}
	}
	src, err := os.ReadFile(filepath.Join(root, "tools", "gotestreport", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`flag\.Duration\("budget", 0,`).Match(src) {
		t.Errorf("tools/gotestreport's -budget default is not 0; a report with no flag must never fail a package for its time")
	}
}
