package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirSaveChecksEveryField pins docs/SPEC.md §5.1 rows 1-5 and 11 and
// docs/SPEC-TABLES.md §2.8, §2.15 on the variable form's writers. The Elixir
// leg refuses in every build, so save/2 answers {:error, :invalid_value} (and
// to_json the same) for a bad value on ANY field, not only a `| required` one:
//
//  1. a ranged integer, a bits(N) value, a counted array past its Max, and a
//     string(N) or bytes(N) past N, each on a plain field;
//  2. an enum name that is no variant, and a deprecated variant, refused
//     rather than raised as a KeyError or written unchanged;
//  3. a [N]T list longer than N, refused rather than cut at N, while a shorter
//     one still pads with the element default;
//  4. a map key of the wrong type: a key an integer-keyed map cannot read as
//     one canonical integer, or two spellings of one integer, refused rather
//     than raised or written twice.
//
// Every refusal has its lawful twin at the same boundary, which writes. Each
// row is named, so a planted old behaviour goes red on its own row. It runs
// elixir, so it is a functional test: it skips by name without the toolchain.
func TestElixirSaveChecksEveryField(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	files, probe := saveChecksProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("save checks probe: %v\n%s", err, out)
	}
}

// TestElixirSaveChecksNegativeControls plants the old behaviour of each fix in
// the generated Elixir and requires the probe to go red on that fix's rows.
func TestElixirSaveChecksNegativeControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	plants := []struct {
		fix, old, new, row string
		alsoOld, alsoNew   string
	}{
		{
			"every field is checked",
			"do: check_field(f, Map.fetch!(value, f.name))",
			"do: nil",
			"FAIL: plain int past its max refuses",
			"", "",
		},
		{
			"enum name refused, not raised",
			"if not Map.has_key?(f.by_name, name) or name in Map.get(f, :retired, []),",
			"if false,",
			"FAIL: enum that is no variant refuses",
			"", "",
		},
		{
			"deprecated variant refused",
			"if not Map.has_key?(f.by_name, name) or name in Map.get(f, :retired, []),",
			"if not Map.has_key?(f.by_name, name),",
			"FAIL: deprecated variant refuses",
			"", "",
		},
		{
			"long fixed array refused, not cut",
			"if length(elements) > n, do: throw(:invalid_value)\n    elements ++ List.duplicate(f.elem_default, n - length(elements))",
			"kept = Enum.take(elements, n)\n    kept ++ List.duplicate(f.elem_default, n - length(kept))",
			"FAIL: fixed array of four refuses",
			// the bound check on the list field is the other half of the fix
			"and n > f.array_bound,\n      do: throw(:invalid_value)",
			"and n > f.array_bound + 1_000_000,\n      do: throw(:invalid_value)",
		},
		{
			"map keys of the wrong type refused",
			"    check_map_keys(f, values)\n",
			"",
			"FAIL: integer map holding 7 and \"07\" refuses",
			"", "",
		},
	}
	for _, p := range plants {
		t.Run(p.fix, func(t *testing.T) {
			t.Parallel()
			files, probe := saveChecksProbe(t)
			elixirPatch(t, files, p.old, p.new)
			if p.alsoOld != "" {
				elixirPatch(t, files, p.alsoOld, p.alsoNew)
			}
			out, err := runArmProbe(t, elixirBinary(t), files, probe)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: the old behaviour left every row green\n%s", out)
			}
			if !strings.Contains(out, p.row) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on %q: %v\n%s", p.row, err, out)
			}
		})
	}
}

