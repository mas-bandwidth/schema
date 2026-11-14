package elixirtable

import (
	"path/filepath"
	"testing"
)

// TestWideningReasonsElixir pins docs/SPEC-TABLES.md §3, §4 and §4.2 on the
// Elixir variable form: a list whose element kind narrowed but whose L still
// holds the writer's wider slots leaves bytes inside the L after its count's
// elements, which is damage, so the read refuses wire_damaged. Beside it, the
// two reasons a load can refuse with, spelled with the reference leg's
// TableRefuseReason names (internal/codegen/gotable/refusal.go): unknown_form
// for a form byte this reader does not carry (§3) and wire_damaged for framing
// damage.
//
// list_scalar_widen.bin is that wire: the corpus answers it `refused` on every
// leg (testdata/conformance/tables/reports.txt), as Go's oracle does
// (internal/tablewire: "bytes left inside an array's L after its elements").
func TestWideningReasonsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "lists", "Save.schema"))
	dir := t.TempDir()
	names := writeModuleDir(t, dir, generateElixir(t, u))
	wire := filepath.Join(root, "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

bytes = File.read!(` + elixirString(wire) + `)

# bytes left inside the narrowed list's L after its elements: damage
{:error, :malformed, slack} = Listdemo.Table.load("Save", bytes)
Probe.check(slack.reason == "wire_damaged", "list slack says wire_damaged: #{inspect(slack)}")

# a form byte this reader does not carry is a refusal, never damage (§3)
<<_form, rest::binary>> = bytes
{:error, :newer_form, refused} = Listdemo.Table.load("Save", <<9, rest::binary>>)
Probe.check(refused.reason == "unknown_form", "a newer form says unknown_form: #{inspect(refused)}")
Probe.check(true, "a refused form is not damage: #{inspect(refused)}")

# framing damage is wire_damaged, with the malformed flag beside it
{:error, :malformed, damaged} = Listdemo.Table.load("Save", binary_part(bytes, 0, byte_size(bytes) - 1))
Probe.check(damaged.reason == "wire_damaged", "a truncated wire says wire_damaged: #{inspect(damaged)}")
Probe.check(damaged.reason == "wire_damaged", "wire_damaged is the verdict: #{inspect(damaged)}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}
