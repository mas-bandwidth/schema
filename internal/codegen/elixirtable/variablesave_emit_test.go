package elixirtable

import (
	"strings"
	"testing"
)

// TestElixirVariableSaveEmitsTheWriteContract is the fast-lane half of
// TestElixirVariableSaveRefusesOffContract: it generates a tiny unit and
// asserts on the emitted text, running no toolchain. The variable writer
// holds EVERY active field to its declaration (not only `required` ones), a
// [N]T is never cut at N, a counted array carries its Min and is born at it,
// a float32 field's range ends are narrowed to float32, and a compressed
// float's -0 is folded to +0 on the variable read and write and on the fixed
// write (docs/SPEC.md §5.1, §4.2, §4.6; docs/SPEC-TABLES.md §3.3).
func TestElixirVariableSaveEmitsTheWriteContract(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package vemit

union Pick
{
    limit int16 | min = -2, max = 3
}

table Tv
{
    pick  Pick
    edge  float32 | min = 0, max = 0.1, resolution = 0.01
    born  [2..4]uint8
    trio  [3]int16
    xs    [..8]int32
}

fixed table Fx
{
    tilt float32 | min = -1, max = 1, resolution = 0.01
    flat float32
}
`)
	var all strings.Builder
	for _, src := range generateElixir(t, u) {
		all.Write(src)
	}
	text := all.String()

	want := []struct{ what, needle string }{
		{"every active field is held, not only required ones", "        if Map.get(f, :text_list, false), do: validate_text_list(f, v)\n        write_value(f, v)\n"},
		{"a union arm payload is held", "{:ok, a} -> write_value(a, if(payload == nil, do: a.default, else: payload))"},
		{"an over-long [N]T refuses", "if Map.has_key?(f, :fixed_array) and n > f.fixed_array, do: throw(:invalid_value)"},
		{"a counted array's Min is held on write", "(n < Map.get(f, :min_count, 0) or n > f.array_bound)"},
		{"a counted array's Min is held on read", "defp clamp_count(n, %{min_count: min}, _report) when n < min,"},
		{"an enum name outside the set refuses", `if v not in [nil, "None"] and not Map.has_key?(f.by_name, v), do: throw(:invalid_value)`},
		{"a non-integer in an int field refuses", "if not is_integer(v), do: throw(:invalid_value)"},
		{"the float32 range is held at float32", "x = float_at_width(v, width)"},
		{"a [2..4] holds its Min", "min_count: 2,"},
		{"an absent [2..4] lands two defaults", "default: [0, 0]"},
		{"a compressed -0 is folded on the variable read", "clamp_int(cfloat_zero(float_of(payload, f.width, f.non_finite), f), f, report)"},
		{"a compressed -0 is folded on the variable write", "v = stored_float(f, Map.get(value, f.name, f.default))"},
		{"a compressed -0 is folded on the fixed write", "R.cf32_bits("},
	}
	for _, w := range want {
		if !strings.Contains(text, w.needle) {
			t.Errorf("%s: the emitted Elixir has no %q", w.what, w.needle)
		}
	}
	// the ends as the runtime holds them: float32's, never the declared double
	edge := fixedFloatLiteral(float64(float32(0.1)))
	if !strings.Contains(text, "max: "+edge+",") {
		t.Errorf("a float32 field's max = 0.1 should be emitted as float32(0.1) = %s", edge)
	}
	if strings.Contains(text, "max: "+fixedFloatLiteral(0.1)+",") {
		t.Errorf("a float32 field's max = 0.1 is emitted as the double %s", fixedFloatLiteral(0.1))
	}

	// the old shapes are gone: a [N]T cut at N, and a check under `required` only
	for _, gone := range []string{
		"kept = Enum.take(elements, n)",
		"if Map.get(f, :required, false) and field_active?(f, desc, value) do",
	} {
		if strings.Contains(text, gone) {
			t.Errorf("the emitted Elixir still has %q", gone)
		}
	}

	// the fixed writer folds the compressed float only: a plain float keeps
	// its sign (docs/SPEC-TABLES.md §3.3)
	if !strings.Contains(text, "R.cf32_bits(") {
		t.Errorf("the fixed writer of the compressed tilt does not fold -0")
	}
	if !strings.Contains(text, "R.f32_bits(") {
		t.Errorf("the fixed writer of the plain flat folds -0: a plain float keeps its sign")
	}
}
