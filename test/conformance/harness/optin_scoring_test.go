package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOptInCorpusScoresEveryOmittedCaseAbsent holds the scorer's lookup to the
// expectation names on EVERY surface, over the committed opt-in corpus
// (testdata/conformance/optin/MANIFEST.txt). run.go scores a case absent when
// notAsked.has(surface, expectation name), so each surface's expectations must
// be named as caseOwners names them: json-write after the file
// (`plain_text.json`), every other surface after the row. A case on an omitted
// unit whose expectation is named another way would be scored as "the driver
// wrote nothing" (11 json-write shapes_* rows on a leg without shapes); a
// case on a required unit must still be asked.
func TestOptInCorpusScoresEveryOmittedCaseAbsent(t *testing.T) {
	root := conformanceRoot(t)
	t.Chdir(root)
	// each text instance's JSON value, which json-write's expectation reads
	jsonDir := t.TempDir()
	m, err := ReadManifest(filepath.Join("testdata", "conformance", "optin", "MANIFEST.txt"), jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range m.Instances {
		if i.JSON != "" {
			if err := os.WriteFile(i.JSON, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	skip := map[string]bool{}
	for key := range m.OptIn {
		skip[key] = true
	}
	owners, err := caseOwners(m)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := casesNotAsked(m, skip)
	if err != nil {
		t.Fatal(err)
	}
	scored := map[caseKey]bool{}
	for _, s := range surfaces {
		want, err := expectations(m, s, map[string]string{}, jsonDir)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		for _, e := range want {
			key := caseKey{s, e.name}
			unit, ok := owners[key]
			if !ok {
				t.Errorf("%s / %s: an expectation no row owns under that name; the scorer's lookup misses it", s, e.name)
				continue
			}
			if got := cases.has(s, e.name); got != skip[unit] {
				t.Errorf("%s / %s on unit %s: scored absent = %v, want %v", s, e.name, unit, got, skip[unit])
			}
			scored[key] = true
		}
	}
	// the omitted plain instance on its three surfaces, and the control
	for _, key := range []caseKey{{"wire", "plain_text"}, {"json-read", "plain_text"}, {"json-write", "plain_text.json"}} {
		if !scored[key] || !cases.has(key.surface, key.name) {
			t.Errorf("%s / %s: not an expectation the scorer marks absent", key.surface, key.name)
		}
	}
	if !scored[caseKey{"json-write", "score.json"}] || cases.has("json-write", "score.json") {
		t.Error("the control json-write/score.json is not asked")
	}
	// and every case the leg is not asked is one some surface expects
	for key := range cases {
		if !scored[key] {
			t.Errorf("%s / %s: not asked, and no surface expects it", key.surface, key.name)
		}
	}
}
