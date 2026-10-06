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

	}
	for _, tc := range tasks {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
