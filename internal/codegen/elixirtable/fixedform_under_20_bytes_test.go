package elixirtable

// §schema#876 item F7, row fixed_form_under_20_bytes: a file under twenty bytes
// is malformed, not a refusal by name. F7 was a GAP on all nine legs — its
// matrix row is GGGGGGGGG — and five of the nine have since landed (go, c,
// dart, js, cpp). A refusal by name (the seven §1.1 names, :layout_malformed
// among them) and :malformed are TWO DIFFERENT answers and are NEVER both set:
// a short file is the RESIDUE of a truncation, so it owes :malformed and not
// :layout_malformed. On THIS leg the guard is a FUNCTION CLAUSE, not an if, so
// the row is a clause-order finding: k == 0 is caught by the EMPTY clause at
// fixedruntime.go:216, and k = 1..19 by the LENGTH clause at fixedruntime.go:230
// (the reserved-byte clause at 227 stands before it but a real corpus
// truncation carries zero reserved bytes, and under @file_hash_at bytes it
// cannot match 227's pattern at all). "Nothing was decoded" is observable in
// the return's SECOND SLOT: load/1 hands back {:error, reason, report}, so a
// malformed read puts the ATOM :malformed where a decoded read puts a NONEMPTY
// LIST — asserted as not is_list(reason), paired against the full file's read
// that returns one. CONTROL 2 disables the length clause so the file falls
// through to :layout_malformed — the c/js/cpp SWAPPED shape — and the row goes
// red at k=1.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The twenty the doc names is @file_header_bytes (16, fixedruntime.go: the form
// byte, seven reserved zeros and the eight-byte hash) plus the layout's own
// little-unsigned-32 length (the next 4): never a bare 20.
const (
	fixedFileHeaderBytes = 16
	fixedFileLenBytes    = 4
	fixedMinFileBytes    = fixedFileHeaderBytes + fixedFileLenBytes
)

func TestFixedFormUnder20Bytes(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	data, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < fixedMinFileBytes {
		t.Fatalf("the corpus fixture %s is only %d bytes, shorter than the %d-byte header",
			oldFile, len(data), fixedMinFileBytes)
	}

	// THE FULL FILE READS CLEANLY FIRST, so a broken fixture cannot pass the
	// row by accident: the same bytes, untruncated, must decode.
	fullBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the FULL corpus file refused: #{inspect(tag)} #{why(report)}")
  check(is_list(values) and values != [],
    "the full file decoded no value, so the truncations below withhold nothing: #{inspect(values)}")
  check(report.malformed == false, "a clean full read is not malformed")`
	if out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, oldFile, fullBody); err != nil {
		t.Fatalf("full-length control read: %v\n%s", err, out)
	}

	// THE ROW: truncate the same bytes to k bytes for k in [0, 20) and hand
	// each to the probe. The length clause at fixedruntime.go:230 owes
	// :malformed for every k; k == 0 is owed it by the empty clause at 216.
	truncBody := `  data = File.read!(file())
  {tag, reason, report} = load(data)
  check(tag == :error, "a file under twenty bytes must refuse: #{inspect(tag)}")
  check(reason == :malformed,
    "a truncation is malformed, never a named refusal: #{inspect(reason)}")
  check(not is_list(reason),
    "malformed decoded nothing: the second slot is a reason atom, not values: #{inspect(reason)}")
  check(report.malformed == true, "a malformed read sets malformed: #{why(report)}")
  check(report.unknown == 0, "no unknown: #{why(report)}")
  check(report.kind_mismatch == 0, "no kind_mismatch: #{why(report)}")
  check(report.clamped == 0, "no clamped: #{why(report)}")
  check(report.widened == 0, "no widened: #{why(report)}")
  check(report.duplicate == 0, "no duplicate: #{why(report)}")
  check(report.layout_hash == 0, "no layout_hash: #{why(report)}")`

	dir := t.TempDir()
	for k := 0; k < fixedMinFileBytes; k++ {
		truncFile := filepath.Join(dir, fmt.Sprintf("under_20_%d.bin", k))
		if err := os.WriteFile(truncFile, data[:k], 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow",
			[]string{"VOLD_array_bounded_grow"}, 0, truncFile, truncBody); err != nil {
			t.Fatalf("k=%d under twenty bytes: %v\n%s", k, err, out)
		}
	}
}
