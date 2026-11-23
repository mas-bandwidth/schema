package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirVariableSaveRefusesOffContract pins docs/SPEC.md §5.1 on the
// Elixir variable form (ELIXIR-1, ELIXIR-2 of the 2026-11-21 review). Elixir
// refuses an off-declaration value in every build (ruling write-refusal), and
// the variable save/2 answers {:error, :invalid_value}: never raising, never
// truncating into the width, never cutting an over-long [N]T. The probe puts
// each row on a PLAIN variable table, no field `required`, because the old
// writer checked only `required` fields. The JSON writer (to_json/2) runs the
// same check, so an enum name outside the variant set and a ranged float off
// its range refuse there too. Each refusal has its lawful twin at the same
// boundary, which writes.
//
// It also pins three read rules the same review found on this leg's variable
// and JSON reads: a float32 range is held at float32 (so 0.1 under max = 0.1
// writes and reads back), a counted array below its Min is refused and an
// absent one lands at its birth count, and a compressed float's -0 is +0 on
// read and on write (docs/SPEC-TABLES.md §3.3, ruling fixed-compressed-negzero).
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestElixirVariableSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := variableSaveProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("variable save probe: %v\n%s", err, out)
	}
}

// TestElixirVariableSaveRefusesOffContractNegativeControl is the reversed
// witness: the write check loses its integer range, and the probe must go red
// on the ranged int row.
func TestElixirVariableSaveRefusesOffContractNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := variableSaveProbe(t)
	elixirPatch(t, files, "%{min: lo, max: hi} when is_number(v) and (v < lo or v > hi) -> throw(:invalid_value)", "%{min: lo, max: hi} when is_number(v) and lo > hi -> throw(:invalid_value)")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a variable writer with no range check left every row green\n%s", out)
	}
	if !strings.Contains(out, "FAIL: save penetration 11 past its max refuses") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the range row: %v\n%s", err, out)
	}
}

const variableSaveProbeSource = `
defmodule VSProbe do
  @t Vsave.Table
  @fails :fails

  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    :counters.add(:persistent_term.get(@fails), 1, 1)
  end

  def verdict(fun) do
    try do
      case fun.() do
        {:ok, _} -> :wrote
        {:error, :invalid_value} -> :refused
        {:error, :non_finite_float} -> :refused
        other -> {:other, other}
      end
    rescue
      e -> {:raised, e.__struct__}
    catch
      kind, what -> {:escaped, kind, what}
    end
  end

  def save(field, value, want, label) do
    got = verdict(fn -> @t.save("Tv", %{field => value}) end)
    check(got == want, "save " <> label <> ": got " <> inspect(got))
  end

  def json(field, value, want, label) do
    got = verdict(fn -> @t.to_json("Tv", %{field => value}) end)
    check(got == want, "json " <> label <> ": got " <> inspect(got))
  end

  def reads(bytes, field, want, label) do
    got =
      case @t.load("Tv", bytes) do
        {:ok, v, _report} -> {:ok, Map.get(v, field)}
        {:error, _reason, _report} -> :refused
      end

    check(got === want, "load " <> label <> ": got " <> inspect(got))
  end

  def json_reads(text, field, want, label) do
    got =
      case @t.from_json("Tv", text) do
        {:ok, v, _report} -> {:ok, Map.get(v, field)}
        {:error, _reason, _report} -> :refused
      end

    check(got === want, "from_json " <> label <> ": got " <> inspect(got))
  end

  def bytes!(value) do
    {:ok, bytes} = @t.save("Tv", value)
    bytes
  end

  def swap!(bytes, from, to) do
    check(:binary.matches(bytes, from) |> length() == 1, "the planted pattern occurs once")
    :binary.replace(bytes, from, to)
  end

  def done do
    n = :counters.get(:persistent_term.get(@fails), 1)
    if n > 0, do: System.halt(1)
    IO.puts("ok")
  end
end

:persistent_term.put(:fails, :counters.new(1, []))

CASES

# rule 1: a float32 range is held at float32 (0.1 under max = 0.1)
VSProbe.reads(VSProbe.bytes!(%{"edge" => 0.1}), "edge", {:ok, 0.10000000149011612}, "0.1 under max = 0.1 reads back")
VSProbe.json_reads(~s({"edge": 0.1}), "edge", {:ok, 0.10000000149011612}, "0.1 under max = 0.1 reads")

# rule 2: a counted array below its Min refuses; an absent one is born at Min
VSProbe.reads(VSProbe.bytes!(%{}), "born", {:ok, [0, 0]}, "an absent [2..4] lands at its birth count")
VSProbe.reads(VSProbe.bytes!(%{"born" => [7, 8]}), "born", {:ok, [7, 8]}, "a [2..4] at its Min reads")
VSProbe.reads(VSProbe.swap!(VSProbe.bytes!(%{"born" => [7, 8]}), <<14, 4, 6, 2, 7, 8>>, <<14, 3, 6, 1, 7>>), "born", :refused, "a [2..4] count of 1 refuses")
VSProbe.json_reads(~s({"born": [7]}), "born", :refused, "a [2..4] of one element refuses")
VSProbe.json_reads(~s({"born": [7, 8]}), "born", {:ok, [7, 8]}, "a [2..4] of two elements reads")

# rule 3: a compressed float's -0 is +0 on write and on read
VSProbe.check(VSProbe.bytes!(%{"tilt" => -0.0}) == VSProbe.bytes!(%{"tilt" => 0.0}), "save of a compressed -0 writes the bytes of +0")
VSProbe.reads(VSProbe.swap!(VSProbe.bytes!(%{"tilt" => 1.0}), <<0, 0, 0x80, 0x3F>>, <<0, 0, 0, 0x80>>), "tilt", {:ok, 0.0}, "a compressed -0 reads as +0")
VSProbe.json_reads(~s({"tilt": -0.0}), "tilt", {:ok, 0.0}, "a compressed -0 text reads as +0")
VSProbe.check(<<0.0::float>> == (case Vsave.Table.from_json("Tv", ~s({"tilt": -0.0})) do
  {:ok, v, _} -> <<v["tilt"]::float>>
  _ -> :refused
end), "a compressed -0 text lands the +0 bits")

VSProbe.done()
`

func variableSaveProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, `package vsave

enum Grade { Bronze, Silver, Gold }

union Pick
{
    limit int16 | min = -2, max = 3
    label string(4)
    grade Grade
    level Inner
}

table Inner
{
    level int8 | min = 0, max = 5
}

table Tv
{
    penetration int32 = 1 | min = 0, max = 10
    channel     bits(6)
    note        string(16)
    blob        bytes(4)
    xs          [..8]int32
    born        [2..4]uint8
    trio        [3]int16
    ranged      [2]int16 | min = -2, max = 3
    ratio       float32 | min = 0, max = 10, resolution = 0.01
    edge        float32 | min = 0, max = 0.1, resolution = 0.01
    tilt        float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    plain       int32
    small       uint8
    wide        int64
    grade       Grade
    pick        Pick
    picks       [..3]Pick
    grades      [..3]Grade
    inner       Inner
    inners      [..2]Inner
    floats      [..2]float32 | min = 0, max = 1, resolution = 0.5
}
`)
	files := generateElixir(t, u)

	var cases strings.Builder
	row := func(writer, field, value, want, label string) {
		fmt.Fprintf(&cases, "VSProbe.%s(%q, %s, :%s, %s)\n", writer, field, value, want, elixirString(field+" "+label))
	}
	save := func(field, value, want, label string) { row("save", field, value, want, label) }

	// §5.1 row 1: a ranged integer off min/max
	save("penetration", "10", "wrote", "10 at its max writes")
	save("penetration", "11", "refused", "11 past its max refuses")
	save("penetration", "-1", "refused", "-1 under its min refuses")
	// ELIXIR-2: a non-integer in an integer field refuses, it does not raise
	save("penetration", "1.5", "refused", "a float in an int field refuses")
	save("penetration", `"x"`, "refused", "a binary in an int field refuses")
	// row 2: bits(N) past N
	save("channel", "63", "wrote", "bits(6) at its top writes")
	save("channel", "64", "refused", "bits(6) past its top refuses")
	// row 3: a counted array's count past Max or below Min
	save("xs", "[1, 2, 3, 4, 5, 6, 7, 8]", "wrote", "[..8] of 8 writes")
	save("xs", "[1, 2, 3, 4, 5, 6, 7, 8, 9]", "refused", "[..8] of 9 refuses")
	save("born", "[1, 2]", "wrote", "[2..4] of 2 writes")
	save("born", "[1, 2, 3, 4]", "wrote", "[2..4] of 4 writes")
	save("born", "[1]", "refused", "[2..4] of 1 refuses")
	save("born", "[]", "refused", "[2..4] of 0 refuses")
	save("born", "[1, 2, 3, 4, 5]", "refused", "[2..4] of 5 refuses")
	// a [N]T longer than N refuses rather than being cut at N
	save("trio", "[1, 2, 3]", "wrote", "[3] of 3 writes")
	save("trio", "[1]", "wrote", "[3] of 1 pads with the default and writes")
	save("trio", "[1, 2, 3, 4]", "refused", "[3] of 4 refuses, never cut")
	save("ranged", "[3, -2]", "wrote", "ranged elements at their ends write")
	save("ranged", "[4, 0]", "refused", "a ranged element past its max refuses")
	// row 4: string(N)/bytes(N) past N
	save("note", `"aaaaaaaaaaaaaaaa"`, "wrote", "string(16) of 16 writes")
	save("note", `"aaaaaaaaaaaaaaaaa"`, "refused", "string(16) of 17 refuses")
	save("blob", "<<1, 2, 3, 4>>", "wrote", "bytes(4) of 4 writes")
	save("blob", "<<1, 2, 3, 4, 5>>", "refused", "bytes(4) of 5 refuses")
	// row 5: an enum name outside the variant set
	save("grade", `"Gold"`, "wrote", "an enum member writes")
	save("grade", `"Platinum"`, "refused", "a name outside the variant set refuses")
	save("grades", `["Silver"]`, "wrote", "an enum element writes")
	save("grades", `["Platinum"]`, "refused", "an enum element outside the set refuses")
	// row 6: a union tag outside the arm set, and every arm payload's own rule
	save("pick", `{"limit", 3}`, "wrote", "an arm at its max writes")
	save("pick", `{"limit", 4}`, "refused", "an arm past its max refuses")
	save("pick", `{"label", "abcd"}`, "wrote", "an arm string at its bound writes")
	save("pick", `{"label", "abcde"}`, "refused", "an arm string past its bound refuses")
	save("pick", `{"grade", "Platinum"}`, "refused", "an arm enum outside the set refuses")
	save("pick", `{"level", %{"level" => 6}}`, "refused", "an arm table field past its max refuses")
	save("pick", `{"nosuch", 1}`, "refused", "a tag outside the arm set refuses")
	save("picks", `[{"limit", 4}]`, "refused", "an arm element past its max refuses")
	save("picks", `[{"limit", 3}]`, "wrote", "an arm element at its max writes")
	// nested bodies hold the same rules
	save("inner", `%{"level" => 5}`, "wrote", "a nested field at its max writes")
	save("inner", `%{"level" => 6}`, "refused", "a nested field past its max refuses")
	save("inners", `[%{"level" => 6}]`, "refused", "a table element field past its max refuses")
	// row 10: a ranged float off its range, at float32 (rule 1)
	save("ratio", "10.0", "wrote", "a ranged float at its max writes")
	save("ratio", "10.5", "refused", "a ranged float past its max refuses")
	save("ratio", "-0.5", "refused", "a ranged float under its min refuses")
	save("edge", "0.1", "wrote", "0.1 under max = 0.1 writes (float32 bound)")
	save("edge", "0.11", "refused", "0.11 past max = 0.1 refuses")
	save("floats", "[1.0, 0.0]", "wrote", "ranged float elements at their ends write")
	save("floats", "[1.5]", "refused", "a ranged float element past its max refuses")
	// row 11: a bare intN/uintN outside its width, never truncated
	save("plain", "2147483647", "wrote", "int32 at its top writes")
	save("plain", "2147483648", "refused", "int32 past its top refuses")
	save("plain", "1099511627776", "refused", "2^40 in an int32 refuses, never truncated")
	save("small", "255", "wrote", "uint8 at its top writes")
	save("small", "256", "refused", "uint8 past its top refuses")
	save("small", "-1", "refused", "uint8 below zero refuses")
	save("wide", "-9223372036854775808", "wrote", "int64 at its bottom writes")
	save("wide", "9223372036854775808", "refused", "int64 past its top refuses")

	// the JSON writer holds the same contract (verify-writes.md, Elixir JSON column)
	json := func(field, value, want, label string) { row("json", field, value, want, label) }
	json("grade", `"Gold"`, "wrote", "an enum member writes")
	json("grade", `"Platinum"`, "refused", "a name outside the variant set refuses")
	json("ratio", "10.0", "wrote", "a ranged float at its max writes")
	json("ratio", "10.5", "refused", "a ranged float past its max refuses")
	json("edge", "0.1", "wrote", "0.1 under max = 0.1 writes")
	json("pick", `{"grade", "Platinum"}`, "refused", "an arm enum outside the set refuses")
	json("trio", "[1, 2, 3, 4]", "refused", "[3] of 4 refuses")
	json("born", "[1]", "refused", "[2..4] of 1 refuses")
	json("penetration", "11", "refused", "a ranged int past its max refuses")

	return files, strings.ReplaceAll(variableSaveProbeSource, "CASES", cases.String())
}
