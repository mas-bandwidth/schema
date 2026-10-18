// The first half of USAGE.md states what Go carries today. Each case below is
// one row of the spec-vs-Go review: the page holds Go's spelling or the name
// Go answers, and no longer holds the stale sentence the row replaced.
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

	cases := []struct {
		row   string
		want  []string
		stale []string
	}{
		{
			row:   "docs and tags reach every item",
			want:  []string{"descriptors carry both", "a tag rides every declaration and declared item"},
			stale: []string{"does not read a `///` block", "tag is carried on a `type` declaration alone"},
		},
		{
			row:   "tags diagnostic",
			want:  []string{"attribute min requires a value, spelled min = value"},
			stale: []string{"min takes a value: write min = 0"},
		},
		{
			row:  "doc comment above an if branch",
			want: []string{"above an `if` branch"},
		},
		{
			row:   "payload-free arms in a table closure",
			want:  []string{"C++, C, C# and Go"},
			stale: []string{"Payload-free arms in a table closure are C++ only"},
		},
		{
			row:   "wstring table kind 33",
			want:  []string{"All nine carry table kind `33`"},
			stale: []string{"C++ also carries table kind `33`"},
		},
		{
			row:   "value defaults on the table wire",
			want:  []string{"C++ and Go carry all three on"},
			stale: []string{"C and Go carry them on the packet wire"},
		},
		{
			row:   "pointer surface on the Go wire",
			want:  []string{"RootBuilder", "LoadBuilder"},
			stale: []string{"a follow-on in those six"},
		},
		{
			row:   "form byte",
			want:  []string{"seven other backends still write"},
			stale: []string{"eight other backends still write"},
		},
		{
			row:   "message form 2",
			want:  []string{"Go carries the bitpacked"},
			stale: []string{"The other eight targets carry"},
		},
		{
			row:   "table-armed unions",
			want:  []string{"The C++ reference, the tool, C, C# and Go"},
			stale: []string{"every other backend refuses a unit that declares one"},
		},
		{
			row:  "arm refusals",
			want: []string{"an array of tables, or a `timestamp`"},
		},
		{
			row:   "maps",
			want:  []string{"carried by the C++ reference, the tool and Go"},
			stale: []string{"Every other backend"},
		},
		{
			row:  "one-sided ranges",
			want: []string{"Go carries the one-sided spelling first"},
		},
		{
			row:  "required",
			want: []string{"Go carries `| required` first"},
		},
		{
			row: "fixed refusal names",
			want: []string{
				"layout_record_too_large",
				"text_ill_formed",
				"count_over_bound",
				"length_over_bound",
				"truncated",
				"reserved_not_zero",
				"`layout_newer` is reserved for that proof",
			},
			stale: []string{"Framing damage sets `Malformed` rather than a name"},
		},
		{
			row:  "surface",
			want: []string{"Every surface implies `wire`", "surface is refused by name, listing the nine"},
		},
		{
			row:  "strict plain reads",
			want: []string{"records ahead of a tolerant stop", "locked builder's `RootFromJson` answers `false` with an empty report"},
		},
		{
			row:  "compressed float -0 default",
			want: []string{"compressed float's declared `-0` default becomes `+0`"},
		},
	}

	for _, c := range cases {
		t.Run(c.row, func(t *testing.T) {
			for _, want := range c.want {
				if !strings.Contains(page, want) {
					t.Errorf("USAGE.md no longer states Go's rule %q", want)
				}
			}
			for _, stale := range c.stale {
				if strings.Contains(page, stale) {
					t.Errorf("USAGE.md still carries the stale sentence %q", stale)
				}
			}
		})
	}
}
