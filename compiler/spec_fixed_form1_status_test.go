// A FIXED ROOT REFUSES FORM 1, AND THE FORM-1 PAIR BESIDE IT IS A STATUS LINE
// (docs/SPEC-TABLES.md "The three wires", §3.4, the reason table, §15).
//
// The rule is Glenn's, 2026-11-03: "If we expect a fixed table, but it is not,
// refuse." Every read at a declared fixed root that carries form `3` refuses a
// form-`1` file as `previous_form`, and every write there writes form `3`
// (docs/rulings.json fixed-root-refuses-form1). What the legs do today is
// status, and the page says so beside the rule: every leg still emits a
// form-`1` `<T>Save` for a declared fixed table and a `<T>Load` and
// `ReadStrict` that read form `1` there, and a declared fixed table past one
// of §3.4's bounds carries no form `3` at all.
//
// The stale sentences are the ones the ruling retired: §3.4's "THE READ SIDE
// STILL ACCEPTS BOTH", the reason table's "The STRICT read does not", the
// Go-only status line, and §15's "accepts BOTH, by the form byte".
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedRootRefusesForm1(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC-TABLES.md"
	preamble := specSection(t, page, "## The three wires", "## The performance ladder")
	selection := specSection(t, page, "#### SELECTION IS BY THE KEYWORD", "#### A VARIABLE TABLE IN THE UNIT CHANGES NOTHING FOR ITS FIXED TABLES")
	reasons := specSection(t, page, "## 6. The two lives of a table", "## 7. The cooked form")
	followOns := specSection(t, page, "## 15. Named follow-ons", "## 16. The text form: JSON in and out of one table")
	ruling := "If we expect a fixed table, but it is not, refuse."

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "The three wires: the rule, then the status line",
			text: preamble,
			wants: []string{
				ruling,
				"previous_form",
				"`ReadStrict`",
				"the retain load",
				"`schema pack`",
				"STATUS: EVERY LEG STILL CARRIES THE FORM-`1` PAIR AT A FIXED ROOT",
				"This is status, not the",
				"<T>Save",
				"<T>Load",
				"65536",
				"§3.4",
				"§15",
			},
			stales: []string{
				"GO STATUS",
				"SCHEDULED FOR REMOVAL once form `3` lands",
			},
		},
		{
			row:   "§3.4 SELECTION: the read side refuses",
			text:  selection,
			wants: []string{ruling, "THE READ SIDE REFUSES THE OTHER FORM, BY NAME", "previous_form"},
			stales: []string{
				"THE READ SIDE STILL ACCEPTS BOTH",
				"reads form `1` and form `3`, by the form byte",
			},
		},
		{
			row:    "§6 reason table: previous_form at every fixed-root read",
			text:   reasons,
			wants:  []string{"**Every read at a declared fixed root answers it**", ruling},
			stales: []string{"**The STRICT read does not**"},
		},
		{
			row:    "§15: the work owed, in layers",
			text:   followOns,
			wants:  []string{"THE PLAIN ENTRY POINTS AT A FIXED ROOT ARE STILL FORM `1`", "the tool's engine"},
			stales: []string{"accepts BOTH, by the form byte"},
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
					t.Errorf("%s: the section still states the retired sentence %q", r.row, stale)
				}
			}
		})
	}
}
