package elixirtable

// `forged_ordinal_both_plans` is §5.8 row 12 of docs/FIXED-FORM-VERSIONING-TESTS.md:
// the `VOLD_/VNEW_enum_append` pair — `Tier { Bronze, Silver, Gold }` → `+ Platinum` —
// with `old_enum_append.bin` forged so `r0.tier` is `4`, an ordinal past the writer's
// three variants but a name the reader has. The SAME bytes are read TWICE, once by the
// NEW build's COMPILED plan and once by the OLD build's IDENTITY plan. Both refuse
// `out_of_range` and land nothing (docs/FIXED-FORM-ALGORITHM.md §4.6): the bound is
// the writer's variant count, and a refusal moves no counter.
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
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("tier=3 followed by seq=9 occurs more than once in %s", old)
	}
	data[at] = 4 // the forge
	forged := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  data = File.read!(file())
  {tag, reason, report} = load(data)
  check(tag == :error and reason == :out_of_range,
    "a value past the writer's set refuses the batch (§4.6): #{inspect({tag, reason})}")
  check(report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, forged, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged ordinal: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VOLD_enum_append", nil, 0, forged, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged ordinal: %v\n%s", err, out)
	}
}
