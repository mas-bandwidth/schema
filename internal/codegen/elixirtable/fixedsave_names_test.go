package elixirtable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirFixedSaveLooksUpNamesWithoutAtoms pins the write contract
// (docs/SPEC.md §5.1, the Elixir row) at the table surface's bridge to the
// fixed form: save/2 of a fixed-class root resolves an enum name and a union
// arm name through the descriptor's own name table, and a name the table does
// not hold is refused WITHOUT creating an atom. Atoms are never collected, so
// a writer that turns a caller's string into one before looking it up grows
// the node's atom table with every distinct refused name.
//
// tabledemo's LoadoutConfig is a fixed table: its grade is an enum field and
// primary.effect is a union. The committed form-3 vector loads and re-saves
// byte for byte, every known grade and arm still saves, and 2,000 distinct
// unknown grade names and 2,000 distinct unknown arm names are each refused
// with the atom count at most a small constant above where it started.
//
// It runs elixir, so it is a functional test: under SCHEMA_REQUIRE_CORPUS=1 a
// missing toolchain fails by name (elixirBinary).
func TestElixirFixedSaveLooksUpNamesWithoutAtoms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := generateElixir(t, loadUnit(t, filepath.Join(root, "tables", "examples", "Tables.schema")))
	wire := filepath.Join(root, "testdata", "conformance", "tables", "wire", "loadout_full.bin")
	probe := strings.ReplaceAll(fixedSaveNamesProbe, "WIRE", fmt.Sprintf("%q", wire))
	out, err := runArmProbe(t, elixirBin, files, probe)
	if err != nil {
		t.Fatalf("fixed-class save name lookup: %v\n%s", err, out)
	}
}

const fixedSaveNamesProbe = `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  # The refusal forms docs/SPEC.md §5.1 gives the Elixir leg: {:error,
  # :invalid_value} from save/2, ArgumentError from the fixed writer.
  def refused?(value) do
    try do
      case Tabledemo.Table.save("LoadoutConfig", value) do
        {:error, :invalid_value} -> true
        _ -> false
      end
    rescue
      ArgumentError -> true
    end
  end
end

wire = File.read!(WIRE)
{:ok, v, report} = Tabledemo.Table.load("LoadoutConfig", wire)
Probe.check(report.unknown == 0, "clean load: #{inspect(report)}")
{:ok, out} = Tabledemo.Table.save("LoadoutConfig", v)
Probe.check(out == wire, "the committed vector re-saves byte for byte")

for grade <- ["Bronze", "Silver", "Gold"] do
  {:ok, bytes} = Tabledemo.Table.save("LoadoutConfig", Map.put(v, "grade", grade))
  {:ok, back, _} = Tabledemo.Table.load("LoadoutConfig", bytes)
  Probe.check(back["grade"] == grade, "grade #{grade} saves and reads back: #{inspect(back["grade"])}")
  Probe.check(Map.put(back, "grade", v["grade"]) == v, "grade #{grade} moves nothing else")
end

for effect <- [{"buff", %{"multiplier" => 0.5}}, {"debuff", %{"amount" => 7}}, nil] do
  value = put_in(v, ["primary", "effect"], effect)
  {:ok, bytes} = Tabledemo.Table.save("LoadoutConfig", value)
  {:ok, back, _} = Tabledemo.Table.load("LoadoutConfig", bytes)
  Probe.check(back == value, "arm #{inspect(effect)} saves and reads back: #{inspect(back["primary"])}")
end

before = :erlang.system_info(:atom_count)

for i <- 1..2000 do
  grade = "Unheard" <> Integer.to_string(i) <> "Grade"
  Probe.check(Probe.refused?(Map.put(v, "grade", grade)), "unknown grade #{grade} is refused")
  arm = "unheardArm" <> Integer.to_string(i)
  value = put_in(v, ["primary", "effect"], {arm, %{}})
  Probe.check(Probe.refused?(value), "unknown arm #{arm} is refused")
end

grown = :erlang.system_info(:atom_count) - before
Probe.check(grown <= 32, "4000 refused names created #{grown} atoms")
IO.puts("ok")
`
