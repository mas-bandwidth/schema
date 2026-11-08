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
  check(n == -1, 'a count past the writer bound refuses whole: n=$n ${why(report)}');
  check(report.refused == t.TableFixedRefusal.countOverBound,
      'a forged count past the writer bound refuses count_over_bound: ${why(report)}');
  check(fresh(values[0]),
      'a refused count lands nothing, not even a neighbour: ${values[0].valsCount}');
  check(report.unknown == 0 && report.widened == 0,
      'a forged count moved a counter it should not: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, forged, body)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}
