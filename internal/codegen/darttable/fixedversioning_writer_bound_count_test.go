package darttable

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writer_bound_count is §5.8 row 4: the array_bounded_grow pair, [..4]int32
// on the writer and [..8]int32 on the reader, its old_...bin's count word
// forged from 4 to 7 — a value between the writer's 4 and the reader's 8. The
// NEW build reads the forged OLD bytes through its lineage, so the plan is
// COMPILED and the bound it carries is the WRITER's 4. `clamped` is asserted
// EXACTLY `== 1`, never `>= 1`: a leg that counts in the `count` op as well as
// the bounds pass lands 2 and passes a looser read the row exists to catch.

func TestFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	old := filepath.Join(corpus, "old_array_bounded_grow.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// The OLD record is lead 0xAAAAAAAA immediately followed by the count 4,
	// both little-endian uint32: an eight-byte needle.
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("the lead/count needle is not in %s", old)
	}
	if bytes.Index(data[at+1:], needle) >= 0 {
		t.Fatalf("the lead/count needle occurs more than once in %s", old)
	}
	binary.LittleEndian.PutUint32(data[at+4:at+8], 7) // the forge
	forged := filepath.Join(t.TempDir(), "hostile_array_bounded_grow.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the forged file carries one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged count is not a refusal and not malformed: ${why(report)}');
  check(values[0].lead == 0xAAAAAAAA && values[0].trail == 0xBBBBBBBB,
      'a forged count moved a neighbour: ${values[0].lead} ${values[0].trail}');
  check(values[0].valsCount == 4,
      'the count is not the WRITER bound (4): ${values[0].valsCount}');
  for (var i = 0; i < 4; i++) {
    check(values[0].vals[i] == 1000 + i,
        'slot $i is not the writer value: ${values[0].vals[i]}');
  }
  for (var i = 4; i < 8; i++) {
    check(values[0].vals[i] == 0,
        'slot $i past the writer bound is not the reader default: ${values[0].vals[i]}');
  }
  check(report.clamped == 1,
      'a forged count past the writer bound is clamped exactly once: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'a forged count moved a counter it should not: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, forged, body)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}
