package baseline_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// TestBitsNarrowedInsideOneBucketWarns: a `bits(N)` field shares its `kind`
// with every other N in the same 8/16/32/64 storage bucket, and with the plain
// unsigned integer of that width, so the kind token alone cannot see N
// narrowed inside the bucket. A reader refuses a stored value past 2^N - 1
// `out_of_range` (docs/SPEC-TABLES.md §4), so a narrowed N is a range
// tightened (§18.2): it warns, and a widened one passes.
func TestBitsNarrowedInsideOneBucketWarns(t *testing.T) {
	const tmpl = "package fixture\n\ntable T\n{\n    x %s\n}\n"
	cases := []struct {
		name, before, after string
		warn                bool
	}{
		{"bits(12) to bits(9)", "bits(12)", "bits(9)", true},
		{"uint16 to bits(9)", "uint16", "bits(9)", true},
		{"uint16 with a declared maximum past 511 to bits(9)", "uint16 | min = 0, max = 60000", "bits(9)", true},
		{"bits(9) to bits(12)", "bits(9)", "bits(12)", false},
		{"bits(9) to uint16", "bits(9)", "uint16", false},
		{"uint16 to bits(16)", "uint16", "bits(16)", false},
		{"uint16 with a declared maximum of 300 to bits(9)", "uint16 | min = 0, max = 300", "bits(9)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := fmt.Sprintf(tmpl, c.before)
			after := fmt.Sprintf(tmpl, c.after)
			got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy)
			refusals, warns := baseline.Split(got)
			if len(refusals) != 0 {
				t.Fatalf("%s must not refuse, got:%s", c.name, summary(got))
			}
			if c.warn {
				if len(warns) != 1 || !find(got, baseline.Warn, "T.x", "bit width") {
					t.Fatalf("%s must warn once, naming T.x and its bit width, got:%s", c.name, summary(got))
				}
				// the ATTRIBUTION control: with the bits row removed the
				// narrowing is silent again, so the warning came from that row
				if got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), without("bits")); find(got, baseline.Warn, "T.x", "bit width") {
					t.Errorf("with the bits rule removed the narrowing should not warn on its width, got:%s", summary(got))
				}
				return
			}
			if find(got, baseline.Warn, "T.x", "bit width") {
				t.Errorf("%s widens or keeps the domain and must not warn on its width, got:%s", c.name, summary(got))
			}
		})
	}
}
