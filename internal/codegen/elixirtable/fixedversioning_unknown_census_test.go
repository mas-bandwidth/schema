package elixirtable

// §5.8 row 11, `unknown_census`: the NEW build reads `old_unknown_census.bin`
// through a HANDED-IN lineage entry, because the pair is deliberately UNLAWFUL
// (a removed `Item.drop`, refused by §5.1) and so has no lock to read it from.
// The bytes are lawful: one Census record, four Item elements, every `a`
// (10..13) and every `drop` (900..903) set. `report.unknown` is asserted
// EXACTLY `== 1`: `Item.drop` is ONE field of ONE peer however many of the four
// elements carry it, and `>= 1` would pass a read that miscounted per element
// as `4`, which is the divergence this row exists to catch.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	body := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader refused the older writer file: #{inspect(tag)} #{why(report)}")
  check(report.malformed == false, "a clean NEW-READS-OLD is not malformed")
  check(report.unknown == 1, "unknown == #{report.unknown}, want 1: Item.drop is ONE field of ONE peer, not one per element")
  check(report.kind_mismatch == 0, "kind_mismatch moved: #{report.kind_mismatch}")
  check(report.widened == 0, "widened moved: #{report.widened}")
  check(report.clamped == 0, "clamped moved: #{report.clamped}")
  check(length(values) == 1, "the file carries one record, not #{length(values)}")
  v = hd(values)
  check(v.lead == 1 and v.trail == 2, "lead/trail did not stand: #{inspect({v.lead, v.trail})}")
  Enum.zip(v.items, [10, 11, 12, 13])
  |> Enum.with_index()
  |> Enum.each(fn {{e, w}, k} ->
    check(e.a == w, "items[#{k}].a landed #{inspect(e.a)}, want #{w}")
  end)
  # The census lands ONCE, after the record loop (§5.9 #6); a SECOND read of
  # the same peer reports unknown == 1 again.
  {tag2, _values2, report2} = load(data)
  check(tag2 == :ok and report2.unknown == 1,
    "a second read of the same peer reports unknown == 1 again: #{inspect(report2.unknown)}")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		filepath.Join(corpus, "old_unknown_census.bin"), body)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
