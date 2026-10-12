package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCardDescriptorsNameEveryField pins docs/SPEC-TABLES.md §8.1, §8.3: the
// generated descriptors of tables/cards name every field in declaration order
// with its kind, its shape (:value / :list / :blob) and its declared bound,
// against a vocabulary written here in the test and NOT read back from the
// descriptors. Adding a field to the declaration without updating this
// vocabulary reds the test, which is the gate the card's descriptor row stands
// on.
func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "cards", "Card.schema"))
	files := generateElixir(t, u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
# the pinned vocabulary: {table, field, kind, shape, bound} in declaration
# order, written here and never derived from the descriptors.
vocab = %{
  "PathText" => [{"s", 12, :value, 192}],
  "CardID" => [{"s", 12, :value, 64}],
  "KeyValue" => [{"key", 12, :value, 32}, {"value", 17, :blob, nil}],
  "Consumer" => [
    {"kind", 12, :value, 16},
    {"who", 12, :value, 32},
    {"route", 12, :value, 48},
    {"model", 12, :value, 64},
    {"started", 5, :value, nil},
    {"ended", 5, :value, nil},
    {"outcome", 12, :value, 16},
    {"line", 12, :value, 200},
    {"cost_micro_usd", 5, :value, nil}
  ],
  "Card" => [
    {"id", 12, :value, 64},
    {"sha", 12, :value, 40},
    {"tier", 30, :value, nil},
    {"repo", 12, :value, 96},
    {"_base", 12, :value, 96},
    {"kind", 12, :value, 24},
    {"priority", 30, :value, nil},
    {"stream", 12, :value, 32},
    {"paths", 14, :list, nil},
    {"_new", 14, :list, nil},
    {"shared", 14, :list, nil},
    {"needs", 14, :list, nil},
    {"test", 12, :value, 192},
    {"done", 17, :blob, nil},
    {"deadline_minutes", 4, :value, {0, 100000}},
    {"source", 17, :blob, nil},
    {"epoch", 4, :value, nil},
    {"column", 30, :value, nil},
    {"holder", 12, :value, 32},
    {"attempt", 4, :value, nil},
    {"head", 12, :value, 40},
    {"branch", 12, :value, 128},
    {"body", 17, :blob, nil},
    {"extra", 14, :list, nil},
    {"consumers", 14, :list, nil}
  ]
}
desc = Cards.Table.descriptors()
got = Map.new(desc, fn {name, d} ->
  {name, Enum.map(d.fields, fn f -> {f.name, f.kind, f.shape, f.bound} end)}
end)
Probe.check(got == vocab, "descriptors differ from the pinned vocabulary: #{inspect(got)}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}

// retainPair generates both sides of the pair under testdata/cards and runs the
// given probes against them, one BEAM per side because both declare `package
// cards`. It returns the round trip's output text.
func retainPair(t *testing.T, producer, roundTrip, verifier string) (string, string, string) {
	t.Helper()
	elixirBin := elixirBinary(t)
	td := filepath.Join("testdata", "cards")

	newU := loadUnit(t, filepath.Join(td, "newer.schema"))
	newFiles := generateElixir(t, newU)
	newDir := t.TempDir()
	newNames := writeModuleDir(t, newDir, newFiles)
	wirePath := filepath.Join(t.TempDir(), "populated.wire")
	runProbe(t, elixirBin, newDir, newNames, producer+`wire = Cards.Table.save("Card", %{"id" => "x", "n" => 7, "extra" => "hello", "future_i" => 42, "blob" => "world"})
case wire do
  {:ok, bytes} -> File.write!(`+elixirString(wirePath)+`, bytes)
  other -> IO.inspect(other, label: "save"); System.halt(1)
end
IO.puts("saved")
`)

	oldU := loadUnit(t, filepath.Join(td, "older.schema"))
	oldFiles := generateElixir(t, oldU)
	oldDir := t.TempDir()
	oldNames := writeModuleDir(t, oldDir, oldFiles)
	outPath := filepath.Join(t.TempDir(), "retained.wire")
	outText := runProbe(t, elixirBin, oldDir, oldNames, `wire = File.read!(`+elixirString(wirePath)+`)`+"\n"+roundTrip+`File.write!(`+elixirString(outPath)+`, out)
IO.puts("written")
`)

	verOut := runProbe(t, elixirBin, newDir, newNames, `wire = File.read!(`+elixirString(outPath)+`)`+"\n"+verifier)

	return outPath, outText, verOut
}

// TestCardConstructsRetainUnknownFields pins docs/SPEC-TABLES.md §6.6: the
// older declaration's retained load keeps the newer declaration's unknown
// scalar and string fields, re-emits them in a retained save byte-equal to the
// Go leg's, and the newer declaration rereads every retained value equal. The
// unknown *string (kind 17, a node index) and the node record it rode under are
// the excluded classes, reported as loss and never retained silently.
func TestCardConstructsRetainUnknownFields(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	producer := `defmodule Probe do end
`
	roundTrip := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, value, report, retain} = Cards.Table.load_retain("Card", wire, retain, report)
IO.puts("LOAD unknown=#{report.unknown} retained=#{report.retained} retain_lost=#{report.retain_lost}")
Probe.check(report.unknown == 4, "LOAD unknown: #{inspect(report)}")
Probe.check(report.retained == 2, "LOAD retained: #{inspect(report)}")
Probe.check(report.retain_lost == 2, "LOAD retain_lost: #{inspect(report)}")
size = Cards.Table.measure_retain("Card", value, retain)
{:ok, out, report, _retain} = Cards.Table.save_retain("Card", value, retain, report)
IO.puts("SAVE retained=#{report.retained} retain_lost=#{report.retain_lost} len=#{byte_size(out)}")
Probe.check(report.retained == 2 and report.retain_lost == 2, "SAVE counters: #{inspect(report)}")
`
	verifier := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
{:ok, value, report} = Cards.Table.load("Card", wire)
Probe.check(report.malformed == false and report.unknown == 0, "verify load: #{inspect(report)}")
Probe.check(value["id"] == "x", "id: #{inspect(value["id"])}")
Probe.check(value["n"] == 7, "n")
Probe.check(value["extra"] == "hello", "extra")
Probe.check(value["future_i"] == 42, "future_i")
Probe.check(value["blob"] == nil, "blob lost: #{inspect(value["blob"])}")
IO.puts("ok")
`

	outPath, outText, verOut := retainPair(t, producer, roundTrip, verifier)
	if !strings.Contains(verOut, "ok") {
		t.Errorf("the newer declaration did not reread the retained values equal:\n%s", verOut)
	}
	// the retained save is byte-equal to the Go leg's, committed as evidence
	// (retain-go.wire, produced by internal/codegen/gotable's generated code).
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "elixirtable", "testdata", "cards", "retain-go.wire"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the retained save differs from the Go leg's: got %d bytes, want %d bytes\n%s", len(got), len(want), outText)
	}
}

