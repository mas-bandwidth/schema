package elixirtable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirBitsPastWidthRefuses pins docs/SPEC-TABLES.md §3 and §4 on the
// Elixir variable form: a bits(N) field rides as the narrowest unsigned kind
// that holds it, so its payload can carry a value above 2^N - 1. THE READ NEVER
// CLAMPS: the whole load refuses out_of_range, naming the field, and the
// retaining read refuses it too. The old read accepted the over-wide value (PR
// #2775 clamped it to the top of the field), so the probe is red on that code.
//
// It runs elixir, so it is a functional test: make tables-elixir-... runs it
// under SCHEMA_REQUIRE_CORPUS=1, where a missing toolchain fails by name;
// without that variable and without Elixir it skips by name (elixirBinary).
func TestElixirBitsPastWidthRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := bitsPastWidthProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("bits(N) past-width probe: %v\n%s", err, out)
	}
}

// TestElixirBitsPastWidthRefusesNegativeControl is the reversed witness: the
// emitted width guard is removed, and the same probe must go red on a check.
func TestElixirBitsPastWidthRefusesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := bitsPastWidthProbe(t)
	elixirPatch(t, files, bitsClampAnchor, "    {v, report}")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: removing the bits(N) width guard left every row green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: ") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on a check: %v\n%s", err, out)
	}
}

// bitsClampAnchor is the one width guard the variable runtime emits, at its
// exact indentation.
const bitsClampAnchor = "    if v > Bitwise.bsl(1, bits) - 1,\n" +
	"      do: throw({:out_of_range, name}),\n" +
	"      else: {v, report}"

const bitsPastWidthProbeSource = `
defmodule BitsProbe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def scalar(wire, want, label) do
    case Bitspastwidth.Table.load("Root", wire) do
      {:ok, v, _report} -> check(v["mask"] == want, label <> ": got " <> inspect(v))
      other -> check(false, label <> ": refused " <> inspect(other))
    end
  end

  def refused(wire, label) do
    case Bitspastwidth.Table.load("Root", wire) do
      {:error, :out_of_range, report} ->
        check(report.reason == "out_of_range: mask", label <> ": reason " <> inspect(report))

      other ->
        check(false, label <> ": got " <> inspect(other))
    end
  end

  def refused_retain(wire, label) do
    store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 512}
    report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}

    case Bitspastwidth.Table.load_retain("Root", wire, store, report) do
      {:error, :out_of_range, report, _retain} ->
        check(report.reason == "out_of_range: mask", label <> ": reason " <> inspect(report))

      other ->
        check(false, label <> ": got " <> inspect(other))
    end
  end

  def refused_json(text, label) do
    case Bitspastwidth.Table.from_json("Root", text) do
      {:error, :malformed, _report} ->
        :ok

      other ->
        check(false, label <> ": got " <> inspect(other))
    end
  end
end

CASES
IO.puts("ok")
`

func bitsPastWidthProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, `package bitspastwidth

table Root
{
    mask bits(6)
}
`)
	files := generateElixir(t, u)

	var cases strings.Builder

	scalar := func(value byte, label string) {
		fmt.Fprintf(&cases, "BitsProbe.scalar(%s, %d, %s)\n",
			elixirBitsWire(bitsPastWidthWire(value)), value, elixirString(label))
	}
	scalar(0, "bits(6) zero reads")
	scalar(63, "bits(6) at its top reads")

	for _, value := range []byte{64, 154, 255} {
		fmt.Fprintf(&cases, "BitsProbe.refused(%s, %s)\n",
			elixirBitsWire(bitsPastWidthWire(value)),
			elixirString(fmt.Sprintf("bits(6) value %d past its width refuses", value)))
	}

	fmt.Fprintf(&cases, "BitsProbe.refused_retain(%s, %s)\n",
		elixirBitsWire(bitsPastWidthWire(154)),
		elixirString("bits(6) value 154 past its width refuses retaining"))

	fmt.Fprintf(&cases, "BitsProbe.refused_json(%s, %s)\n",
		elixirString("{\"mask\": 64}"),
		elixirString("bits(6) value 64 past its width in JSON refuses"))

	probe := strings.ReplaceAll(bitsPastWidthProbeSource, "CASES", cases.String())
	return files, probe
}

// bitsPastWidthWire is a one-field variable-form root: form byte 1, one field
// entry for `mask` under the u8 kind that holds bits(6), the body terminator,
// the id table and its count.
func bitsPastWidthWire(value byte) []byte {
	wire := []byte{1, 1, byte(ir.TableKindU8), value, 0}
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("mask"))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

func elixirBitsWire(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return "<<" + strings.Join(parts, ",") + ">>"
}
