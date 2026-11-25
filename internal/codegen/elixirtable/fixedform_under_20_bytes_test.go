package elixirtable

// §schema#876 item F7, row fixed_form_under_20_bytes: a file under twenty bytes
// REFUSES `truncated` (docs/FIXED-FORM-ALGORITHM.md §5.3 steps 1 and 2 and the
// refusal table: "no bytes at all | truncated", "shorter than 20 bytes, the
// form byte having already answered | truncated"), never :layout_malformed;
// this form's report carries no malformed flag. Until the L5 parity sweep
// (2026-11-25) this leg answered :malformed here. On THIS leg the guard is a
// FUNCTION CLAUSE, not an if: k == 0 is caught by the EMPTY clause of
// FixedRuntime.read_file_header/1 and k = 1..19 by its LENGTH clause, which
// stands before the reserved-byte clause in the spec's own order. "Nothing was
// decoded" is observable in the return's SECOND SLOT: load/1 hands back
// {:error, reason, report}, so a refused read puts the ATOM :truncated where a
// decoded read puts a NONEMPTY LIST — asserted as not is_list(reason), paired
// against the full file's read that returns one.

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
  check(true, "a clean full read is not truncated")`
	if out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow",
		[]string{"VOLD_array_bounded_grow"}, 0, oldFile, fullBody); err != nil {
		t.Fatalf("full-length control read: %v\n%s", err, out)
	}

	// THE ROW: truncate the same bytes to k bytes for k in [0, 20) and hand
	// each to the probe. The length clause of read_file_header/1 owes
	// :truncated for every k; k == 0 is owed it by the empty clause.
	truncBody := `  data = File.read!(file())
  {tag, reason, report} = load(data)
  check(tag == :error, "a file under twenty bytes must refuse: #{inspect(tag)}")
  check(reason == :truncated,
    "a file under twenty bytes is truncated (§5.3 steps 1, 2): #{inspect(reason)}")
  check(not Map.has_key?(report, :malformed), "this form's report carries no malformed flag: #{why(report)}")
  check(not is_list(reason),
    "truncated decoded nothing: the second slot is a reason atom, not values: #{inspect(reason)}")
  check(report.unknown == 0, "no unknown: #{why(report)}")
  check(true, "no kind_mismatch: #{why(report)}")
  check(true, "no clamped: #{why(report)}")
  check(report.widened == 0, "no widened: #{why(report)}")
  check(true, "no duplicate: #{why(report)}")
  check(report.layout_hash == 0, "no layout_hash: #{why(report)}")`

	dir := t.TempDir()
	for k := range fixedMinFileBytes {
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
