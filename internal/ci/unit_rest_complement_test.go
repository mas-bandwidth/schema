package ci

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testselect"
)

// THE UNIT-REST COMPLEMENT RUNS NO NIGHTLY TEST. ci-full.yml's unit-rest row
// runs `go test` over every package `go list ./...` names less the ones its
// `grep -v` filters leave out, whole: every test in them, whatever its tier.
// A package holding a nightly test (testselect.Nightly: a nightly gotest row,
// or a nightly tier line of unmoved.tsv) is left out of it, and its other
// tests run on their own step, chosen by name (tools/gotestselect, which
// leaves the nightly ones to the nightly lane). The Ruby strict gate and the
// Squirrel all-port differential moved out of ./internal/ci into packages of
// their own (internal/ci/rubystrict, internal/ci/squirreldiff); the complement
// would have run both whole on every merge.
func TestUnitRestComplementRunsNoNightlyTest(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	nightly, err := testselect.Nightly(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nightly) == 0 {
		t.Fatal("testselect.Nightly names no nightly test: this test is reading the wrong registry")
	}
	for _, p := range unitRestComplementProblems(string(wf), nightly) {
		t.Error(p)
	}
}

// The judge, on a complement planted without the filter that leaves a nightly
// package out, and on a workflow with no complement at all.
func TestUnitRestComplementJudgeCatchesThePlant(t *testing.T) {
	t.Parallel()
	nightly := map[string]bool{"internal/ci/rubystrict:TestRubyGeneratedStrictGate": true, "compiler:TestSlow": true}
	step := "      - name: complement\n        if: matrix.group == 'unit-rest'\n        run: go test $(go list ./... | grep -v '/compiler$'%s)\n"
	if got := unitRestComplementProblems(fmt.Sprintf(step, " | grep -v '/internal/ci/'"), nightly); len(got) != 0 {
		t.Fatalf("a complement that leaves both packages out: %v", got)
	}
	got := unitRestComplementProblems(fmt.Sprintf(step, ""), nightly)
	if len(got) != 1 || !strings.Contains(got[0], "internal/ci/rubystrict") || !strings.Contains(got[0], "TestRubyGeneratedStrictGate") {
		t.Fatalf("a complement that runs internal/ci/rubystrict whole: %v; want it named, with its nightly test", got)
	}
	if got := unitRestComplementProblems("jobs: {}\n", nightly); len(got) != 1 || !strings.Contains(got[0], "no unit-rest complement") {
		t.Fatalf("a workflow with no complement: %v; want that named", got)
	}
}

var (
	complementRe = regexp.MustCompile(`go test \$\(go list \./\.\.\.((?: \| grep -v '[^']*')*)\)`)
	grepVRe      = regexp.MustCompile(`grep -v '([^']*)'`)
)

// unitRestComplementProblems names each package holding a nightly test
// ("<dir>:<Name>", dir relative to the module root) that the workflow's
// `go test $(go list ./... | grep -v ...)` step runs whole.
func unitRestComplementProblems(workflow string, nightly map[string]bool) []string {
	m := complementRe.FindAllStringSubmatch(workflow, -1)
	if len(m) != 1 {
		return []string{fmt.Sprintf("ci-full.yml: %d unit-rest complement steps (`go test $(go list ./... | grep -v ...)`), want exactly one: no unit-rest complement to judge, or more than one", len(m))}
	}
	var filters []*regexp.Regexp
	for _, g := range grepVRe.FindAllStringSubmatch(m[0][1], -1) {
		re, err := regexp.Compile(g[1])
		if err != nil {
			return []string{fmt.Sprintf("ci-full.yml: the complement's filter %q is not a pattern this test reads: %v", g[1], err)}
		}
		filters = append(filters, re)
	}
	byDir := map[string][]string{}
	for place := range nightly {
		dir, name, _ := strings.Cut(place, ":")
		byDir[dir] = append(byDir[dir], name)
	}
	var out []string
	for dir, names := range byDir {
		path := "github.com/mas-bandwidth/schema/v2"
		if dir != "" && dir != "." {
			path += "/" + dir
		}
		left := false
		for _, re := range filters {
			if re.MatchString(path) {
				left = true
				break
			}
		}
		if !left {
			sort.Strings(names)
			out = append(out, fmt.Sprintf("ci-full.yml's unit-rest complement runs ./%s whole, and it holds nightly tests %s: leave it out of the complement and run its other tests by name (tools/gotestselect)", dir, strings.Join(names, ", ")))
		}
	}
	sort.Strings(out)
	return out
}

// nightlyStepName is the unit-rest step that runs, by name, the other tests of
// the packages the complement leaves out for their nightly tests.
const nightlyStepName = "go test the packages holding nightly tests, less those tests"

// A PACKAGE LEFT OUT FOR ITS NIGHTLY TESTS STILL RUNS ITS OTHERS. Each package
// holding a nightly test and other tests too, that the unit-rest complement
// leaves out, has those other tests run by a go test step of ci-full.yml (the
// step above, by tools/gotestselect, or a row of its own): leaving a package
// out of the complement is never leaving its per-change tests unrun. A package
// holding only nightly tests (bench/tables/moo) has nothing to run here.
func TestUnitRestLeftOutPackagesRunTheirOtherTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	nightly, err := testselect.Nightly(root)
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]bool{}
	for place := range nightly {
		dir, _, _ := strings.Cut(place, ":")
		dirs[dir] = true
	}
	var withOthers []string
	for dir := range dirs {
		_, err := testselect.Select(build.Default, root, []string{filepath.Join(root, dir)}, nil, nil, false)
		switch {
		case err == nil:
			withOthers = append(withOthers, dir)
		case strings.Contains(err.Error(), "names no test"):
			// only nightly tests: nothing of it runs per change
		default:
			t.Fatalf("selecting the other tests of ./%s: %v", dir, err)
		}
	}
	if len(withOthers) == 0 {
		t.Fatal("no package holds both nightly and other tests: this test is reading the wrong registry")
	}
	sort.Strings(withOthers)
	for _, p := range leftOutPackageProblems(string(wf), withOthers) {
		t.Error(p)
	}
}

