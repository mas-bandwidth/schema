package elixirtable

// elixir/F12 — "second layout for a held hash", docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 5: "a known hash over different bytes is `layout_malformed`". The
// hash is TAKEN AS GIVEN (step 4: the definitions digest is not on the wire),
// so a file whose header hash is one the lineage holds but whose layout bytes
// differ from the bytes the lock recorded is ONE name — a lie about a known
// version — refused before a record byte is touched, and never `layout_newer`.
//
// The vectors are cT01SecondLayout's (internal/codegen/ctable/
// fixedroadmap_t01_test.go:166-233), driven through this leg's own probe helper
// t01Probe over the same field_append lock pair (VNEW_field_append with
// VOLD_field_append as its single older entry): every byte of the held layout
// bent in turn; the OTHER lock entry's real layout under this build's hash; and
// this build's layout under the older entry's hash. Each is refused
// `:layout_malformed`, decodes nothing (the third slot is the reason ATOM, not
// a list of built values) and moves no report counter. The contrast is a hash
// held by no entry, which is `:layout_newer` and reports the file's hash.
//
// The vectors are built from the build's OWN lineage — T.lineage_fixed_known/0,
// the handed constants of §5.3 step 8 — and not from the C++ reference corpus,
// for the reason every t-card of this leg carries: the harness reaches no
// sibling runtime checkout. The law is a byte comparison against the bytes the
// lock recorded, so the module's own two entries are exactly the premises the
// refusal needs.

import (
	"testing"
)

func TestFixedRoadmapElixirT13SecondLayoutUnderAHeldHash(t *testing.T) {
	t.Parallel()
	body := `  known = T.lineage_fixed_known()
  check(tuple_size(known) == 2, "the lineage holds the older entry and the reader's own: #{tuple_size(known)}")
  older = elem(known, 0)
  own = elem(known, 1)
  check(own.layout != older.layout, "the premise fails: the two lock entries carry one layout")

  fresh = R.report()
  body_bytes = T.lineage_fixed_body_bytes()

  join = fn entry, layout ->
    R.file_header(entry.hash, byte_size(layout)) <>
      layout <> <<entry.hash::little-unsigned-64, 0::size(body_bytes)-unit(8)>>
  end

  malformed = fn label, bytes ->
    {tag, reason, report} = load(bytes)
    check(tag == :error and reason == :layout_malformed,
      "#{label}: a second layout for a held hash is layout_malformed: #{inspect({tag, reason})}")
    check(not is_list(reason), "#{label}: nothing was decoded")
    check(report == fresh, "#{label}: a named refusal moves no counter: #{why(report)}")
  end

  clean = join.(own, own.layout)
  {ctag, cvalues, creport} = load(clean)
  check(ctag == :ok and is_list(cvalues) and cvalues != [] and creport.malformed == false,
    "the un-bent layout under its held hash must open: #{inspect({ctag, cvalues})} #{why(creport)}")

  held = own.layout
  for b <- 0..(byte_size(held) - 1) do
    bent =
      <<binary_part(held, 0, b)::binary, Bitwise.bxor(:binary.at(held, b), 0x01),
        binary_part(held, b + 1, byte_size(held) - b - 1)::binary>>

    malformed.("layout byte #{b} bent", join.(own, bent))
  end

  malformed.("the older entry's real layout under this build's hash", join.(own, older.layout))
  malformed.("this build's layout under the older entry's hash", join.(older, own.layout))

  stranger = 0x0EADBEEFCAFEF00D
  {stag, sreason, sreport} = load(join.(%{own | hash: stranger}, own.layout))
  check(stag == :error and sreason == :layout_newer,
    "a hash held by no entry is layout_newer, not #{inspect({stag, sreason})}")
  check(sreport.layout_hash == stranger,
    "layout_newer reports the file's hash: #{inspect(sreport.layout_hash)}")
  check(sreport == %{fresh | layout_hash: stranger},
    "layout_newer moves nothing but the file hash: #{why(sreport)}")`
	t01Probe(t, "VNEW_field_append", []string{"VOLD_field_append"}, 0, "", body)
}