// retainRoundTrip runs the older declaration's retained round trip over a wire
// produced by the newer declaration, with the given byte and id capacities, and
// returns the probe output. The two sides run in separate BEAMs (both declare
// `package cards`).
func retainRoundTrip(t *testing.T, cap, idCap string) string {
	t.Helper()
	elixirBin := elixirBinary(t)
	td := filepath.Join("testdata", "cards")

	newU := loadUnit(t, filepath.Join(td, "newer.schema"))
	newDir := t.TempDir()
	newNames := writeModuleDir(t, newDir, generateElixir(t, newU))
	wirePath := filepath.Join(t.TempDir(), "populated.wire")
	runProbe(t, elixirBin, newDir, newNames, `wire = Cards.Table.save("Card", %{"id" => "x", "n" => 7, "extra" => "hello", "future_i" => 42, "blob" => "world"})
case wire do
  {:ok, bytes} -> File.write!(`+elixirString(wirePath)+`, bytes)
  _ -> System.halt(1)
end
IO.puts("saved")
`)

	oldU := loadUnit(t, filepath.Join(td, "older.schema"))
	oldDir := t.TempDir()
	oldNames := writeModuleDir(t, oldDir, generateElixir(t, oldU))
	probe := `wire = File.read!(` + elixirString(wirePath) + `)
retain = %{records: [], used: 0, cap: ` + cap + `, ids: [], id_cap: ` + idCap + `}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, value, report, retain} = Cards.Table.load_retain("Card", wire, retain, report)
IO.puts("LOAD unknown=#{report.unknown} retained=#{report.retained} retain_lost=#{report.retain_lost}")
size = Cards.Table.measure_retain("Card", value, retain)
{:ok, out, report, _retain} = Cards.Table.save_retain("Card", value, retain, report)
IO.puts("SAVE retained=#{report.retained} retain_lost=#{report.retain_lost} len=#{byte_size(out)}")
`
	return runProbe(t, elixirBin, oldDir, oldNames, probe)
}

