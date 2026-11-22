package baseline_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// TestBaselinePassesEveryWidening holds R17 at the baseline (Glenn,
// 2026-11-22: "the newer reader can read old data, but never the other way").
// Every widening of docs/SPEC-TABLES.md §4 THE ONE WIDENING RULE passes, at a
// field, an array element, a union arm and a map key: the NEW reader reads
// every OLD value exactly and counts `widened`. An OLD reader given the NEW
// file refuses it `kind_mismatch` by name, which is lawful, so it is no
// reason to refuse the edit. What a NEW reader cannot read from OLD data still
// refuses, naming the field: every narrowing and every retype.
//
// RED WHEN PLANTED: the same widenings are run under the OLD refusal (the
// kind, elem and keykind rows put back on RuleFixed) and each must refuse
// there, so the pass asserted here is the widening rule and nothing else; put
// the old refusal back in DefaultTokenPolicy and this test goes red.
func TestBaselinePassesEveryWidening(t *testing.T) {
	const scalars = "package fixture\n\nunion U\n{\n    a  %[1]s\n    b  int8\n}\n\ntable T\n{\n    x  %[1]s\n    xs [..4]%[1]s\n    u  U\n}\n"
	// bits(N) has no array form in v1, so it runs at a field and an arm
	const bitsOnly = "package fixture\n\nunion U\n{\n    a  %[1]s\n    b  int8\n}\n\ntable T\n{\n    x  %[1]s\n    u  U\n}\n"
	const keys = "package fixture\n\ntable T\n{\n    m map[%s]int32\n}\n"
	planted := maps.Clone(baseline.DefaultTokenPolicy)
	for _, k := range []string{"kind", "elem", "keykind"} {
		planted[k] = baseline.RuleFixed
	}
	// where each site is named, and the noun its kind token carries
	scalarSites := []struct{ where, noun string }{
		{"T.x", "wire kind"},
		{"T.xs", "array element kind"},
		{"union U.a", "wire kind"},
	}
	bitsSites := []struct{ where, noun string }{scalarSites[0], scalarSites[2]}
	keySites := []struct{ where, noun string }{{"T.m", "map key kind"}}
	cases := []struct {
		before, after string
		widens        bool
		keyed         bool // also run as a map key
	}{
		// THE WIDENINGS: an integer kind to a wider one of the same
		// signedness, and f32 to f64
		{"int8", "int16", true, true},
		{"int16", "int32", true, true},
		{"int32", "int64", true, true},
		{"int8", "int64", true, true},
		{"uint8", "uint16", true, true},
		{"uint8", "uint32", true, true},
		{"uint32", "uint64", true, true},
		{"bits(8)", "bits(9)", true, false},
		{"float32", "float64", true, false},
		// THE NARROWINGS: the reverse of each
		{"int64", "int32", false, true},
		{"uint32", "uint8", false, true},
		{"bits(9)", "bits(8)", false, false},
		{"float64", "float32", false, false},
		// THE RETYPES: across the ladders, and between float and integer
		{"int32", "uint32", false, true},
		{"uint8", "int16", false, true},
		{"int32", "uint64", false, true},
		{"int32", "float64", false, false},
		{"float32", "int64", false, false},
	}
	run := func(t *testing.T, tmpl string, sites []struct{ where, noun string }, before, after string, widens bool) {
		t.Helper()
		was := committed(t, fmt.Sprintf(tmpl, before))
		now := baseline.Render(unit(t, fmt.Sprintf(tmpl, after)))
		got := baseline.Diff(was, now, baseline.DefaultTokenPolicy)
		if widens {
			if len(got) != 0 {
				t.Errorf("%s to %s is a widening and must pass in silence (R17), got:%s", before, after, summary(got))
			}
			old := baseline.Diff(was, now, planted)
			for _, s := range sites {
				if !find(old, baseline.Refuse, s.where, s.noun) {
					t.Errorf("CONTROL: under the planted old refusal %s to %s must refuse at %s, got:%s", before, after, s.where, summary(old))
				}
			}
			return
		}
		for _, s := range sites {
			if !find(got, baseline.Refuse, s.where, s.noun) {
				t.Errorf("%s to %s is not a widening and must refuse at %s naming its %s, got:%s", before, after, s.where, s.noun, summary(got))
			}
		}
	}
	for _, c := range cases {
		tmpl, sites := scalars, scalarSites
		if strings.HasPrefix(c.before, "bits") {
			tmpl, sites = bitsOnly, bitsSites
		}
		t.Run(c.before+" to "+c.after, func(t *testing.T) {
			run(t, tmpl, sites, c.before, c.after, c.widens)
		})
		if c.keyed {
			t.Run("map key "+c.before+" to "+c.after, func(t *testing.T) {
				run(t, keys, keySites, c.before, c.after, c.widens)
			})
		}
	}
}
