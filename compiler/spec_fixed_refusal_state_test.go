// A REFUSED FIXED LOAD LEAVES EVERY ROW AT ITS DECLARED DEFAULTS
// (docs/FIXED-FORM-ALGORITHM.md §5.3 and §7 item 3, docs/SPEC-TABLES.md §6.5;
// ruling R4, 2026-11-22).
//
// Every refusal of a fixed load, out_of_range included, lands no byte read
// from the file and leaves every row the call was handed at its declared
// defaults: no partly landed row survives. The bounds checks run in the check
// pass, before any record lands, and step 12 lands values already known good.
// The page states the same end state in three places, and this pin holds each
// of them: §5.3's LOAD, §7 item 3's negative controls, and §6.5's rule for
// every read.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedRefusalLeavesValues(t *testing.T) {
	t.Parallel()
	const algorithm = "../docs/FIXED-FORM-ALGORITHM.md"
	const tables = "../docs/SPEC-TABLES.md"
	load := specSection(t, algorithm, "### 5.3 ", "### 5.4 ")
	proofs := specSection(t, algorithm, "## 7. What a port takes", "## 8. Coverage")
	fixedLoad := specSection(t, tables, "### 6.5 ", "### 6.6 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§5.3 LOAD: the bounds checks run in the check pass",
			text: load,
			wants: []string{
				"`out_of_range` included, lands not one byte read from the file, and then sets every row handed in to its declared defaults",
				"the bounds checks run in the check pass, before any record lands",
				"step 12 lands values already known good and refuses nothing",
				"every row the call was handed holds its declared defaults",
				"which is the state SPEC-TABLES §6.5 states for every read",
				"A TAG is checked in the check pass, before any record lands",
				"Only then does it land the records, values already known good",
			},
			stales: []string{
				"resetting every value written",
				"A refusal the bounds pass meets after a record has landed",
				"A TAG is checked in the bounds pass, not in the check loop",
				"Only then does it land the records and run the bounds pass",
				"never reset to its declared defaults",
			},
		},
		{
			row:  "§6.5: every row handed in holds its declared defaults",
			text: fixedLoad,
			wants: []string{
				"THE CALLER'S VALUES AFTER A REFUSAL: every instance the call was handed holds its declared defaults",
				"a fixed batch read resets EVERY row it was handed",
			},
			stales: []string{
				"`FixedLoad` leaves every value as it was",
			},
		},
		{
			row:  "§7 item 3: a refusal leaves every row at its declared defaults",
			text: proofs,
			wants: []string{
				"nothing decoded, no counter moved, and every row the caller handed in at its declared defaults",
				"never clamped, never partly landed",
			},
			stales: []string{
				"every byte of the caller's destination as it was before the load",
			},
		},
	}

	// the pages wrap at a fixed width, so a sentence is matched with its line
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
