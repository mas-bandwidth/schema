package elixirtable

// `cfloat_res_refine` is the §5.8 row whose widening is the REFINEMENT of the
// compressed float's step: `aim float32 | min = -1, max = 1, resolution = 0.1`
// on VOLD and `resolution = 0.01` on VNEW. In the FIXED form the float rides as
// the float32 itself (SPEC §3.4), so this row takes NO hostile file — the step
// is a definition in the digest, not the bytes. `clamped` is asserted EXACTLY
// == 0: the old step 0.1 is a whole multiple of the reader's 0.01, so a reader
// that requantized would move the counter, which a `>= 1` would pass.
//
// `aim` IS COMPARED BY ITS float32 BITS, AGAINST THE CORPUS MANIFEST
// (schema#1164, Glenn 2026-09-19: "do whatever is needed to make sure that a
// new reader can read an old writer"). It was `v.aim == 0.30000001192092896` —
// exact, because the BEAM has one float type and that literal IS the f32 0.3
// widened, but exact BY ACCIDENT of a number transcribed by hand, with nothing
// saying so and nothing tying it to the reference. Rebuilding the value as a
// 32-bit float binary and reading the word back says it, and takes the number
// from the manifest instead of from this file.
import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "old_cfloat_res_refine.bin")
	aim := manifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")
	body := fmt.Sprintf(`  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader refused the older writer file: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
  <<aimbits::little-unsigned-32>> = <<v.aim::float-little-32>>
  check(aimbits == %[1]d,
    "the old value did not land BIT-EXACT: aim bits 0x#{Integer.to_string(aimbits, 16)}, the manifest says 0x#{Integer.to_string(%[1]d, 16)} (#{inspect(v.aim)})")
  check(report.clamped == 0,
    "clamped == #{report.clamped}, want 0: the refinement must not requantize")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clean NEW-READS-OLD is not malformed")`, aim)
	out, err := runVersionProbe(t, elixirBin, "VNEW_cfloat_res_refine",
		[]string{"VOLD_cfloat_res_refine"}, 0, file, body)
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
