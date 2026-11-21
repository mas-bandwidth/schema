package main

import "testing"

// TestOptInInstanceIsNotAskedOnJsonWrite holds the opt-in rule on the
// json-write surface: an instance on a unit the leg has not opted into is
// dropped from the leg's manifest, so its json-write case (named after the
// file, <instance>.json) counts absent like its wire and json-read cases, and
// a case on a unit the leg does answer is still asked.
func TestOptInInstanceIsNotAskedOnJsonWrite(t *testing.T) {
	t.Parallel()
	text := "unit demo tables/demo\n" +
		"unit extra tables/extra\n" +
		"instance kept demo Root a.bin\n" +
		"instance left extra Root b.bin\n"
	_, notAsked := optInLines(text, map[string]bool{"extra": true})
	for _, c := range []struct {
		surface, name string
		want          bool
	}{
		{"wire", "left", true},
		{"json-read", "left", true},
		{"json-write", "left.json", true},
		{"json-write", "kept.json", false},
		{"wire", "kept", false},
		// only json-write names a case after its file
		{"wire", "left.json", false},
	} {
		if got := caseNotAsked(notAsked, c.surface, c.name); got != c.want {
			t.Errorf("caseNotAsked(%s, %s) = %v, want %v", c.surface, c.name, got, c.want)
		}
	}
}

// TestOptInJsonWriteRemovesExactlyOneSuffix holds the lookup to the one
// suffix the surface adds. Two rows, `score` on a unit the leg answers and
// `score.json` on one it does not: the json-write case `score.json` is the
// answered row's and is asked, and `score.json.json` is the omitted row's and
// is not. On every other surface a case carries its row's own name.
func TestOptInJsonWriteRemovesExactlyOneSuffix(t *testing.T) {
	t.Parallel()
	text := "unit demo tables/demo\n" +
		"unit extra tables/extra\n" +
		"instance score demo Root a.bin\n" +
		"instance score.json extra Root b.bin\n"
	_, notAsked := optInLines(text, map[string]bool{"extra": true})
	for _, c := range []struct {
		surface, name string
		want          bool
	}{
		{"json-write", "score.json", false},
		{"json-write", "score.json.json", true},
		{"wire", "score", false},
		{"wire", "score.json", true},
		{"json-read", "score.json", true},
		// a json-write case always carries the suffix; a bare name is no case of an omitted row
		{"json-write", "score", false},
	} {
		if got := caseNotAsked(notAsked, c.surface, c.name); got != c.want {
			t.Errorf("caseNotAsked(%s, %s) = %v, want %v", c.surface, c.name, got, c.want)
		}
	}
}
