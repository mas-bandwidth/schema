package elixirtable

// `unknown_census`, §5.8 row 11 of docs/FIXED-FORM-VERSIONING-TESTS.md, on the
// elixir leg. `VOLD_/VNEW_unknown_census`: OLD `Item { a, drop }`, NEW the same
// with `Item.drop` REMOVED, inside `Census { lead, items [4]Item, trail }`. The
// pair is deliberately UNLAWFUL — §5.1 refuses a removal — so the lineage entry
// is HANDED IN by the probe and never read from a lock.
//
// THE ENTRY IS UNLAWFUL AND IS REFUSED, NEVER COUNTED (docs/FIXED-FORM-ALGORITHM.md
// §5.4, §5.9 #30, §5.9 row 9 of the port order; the Go reference's
// TestFixedVersioningUnknownCensus): COMPILE lays no plan for a writer field no
// reader field names, so the load refuses layout_malformed before any record
// lands, moves no counter, and a SECOND read refuses the same way. This row
// once asserted the retired census, unknown == 1.
import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "old_unknown_census.bin")
	body := fmt.Sprintf(`  data = File.read!(%s)
  {tag, why, report} = load(data)
  check(tag == :error and why == :layout_malformed,
    "the unlawful lineage entry was not refused layout_malformed: #{inspect({tag, why})} #{inspect(report)}")
  check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{inspect(report)}")

  # THE SECOND READ IS REFUSED THE SAME WAY: the lane's refusal is the build's,
  # laid down once, never a per-run accident.
  {tag2, why2, _report2} = load(data)
  check(tag2 == :error and why2 == :layout_malformed,
    "the second read was not refused layout_malformed: #{inspect({tag2, why2})}")`,
		elixirString(file))

	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census",
		[]string{"VOLD_unknown_census"}, 0, file, body)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