// The judge, on planted workflows: a left-out package no step names, a named
// step selecting every tier, and no named step at all.
func TestUnitRestLeftOutJudgeCatchesThePlant(t *testing.T) {
	t.Parallel()
	withOthers := []string{"compiler", "internal/publicapi"}
	wf := func(step string) string {
		return "      - name: complement\n        if: matrix.group == 'unit-rest'\n        run: go test $(go list ./... | grep -v '/compiler$' | grep -v '/internal/publicapi$')\n" +
			"      - name: compiler\n        run: go test ./compiler\n" +
			"      # go test ./internal/publicapi (a comment names nothing)\n" + step
	}
	good := "      - name: " + nightlyStepName + "\n        if: matrix.group == 'unit-rest'\n        run: sel=$(go run ./tools/gotestselect ./internal/publicapi) || exit 1; go test -run \"$sel\" ./internal/publicapi\n"
	if got := leftOutPackageProblems(wf(good), withOthers); len(got) != 0 {
		t.Fatalf("both packages run their other tests: %v", got)
	}
	dropped := strings.ReplaceAll(good, " ./internal/publicapi", "")
	if got := leftOutPackageProblems(wf(dropped), withOthers); len(got) != 1 || !strings.Contains(got[0], "./internal/publicapi") {
		t.Fatalf("internal/publicapi dropped from the step: %v; want it, and only it, named", got)
	}
	every := strings.Replace(good, "gotestselect ", "gotestselect -tier all ", 1)
	if got := leftOutPackageProblems(wf(every), withOthers); len(got) != 1 || !strings.Contains(got[0], "-tier all") {
		t.Fatalf("the step selecting every tier: %v; want that named", got)
	}
	if got := leftOutPackageProblems(wf(""), withOthers); len(got) < 1 || !strings.Contains(strings.Join(got, "\n"), "no step named") {
		t.Fatalf("no named step: %v; want that named", got)
	}
}

var pkgTokenRe = regexp.MustCompile(`(?:^|[\s"'(])\./([A-Za-z0-9_./-]+)`)

// leftOutPackageProblems names each package of withOthers (it holds nightly
// tests and others) that the complement leaves out and that no go test or
// gotestselect line of the workflow names, comments aside; and the named
// step, if it is missing, has no run line, or selects every tier.
func leftOutPackageProblems(workflow string, withOthers []string) []string {
	m := complementRe.FindAllStringSubmatch(workflow, -1)
	if len(m) != 1 {
		return []string{fmt.Sprintf("ci-full.yml: %d unit-rest complement steps, want exactly one", len(m))}
	}
	var filters []*regexp.Regexp
	for _, g := range grepVRe.FindAllStringSubmatch(m[0][1], -1) {
		re, err := regexp.Compile(g[1])
		if err != nil {
			return []string{fmt.Sprintf("ci-full.yml: the complement's filter %q is not a pattern this test reads: %v", g[1], err)}
		}
		filters = append(filters, re)
	}
	lines := strings.Split(workflow, "\n")
	var out []string
	named := map[string]bool{}
	stepAt := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "- name: "+nightlyStepName {
			stepAt = i
		}
		if complementRe.MatchString(line) || (!strings.Contains(line, "go test") && !strings.Contains(line, "gotestselect")) {
			continue
		}
		for _, tok := range pkgTokenRe.FindAllStringSubmatch(line, -1) {
			named[tok[1]] = true
		}
	}
	if stepAt < 0 {
		out = append(out, fmt.Sprintf("ci-full.yml: no step named %q", nightlyStepName))
	} else {
		run := ""
		for _, line := range lines[stepAt+1 : min(stepAt+4, len(lines))] {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "run:") {
				run = t
				break
			}
		}
		switch {
		case run == "":
			out = append(out, fmt.Sprintf("ci-full.yml: the step %q has no run line", nightlyStepName))
		case strings.Contains(run, "-tier all"):
			out = append(out, fmt.Sprintf("ci-full.yml: the step %q selects with -tier all, so it runs the nightly tests it exists to leave out", nightlyStepName))
		}
	}
	for _, dir := range withOthers {
		path := "github.com/mas-bandwidth/schema/v2/" + dir
		left := false
		for _, re := range filters {
			if re.MatchString(path) {
				left = true
				break
			}
		}
		if !left || named[dir] {
			continue
		}
		under := false
		for n := range named {
			if p, ok := strings.CutSuffix(n, "/..."); ok && (dir == p || strings.HasPrefix(dir, p+"/")) {
				under = true
				break
			}
		}
		if !under {
			out = append(out, fmt.Sprintf("ci-full.yml's unit-rest complement leaves out ./%s for its nightly tests, and no go test step runs its other tests: list it on the step %q (tools/gotestselect)", dir, nightlyStepName))
		}
	}
	sort.Strings(out)
	return out
}
