package darttable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row, `cfloat_range_widen`, on the DART leg: three old-generation reads.
// A compressed float rides as the float32 itself (SPEC §3.4); the lineage plan
// carries the OLD writer's range [-1,1], and the NEW reader holds a value read
// under that entry to the WRITER's range (docs/rulings.json strict-only-read,
// FIXED-FORM-ALGORITHM §4.6): outside it the whole load refuses out_of_range,
// never a clamp. `old_` is aim 0.5, inside the writer's range: the control, it
// lands 0.5 exactly. `hostile_` forges aim to 1.5 — past the OLD writer's
// [-1,1], inside the NEW reader's [-2,2] — and refuses whole: only the writer's
// pass can make it refuse. `past_` forges aim to 5.0, past the reader's [-2,2]
// too, and refuses whole. A refusal lands nothing and moves no counter.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	reader := "VNEW_cfloat_range_widen"
	older := []string{"VOLD_cfloat_range_widen"}

	oldFile := filepath.Join(corpus, "old_cfloat_range_widen.bin")
	hostileFile := filepath.Join(corpus, "hostile_cfloat_range_widen.bin")
	pastFile := filepath.Join(corpus, "past_cfloat_range_widen.bin")

	// The control's number comes from the corpus manifest (`values=r0.aim`).
	oldAim := fmt.Sprintf("0x%08X", versionManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))

	bodyOld := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the old writer did not read one record: n=$n ${why(report)}');
  check(report.refused == 0,
      'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  final aimBits = ByteData(4)..setFloat32(0, values[0].aim, Endian.little);
  check(aimBits.getUint32(0, Endian.little) == @OLDAIM@,
      'aim is not the old writer 0.5: ${values[0].aim}');
  check(report.unknown == 0 && report.widened == 0,
      'counters moved on a clean backward read: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, reader, older, 0, oldFile, strings.ReplaceAll(bodyOld, "@OLDAIM@", oldAim))
	if err != nil {
		t.Fatalf("cfloat_range_widen old_: %v\n%s", err, out)
	}

	bodyRefuse := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1, '@TAG@: the forged file refuses whole: n=$n ${why(report)}');
  check(report.refused == t.TableFixedRefusal.outOfRange,
      '@TAG@: a value past the WRITER range refuses out_of_range: ${why(report)}');
  check(fresh(values[0]),
      '@TAG@: a refused read lands nothing, not even the bracketing fields: ${values[0].aim}');
  check(report.unknown == 0 && report.widened == 0,
      '@TAG@: counters moved on a refused read: ${why(report)}');`

	for _, c := range []struct{ tag, file string }{{"hostile_", hostileFile}, {"past_", pastFile}} {
		out, err = runVersionProbe(t, dartBin, reader, older, 0, c.file, strings.ReplaceAll(bodyRefuse, "@TAG@", c.tag))
		if err != nil {
			t.Fatalf("cfloat_range_widen %s: %v\n%s", c.tag, err, out)
		}
	}
}
