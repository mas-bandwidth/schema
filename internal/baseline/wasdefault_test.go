package baseline_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/baseline"
)

const wasDefaultSrc = `package fixture

enum Grade
{
    Bronze
    Silver
    Gold
}

table Player
{
    grade Grade = Silver
}
`

// TestDefaultVariantRenamedUnderWasPasses: a table enum value rides as its
// variant's wire id, the hash of the `was` name after a rename
// (docs/SPEC-TABLES.md §5), so renaming the DEFAULT variant under `was` keeps
// what an elided field means. The baseline judges the default by the variant
// it names, not by its spelling: the rename passes, including a second rename
// against a baseline written after the first, while a default moved to
// another variant still refuses.
func TestDefaultVariantRenamedUnderWasPasses(t *testing.T) {
	renamed := editOf(t, editOf(t, wasDefaultSrc, "    Silver\n", "    Argent | was = \"Silver\"\n"), "Grade = Silver", "Grade = Argent")
	again := editOf(t, editOf(t, wasDefaultSrc, "    Silver\n", "    Plata | was = \"Silver\"\n"), "Grade = Silver", "Grade = Plata")
	for _, c := range []struct{ name, before, after string }{
		{"Silver renamed Argent under was", wasDefaultSrc, renamed},
		{"Argent renamed Plata under the same was, against a baseline written after the first rename", renamed, again},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := baseline.Diff(committed(t, c.before), baseline.Render(unit(t, c.after)), baseline.DefaultTokenPolicy)
			if refusals, _ := baseline.Split(got); len(refusals) != 0 {
				t.Fatalf("a default variant renamed under was keeps its meaning and must not refuse, got:%s", summary(got))
			}
		})
	}
	// the DISCRIMINATION control: the default moved to another variant is a
	// new meaning for every elided field, and refuses
	moved := editOf(t, wasDefaultSrc, "Grade = Silver", "Grade = Gold")
	got := baseline.Diff(committed(t, wasDefaultSrc), baseline.Render(unit(t, moved)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "Player.grade", "specified default Silver -> Gold") {
		t.Errorf("a default moved Silver -> Gold must refuse, got:%s", summary(got))
	}
	// and a bare rename, which moves the wire id, still refuses the default
	bare := editOf(t, editOf(t, wasDefaultSrc, "    Silver\n", "    Argent\n"), "Grade = Silver", "Grade = Argent")
	got = baseline.Diff(committed(t, wasDefaultSrc), baseline.Render(unit(t, bare)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "Player.grade", "specified default Silver -> Argent") {
		t.Errorf("a bare rename of the default variant must refuse the default, got:%s", summary(got))
	}
}
