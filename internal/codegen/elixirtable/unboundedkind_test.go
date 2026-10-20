package elixirtable

// TestUnboundedListKindMismatchElixir pins docs/SPEC-TABLES.md §3, §3.3's
// framing rule and §4's kind_mismatch on the listdemo.Unbounded mutant the
// Elixir wire fuzzer found (run seed 24845619678, pass kind #71). The root's
// items list carries a Unit element whose body terminates before its own L
// ends: the bytes the walk did reach name Unit.v under a bool, which is one
// kind_mismatch, and the unclaimed tail is malformed. The generated Elixir
// reader once dropped the whole prefix on the early terminator, so it counted
// the damage and not the mismatch; the reference counts both. The test embeds
// the mutant, feeds it to the generated Elixir and holds every report counter
// and the saved bytes to internal/tablewire.
//
// This test runs Elixir, so it is a functional test: the gate runs it under
// SCHEMA_REQUIRE_CORPUS=1, where a missing toolchain fails by name, and
// without that variable and without Elixir it skips by name (elixirBinary).

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const elixirUnboundedKindMutantHex = "01010e1a0d0307020107000000000702040e00000000070204150000000003042a000000006f2c414fbf84783e090602864ceb63aff3a4484419abd7560300000000000000"

func TestUnboundedListKindMismatchElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)

	wire, err := hex.DecodeString(elixirUnboundedKindMutantHex)
	if err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "lists", "Migrate.schema"))
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Unbounded"))

	var want tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &want); err != nil {
		t.Fatalf("oracle decode: %v", err)
	}
	wantWire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
	}

	files := generateElixir(t, u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	wirePath := filepath.Join(dir, "mutant.bin")
	if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
		t.Fatal(err)
	}

	probe := fmt.Sprintf(`
defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end

wire = File.read!(%s)
{:ok, value, report} = Listdemo.Table.load("Unbounded", wire)
Probe.check(report.unknown == %d, "unknown elixir #{report.unknown}, oracle %d")
Probe.check(report.kind_mismatch == %d, "kind_mismatch elixir #{report.kind_mismatch}, oracle %d")
Probe.check(report.widened == %d, "widened elixir #{report.widened}, oracle %d")
Probe.check(report.clamped == %d, "clamped elixir #{report.clamped}, oracle %d")
Probe.check(report.malformed == %t, "malformed elixir #{report.malformed}, oracle %t")
Probe.check(report.retained == %d, "retained elixir #{report.retained}, oracle %d")
Probe.check(report.retain_lost == %d, "retain_lost elixir #{report.retain_lost}, oracle %d")
{:ok, out} = Listdemo.Table.save("Unbounded", value)
saved = Base.encode16(out, case: :lower)
Probe.check(saved == %s, "saved elixir #{saved}, oracle %s")
`,
		elixirString(wirePath),
		want.Unknown, want.Unknown,
		want.KindMismatch, want.KindMismatch,
		want.Widened, want.Widened,
		want.Clamped, want.Clamped,
		want.Malformed, want.Malformed,
		want.Retained, want.Retained,
		want.RetainLost, want.RetainLost,
		elixirString(hex.EncodeToString(wantWire)), hex.EncodeToString(wantWire),
	)
	runProbe(t, elixirBin, dir, names, probe)
}
