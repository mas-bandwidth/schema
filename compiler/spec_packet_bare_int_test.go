// TestSpecPacketWritesBareIntAtDeclaredWidth pins the packet Write's rule for
// a bare integer's declared width (SPEC.md §5.1 row 11): a bare `intN`/`uintN`
// value outside its declared width, where the target's value type is wider
// than the wire, is checked by the leg's own mechanism (docs/rulings.json
// `write-refusal`: a debug assert in an assert leg, a refusal in every build
// elsewhere). PACKET-WIRE-ALGORITHM.md §2 states the mechanism across the
// writer contracts and §2.4 states the width rule for the bare integer row.
// The §5.1 packet column keeps its earlier row list; row 11 rides the same
// mechanism, and test/packet-write-bareint holds the shared cases the port
// cards run. Neither page says a check runs "every build in every leg".
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecPacketWritesBareIntAtDeclaredWidth(t *testing.T) {
	t.Parallel()
	wireData, err := os.ReadFile("../docs/PACKET-WIRE-ALGORITHM.md")
	if err != nil {
		t.Fatal(err)
	}
	specData, err := os.ReadFile("../docs/SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	wire := string(wireData)
	spec := string(specData)

	write := oneLine(specTablesSection(t, spec, "### 5.1 The write contract", "## 6. Generated code"))
	row := "SPEC §5.1 row 11 the bare integer's width"
	for _, want := range []string{
		"| 11 | a bare `intN`/`uintN` value outside its declared width, where the target's value type is wider than the wire | not a wire value: only the declared `N` bits ride, and the reader materializes an `N`-bit value the declaration admits |",
		"**Row 11 is the bare integer's declared width.**",
		"A bare `intN`/`uintN` value is an `N`-bit value.",
		"a debug assert in C, C++, C#, Rust, Java and Dart",
		"a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
		"the reader materializes an `N`-bit value the declaration admits",
	} {
		mustState(t, row, write, want)
	}

	rust := ""
	for _, line := range strings.Split(spec, "\n") {
		if strings.HasPrefix(line, "| Rust | `debug_assert!`") {
			rust = line
			break
		}
	}
	if rust == "" {
		t.Fatal("docs/SPEC.md §5.1 no longer holds the Rust idiom row")
	}
	for _, want := range []string{
		"A bare integer outside its declared width cannot be built either",
		"its storage is exactly that width",
	} {
		mustState(t, "SPEC §5.1 the Rust idiom row", rust, want)
	}
	mustState(t, "SPEC §5.1 the packet column", write, "packet `Write` (rows 1–8 and 10)")

	general := oneLine(specTablesSection(t, wire, "## 2. The writer", "### 2.1 "))
	row = "PACKET-WIRE-ALGORITHM §2 the writer contracts"
	for _, want := range []string{
		"a bare integer outside its declared width (row 11)",
		"a debug assert in C, C++, C#, Rust, Java and Dart",
		"a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
	} {
		mustState(t, row, general, want)
	}
	for _, stale := range []string{
		"every-build only in Go and Elixir",
	} {
		mustNotState(t, row, general, stale)
	}

	scalar := oneLine(specTablesSection(t, wire, "### 2.4 WRITE_SCALAR", "### 2.5 "))
	row = "PACKET-WIRE-ALGORITHM §2.4 the bare integer write"
	for _, want := range []string{
		"A value outside the declared width, where the target's value type is wider than the wire",
		"SPEC §5.1 row 11",
		"checked by the leg's mechanism before the low `N` bits ride",
		"same-width two's-complement (signed) or unsigned pattern",
	} {
		mustState(t, row, scalar, want)
	}

	for _, page := range []struct{ name, text string }{
		{"docs/SPEC.md", spec},
		{"docs/PACKET-WIRE-ALGORITHM.md", wire},
	} {
		mustNotState(t, page.name, oneLine(page.text), "every build in every leg")
	}

	if _, err := os.Stat("../test/packet-write-bareint/BareInt.schema"); err != nil {
		t.Errorf("test/packet-write-bareint/BareInt.schema: %v", err)
	}
	casesData, err := os.ReadFile("../test/packet-write-bareint/cases.txt")
	if err != nil {
		t.Fatalf("test/packet-write-bareint/cases.txt: %v", err)
	}
	cases := string(casesData)
	casesText := oneLine(cases)
	mustState(t, "cases.txt header", casesText,
		"\"refused\" means: by the debug build's assert in an assert leg")
	mustState(t, "cases.txt header", casesText,
		"by the writer's refusal in every build in an every-build leg")
	got := map[string]string{}
	for _, line := range strings.Split(cases, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			t.Errorf("cases.txt: %q is not <name> <value> <verdict>", line)
			continue
		}
		got[f[1]] = strings.Join(f[2:], " ")
	}
	for value, verdict := range map[string]string{
		"-128":                 "written",
		"127":                  "written",
		"-129":                 "refused",
		"128":                  "refused",
		"-32768":               "written",
		"32767":                "written",
		"32768":                "refused",
		"2147483647":           "written",
		"2147483648":           "refused",
		"-9223372036854775808": "written",
		"9223372036854775808":  "refused",
		"255":                  "written",
		"256":                  "refused",
		"-1":                   "refused",
		"65536":                "refused",
		"4294967296":           "refused",
		"18446744073709551615": "written",
		"18446744073709551616": "refused",
	} {
		if got[value] != verdict {
			t.Errorf("cases.txt: value %s has verdict %q, want %q", value, got[value], verdict)
		}
	}
	if len(got) != 18 {
		t.Errorf("cases.txt: %d cases, want the 18 the rule names", len(got))
	}
}
