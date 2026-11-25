package main

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestMessageFuzzUnitsCutIsTheSlice: --units cuts the message roster to the
// roots of exactly the named units (a goose part builds only its slice's
// legs, make/verify-vm.mk _verify-goose-fuzz-message), and slices that
// partition the roster's units ask the whole roster between them, each root
// once. The slices are taken from the roster itself, alternate units in name
// order, so no count is written here. A unit the manifest does not name, and
// a cut that keeps no root, refuse the run.
func TestMessageFuzzUnitsCutIsTheSlice(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	u := newUnits(m)
	names := func(roots []*messageFuzzRoot) []string {
		var out []string
		for _, r := range roots {
			out = append(out, r.unit+"."+r.root)
		}
		sort.Strings(out)
		return out
	}
	whole, _, err := buildMessageFuzzRoots(u, messageFuzzOptions{})
	if err != nil {
		t.Fatalf("buildMessageFuzzRoots (whole): %v", err)
	}
	var units []string
	for _, r := range whole {
		units = append(units, r.unit)
	}
	sort.Strings(units)
	units = slices.Compact(units)
	if len(units) < 2 {
		t.Fatalf("the roster has %d units; the cut needs two to be tested", len(units))
	}
	slice := [2][]string{}
	for i, unit := range units {
		slice[i%2] = append(slice[i%2], unit)
	}
	var union []string
	for i, s := range slice {
		roots, _, err := buildMessageFuzzRoots(u, messageFuzzOptions{units: splitUnits(" " + strings.Join(s, " , ") + ",")})
		if err != nil {
			t.Fatalf("slice %d/2 (%s): %v", i+1, strings.Join(s, ","), err)
		}
		for _, r := range roots {
			if !slices.Contains(s, r.unit) {
				t.Errorf("slice %d/2 asks %s.%s, whose unit it did not name", i+1, r.unit, r.root)
			}
		}
		union = append(union, names(roots)...)
	}
	sort.Strings(union)
	if got, want := union, names(whole); !slices.Equal(got, want) {
		t.Errorf("the two slices ask %d roots %v; the roster is %d roots %v", len(got), got, len(want), want)
	}

	if _, _, err := buildMessageFuzzRoots(u, messageFuzzOptions{units: []string{"no-such-unit"}}); err == nil || !strings.Contains(err.Error(), "no-such-unit") {
		t.Errorf("--units no-such-unit: err %v, want a refusal naming the unit", err)
	}
	var rootless string
	for _, mu := range m.Units {
		if !slices.Contains(units, mu.Key) {
			rootless = mu.Key
			break
		}
	}
	if rootless == "" {
		t.Fatal("every manifest unit has a roster root; the empty cut is not tested")
	}
	if _, _, err := buildMessageFuzzRoots(u, messageFuzzOptions{units: []string{rootless}}); err == nil || !strings.Contains(err.Error(), "nothing would be fuzzed") {
		t.Errorf("--units %s (no roster root): err %v, want a refusal", rootless, err)
	}
	if got := splitUnits(""); got != nil {
		t.Errorf("splitUnits(\"\") = %v, want no cut", got)
	}
}
