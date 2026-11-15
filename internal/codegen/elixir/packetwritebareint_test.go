package elixir

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestElixirPacketWriteRefusesBareIntOutsideWidth holds SPEC §5.1 row 1 on
// the packet writer where the shared cases list does not reach: a bare
// integer outside its declared width, or a value that is not an integer, is
// refused as an array element and as a union arm's payload, and each width's
// boundary writes and reads back. The probe checks the verdict only.
func TestElixirPacketWriteRefusesBareIntOutsideWidth(t *testing.T) {
	t.Parallel()
	elixirBin, elixircBin := elixirToolchain(t)

	out, err := emitUnit(t, map[string]string{
		"BareElems.schema": `package Bareelems

type Cell
{
    value int8
}

type Row
{
    cells [2]Cell
    counts [2]uint16
}

union Arm
{
    cell Cell
}

type Holder
{
    arm Arm
}
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	dir := t.TempDir()
	ebin := filepath.Join(dir, "ebin")
	if err := os.MkdirAll(ebin, 0o700); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name, data := range out {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	cmd := exec.Command(elixircBin, append([]string{"-o", ebin}, names...)...)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	compiled, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, compiled)
	}

	probe := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probe, []byte(packetWriteBareIntProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(elixirBin, "-pa", ebin, probe)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	run, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bare integer write probe: %v\n%s", err, run)
	}
	if !strings.Contains(string(run), "ok") {
		t.Fatalf("bare integer write probe did not report ok:\n%s", run)
	}
}

// packetWriteBareIntProbe is the verdict witness: a refusal is {:error, _}
// from the non-bang writer, a write is {:ok, wire} that reads back the value.
const packetWriteBareIntProbe = `
defmodule BareIntProbe do
  def refused(result, label) do
    case result do
      {:error, _} ->
        :ok

      other ->
        IO.puts(:stderr, "FAIL: " <> label <> " wrote " <> inspect(other))
        System.halt(1)
    end
  end

  def roundtrip(result, read, want, label) do
    case result do
      {:ok, wire} ->
        case read.(wire) do
          {:ok, ^want, _} ->
            :ok

          other ->
            IO.puts(:stderr, "FAIL: " <> label <> " read " <> inspect(other))
            System.halt(1)
        end

      other ->
        IO.puts(:stderr, "FAIL: " <> label <> " refused " <> inspect(other))
        System.halt(1)
    end
  end
end

alias Bareelems.BareElems, as: B
alias Bareelems.{Cell, Holder, Row}

row = fn cells, counts -> %Row{cells: Enum.map(cells, &%Cell{value: &1}), counts: counts} end

BareIntProbe.refused(B.write_row(row.([0, 128], [0, 0])), "array int8 element 128")
BareIntProbe.refused(B.write_row(row.([-129, 0], [0, 0])), "array int8 element -129")
BareIntProbe.refused(B.write_row(row.([0, 0], [65_536, 0])), "array uint16 element 65536")
BareIntProbe.refused(B.write_row(row.([0, 0], [0, -1])), "array uint16 element -1")
BareIntProbe.refused(B.write_row(row.([0, 0], [0, 1.5])), "array uint16 element 1.5")

BareIntProbe.roundtrip(
  B.write_row(row.([-128, 127], [0, 65_535])),
  fn wire -> B.read_row_bits(wire, B.row_max_bits()) end,
  row.([-128, 127], [0, 65_535]),
  "array boundaries"
)

BareIntProbe.refused(B.write_holder(%Holder{arm: {:cell, %Cell{value: 200}}}), "arm int8 200")
BareIntProbe.refused(B.write_holder(%Holder{arm: {:cell, %Cell{value: -129}}}), "arm int8 -129")
BareIntProbe.refused(B.write_holder(%Holder{arm: {:cell, %Cell{value: 0.5}}}), "arm int8 0.5")

BareIntProbe.roundtrip(
  B.write_holder(%Holder{arm: {:cell, %Cell{value: -128}}}),
  fn wire -> B.read_holder_bits(wire, B.holder_max_bits()) end,
  %Holder{arm: {:cell, %Cell{value: -128}}},
  "arm int8 -128"
)

IO.puts("ok")
`