// TestCardConstructsRetainStoreResets pins docs/SPEC-TABLES.md §6.6: a
// retention buffer belongs to one loaded region, so the next LoadRetain into it
// resets both stores (records and used bytes) and the id list, keeping only the
// caller's capacities. Reusing the same store across two loads must not keep
// the first load's records; the Go leg resets the same way
// (internal/codegen/gotable/retain.go emits retain.reset at load).
func TestCardConstructsRetainStoreResets(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	td := filepath.Join("testdata", "cards")

	newU := loadUnit(t, filepath.Join(td, "newer.schema"))
	newDir := t.TempDir()
	newNames := writeModuleDir(t, newDir, generateElixir(t, newU))
	wirePath := filepath.Join(t.TempDir(), "populated.wire")
	runProbe(t, elixirBin, newDir, newNames, `wire = Cards.Table.save("Card", %{"id" => "x", "n" => 7, "extra" => "hello", "future_i" => 42, "blob" => "world"})
case wire do
  {:ok, bytes} -> File.write!(`+elixirString(wirePath)+`, bytes)
  _ -> System.halt(1)
end
IO.puts("saved")
`)

	oldU := loadUnit(t, filepath.Join(td, "older.schema"))
	oldDir := t.TempDir()
	oldNames := writeModuleDir(t, oldDir, generateElixir(t, oldU))
	probe := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wirePath) + `)
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
report = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, _, _, retain1} = Cards.Table.load_retain("Card", wire, retain, report)
used1 = retain1.used
records1 = length(retain1.records)
ids1 = length(retain1.ids)
Probe.check(used1 > 0, "first load retained nothing")
{:ok, _, _, retain2} = Cards.Table.load_retain("Card", wire, retain1, report)
Probe.check(retain2.used == used1, "second load kept stale bytes: #{used1} -> #{retain2.used}")
Probe.check(length(retain2.records) == records1, "second load kept stale records: #{records1} -> #{length(retain2.records)}")
Probe.check(length(retain2.ids) == ids1, "second load kept stale ids: #{ids1} -> #{length(retain2.ids)}")
IO.puts("ok")
`
	out := runProbe(t, elixirBin, oldDir, oldNames, probe)
	if !strings.Contains(out, "ok") {
		t.Errorf("a reused retention store was not reset across loads:\n%s", out)
	}
}

// TestRetentionCapacityIsALoss pins docs/SPEC-TABLES.md §6.6: a retention buffer
// one byte short, or an id list one entry short, drops the record whole and
// counts one retain_lost, and the input bytes are never changed.
func TestRetentionCapacityIsALoss(t *testing.T) {
	t.Parallel()
	td := filepath.Join("testdata", "cards")
	wire, err := os.ReadFile(filepath.Join(td, "retain-newer.wire"))
	if err != nil {
		t.Fatal(err)
	}
	before := string(wire)

	short := retainRoundTrip(t, "3", "131072")
	if !strings.Contains(short, "retained=0") {
		t.Errorf("a buffer one byte short kept a record:\n%s", short)
	}
	if !strings.Contains(short, "retain_lost=4") {
		t.Errorf("a buffer one byte short mis-counted the loss:\n%s", short)
	}

	noIds := retainRoundTrip(t, "1048576", "0")
	if !strings.Contains(noIds, "LOAD unknown=4 retained=2 retain_lost=2") {
		t.Errorf("an id list one entry short changed the load counters:\n%s", noIds)
	}
	if !strings.Contains(noIds, "SAVE retained=2 retain_lost=4") {
		t.Errorf("the id list overflow was not reported as loss:\n%s", noIds)
	}

	after, err := os.ReadFile(filepath.Join(td, "retain-newer.wire"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Errorf("the input bytes changed across the retained round trips")
	}
}
