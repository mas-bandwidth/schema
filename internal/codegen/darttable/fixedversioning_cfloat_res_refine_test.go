package darttable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row, `cfloat_res_refine`, on the DART leg: the NEW build reads
// `old_cfloat_res_refine.bin`, where the step refined 0.1 -> 0.01. This row
// takes NO forged file — a value off the old grid is still a float32 the reader
// lands, and the law's refusal for a moved step is the LOCK's and the HASH's.
// `report.clamped` is asserted EXACTLY `== 0`, with every other counter `0`: the
// refinement lives in the digest ('Q', bill §13), so the hash moves and no read
// counter does; `>= 1` would pass a leg that requantized the old value.
func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	// THE NUMBER COMES FROM THE CORPUS MANIFEST, not from a literal here
	// (schema#1164). This leg already compared BITS, which is the right
	// comparison; what it did not do is take the number from the REFERENCE.
	aim := fmt.Sprintf("0x%08X", versionManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim"))
	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the newer reader did not read the older writer one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(
    report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
        report.clamped == 0 && report.duplicate == 0,
    'counters moved on a clean backward read: ${why(report)}',
  );
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  final aimBits = ByteData(4)..setFloat32(0, values[0].aim, Endian.little);
  check(aimBits.getUint32(0, Endian.little) == @AIM@,
      'aim is not the corpus manifest value @AIM@, bit for bit: ${values[0].aim}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_cfloat_res_refine", []string{"VOLD_cfloat_res_refine"}, 0,
		filepath.Join(corpus, "old_cfloat_res_refine.bin"), strings.ReplaceAll(body, "@AIM@", aim))
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
