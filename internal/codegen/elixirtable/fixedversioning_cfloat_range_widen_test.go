package elixirtable

// `cfloat_range_widen` is the fixed-form row whose WIDENING is the RANGE of the
// compressed float: `aim float32 | min = -1, max = 1, resolution = 0.01` on
// VOLD and `min = -2, max = 2` on VNEW. A compressed float is held to the
// WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json strict-only-read, docs/FIXED-FORM-ALGORITHM.md §4.6): a
// value past the writer's range refuses the whole load out_of_range. old_ is
// the control: the OLD writer's range is NARROWER than the reader's and its
// own 0.5 lands exactly. hostile_ (1.5: past the OLD writer's [-1, 1], inside
// the NEW reader's [-2, 2]) refuses whole, which is what says the bound is the
// writer's; past_ (5.0, past both) refuses whole too. Delete the emitter's
// writer pass for the float (fixedWriterFloatBoundsOf) and hostile_ goes red
// while past_ stays green on the reader's own bound.
import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	// `aim` IS COMPARED BY ITS float32 BITS, FROM THE CORPUS MANIFEST
	// (`values=r0.aim`), so the value that lands is tied to the reference and
	// not to a literal transcribed by hand.
	aimCheck := func(which string, bits uint32, note string) string {
		return fmt.Sprintf(`  <<aimbits::little-unsigned-32>> = <<v.aim::float-little-32>>
  check(aimbits == %[2]d, "%[1]s: aim bits 0x#{Integer.to_string(aimbits, 16)}, want 0x#{Integer.to_string(%[2]d, 16)} -- %[3]s (#{inspect(v.aim)})")`, which, bits, note)
	}
	oldAim := aimCheck("old_", manifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"), "the writer's own value, from the corpus manifest")

	oldFile := filepath.Join(corpus, "old_cfloat_range_widen.bin")
	hostileFile := filepath.Join(corpus, "hostile_cfloat_range_widen.bin")
	pastFile := filepath.Join(corpus, "past_cfloat_range_widen.bin")

	// old_ — the OLD writer's aim = 0.5, inside its own [-1, 1]: lands 0.5
	// exactly, nothing counted.
	oldBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader refused the older writer file: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
  @AIM@
  check(
    report.unknown == 0 and true and report.widened == 0 and
      true,
    "a counter that must not move did: #{why(report)}"
  )
  check(true, "a clean NEW-READS-OLD reads whole")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, oldFile, strings.ReplaceAll(oldBody, "@AIM@", oldAim))
	if err != nil {
		t.Fatalf("old_: %v\n%s", err, out)
	}

	// hostile_ (1.5, past the WRITER's [-1, 1]) and past_ (5.0, past the
	// reader's [-2, 2] too) — each refuses the whole load out_of_range and
	// builds no value.
	refuseBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :error,
    "a value past the writer's range must refuse: #{inspect(tag)} #{why(report)}")
  check(values == :out_of_range,
    "the refusal is out_of_range and built no value: #{inspect(values)}")
  check(report.unknown == 0 and report.widened == 0,
    "the refusal moved nothing: #{why(report)}")`
	out, err = runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, hostileFile, refuseBody)
	if err != nil {
		t.Fatalf("hostile_: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, pastFile, refuseBody)
	if err != nil {
		t.Fatalf("past_: %v\n%s", err, out)
	}
}
