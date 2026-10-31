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
	specBytes, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	spec := string(specBytes)

	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	pageFlat := flat(page)
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
	// Wrapped prose still counts: a carrier or a false exclusion is the words
	// in order, not the line break.
	hasFlat := func(want string) {
		t.Helper()
		if !strings.Contains(pageFlat, flat(want)) {
			t.Errorf("USAGE.md does not state %q", want)
		}
	}
	notHasFlat := func(stale string) {
		t.Helper()
		if strings.Contains(pageFlat, flat(stale)) {
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

	// union: payload-free arms in a table closure are carried by C++, C, C# and Go.
	has("Payload-free arms in a table closure are carried by C++, C, C#\nand Go; the other five targets refuse")
	notHas("Payload-free arms in a table closure are C++ only")
	notHas("other six targets refuse that table-side use")

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

	// message form: one list, SPEC-TABLES.md "WHERE THE FORM IS CARRIED".
	// A supported carrier missing from that section, or a false exclusion
	// back in USAGE or SPEC, fails. The stale C++/Go-only sentences are not
	// required.
	hasFlat("WHERE THE FORM IS CARRIED")
	hasFlat("It is not a C++ and Go-only path, and it is not the same subset in every language.")
	notHasFlat("The C++ reference, the tool and Go read and write the bitpacked form-`2`")
	notHasFlat("The C++ reference, Go and the compiler's own engine read and write the current bitpacked form-")
	notHasFlat("other seven language backends carry the variable form alone")
	notHas("ahead of its implementation")
	notHas("its body is byte framed")
	notHasFlat("announcement surface only")
	notHasFlat("nothing here reads a form-")

	section := messageCarrierSection(spec)
	if section == "" {
		t.Fatal("SPEC-TABLES.md has no WHERE THE FORM IS CARRIED section")
	}
	sectionFlat := flat(section)
	hasSection := func(want string) {
		t.Helper()
		if !strings.Contains(sectionFlat, flat(want)) {
			t.Errorf("WHERE THE FORM IS CARRIED does not state %q", want)
		}
	}
	notSection := func(stale string) {
		t.Helper()
		if strings.Contains(sectionFlat, flat(stale)) {
			t.Errorf("WHERE THE FORM IS CARRIED holds the false exclusion %q", stale)
		}
	}
	hasSection("**C++** (`internal/codegen/cpptable/messageload.go`")
	hasSection("EVERY BACKEND EMITS THE BATCH FOR FIXED ROOTS ONLY, AND NO BACKEND EMITS A MESSAGE VERB, A MESSAGE BODY CODEC OR A REFUSAL STUB NAMED FOR A VARIABLE TABLE")
	hasSection("`emitMessageEntries` writes `MeasureMessages`, `SaveMessages` and `LoadMessages` for a fixed root")
	hasSection("**C** (`internal/codegen/ctable/message.go`")
	hasSection("`measure_messages`, `save_messages` and `load_messages` for each fixed table that is not a map entry")
	hasSection("**C#** (`internal/codegen/cstable/message.go`")
	hasSection("for each fixed table and what it holds by value (`messageClosure`)")
	hasSection("**Go** (`internal/codegen/gotable/messagewrite.go`")
	hasSection("carries the batch for a `fixed table` root only")
	hasSection("The body codecs are emitted for the fixed closure alone (`messageCarries`)")
	hasSection("`LoadRetainMessages` and `SaveRetainMessages` on a fixed root are named refusals")
	hasSection("**Rust** (`internal/codegen/rusttable/message.go`")
	hasSection("**Java** (`internal/codegen/javatable/messageform.go`")
	hasSection("writes no message file otherwise")
	hasSection("**JavaScript** (`internal/codegen/jstable/messageform.go`")
	hasSection("`messagebatch.go` gates the three batch verbs to the fixed roots")
	hasSection("**Lua, Luau, Moo, Lobster and Squirrel**")
	notSection("over one region")
	notSection("variable table")
	hasSection("**Dart** (`internal/codegen/darttable/messageform_announce.go`")
	hasSection("`emitMessageBatch` returns unless the root is a declared fixed table")
	hasSection("**Elixir** (`internal/codegen/elixirtable/elixirtable.go`")
	hasSection("The module carries the announcement (`announce`, `announce_measure`, `announce_read`) and the bitpacked batch (`measure_messages`, `save_messages`, `load_messages`)")
	hasSection("not a generated verb per root")
	notSection("other seven language backends carry the variable form alone")
	notSection("other seven language backends carry the VARIABLE form")
	notSection("The eight ports carry the VARIABLE form alone")
	notSection("announcement surface only")
	notSection("nothing here reads a form-")
	notSection("are a named follow-on beside the wire-form work M20")
	notSection("The C++ reference, the tool and Go read and write the bitpacked form-")
	notSection("engine carry the BITPACKED body under form byte")
	specFlat := flat(spec)
	if strings.Contains(specFlat, flat("The C++ reference, Go and the compiler's own engine carry the BITPACKED body under form byte `2`")) {
		t.Error("SPEC-TABLES.md still limits the bitpacked body to the C++ reference, Go and the engine")
	}
	if strings.Contains(specFlat, flat("The C++ reference, Go and the compiler's own engine read and write the current bitpacked form-")) {
		t.Error("SPEC-TABLES.md still limits the bitpacked form-2 path to the C++ reference, Go and the engine")
	}
	if strings.Contains(specFlat, "The eight ports carry the VARIABLE form alone") {
		t.Error("SPEC-TABLES.md still excludes ports from the form-2 carriers")
	}
	if strings.Contains(specFlat, "other seven language backends carry the VARIABLE form") || strings.Contains(specFlat, "other seven language backends carry the variable form alone") {
		t.Error("SPEC-TABLES.md still says the other seven carry the variable form alone")
	}

	// messages, table arms: C++, the tool, C, C# and Go carry table-armed unions.
	has("The C++ reference, the tool, C, C# and Go carry")
	notHas("The C++ reference and the tool carry the")
	notHas("The C++ reference, the tool and Go carry")

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

// messageCarrierSection is the one capability list, from its heading through
// the next section. A carrier named only elsewhere does not satisfy the pin.
func messageCarrierSection(spec string) string {
	const start = "**WHERE THE FORM IS CARRIED.**"
	i := strings.Index(spec, start)
	if i < 0 {
		return ""
	}
	rest := spec[i:]
	before, _, ok := strings.Cut(rest, "#### THE SURFACE")
	if !ok {
		return ""
	}
	return before
}
