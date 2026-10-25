package elixirtable

import (
	"path/filepath"
	"testing"
)

// TestWideningReasonsElixir pins docs/SPEC-TABLES.md §4 and §4.2 on the Elixir
// variable form: an OLDER writer's NARROWER element, read through the newer
// reader, is decoded at its own width, the value lands exactly, the report
// counts one widening, and the report's reason says the read finished. Beside
// it, the two reasons a load can refuse with, spelled with the reference leg's
// TableRefuseReason names (internal/codegen/gotable/refusal.go): unknown_form
// for a form byte this reader does not carry (§3) and wire_damaged for framing
// damage, with the report's malformed flag set beside it.
//
// list_scalar_widen.bin is the corpus's older-writer witness for a list
// element written narrower than the reader declares (reports.txt pins it at
// 0,0,1,0,0,false,read on every leg).
func TestWideningReasonsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "lists", "SaveGame.schema"))
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

# the older, narrower writer through the newer reader: widened, landed, a read
{:ok, value, report} = Listdemo.Table.load("Save", bytes)
Probe.check(value["scores"] == [10, 0, 0], "the narrow scores land exactly: #{inspect(value)}")
Probe.check(report.widened == 1, "one widening is counted: #{inspect(report)}")
Probe.check(Map.has_key?(report, :reason), "the report carries a reason: #{inspect(report)}")
Probe.check(report.reason == "", "a widened read finished, so its reason is empty: #{inspect(report)}")
Probe.check(report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and report.malformed == false,
  "a widening is no other event: #{inspect(report)}")

# a form byte this reader does not carry is a refusal, never damage (§3)
<<_form, rest::binary>> = bytes
{:error, :newer_form, refused} = Listdemo.Table.load("Save", <<9, rest::binary>>)
Probe.check(refused.reason == "unknown_form", "a newer form says unknown_form: #{inspect(refused)}")
Probe.check(refused.malformed == false, "a refused form is not damage: #{inspect(refused)}")

# framing damage is wire_damaged, with the malformed flag beside it
{:error, :malformed, damaged} = Listdemo.Table.load("Save", binary_part(bytes, 0, byte_size(bytes) - 1))
Probe.check(damaged.reason == "wire_damaged", "a truncated wire says wire_damaged: #{inspect(damaged)}")
Probe.check(damaged.malformed == true, "wire_damaged sets malformed: #{inspect(damaged)}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}
