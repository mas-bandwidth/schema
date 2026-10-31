// THE CLAIM CHECK AND ITS REVERSED WITNESS (test/conformance/README.md, "The
// nightly"). A case a leg CLAIMS — its unit uses a construct the leg's target
// registers as carried — and answers ABSENT turns the run red; the same
// absence from a leg that does not claim the case stays green and is counted
// as not carried. These drive `run` with fake drivers over the one-instance
// corpus absence_test.go builds: no language leg, no network, no clock.
package main

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeClaims is a claim over the fake corpus's one instance: its unit uses
// uses, and each leg in carried carries what it names.
func fakeClaims(uses []string, carried map[string][]string) *claimSet {
	info := caseInfo{uses: uses, class: "fixed"}
	set := &claimSet{
		cases: map[string]map[string]caseInfo{
			"wire":       {"alpha": info},
			"json-read":  {"alpha": info},
			"json-write": {"alpha.json": info},
		},
		carried: map[string]map[string]bool{},
	}
	for lang, constructs := range carried {
		set.carried[lang] = map[string]bool{}
		for _, c := range constructs {
			set.carried[lang][c] = true
		}
	}
	return set
}

// runClaimed runs the fake corpus with the claim check on and hands back the
// matrix, the verdict and the coverage rows.
func (c fakeCorpus) runClaimed(t *testing.T, drivers string, claims *claimSet) (string, bool, []coverageRow) {
	t.Helper()
	var out bytes.Buffer
	var rows []coverageRow
	ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work"), "", "", runOptions{claims: claims, coverage: &rows})
	if err != nil {
		t.Fatalf("harness run: %v\n%s", err, out.String())
	}
	return out.String(), ok, rows
}

func findRow(rows []coverageRow, lang, surface string) (coverageRow, bool) {
	for _, r := range rows {
		if r.Lang == lang && r.Surface == surface {
			return r, true
		}
	}
	return coverageRow{}, false
}

// TestClaimedAbsentFails is the REVERSED WITNESS: a planted claimed-but-absent
// case must turn the check red, by name, and count as claimed in the coverage.
func TestClaimedAbsentFails(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {caseGone: true}})
	out, ok, rows := c.runClaimed(t, drivers, fakeClaims([]string{"timestamp"}, map[string][]string{"zz": {"timestamp"}}))
	if ok {
		t.Fatalf("a CLAIMED case answered ABSENT left the run green:\n%s", out)
	}
	mustSay(t, out, []string{"zz / wire: alpha: CLAIMED and answered ABSENT", "uses timestamp", "compiler/carries_zz.go"}, []string{footer})
	r, found := findRow(rows, "zz", "wire")
	if !found || r.AbsentClaimed != 1 || r.NotCarried != 0 || r.Cases != 1 || r.Class != "fixed" {
		t.Errorf("the coverage row does not count the claimed absence: %+v (found %v)", r, found)
	}
}

// TestUnclaimedAbsentIsCoverage is the other half: the same absence from a leg
// whose target does not carry the construct is not a failure, and the
// coverage counts it as not carried.
func TestUnclaimedAbsentIsCoverage(t *testing.T) {
	needShell(t)
	for _, tc := range []struct {
		name    string
		uses    []string
		carried []string
	}{
		{"the leg does not carry the construct", []string{"timestamp"}, nil},
		{"the leg carries one of two", []string{"timestamp", "required"}, []string{"timestamp"}},
		{"the unit uses no registered construct", nil, []string{"timestamp", "required"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeCorpus(t)
			drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {caseGone: true}})
			out, ok, rows := c.runClaimed(t, drivers, fakeClaims(tc.uses, map[string][]string{"zz": tc.carried}))
			if !ok {
				t.Fatalf("an UNCLAIMED absence turned the run red:\n%s", out)
			}
			mustSay(t, out, []string{footer, "pass 0/0 +1a"}, []string{"CLAIMED"})
			r, found := findRow(rows, "zz", "wire")
			if !found || r.NotCarried != 1 || r.AbsentClaimed != 0 {
				t.Errorf("the coverage row does not count the absence as not carried: %+v", r)
			}
		})
	}
}

// TestClaimSkipsAbsentSurface: a surface the leg does not run is a missing
// surface, named in the coverage, and not a claim about one case.
func TestClaimSkipsAbsentSurface(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {unlisted: "json-write"}})
	out, ok, rows := c.runClaimed(t, drivers, fakeClaims([]string{"timestamp"}, map[string][]string{"zz": {"timestamp"}}))
	if !ok {
		t.Fatalf("a whole absent surface was scored as a claimed case:\n%s", out)
	}
	if r, found := findRow(rows, "zz", "json-write"); !found || !r.SurfaceAbsent || r.Failed != 0 {
		t.Errorf("the coverage does not name the absent surface: %+v", r)
	}
	if r, _ := findRow(rows, "zz", "wire"); r.Answered != 1 {
		t.Errorf("the answered surface is not counted: %+v", r)
	}
}

