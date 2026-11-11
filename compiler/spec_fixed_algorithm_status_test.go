// THE PAGE STATES THE WIRE, NOT WHICH LEGS HAVE PROVED IT
// (docs/FIXED-FORM-ALGORITHM.md preamble, §8, "Reference fixes").
//
// The rule: this page states the wire every leg implements, so it names no
// count of languages. Per-leg status is ROADMAP.md's and docs/roadmap.sexp's,
// and §7's proofs are what a leg ticks there. Each reference fix names where
// its rule is stated, not where it stands.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedAlgorithmStatusCurrent(t *testing.T) {
	t.Parallel()
	const page = "../docs/FIXED-FORM-ALGORITHM.md"
	fixes := specSection(t, page, "## Reference fixes", "Three smaller divergences")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:    "preamble: no count of languages",
			text:   specSection(t, page, "# The fixed form as an algorithm", "## 0. The notation"),
			wants:  []string{"this page states the same wire as steps"},
			stales: []string{"nine languages", "Nine languages"},
		},
		{
			row:  "§8: per-leg status belongs to the roadmap",
			text: specSection(t, page, "## 8. Coverage", "## Reference fixes"),
			wants: []string{
				"ROADMAP.md",
				"docs/roadmap.sexp",
				"§7",
				"C++ is the reference writer of the dump corpus, not an exemption from the proof",
			},
			stales: []string{
				"Nine languages, one wire",
				"Coverage matrix stub",
				"| construct | fixture | cpp |",
				"A blank cell is not a skip",
			},
		},
		{
			row:  "Reference fixes: where the rule is stated, not where it stands",
			text: fixes,
			wants: []string{
				"where the rule is stated",
				"§3.1",
				"§4.5",
				"§4.6",
				"docs/SPEC-TABLES.md §15",
			},
			stales: []string{
				"Reference fixes pending",
				"where it stands",
				"pending",
				"in flight",
			},
		},
		{
			row:    "fix 10: the bytes(N) row is stated in §4.1 and §5.2",
			text:   specSection(t, page, "**The `bytes(N)` row swap**", "| 11 |"),
			wants:  []string{"§4.1", "§5.2"},
			stales: []string{"§4.5"},
		},
	}

	// the page wraps at a fixed width, so a sentence is matched with its line
	// breaks folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, r := range rows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			text := flat(r.text)
			for _, want := range r.wants {
				if !strings.Contains(text, want) {
					t.Errorf("%s: the section no longer states %q", r.row, want)
				}
			}
			for _, stale := range r.stales {
				if strings.Contains(text, flat(stale)) {
					t.Errorf("%s: the section still states the stale passage %q", r.row, stale)
				}
			}
		})
	}
}
