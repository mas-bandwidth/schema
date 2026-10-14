package darttable

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writer_bound_count, two lineage peers (§5.8 row 4). The reader is
// VNEW_array_bounded_grow ([..8]int32); it is handed TWO peers with DIFFERENT
// bounds — VOLD_array_bounded_grow ([..4]int32), which WROTE the file, and
// VMID_array_bounded_grow ([..6]int32), the distractor, which did not. The
// count word is forged to 7 exactly as the single-peer test forges it. The
// number that makes this test worth having is 6: a reader that clamps to its
// own bound lands 8, one that clamps to whichever peer it saw last lands 6, and
// only a plan that carries the WRITING peer's bound lands 4.

func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
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
	if bytes.Contains(data[at+1:], needle) {
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
      'the count is not the WRITER bound (4), never the distractor 6, '
      'never the reader 8, never the forge 7: ${values[0].valsCount}');
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
		[]string{"VOLD_array_bounded_grow", "VMID_array_bounded_grow"}, 0, forged, body)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
