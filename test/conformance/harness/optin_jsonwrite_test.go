package main

import (
	"strings"
	"testing"
)

// notAskedFor is the cases a leg that omits the unit `extra` is not handed.
func notAskedFor(t *testing.T, m *Manifest) notAsked {
	t.Helper()
	cases, err := casesNotAsked(m, map[string]bool{"extra": true})
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

type askedCase struct {
	surface, name string
	notAsked      bool
}

func checkAsked(t *testing.T, cases notAsked, want []askedCase) {
	t.Helper()
	for _, c := range want {
		if got := cases.has(c.surface, c.name); got != c.notAsked {
			t.Errorf("not asked (%s, %s) = %v, want %v", c.surface, c.name, got, c.notAsked)
		}
	}
}

// TestOptInInstanceIsNotAskedOnJsonWrite holds the opt-in rule on the
// json-write surface: an instance on a unit the leg has not opted into is
// dropped from the leg's manifest, so its json-write case (named after the
// file, <instance>.json) counts absent like its wire and json-read cases, and
// a case on a unit the leg does answer is still asked.
func TestOptInInstanceIsNotAskedOnJsonWrite(t *testing.T) {
	t.Parallel()
	m := &Manifest{Instances: []Instance{
		{Name: "kept", Unit: "demo", Root: "Root", Wire: "a.bin"},
		{Name: "left", Unit: "extra", Root: "Root", Wire: "b.bin"},
	}}
	checkAsked(t, notAskedFor(t, m), []askedCase{
		{"wire", "left", true},
		{"json-read", "left", true},
		{"json-write", "left.json", true},
		{"json-write", "kept.json", false},
		{"wire", "kept", false},
		// only json-write names a case after its file
		{"wire", "left.json", false},
		{"json-write", "left", false},
	})
}

// TestOptInJsonWriteCaseIsItsOwnRowsOnly: two rows, `score` on a unit the leg
// answers and `score.json` on one it does not. The json-write case
// `score.json` is the answered row's and is asked; `score.json.json` is the
// omitted row's and is not. On every other surface a case carries its row's
// own name.
func TestOptInJsonWriteCaseIsItsOwnRowsOnly(t *testing.T) {
	t.Parallel()
	m := &Manifest{Instances: []Instance{
		{Name: "score", Unit: "demo", Root: "Root", Wire: "a.bin"},
		{Name: "score.json", Unit: "extra", Root: "Root", Wire: "b.bin"},
	}}
	checkAsked(t, notAskedFor(t, m), []askedCase{
		{"json-write", "score.json", false},
		{"json-write", "score.json.json", true},
		{"wire", "score", false},
		{"json-read", "score", false},
		{"wire", "score.json", true},
		{"json-read", "score.json", true},
	})
}

// TestOptInCaseIsKeyedByUnitSurfaceAndName: a required instance and a
// write-refusal row on an omitted opt-in unit share one name. They are two
// cases, on two surfaces, of two units: the leg is still asked for the
// instance's wire and text, and is not asked for the write-refusal row.
func TestOptInCaseIsKeyedByUnitSurfaceAndName(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Instances: []Instance{
			{Name: "shared", Unit: "demo", Root: "Root", Wire: "a.bin"},
		},
		WriteRefusals: []WriteRefusal{
			{Name: "shared", Unit: "extra", Root: "Other", Wire: "b.bin", Path: "n", Kind: "int", Value: "1", Verdict: "refused"},
			{Form: "json", Name: "shared", Unit: "extra", Root: "Other", Wire: "b.bin", Path: "n", Kind: "int", Value: "1", Verdict: "refused"},
		},
	}
	cases := notAskedFor(t, m)
	checkAsked(t, cases, []askedCase{
		{"wire", "shared", false},
		{"json-read", "shared", false},
		{"json-write", "shared.json", false},
		{"write-refusal", "shared", true},
		{"write-refusal-json", "shared", true},
		{"write-refusal-fixed", "shared", false},
	})
	if unit := cases[caseKey{"write-refusal", "shared"}]; unit != "extra" {
		t.Errorf("the write-refusal case is owned by %q, want extra", unit)
	}
}

// TestManifestRefusesOneCaseOfTwoUnits: one name on one surface from rows on
// two units has no single owner, so the manifest is refused (ReadManifest
// runs the same check on every manifest it reads); the same name on two
// surfaces is fine.
func TestManifestRefusesOneCaseOfTwoUnits(t *testing.T) {
	t.Parallel()
	// an instance `a` and a json-write-refusal `a` both answer json-write as a.json
	ambiguous := &Manifest{
		Instances:         []Instance{{Name: "a", Unit: "demo", Root: "Root", Wire: "a.bin"}},
		JSONWriteRefusals: []JSONWriteRefusal{{Name: "a", Unit: "extra", Root: "Other"}},
	}
	_, err := caseOwners(ambiguous)
	if err == nil {
		t.Fatal("caseOwners accepted one json-write case owned by two units")
	}
	for _, want := range []string{"ambiguous", `"a.json"`, "json-write", "demo", "extra"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %s", err, want)
		}
	}
	if _, err := casesNotAsked(ambiguous, map[string]bool{"extra": true}); err == nil {
		t.Error("casesNotAsked accepted the ambiguous manifest")
	}
	sameUnit := &Manifest{
		Instances:         []Instance{{Name: "a", Unit: "demo", Root: "Root", Wire: "a.bin", NoText: true}},
		JSONWriteRefusals: []JSONWriteRefusal{{Name: "a", Unit: "extra", Root: "Other"}},
	}
	if _, err := caseOwners(sameUnit); err != nil {
		t.Errorf("a no-text instance has no json-write case, so the name is free there: %v", err)
	}
}
