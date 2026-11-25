package ci

import (
	"fmt"
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
