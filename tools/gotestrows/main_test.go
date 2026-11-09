package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPlanRunsEveryTestExactlyOnce: every package lands in exactly one packed
// row or in its split rows, a split package's rows are its patterns plus the
// complement that skips all of them, and no packed row carries more than the
// budget unless it holds a single package.
func TestPlanRunsEveryTestExactlyOnce(t *testing.T) {
	tb := table{
		short:  map[string]float64{"./a": 50, "./b": 30, "./c": 25, "./big": 90},
		plain:  map[string]float64{},
		splits: map[string][]string{"./s": {"^TestHeavy$", "^TestOther$"}},
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
	for _, name := range []string{"TestHeavy", "TestOther", "TestNew", "TestHeavyish"} {
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

// TestPlanControlDroppedComplementLosesATest is the reversed witness: a split
// package without its complement row leaves an unnamed test in no row, and
// the exactly-once check above catches it.
func TestPlanControlDroppedComplementLosesATest(t *testing.T) {
	tb := table{short: map[string]float64{}, plain: map[string]float64{}, splits: map[string][]string{"./s": {"^TestHeavy$"}}}
	rows := plan(tb, []string{"./s"}, true, 60)
	broken := rows[:len(rows)-1] // the complement is the last split row
	n := 0
	for _, r := range broken {
		if selects(t, r, "TestNew") {
			n++
		}
	}
	if n != 0 {
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
}

func selects(t *testing.T, r Row, name string) bool {
	t.Helper()
	if r.Run != "" && !regexp.MustCompile(r.Run).MatchString(name) {
		return false
	}
	if r.Skip != "" && regexp.MustCompile(r.Skip).MatchString(name) {
		return false
	}
	return true
}