// TestBuildClaimsRefusesAnUnregisteredLeg: a leg whose target registers
// nothing in this build is an error naming the tag, never a leg that quietly
// claims nothing.
func TestBuildClaimsRefusesAnUnregisteredLeg(t *testing.T) {
	m := &Manifest{}
	if _, err := buildClaims(m, []string{"zz-unregistered"}); err == nil || !strings.Contains(err.Error(), "-tags schema_leg_zz-unregistered") {
		t.Errorf("buildClaims accepted a leg with no carries registration: %v", err)
	}
}

// TestCaseIndexCoversTheCorpus holds the one piece of mapping data — which
// unit each case reads — to the committed manifest: every case of every
// surface that names a unit has an entry, every unit loads, and the cases
// whose unit uses a registered construct are claimed by exactly the targets
// that carry it (compiler/carries_<target>.go).
func TestCaseIndexCoversTheCorpus(t *testing.T) {
	t.Chdir("../../..")
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := readReports(defaultReports)
	if err != nil {
		t.Fatal(err)
	}
	set, err := buildClaims(m, []string{"go", "cpp", "squirrel"})
	if err != nil {
		t.Fatal(err)
	}
	unitless := []string{"forgery", "cook-forgery", "cook-reason", "block-reason"}
	claimedSomewhere := 0
	for _, s := range surfaces {
		want, err := expectations(m, s, reports, defaultJSONDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range want {
			info, ok := set.cases[s][e.name]
			if slices.Contains(unitless, s) {
				if ok {
					t.Errorf("%s / %s: a fixture surface has a unit entry", s, e.name)
				}
				continue
			}
			if !ok {
				t.Errorf("%s / %s: no unit for the case — caseIndex does not read this surface's lines", s, e.name)
				continue
			}
			if len(info.uses) == 0 {
				continue
			}
			claimedSomewhere++
			// Go carries every construct, so it claims every case that uses one
			if claimed, _ := set.claims("go", s, e.name); !claimed {
				t.Errorf("%s / %s uses %v and Go does not claim it", s, e.name, info.uses)
			}
			// Squirrel registers none, so it claims none
			if claimed, _ := set.claims("squirrel", s, e.name); claimed {
				t.Errorf("%s / %s: Squirrel registers no construct and claims it", s, e.name)
			}
		}
	}
	if claimedSomewhere == 0 {
		t.Error("no case in the corpus uses a registered construct; the claim check would enforce nothing")
	}
	// the corpus's optional-over-a-variable-closure rows (docs/SPEC-TABLES.md
	// §2.3), which the C++ reference carries
	if claimed, uses := set.claims("cpp", "wire", "guard_true_both_edges"); !claimed || !slices.Contains(uses, "optional-variable-closure") {
		t.Errorf("cpp does not claim guard_true_both_edges by optional-variable-closure: %v %v", claimed, uses)
	}
	if got := set.cases["wire"]["graph_tree"].class; got != "variable" {
		t.Errorf("graph_tree is class %q, want variable", got)
	}
	if got := set.cases["wire"]["root_full"].class; got != "fixed" {
		t.Errorf("root_full is class %q, want fixed", got)
	}
}

// TestCoverageMarkdown holds the table's cells to the legend it prints.
func TestCoverageMarkdown(t *testing.T) {
	rows := []coverageRow{
		{Lang: "aa", Direction: directionReadsCpp, Surface: "wire", Class: "fixed", Cases: 16, Answered: 12, NotCarried: 4},
		{Lang: "bb", Direction: directionReadsCpp, Surface: "wire", Class: "fixed", Cases: 16, Answered: 13, Failed: 1, AbsentClaimed: 2},
		{Lang: "bb", Direction: directionReadsCpp, Surface: "block", SurfaceAbsent: true},
		{Lang: "aa", Direction: directionCppReads, Surface: "wire", Class: "fixed", Cases: 12, Answered: 12},
	}
	var b bytes.Buffer
	coverageMarkdown(&b, rows, map[string]string{"aa": "pass", "bb": "none"})
	mustSay(t, b.String(), []string{
		"| surface | aa | bb |",
		"| wire (fixed) | 12/16 +4nc | 13/16 x1 !2 |",
		"| block | — | absent |",
		"## C++ reads what each port writes",
		"| wire (fixed) | 12/12 |",
		"| bb | none |",
	}, nil)
}

// TestReadCoverageReadsOnlyItsOwnFiles: the coverage rows ride the same
// artifacts as a leg's answers, and a json-write answer is a .json file that
// is not a row.
func TestReadCoverageReadsOnlyItsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "port-zz-writes", "nightly-report", "coverage-zz.json"),
		`[{"lang":"zz","direction":"reads-cpp","surface":"wire","cases":1,"answered":1}]`)
	writeFile(t, filepath.Join(dir, "crossread-report", "crossread-zz.json"),
		`[{"lang":"zz","direction":"cpp-reads","surface":"wire","cases":1,"answered":1}]`)
	writeFile(t, filepath.Join(dir, "port-zz-writes", "nightly-report", "fixedform-zz.txt"), "pass\n")
	writeFile(t, filepath.Join(dir, "port-zz-writes", "json-write", "alpha.json"), `{"alpha":1}`)
	rows, fixed, err := readCoverage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || fixed["zz"] != "pass" {
		t.Errorf("readCoverage = %+v, %v", rows, fixed)
	}
}
