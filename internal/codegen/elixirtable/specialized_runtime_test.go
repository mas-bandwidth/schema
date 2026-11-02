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
// without the cook writer's extent walk (each step would branch on a literal
// false has_extent). A unit with no union carries no union runtime and no
// @unions (every Map.fetch! on an empty one always raises) and no cook union
// writer, and a unit with
// unions carries only the arm clauses its arms' kinds reach. A unit that needs
// a clause keeps it.
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
	for _, gone := range []string{"when table in @uncarried", "table in @uncarried or", "@cook_uncarried", cookExtentDef, cookCarveDef} {
		if strings.Contains(src, gone) {
			t.Errorf("a unit with no uncarried root and no extent still emits %q", gone)
		}
	}
	for _, name := range extentWalkFamilies {
		if strings.Contains(src, "defp "+name+"(") {
			t.Errorf("a unit with no extent still emits the walk %s", name)
		}
	}
	for _, gone := range append(append([]string{"@unions", "defp arm_header(", "defp json_read_arm(", "elem_kind: 15}", "defp cook_union("}, unionClauses...), cookUnionClauses...) {
		if strings.Contains(src, gone) {
			t.Errorf("a unit with no union still emits %q", gone)
		}
	}
	if !strings.Contains(src, "      if not is_map(value) do\n") {
		t.Error("the cook plan lost its value check")
	}
	for _, kept := range []string{"  defp cook_extent(_desc, _value), do: 0\n", "  defp cook_carve(_desc, _value, _node_off, _index, _order, buf), do: buf\n"} {
		if !strings.Contains(src, kept) {
			t.Errorf("the no-extent entry %q is gone", kept)
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
	for _, kept := range []string{"def load_measure(table, _data) when table in @uncarried", cookExtentDef, cookCarveDef, "cook_walk_record", "cook_walk_holder", "cook_put_member"} {
		if !strings.Contains(src, kept) && !strings.Contains(src, "defp "+kept+"(") {
			t.Errorf("a unit with an uncarried root and an extent lost %q", kept)
		}
	}

	arms, err := Generate(unitFrom(t, `package probe

table Boost
{
    a int32
}

union Effect
{
    boost Boost
    ping
}

table Root
{
    effect Effect
}
`))
	if err != nil {
		t.Fatal(err)
	}
	src = string(arms[VariableTableModule+".ex"])
	for _, kept := range append([]string{"  defp arm_content(%{t: :table} = a,", "  defp arm_content(%{t: :void},", "      %{t: :void} -> acc\n", "        if input == nil do\n", "  defp cook_union("}, cookUnionClauses...) {
		if !strings.Contains(src, kept) {
			t.Errorf("a unit with table and void arms lost %q", kept)
		}
	}
	for _, gone := range []string{"  defp arm_content(%{t: :int}", "  defp arm_content(%{t: :enum}", "when t in [:pointer, :blob]", "a.t != :pointer"} {
		if strings.Contains(src, gone) {
			t.Errorf("a unit whose arms are a table and a void still emits %q", gone)
		}
	}
}
