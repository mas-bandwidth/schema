package elixirtable

// THE VARIABLE WRITERS REFUSE A COUNTED ARRAY BELOW ITS MIN AND A LIST FIELD
// THAT HOLDS NO LIST (docs/SPEC.md §5.1 row 3; docs/SPEC-TABLES.md §22.2, §16.3;
// SF-22, SF-28). save/2 and to_json/2 refuse in every build: [2..4]int32 holding
// one element is invalid_value, and a list field holding a number is
// invalid_value, never written as an empty list. Each refused case has a lawful
// twin at the Min. Each plant puts the old code back and must turn the probe red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

  def refused(f) do
    try do
      case f.() do
        {:error, _} -> true
        _ -> false
      end
    catch
      :invalid_value -> true
    end
  end
end

alias Cmw.Table, as: T

# save/2
Probe.check(Probe.refused(fn -> T.save("Root", %{"items" => [1]}) end), "save: a count below Min was written")
Probe.check(match?({:ok, _}, T.save("Root", %{"items" => [1, 2]})), "save: a count at Min was refused")
Probe.check(match?({:ok, _}, T.save("Root", %{"items" => [1, 2], "tags" => [3]})), "save: lists were refused")

# to_json/2
Probe.check(Probe.refused(fn -> T.to_json("Root", %{"items" => [1]}) end), "to_json: a count below Min was written")
Probe.check(match?({:ok, _}, T.to_json("Root", %{"items" => [1, 2]})), "to_json: a count at Min was refused")
Probe.check(Probe.refused(fn -> T.to_json("Root", %{"items" => [1, 2], "tags" => 5}) end), "to_json: a non-list in a list field was written")
Probe.check(match?({:ok, _}, T.to_json("Root", %{"items" => [1, 2], "tags" => [3]})), "to_json: lists were refused")
IO.puts("ok")
`

func TestElixirVariableWritersRefuseCountBelowMinAndNonList(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	files := generateElixir(t, cmwUnit(t))
	out, err := runArmProbe(t, elixirBin, files, countedMinWriteProbe)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), "ok") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for name, plant := range map[string][2]string{
		"save-min":      {"if n < Map.get(f, :min_count, 0),\n      do: throw(:invalid_value)\n\n    required_value(f, v)", "required_value(f, v)"},
		"json-non-list": {"defp json_field(%{t: :list}, _other, _depth), do: throw(:invalid_value)", "defp json_field(%{t: :list}, _other, _depth), do: \"[]\""},
	} {
		t.Run(name, func(t *testing.T) {
			files := generateElixir(t, cmwUnit(t))
			elixirPatch(t, files, plant[0], plant[1])
			out, err := runArmProbe(t, elixirBin, files, countedMinWriteProbe)
			if err == nil {
				t.Fatalf("REVERSED WITNESS FAILED: the old behaviour planted back left the probe green\n%s", out)
			}
			if !strings.Contains(out, "FAIL: ") {
				t.Fatalf("the probe went red, but not on a check: %v\n%s", err, out)
			}
		})
	}
}
