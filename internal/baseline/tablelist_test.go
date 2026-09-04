package baseline_test

// The UNBOUNDED ARRAY's half of the baseline (docs/SPEC-TABLES.md §2.9,
// §18.1, §18.2): the field renders `array=unbounded` with NO `bound=`, and
// the missing token IS the capacity fact — a `bound=` APPEARING on an
// `array=` move is a capacity SHRINK and warns, one VANISHING is a capacity
// GROWTH and passes.
//
// Every case carries this file's two controls: the DISCRIMINATION control
// (the same field edited in the direction the wire absorbs) and the
// ATTRIBUTION control (the same edit re-judged with the one policy row
// removed, which must go quiet).

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const listFixtureSrc = `package fixture

table Placement
{
    x float32
}

table Save
{
    placements []Placement
    scores     []int32
}
`

func listDiff(t *testing.T, base, live string, policy map[string]baseline.TokenRule) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, base), baseline.Render(unit(t, live)), policy)
}

// TestUnboundedRendersWithNoBound: `array=unbounded` and no `bound=` at all,
// which is what leaves the capacity verdict to the token's own presence.
func TestUnboundedRendersWithNoBound(t *testing.T) {
	text := baseline.Render(unit(t, listFixtureSrc)).Text()
	for _, want := range []string{
		"kind=14 elem=13 type=Placement array=unbounded\n",
		"kind=14 elem=4 array=unbounded\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the list field's line does not read %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "array=unbounded bound=") {
		t.Errorf("an unbounded array carries no bound=:\n%s", text)
	}
}

// TestAddingABoundWarns: the direction that ADDS a bound gains the clamp, so
// it warns as any capacity shrunk does (§2.9, §18.2).
func TestAddingABoundWarns(t *testing.T) {
	bounded := editOf(t, listFixtureSrc, "scores     []int32", "scores     [..4]int32")
	fs := listDiff(t, listFixtureSrc, bounded, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Warn, "Save.scores", "bound 4 added") {
		t.Errorf("an unbounded array given a bound does not warn: %s", summary(fs))
	}
	// ATTRIBUTION: drop the bound row and the edit goes quiet. The `array=`
	// row is a SHAPE row and a bounded/unbounded move is not a map move, so
	// it says nothing on its own — which is what leaves the whole verdict to
	// the bound's presence.
	if fs := listDiff(t, listFixtureSrc, bounded, without("bound")); len(fs) != 0 {
		t.Errorf("with the bound row dropped the edit still reports: %s", summary(fs))
	}
}

// TestRemovingABoundPasses: the direction that REMOVES one is the largest
// growth there is — no stored count can fail a bound that is gone — so it
// passes in silence (§2.9, §18.2).
func TestRemovingABoundPasses(t *testing.T) {
	bounded := editOf(t, listFixtureSrc, "scores     []int32", "scores     [..4]int32")
	if fs := listDiff(t, bounded, listFixtureSrc, baseline.DefaultTokenPolicy); len(fs) != 0 {
		t.Errorf("a bound REMOVED for []T is a capacity grown and passes: %s", summary(fs))
	}
	// DISCRIMINATION: the same field, the same direction of edit, at a
	// capacity the baseline does judge — a bound SHRUNK still warns, so the
	// silence above is the missing token and not a policy that stopped
	// looking at this field.
	shrunk := editOf(t, bounded, "scores     [..4]int32", "scores     [..2]int32")
	if fs := listDiff(t, bounded, shrunk, baseline.DefaultTokenPolicy); !find(fs, baseline.Warn, "Save.scores", "bound 4 -> 2") {
		t.Errorf("a bound shrunk between two bounded spellings still warns: %s", summary(fs))
	}
}

// TestTheElementIsJudgedUnmoved: `elem=`, `type=` and `kind=` are unmoved by
// the array's class, so an element retyped or moved to or from `[]*T` refuses
// under the shape that was there and under the shape that replaces it alike
// (§2.9, §18.1).
func TestTheElementIsJudgedUnmoved(t *testing.T) {
	retyped := editOf(t, listFixtureSrc, "scores     []int32", "scores     []float32")
	if fs := listDiff(t, listFixtureSrc, retyped, baseline.DefaultTokenPolicy); len(fs) == 0 {
		t.Errorf("an unbounded array's element retyped is not reported: %s", summary(fs))
	}
	pointered := editOf(t, listFixtureSrc, "placements []Placement", "placements []*Placement")
	if fs := listDiff(t, listFixtureSrc, pointered, baseline.DefaultTokenPolicy); len(fs) == 0 {
		t.Errorf("an unbounded array's element moved to []*T is not reported: %s", summary(fs))
	}
}
