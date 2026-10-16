package elixirtable

import (
	"path/filepath"
	"testing"
)

// TestWideningReasonsElixir pins docs/SPEC-TABLES.md §4: a narrower wire
// element from the older writer is decoded at its own width, the value lands,
// and the report carries the reference reason alongside the widened counter.
// list_scalar_widen.bin is the older writer and narrower element witness.
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
{:ok, value, report} = Listdemo.Table.load("Save", File.read!(` + elixirString(wire) + `))
Probe.check(value["scores"] == [10, 0, 0], "narrow scores land exactly: #{inspect(value)}")
Probe.check(report.widened == 1, "one widening is counted: #{inspect(report)}")
Probe.check(report.reason == "", "a clean widening carries an empty reason string: #{inspect(report)}")
Probe.check(report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and report.malformed == false,
  "widening does not become another event: #{inspect(report)}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}
