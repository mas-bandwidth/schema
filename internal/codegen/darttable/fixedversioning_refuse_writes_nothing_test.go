package darttable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 9, `refuse_writes_nothing`, on the DART leg: the NEW build reads
// `old_nested_append.bin` with record 0's eight-byte per-record hash bitwise
// INVERTED and the header's hash at file+8 untouched, so the header still
// selects the OLD lineage entry — a COMPILED plan with a NONEMPTY fill list —
// and §5.3 step 11 refuses by name (`no_layout`) before the prefill can run.
// Every counter is asserted EXACTLY `== 0`, never `>= 1`: a refusal that is not
// total moves one, and a `>=` read passes the leak the row exists to catch.

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	body := `  final data = File(FILE).readAsBytesSync();
  final head = ByteData.sublistView(data);
  final layoutBytes = head.getUint32(t.TableFixedLimits.headerBytes, Endian.little);
  final at0 = t.TableFixedLimits.layoutAt + layoutBytes;
  check(data.length - at0 > 8, 'record 0 is too short to forge: ${data.length - at0}');
  head.setUint64(at0, ~head.getUint64(at0, Endian.little), Endian.little); // the forge
  plan.image.fillRange(0, plan.image.length, 0x5A);
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1, 'a record hash naming no layout refuses: n=$n ${why(report)}');
  check(name(report.refused) == 'no_layout',
      'a record hash naming no layout owes no_layout, not ${name(report.refused)}');
  check(!report.malformed, 'a refusal by name never sets malformed too (the joint answer)');
  check(report.clamped == 0 && report.unknown == 0 && report.kindMismatch == 0 &&
      report.widened == 0 && report.duplicate == 0,
      'REFUSE is total: no counter moves: ${why(report)}');
  for (var i = 0; i < plan.image.length; i++) {
    check(plan.image[i] == 0x5A, 'the prefill wrote into the poisoned image at $i: ${plan.image[i]}');
  }`

	out, err := runVersionProbe(t, dartBin, "VNEW_nested_append", []string{"VOLD_nested_append"}, 0,
		filepath.Join(corpus, "old_nested_append.bin"), body)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
