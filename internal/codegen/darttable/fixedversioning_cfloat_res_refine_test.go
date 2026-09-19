package darttable

import (
	"path/filepath"
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
  check(aimBits.getUint32(0, Endian.little) == 0x3E99999A,
      'aim is not the float32 the old writer quantized to 0.1: ${values[0].aim}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_cfloat_res_refine", []string{"VOLD_cfloat_res_refine"}, 0,
		filepath.Join(corpus, "old_cfloat_res_refine.bin"), body)
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
