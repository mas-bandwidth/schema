package baseline_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const keyedRangeSrc = `package fixture

enum Team
{
    Red
    Blue
}

table Score
{
    points [Team]int32 | min = -100, max = 100
}
`

// TestEnumKeyedArrayElementRangeTighteningWarns: an enum-keyed array's BOUND
// is its key enum's size and is judged by the enum walk, but its ELEMENT is an
// ordinary value: a reader refuses a stored element past its declared range
// `out_of_range` (docs/SPEC-TABLES.md §4), so a tightened element range warns
// as it does on any other field. Only the bound is exempt.
func TestEnumKeyedArrayElementRangeTighteningWarns(t *testing.T) {
	for _, c := range []struct {
		name, after, token string
		warn               bool
	}{
		{"the element maximum lowered", "| min = -100, max = 50", "maximum 100 -> 50", true},
		{"the element minimum raised", "| min = -50, max = 100", "minimum -100 -> -50", true},
		{"the element maximum raised", "| min = -100, max = 200", "maximum", false},
		{"the element minimum lowered", "| min = -200, max = 100", "minimum", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			after := editOf(t, keyedRangeSrc, "| min = -100, max = 100", c.after)
			got := baseline.Diff(committed(t, keyedRangeSrc), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy)
			if refusals, _ := baseline.Split(got); len(refusals) != 0 {
				t.Fatalf("must not refuse, got:%s", summary(got))
			}
			if find(got, baseline.Warn, "Score.points", c.token) != c.warn {
				t.Errorf("warn on %q = %t, want %t, got:%s", c.token, !c.warn, c.warn, summary(got))
			}
		})
	}
	// the bound stays the enum walk's: a variant appended moves the bound and
	// draws nothing from this row
	grown := editOf(t, keyedRangeSrc, "    Blue\n", "    Blue\n    Green\n")
	got := baseline.Diff(committed(t, keyedRangeSrc), baseline.Render(unit(t, grown)), baseline.DefaultTokenPolicy)
	if find(got, baseline.Warn, "Score.points", "array bound") {
		t.Errorf("a key enum's size is the enum walk's to judge, not the bound row's, got:%s", summary(got))
	}
	// and a variant removed is reported by the enum walk, not a second time
	// by the bound row
	shrunk := editOf(t, keyedRangeSrc, "    Blue\n", "")
	got = baseline.Diff(committed(t, keyedRangeSrc), baseline.Render(unit(t, shrunk)), baseline.DefaultTokenPolicy)
	if find(got, baseline.Warn, "Score.points", "array bound") {
		t.Errorf("a key enum variant removed must not draw an array bound finding, got:%s", summary(got))
	}
}
