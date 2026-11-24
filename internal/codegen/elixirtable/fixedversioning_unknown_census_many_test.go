package elixirtable

// `unknown_census`, the many-record file (§5.8 row 11): THREE records under the
// DELIBERATELY UNLAWFUL entry (Item.drop removed). The entry is refused, never
// counted (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30; the Go reference's
// TestFixedVersioningUnknownCensusManyRecords): the whole load refuses
// layout_malformed before any of the three records lands, and no counter moves.
// This row once asserted the retired census, unknown == 1 across the file.
import (
	"fmt"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	t.Parallel()
	// A MISSING CORPUS OR ELIXIR FAILS BY NAME whenever SCHEMA_SLOW,
	// SCHEMA_REQUIRE_CORPUS or CI promised this row; it never skips quietly.
	file := lineageCorpus(t, "many_unknown_census.bin")
	elixirBin := lineageElixir(t)
	body := fmt.Sprintf(`  data = File.read!(%s)
  {tag, why, report} = load(data)
  check(tag == :error and why == :layout_malformed,
    "the unlawful lineage entry was not refused layout_malformed: #{inspect({tag, why})} #{inspect(report)}")
  check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{inspect(report)}")
  # NO VALUES RIDE A REFUSAL, and the reader's declared defaults stand
  # (SPEC-TABLES §6.5): Elixir hands back a fresh list and never writes caller
  # rows, so the rows a refused load leaves the caller are the declared ones.
  check(not is_list(why), "a refusal handed back values: #{inspect(why)}")
  {d, _c, _m} = T.census_fixed_decode(prefill(), 0, false)
  check(d.lead == 1 and d.trail == 2 and Enum.all?(d.items, &(&1.a == 0)),
    "the reader's declared defaults moved: #{inspect(d)}")

  # THE SECOND READ IS REFUSED THE SAME WAY.
  {tag2, why2, _report2} = load(data)
  check(tag2 == :error and why2 == :layout_malformed,
    "the second read was not refused layout_malformed: #{inspect({tag2, why2})}")`,
		elixirString(file))

	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census",
		[]string{"VOLD_unknown_census"}, 0, file, body)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