const saveChecksProbeSource = `
defmodule SCProbe do
  @base %{}

  def fail(what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def check(true, _), do: :ok
  def check(false, what), do: fail(what)

  def save(v) do
    Savechecks.Table.save("Rec", Map.merge(@base, v))
  rescue
    e -> {:raised, e.__struct__}
  end

  def json(v) do
    Savechecks.Table.to_json("Rec", Map.merge(@base, v))
  rescue
    e -> {:raised, e.__struct__}
  end

  def writes(v, what) do
    check(match?({:ok, _}, save(v)), what <> " writes: " <> inspect(save(v)))
    check(match?({:ok, _}, json(v)), what <> " writes as text: " <> inspect(json(v)))
  end

  def refuses(v, what) do
    check(save(v) == {:error, :invalid_value}, what <> " refuses: " <> inspect(save(v)))
  end

  def refuses_both(v, what) do
    refuses(v, what)
    check(json(v) == {:error, :invalid_value}, what <> " refuses as text: " <> inspect(json(v)))
  end

  def done do
    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

SCProbe.writes(%{}, "an empty record")

# 1. every field is checked, not only a required one
SCProbe.writes(%{"r" => 9}, "plain int at its max")
SCProbe.refuses_both(%{"r" => 10}, "plain int past its max")
SCProbe.refuses_both(%{"r" => 0}, "plain int under its min")
SCProbe.writes(%{"b" => 63}, "plain bits(6) at its top")
SCProbe.refuses_both(%{"b" => 64}, "plain bits(6) past its top")
SCProbe.writes(%{"s" => "abcd"}, "string(4) at its bound")
SCProbe.refuses(%{"s" => "abcde"}, "string(4) past its bound")
SCProbe.writes(%{"y" => <<1, 2, 3, 4>>}, "bytes(4) at its bound")
SCProbe.refuses(%{"y" => <<1, 2, 3, 4, 5>>}, "bytes(4) past its bound")
SCProbe.writes(%{"c" => [1, 2, 3]}, "counted array at its Max")
SCProbe.refuses(%{"c" => [1, 2, 3, 4]}, "counted array past its Max")
SCProbe.refuses(%{"c" => [1, 2, 256]}, "an element past its integer width")
SCProbe.refuses(%{"o" => 300}, "an optional uint8 past its width")

# 2. an enum name that is no variant, and a deprecated one
SCProbe.writes(%{"e" => "Scout"}, "an enum variant")
SCProbe.writes(%{"e" => "None"}, "the enum None")
SCProbe.refuses_both(%{"e" => "Nope"}, "enum that is no variant")
SCProbe.refuses_both(%{"e" => "Bomber"}, "deprecated variant")

# 3. a fixed [N]T: short pads, long refuses
SCProbe.writes(%{"f" => [1, 2, 3]}, "fixed array of three")
SCProbe.writes(%{"f" => [1]}, "fixed array of one pads")
SCProbe.refuses_both(%{"f" => [1, 2, 3, 4]}, "fixed array of four")

short = SCProbe.save(%{"f" => [1]})
full = SCProbe.save(%{"f" => [1, 0, 0]})
SCProbe.check(short == full, "a short fixed array writes as its default-padded twin")

# 4. map keys
SCProbe.writes(%{"m" => %{7 => 1, 8 => 2}}, "integer map with integer keys")
SCProbe.writes(%{"m" => %{"7" => 1, "-8" => 2}}, "integer map with canonical loaded keys")
SCProbe.refuses(%{"m" => %{7 => 1, "07" => 2}}, "integer map holding 7 and \"07\"")
SCProbe.refuses(%{"m" => %{7 => 1, "7" => 2}}, "integer map holding 7 and \"7\"")
SCProbe.refuses(%{"m" => %{"abc" => 1}}, "integer map with a non-numeric key")
SCProbe.refuses(%{"m" => %{1.5 => 1}}, "integer map with a float key")
SCProbe.refuses(%{"ms" => %{7 => 1}}, "string map with an integer key")
SCProbe.writes(%{"ms" => %{"7" => 1, "07" => 2}}, "string map keeps its text keys")

SCProbe.done()
`

func saveChecksProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, `package savechecks

enum Ship
{
    Fighter
    Bomber | deprecated
    Scout
}

table Rec
{
    r  int32 = 5 | min = 1, max = 9
    b  bits(6)
    s  string(4)
    y  bytes(4)
    c  [..3]uint8
    f  [3]uint8
    e  Ship
    o  ?uint8
    m  map[int32]uint8
    ms map[string(8)]uint8
}
`)
	return generateElixir(t, u), saveChecksProbeSource
}
