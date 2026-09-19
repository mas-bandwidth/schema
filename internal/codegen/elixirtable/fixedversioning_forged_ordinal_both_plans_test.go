package elixirtable

// `forged_ordinal_both_plans` is §5.8 row 12 of docs/FIXED-FORM-VERSIONING-TESTS.md:
// the `VOLD_/VNEW_enum_append` pair — `Tier { Bronze, Silver, Gold }` → `+ Platinum` —
// with `old_enum_append.bin` forged so `r0.tier` is `4`, an ordinal past the writer's
// three variants but a name the reader has. The SAME bytes are read TWICE, once by the
// NEW build's COMPILED plan and once by the OLD build's IDENTITY plan. The counter
// asserted EXACTLY is `clamped == 1` on BOTH, never `>= 1`: a leg that also counts in
// the `ordinal` op lands 2 and passes the double-count the row exists to catch.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	old := filepath.Join(corpus, "old_enum_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// THE FORGE: locate `r0.tier` by its declared value 3 and its neighbour
	// `r0.seq = 9` (the manifest's lawful values for the writer), assert the
	// needle occurs exactly once, and overwrite the tier byte with 4.
	needle := []byte{3, 9, 0, 0, 0}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("tier=3 followed by seq=9 is not in %s", old)
	}
	if bytes.Index(data[at+1:], needle) >= 0 {
		t.Fatalf("tier=3 followed by seq=9 occurs more than once in %s", old)
	}
	data[at] = 4 // the forge
	forged := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged ordinal is not a refusal: #{inspect({tag, why(report)})}")
  check(report.malformed == false, "a forged ordinal is not malformed: #{why(report)}")
  check(length(values) == 1, "the forged file carries one record, not #{length(values)}")
  v = hd(values)
  check(v.tier == 0, "a forged ordinal lands None: #{inspect(v.tier)}")
  check(v.tier != 4, "a forged ordinal never lands Platinum: #{inspect(v.tier)}")
  check(v.seq == 9, "the scalar after the enum is untouched: #{inspect(v.seq)}")
  check(report.clamped == 1, "the forged ordinal is counted exactly once: #{why(report)}")
  check(report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and report.duplicate == 0,
    "no other counter moved: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, forged, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged ordinal: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VOLD_enum_append", nil, 0, forged, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged ordinal: %v\n%s", err, out)
	}
}
