package elixir

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestElixirPacketWriteRefusesWideBits holds SPEC §5.1 row 2 on the packet
// writer: a bits(N) value outside [0, 2^N-1] is refused, as a lone field and
// inside a packed run, and as an array element. 127 into bits(7) writes and
// reads back 127. The same check is not elided at 32 or 64 bits. The probe
// checks the verdict only — {:error, _} against {:ok, _} — never the
// exception text.
func TestElixirPacketWriteRefusesWideBits(t *testing.T) {
	t.Parallel()
	elixirBin, elixircBin := elixirToolchain(t)

	out, err := emitUnit(t, map[string]string{
		"Bits.schema": `package Bitspacket

type Lone
{
    mask bits(7)
}

type Packed
{
    lead bits(3)
    mask bits(7)
    tail bits(6)
}

type Cell
{
    mask bits(7)
}

type Row
{
    cells [2]Cell
}

type Wide32
{
    mask bits(32)
}

type Wide64
{
    mask bits(64)
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
	if err := os.WriteFile(probe, []byte(packetWriteWideBitsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(elixirBin, "-pa", ebin, probe)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	run, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wide bits write probe: %v\n%s", err, run)
	}
	if !strings.Contains(string(run), "ok") {
		t.Fatalf("wide bits write probe did not report ok:\n%s", run)
	}
}

// packetWriteWideBitsProbe is the verdict witness. A refusal is {:error, _}
// from the non-bang writer; a write is {:ok, wire} that reads back the value.
// Nothing here matches the exception text.
const packetWriteWideBitsProbe = `
defmodule PacketWriteProbe do
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

alias Bitspacket.Bits

PacketWriteProbe.refused(
  Bits.write_lone(%Bitspacket.Lone{mask: 200}),
  "lone bits(7) value 200"
)

PacketWriteProbe.refused(
  Bits.write_lone(%Bitspacket.Lone{mask: -1}),
  "lone bits(7) value -1"
)

PacketWriteProbe.roundtrip(
  Bits.write_lone(%Bitspacket.Lone{mask: 127}),
  fn wire -> Bits.read_lone_bits(wire, Bits.lone_max_bits()) end,
  %Bitspacket.Lone{mask: 127},
  "lone bits(7) value 127"
)

PacketWriteProbe.refused(
  Bits.write_packed(%Bitspacket.Packed{lead: 5, mask: 200, tail: 9}),
  "packed bits(7) value 200"
)

PacketWriteProbe.refused(
  Bits.write_packed(%Bitspacket.Packed{lead: 5, mask: -1, tail: 9}),
  "packed bits(7) value -1"
)

PacketWriteProbe.roundtrip(
  Bits.write_packed(%Bitspacket.Packed{lead: 5, mask: 127, tail: 9}),
  fn wire -> Bits.read_packed_bits(wire, Bits.packed_max_bits()) end,
  %Bitspacket.Packed{lead: 5, mask: 127, tail: 9},
  "packed bits(7) value 127"
)

PacketWriteProbe.refused(
  Bits.write_row(%Bitspacket.Row{cells: [%Bitspacket.Cell{mask: 0}, %Bitspacket.Cell{mask: 200}]}),
  "array bits(7) value 200"
)

PacketWriteProbe.refused(
  Bits.write_row(%Bitspacket.Row{cells: [%Bitspacket.Cell{mask: -1}, %Bitspacket.Cell{mask: 0}]}),
  "array bits(7) value -1"
)

PacketWriteProbe.roundtrip(
  Bits.write_row(%Bitspacket.Row{cells: [%Bitspacket.Cell{mask: 127}, %Bitspacket.Cell{mask: 0}]}),
  fn wire -> Bits.read_row_bits(wire, Bits.row_max_bits()) end,
  %Bitspacket.Row{cells: [%Bitspacket.Cell{mask: 127}, %Bitspacket.Cell{mask: 0}]},
  "array bits(7) value 127"
)

top32 = 4_294_967_295
PacketWriteProbe.refused(
  Bits.write_wide32(%Bitspacket.Wide32{mask: top32 + 1}),
  "bits(32) value 2^32"
)

PacketWriteProbe.refused(
  Bits.write_wide32(%Bitspacket.Wide32{mask: -1}),
  "bits(32) value -1"
)

PacketWriteProbe.roundtrip(
  Bits.write_wide32(%Bitspacket.Wide32{mask: top32}),
  fn wire -> Bits.read_wide32_bits(wire, Bits.wide32_max_bits()) end,
  %Bitspacket.Wide32{mask: top32},
  "bits(32) value 2^32-1"
)

top64 = 18_446_744_073_709_551_615
PacketWriteProbe.refused(
  Bits.write_wide64(%Bitspacket.Wide64{mask: top64 + 1}),
  "bits(64) value 2^64"
)

PacketWriteProbe.refused(
  Bits.write_wide64(%Bitspacket.Wide64{mask: -1}),
  "bits(64) value -1"
)

PacketWriteProbe.roundtrip(
  Bits.write_wide64(%Bitspacket.Wide64{mask: top64}),
  fn wire -> Bits.read_wide64_bits(wire, Bits.wide64_max_bits()) end,
  %Bitspacket.Wide64{mask: top64},
  "bits(64) value 2^64-1"
)

IO.puts("ok")
`
