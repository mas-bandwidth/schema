// TestFixedAlgorithmCurrent pins docs/FIXED-FORM-ALGORITHM.md to the current
// rules: `layout_newer` is retired (§5.3), wide text is removed and a text
// length is in bytes (§4.5), write-side bound checks follow each language's own
// idiom (docs/rulings.json write-refusal), and the writer's ranges ride per
// lineage entry in the plan (§4.6).
package compiler

import (
	"os"
	"testing"
)

func TestFixedAlgorithmCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/FIXED-FORM-ALGORITHM.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	rows := []struct {
		row    string
		wants  []string
		stales []string
	}{
		{
			row: "§5.3 the retired layout name",
			wants: []string{
				"`layout_unknown` alone is \"and nothing else\" (§5.9 #7)",
				"keeps **`18`** for the retired `layout_newer` and answers",
				"| `layout_unknown`, `layout_unsupported` and the floor",
				"named `layout_unknown` rather than compiled both ways",
				"what belongs to `layout_unknown` alone is the words **AND NOTHING ELSE**",
				"`layout_newer` answers no read",
			},
			stales: []string{
				"`layout_newer` alone is",
				"The C and C++ pair's values are",
				"`18` `layout_newer`",
				"| `layout_newer`, `layout_unsupported` and the floor",
				"named `layout_newer` rather than compiled both ways",
			},
		},
		{
			row: "§4.5 a text length is bytes",
			wants: []string{
				"**The length unit is BYTES, and the CONTENT RULES apply to the USED BYTES and nothing else**",
				"UTF-8 validity over `v` bytes, never over `N`",
				"`v > size` then `REFUSE length_over_bound`",
			},
			stales: []string{
				"`unit := (meta == wide) ? 2 : 1`",
				"`cap := size / unit`",
				"wide code units over `v`, never `2N`, an astral pair counting two",
			},
		},
		{
			row: "§3.1 write-side checks per leg",
			wants: []string{
				"`docs/rulings.json`, `write-refusal`",
				"a debug-only assert in C, C++, C#, Rust, Java and Dart",
				"a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
			},
			stales: []string{
				"Write-side bound checks are DEBUG ONLY, by rule, in every language that has a debug-only idiom.",
				"Go has no debug-only idiom, so the Go fixed writer REFUSES in every build",
			},
		},
		{
			row: "§4.6 the writer's ranges ride per lineage entry",
			wants: []string{
				"per lineage entry in the plan the generator lays down",
				"`TableFixedKnownRange` carries them per entry",
				"a backend's lineage input must carry them",
			},
			stales: []string{
				"The reference carries them per entry for ranges and still owes the rest",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.row, page, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.row, page, stale)
		}
	}
}
