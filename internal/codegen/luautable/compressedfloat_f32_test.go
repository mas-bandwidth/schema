package luautable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauCompressedFloatF32Range pins two cross-leg rules on the Luau table
// runtime (review 2026-11-21): a compressed float's declared range is
// compared at float32 (docs/SPEC.md §4.6: every runtime evaluates the triple
// at float32), so 0.1f under max = 0.1 is written and read in every form, a
// double that rounds to 0.1f is held as 0.1f, and a value past the float32
// end is still refused; and a compressed float holding -0 is +0
// (docs/SPEC-TABLES.md §3.3, ruling fixed-compressed-negzero). The schema,
// the probe and every case are the committed corpus
// testdata/review1121/compressed-float32.
func TestLuauCompressedFloatF32Range(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "compressed-float32")
	rows := review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 4)
	held, refused := 0, 0
	for _, r := range rows {
		if strings.HasPrefix(r[3], "refused") {
			refused++
		} else {
			held++
		}
	}
	if held == 0 || refused == 0 {
		t.Fatalf("the corpus holds %d held and %d refused rows: each is proven beside the other", held, refused)
	}
	u := review1121Unit(t, filepath.Join(corpus, "Probe.schema"))
	dir := review1121Modules(t, u, filepath.Join(corpus, "probe.luau"))
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(u),
		"cases":    review1121CaseLines(rows),
	})
	got := review1121Verdicts(t, raw)
	for _, r := range rows {
		if got[r[0]] != r[3] {
			t.Errorf("%s (%s %s): got %q, want %q", r[0], r[1], r[2], got[r[0]], r[3])
		}
	}
}
