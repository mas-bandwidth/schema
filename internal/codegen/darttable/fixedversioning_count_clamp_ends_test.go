package darttable

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// dart/C1 and dart/C2 are the two ends of the count clamp in the array-bounds
// work set: "count clamp v<0" and "count clamp v>Max", quoted verbatim from
// docs/roadmap.sexp. Row 4, writer_bound_count, forges this same count word
// from 4 to 7 — neither negative nor past the reader's own max of 8 — so it
// touches neither end. On 2026-09-19 the negative arm was deleted from this
// leg's emitted runtime and not one of the 68 landed fixed-table tests went
// red, and the same deletion is 0-red on c, cs, elixir and js: it is guarded
// by nothing. clamped is asserted EXACTLY == 1, never >= 1, because a leg that
// counts in the count op and again in the bounds pass lands 2. C2's valsCount
// == 4 is the WRITER's bound carried by the plan, never the reader's own 8.

func TestFixedVersioningCountClampEnds(t *testing.T) {
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

	// C1 — the count forged to -1 (0xFFFFFFFF read as the wire's little-endian
	// int32), which clamps to zero.
	neg := make([]byte, len(data))
	copy(neg, data)
	binary.LittleEndian.PutUint32(neg[at+4:at+8], 0xFFFFFFFF)
	forgedNeg := filepath.Join(t.TempDir(), "negative_array_bounded_grow.bin")
	if err := os.WriteFile(forgedNeg, neg, 0o600); err != nil {
		t.Fatal(err)
	}

	negBody := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the forged file carries one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged count is not a refusal and not malformed: ${why(report)}');
  check(values[0].lead == 0xAAAAAAAA && values[0].trail == 0xBBBBBBBB,
      'a forged count moved a neighbour: ${values[0].lead} ${values[0].trail}');
  check(values[0].valsCount == 0,
      'the negative count does not clamp to zero: ${values[0].valsCount}');
  check(report.clamped == 1,
      'a negative count is clamped exactly once: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'a negative count moved a counter it should not: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, forgedNeg, negBody)
	if err != nil {
		t.Fatalf("count clamp v<0: %v\n%s", err, out)
	}

	// C2 — the count forged to 9, past the writer's 4 and past the reader's 8.
	over := make([]byte, len(data))
	copy(over, data)
	binary.LittleEndian.PutUint32(over[at+4:at+8], 9)
	forgedOver := filepath.Join(t.TempDir(), "over_array_bounded_grow.bin")
	if err := os.WriteFile(forgedOver, over, 0o600); err != nil {
		t.Fatal(err)
	}

	overBody := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the forged file carries one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged count is not a refusal and not malformed: ${why(report)}');
  check(values[0].lead == 0xAAAAAAAA && values[0].trail == 0xBBBBBBBB,
      'a forged count moved a neighbour: ${values[0].lead} ${values[0].trail}');
  check(values[0].valsCount == 4,
      'the count is not the WRITER bound (4), never the reader 8, never the forge 9: ${values[0].valsCount}');
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

	out, err = runVersionProbe(t, dartBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, forgedOver, overBody)
	if err != nil {
		t.Fatalf("count clamp v>Max: %v\n%s", err, out)
	}
}
