package elixir

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const packetNonIntegerSchema = `package Nonintpacket

type Probe
{
    version int32 | min = 0, max = 7
    mask    bits(6)
    ratio   fixed(8,8) | min = -2, max = 3
    locked  int32 = 5 | min = 5, max = 5
}
`

// TestElixirPacketWriteRefusesNonInteger pins ELIXIR-3 of the 2026-11-21
// review on the packet writer (docs/SPEC.md §5.1, the Elixir packet Write
// line: "1–8 and 10 every build (ArgumentError)"). A ranged integer, a
// bits(N) and a fixed raw value were held by comparisons alone, and `3.0 < 0`
// and `3.0 > 7` are both false on the BEAM, so a float passed the range and
// raised ArithmeticError in the bit arithmetic after it, past the write_x/1
// verdict form, which answers ArgumentError's refusal only. A degenerate
// range compared with `!=`, under which 5.0 is the locked 5. Each now refuses
// a non-integer with {:error, _}, and each lawful twin writes and reads back.
func TestElixirPacketWriteRefusesNonInteger(t *testing.T) {
	t.Parallel()
	elixirBin, elixircBin := elixirToolchain(t)

	out, err := emitUnit(t, map[string]string{"Wire.schema": packetNonIntegerSchema})
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
	if err := os.WriteFile(probe, []byte(packetNonIntegerProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(elixirBin, "-pa", ebin, probe)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	run, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("non-integer write probe: %v\n%s", err, run)
	}
	if !strings.Contains(string(run), "ok") {
		t.Fatalf("non-integer write probe did not report ok:\n%s", run)
	}
}

// TestElixirPacketWriteEmitsTheIntegerCheck is the fast-lane half: the
// emitted writer refuses a non-integer before every range and bits(N) check,
// and a degenerate range compares with exact inequality.
func TestElixirPacketWriteEmitsTheIntegerCheck(t *testing.T) {
	t.Parallel()
	out, err := emitUnit(t, map[string]string{"Wire.schema": packetNonIntegerSchema})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var all strings.Builder
	for name, data := range out {
		if strings.HasSuffix(name, ".ex") {
			all.Write(data)
		}
	}
	text := all.String()
	for _, field := range []string{"version", "mask", "ratio"} {
		if !strings.Contains(text, "not is_integer(value_"+field+")") {
			t.Errorf("the writer of %s does not refuse a non-integer first", field)
		}
	}
	if !strings.Contains(text, "value_locked !== 5") {
		t.Errorf("the degenerate range does not compare with exact inequality")
	}
	if strings.Contains(text, "value_locked != 5") {
		t.Errorf("the degenerate range still compares with !=, under which 5.0 is 5")
	}
}

const packetNonIntegerProbe = `
defmodule NonIntProbe do
  def verdict(fun) do
    try do
      case fun.() do
        {:error, _} -> :refused
        {:ok, _} -> :wrote
        other -> {:other, other}
      end
    rescue
      e -> {:raised, e.__struct__}
    end
  end

  def want(fun, want, label) do
    got = verdict(fun)

    if got != want do
      IO.puts(:stderr, "FAIL: " <> label <> ": got " <> inspect(got))
      System.halt(1)
    end
  end
end

alias Nonintpacket.Wire, as: P

base = %Nonintpacket.Probe{version: 3, mask: 9, ratio: 256, locked: 5}
w = fn v -> fn -> P.write_probe(v) end end

NonIntProbe.want(w.(base), :wrote, "the lawful value writes")
NonIntProbe.want(w.(%{base | version: 3.0}), :refused, "a float in a ranged int refuses")
NonIntProbe.want(w.(%{base | mask: 9.0}), :refused, "a float in a bits(6) refuses")
NonIntProbe.want(w.(%{base | ratio: 256.0}), :refused, "a float raw in a fixed refuses")
NonIntProbe.want(w.(%{base | locked: 5.0}), :refused, "5.0 in a locked int refuses")
NonIntProbe.want(w.(%{base | version: 8}), :refused, "a ranged int past its max refuses")

{:ok, wire} = P.write_probe(base)

case P.read_probe_bits(wire, P.probe_max_bits()) do
  {:ok, ^base, _} -> :ok
  other ->
    IO.puts(:stderr, "FAIL: the lawful value did not read back: " <> inspect(other))
    System.halt(1)
end

IO.puts("ok")
`
