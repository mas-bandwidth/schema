package elixirtable

// `cfloat_range_widen` is the fixed-form row whose WIDENING is the RANGE of the
// compressed float: `aim float32 | min = -1, max = 1, resolution = 0.01` on
// VOLD and `min = -2, max = 2` on VNEW. In the fixed form the float rides as
// the float32 itself (SPEC §3.4), so the bound a read passes is the READER's
// and not the writer's: a value inside the new range lands WHOLE and UNCOUNTED,
// and only a value past the READER's own bound clamps and counts. `clamped` is
// asserted EXACTLY on every read (0, 0, 1), because this row exists to catch a
// reader that counts twice or counts the wrong read. The hostile_ read alone
// would not be worth having: with the emitter's float bounds pass deleted
// outright it STAYS GREEN and only past_ goes red — hostile_ landing 1.5 (not
// the writer's 1.0) and past_ landing 2.0 (not 5.0) is what says there is a
// pass at all.
import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	// `aim` IS COMPARED BY ITS float32 BITS, AND TWO OF THE THREE COME FROM THE
	// CORPUS MANIFEST (schema#1164). The BEAM has one float type and it is a
	// double, so `v.aim == 0.5` was exact -- but exact by accident of a literal
	// transcribed by hand, with nothing tying it to the reference. `old_`'s value
	// is the manifest's `values=r0.aim`, `hostile_`'s is the manifest's own
	// `forged=r0.aim@104`. `past_`'s 0x40000000 stays a literal and says why: the
	// file carries 5.0 and what lands is THIS READER'S OWN DECLARED MAX 2.0,
	// which is the whole content of that column.
	aimCheck := func(which string, bits uint32, note string) string {
		return fmt.Sprintf(`  <<aimbits::little-unsigned-32>> = <<v.aim::float-little-32>>
  check(aimbits == %[2]d, "%[1]s: aim bits 0x#{Integer.to_string(aimbits, 16)}, want 0x#{Integer.to_string(%[2]d, 16)} -- %[3]s (#{inspect(v.aim)})")`, which, bits, note)
	}
	oldAim := aimCheck("old_", manifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"), "the writer's own value, from the corpus manifest")
	hostileAim := aimCheck("hostile_", manifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"), "the manifest's own forged value, landed WHOLE because the bounds pass is the READER's")
	pastAim := aimCheck("past_", 0x40000000, "THIS READER'S OWN declared max 2.0, not a manifest value: the file carries 5.0")

	oldFile := filepath.Join(corpus, "old_cfloat_range_widen.bin")
	hostileFile := filepath.Join(corpus, "hostile_cfloat_range_widen.bin")
	pastFile := filepath.Join(corpus, "past_cfloat_range_widen.bin")

	// old_ — the OLD writer's aim = 0.5, on the old grid: lands 0.5 exactly,
	// clamp 0.
	oldBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the newer reader refused the older writer file: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
@AIM@
  check(report.clamped == 0, "clamped == #{report.clamped}, want 0: the old value is not clamped")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clean NEW-READS-OLD is not malformed")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, oldFile, strings.ReplaceAll(oldBody, "@AIM@", oldAim))
	if err != nil {
		t.Fatalf("old_: %v\n%s", err, out)
	}

	// hostile_ — the OLD file's bytes with aim forged to 1.5, outside the old
	// writer's [-1, 1] but INSIDE the new reader's [-2, 2]: lands 1.5 WHOLE,
	// clamp 0.
	hostileBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the hostile file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
@AIM@
  check(report.clamped == 0, "clamped == #{report.clamped}, want 0: inside the reader's range lands whole")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clean in-range read is not malformed")`
	out, err = runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, hostileFile, strings.ReplaceAll(hostileBody, "@AIM@", hostileAim))
	if err != nil {
		t.Fatalf("hostile_: %v\n%s", err, out)
	}

	// past_ — the same old bytes with aim forged to 5.0, outside the new
	// reader's [-2, 2] too: lands the READER's own max 2.0, clamp 1 exactly.
	pastBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the past file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  leadtrail(v, 1, 2)
@AIM@
  check(report.clamped == 1, "clamped == #{report.clamped}, want 1: past the reader's own max clamps once")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clean clamped read is not malformed")`
	out, err = runVersionProbe(t, elixirBin, "VNEW_cfloat_range_widen",
		[]string{"VOLD_cfloat_range_widen"}, 0, pastFile, strings.ReplaceAll(pastBody, "@AIM@", pastAim))
	if err != nil {
		t.Fatalf("past_: %v\n%s", err, out)
	}
}
