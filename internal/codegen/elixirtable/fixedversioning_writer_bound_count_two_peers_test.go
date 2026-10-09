package elixirtable

// §5.8 row 4 — writer_bound_count with TWO lineage peers. The reader is
// VNEW_array_bounded_grow ([..8]int32). It holds TWO peers with DIFFERENT
// bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE the file, and
// VMID_array_bounded_grow ([..6]int32), the distractor, which did not. The
// count word is forged to 7 exactly as the landed single-peer test forges it.
// The number that makes this test worth having is 6: a reader that clamps to
// its own bound lands 8, a reader that clamps to whichever peer it saw last
// lands 6, and only a plan that carries the WRITING peer's bound lands 4.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	data, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatal(err)
	}

	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	if n := bytes.Count(data, needle); n != 1 {
		t.Fatalf("the locator %X occurs %d times, want exactly once", needle, n)
	}
	at := bytes.Index(data, needle)
	data[at+4] = 0x07 // the forge: the count word 4 -> 7, between the writer's 4 and the reader's 8

	forged := filepath.Join(t.TempDir(), "hostile_array_bounded_grow.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged OLD file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "the forged file reads one record, not #{length(values)}")
  v = hd(values)
  check(v.vals == [1000, 1001, 1002, 1003],
    "the writer's four elements landed and only four: #{inspect(v.vals)}")
  check(v.lead == 0xAAAAAAAA and v.trail == 0xBBBBBBBB,
    "the forged count moved a neighbour: #{inspect({v.lead, v.trail})}")
  check(report.clamped == 1,
    "the count clamped once, not #{report.clamped}: #{why(report)}")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a count forge is a clamp and nothing else: #{why(report)}"
  )
  check(report.malformed == false, "a clean clamped read is not malformed")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow", "VMID_array_bounded_grow"}, 0, forged, body)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
