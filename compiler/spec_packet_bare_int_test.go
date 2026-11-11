// TestSpecPacketWriteRefusesBareIntOutsideWidth pins docs/SPEC.md §5.1's
// bare-integer row: a bare integer's range is its declared width, and a value
// the width cannot carry, or one that is not an integer, is a value off its
// declaration, held by the leg's own mechanism (a debug assert in the assert
// legs, a refusal in every build in the others). It pins the same width test
// in docs/PACKET-WIRE-ALGORITHM.md §2.4, the Rust idiom sentence (a union is
// a native enum; an enum is a newtype whose ordinal can be built out of set),
// and the shared cases in test/packet-write-bareint/.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// collapseSpace folds every run of white space to one space, so a pinned
// sentence holds however the page wraps it.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// bareIntSection is the text of page from the heading start up to the next
// heading end.
func bareIntSection(t *testing.T, name, page, start, end string) string {
	t.Helper()
	i := strings.Index(page, start)
	if i < 0 {
		t.Fatalf("%s no longer holds the section heading %q", name, start)
	}
	rest := page[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		t.Fatalf("%s no longer holds the section heading %q after %q", name, end, start)
	}
	return rest[:len(start)+j]
}

func TestSpecPacketWriteRefusesBareIntOutsideWidth(t *testing.T) {
	t.Parallel()
	specData, err := os.ReadFile("../docs/SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	algoData, err := os.ReadFile("../docs/PACKET-WIRE-ALGORITHM.md")
	if err != nil {
		t.Fatal(err)
	}
	spec := collapseSpace(string(specData))
	algo := collapseSpace(string(algoData))
	contract := bareIntSection(t, "docs/SPEC.md", spec, "### 5.1 The write contract", "## 6. Generated code")
	scalar := bareIntSection(t, "docs/PACKET-WIRE-ALGORITHM.md", algo, "### 2.4 WRITE_SCALAR", "### 2.5 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "SPEC §5.1 row 11, the bare integer off its width",
			text: contract,
			wants: []string{
				"| 11 | a bare integer",
				"whose value its declared width cannot carry, or that is not an integer",
				"binds only where a leg's storage can hold such a value",
				"held by the leg's mechanism (`write-refusal`): a debug assert in C, C++, C#, Rust, Java and Dart, a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
				"never masks or wraps such a value in the build where its check is active",
				"what an assert leg's release build writes for one is unspecified",
				"test/packet-write-bareint/",
			},
		},
		{
			row:  "SPEC §5.1 the Rust idiom sentence",
			text: contract,
			wants: []string{
				"An out-of-set union tag cannot be built: a union is a native enum",
				"an out-of-set ordinal can be built, and the writer's `debug_assert!` holds it",
			},
			stales: []string{
				"An out-of-set enum or union tag cannot be built",
			},
		},
		{
			row:  "PACKET-WIRE-ALGORITHM §2.4 the bare integer's width test",
			text: scalar,
			wants: []string{
				"**Width test first**",
				"a value the declared width cannot carry, or one that is not an integer",
				"held by the leg's mechanism: a debug assert in the assert legs (C, C++, C#, Rust, Java, Dart), a refusal in every build in the others",
			},
		},
		{
			row:    "docs/SPEC.md as a whole",
			text:   spec,
			stales: []string{"every build in every leg"},
		},
		{
			row:    "docs/PACKET-WIRE-ALGORITHM.md as a whole",
			text:   algo,
			stales: []string{"every build in every leg"},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.row, r.text, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.row, r.text, stale)
		}
	}

	schema, err := os.ReadFile("../test/packet-write-bareint/BareInt.schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"i8 int8", "u8 uint8", "u32 uint32", "u64 uint64"} {
		mustState(t, "BareInt.schema", collapseSpace(string(schema)), field)
	}

	data, err := os.ReadFile("../test/packet-write-bareint/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{}
	var comments []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			comments = append(comments, strings.TrimPrefix(line, "#"))
			continue
		}
		if line == "" {
			continue
		}
		cases[collapseSpace(line)] = true
	}
	header := collapseSpace(strings.Join(comments, "\n"))
	mustState(t, "cases.txt header", header, "by the debug build's assert in an assert leg")
	mustState(t, "cases.txt header", header, "by the writer's refusal, in every build, in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose")
	mustState(t, "cases.txt header", header, "skips it and says so")
	want := []string{
		"i8 300 refused",
		"i8 -129 refused",
		"i8 1.5 refused",
		"u8 256 refused",
		"u8 -1 refused",
		"u32 4294967296 refused",
		"u64 18446744073709551616 refused",
		"u64 -1 refused",
		"i8 -128 written",
		"i8 127 written",
		"u8 0 written",
		"u8 255 written",
		"u32 4294967295 written",
		"u64 18446744073709551615 written",
	}
	for _, c := range want {
		if !cases[c] {
			t.Errorf("test/packet-write-bareint/cases.txt does not hold the case %q", c)
		}
	}
	if len(cases) != len(want) {
		t.Errorf("test/packet-write-bareint/cases.txt holds %d cases, want %d", len(cases), len(want))
	}
}
