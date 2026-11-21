package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ciPolicyRowRe reads one row of ci-fast.yml's ci-policy matrix: the shard
// name, then its `go test` selection, `-run` or `-skip` of `^(A|B|...)`.
var ciPolicyRowRe = regexp.MustCompile(`(?m)^          - shard: ([a-z-]+)\n            select: -(run|skip) '\^\(([A-Za-z0-9_|]+)\)'\n`)

type ciPolicyRow struct {
	shard, flag string
	prefixes    []string
}

func ciPolicyRows(wf string) []ciPolicyRow {
	var rows []ciPolicyRow
	for _, m := range ciPolicyRowRe.FindAllStringSubmatch(yamlJobs(wf)["ci-policy"], -1) {
		rows = append(rows, ciPolicyRow{shard: m[1], flag: m[2], prefixes: strings.Split(m[3], "|")})
	}
	return rows
}

// ciPolicyShardErrors holds the ci-policy rows complete: exactly one `-skip`
// row, named `rest`, whose prefixes are exactly the union of every `-run`
// row's, no prefix in two rows, and every test name selected by exactly one
// row, the way `go test` selects a top-level test.
func ciPolicyShardErrors(wf string, tests []string) []string {
	var errs []string
	policy := yamlJobs(wf)["ci-policy"]
	if !strings.Contains(policy, "run: go test ${{ matrix.select }} ./internal/ci/") {
		errs = append(errs, "ci-policy does not run `go test ${{ matrix.select }} ./internal/ci/`")
	}
	rows := ciPolicyRows(wf)
	if got := strings.Count(policy, "          - shard: "); got != len(rows) || len(rows) < 2 {
		errs = append(errs, "ci-policy matrix rows must each be `- shard:` then `select: -run|-skip '^(A|B)'`")
		return errs
	}
	var union, skip []string
	skips := 0
	for _, r := range rows {
		switch r.flag {
		case "run":
			for _, p := range r.prefixes {
				if slices.Contains(union, p) {
					errs = append(errs, "prefix "+p+" is in two -run rows")
				}
				union = append(union, p)
			}
		case "skip":
			skips++
			skip = r.prefixes
			if r.shard != "rest" {
				errs = append(errs, "the -skip row must be named rest, not "+r.shard)
			}
		}
	}
	if skips != 1 {
		errs = append(errs, "ci-policy needs exactly one -skip row")
	}
	a, b := slices.Clone(union), slices.Clone(skip)
	sort.Strings(a)
	sort.Strings(b)
	if !slices.Equal(a, b) {
		errs = append(errs, "the rest row skips "+strings.Join(b, "|")+" but the -run rows select "+strings.Join(a, "|"))
	}
	for _, name := range tests {
		n := 0
		for _, r := range rows {
			matched := false
			for _, p := range r.prefixes {
				if strings.HasPrefix(name, p) {
					matched = true
				}
			}
			if matched == (r.flag == "run") {
				n++
			}
		}
		if n != 1 {
			errs = append(errs, name+" is selected by "+strconv.Itoa(n)+" ci-policy rows, want 1")
		}
	}
	for _, r := range rows {
		if r.flag != "run" {
			continue
		}
		for _, p := range r.prefixes {
			if !slices.ContainsFunc(tests, func(n string) bool { return strings.HasPrefix(n, p) }) {
				errs = append(errs, "row "+r.shard+" prefix "+p+" selects no test")
			}
		}
	}
	return errs
}

// packageTestNames lists the top-level Test, Example and Fuzz functions of
// the package in dir, the names `go test -run` and `-skip` select on.
func packageTestNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			for _, prefix := range []string{"Test", "Example", "Fuzz"} {
				if strings.HasPrefix(fn.Name.Name, prefix) && fn.Name.Name != "TestMain" {
					names = append(names, fn.Name.Name)
				}
			}
		}
	}
	return names
}

// TestCIPolicyShardsCoverEveryTest holds the three ci-policy rows of
// ci-fast.yml complete: every test in this package runs in exactly one row.
func TestCIPolicyShardsCoverEveryTest(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	tests := packageTestNames(t, ".")
	if len(tests) < 100 {
		t.Fatalf("found only %d tests in internal/ci", len(tests))
	}
	if errs := ciPolicyShardErrors(wf, tests); len(errs) != 0 {
		t.Fatal(strings.Join(errs, "\n"))
	}
	// NEGATIVE CONTROLS: a prefix dropped from the rest row's skip (its tests
	// would run twice), a prefix dropped from a -run row but still skipped
	// (its tests would run nowhere), and a new test no row names.
	for _, mutate := range []struct{ name, from, to string }{
		{"skip loses a prefix", "-skip '^(TestFastPlan|", "-skip '^("},
		{"run loses a prefix", "-run '^(TestFastPlan|", "-run '^("},
		{"go test unsharded", "run: go test ${{ matrix.select }} ./internal/ci/", "run: go test ./internal/ci/"},
	} {
		mutated := strings.Replace(wf, mutate.from, mutate.to, 1)
		if mutated == wf {
			t.Fatalf("negative control %q did not mutate ci-fast.yml", mutate.name)
		}
		if len(ciPolicyShardErrors(mutated, tests)) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %s stayed green", mutate.name)
		}
	}
}
