package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirWriteContractRefuses pins docs/SPEC.md §5.1 on the Elixir leg,
// which has no compile-out assert and so refuses in every build. The fixed
// writer (FixedSave, here the fixed root's Table.save) raises ArgumentError
// for a bits(N) value past its N bits and for a string that is ill-formed or
// holds a zero byte, which the fixed reader would refuse as text_ill_formed.
// The message writer (save_messages) answers {:error, :invalid_value} for an
// integer off its declared range or its bits(N), a string past its bound,
// ill-formed or holding a zero byte, and an enum reference that names no
// member, and never masks one into the width. Each refusal has its lawful
// twin at the same boundary, which writes.
//
// It runs elixir, so it is a functional test: without the toolchain it skips
// by name (elixirBinary), and SCHEMA_REQUIRE_CORPUS=1 makes that a failure.
func TestElixirWriteContractRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := writeContractProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("write contract probe: %v\n%s", err, out)
	}
}

// TestElixirWriteContractRefusesNegativeControl is the reversed witness: the
// message writer's integer rule loses its range, and the probe must go red on
// the message bits(6) row, the first integer row past its domain.
func TestElixirWriteContractRefusesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := writeContractProbe(t)
	elixirPatch(t, files, "check(is_integer(value) and value >= lo and value <= hi)", "check(is_integer(value) and lo <= hi)")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a message writer with no range check left every row green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: message bits(6) past its top refuses") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the range row: %v\n%s", err, out)
	}
}

const writeContractProbeSource = `
defmodule WCProbe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  @base %{"qty" => 5, "code" => "ab", "coin" => "EUR", "mask" => 3}

  def fixed(field, value, want, label) do
    got =
      try do
        case Writecontract.Table.save("Order", Map.put(@base, field, value)) do
          {:ok, _bytes} -> :wrote
          {:error, _reason} -> :refused
        end
      rescue
        ArgumentError -> :refused
      end

    check(got == want, label <> ": got " <> inspect(got))
  end

  def message(field, value, want, label) do
    m = Writecontract.TableMessage
    {:ok, vocab} = m.announce_read(m.announce())
    desc = Writecontract.Table.descriptors()["Order"]
    ref = fn id -> Enum.find_index(vocab.entries, &(&1.id == id)) + 1 end
    id = fn name -> Enum.find(desc.fields, &(&1.name == name)).id end
    coin = Enum.find(desc.fields, &(&1.name == "coin"))

    body =
      for {name, v} <- Map.put(@base, field, value) do
        case {name, v} do
          {"coin", {:field_ref, other}} -> {ref.(id.("coin")), ref.(id.(other))}
          {"coin", member} -> {ref.(id.("coin")), ref.(Map.fetch!(coin.by_name, member))}
          _ -> {ref.(id.(name)), v}
        end
      end

    got =
      case m.save_messages(vocab, [body]) do
        {:ok, _bytes} -> :wrote
        {:error, :invalid_value} -> :refused
        other -> other
      end

    check(got == want, label <> ": got " <> inspect(got))
  end
end

CASES
IO.puts("ok")
`

func writeContractProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, `package writecontract

enum Coin { USD, EUR }

fixed table Order
{
    qty  uint8 = 1 | min = 1, max = 99
    code string(8)
    coin Coin
    mask bits(6)
}
`)
	files := generateElixir(t, u)

	var cases strings.Builder
	row := func(writer, field, value, want, label string) {
		fmt.Fprintf(&cases, "WCProbe.%s(%q, %s, :%s, %s)\n", writer, field, value, want, elixirString(writer+" "+label))
	}
	for _, writer := range []string{"fixed", "message"} {
		row(writer, "mask", "63", "wrote", "bits(6) at its top writes")
		row(writer, "mask", "64", "refused", "bits(6) past its top refuses")
		row(writer, "code", "<<0x61, 0xC3, 0xA9, 0x62>>", "wrote", "well-formed text writes")
		row(writer, "code", "<<0x61, 0xFF, 0x62>>", "refused", "ill-formed text refuses")
		row(writer, "code", "<<0x61, 0x00, 0x62>>", "refused", "a zero byte refuses")
		row(writer, "qty", "99", "wrote", "qty 99 at its max writes")
		row(writer, "qty", "100", "refused", "qty 100 past its max refuses")
		row(writer, "qty", "0", "refused", "qty 0 under its min refuses")
	}
	row("message", "code", `"aaaaaaaa"`, "wrote", "text at its bound writes")
	row("message", "code", `"aaaaaaaaa"`, "refused", "text past its bound refuses")
	row("message", "coin", `"USD"`, "wrote", "an enum member writes")
	row("message", "coin", `{:field_ref, "qty"}`, "refused", "a reference to a field and not a member refuses")

	return files, strings.ReplaceAll(writeContractProbeSource, "CASES", cases.String())
}

// TestElixirJSONDepthCountsContainers pins docs/SPEC-TABLES.md §16.2 on the
// Elixir text reader: the root container sits at depth 0 and a CONTAINER past
// depth 128 is refused, so a pointer chain whose deepest node sits at depth
// 128 reads, scalar members and all, and one node more refuses. The old reader
// counted a scalar member as a level, so the deepest node's own fields were
// past the cap and graph_chain_text answered absent.
func TestElixirJSONDepthCountsContainers(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := jsonDepthProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("JSON depth probe: %v\n%s", err, out)
	}
}

// TestElixirJSONDepthCountsContainersNegativeControl puts the old cap back,
// which counts every value: the chain at the cap must then refuse.
func TestElixirJSONDepthCountsContainersNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := jsonDepthProbe(t)
	elixirPatch(t, files, "<<c, _::binary>> when c in [?{, ?[] and depth > 128 -> :error", "_ when depth > 128 -> :error")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a cap that counts scalars left every row green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: a chain whose deepest node is at depth 128 reads") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the chain at the cap: %v\n%s", err, out)
	}
}

const jsonDepthProbeSource = `
defmodule DepthProbe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def chain(nodes) do
    Enum.reduce(nodes..1//-1, nil, fn i, inner ->
      next = if inner == nil, do: "", else: ", \"next\": " <> inner
      "{\"value\": " <> Integer.to_string(i) <> next <> "}"
    end)
  end

  def reads(nodes, label) do
    got = Jsondepth.Table.from_json("Node", chain(nodes))
    check(match?({:ok, _, _}, got), label <> ": got " <> inspect(got, limit: 3))
  end

  def refused(nodes, label) do
    got = Jsondepth.Table.from_json("Node", chain(nodes))
    check(match?({:error, _, _}, got), label <> ": got " <> inspect(got, limit: 3))
  end
end

DepthProbe.reads(129, "a chain whose deepest node is at depth 128 reads")
DepthProbe.refused(130, "a chain whose deepest node is at depth 129 refuses")
IO.puts("ok")
`

func jsonDepthProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, `package jsondepth

table Node
{
    value int32
    next  *Node
}
`)
	return generateElixir(t, u), jsonDepthProbeSource
}
