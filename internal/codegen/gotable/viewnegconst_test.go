package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

// TestViewConstantListsItsSignedValue holds §8.3's one sentence about a
// constant's integer column: int_value is the folded value. A negative
// constant is folded to a signed big.Int, and both the generated UnitView()
// and the listing pin must carry that signed value rather than its magnitude.
func TestViewConstantListsItsSignedValue(t *testing.T) {
	t.Parallel()
	const source = `package probe
const Neg = -5
const Big = 9223372036854775807
fixed table Root
{
    value int32 = 0
}
`
	unit := unitFrom(t, source)

	pin := viewlisting.Listing(unit)
	for _, want := range []struct {
		name  string
		value string
	}{
		{"Neg", "int=-5 "},
		{"Big", "int=9223372036854775807 "},
	} {
		line := ""
		for candidate := range strings.SplitSeq(pin, "\n") {
			if strings.HasPrefix(candidate, "constant "+want.name+" ") {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("viewlisting pin carries no line for constant %s:\n%s", want.name, pin)
		}
		if !strings.Contains(line, want.value) {
			t.Errorf("viewlisting pin lists constant %s as %q, want %q (docs/SPEC-TABLES.md §8.3: int_value is the folded value)\n%s",
				want.name, line, want.value, pin)
		}
	}

	view := generate(t, source)["ProbeView.go"]
	if len(view) == 0 {
		t.Fatal("the Go emitter produced no ProbeView.go for a unit that declares a table")
	}
	for _, want := range []struct {
		name  string
		value string
	}{
		{"Neg", "IntValue: -5,"},
		{"Big", "IntValue: 9223372036854775807,"},
	} {
		line := ""
		for candidate := range strings.SplitSeq(string(view), "\n") {
			if strings.Contains(candidate, `Name: "`+want.name+`",`) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("Go's UnitView carries no row for constant %s:\n%s", want.name, view)
		}
		if !strings.Contains(line, want.value) {
			t.Errorf("Go's UnitView lists constant %s as %q, want %q (docs/SPEC-TABLES.md §8.3: int_value is the folded value)",
				want.name, line, want.value)
		}
	}
}
