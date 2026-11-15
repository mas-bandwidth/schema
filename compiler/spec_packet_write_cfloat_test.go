// TestSpecPacketWriteRefusesOffRangeCompressedFloat pins the packet Write's
// rule for a compressed float off its range (SPEC.md §5.1 row 10): a NaN, an
// infinity, or a finite value outside [min, max] is checked before quantizing,
// by the leg's own mechanism (docs/rulings.json `write-refusal`: a debug
// assert in an assert leg, a refusal in every build elsewhere).
// PACKET-WIRE-ALGORITHM.md §2.5 states the check and no longer states the
// [0, 1] value clamp as a step of the algorithm; the integer clamp after
// rounding stays. The §5.1 packet column covers row 10, neither page says a
// check runs "every build in every leg", and test/packet-write-cfloat holds
// the shared cases the port cards run.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecPacketWriteRefusesOffRangeCompressedFloat(t *testing.T) {
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

	write := oneLine(specTablesSection(t, wire, "### 2.5 Compressed float (write)", "### 2.6 "))
	row := "PACKET-WIRE-ALGORITHM §2.5 the compressed-float write"
	for _, want := range []string{
		"CHECK value is finite and min <= value <= max",
		"`write-refusal`",
		"a debug assert in C, C++, C#, Rust, Java and Dart",
		"a refusal in every build in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose",
		"before quantizing",
		"unspecified (SPEC §5)",
		"if integer > steps: integer := steps",
		"The integer clamp is not a value clamp",
		"Negative zero normalises to +0",
	} {
		mustState(t, row, write, want)
	}
	for _, stale := range []string{
		"ASSERT value is finite",
		"normalized := clamp to [0, 1]",
		"NaN forced in via",
	} {
		mustNotState(t, row, write, stale)
	}

	header := ""
	for _, line := range strings.Split(spec, "\n") {
		if strings.HasPrefix(line, "| leg | `FixedSave` | `SaveMessages` | packet `Write`") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatal("docs/SPEC.md §5.1 no longer holds the FixedSave / SaveMessages / packet Write table")
	}
	if strings.Contains(header, "(rows 1–7)") || !strings.Contains(header, "10)") {
		t.Errorf("SPEC §5.1: the packet Write column header %q does not cover row 10", header)
	}
	mustState(t, "SPEC §5.1 strict-only", oneLine(spec),
		"no writer quantizes one into it in the build where its check is active")

	for _, page := range []struct{ name, text string }{
		{"docs/SPEC.md", spec},
		{"docs/PACKET-WIRE-ALGORITHM.md", wire},
	} {
		mustNotState(t, page.name, oneLine(page.text), "every build in every leg")
	}

	if _, err := os.Stat("../test/packet-write-cfloat/CFloat.schema"); err != nil {
		t.Errorf("test/packet-write-cfloat/CFloat.schema: %v", err)
	}
	casesData, err := os.ReadFile("../test/packet-write-cfloat/cases.txt")
	if err != nil {
		t.Fatalf("test/packet-write-cfloat/cases.txt: %v", err)
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
		"NaN":           "refused",
		"+Inf":          "refused",
		"-Inf":          "refused",
		"10.5":          "refused",
		"-0.5":          "refused",
		"3.4028235e+38": "refused",
		"0.0":           "written",
		"10.0":          "written",
		"5.0":           "written",
		"-0.0":          "written reads-back +0.0",
	} {
		if got[value] != verdict {
			t.Errorf("cases.txt: value %s has verdict %q, want %q", value, got[value], verdict)
		}
	}
	if len(got) != 10 {
		t.Errorf("cases.txt: %d cases, want the 10 the rule names", len(got))
	}
}

// oneLine joins a page's wrapped lines (and a case list's `#` comment lines)
// into single-spaced text, so a sentence pins whatever the wrap.
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\n#", "\n")), " ")
}
