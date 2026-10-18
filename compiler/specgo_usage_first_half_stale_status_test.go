package compiler

import (
	"os"
	"strings"
	"testing"
)

// TestSpecGoUsageFirstHalfStaleStatus pins the first-half rows of the
// spec-vs-Go review of 2026-10-18 into docs/USAGE.md. For each row it asserts
// the page holds the row's Go spelling or name and no longer holds the stale
// sentence the row replaced.
func TestSpecGoUsageFirstHalfStaleStatus(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)

	has := func(want string) {
		t.Helper()
		if !strings.Contains(page, want) {
			t.Errorf("USAGE.md does not state %q", want)
		}
	}
	notHas := func(stale string) {
		t.Helper()
		if strings.Contains(page, stale) {
			t.Errorf("USAGE.md still holds the stale sentence %q", stale)
		}
	}

	// docs and tags banner: docs and tags reach every item and Go's descriptors.
	has("Docs and tags reach every item")
	notHas("does not read a `///` block")
	notHas("carried on a `type` declaration alone")

	// tags: quote the real diagnostic.
	has("attribute min requires a value, spelled min = value")
	notHas("min takes a value: write min = 0")

	// doc comments: /// above an if branch is refused.
	has("above an if branch")

	// union: payload-free arms in a table closure are carried by Go and C.
	has("Payload-free arms in a table closure are carried by C++, C")
	notHas("Payload-free arms in a table closure are C++ only")

	// wstring(N): all nine carry table kind 33.
	has("All nine also carry table kind `33`")
	notHas("C++ also carries table kind `33`")

	// defaults: Go carries string, bytes and flags defaults on the table wire.
	has("C++ and Go carry all three on")
	notHas("C and Go carry them on the packet wire")

	// Tables intro: Go carries the pointer surface with RootBuilder/LoadBuilder.
	has("`RootBuilder` and `LoadBuilder`")
	notHas("is a follow-on in those six")

	// form byte: Go writes the current form.
	has("and by Go; the seven other backends")
	notHas("the eight other backends still write its previous")

	// message form: Go carries the bitpacked form 2.
	has("The C++ backend, the tool and Go carry a")
	notHas("The C++ backend and the tool carry a form-`2`")

	// messages, table arms: Go carries table-armed unions.
	has("The C++ reference, the tool and Go carry")
	notHas("The C++ reference and the tool carry the")

	// messages, arm refusals: an array of tables and a timestamp.
	has("an array of tables")
	has("a timestamp, or an unbounded")

	// maps banner: Go carries maps.
	has("carried by the C++ reference, the tool and Go")
	notHas("carried by the C++ reference and the tool.")

	// ranged integers: cite SPEC-TABLES.md §4, not §4.3.
	has("(SPEC-TABLES.md §4). The one-sided spelling")
	notHas("(SPEC-TABLES.md §4.3)")
	has("Go carries it first, and")

	// | required is Go-first.
	has("Go carries `| required` first; every other target")

	// --surface: every surface implies wire; unknown surface refused by name.
	has("Every surface implies `wire`")
	has("An unknown surface is refused by name")

	// strict plain reads: the two exceptions.
	has("keeps the records it decoded ahead of")
	has("`FromJson` answers `false` with an empty report")

	// floats: a compressed float's -0 default becomes +0.
	has("compressed float's `-0` lands and reads back as `+0`")
}
