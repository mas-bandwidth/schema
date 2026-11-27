package elixirtable

// `unknown_census`, the many-record file (§5.8 row 11): THREE records under the
// DELIBERATELY UNLAWFUL entry (Item.drop removed). The entry is refused, never
// counted (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30; the Go reference's
// TestFixedVersioningUnknownCensusManyRecords): the whole load refuses
// layout_malformed before any of the three records lands, and no counter moves.
// This row once asserted the retired census, unknown == 1 across the file.
import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "many_unknown_census.bin")
	body := fmt.Sprintf(`  data = File.read!(%s)
  {tag, why, report} = load(data)
  check(tag == :error and why == :layout_malformed,
    "the unlawful lineage entry was not refused layout_malformed: #{inspect({tag, why})} #{inspect(report)}")
  check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{inspect(report)}")`,
		elixirString(file))

	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census",
		[]string{"VOLD_unknown_census"}, 0, file, body)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
