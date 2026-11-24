package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// verifySelector returns ci-fast.yml's plan selector, from the broad rule to
// its outputs, with tail appended to print what a test reads.
func verifySelector(t *testing.T, root, tail string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(rest, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(tail)
	return script.String()
}

// runSelector runs the selector script over one changed-file list.
func runSelector(t *testing.T, root, script, files string) string {
	t.Helper()
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
	cmd.Env = append(scriptEnv(os.Environ()), "files="+files)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan script: %v: %s", err, out)
	}
	return selectorOutput(out)
}

// everyLegJSON is verify_legs_json naming all 18 legs.
func everyLegJSON(t *testing.T) string {
	t.Helper()
	legs := slices.Clone(inputfamilies.Legs)
	if len(legs) != 18 {
		t.Fatalf("inputfamilies.Legs has %d legs; want 18", len(legs))
	}
	slices.Sort(legs)
	return `["` + strings.Join(legs, `","`) + `"]`
}

// TestFastPlanSharedCorpusSelectsEveryLeg runs ci-fast.yml's plan selector
// over a change to the shared conformance corpus and requires a verify-leg
// row for every one of the 18 legs: each leg's verify-<leg>-conformance runs
// the conformance harness over testdata/conformance/tables/MANIFEST.txt.
//
// PR 4908 changed MANIFEST.txt (rows that reach every leg) and the plan
// selected verify rows only for the legs whose code it touched: no cpp row,
// and verify-leg (elixir, conformance) skipped, which the verdict read as
// green. testdata/pr4908-changed-files.txt is that PR's whole file list.
// The corpus paths come from tools/inputfamilies' conformance-corpus family,
// so a new input there is a new case here, never a hand list.
func TestFastPlanSharedCorpusSelectsEveryLeg(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	script := verifySelector(t, root, `printf '%s' "$verify_legs_json"`)
	want := everyLegJSON(t)

	pr, err := os.ReadFile(filepath.Join(root, "internal/ci/testdata/pr4908-changed-files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, files string }{
		{"pr4908", strings.TrimSpace(string(pr))},
	}
	var corpus *inputfamilies.Family
	for i := range inputfamilies.Families {
		if inputfamilies.Families[i].Name == "conformance-corpus" {
			corpus = &inputfamilies.Families[i]
		}
	}
	if corpus == nil || len(corpus.Inputs) == 0 {
		t.Fatal("inputfamilies has no conformance-corpus family with inputs")
	}
	for _, in := range corpus.Inputs {
		// a file inside a "dir/" input, or the pattern's own text with its
		// wildcard filled in
		f := in
		if strings.HasSuffix(in, "/") {
			f = in + "MANIFEST.txt"
		}
		f = strings.ReplaceAll(f, "*", "x")
		if !corpus.Matches(f) {
			t.Fatalf("synthetic corpus file %q does not match its family", f)
		}
		cases = append(cases, struct{ name, files string }{"corpus " + f, f})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			if got := runSelector(t, root, script, tc.files); got != want {
				t.Fatalf("a shared conformance corpus change selected verify_legs=%s; want every leg %s", got, want)
			}
		})
	}
}

// TestFastPlanSharedVerifyCodeSelectsEveryLeg holds ci-fast.yml's plan to
// select all 18 verify legs for a non-test change to code every leg's verify
// rows run: the conformance harness, the test/verify-* cross-port tools, the
// main-module packages they import that no leg owns, and the make templates
// that define verify-<leg>-<gate>. The packages are derived from `go list
// -deps` of those programs under every schema_leg_* tag their files name, and
// the templates from the make files with a verify-$(1) rule header (a make
// file that only calls those targets, a negative control, is not one), so a new
// import or template no leg owns is red here, never a stale hand list. A
// package a leg owns (its add_leg rule selects it) keeps per-leg selection.
// A test-only harness change selects nothing: it changes no leg's answer.
func TestFastPlanSharedVerifyCodeSelectsEveryLeg(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	script := verifySelector(t, root, `printf '%s|%s' "$touched_legs" "$verify_legs_json"`)
	want := everyLegJSON(t)

	// the programs every leg's verify rows run
	tools := []string{"test/conformance/harness"}
	verifyDirs, err := provesran.Glob(filepath.Join(root, "test", "verify-*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range verifyDirs {
		rel, _ := filepath.Rel(root, filepath.Dir(m))
		tools = append(tools, filepath.ToSlash(rel))
	}
	// every schema_leg_* tag their sources name, so each leg's variant is in
	// the import closure
	tagRe := regexp.MustCompile(`\bschema_leg_[a-z]+\b`)
	var tags []string
	for _, d := range tools {
		srcs, err := provesran.Glob(filepath.Join(root, d, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range srcs {
			b, err := os.ReadFile(s)
			if err != nil {
				t.Fatal(err)
			}
			for _, tag := range tagRe.FindAllString(string(b), -1) {
				if !slices.Contains(tags, tag) {
					tags = append(tags, tag)
				}
			}
		}
	}
	slices.Sort(tags)
	args := []string{"list", "-deps", "-tags", strings.Join(tags, ","), "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}"}
	for _, d := range tools {
		args = append(args, "./"+d)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}
	const mod = "github.com/mas-bandwidth/schema/v2/"
	var deps []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if dir, ok := strings.CutPrefix(strings.TrimSpace(line), mod); ok {
			deps = append(deps, dir)
		}
	}
	for _, d := range tools {
		if !slices.Contains(deps, d) {
			t.Fatalf("go list -deps did not name its own root %s", d)
		}
	}

	// the make files that define every leg's verify targets
	mks, err := provesran.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	// a rule header: verify-$(1)-<gate>: or _verify-$(1)-<gate>:
	verifyRuleRe := regexp.MustCompile(`(?m)^_?verify-\$\(1\)[^:\s]*:`)
	var templates []string
	for _, mk := range mks {
		b, err := os.ReadFile(mk)
		if err != nil {
			t.Fatal(err)
		}
		if verifyRuleRe.MatchString(string(b)) {
			rel, _ := filepath.Rel(root, mk)
			templates = append(templates, filepath.ToSlash(rel))
		}
	}
	if len(templates) == 0 {
		t.Fatal("no make file defines a verify-$(1) rule")
	}

	type tc struct{ name, files string }
	cases := []tc{{"harness run.go", "test/conformance/harness/run.go"}}
	for _, d := range deps {
		cases = append(cases, tc{"package " + d, d + "/x.go"})
	}
	for _, mk := range templates {
		cases = append(cases, tc{"template " + mk, mk})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			touched, got, _ := strings.Cut(runSelector(t, root, script, c.files), "|")
			if strings.TrimSpace(touched) != "" && strings.HasPrefix(c.name, "package ") {
				return // a leg owns it: its add_leg rule selects that leg
			}
			if got != want {
				t.Fatalf("%s changed: verify_legs=%s; want every leg %s", c.files, got, want)
			}
		})
	}
	t.Run("harness test file only", func(t *testing.T) {
		t.Parallel()
		f := "test/conformance/harness/textzeromatrix_test.go"
		if got := runSelector(t, root, script, f); got != "|[]" {
			t.Fatalf("%s changed: selected %q; want no leg (a test file changes no leg's answer)", f, got)
		}
	})
}
