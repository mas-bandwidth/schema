package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lobsterF32EndsSchema has a compressed float32 leaf and array and a plain
// float32 in each form, under the corpus's ends (max = 0.1, min = -0.3).
const lobsterF32EndsSchema = `package f32src
table V
{
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
    xs [2]float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
fixed table F
{
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
`

// lobsterF32EndsFaults says what in the runtime's load.lobster and the
// generated table module does not hold A COMPRESSED FLOAT'S RANGE AT FLOAT32
// (docs/SPEC-TABLES.md §3.3), one line per fault.
func lobsterF32EndsFaults(load, table string) []string {
	var faults []string
	// the one range test the writers' check, the variable read and the JSON
	// read all take narrows the value and both ends to float32
	at := strings.Index(load, "def float_in_range(")
	if at < 0 {
		return append(faults, "load.lobster has no float_in_range")
	}
	body, _, _ := strings.Cut(load[at:], "\n\n")
	for _, want := range []string{"let v = fround(x)", "return v >= fround(fd.flo) and v <= fround(fd.fhi)"} {
		if !strings.Contains(body, want) {
			faults = append(faults, "float_in_range does not hold a float32 at float32: no "+want)
		}
	}
	// the typed fixed record's bounds spell the float32 ends themselves
	const want = "if v·.R < -0.30000001192092896 or v·.R > 0.10000000149011612:"
	if strings.Count(table, want) < 2 {
		faults = append(faults, "the table module's fixed bounds and check hold no "+want)
	}
	if strings.Contains(table, "v·.R > 0.1:") || strings.Contains(table, "v·.R < -0.3 ") {
		faults = append(faults, "the table module compares against a declared end as a float64")
	}
	// the control: the plain float p carries no range anywhere
	if strings.Contains(table, "v·.P <") || strings.Contains(table, "v·.P >") {
		faults = append(faults, "the plain float32 p is held to a range")
	}
	for line := range strings.SplitSeq(table, "\n") {
		if strings.Contains(line, `name: "p",`) && strings.Contains(line, "franged: true") {
			faults = append(faults, "the plain float32 p's descriptor carries a range")
		}
	}
	return faults
}

// TestLobsterCompressedF32EndsSource pins the float32 range test in the
// generated Lobster without a toolchain; TestLobsterCompressedF32EndsCorpus
// runs the corpus itself. Its negative control widens float_in_range to
// float64 and requires the fault named.
func TestLobsterCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, lobsterF32EndsSchema)
	load, err := os.ReadFile(filepath.Join(g.dir, "load.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.mods) != 1 {
		t.Fatalf("table modules %q, want one", g.mods)
	}
	table, err := os.ReadFile(filepath.Join(g.dir, g.mods[0]+".lobster"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range lobsterF32EndsFaults(string(load), string(table)) {
		t.Error(f)
	}
	t.Run("NegativeControl", func(t *testing.T) {
		wide := strings.Replace(string(load), "let v = fround(x)", "let v = x", 1)
		if got := lobsterF32EndsFaults(wide, string(table)); len(got) == 0 {
			t.Error("a float_in_range that compares at float64 went unnoticed")
		}
	})
}
