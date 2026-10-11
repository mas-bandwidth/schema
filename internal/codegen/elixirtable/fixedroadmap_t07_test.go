package elixirtable

// THE ROADMAP'S LIVE-EXTENTS, WRITING AND UNION-GUARD TASKS ON THE ELIXIR LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2's
// COMPILE and §5.4, docs/SPEC-TABLES.md §3.4; docs/FIXED-FORM-VERSIONING-TESTS.md).
// One subtest per task id this card proves, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling runtime checkout.
//
// elixir/C14 [verify]: the bounds pass and the ordinal count walk LIVE elements
// only — the plan's `:live` gate is the count the generated walk is held to.
//
// elixir/W4 [verify]: read slack is unspecified: the read binds a text field's
// used span and never reads the bytes behind it.
//
// elixir/W9 [verify]: prefill covers the unwritten ranges only: the plan writes
// into one image born from the prefill, and the assembler fills every gap the
// plan leaves from it.
//
// elixir/W1 [verify]: write slack is template zeros: the writer pads a text
// field out to its declared bound with a zero segment.
//
// elixir/W5 [verify]: the write-side check is a debug-assert equivalent (the C++
// reference's `schema_assert`, compiled out by NDEBUG), and this port raises.

import (
	"strings"
	"testing"
)

// t07Filter is the emitted one <Base>Fixed.ex module of a unit.
func t07Filter(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t07Has is a required substring of the emitted source, named by rule.
func t07Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// ---- elixir/C14 -------------------------------------------------------------

// t07C14 holds elixir/C14 to §5.4 and SPEC-TABLES §3.4: the bounds pass and the
// ordinal count walk LIVE elements only. On a compiled plan that is the `:live`
// wrapper the interpreter carries; on the identity plan the generated walk stops
// at the count, so a slot behind the writer's count is never matched.
func t07C14(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "defp step([{:live, count_dst, index, inner} | rest]", "C14 the live gate's step")
	t07Has(t, fixedRuntimeBody, "defp live_at(", "C14 the live gate's count")
	t07Has(t, fixedRuntimeBody, "defp live_wrap_entry(e, dst, i), do: {:live, dst, i, e}", "C14 a counted run is wrapped live")

	out, err := Generate(unitFrom(t, `package probe

enum Grade { Bronze, Gold }

fixed table Live
{
    marks  [..4]int32 | min = 0, max = 10
    note   ?int32 | min = 0, max = 10
}
`))
	if err != nil {
		t.Fatalf("C14: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "defp live_marks_fixed_list(_bin, 0, c, m), do: {[], c, m}", "C14 the identity walk stops at the count")
	t07Has(t, body, "R.clamps(c, ", "C14 the bounds pass is the generated projection")
}

// ---- elixir/W4 --------------------------------------------------------------

// t07W4 holds elixir/W4 to SPEC-TABLES §3.4's "read slack is unspecified": a
// text field's read binds only the used span it decodes and never reads the
// bytes behind it; the writer's `text_slack` is the only site that touches the
// slack, and it is on the write side.
func t07W4(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "def text(used, default, m, present) do", "W4 the read binds the used span")
	t07Has(t, fixedRuntimeBody, "def text_slack(bytes, n) when is_binary(bytes) and byte_size(bytes) <= n,", "W4 the writer owns the slack")
	out, err := Generate(unitFrom(t, `package probe

fixed table Note
{
    name string(8)
    wide wstring(4)
}
`))
	if err != nil {
		t.Fatalf("W4: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "R.text(", "W4 the read decodes the used bytes of a string(N)")
	t07Has(t, body, "R.wtext(", "W4 the read decodes the used units of a wstring(N)")
}

// ---- elixir/W9 --------------------------------------------------------------

// t07W9 holds elixir/W9 to §5.2's PREFILL: the plan writes into ONE image born
// from the prefill, and the assembler fills every range the plan did not write
// from that same prefill, so an unwritten range is the declared default and a
// written one is the plan's.
func t07W9(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "def run(entries, body, prefill, report) do", "W9 the plan runs against the prefill")
	t07Has(t, fixedRuntimeBody, "{assemble(writes, prefill), report}", "W9 the image is assembled from the prefill")
	t07Has(t, fixedRuntimeBody, "defp assemble(writes, prefill) do", "W9 the assembler fills the unwritten ranges")
	out, err := Generate(unitFrom(t, `package probe

fixed table Note
{
    name string(8)
}
`))
	if err != nil {
		t.Fatalf("W9: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "R.run(plan, R.detach(body, copy), @", "W9 the read runs the plan against the prefill constant")
	t07Has(t, body, "_prefill, report)", "W9 the prefill constant is the image's seed")
}

// ---- elixir/W1 --------------------------------------------------------------

// t07W1 holds elixir/W1 to §3.4's write side: a text field is written as the
// used bytes then a ZERO segment out to its declared bound, so the slack of a
// written record is template zeros and never the caller's bytes.
func t07W1(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "def text_slack(bytes, n) when is_binary(bytes) and byte_size(bytes) <= n,", "W1 the write measures the slack")
	out, err := Generate(unitFrom(t, `package probe

fixed table Note
{
    name string(8)
    wide wstring(4)
}
`))
	if err != nil {
		t.Fatalf("W1: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "0::size(R.text_slack(", "W1 the write pads the slack with zeros")
}

// ---- elixir/W5 --------------------------------------------------------------

// t07W5 holds elixir/W5 to §3.4's write-side contract and the C++ reference's
// `schema_assert`/NDEBUG rule: the write check is a debug-assert equivalent, and
// this port does what its packet codec does — a check that raises — because the
// BEAM has no compile-out assert.
func t07W5(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "NDEBUG", "W5 the C++ reference's NDEBUG assert is the rule")
	t07Has(t, fixedRuntimeBody, "def ranged(value, {lo, hi}) when is_integer(value) and value >= lo and value <= hi, do: value", "W5 the write check passes an in-range value")
	t07Has(t, fixedRuntimeBody, "raise ArgumentError, \"#{inspect(value)} is outside the declared range #{lo}..#{hi}\"", "W5 the write check raises on a caller's bug")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT07Writes(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/C14", check: t07C14},
		{id: "elixir/W4", check: t07W4},
		{id: "elixir/W9", check: t07W9},
		{id: "elixir/W1", check: t07W1},
		{id: "elixir/W5", check: t07W5},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
