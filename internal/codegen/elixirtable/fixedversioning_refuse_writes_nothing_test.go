package elixirtable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 9, `refuse_writes_nothing`: the NEW reader reads `old_nested_append.bin`
// through its lineage — a COMPILED plan with a NONEMPTY fill list (Vec.w = 88)
// is in hand — with record 0's per-record hash (its first eight bytes) bitwise
// INVERTED and the header's hash at file+8 untouched, so step 11 compares a hash
// that names no layout. Every counter is asserted EXACTLY zero: a `>= 1` passes
// the read this row exists to catch, a reader that ran the plan before the hash
// check refused.
func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	body := `  data = File.read!(file())
  check(byte_size(data) >= 20, "the corpus file is too short for a header")
  <<_::binary-size(16), layout_len::little-unsigned-32, _::binary>> = data
  rec0 = 20 + layout_len
  check(rec0 + 8 <= byte_size(data), "record 0 does not fit the corpus file")
  <<head::binary-size(rec0), h::little-unsigned-64, rest::binary>> = data
  data = <<head::binary, Bitwise.bxor(h, 0xFFFFFFFFFFFFFFFF)::little-unsigned-64, rest::binary>> # the forge: record 0's per-record hash, inverted
  fresh = fresh_value()
  {tag, why, report} = load(data)
  check(tag == :error, "a record whose hash names no held layout must refuse: #{inspect(tag)}")
  check(why == :no_layout, "the record hash owes no_layout, not #{inspect(why)}")
  check(report.malformed == false, "a refusal by name never sets malformed too (the joint answer)")
  check(report.layout_hash == 0, "no_layout is not a layout refusal: layout_hash must stay untouched")
  check(
    report.widened == 0 and report.unknown == 0 and report.kind_mismatch == 0 and
      report.clamped == 0 and report.duplicate == 0,
    "REFUSE is total: no counter moves: #{why(report)}"
  )
  check(fresh == fresh_value(), "REFUSE wrote destination values")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_nested_append", []string{"VOLD_nested_append"}, 0,
		filepath.Join(corpus, "old_nested_append.bin"), body)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
