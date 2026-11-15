// TestSpecPacketWriteBareIntWidth pins the packet Write's rule for a bare
// intN / uintN value outside its declared N-bit width (SPEC.md §5.1 row 11):
// the declaration does not admit it, and the leg's own mechanism checks it
// (docs/rulings.json `write-refusal`: a debug assert in an assert leg, a
// refusal in every build elsewhere). PACKET-WIRE-ALGORITHM.md §2 names it
// among the writer contracts and §2.4 states it on the bare-integer row.
// The §5.1 Rust idiom row records that the value cannot be built there, and
// test/packet-write-bareint holds the shared cases the port cards run.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecPacketWriteBareIntWidth(t *testing.T) {
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

	writer := oneLine(specTablesSection(t, wire, "## 2. The writer", "### 2.1 bits_required"))
	row := "PACKET-WIRE-ALGORITHM §2 the writer"
	for _, want := range []string{
		"a bare integer off its declared width",
		"a debug `ASSERT` where the language has that idiom, a refusal in every build where it does not",
		"(SPEC §5.1)",
		"No write-side check is a substitute for the reader",
	} {
		mustState(t, row, writer, want)
	}
	for _, stale := range []string{
		"every-build only in Go and Elixir",
		"every build in every leg",
	} {
		mustNotState(t, row, writer, stale)
	}

	scalar := oneLine(specTablesSection(t, wire, "### 2.4 WRITE_SCALAR", "### 2.5 Compressed float (write)"))
	row = "PACKET-WIRE-ALGORITHM §2.4 the bare-integer row"
	for _, want := range []string{
		"The value must fit its declared width (SPEC §5.1 row 11)",
		"a debug assert in C, C++, C#, Rust, Java and Dart",
		"a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
		"cast to the **same-width unsigned** first",
	} {
		mustState(t, row, scalar, want)
	}

	row = "SPEC §5.1 row 11"
	for _, want := range []string{
		"a bare `intN`/`uintN` value outside its declared `N`-bit width",
		"not on the wire to refuse",
		"the reader lands the low `N` bits",
		"The writer's check is the only guard",
	} {
		mustState(t, row, oneLine(spec), want)
	}

	mustState(t, "SPEC §5.1 the Rust idiom row", oneLine(spec),
		"A bare integer outside its declared width cannot be built either")
	mustState(t, "SPEC §5.1 the Rust idiom row", oneLine(spec),
		"the storage is `iN`/`uN`")

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
		if len(f) < 4 {
			t.Errorf("cases.txt: %q is not <name> <type> <value> <verdict>", line)
			continue
		}
		got[f[0]] = strings.Join(f[3:], " ")
	}
	for name, verdict := range map[string]string{
		"int8-min":         "written",
		"int8-max":         "written",
		"int8-above-max":   "refused",
		"int8-below-min":   "refused",
		"uint8-max":        "written",
		"uint8-above-max":  "refused",
		"uint8-negative":   "refused",
		"int16-max":        "written",
		"int16-above-max":  "refused",
		"int16-below-min":  "refused",
		"uint16-above-max": "refused",
		"int32-above-max":  "refused",
		"uint32-above-max": "refused",
		"int64-min":        "written",
		"int64-max":        "written",
		"int64-above-max":  "refused",
		"uint64-max":       "written",
		"uint64-above-max": "refused",
		"uint64-negative":  "refused",
	} {
		if got[name] != verdict {
			t.Errorf("cases.txt: case %s has verdict %q, want %q", name, got[name], verdict)
		}
	}
	if len(got) != 27 {
		t.Errorf("cases.txt: %d cases, want the 27 the widths name", len(got))
	}
}
