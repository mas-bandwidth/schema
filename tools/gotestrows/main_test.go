package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCommittedSplitsRunEveryDeclaredTestExactlyOnce holds the table's
// patterns against declared tests and fuzz seed targets. The complement
// supplies the union, but overlapping patterns would run a test in two jobs.
func TestCommittedSplitsRunEveryDeclaredTestExactlyOnce(t *testing.T) {
	t.Parallel()
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string]bool{}
	for pkg := range tb.splits {
		packages[pkg] = true
	}
	for pkg := range tb.rides {
		packages[pkg] = true
	}
	for pkg := range tb.subsplits {
		packages[pkg] = true
	}
	for pkg := range packages {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()
			rows := plan(tb, []string{pkg}, true, 60)
			dir := filepath.Join("..", "..", strings.TrimPrefix(pkg, "./"))
			files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
			if err != nil {
				t.Fatal(err)
			}
			matches := make([]int, len(rows))
			for _, path := range files {
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv != nil || (!strings.HasPrefix(fn.Name.Name, "Test") && !strings.HasPrefix(fn.Name.Name, "Fuzz")) {
						continue
					}
					selected := 0
					for i, row := range rows {
						if selects(t, row, fn.Name.Name) {
							selected++
							matches[i]++
						}
					}
					want := 1
					for _, pat := range tb.ridePatterns(pkg) {
						if regexp.MustCompile(pat).MatchString(fn.Name.Name) {
							want = 0 // it rides its leg's verify row (a rides line)
						}
					}
					parent := ""
					for _, sub := range tb.subsplits[pkg] {
						if regexp.MustCompile(sub.parent).MatchString(fn.Name.Name) {
							if parent != "" && parent != sub.parent {
								t.Fatalf("%s has overlapping parent groups", fn.Name.Name)
							}
							parent = sub.parent
							want++
						}
					}
					// Subdivided parents run to discover children in each
					// child row and its complement; leaves run exactly once.
					if selected != want {
						t.Errorf("%s is selected by %d rows, want %d", fn.Name.Name, selected, want)
					}
				}
			}
			for i, count := range matches {
				if count == 0 {
					t.Errorf("row %+v selects no declared tests", rows[i])
				}
			}
		})
	}
}

// TestPlanRunsEveryTestExactlyOnce: every package lands in exactly one packed
// row or in its split rows, a split package's rows are its patterns plus the
// complement that skips all of them, and no packed row carries more than the
// budget unless it holds a single package.
func TestPlanRunsEveryTestExactlyOnce(t *testing.T) {
	tb := table{
		short:  map[string]float64{"./a": 50, "./b": 30, "./c": 25, "./big": 90},
		plain:  map[string]float64{},
		splits: map[string][]string{"./s": {"^(TestHeavy|FuzzHeavy)$", "^TestOther$"}},
	}
	pkgs := []string{"./a", "./b", "./c", "./big", "./s", "./unknown"}
	rows := plan(tb, pkgs, true, 60)

	count := map[string]int{}
	var split []Row
	for _, r := range rows {
		if r.Packages == "./s" {
			split = append(split, r)
			continue
		}
		if r.Run != "" || r.Skip != "" {
			t.Fatalf("a whole-package row carries a filter: %+v", r)
		}
		var load float64
		ps := strings.Fields(r.Packages)
		for _, p := range ps {
			count[p]++
			w, ok := tb.short[p]
			if !ok {
				w = defaultWeight
			}
			load += w
		}
		if len(ps) > 1 && load > 60 {
			t.Fatalf("row %q carries %.0f s, over the 60 s budget", r.Packages, load)
		}
	}
	for _, p := range []string{"./a", "./b", "./c", "./big", "./unknown"} {
		if count[p] != 1 {
			t.Fatalf("%s is in %d rows, want 1", p, count[p])
		}
	}
	if len(split) != 3 {
		t.Fatalf("./s has %d rows, want its 2 patterns and the complement: %+v", len(split), split)
	}
	// every test name lands in exactly one of the split rows
	for _, name := range []string{"TestHeavy", "TestOther", "TestNew", "TestHeavyish", "FuzzHeavy", "FuzzNew"} {
		n := 0
		for _, r := range split {
			if selects(t, r, name) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s is selected by %d of ./s's rows, want 1", name, n)
		}
	}
}

// TestPlanOverlappingSplitsRunEachTestOnce: a broad split line after a narrow
// one it overlaps (a name range after a single heavy test) takes only what the
// narrow line left, so the heavy test runs in its own row and nowhere else.
func TestPlanOverlappingSplitsRunEachTestOnce(t *testing.T) {
	t.Parallel()
	tb := table{short: map[string]float64{}, plain: map[string]float64{}, splits: map[string][]string{"./s": {"^TestHeavy$", "^TestFixed(Depth|Record)", "^Test[A-M]"}}}
	rows := plan(tb, []string{"./s"}, true, 60)
	if len(rows) != 4 {
		t.Fatalf("./s has %d rows, want its 3 patterns and the complement: %+v", len(rows), rows)
	}
	for _, name := range []string{"TestHeavy", "TestFixedDepthBound", "TestFixedOther", "TestAlpha", "TestMango", "TestZed", "TestHeavyish"} {
		n := 0
		for _, r := range rows {
			if selects(t, r, name) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s is selected by %d of ./s's rows, want 1: %+v", name, n, rows)
		}
	}
}

