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
// ranged fixed array, and a uint64 above 2^63 compared unsigned — and each
// refuses the whole load. A clean older file still reads.
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
	t.Run("in_range", func(t *testing.T) {
		t.Parallel()
		reader := luauVersionUnit(t, "VNEW_range_widen")
		root := luauVersionRoot(t, reader)
		body := `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and #values == 1, "the writer's own file reads: " .. why(report))
` + luauManifestChecks(t, corpus, "old_range_widen.bin", root)
		luauVersionProbe(t, "VNEW_range_widen", []string{"VOLD_range_widen"}, 0,
			map[string][]byte{"file": luauCorpusFile(t, corpus, "old_range_widen.bin")}, body)
	})
}
