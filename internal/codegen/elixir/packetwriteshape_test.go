package elixir

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// packetShapeSchema declares one field of every kind whose Elixir term has a
// shape the writer must check before it reads the term: records (a field, an
// element, an arm payload, an empty record's payload), lists, a bool, a
// string, bytes and a flags value.
const packetShapeSchema = `package Shapepacket

flags Perm { Read, Write }

type Arm
{
    n int32 | min = -5, max = 5
}

type Nothing {}

union Pick
{
    arm   Arm
    blank Nothing
    ping
}

type Probe
{
    ok   bool
    a    Arm
    u    Pick
    xs   [..2]Arm
    fs   [2]int8
    s    string(4)
    y    bytes(4)
    p    Perm
}
`

// TestElixirPacketWriteRefusesEveryValueShape pins the Elixir writer's shape
// checks (SPEC §5.1; the dynamic legs refuse a bad value in every build):
// every term of the wrong shape is {:error, _} from write_x/1, never a
// MatchError, BadMapError, KeyError, ArithmeticError or a BEAM ArgumentError
// from length/1 or byte_size/1 past the verdict form, and never written. The
// lawful value writes and reads back.
func TestElixirPacketWriteRefusesEveryValueShape(t *testing.T) {
	t.Parallel()
	elixirBin, elixircBin := elixirToolchain(t)

	out, err := emitUnit(t, map[string]string{"Wire.schema": packetShapeSchema})
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

	cmd := testproc.Command(t, elixircBin, append([]string{"-o", ebin}, names...)...)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	compiled, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, compiled)
	}

	probe := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probe, []byte(packetShapeProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = testproc.Command(t, elixirBin, "-pa", ebin, probe)
	cmd.Dir = dir
	cmd.Env = toolchainEnv(elixirBin)
	run, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shape write probe: %v\n%s", err, run)
	}
	if !strings.Contains(string(run), "ok 21 shapes") {
		t.Fatalf("shape write probe did not report ok:\n%s", run)
	}
}

const packetShapeProbe = `
defmodule ShapeProbe do
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

alias Shapepacket.Wire, as: P

base = %Shapepacket.Probe{
  ok: true,
  a: %Shapepacket.Arm{n: 1},
  u: {:arm, %Shapepacket.Arm{n: 2}},
  xs: [%Shapepacket.Arm{n: 3}],
  fs: [1, 2],
  s: "ab",
  y: <<1, 2>>,
  p: 1
}

w = fn v -> fn -> P.write_probe(v) end end
refuse = fn v, label -> ShapeProbe.want(w.(v), :refused, label) end

ShapeProbe.want(w.(base), :wrote, "the lawful value writes")
ShapeProbe.want(w.(%{base | u: {:blank, %Shapepacket.Nothing{}}}), :wrote, "the empty arm writes")
refuse.(7, "a value that is not a Probe")
refuse.(%Shapepacket.Arm{n: 1}, "another record for the Probe")
refuse.(%{ok: true}, "a bare map for the Probe")
refuse.(%{base | a: 1}, "an integer for a record field")
refuse.(%{base | a: %{n: 1}}, "a bare map for a record field")
refuse.(%{base | u: {:arm, 1}}, "an integer arm payload")
refuse.(%{base | u: {:arm, %Shapepacket.Nothing{}}}, "another record as the arm payload")
refuse.(%{base | u: {:blank, 1}}, "an integer for an empty record's payload")
refuse.(%{base | u: {:ping, 1}}, "a payload on a void arm")
refuse.(%{base | xs: 1}, "an integer for a counted list")
refuse.(%{base | xs: [1]}, "an integer element of a record list")
refuse.(%{base | xs: [%Shapepacket.Arm{n: 1} | 2]}, "an improper list")
refuse.(%{base | fs: {1, 2}}, "a tuple for a fixed list")
refuse.(%{base | ok: 1}, "an integer for a bool")
refuse.(%{base | ok: nil}, "nil for a bool")
refuse.(%{base | s: 5}, "an integer for a string")
refuse.(%{base | y: [1, 2]}, "a list for bytes")
refuse.(%{base | y: <<1::3>>}, "a bitstring for bytes")
refuse.(%{base | p: 1.0}, "a float for a flags value")

{:ok, wire} = P.write_probe(base)

case P.read_probe(wire, bit_size(wire)) do
  {:ok, ^base} -> :ok
  other ->
    IO.puts(:stderr, "FAIL: the lawful value did not read back: " <> inspect(other))
    System.halt(1)
end

IO.puts("ok 21 shapes")
`
