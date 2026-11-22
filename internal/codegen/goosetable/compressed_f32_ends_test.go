package goosetable_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// gooseF32EndsSchema has a compressed float32 leaf and array and a plain
// float32 in each form, under the corpus's ends (max = 0.1, min = -0.3).
const gooseF32EndsSchema = `package f32src
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

// gooseF32EndsFaults says what in files does not hold A COMPRESSED FLOAT'S
// RANGE AT FLOAT32 (docs/SPEC-TABLES.md §3.3), one line per fault.
func gooseF32EndsFaults(files map[string][]byte) []string {
	var faults []string
	rt, table := string(files["schema_tables.goose"]), ""
	for _, b := range files {
		if strings.Contains(string(b), "fn _FFixedCheck(") {
			table = string(b)
		}
	}
	if table == "" {
		return append(faults, "no generated module holds F's fixed check")
	}
	// the one range test every variable, JSON, cook and block read and every
	// writer takes narrows the value and both ends to float32
	at := strings.Index(rt, "fn frange_holds(")
	if at < 0 {
		return append(faults, "schema_tables.goose has no frange_holds")
	}
	end := strings.Index(rt[at:], "\n}\n")
	body := rt[at : at+end]
	for _, want := range []string{"let x = v as f32;", "let lo = d.flo as f32;", "let hi = d.fhi as f32;", "return x >= lo && x <= hi;"} {
		if !strings.Contains(body, want) {
			faults = append(faults, "frange_holds does not hold a float32 at float32: no "+want)
		}
	}
	// the typed fixed and message writers and the fixed read spell the
	// float32 ends themselves
	const hi = "0.10000000149011612"
	for _, want := range []string{
		"if !(v.R >= (-0.30000001192092896) && v.R <= " + hi + ") { return false; }",
		"if v.R < (-0.30000001192092896) || v.R > " + hi + " { return schema_rt::FixedRefuseOutOfRange; }",
		"schema_rt::msg_quantize(v.R, schema_rt::fixed_bits_f32(0xbe99999a), schema_rt::fixed_bits_f32(0x3ecccccd)",
	} {
		if !strings.Contains(table, want) {
			faults = append(faults, "the table module holds no "+want)
		}
	}
	if strings.Contains(table, "<= 0.1)") || strings.Contains(table, "> 0.1 ") {
		faults = append(faults, "the table module compares against the declared max as a float64")
	}
	// the control: the plain float p carries no range anywhere
	if strings.Contains(table, "v.P >=") || strings.Contains(table, "v.P <") {
		faults = append(faults, "the plain float32 p is held to a range")
	}
	for line := range strings.SplitSeq(table, "\n") {
		if strings.Contains(line, `name: "p",`) && strings.Contains(line, "frange: true") {
			faults = append(faults, "the plain float32 p's descriptor carries a range")
		}
	}
	return faults
}

// TestGooseCompressedF32EndsSource pins the float32 range test in the
// generated Goose without a toolchain; TestGooseCompressedF32EndsCorpus runs
// the corpus itself. Its negative control widens frange_holds to float64 and
// requires the fault named.
func TestGooseCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	u := probeUnit(t, gooseF32EndsSchema)
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for _, f := range gooseF32EndsFaults(files) {
		t.Error(f)
	}
	t.Run("NegativeControl", func(t *testing.T) {
		wide := maps.Clone(files)
		wide["schema_tables.goose"] = []byte(strings.Replace(string(files["schema_tables.goose"]), "let x = v as f32;", "let x = v;", 1))
		if got := gooseF32EndsFaults(wide); len(got) == 0 {
			t.Error("a frange_holds that compares at float64 went unnoticed")
		}
	})
}
