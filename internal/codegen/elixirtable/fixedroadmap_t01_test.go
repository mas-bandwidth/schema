package elixirtable

// THE ELIXIR LEG'S FIXED-TABLE FRAMING ROWS (docs/FIXED-FORM-ALGORITHM.md §2,
// §5.3 and §5.9 #47; the audit matrix is schema#876). One subtest per roadmap
// task id of docs/roadmap-evidence/fixed-elixir.sexp that was not already held
// by a named test of this package. The BEAM has no caller-owned destination:
// a read's destination is the value it RETURNS, so "not one destination byte is
// written" is asserted as the return's second slot being the reason ATOM, paired
// with a control read of the same reader that returns a NONEMPTY LIST.
//
// Held by an existing test and so given no subtest here: F7
// (fixedform_under_20_bytes_test.go), F8 (fixedform_ragged_tail_test.go), F10
// (the ragged test's whole-record case and
// fixedversioning_refuse_writes_nothing_test.go) and R8
// (fixedversioning_test.go, OldRefusesNew and Hash/hash_unknown).

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t01Probe runs body inside the generated probe of `reader`, with R aliased to
// the unit's FixedRuntime so a body can name the fresh report and the lanes.
func t01Probe(t *testing.T, reader string, older []string, retire int, file, body string) {
	t.Helper()
	ns := ir.GoExportName(versionSchema(t, reader).Package)
	body = "  alias " + ns + ".FixedRuntime, as: R\n" + body
	if out, err := runVersionProbe(t, elixirBinary(t), reader, older, retire, file, body); err != nil {
		t.Fatalf("%s: %v\n%s", reader, err, out)
	}
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	oldFile := corpus + "/old_field_append.bin"
	newFile := corpus + "/new_field_append.bin"
	lineage := []string{"VOLD_field_append"}

	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		// §5.3 step 3: "L := LE(4, file+16) ; if 20 + L > len(file): REFUSE
		// layout_malformed". Every cut that leaves the file past its 20-byte
		// header but short of 20+L is the one name, with the report untouched.
		{"elixir/F4", func(t *testing.T) {
			t01Probe(t, "VNEW_field_append", lineage, 0, oldFile, `  data = File.read!(file())
  <<_::binary-size(16), l::little-unsigned-32, _::binary>> = data
  {ctag, cvalues, creport} = load(data)
  check(ctag == :ok and is_list(cvalues) and cvalues != [], "the untruncated control refuses: #{inspect(ctag)} #{why(creport)}")
  for cut <- 20..(20 + l - 1) do
    {tag, reason, report} = load(binary_part(data, 0, cut))
    check(tag == :error, "cut #{cut}: a layout past the file must refuse: #{inspect(tag)}")
    check(reason == :layout_malformed, "cut #{cut}: 20 + L > len(file) is layout_malformed, not #{inspect(reason)}")
    check(not is_list(reason), "cut #{cut}: nothing was decoded")
    check(report == R.report(), "cut #{cut}: a named refusal moves nothing and is never malformed: #{why(report)}")
  end
  {wtag, wvalues, wreport} = load(binary_part(data, 0, 20 + l))
  check(wtag == :ok and wvalues == [] and wreport.malformed == false,
    "20 + L == len(file) is a whole layout and no records: #{inspect({wtag, wvalues})}")`)
		}},

		// §5.3 step 10: "n := rest / record_bytes ; if n > capacity: REFUSE
		// batch_too_large". n == capacity reads, n > capacity refuses, the
		// ragged tail (step 9) answers before the capacity does and the
		// capacity (step 10) answers before the per-record hash (step 11).
		{"elixir/F9", func(t *testing.T) {
			t01Probe(t, "VNEW_array_bounded_grow", nil, 0, corpus+"/new_array_bounded_grow.bin", `  data = File.read!(file())
  <<_::binary-size(16), l::little-unsigned-32, _::binary>> = data
  rb = T.array_bounded_grow_fixed_record_bytes()
  n = div(byte_size(data) - 20 - l, rb)
  check(n >= 1 and rem(byte_size(data) - 20 - l, rb) == 0, "the corpus file is not whole records: #{n}")
  {otag, ovalues, oreport} = load(data, batch_capacity: n)
  check(otag == :ok and length(ovalues) == n and oreport.malformed == false,
    "n == capacity reads every record: #{inspect(otag)} #{why(oreport)}")
  {dtag, _, _} = load(data)
  check(dtag == :ok, "the default capacity reads the same file: #{inspect(dtag)}")
  for cap <- [n - 1, 0] do
    {tag, reason, report} = load(data, batch_capacity: cap)
    check(tag == :error and reason == :batch_too_large,
      "n > capacity (#{cap}) is batch_too_large: #{inspect({tag, reason})}")
    check(not is_list(reason), "the refusal built no value")
    check(report == R.report(), "REFUSE is total and never malformed: #{why(report)}")
  end
  <<first8::binary-size(8), rest::binary>> = binary_part(data, byte_size(data) - rb, rb)
  forged = data <> <<Bitwise.bxor(:binary.decode_unsigned(first8, :little), 0xFFFFFFFFFFFFFFFF)::little-unsigned-64>> <> rest
  {ftag, freason, _} = load(forged, batch_capacity: n)
  check(ftag == :error and freason == :batch_too_large,
    "step 10 answers before step 11's record hash: #{inspect({ftag, freason})}")
  {rtag, rreason, rreport} = load(data <> <<0x5A>>, batch_capacity: 0)
  check(rtag == :error and rreason == :malformed and rreport.malformed == true,
    "step 9's ragged tail answers before the capacity: #{inspect({rtag, rreason})}")`)
		}},

		// §5.3 step 8 / §5.9 #47: "EVERY HASH THE RUNTIME HOLDS IS A HANDED
		// CONSTANT". The identity lane is the lineage index whose handed hash is
		// the reader's own; a file whose layout bytes are this build's own but
		// whose header hash is in no entry is never identity.
		{"elixir/R7", func(t *testing.T) {
			t01Probe(t, "VNEW_field_append", lineage, 0, newFile, `  known = T.lineage_fixed_known()
  last = tuple_size(known) - 1
  check(tuple_size(known) == 2, "the lineage holds the older entry and the reader's own: #{tuple_size(known)}")
  check(R.lineage_lane(T, :lineage, last) == :identity, "the reader's own index is the identity lane")
  check(match?({:ok, _, _, _, _, _}, R.lineage_lane(T, :lineage, 0)), "the older index is a compiled lane, not identity")
  data = File.read!(file())
  <<_::binary-size(8), hash::little-unsigned-64, _::binary>> = data
  check(hash == elem(known, last).hash, "the file's header hash is the handed constant of the last index")
  {tag, _, report} = load(data)
  check(tag == :ok and report.malformed == false, "the reader's own file reads on the identity lane: #{inspect(tag)}")
  <<head::binary-size(8), _::binary-size(8), rest::binary>> = data
  forged = <<head::binary, 0x0EADBEEFCAFEF00D::little-unsigned-64, rest::binary>>
  {ftag, freason, freport} = load(forged)
  check(ftag == :error and freason == :layout_newer and freport.layout_hash == 0x0EADBEEFCAFEF00D,
    "own layout bytes under a hash in no entry are never identity: #{inspect({ftag, freason})}")`)
		}},

		// §5.3 steps 3 and 7: "a known hash with a different layout length or
		// bytes → layout_malformed; the seven §1.1 malformations under a known
		// hash all come back as this one name". The seven are forged one rule
		// each (§1.1's table), the length cases move L itself, and the residue
		// (fewer than four bytes) is the same name.
		{"elixir/R9", func(t *testing.T) {
			t01Probe(t, "VNEW_field_append", lineage, 0, oldFile, `  data = File.read!(file())
  <<head::binary-size(16), l::little-unsigned-32, tail::binary>> = data
  <<layout::binary-size(^l), records::binary>> = tail
  put = fn bin, off, new ->
    n = byte_size(new)
    <<a::binary-size(^off), _::binary-size(^n), b::binary>> = bin
    a <> new <> b
  end
  file_of = fn lay -> <<head::binary, byte_size(lay)::little-unsigned-32, lay::binary, records::binary>> end
  <<count::little-unsigned-32, _::binary>> = layout
  <<_::binary-size(21), _id::binary-size(8), k1::8, s1::little-unsigned-32, _::binary>> = layout
  deep =
    <<66::little-unsigned-32>> <>
      Enum.into(1..66, <<>>, fn i -> <<0::64, 13::8, 0::32, (if i < 66, do: 1, else: 0)::little-unsigned-32>> end)
  mutants = [
    {"length + 1", layout <> <<0>>},
    {"length - 1", binary_part(layout, 0, l - 1)},
    {"1 count", put.(layout, 0, <<count + 1::little-unsigned-32>>)},
    {"2 kind unknown", put.(layout, 4 + 17 + 8, <<31>>)},
    {"3 size mismatch", put.(layout, 4 + 17 + 9, <<s1 + 1::little-unsigned-32>>)},
    {"4 kind invalid", put.(layout, 4 + 17 + 8, <<(if k1 == 14, do: 13, else: 14)>>)},
    {"5 tree unclosed", put.(layout, 4 + 13, <<0::little-unsigned-32>>)},
    {"6 record too large", put.(layout, 4 + 9, <<65537::little-unsigned-32>>)},
    {"7 too deep", deep},
    {"empty residue", <<>>},
    {"three byte residue", <<1, 0, 0>>}
  ]
  {ctag, cvalues, _} = load(file_of.(layout))
  check(ctag == :ok and is_list(cvalues) and cvalues != [], "the rebuilt unmutated control refuses: #{inspect(ctag)}")
  for {label, lay} <- mutants do
    {tag, reason, report} = load(file_of.(lay))
    check(tag == :error and reason == :layout_malformed,
      "#{label} under a known hash is layout_malformed: #{inspect({tag, reason})}")
    check(not is_list(reason), "#{label}: nothing was decoded")
    check(report == R.report(), "#{label}: a named refusal is never malformed and moves nothing: #{why(report)}")
  end`)
		}},

		// §5.3 step 11: "if LE(8, at) != h: REFUSE no_layout -- BEFORE any byte
		// is landed". A forged LAST record refuses the whole batch with no
		// value of the records before it, and the generated read splits (checks
		// every record hash) before it runs the plan over any body.
		{"elixir/R12", func(t *testing.T) {
			t01Probe(t, "VNEW_array_bounded_grow", nil, 0, corpus+"/new_array_bounded_grow.bin", `  data = File.read!(file())
  <<_::binary-size(16), l::little-unsigned-32, _::binary>> = data
  rb = T.array_bounded_grow_fixed_record_bytes()
  hdr = binary_part(data, 0, 20 + l)
  rec = binary_part(data, 20 + l, rb)
  <<h::little-unsigned-64, body::binary>> = rec
  bad = <<Bitwise.bxor(h, 0xFFFFFFFFFFFFFFFF)::little-unsigned-64, body::binary>>
  {ctag, cvalues, _} = load(hdr <> rec <> rec <> rec)
  check(ctag == :ok and length(cvalues) == 3, "three good records read as three values: #{inspect(ctag)}")
  {tag, reason, report} = load(hdr <> rec <> rec <> bad)
  check(tag == :error and reason == :no_layout, "a forged last record is no_layout: #{inspect({tag, reason})}")
  check(not is_list(reason), "records 0 and 1 are good and not one value of them came back")
  check(report == R.report(), "no_layout moves nothing and is never malformed: #{why(report)}")`)
			u := versionSchema(t, "VNEW_array_bounded_grow")
			out, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			gen := findGenerated(t, out, "_fixed_run(")
			split := strings.Index(gen, "<- array_bounded_grow_fixed_split(records")
			run := strings.Index(gen, "R.run(plan")
			if split < 0 || run < 0 || split > run {
				t.Fatalf("the per-record hash check (split at %d) must stand before the prefill-and-run (R.run at %d)", split, run)
			}
		}},

		// §5.3's refusal table and SPEC-TABLES §3.4: "a REFUSAL BY NAME ...
		// malformed false, -1, all zero, and not one destination byte written";
		// a MALFORMED read sets malformed and no name. Every file-path refusal
		// this reader can answer, as {:error, name, report} with the whole
		// report compared to a fresh one (plus the file's hash on the two
		// layout refusals that report it).
		{"elixir/R13", func(t *testing.T) {
			body := `  data = File.read!(file())
  <<head::binary-size(8), h::little-unsigned-64, rest::binary>> = data
  <<_::binary-size(16), l::little-unsigned-32, _::binary>> = data
  rb = byte_size(data) - 20 - l
  recs = binary_part(data, 20 + l, byte_size(data) - 20 - l)
  <<rh::little-unsigned-64, rbody::binary>> = binary_part(recs, 0, rb)
  fresh = R.report()
  cases = [
    {:previous_form, <<1, 0, 0, 0, 0, 0, 0, 0, 0, 0>>, [], 0},
    {:message_form_as_file, <<2, 0, 0>>, [], 0},
    {:newer_form, <<9>> <> binary_part(data, 1, byte_size(data) - 1), [], 0},
    {:layout_newer, <<head::binary, 0x0EADBEEFCAFEF00D::little-unsigned-64, rest::binary>>, [], 0x0EADBEEFCAFEF00D},
    {:layout_malformed, binary_part(data, 0, 20 + l - 1), [], 0},
    {:layout_malformed, <<binary_part(data, 0, 24)::binary, Bitwise.bxor(:binary.at(data, 24), 0xFF), binary_part(data, 25, byte_size(data) - 25)::binary>>, [], 0},
    {:plan_too_large, data, [plan_capacity: 1], 0},
    {:batch_too_large, data, [batch_capacity: 0], 0},
    {:no_layout, binary_part(data, 0, 20 + l) <> <<Bitwise.bxor(rh, 0xFFFFFFFFFFFFFFFF)::little-unsigned-64, rbody::binary>>, [], 0}
  ]
  {ctag, cvalues, _} = load(data)
  check(ctag == :ok and is_list(cvalues) and cvalues != [], "the control read builds values: #{inspect(ctag)}")
  for {name, bytes, opts, want_hash} <- cases do
    {tag, reason, report} = load(bytes, opts)
    check(tag == :error and reason == name, "#{name}: got #{inspect({tag, reason})}")
    check(not is_list(reason), "#{name}: a refusal built no value")
    check(report.malformed == false, "#{name}: refused+reason and malformed are never both set")
    check(report == %{fresh | layout_hash: want_hash}, "#{name}: every counter stays zero: #{why(report)}")
  end
  for {label, bytes} <- [{"empty", <<>>}, {"nineteen bytes", binary_part(data, 0, 19)}, {"reserved byte", <<3, 1>> <> binary_part(data, 2, byte_size(data) - 2)}, {"ragged tail", data <> <<0x5A>>}] do
    {tag, reason, report} = load(bytes)
    check(tag == :error and reason == :malformed, "#{label}: malformed names no reason: #{inspect({tag, reason})}")
    check(report == %{fresh | malformed: true}, "#{label}: only malformed is set: #{why(report)}")
  end
  _ = h`
			t01Probe(t, "VNEW_field_append", lineage, 0, oldFile, body)
			t01Probe(t, "VNEW_floor", []string{"VOLD_floor", "VMID_floor"}, 1, corpus+"/old_floor.bin", `  data = File.read!(file())
  <<_::binary-size(8), h::little-unsigned-64, _::binary>> = data
  {tag, reason, report} = load(data)
  check(tag == :error and reason == :layout_unsupported, "below the floor: #{inspect({tag, reason})}")
  check(not is_list(reason), "layout_unsupported built no value")
  check(report == %{R.report() | layout_hash: h}, "layout_unsupported reports the file hash and nothing else: #{why(report)}")`)
		}},
	}
	for _, tc := range tasks {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
