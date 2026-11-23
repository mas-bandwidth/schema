package main

import (
	"strings"
	"testing"
)

// TestEmptySelectionIsAnError: a filter that selects nothing (--surfaces naming
// surfaces with no expectation, --only naming a leg that answers none of the
// selected surfaces) must not print a green matrix and returns an error. Pure: no driver runs.
func TestEmptySelectionIsAnError(t *testing.T) {
	cell := func(total int, absent bool) map[string]*result {
		return map[string]*result{"wire": {total: total, absent: absent}}
	}
	cases := []struct {
		name    string
		results map[string]map[string]*result
		say     string
	}{
		{"a selected cell with rows is fine", map[string]map[string]*result{"go": cell(3, false)}, ""},
		{"zero rows on the only selected surface", map[string]map[string]*result{"go": cell(0, false)}, "ZERO rows"},
		{"no cells at all", map[string]map[string]*result{"go": {}}, "ZERO rows"},
		{"every selected cell absent", map[string]map[string]*result{"go": cell(3, true)}, "ZERO legs"},
		{"one leg absent, one answering", map[string]map[string]*result{"go": cell(3, true), "cpp": cell(3, false)}, ""},
	}
	for _, c := range cases {
		err := emptySelection(c.results, "go", "", map[string]bool{"wire": true})
		switch {
		case c.say == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.say != "" && (err == nil || !strings.Contains(err.Error(), c.say)):
			t.Errorf("%s: err = %v, want one containing %q", c.name, err, c.say)
		}
	}
}

// TestRunWithErrorsOnAFilterThatSelectsNothing drives the whole run: a leg is
// asked for a surface the corpus holds no case for, and a leg is asked for
// surfaces it leaves out of its list. Both were green before.
func TestRunWithErrorsOnAFilterThatSelectsNothing(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	drivers := c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {unlisted: "wire"}})
	for _, tc := range []struct{ name, surfaces, only, say string }{
		{"zero rows", "cook", "", "ZERO rows"},
		{"zero legs", "wire", "zz", "ZERO legs"},
	} {
		only, err := parseSurfaces(tc.surfaces)
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers, t.TempDir()+"/work", tc.only, "", runOptions{only: only})
		if ok || err == nil || !strings.Contains(err.Error(), tc.say) {
			t.Errorf("%s: ok=%v err=%v, want an error containing %q\n%s", tc.name, ok, err, tc.say, out.String())
		}
	}
}
