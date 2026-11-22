package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// THE CORPUS CAN FAIL EVERY LEG THAT REFUSES EVERYTHING: every must-refuse
// row in the committed manifest has a lawful twin (discriminate.go, rule 1).
func TestEveryMustRefuseRowHasALawfulTwin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	reports, err := readReports(filepath.Join(root, defaultReports))
	if err != nil {
		t.Fatal(err)
	}
	d, err := buildDiscrimination(m, reports)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.refusal) == 0 {
		t.Fatal("the manifest yielded no must-refuse rows: the proof is watching nothing")
	}
	if p := d.problems(); len(p) > 0 {
		t.Fatalf("%d must-refuse row(s) cannot fail:\n%s", len(p), strings.Join(p, "\n"))
	}
}

// THE CONTROL for rule 1: the armmin_fix_count shape seen 2026-11-22, two
// form-3 framing rows that must both refuse and no row at that root that must
// read. Every leg passes them by refusing the form; the proof names both.
func TestRefusalsWithoutATwinAreNamed(t *testing.T) {
	t.Parallel()
	m := &Manifest{Framings: []FramingCase{
		{Name: "armmin_fix_count_0", Unit: "tabledemo", Root: "MinArmFix", Refused: true},
		{Name: "armmin_fix_count_1", Unit: "tabledemo", Root: "MinArmFix", Refused: true},
		{Name: "frame_empty_table", Unit: "tblr1", Root: "Cfg"},
		{Name: "frame_form_zero", Unit: "tblr1", Root: "Cfg", Refused: true},
	}}
	d, err := buildDiscrimination(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := d.problems()
	if len(p) != 2 || !strings.Contains(p[0], "armmin_fix_count_0") || !strings.Contains(p[1], "armmin_fix_count_1") {
		t.Fatalf("problems = %q, want the two armmin rows and nothing else", p)
	}
}

// THE CONTROL for rule 2: a leg that refuses a whole root (every framing row
// answered `refused`) passes the must-refuse row on the old scoring and fails
// the lawful twin. The refusal is moved to refusal-only, out of the pass count,
// and a leg that reads the twin keeps its credit. For the reference leg the
// same row is a failure.
func TestARefusalIsCreditedOnlyBesideAnAnsweredTwin(t *testing.T) {
	t.Parallel()
	m := &Manifest{Framings: []FramingCase{
		{Name: "frame_read", Unit: "u", Root: "R"},
		{Name: "frame_bad", Unit: "u", Root: "R", Refused: true},
	}}
	d, err := buildDiscrimination(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	score := func(twin string) *result {
		r := &result{total: 2, pass: 1}
		r.verdict("frame_bad", "pass")
		r.verdict("frame_read", twin)
		if twin == "pass" {
			r.pass++
		}
		return r
	}

	refusesAll := score("fail")
	d.credit("lobster", map[string]*result{"framing": refusesAll}, false)
	if refusesAll.pass != 0 || refusesAll.cases["frame_bad"] != outcomeUncredited || len(refusesAll.uncredited) != 1 {
		t.Fatalf("a leg refusing the form kept its refusal credit: %+v", refusesAll)
	}
	if len(refusesAll.failures) != 0 {
		t.Fatalf("a port's refusal-only row is work left, not a failure: %+v", refusesAll.failures)
	}

	absentTwin := score("absent")
	d.credit("go", map[string]*result{"framing": absentTwin}, false)
	if absentTwin.pass != 0 || len(absentTwin.uncredited) != 1 {
		t.Fatalf("an ABSENT twin credited the refusal: %+v", absentTwin)
	}

	reads := score("pass")
	d.credit("c", map[string]*result{"framing": reads}, false)
	if reads.pass != 2 || len(reads.uncredited) != 0 {
		t.Fatalf("a leg that reads the twin lost its credit: %+v", reads)
	}

	reference := score("fail")
	d.credit("cpp", map[string]*result{"framing": reference}, true)
	if len(reference.failures) != 1 || !strings.Contains(reference.failures[0], "REFUSAL-ONLY") {
		t.Fatalf("the reference leg's refusal-only row is not a failure: %+v", reference.failures)
	}

	var out strings.Builder
	report(&out, []string{"lobster"}, map[string]map[string]*result{"lobster": {"framing": refusesAll}})
	text := out.String()
	for _, want := range []string{"pass 0/1 +1r", "REFUSAL-ONLY", "lobster / framing: 1 row(s)", "credited 0 of 2; absent 0; refusal-only 1; failed 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("the matrix does not say %q:\n%s", want, text)
		}
	}
}

// ABSENT IS NEVER CREDITED: the score line counts it apart from credited.
func TestScoreCountsAbsentApart(t *testing.T) {
	t.Parallel()
	r := &result{total: 5, pass: 3, missing: 2}
	var out strings.Builder
	report(&out, []string{"go"}, map[string]map[string]*result{"go": {"wire": r}})
	if !strings.Contains(out.String(), "credited 3 of 5; absent 2; refusal-only 0; failed 0") {
		t.Fatalf("score line:\n%s", out.String())
	}
	// a whole surface absent is absent cases, never credited ones
	r = &result{total: 4, absent: true}
	out.Reset()
	report(&out, []string{"go"}, map[string]map[string]*result{"go": {"wire": r}})
	if !strings.Contains(out.String(), "credited 0 of 4; absent 4") {
		t.Fatalf("score line:\n%s", out.String())
	}
}
