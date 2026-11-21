package luautable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.6, bill
// §12.5): a fixed read of a record written under an older lineage entry holds
// each ranged field to the WRITER's range for that entry, never the reader's,
// whose range is the wider one. The shared vectors forge a value past the OLD
// writer's range and inside the NEW reader's — a scalar, one element of a
// ranged fixed array, a uint64 above 2^63 compared unsigned, and a compressed
// float (docs/rulings.json strict-only-read) — and each refuses the whole
// load. Each row's clean older file still reads.
func TestLuauFixedLineageHoldsWriterRanges(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			body := `local report: any = {}
local values = LOAD("file", report)
check(values == nil,
	"a value outside the WRITER's range is damage: " .. why(report))
check(report.widened == 0 and report.unknown == 0 and report.kindMismatch == 0,
	"REFUSE is total: no counter moves: " .. why(report))
`
			luauVersionProbe(t, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				map[string][]byte{"file": luauCorpusFile(t, corpus, vec.File)}, body)
		})
	}
	// every row's clean older file reads exactly: a writer range carried
	// wrong (a compressed float's spelled as an integer 0..0, say) refuses it
	seen := map[string]bool{}
	for _, vec := range fixedvector.WriterRange {
		if seen[vec.Row] {
			continue
		}
		seen[vec.Row] = true
		t.Run("in_range_"+vec.Row, func(t *testing.T) {
			t.Parallel()
			reader := luauVersionUnit(t, "VNEW_"+vec.Row)
			root := luauVersionRoot(t, reader)
			body := `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and #values == 1, "the writer's own file reads: " .. why(report))
` + luauManifestChecks(t, corpus, "old_"+vec.Row+".bin", root)
			luauVersionProbe(t, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				map[string][]byte{"file": luauCorpusFile(t, corpus, "old_"+vec.Row+".bin")}, body)
		})
	}
}
