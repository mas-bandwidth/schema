package elixirtable

import (
	"strings"
	"testing"
)

// TestVariableRuntimeCarriesOnlyClausesTheUnitCanReach pins the variable-form
// runtime's per-unit clauses. The descriptors are module attributes, so
// Elixir's type checker sees the unit's literal data and reports a clause that
// data can never reach as a warning, which the strict compile
// (make/elixir-strict.sh, tables-elixir-fixed-form) refuses. A unit whose
// @uncarried is empty is emitted without the uncarried refusals (each guard
// would read `table in []`), and a unit with no extent anywhere is emitted
// without the nested-record extent walks (each would branch on a literal
// false has_extent). A unit that needs them keeps them.
func TestVariableRuntimeCarriesOnlyClausesTheUnitCanReach(t *testing.T) {
	t.Parallel()
	plain, err := Generate(unitFrom(t, `package probe

table Inner
{
    a int32
}

table Outer
{
    inner Inner
    b     int32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	src := string(plain[VariableTableModule+".ex"])
	for _, gone := range append([]string{"when table in @uncarried", "table in @uncarried or"}, extentWalkClauses...) {
		if strings.Contains(src, gone) {
			t.Errorf("a unit with no uncarried root and no extent still emits %q", gone)
		}
	}
	if !strings.Contains(src, "      if not is_map(value) do\n") {
		t.Error("the cook plan lost its value check with the uncarried guard")
	}
	for _, kept := range []string{"  defp cook_extent_field(_, _, at), do: at\n", "  defp cook_carve_after(_, _, _, _, cursor, _, _, buf), do: {cursor, buf}\n"} {
		if !strings.Contains(src, kept) {
			t.Errorf("the fallback clause %q is gone", kept)
		}
	}

	full, err := Generate(unitFrom(t, `package probe

table Inner
{
    values []int32 | min = 0, max = 0
}

table Outer
{
    inner   Inner
    entries map[string(8)]Inner
}
`))
	if err != nil {
		t.Fatal(err)
	}
	src = string(full[VariableTableModule+".ex"])
	for _, kept := range append([]string{"def load_measure(table, _data) when table in @uncarried", "if table in @uncarried or not is_map(value) do"}, extentWalkClauses...) {
		if !strings.Contains(src, kept) {
			t.Errorf("a unit with an uncarried root and an extent lost %q", kept)
		}
	}
}
