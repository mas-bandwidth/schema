package javatable

import (
	"regexp"
	"strings"
	"testing"
)

// TestViewConstantCarriesItsSignedValueJava holds §8.3's sentence about a
// constant's integer column on the Java leg: int_value is the folded value, so
// the generated registry carries a negative constant with its sign, not its
// magnitude (the Go leg's TestViewConstantListsItsSignedValue, same probe).
func TestViewConstantCarriesItsSignedValueJava(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
const Neg = -5
const Big = 9223372036854775807
fixed table Root
{
    value int32 = 0
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	view := string(files["TableUnitView.java"])
	if view == "" {
		t.Fatal("the Java emitter produced no TableUnitView.java for a unit that declares a table")
	}
	for _, want := range []struct{ name, value string }{
		{"Neg", "-5"},
		{"Big", "9223372036854775807"},
	} {
		// the formatter's shape puts each argument on its own line
		row := regexp.MustCompile(`TableConstantInfo\.of\(\s*"` + want.name + `",\s*"[^"]*",\s*"[^"]*",\s*false,\s*(-?[0-9]+)L,`)
		m := row.FindStringSubmatch(view)
		if m == nil {
			t.Fatalf("the Java registry carries no row for constant %s:\n%s", want.name, view)
		}
		if m[1] != want.value {
			t.Errorf("the Java registry lists constant %s as %s, want %s (docs/SPEC-TABLES.md §8.3: int_value is the folded value)",
				want.name, strings.TrimSpace(m[1]), want.value)
		}
	}
}
