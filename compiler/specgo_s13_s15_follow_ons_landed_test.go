// The §13.6 and §15 rows whose Go implementation has landed: what the standard
// says now, held to the spelling this card wrote. Each case names what the
// section must hold and the stale sentence the row replaced; the page is read
// once, with runs of whitespace collapsed so a wrapped line cannot fail a row.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS13S15FollowOnsLanded(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatalf("read the page: %v", err)
	}
	page := strings.Join(strings.Fields(string(raw)), " ")

	cases := []struct {
		row    string
		holds  []string
		absent []string
	}{
		{
			row:    "§15 the variable class on the wire",
			holds:  []string{"Go carries the variable class on the wire"},
			absent: []string{"what the other seven are waiting for is the variable class"},
		},
		{
			row:    "§15 the variable class's text form",
			holds:  []string{"Go carries the graph text form", "pointers, labels and blobs"},
			absent: []string{"with its wire: the pointer adapters"},
		},
		{
			row:    "§15 the cook's write side",
			holds:  []string{"C++, C and Go write cooks"},
			absent: []string{"C++ and C write cooks of either class today"},
		},
		{
			row:    "§15 the unit registry",
			holds:  []string{"C++, C and Go emit the registry", "ViewConstant"},
			absent: []string{"the remaining seven ports do not"},
		},
		{
			row:    "§15 the view of a table-free unit",
			holds:  []string{"Go, like C++, emits nothing for a unit with no table"},
			absent: []string{"so a unit that declares no table gets none"},
		},
		{
			row:    "§15 maps",
			holds:  []string{"C++, C, C# and Go carry it", "map[K]V"},
			absent: []string{"every backend refuses a unit that declares one"},
		},
		{
			row:    "§15 unbounded arrays",
			holds:  []string{"registered list carriers"},
			absent: []string{"every port refuses a unit that declares one"},
		},
		{
			row:    "§15 optional array",
			holds:  []string{"carry `?[..N]T` and `?[N]T`"},
			absent: []string{"C++ and the tool carry `?[..N]T`"},
		},
		{
			row:    "§15 byte buffer",
			holds:  []string{"`*bytes` and `*string`", "blob_shared"},
			absent: []string{"C++ carries it and every other backend refuses the unit"},
		},
		{
			row:    "§15 array of unions",
			holds:  []string{"C++, C# and Go carry `[..N]Body` and `[N]Body`"},
			absent: []string{"C, C++ and the tool carry `[..N]Body`"},
		},
		{
			row:    "§15 table arms",
			holds:  []string{"Go carry table arms"},
			absent: []string{"C++ carries it and every other backend refuses the unit by name"},
		},
		{
			row:    "§15 general arms",
			holds:  []string{"C# and Go carry the scalar"},
			absent: []string{"C++ and the tool carry the scalar"},
		},
		{
			row:    "§15 wide kinds",
			holds:  []string{"the tool and Go", "carriers beside them"},
			absent: []string{"holds the eight refusals"},
		},
		{
			row:   "§15 an array of ?T",
			holds: []string{"`[..N]?string(M)` and `[]?string(M)` are the exception and are accepted"},
		},
		{
			row:    "§15 ?string and ?bytes",
			holds:  []string{"?wstring(N)"},
			absent: []string{"`?string(N)` and `?bytes(N)`**"},
		},
		{
			row:   "§15 a map as a map's value",
			holds: []string{"map[K]?map[K2]V"},
		},
		{
			row:   "§15 the timestamp follow-ons",
			holds: []string{"?timestamp", "timestamp MAP VALUE", "timestamp union ARM"},
		},
		{
			row:    "§15 the baseline follow-on",
			holds:  []string{"`size=` vanishing", "`keybound=` moving to `0`", "DONE"},
			absent: []string{"Also owed: internal/baseline"},
		},
		{
			row:   "§15 several layouts per stream",
			holds: []string{"several layouts per stream"},
		},
		{
			row:    "§13.6 and §14 #23 the bare stride tag",
			holds:  []string{"bare `| stride`", "only the valued `stride = N` is refused"},
			absent: []string{"an unknown attribute is refused as unknown"},
		},
	}

	for _, c := range cases {
		for _, want := range c.holds {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the page no longer holds %q", c.row, want)
			}
		}
		for _, stale := range c.absent {
			if strings.Contains(page, stale) {
				t.Errorf("%s: the page still holds the stale sentence %q", c.row, stale)
			}
		}
	}
}
