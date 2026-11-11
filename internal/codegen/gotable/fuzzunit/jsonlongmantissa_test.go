package tabledemo_test

import (
	"strings"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

// TestGeneratedJSONLongMantissaReadsByValue holds docs/SPEC-TABLES.md §16.2
// on the generated reader: a valid JSON number is read by its value however
// many bytes its mantissa spans, so a long run of zeros followed by a negative
// exponent still lands the value the digits name.
func TestGeneratedJSONLongMantissaReadsByValue(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return strings.Repeat("0", n) }
	read := func(text string) (tabledemo.ProfileConfig, bool) {
		var v tabledemo.ProfileConfig
		var r tabledemo.TableReport
		return v, tabledemo.ProfileConfigFromJSON(&v, []byte(text), &r)
	}
	for _, c := range []struct {
		name string
		text string
		want float64
	}{
		{"800 zeros", "1" + zeros(800) + "e-800", 1},
		{"1000 zeros", "1" + zeros(1000) + "e-1000", 1},
		{"5000 zeros", "1" + zeros(5000) + "e-5000", 1},
		{"799 zeros control", "1" + zeros(799) + "e-799", 1},
		{"long mantissa", "123" + zeros(900) + "e-900", 123},
		{"long mantissa fraction", "123" + zeros(900) + "e-902", 1.23},
	} {
		v, ok := read(`{"precision":` + c.text + `}`)
		if !ok {
			t.Errorf("%s: precision refused", c.name)
			continue
		}
		if v.Precision != c.want {
			t.Errorf("%s: precision landed %v, want %v", c.name, v.Precision, c.want)
		}
	}
	for _, c := range []struct {
		name string
		text string
	}{
		{"800 zeros", "1" + zeros(800) + "e-800"},
		{"1000 zeros", "1" + zeros(1000) + "e-1000"},
		{"5000 zeros", "1" + zeros(5000) + "e-5000"},
		{"799 zeros control", "1" + zeros(799) + "e-799"},
	} {
		v, ok := read(`{"experience":` + c.text + `}`)
		if !ok {
			t.Errorf("%s: experience refused", c.name)
			continue
		}
		if v.Experience != 1 {
			t.Errorf("%s: experience landed %d, want 1", c.name, v.Experience)
		}
	}
}