// TestPlanControlDroppedComplementLosesATest is the reversed witness: a split
// package without its complement row leaves an unnamed test in no row, and
// the exactly-once check above catches it.
func TestPlanControlDroppedComplementLosesATest(t *testing.T) {
	tb := table{short: map[string]float64{}, plain: map[string]float64{}, splits: map[string][]string{"./s": {"^TestHeavy$"}}}
	rows := plan(tb, []string{"./s"}, true, 60)
	if len(rows) != 2 {
		t.Fatalf("./s has %d rows, want its 1 pattern and the complement: %+v", len(rows), rows)
	}
	whole := 0
	for _, r := range rows {
		if selects(t, r, "TestNew") {
			whole++
		}
	}
	if whole != 1 {
		t.Fatalf("with the complement TestNew is selected %d times, want 1: the control has nothing to lose", whole)
	}
	broken := rows[:len(rows)-1] // the complement is the last split row
	n := 0
	for _, r := range broken {
		if selects(t, r, "TestNew") {
			n++
		}
	}
	if n > 0 {
		t.Fatalf("without the complement TestNew is still selected %d times; the control proves nothing", n)
	}
}

// TestTheTableParses: the committed table reads, and every split pattern
// compiles.
func TestTheTableParses(t *testing.T) {
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.short) == 0 {
		t.Fatal("the table has no weights")
	}
	for p, pats := range tb.splits {
		for _, pat := range pats {
			if _, err := regexp.Compile(pat); err != nil {
				t.Fatalf("%s: split %q: %v", p, pat, err)
			}
			if strings.Contains(pat, "/") {
				t.Fatalf("%s: split %q names a subtest; a split is a top-level test pattern", p, pat)
			}
		}
		if _, err := os.Stat(filepath.Join("..", "..", strings.TrimPrefix(p, "./"))); err != nil {
			t.Fatalf("split package %s does not exist: %v", p, err)
		}
	}
	for p, subs := range tb.subsplits {
		for _, sub := range subs {
			for _, pat := range []string{sub.parent, sub.child} {
				if pat == "" || strings.Contains(pat, "/") {
					t.Fatalf("%s: subsplit pattern %q must name one nonempty hierarchy level", p, pat)
				}
				if _, err := regexp.Compile(pat); err != nil {
					t.Fatalf("%s: subsplit %q: %v", p, pat, err)
				}
			}
		}
		if _, err := os.Stat(filepath.Join("..", "..", strings.TrimPrefix(p, "./"))); err != nil {
			t.Fatalf("subsplit package %s does not exist: %v", p, err)
		}
	}
}

func selects(t *testing.T, r Row, name string) bool {
	t.Helper()
	match := func(pattern string) (bool, bool) {
		pats, names := strings.Split(pattern, "/"), strings.Split(name, "/")
		for i := 0; i < len(pats) && i < len(names); i++ {
			if !regexp.MustCompile(pats[i]).MatchString(names[i]) {
				return false, false
			}
		}
		return true, len(pats) > len(names)
	}
	if ok, _ := match(r.Run); r.Run != "" && !ok {
		return false
	}
	if ok, partial := match(r.Skip); r.Skip != "" && ok && !partial {
		return false
	}
	return true
}

// This is the old namespace's exact partition, including future names that
// cannot be discovered from today's declarations. The generic declared-test
// proof above separately holds every current case to one execution.
func TestGoNamespaceSplitPreservesOldBoundary(t *testing.T) {
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{"^TestGo[A-M]", "^TestGo[N-R]", "^TestGo[^A-S]"}
	for _, pattern := range patterns {
		found := false
		for _, got := range tb.splits["./internal/codegen/gotable"] {
			found = found || got == pattern
		}
		if !found {
			t.Fatalf("missing namespace bucket %s", pattern)
		}
	}
	old := regexp.MustCompile("^TestGo[^S]")
	for _, char := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789λ" {
		name := "TestGo" + string(char) + "FutureCase"
		count := 0
		for _, pattern := range patterns {
			if regexp.MustCompile(pattern).MatchString(name) {
				count++
			}
		}
		want := 0
		if old.MatchString(name) {
			want = 1
		}
		if count != want {
			t.Errorf("%s selected %d times, old boundary requires %d", name, count, want)
		}
	}
}

