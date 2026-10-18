package compiler

import (
	"os"
	"strings"
	"testing"
)

// The pages' §7 cook-writer rules, pinned to the Go implementation that is
// landed: a cook writer may exist in any port, Go is one, and the spelling
// and refusals below are Go's. Each row states one section's rule, and the
// stale sentence the row replaces is named beside it so the pin is red at the
// base this card starts from.
func TestSpecGoS7CookWritersAnyPort(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return string(body)
	}
	spec := read("../docs/SPEC-TABLES.md")
	usage := read("../docs/USAGE.md")

	rows := []struct {
		name  string
		page  string
		wants []string
		stale []string
	}{
		{
			name: "any port and Go's surface",
			page: spec,
			wants: []string{
				"A cook writer may exist in any port",
				"func SettingsCookMeasureReason(value *Settings) (int64, error)",
				"func SettingsCookFrom(value *Settings, buffer []byte, order TableByteOrder) bool",
			},
			stale: []string{"emitted by the C++ backend for every table"},
		},
		{
			name:  "usage status says any port",
			page:  usage,
			wants: []string{"A cook writer may exist in any port", "C, C++ and Go emit their cook writers", "Go's spellings are `<Root>CookMeasure`", "for every table that declares `cook`", "a declaration omitting `cook` gets no `Open`"},
			stale: []string{"the C++ WRITE side are built", "In every other language,"},
		},
		{
			name:  "typed-reason Open",
			page:  spec,
			wants: []string{"func (cook *XCook) Open(base, length) error"},
		},
		{
			name:  "XAt variadic arena",
			page:  spec,
			wants: []string{"arena ...*TableArena"},
			stale: []string{"`r := XAt(slot)`, `slot *int64`"},
		},
		{
			name:  "RegionAlignment is the unit ceiling",
			page:  spec,
			wants: []string{"the UNIT's ceiling"},
		},
		{
			name:  "Go writer refusals",
			page:  spec,
			wants: []string{"invalid_value", "data_cycle", "allocation_failed"},
		},
		{
			name:  "map cooks refuse by name and Go holds the extent",
			page:  spec,
			wants: []string{"map-bearing unit BY NAME", "TestCookWriterMapExtentLayout"},
		},
		{
			name:  "forms omitting cook gets no Open",
			page:  spec,
			wants: []string{"declaration omits `cook`"},
		},
		{
			name:  "Go layout contract is init panic",
			page:  spec,
			wants: []string{"Go's layout contract is an `init()` panic"},
		},
		{
			name:  "wstring blob marked ahead of implementation",
			page:  spec,
			wants: []string{"the checker refuses `*wstring`"},
		},
		{
			name:  "follow-on names the port rule and Go",
			page:  spec,
			wants: []string{"C, C++ and Go write\n  cooks of either", "Go's is the claimed"},
			stale: []string{"C++ and C write cooks of", "THE COOK's WRITE SIDE in every other language"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, want := range row.wants {
				if !strings.Contains(row.page, want) {
					t.Errorf("the page no longer states %q", want)
				}
			}
			for _, stale := range row.stale {
				if strings.Contains(row.page, stale) {
					t.Errorf("the page still holds the stale sentence %q", stale)
				}
			}
		})
	}
}
