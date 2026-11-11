package elixirtable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
// list_scalar_widen.bin respells an int32 element kind to int8 and leaves the
// wider slots inside the array's L: the bytes left there are damage (§3, §4),
// and both the plain and the strict reads refuse it. The lawful widening is
// driven from a writer whose scores are genuinely int8: the value lands at the
// wire width and one widened counts.
func TestWideningReasonsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "lists", "Save.schema"))
	reader := tabletext.NewModel(u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, generateElixir(t, u))
	damaged, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var damagedReport tabletext.Report
	if ok, e := tablewire.Decode(reader, reader.New(u.Tables["Save"]), damaged, &damagedReport); ok || !tablewire.ReadRefusal(e) {
		t.Fatalf("list_scalar_widen.bin is not a refusal on the engine: ok=%v err=%v report=%+v", ok, e, damagedReport)
	}

	writer := unitFrom(t, `package listdemo
table Save
{
    scores []int8
}`)
	writerModel := tabletext.NewModel(writer)
	writerInst := writerModel.New(writerModel.Lookup("Save"))
	var writerReport tabletext.Report
	if !writerModel.Read(writerInst, []byte(`{"scores":[10,20,30]}`), &writerReport) || writerReport.Refused {
		t.Fatalf("the narrow writer text did not place: %+v", writerReport)
	}
	widened, err := tablewire.Encode(writerModel, writerInst)
	if err != nil {
		t.Fatal(err)
	}
	var widenedReport tabletext.Report
	if ok, e := tablewire.Decode(reader, reader.New(u.Tables["Save"]), widened, &widenedReport); !ok || e != nil || widenedReport.Refused || widenedReport.Widened != 1 {
		t.Fatalf("the genuine widening is not a clean read on the engine: ok=%v err=%v report=%+v", ok, e, widenedReport)
	}

	probe := `
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

# the narrower writer through the newer reader: widened, landed, a read
bytes = Base.decode16!("` + hex.EncodeToString(widened) + `", case: :lower)
{:ok, value, report} = Listdemo.Table.load("Save", bytes)
Probe.check(value["scores"] == [10, 20, 30], "the narrow scores land exactly: #{inspect(value)}")
Probe.check(report.widened == 1, "one widening is counted: #{inspect(report)}")
Probe.check(Map.has_key?(report, :reason), "the report carries a reason: #{inspect(report)}")
Probe.check(report.reason == "", "a widened read finished, so its reason is empty: #{inspect(report)}")
Probe.check(report.unknown == 0, "a widening is no other event: #{inspect(report)}")

# bytes left inside the narrowed array's L are damage (§3, §4): both reads refuse
damaged = Base.decode16!("` + hex.EncodeToString(damaged) + `", case: :lower)
for {name, result} <- [{"load", Listdemo.Table.load("Save", damaged)}, {"variable_load", Listdemo.Table.variable_load("Save", damaged)}] do
  case result do
    {:error, reason, refused} when is_atom(reason) ->
      Probe.check(refused.reason != "", name <> " refusal carries no reason: #{inspect(refused)}")

    other ->
      Probe.check(false, name <> " landed the damaged wire: #{inspect(other)}")
  end
end

# a form byte this reader does not carry is a refusal, never damage (§3)
<<_form, rest::binary>> = bytes
{:error, :newer_form, refused} = Listdemo.Table.load("Save", <<9, rest::binary>>)
Probe.check(refused.reason == "unknown_form", "a newer form says unknown_form: #{inspect(refused)}")
Probe.check(true, "a refused form is not damage: #{inspect(refused)}")

# framing damage is wire_damaged, with the malformed flag beside it
{:error, :malformed, trunc} = Listdemo.Table.load("Save", binary_part(bytes, 0, byte_size(bytes) - 1))
Probe.check(trunc.reason == "wire_damaged", "a truncated wire says wire_damaged: #{inspect(trunc)}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}
