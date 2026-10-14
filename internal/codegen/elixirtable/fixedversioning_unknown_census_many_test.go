package elixirtable

// `unknown_census`, the many-record half of §5.8 row 11's clause (§5.4): the
// compile census lands ONCE PER PEER AND NEVER PER RECORD. The landed one-record
// row reads `old_unknown_census.bin` — ONE record — where "once per peer" and
// "once per record" are the SAME number, 1, so the second half has never been
// under a gate. This file carries THREE records; a reader that counts per record
// lands `unknown == 3` here and `1` on the landed row, so only this file sees
// the difference. `unknown` is asserted EXACTLY == 1: `Item.drop` is ONE field
// of ONE peer, never one per record.
import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "many_unknown_census.bin")
	body := fmt.Sprintf(`  data = File.read!(%s)
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader did not read the older writer's records: #{inspect(tag)} #{why(report)}")
  check(length(values) == 3, "n == #{length(values)}, want 3")
  want = [[10, 11, 12, 13], [20, 21, 22, 23], [30, 31, 32, 33]]
  Enum.zip(values, want)
  |> Enum.with_index()
  |> Enum.each(fn {{v, w}, k} ->
    leadtrail(v, 1, 2)
    check(Enum.map(v.items, & &1.a) == w,
      "record #{k} the four a values did not land: #{inspect(Enum.map(v.items, & &1.a))}")
  end)
  check(report.unknown == 1,
    "unknown == #{report.unknown}, want 1: Item.drop is ONE field of ONE peer, not one per record")
  check(report.kind_mismatch == 0 and report.widened == 0 and report.clamped == 0,
    "a counter that must not move did: #{why(report)}")
  check(report.malformed == false,
    "a clean NEW-READS-OLD is not damage: #{why(report)}")`,
		elixirString(file))

	out, err := runVersionProbe(t, elixirBin, "VNEW_unknown_census",
		[]string{"VOLD_unknown_census"}, 0, file, body)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
