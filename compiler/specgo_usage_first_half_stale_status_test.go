// TestSpecGoUsageFirstHalfStaleStatus pins the first-half USAGE.md rows of the
// 2026-10-18 spec-vs-Go review: each row's Go spelling or name is on the page,
// and the stale sentence it replaced is gone. One literal per row.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoUsageFirstHalfStaleStatus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	rows := []struct {
		name  string
		holds string
		stale string
	}{
		{"docs and tags reach every item", "both into its descriptors", "The front end does not read a `///` block"},
		{"tag refusal diagnostic", "attribute min requires a value, spelled min = value", "min takes a value"},
		{"doc above an if branch is refused", "above an `if` branch", ""},
		{"payload-free table arms", "carried by C++, C", "Payload-free arms in a table closure are C++ only"},
		{"wstring table kind 33", "All nine carry table kind `33`", "C++ also carries table kind `33`"},
		{"value defaults on the table wire", "C++ and Go carry all three on", "C and Go carry them on the packet wire"},
		{"pointer surface on the wire", "Go has `RootBuilder` and `LoadBuilder`", "the pointer surface ON THE WIRE is a follow-on in those six"},
		{"form byte carriers", "the tool and Go; the seven other backends", "the eight other backends still write its previous"},
		{"message form 2", "Go already carries", "The other eight targets carry"},
		{"table-armed unions", "C++, Go and the tool carry the form", "The C++ reference and the tool carry the"},
		{"arm refusals", "an array of tables or a `timestamp`", ""},
		{"maps", "carried by the C++ reference, the tool and Go", "carried by the C++ reference and the tool"},
		{"one-sided ranges are Go-first", "Go carries the one-sided range first", ""},
		{"required is Go-first", "Go carries `| required` first", ""},
		{"Go fixed refusal names", "TableFixedReasonCountOverBound", "TableFixedReasonLayoutNewer"},
		{"every surface implies wire", "Naming any surface implies `wire`", ""},
		{"unknown surface refused by name", "An unknown surface is refused by name", ""},
		{"strict LoadMessages keeps a prefix", "RootLoadMessages` still returns", ""},
		{"strict locked FromJson", "locked builder's `FromJson` answers false with an empty report", ""},
		{"compressed float negative zero", "compressed float's `-0` default becomes `+0`", ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if !strings.Contains(page, row.holds) {
				t.Errorf("USAGE.md does not hold the Go spelling %q", row.holds)
			}
			if row.stale != "" && strings.Contains(page, row.stale) {
				t.Errorf("USAGE.md still holds the stale sentence %q", row.stale)
			}
		})
	}
}
