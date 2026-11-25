package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRowSkips: every row listRows writes carries no skip, a split row
// and its complement become the names they run, a package with a nightly
// test is listed even when whole, and a whole package with none is left as
// it is.
func TestNoRowSkips(t *testing.T) {
	t.Parallel()
	names := map[string][]string{"./s": {"TestHeavy", "TestOther", "TestNew"}, "./n": {"TestDay", "TestNight"}, "./w": {"TestW"}}
	nightly := map[string]bool{"./n:TestNight": true}
	list := func(pkgs []string, run, not string) (string, error) {
		var keep []string
		for _, p := range pkgs {
			for _, n := range names[p] {
				if (run == "" || regexp.MustCompile(run).MatchString(n)) && (not == "" || !regexp.MustCompile(not).MatchString(n)) && !nightly[p+":"+n] {
					keep = append(keep, n)
				}
			}
		}
		return "^(" + strings.Join(keep, "|") + ")$", nil
	}
	tb := table{splits: map[string][]string{"./s": {"^TestHeavy$", "^TestOther$"}}}
	rows, err := listRows(plan(tb, []string{"./s", "./n", "./w"}, false, 60), map[string]bool{"./n": true}, list)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		if r.Skip != "" {
			t.Errorf("a row skips: %+v", r)
		}
		got[r.Packages+" "+r.Run] = true
	}
	for _, want := range []string{"./s ^TestHeavy$", "./s ^(TestOther)$", "./s ^(TestNew)$", "./n ./w ^(TestDay|TestW)$"} {
		if !got[want] {
			t.Errorf("no row %q in %v", want, got)
		}
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "skip") {
		t.Errorf("the matrix carries a skip: %s", out)
	}
}

// TestSubtestComplementIsWhatTheBucketsLeave: the complement of the table's
// seed buckets names every other child and none of theirs; a child that is
// not a numbered bucket, or a digit in two buckets, is refused by name.
func TestSubtestComplementIsWhatTheBucketsLeave(t *testing.T) {
	t.Parallel()
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	var children []string
	for _, s := range tb.subsplits["./internal/fuzz"] {
		children = append(children, s.child)
	}
	comp, err := subtestComplement(children)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(comp)
	for _, name := range []string{"seed#9", "seed#19", "seed#123x", "seed#", "seed", "s", "named-seed", "0123abcd", "seedling"} {
		if !re.MatchString(name) {
			t.Errorf("the complement %s leaves out %q, which no bucket runs", comp, name)
		}
	}
	for _, name := range []string{"seed#0", "seed#11", "seed#5", "seed#18"} {
		if re.MatchString(name) {
			t.Errorf("the complement %s runs %q, which a bucket runs", comp, name)
		}
	}
	if _, err := subtestComplement([]string{"^seed#[0-9]*[01]$", "^named$"}); err == nil || !strings.Contains(err.Error(), "cannot be listed") {
		t.Errorf("a child that is not a numbered bucket: %v", err)
	}
	if _, err := subtestComplement([]string{"^seed#[0-9]*[01]$", "^seed#[0-9]*[12]$"}); err == nil || !strings.Contains(err.Error(), "two subsplit buckets") {
		t.Errorf("a digit in two buckets: %v", err)
	}
}
