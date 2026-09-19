package darttable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row, `cfloat_range_widen`, on the DART leg: three old-generation reads.
// `old_` is aim 0.5 (lands 0.5). `hostile_` forges aim to 1.5 — past the OLD
// writer's [-1,1], inside the NEW reader's [-2,2] — and must land 1.5 WHOLE,
// clamp 0. `past_` forges aim to 5.0, past the reader's [-2,2] too, and must
// clamp to the READER's 2.0, clamp 1. `clamped` is asserted EXACTLY, never
// `>= 1`, to catch a reader counting twice or the wrong read. `hostile_` alone
// proves nothing: only `past_` landing 2.0, not 5.0, proves the pass runs.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	reader := "VNEW_cfloat_range_widen"
	older := []string{"VOLD_cfloat_range_widen"}

	oldFile := filepath.Join(corpus, "old_cfloat_range_widen.bin")
	hostileFile := filepath.Join(corpus, "hostile_cfloat_range_widen.bin")
	pastFile := filepath.Join(corpus, "past_cfloat_range_widen.bin")

	// TWO OF THE THREE NUMBERS COME FROM THE CORPUS MANIFEST AND THE THIRD
	// CANNOT (schema#1164). `old_`'s is `values=r0.aim`, `hostile_`'s is the
	// manifest's own `forged=r0.aim@104`. `past_`'s 0x40000000 stays a literal:
	// the file carries 5.0 and what lands is THIS READER'S OWN DECLARED MAX 2.0,
	// which is the whole content of that column.
	oldAim := fmt.Sprintf("0x%08X", versionManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))
	hostileAim := fmt.Sprintf("0x%08X", versionManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"))

	bodyOld := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the old writer did not read one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  final aimBits = ByteData(4)..setFloat32(0, values[0].aim, Endian.little);
  check(aimBits.getUint32(0, Endian.little) == @OLDAIM@,
      'aim is not the old writer 0.5: ${values[0].aim}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.clamped == 0 && report.duplicate == 0,
      'counters moved on a clean backward read: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, reader, older, 0, oldFile, strings.ReplaceAll(bodyOld, "@OLDAIM@", oldAim))
	if err != nil {
		t.Fatalf("cfloat_range_widen old_: %v\n%s", err, out)
	}

	bodyHostile := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the hostile file carried one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged aim is not a refusal and not malformed: ${why(report)}');
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  final aimBits = ByteData(4)..setFloat32(0, values[0].aim, Endian.little);
  check(aimBits.getUint32(0, Endian.little) == @HOSTILEAIM@,
      'aim 1.5 inside the reader range must land whole, not clamp: ${values[0].aim}');
  check(report.clamped == 0,
      'a value inside the reader range is clamped exactly zero times: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'counters moved on a hostile read: ${why(report)}');`

	out, err = runVersionProbe(t, dartBin, reader, older, 0, hostileFile, strings.ReplaceAll(bodyHostile, "@HOSTILEAIM@", hostileAim))
	if err != nil {
		t.Fatalf("cfloat_range_widen hostile_: %v\n%s", err, out)
	}

	bodyPast := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the past file carried one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged aim is not a refusal and not malformed: ${why(report)}');
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  final aimBits = ByteData(4)..setFloat32(0, values[0].aim, Endian.little);
  check(aimBits.getUint32(0, Endian.little) == 0x40000000,
      'aim 5.0 past the reader max must clamp to the reader 2.0: ${values[0].aim}');
  check(report.clamped == 1,
      'a value past the reader bound is clamped exactly once: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'counters moved on a past read: ${why(report)}');`

	out, err = runVersionProbe(t, dartBin, reader, older, 0, pastFile, bodyPast)
	if err != nil {
		t.Fatalf("cfloat_range_widen past_: %v\n%s", err, out)
	}
}
