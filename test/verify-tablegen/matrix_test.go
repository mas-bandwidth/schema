package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMatrixTargetSpellings is F-193: `--lang 'lobster, js'` ran js under the
// trimmed name and printed it under the untrimmed one, so a failing js read as
// "pass 1 fail 0". Every spelling of the same two targets must key the run and
// the printout alike, and print the committed totals in
// testdata/matrix-spellings.golden: both targets fail the one case.
func TestMatrixTargetSpellings(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "matrix-spellings.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"lobster,js", "lobster, js", " lobster ,js ", "lobster,,js,"} {
		targets := parseTargets(spelling)
		if !slices.Equal(targets, []string{"lobster", "js"}) {
			t.Errorf("--lang %q parsed as %q, want [lobster js]", spelling, targets)
		}
		// the run keys each target's failures by the name it was handed
		m := newMatrix()
		for _, lang := range targets {
			m.cases[lang] = []CorpusCase{{ID: "pin:ci32:fresh:msg"}}
			m.drop(lang, "pin:ci32:fresh:msg", "byte mismatch got=11 want=3")
		}
		var out bytes.Buffer
		writeMatrix(&out, m, targets, 1)
		if !bytes.Equal(out.Bytes(), want) {
			t.Errorf("--lang %q printed\n%s\nwant\n%s", spelling, out.Bytes(), want)
		}
	}
}

func TestParseTargetsAll(t *testing.T) {
	if got := parseTargets("all"); !slices.Equal(got, allLangs) {
		t.Errorf("all = %q", got)
	}
	if got := parseTargets(" , "); len(got) != 0 {
		t.Errorf("an empty list parsed as %q", got)
	}
}
