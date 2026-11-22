package elixirtable

import (
	"strings"
	"testing"
)

// TestElixirCompressedF32EndsSource pins A COMPRESSED FLOAT'S RANGE IS HELD AT
// FLOAT32 (docs/SPEC-TABLES.md §3.3) in the generated Elixir without a
// toolchain: the descriptor every variable, JSON, cook and block read and
// write compares against carries the float32 nearest each end (max = 0.1 is
// 0.10000000149011612, never 0.1), the fixed writer holds the leaf to the same
// pair through R.cf32_bits, and the runtime narrows a written value to
// float32 before the test. The control: a plain float32 carries no range at
// all. TestElixirCompressedF32EndsCorpus runs the corpus itself.
func TestElixirCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package f32src
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
`)
	files := generateElixir(t, u)
	var table, fixed string
	for name, b := range files {
		switch {
		case strings.HasSuffix(name, "Table.ex") && !strings.Contains(name, "Runtime"):
			table = string(b)
		case strings.HasSuffix(name, "Fixed.ex") && !strings.Contains(name, "Runtime"):
			fixed = string(b)
		}
	}
	if table == "" || fixed == "" {
		t.Fatalf("no Table.ex or no fixed module among %d files", len(files))
	}
	const lo, hi = "-0.30000001192092896", "0.10000000149011612"
	for _, want := range []string{
		"min: " + lo + ",", "max: " + hi + ",",
		"elem_min: " + lo + ",", "elem_max: " + hi + ",",
		"<<n::float-32>> = <<v::float-32>>",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("Table.ex holds no %q", want)
		}
	}
	for _, bad := range []string{"min: -0.3,", "max: 0.1,", "elem_min: -0.3,", "elem_max: 0.1,"} {
		if strings.Contains(table, bad) {
			t.Errorf("Table.ex holds the declared end as a float64: %q", bad)
		}
	}
	if !strings.Contains(fixed, "{"+lo+", "+hi+"}") || !strings.Contains(fixed, "R.cf32_bits(v_r, @r_f_r)") {
		t.Error("the fixed writer does not hold r to its float32 ends through R.cf32_bits")
	}
	// the control: the plain float p writes its bits with no range
	if !strings.Contains(fixed, "R.f32_bits(v_p)") || strings.Contains(fixed, "@r_f_p") {
		t.Errorf("the plain float32 p is held to a range in the fixed writer")
	}
	at := strings.Index(table, `name: "p",`)
	if at < 0 {
		t.Fatal(`Table.ex has no descriptor for "p"`)
	}
	end := strings.Index(table[at:], "default:")
	if end < 0 || strings.Contains(table[at:at+end], "min:") {
		t.Errorf("the plain float32 p carries a range in its descriptor")
	}
}
