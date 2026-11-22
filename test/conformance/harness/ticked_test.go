// THE TICKED-CELL RULE AND ITS REVERSED WITNESSES (ticked.go). A case whose
// roadmap row is ticked for a leg, answered ABSENT per case or per surface,
// turns the run red; the same absence on a row not ticked for the leg stays
// green and counts as not carried. Fake drivers over the one-instance corpus
// absence_test.go builds: no language leg, no network, no clock.
package main

import (
	"slices"
	"strings"
	"testing"
)

func tickedClaims(ticks tickSet) *claimSet {
	set := fakeClaims(nil, nil) // the unit uses no registered construct
	set.ticks = ticks
	return set
}

// TestTickedAbsentFails: a per-case `.absent` on a case whose row is ticked
// for the leg fails it, by name and row, and counts as owed in the coverage.
func TestTickedAbsentFails(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {caseGone: true}})
	ticks := tickSet{"variable-tables/fixed-class": {"zz": true}}
	out, ok, rows := c.runClaimed(t, drivers, tickedClaims(ticks))
	if ok {
		t.Fatalf("a TICKED case answered ABSENT left the run green:\n%s", out)
	}
	mustSay(t, out, []string{"zz / wire: alpha: TICKED and answered ABSENT", "variable-tables/fixed-class"}, []string{footer})
	r, found := findRow(rows, "zz", "wire")
	if !found || r.AbsentClaimed != 1 || r.NotCarried != 0 {
		t.Errorf("the coverage row does not count the ticked absence as owed: %+v (found %v)", r, found)
	}
}

// TestUntickedAbsentIsCoverage: the same absence where the row is ticked only
// for another leg, or a different row is ticked, stays green.
func TestUntickedAbsentIsCoverage(t *testing.T) {
	needShell(t)
	for name, ticks := range map[string]tickSet{
		"ticked for another leg":       {"variable-tables/fixed-class": {"yy": true}},
		"another row ticked for zz":    {"variable-tables/variable-class": {"zz": true}},
		"no ticks read (classes only)": nil,
	} {
		t.Run(name, func(t *testing.T) {
			c := newFakeCorpus(t)
			drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {caseGone: true}})
			out, ok, rows := c.runClaimed(t, drivers, tickedClaims(ticks))
			if !ok {
				t.Fatalf("an absence on an unticked row turned the run red:\n%s", out)
			}
			mustSay(t, out, []string{footer, "pass 0/0 +1a"}, []string{"TICKED"})
			if r, _ := findRow(rows, "zz", "wire"); r.NotCarried != 1 || r.AbsentClaimed != 0 {
				t.Errorf("the coverage row does not count the absence as not carried: %+v", r)
			}
		})
	}
}

// TestTickedAbsentSurfaceFails: a surface left out of `list`, or exited 2 on,
// fails the leg when the row its cases read is ticked for it; unticked, it is
// still only a missing surface.
func TestTickedAbsentSurfaceFails(t *testing.T) {
	needShell(t)
	for name, leg := range map[string]fakeLeg{
		"left out of list": {unlisted: "json-write"},
		"exit code 2":      {exitTwo: "json-write"},
	} {
		t.Run(name, func(t *testing.T) {
			c := newFakeCorpus(t)
			drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": leg})
			out, ok, rows := c.runClaimed(t, drivers, tickedClaims(tickSet{"variable-tables/text-form": {"zz": true}}))
			if ok {
				t.Fatalf("a ticked surface answered ABSENT left the run green:\n%s", out)
			}
			mustSay(t, out, []string{"zz / json-write: alpha.json: TICKED and the surface answered ABSENT", "variable-tables/text-form", "FAIL absent"}, []string{footer})
			if r, _ := findRow(rows, "zz", "json-write"); !r.SurfaceAbsent || r.AbsentClaimed != 1 || coverageCell(r) != "absent !1" {
				t.Errorf("the coverage row does not show the ticked surface as owed: %+v", r)
			}

			c = newFakeCorpus(t)
			drivers = c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": leg})
			out, ok, _ = c.runClaimed(t, drivers, tickedClaims(tickSet{"variable-tables/text-form": {"yy": true}}))
			if !ok {
				t.Fatalf("an unticked absent surface turned the run red:\n%s", out)
			}
		})
	}
}

