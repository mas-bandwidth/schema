package elixirtable

// §schema#876 item F8, row fixed_form_ragged_tail: a record region that is not
// a whole number of records is malformed, not refused. F8 is a GAP on go, cpp
// and cs, only PARTIAL on c and elixir. It is F7's direct sibling — the very
// next guard in the same emitted function — and go closed it in #1291
// (gotable/fixedform_ragged_tail_test.go is the reference). malformed and
// refused are THE TWO answers and are NEVER both set: a ragged tail is the
// residue of a bad file, so it owes :malformed and not one of the seven §1.1
// names. record_bytes is the SELECTED LINEAGE ENTRY's record size, compiled
// into the reader (@<root>_record_bytes = fixedHashBytes + body), never on the
// wire, so the guard's record_bytes <= 8 arm is NOT reachable from a file at
// all. rest is the file's bytes after the header and layout, so this row forges
// only the second arm, rest % record_bytes != 0, by appending 1..record_bytes-1
// bytes. extra == record_bytes is one more WHOLE record (the reader owes
// :no_layout here, not malformed), so it is asserted separately. On this leg
// the ragged tail is a BINARY PATTERN-MATCH FALLTHROUGH: <root>_fixed_split/4
// matches a whole record, then a full-size record whose hash names no layout,
// and ANYTHING ELSE falls to the `_ -> {:error, :malformed}` clause — the
// ragged tail. CONTROL 2 turns that catch-all into {:error, :no_layout} and the
// row goes red at extra=1.

import (
	"path/filepath"
	"testing"
)

func TestFixedFormRaggedTail(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	// A SINGLE-LINEAGE reader over its OWN file: record_bytes is the selected
	// entry's record size, and with no older entry selected the selected entry IS
	// the reader's own, so @array_bounded_grow_record_bytes is the split's size.
	oldFile := filepath.Join(corpus, "new_array_bounded_grow.bin")

	// THE FULL FILE READS CLEANLY FIRST, so a broken fixture cannot pass the row
	// by accident: the same bytes, unragged, must decode at least one value.
	fullBody := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the FULL corpus file refused: #{inspect(tag)} #{why(report)}")
  check(is_list(values) and values != [],
    "the full file decoded no value, so the ragged tails below withhold nothing: #{inspect(values)}")
  check(report.malformed == false, "a clean full read is not malformed")`
	if out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow",
		nil, 0, oldFile, fullBody); err != nil {
		t.Fatalf("full-length control read: %v\n%s", err, out)
	}

	// THE ROW: append 1..record_bytes-1 bytes and hand each to the probe. The
	// catch-all clause of <root>_fixed_split/4 owes :malformed for every one;
	// record_bytes is read from the emitted reader, not from a literal, so the
	// loop's bound is the reader's own decision. The destination on this leg is
	// the return's SECOND SLOT, so "untouched" is not is_list(reason): a decoded
	// read puts a NONEMPTY LIST there, a malformed read puts the atom :malformed.
	truncBody := `  data = File.read!(file())
  record_bytes = T.array_bounded_grow_fixed_record_bytes()
  for extra <- 1..(record_bytes - 1) do
    ragged = data <> :binary.copy(<<0x5A>>, extra)
    {tag, reason, report} = load(ragged)
    check(tag == :error, "a ragged tail must refuse: #{inspect(tag)}")
    check(reason == :malformed,
      "a ragged tail is malformed, never a named refusal: #{inspect(reason)}")
    check(not is_list(reason),
      "malformed decoded nothing: the second slot is a reason atom, not values: #{inspect(reason)}")
    check(report.malformed == true, "a ragged tail sets malformed: #{why(report)}")
    check(report.unknown == 0, "no unknown: #{why(report)}")
    check(report.kind_mismatch == 0, "no kind_mismatch: #{why(report)}")
    check(report.clamped == 0, "no clamped: #{why(report)}")
    check(report.widened == 0, "no widened: #{why(report)}")
    check(report.duplicate == 0, "no duplicate: #{why(report)}")
    check(report.layout_hash == 0, "no layout_hash: #{why(report)}")
  end
  # extra == record_bytes is one more WHOLE record, not a ragged tail: the reader
  # owes a refusal by name (:no_layout — the appended 0x5A record's hash names no
  # layout) and not malformed.
  whole = data <> :binary.copy(<<0x5A>>, record_bytes)
  {wtag, wreason, wreport} = load(whole)
  check(wtag == :error, "extra == record_bytes is still a refusal: #{inspect(wtag)}")
  check(wreason == :no_layout,
    "a whole record whose hash names no layout is :no_layout, never malformed: #{inspect(wreason)}")
  check(wreport.malformed == false,
    "extra == record_bytes is not a ragged tail, so it does not set malformed: #{why(wreport)}")`

	if out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow",
		nil, 0, oldFile, truncBody); err != nil {
		t.Fatalf("ragged tail: %v\n%s", err, out)
	}
}
