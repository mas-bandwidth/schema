package elixirtable

// kind_moved, the second half of the UNLAWFUL lineage entry, on the elixir leg
// (docs/FIXED-FORM-VERSIONING-TESTS.md row `kind_moved`): the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30; the Go reference's tableFixedCompileEntry): no plan
// is laid, the load refuses `layout_malformed`, no counter moves, and a second
// load refuses the same way. The VOLD_kind_moved entry is HANDED IN, as the
// unknown_census row hands in its own.
//
// This leg once counted the moved kind as a mismatch and answered the load
// `wire_damaged`. Elixir hands back a fresh list and never writes caller rows,
// so "rows back at their declared defaults" is the refusal carrying no values
// and the reader's own defaults, decoded from its prefill: lead = 1, v = 0.0,
// trail = 2.

import "testing"

func TestFixedVersioningKindMoved(t *testing.T) {
	t.Parallel()
	file := lineageCorpus(t, "old_kind_moved.bin")
	elixirBin := lineageElixir(t)
	body := `  data = File.read!(file())
  for pass <- [1, 2] do
    {tag, why, report} = load(data)
    check(tag == :error and why == :layout_malformed,
      "load #{pass}: the moved kind was not refused layout_malformed: #{inspect({tag, why})} #{inspect(report)}")
    check(not is_list(why), "load #{pass}: a refusal handed back values: #{inspect(why)}")
    check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
      "load #{pass}: a refusal moved a counter: #{inspect(report)}")
  end
  {d, _c, _m} = T.kind_moved_fixed_decode(prefill(), 0, false)
  check(d.lead == 1 and d.v == 0.0 and d.trail == 2,
    "the reader's declared defaults moved: #{inspect(d)}")`
	out, err := runVersionProbe(t, elixirBin, "VNEW_kind_moved",
		[]string{"VOLD_kind_moved"}, 0, file, body)
	if err != nil {
		t.Fatalf("kind_moved: %v\n%s", err, out)
	}
}