// TestCaseRowsNameCommittedRoadmapRows holds caseRows to the committed
// ROADMAP.md: every surface reads at least one row for each class, and every
// row it names is in the table. A renamed or removed row is red here rather
// than a rule that stops applying.
func TestCaseRowsNameCommittedRoadmapRows(t *testing.T) {
	t.Chdir("../../..")
	ticks, err := readTicks(roadmapTablePath, roadmapDataPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range surfaces {
		for _, class := range []string{"fixed", "variable", ""} {
			rows := caseRows(s, class)
			if len(rows) == 0 {
				t.Errorf("surface %s (class %q) reads no roadmap row: add it to caseRows", s, class)
			}
			for _, row := range rows {
				if _, ok := ticks[row]; !ok {
					t.Errorf("surface %s (class %q) reads %s, which ROADMAP.md does not have", s, class, row)
				}
			}
		}
	}
	// a ticked column reads as ticked: the reference column of the fixed class
	if !ticks["variable-tables/fixed-class"][referenceLang] {
		t.Errorf("ROADMAP.md's fixed-class row is not read as ticked for %s: %v", referenceLang, ticks["variable-tables/fixed-class"])
	}
}

// TestParseTicks: the parser reads only ✅ cells, by column, and refuses a
// table row whose title the data does not name.
func TestParseTicks(t *testing.T) {
	data := `(:id "sec" :type :roadmap :title "S" :rows (("a-row" "first \"row\"") ("b" "second")) :columns (("x" "x")))`
	md := "<!-- nova-work:sec:start -->\n\n| feature | x | y |\n|---|:---:|:---:|\n| first \"row\" | ✅ |  |\n| second |  | ✅ |\n| complete | 50% | 50% |\n<!-- nova-work:sec:end -->\n"
	ticks, err := parseTicks(md, data, []string{"sec"})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for row, legs := range ticks {
		for lang := range legs {
			got = append(got, row+" "+lang)
		}
	}
	slices.Sort(got)
	if want := []string{"sec/a-row x", "sec/b y"}; !slices.Equal(got, want) {
		t.Errorf("ticks = %v, want %v", got, want)
	}
	if _, err := parseTicks(strings.Replace(md, "| second |", "| renamed |", 1), data, []string{"sec"}); err == nil || !strings.Contains(err.Error(), `"renamed" has no id`) {
		t.Errorf("a table row the data does not name was accepted: %v", err)
	}
	if _, err := parseTicks(md, data, []string{"missing"}); err == nil {
		t.Error("a section the data does not have was accepted")
	}
}

// TestEnforceTicked is the fast-lane half of the witnesses above, with no
// driver: enforce over recorded outcomes, per case and per surface.
func TestEnforceTicked(t *testing.T) {
	want := []expectation{{name: "alpha"}}
	ticked := tickedClaims(tickSet{"variable-tables/fixed-class": {"zz": true}})

	r := &result{}
	r.mark("alpha", outcomeAbsent)
	ticked.enforce("zz", "wire", want, r)
	if len(r.failures) != 1 || !strings.Contains(r.failures[0], "TICKED and answered ABSENT") || r.outcome["alpha"] != outcomeClaimedAbsent {
		t.Errorf("a ticked case answered ABSENT was not a failure: %+v", r)
	}

	r = &result{absent: true}
	ticked.enforce("zz", "wire", want, r)
	if len(r.failures) != 1 || !strings.Contains(r.failures[0], "the surface answered ABSENT") {
		t.Errorf("a ticked surface answered ABSENT was not a failure: %+v", r)
	}

	for _, lang := range []string{"yy", referenceLang} {
		r = &result{absent: true}
		ticked.enforce(lang, "wire", want, r)
		r2 := &result{}
		r2.mark("alpha", outcomeAbsent)
		ticked.enforce(lang, "wire", want, r2)
		if len(r.failures)+len(r2.failures) != 0 {
			t.Errorf("%s: enforce failed a leg the row is not ticked for (the reference is held by run.go): %v %v", lang, r.failures, r2.failures)
		}
	}

	r = &result{}
	r.mark("alpha", outcomePass)
	ticked.enforce("zz", "wire", want, r)
	if len(r.failures) != 0 {
		t.Errorf("an answered ticked case was failed: %v", r.failures)
	}
}
