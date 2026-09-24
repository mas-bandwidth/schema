package elixirtable

// `unknown_census`, §5.8 row 11 of docs/FIXED-FORM-VERSIONING-TESTS.md, on the
// elixir leg. `VOLD_/VNEW_unknown_census`: OLD `Item { a, drop }`, NEW the same
// with `Item.drop` REMOVED, inside `Census { lead, items [4]Item, trail }`. The
// pair is deliberately UNLAWFUL — §5.1 refuses a removal — so the lineage entry
// is HANDED IN by the probe and never read from a lock.
//
// `unknown` is asserted EXACTLY == 1: `Item.drop` is ONE field of ONE peer
// however many of the four elements carry it. A leg that counts once per ELEMENT
// lands 4, and that 4 is the whole reason this row exists. The census lands ONCE,
// after the record loop, on a read that RETURNS (§5.9 #6) — so a SECOND read of
// the same peer owes the same number again.
import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "old_unknown_census.bin")
	body := fmt.Sprintf(`  data = File.read!(%s)
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader did not read the older writer's record: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
  check(Enum.map(v.items, & &1.a) == [10, 11, 12, 13],
    "the four a values did not land: #{inspect(Enum.map(v.items, & &1.a))}")
  check(report.unknown == 1,
    "unknown == #{report.unknown}, want 1: Item.drop is ONE field of ONE peer, not one per element")
  check(report.kind_mismatch == 0 and report.widened == 0 and report.clamped == 0,
    "a counter that must not move did: #{why(report)}")
  check(report.malformed == false,
    "a clean NEW-READS-OLD is not damage: #{why(report)}")

  # THE SECOND READ OWES THE SAME CENSUS (§5.9 #6): the census is the plan's own
  # number and it lands on every read that returns, never once per run.
  {tag2, values2, report2} = load(data)
  check(tag2 == :ok and length(values2) == 1,
    "the second read did not return one record: #{inspect(tag2)} #{why(report2)}")
  check(report2.unknown == 1,
    "the second read owes the same census: unknown == #{report2.unknown}, want 1")`,
		elixirString(file))

	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census",
		[]string{"VOLD_unknown_census"}, 0, file, body)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
