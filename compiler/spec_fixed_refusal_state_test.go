// A REFUSED FIXED LOAD LEAVES THE CALLER'S VALUES AS THEY WERE
// (docs/FIXED-FORM-ALGORITHM.md §5.3 and §7 item 3, docs/SPEC-TABLES.md §6.5).
//
// Every refusal of a fixed load, out_of_range included, leaves the caller's
// values exactly as they were before the load. The bounds checks run in the
// check pass, before any record lands, and step 12 lands values already known
// good. The page states the same end state in three places, and this pin holds
// each of them to the strict read: §5.3's LOAD, §7 item 3's negative controls,
// and §6.5's FixedLoad clause.
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
				"Every refusal, `out_of_range` included, writes not one destination byte",
				"the bounds checks run in the check pass, before any record lands",
				"step 12 lands values already known good and refuses nothing",
				"never reset to its declared defaults",
				"which is the state SPEC-TABLES §6.5 states for `FixedLoad`",
				"A TAG is checked in the check pass, before any record lands",
				"Only then does it land the records, values already known good",
			},
			stales: []string{
				"resetting every value written",
				"A refusal the bounds pass meets after a record has landed",
				"A TAG is checked in the bounds pass, not in the check loop",
				"Only then does it land the records and run the bounds pass",
			},
		},
		{
			row:   "§6.5: FixedLoad leaves every value as it was",
			text:  fixedLoad,
			wants: []string{"`FixedLoad` leaves every value as it was"},
			stales: []string{
				"`FixedLoad` resets every value it had written",
			},
		},
		{
			row:  "§7 item 3: a refusal leaves the destination as it was",
			text: proofs,
			wants: []string{
				"nothing decoded, no counter moved, and every byte of the caller's destination as it was before the load",
				"never clamped, never zero-filled, never prefilled",
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