// TestRidingTestRunsInNoRow: a rides line's test is skipped by every row of
// its package, packed, split or complement, and -rides-out names its leg for
// any selection that holds the package.
func TestRidingTestRunsInNoRow(t *testing.T) {
	t.Parallel()
	tb := table{short: map[string]float64{}, plain: map[string]float64{},
		splits: map[string][]string{"./s": {"^TestShared"}},
		rides:  map[string][]ride{"./s": {{pattern: "^TestDriverRefusal$", leg: "rust"}}, "./w": {{pattern: "^TestWhole$", leg: "go"}}}}
	for _, pkg := range []string{"./s", "./w"} {
		for _, r := range plan(tb, []string{pkg}, true, 60) {
			for _, name := range []string{"TestDriverRefusal", "TestWhole"} {
				if selects(t, r, name) && tb.ridePatterns(pkg) != nil && regexp.MustCompile(tb.ridePatterns(pkg)[0]).MatchString(name) {
					t.Errorf("row %+v runs the riding test %s", r, name)
				}
			}
		}
	}
	rows := plan(tb, []string{"./s"}, true, 60)
	if len(rows) != 2 || !selects(t, rows[0], "TestSharedFixtures") || !selects(t, rows[1], "TestOther") {
		t.Errorf("the split and complement rows must still run the package's other tests, got %+v", rows)
	}
	if got := rideLegs(tb, []string{"./a", "./s", "./w"}); strings.Join(got, " ") != "go rust" {
		t.Errorf("rideLegs = %v, want [go rust]", got)
	}
	if got := rideLegs(tb, []string{"./a"}); len(got) != 0 {
		t.Errorf("a selection without a riding package must name no leg, got %v", got)
	}
	// PLANTED: a table whose ride is dropped runs the test in a row again.
	tb.rides = nil
	ran := false
	for _, r := range plan(tb, []string{"./s"}, true, 60) {
		ran = ran || selects(t, r, "TestDriverRefusal")
	}
	if !ran {
		t.Error("PLANTED CONTROL FAILED: without its rides line the test ran in no row either: the check reads nothing")
	}
}

// TestEveryRideRunsInItsLegsRow: each committed rides line's test is run by
// the make target its leg's verify-leg conformance row runs, so a riding test
// is never a test that runs nowhere.
func TestEveryRideRunsInItsLegsRow(t *testing.T) {
	t.Parallel()
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	mk, err := os.ReadFile(filepath.Join("..", "..", "make", "verify-compiled.mk"))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for pkg, rides := range tb.rides {
		for _, r := range rides {
			n++
			if errs := rideErrs(string(mk), string(wf), pkg, r); len(errs) > 0 {
				t.Errorf("rides %s %s %s: %s", pkg, r.pattern, r.leg, strings.Join(errs, "; "))
			}
			// PLANTED: the go test line taken out of the make file
			planted := strings.Replace(string(mk), "-run '"+makeQuote(r.pattern)+"' "+pkg, "-run '^TestNothing$$' "+pkg, 1)
			if len(rideErrs(planted, string(wf), pkg, r)) == 0 {
				t.Errorf("PLANTED CONTROL FAILED: rides %s %s still read as run with its go test line gone", pkg, r.pattern)
			}
		}
	}
	if n == 0 {
		t.Fatal("the table has no rides line: the check reads nothing")
	}
}

// rideErrs answers why a ride's test would not run in its leg's row: the
// row's make target (verify-<leg>-conformance-and-driver-fuzz, which the
// verify-leg step runs for that leg's conformance gate) must reach a recipe
// that runs `go test ... -run '<pattern>' <pkg>`.
func rideErrs(mk, wf, pkg string, r ride) []string {
	var errs []string
	target := "verify-" + r.leg + "-conformance-and-driver-fuzz"
	if !strings.Contains(wf, "conformance) target="+target+" ;;") {
		errs = append(errs, "ci-fast.yml's verify-leg does not run "+target+" for the "+r.leg+" conformance gate")
	}
	ruleRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:\n\t@\$\(MAKE\)([^\n]*)`)
	m := ruleRe.FindStringSubmatch(mk)
	if m == nil {
		return append(errs, "make/verify-compiled.mk has no "+target)
	}
	runRe := regexp.MustCompile(`(?m)^([_a-z0-9-]+):[^\n]*\n(?:\t[^\n]*\n)*?\t[^\n]*go test [^\n]*-run '` + regexp.QuoteMeta(makeQuote(r.pattern)) + `' ` + regexp.QuoteMeta(pkg) + `\b`)
	rm := runRe.FindStringSubmatch(mk)
	if rm == nil {
		return append(errs, "no make recipe runs go test -run '"+r.pattern+"' "+pkg)
	}
	if !strings.Contains(" "+m[1]+" ", " "+rm[1]+" ") {
		errs = append(errs, target+" does not make "+rm[1]+", the target that runs the test")
	}
	return errs
}

// makeQuote answers a pattern as a makefile recipe spells it: $ doubled.
func makeQuote(s string) string { return strings.ReplaceAll(s, "$", "$$") }
