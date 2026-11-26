package elixirtable

// THE VARIABLE WRITERS REFUSE A COUNTED ARRAY
// WHOSE COUNT IS OUTSIDE ITS DECLARED BOUNDS (docs/SPEC.md §5.1 row 3;
// docs/SPEC-TABLES.md §22.2, §16.3; SF-22, SF-28). The probe checks the public
// error and lawful values at both bounds through both generated writers. The
// JSON writer also rejects a number supplied to a list field.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func cmwUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Cmw.schema")
	if err := os.WriteFile(path, []byte(countedMinWriteSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

const countedMinWriteSchema = `package cmw
table Root
{
    items [2..4]int32
    tags  []int32
}
`

const countedMinWriteProbe = `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

alias Cmw.Table, as: T

for {name, write} <- [{"save", &T.save/2}, {"to_json", &T.to_json/2}] do
  for {value, description} <- [
    {%{"items" => [1]}, "a count below Min"},
    {%{"items" => [1, 2, 3, 4, 5]}, "a count above Max"}
  ] do
    Probe.check(
      write.("Root", value) == {:error, :invalid_value},
      "#{name}: #{description} did not return invalid_value"
    )
  end

  for items <- [[1, 2], [1, 2, 3, 4]] do
    Probe.check(
      match?({:ok, _}, write.("Root", %{"items" => items, "tags" => [3]})),
      "#{name}: a valid list at a declared bound was refused"
    )
  end
end
Probe.check(
  T.to_json("Root", %{"items" => [1, 2], "tags" => 5}) == {:error, :invalid_value},
  "to_json: a non-list in a list field did not return invalid_value"
)
IO.puts("ok")
`

func TestElixirVariableWritersRefuseCountBelowMinAndNonList(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	files := generateElixir(t, cmwUnit(t))
	out, err := runArmProbe(t, elixirBin, files, countedMinWriteProbe)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), "ok") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	type patch struct{ anchor, replacement string }
	controls := []struct {
		name      string
		patches   []patch
		assertion string
	}{
		{
			// write_value holds a counted array to [Min, Max] for every writer
			name: "save-min",
			patches: []patch{{
				"(n < Map.get(f, :min_count, 0) or n > f.array_bound),",
				"n > f.array_bound,",
			}},
			assertion: "save: a count below Min did not return invalid_value",
		},
		{
			// two layers refuse a non-list: the record check every writer
			// runs first, and the text writer's own list clause; the old
			// writer had neither, so both are planted out
			name: "json-non-list",
			patches: []patch{
				{
					"      not is_list(values) ->\n        throw(:invalid_value)\n",
					"      not is_list(values) ->\n        :ok\n",
				},
				{
					"defp json_field(%{t: :list}, _other, _depth), do: throw(:invalid_value)",
					"defp json_field(%{t: :list}, _other, _depth), do: \"[]\"",
				},
			},
			assertion: "to_json: a non-list in a list field did not return invalid_value",
		},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			files := generateElixir(t, cmwUnit(t))
			for _, p := range control.patches {
				matches := 0
				for _, file := range files {
					matches += strings.Count(string(file), p.anchor)
				}
				if matches != 1 {
					t.Fatalf("expected one writer control anchor %q, found %d", p.anchor, matches)
				}
				elixirPatch(t, files, p.anchor, p.replacement)
			}
			out, err := runArmProbe(t, elixirBin, files, countedMinWriteProbe)
			if err == nil || !strings.Contains(out, "FAIL: "+control.assertion) {
				t.Fatalf("the probe did not fail at %q: %v\n%s", control.assertion, err, out)
			}
			t.Logf("control reached expected assertion: %s", control.assertion)
		})
	}
}
