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
