package elixirtable

// THE FINITE FLOAT RULE on the Elixir table leg (docs/SPEC-TABLES.md §3.3;
// Glenn, 2026-10-27: "Tables go finite-only too, same rule as packets. NaN/inf
// refused on write and read, -0 and subnormals kept"). The Go leg's gate is
// internal/codegen/gotable/floatspecial_test.go
// (TestGoPlainFloatSpecialValuesAgreeAcrossForms); this is the same rule held
// on the generated Elixir, form by form:
//
//   - VARIABLE: every read, the tolerant load and the retaining one, refuses a
//     NaN or an infinity on its raw bits as non_finite_float, naming
//     <Owner>.<field>, at every float position (a scalar of each width, a
//     compressed float, a counted, fixed and unbounded array, a by-value type,
//     a union arm, an enum-keyed slot and a pointee). A retained unknown kind
//     10/11 entry is tested like a known one (ruling 3). Every writer (save,
//     save_retain, to_json) refuses a float32 that narrows to an infinity.
//     -0 and a subnormal ride by their bits, and JSON's -0 keeps its sign.
//   - MESSAGE: the batch read refuses one wherever it sits in the bit stream,
//     and save_messages/measure_messages refuse to write one.
//   - FIXED: the load refuses one in every float slot, a compressed float
//     before its bounds; an ABSENT optional's unspecified payload is not
//     refused; the writer raises ArgumentError non_finite_float.
//
// The probe saves finite SENTINELS, patches each one's bits into a NaN or an
// infinity (five patterns per width: quiet, signalling, both infinities and a
// negative NaN), and prints one PASS or FAIL line per row.
// TestElixirFiniteFloatNegativeControl sabotages the read's bit test in each
// form, and the writer's, in turn, and each sabotage must red exactly its own
// rows and no other.
//
// It runs elixir, so it is a functional test: make tables-elixir-finite-float
// runs it under SCHEMA_REQUIRE_CORPUS=1, where a missing toolchain fails by
// name; without that variable and without Elixir it skips by name.

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const finiteVarSchema = `package finitevar

enum Key { A, B }

union Shape
{
    r float64
    n int32
}

type Pair
{
    s float32
    d float64
}

table Child
{
    c float64
}

table Root
{
    a float32
    b float64
    q float32 = 0 | min = -1, max = 1, resolution = 0.01
    n int32
    ns []int32
    xs [..3]float64
    ys [2]float32
    zs []float32
    pair Pair
    u Shape
    keyed [Key]float32
    child *Child
}

// the RETAINING read and write carry no union and no enum-keyed array yet
// (docs/SPEC-TABLES.md §11, carried?/1), so they are held on a root without
// either
table Plain
{
    a float32
    b float64
    pair Pair
    child *Child
}
`

// finiteOldSchema is an OLDER reader of finitevar's Root: it names `a` alone,
// so every other field of the wire is an unknown entry it retains (§6.6).
const finiteOldSchema = `package finiteold

table Root
{
    a float32
}
`

// THE FIXED FORM (§3.4) is emitted for a unit of fixed tables alone, so it is
// a unit of its own, and it is the unit that carries the message form.
const finiteFixSchema = `package finitefix

fixed table Rec
{
    a float32
    b float64
    q float32 = 0 | min = -1, max = 1, resolution = 0.01
    o ?float32
    ys [2]float64
}
`

const finiteProbe = `
defmodule FiniteProbe do
  import Bitwise

  alias Finitevar.Table, as: V
  alias Finiteold.Table, as: Old
  alias Finitefix.TableMessage, as: M
  alias Finitefix.ProbeFixed, as: F

  @f32 [
    {"qnan", 0x7FC00000},
    {"snan", 0x7F800001},
    {"inf", 0x7F800000},
    {"-inf", 0xFF800000},
    {"-nan", 0xFFC00001}
  ]
  @f64 [
    {"qnan", 0x7FF8000000000000},
    {"snan", 0x7FF0000000000001},
    {"inf", 0x7FF0000000000000},
    {"-inf", 0xFFF0000000000000},
    {"-nan", 0xFFF8000000000001}
  ]

  @var %{
    "a" => 1.25,
    "b" => 1.125,
    "q" => 0.5,
    "n" => 7,
    "xs" => [2.125],
    "ys" => [2.25, 0.0],
    "zs" => [3.25],
    "pair" => %{"s" => 4.25, "d" => 3.125},
    "u" => {"r", 4.125},
    "keyed" => %{"A" => 5.25},
    "child" => %{"c" => 5.125}
  }

  # every float position of the variable Root: the sentinel a save writes
  # there, its width, and the name the refusal gives it
  @var_slots [
    {"a", 1.25, 4, "Root.a"},
    {"b", 1.125, 8, "Root.b"},
    {"q", 0.5, 4, "Root.q"},
    {"xs", 2.125, 8, "Root.xs"},
    {"ys", 2.25, 4, "Root.ys"},
    {"zs", 3.25, 4, "Root.zs"},
    {"pair.s", 4.25, 4, "Pair.s"},
    {"pair.d", 3.125, 8, "Pair.d"},
    {"u.r", 4.125, 8, "Shape.r"},
    {"keyed", 5.25, 4, "Root.keyed"},
    {"child.c", 5.125, 8, "Child.c"}
  ]

  @plain %{
    "a" => 1.25,
    "b" => 1.125,
    "pair" => %{"s" => 4.25, "d" => 3.125},
    "child" => %{"c" => 5.125}
  }

  @plain_slots [
    {"a", 1.25, 4, "Plain.a"},
    {"b", 1.125, 8, "Plain.b"},
    {"pair.s", 4.25, 4, "Pair.s"},
    {"pair.d", 3.125, 8, "Pair.d"},
    {"child.c", 5.125, 8, "Child.c"}
  ]

  @retain %{records: [], used: 0, cap: 1_048_576, ids: [], id_cap: 131_072}
  @report %{
    unknown: 0,
    kind_mismatch: 0,
    widened: 0,
    clamped: 0,
    malformed: false,
    retained: 0,
    retain_lost: 0
  }

  def row(name, fun) do
    result =
      try do
        fun.()
      rescue
        e -> {:fail, "raised " <> Exception.message(e)}
      catch
        kind, value -> {:fail, "#{kind} #{inspect(value)}"}
      end

    case result do
      :ok -> IO.puts("PASS #{name}")
      {:fail, why} -> IO.puts("FAIL #{name}: #{why}")
      other -> IO.puts("FAIL #{name}: #{inspect(other)}")
    end
  end

  def expect(true, _what), do: :ok
  def expect(false, what), do: {:fail, what}

  def bits(v, 4), do: <<v::little-float-32>>
  def bits(v, 8), do: <<v::little-float-64>>
  def raw(b, 4), do: <<b::little-32>>
  def raw(b, 8), do: <<b::little-64>>
  def patterns(4), do: @f32
  def patterns(8), do: @f64

  # the one place a sentinel's bytes sit in a byte-aligned wire
  def patch(wire, old, new) do
    case :binary.matches(wire, old) do
      [{at, n}] ->
        binary_part(wire, 0, at) <> new <> binary_part(wire, at + n, byte_size(wire) - at - n)

      found ->
        raise "the sentinel #{inspect(old)} sits #{length(found)} times in the wire"
    end
  end

  # the one place a 32- or 64-bit sentinel sits in a bit stream, at any offset
  def patch_bits(<<form, m, rest::binary>>, old, new, width) do
    n = bit_size(rest)
    int = :binary.decode_unsigned(rest, :little)
    mask = (1 <<< width) - 1

    case Enum.filter(0..(n - width)//1, fn k -> (int >>> k &&& mask) == old end) do
      [k] ->
        patched = int - (old <<< k) + (new <<< k)
        bytes = :binary.encode_unsigned(patched, :little)
        <<form, m, bytes::binary, 0::size(byte_size(rest) - byte_size(bytes))-unit(8)>>

      found ->
        raise "the sentinel sits #{length(found)} times in the batch"
    end
  end

  def same_bits?(a, b), do: <<a::float>> == <<b::float>>

  def variable do
    {:ok, base} = V.save("Root", @var)

    row("variable finite round trip", fn ->
      {:ok, v, _} = V.load("Root", base)
      {:ok, again} = V.save("Root", v)
      expect(again == base, "the finite value did not round-trip")
    end)

    for {slot, sentinel, w, name} <- @var_slots, {pname, b} <- patterns(w) do
      row("variable read #{slot} #{pname}", fn ->
        wire = patch(base, bits(sentinel, w), raw(b, w))

        case V.load("Root", wire) do
          {:error, :non_finite_float, %{reason: reason}} ->
            expect(reason == "non_finite_float: " <> name, "named #{reason}")

          other ->
            {:fail, inspect(other)}
        end
      end)
    end

    {:ok, plain} = V.save("Plain", @plain)

    for {slot, sentinel, w, name} <- @plain_slots do
      row("variable retain read #{slot}", fn ->
        {_, b} = hd(patterns(w))
        wire = patch(plain, bits(sentinel, w), raw(b, w))

        case V.load_retain("Plain", wire, @retain, @report) do
          {:error, :non_finite_float, %{reason: reason}, %{records: []}} ->
            expect(reason == "non_finite_float: " <> name, "named #{reason}")

          other ->
            {:fail, inspect(other)}
        end
      end)
    end

    row("variable -0 rides by its bits", fn ->
      {:ok, zero} = V.save("Root", %{"a" => 0.0})
      {:ok, minus} = V.save("Root", %{"a" => -0.0, "ys" => [-0.0, -0.0]})
      {:ok, v, _} = V.load("Root", minus)

      expect(
        minus != zero and same_bits?(v["a"], -0.0) and Enum.all?(v["ys"], &same_bits?(&1, -0.0)),
        "a -0 under a +0 default was elided"
      )
    end)

    row("variable subnormal rides", fn ->
      <<tiny::little-float-32>> = <<1::little-32>>
      {:ok, wire} = V.save("Root", %{"a" => tiny})
      {:ok, v, _} = V.load("Root", wire)
      expect(same_bits?(v["a"], tiny), "the subnormal did not ride")
    end)

    row("variable writer refuses a float32 past its range", fn ->
      expect(V.save("Root", %{"a" => 1.0e300}) == {:error, :non_finite_float}, "save wrote it")
    end)

    row("variable writer refuses an element past float32", fn ->
      expect(V.save("Root", %{"zs" => [1.0e300]}) == {:error, :non_finite_float}, "save wrote it")
    end)

    row("variable writer refuses a keyed slot past float32", fn ->
      expect(
        V.save("Root", %{"keyed" => %{"A" => -1.0e300}}) == {:error, :non_finite_float},
        "save wrote it"
      )
    end)

    row("variable retaining writer refuses", fn ->
      case V.save_retain("Plain", %{"a" => 1.0e300}, @retain, @report) do
        {:error, :non_finite_float, _, _} -> :ok
        other -> {:fail, inspect(other)}
      end
    end)

    row("json writer refuses a float32 past its range", fn ->
      expect(V.to_json("Root", %{"a" => 1.0e300}) == {:error, :non_finite_float}, "to_json wrote it")
    end)

    row("json -0 keeps its sign", fn ->
      {:ok, v, report} = V.from_json("Root", ~s({"a": -0, "n": -0, "ns": [-0]}))

      expect(
        same_bits?(v["a"], -0.0) and v["n"] == 0 and v["ns"] == [0] and report.kind_mismatch == 0,
        inspect({v["a"], v["n"], v["ns"], report})
      )
    end)

    row("retained unknown float refuses", fn ->
      wire = patch(base, bits(1.125, 8), raw(0x7FF8000000000000, 8))

      case Old.load_retain("Root", wire, @retain, @report) do
        {:error, :non_finite_float, %{reason: "non_finite_float: Root.0x" <> _}, _} -> :ok
        other -> {:fail, inspect(other)}
      end
    end)

    row("retained unknown finite float is kept", fn ->
      case Old.load_retain("Root", base, @retain, @report) do
        {:ok, _, %{retained: n}, _} when n > 0 -> :ok
        other -> {:fail, inspect(other)}
      end
    end)

    row("plain read skips an unknown float", fn ->
      wire = patch(base, bits(1.125, 8), raw(0x7FF8000000000000, 8))

      case Old.load("Root", wire) do
        {:ok, _, _} -> :ok
        other -> {:fail, inspect(other)}
      end
    end)
  end

  def message do
    {:ok, vocab} = M.announce_read(M.announce())
    entries = Enum.with_index(vocab.entries, 1)

    {_, r10} =
      Enum.find(entries, fn {e, _} -> e.kind == 10 and Map.get(e.shape, :packing, 0) == 0 end)

    {_, r11} = Enum.find(entries, fn {e, _} -> e.kind == 11 end)
    body = [{r10, 1.25}, {r11, 1.125}]
    {:ok, batch} = M.save_messages(vocab, [body])

    row("message finite round trip", fn ->
      expect(M.load_messages(vocab, batch) == {:ok, [body]}, "the batch did not round-trip")
    end)

    for {w, sentinel} <- [{4, 1.25}, {8, 1.125}], {pname, b} <- patterns(w) do
      row("message read float#{w * 8} #{pname}", fn ->
        <<old::little-size(^w)-unit(8)>> = bits(sentinel, w)
        wire = patch_bits(batch, old, b, w * 8)
        expect(M.load_messages(vocab, wire) == {:error, :non_finite_float}, "the batch loaded")
      end)
    end

    row("message writer refuses a float32 past its range", fn ->
      expect(
        M.save_messages(vocab, [[{r10, 1.0e300}]]) == {:error, :non_finite_float} and
          M.measure_messages(vocab, [[{r10, 1.0e300}]]) == {:error, :non_finite_float},
        "the batch was written"
      )
    end)
  end

  def fixed do
    rec = %Finitefix.Rec{a: 1.25, b: 1.125, q: 0.5, o_present: true, o: 2.25, ys: [2.125, 0.0]}
    file = F.rec_fixed_save([rec])

    row("fixed finite round trip", fn ->
      {:ok, values, _} = F.rec_fixed_load(file)
      expect(F.rec_fixed_save(values) == file, "the finite record did not round-trip")
    end)

    slots = [{"a", 1.25, 4}, {"b", 1.125, 8}, {"q", 0.5, 4}, {"o", 2.25, 4}, {"ys", 2.125, 8}]

    for {slot, sentinel, w} <- slots, {pname, b} <- patterns(w) do
      row("fixed read #{slot} #{pname}", fn ->
        wire = patch(file, bits(sentinel, w), raw(b, w))

        case F.rec_fixed_load(wire) do
          {:error, :non_finite_float, _} -> :ok
          other -> {:fail, inspect(other)}
        end
      end)
    end

    row("fixed absent optional payload is not refused", fn ->
      wire = patch(file, <<1>> <> bits(2.25, 4), <<0>> <> raw(0x7FC00000, 4))

      case F.rec_fixed_load(wire) do
        {:ok, [%{o_present: false}], _} -> :ok
        other -> {:fail, inspect(other)}
      end
    end)

    row("fixed -0 rides by its bits", fn ->
      {:ok, [v], _} = F.rec_fixed_load(F.rec_fixed_save([%{rec | a: -0.0, b: -0.0}]))
      expect(same_bits?(v.a, -0.0) and same_bits?(v.b, -0.0), "a -0 lost its sign")
    end)

    for {what, value} <- [{"a float32 past its range", 1.0e300}, {"a tuple", {:nonfinite, 0x7FC00000}}] do
      row("fixed writer refuses #{what}", fn ->
        try do
          F.rec_fixed_save([%{rec | a: value}])
          {:fail, "the record was written"}
        rescue
          e in ArgumentError ->
            expect(String.starts_with?(Exception.message(e), "non_finite_float"), Exception.message(e))
        end
      end)
    end
  end
end

FiniteProbe.variable()
FiniteProbe.message()
FiniteProbe.fixed()
`

// finiteGenerate is the three probe units' generated modules in one map, each
// file named <package>_<file>: the units share file names (Table.ex,
// BuildVersion.ex) and each unit's is its own module.
func finiteGenerate(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, src := range []string{finiteVarSchema, finiteOldSchema, finiteFixSchema} {
		pkg, _, _ := strings.Cut(strings.TrimPrefix(src, "package "), "\n")
		for name, data := range generateElixir(t, unitFrom(t, src)) {
			if !strings.Contains(name, "/") && strings.HasSuffix(name, ".ex") {
				files[pkg+"_"+name] = data
			}
		}
	}
	return files
}

// finiteModuleDir writes the modules to dir and returns them in load order:
// every unit's BuildVersion and PacketRuntime first, because a Cook or Block
// module names its build version, and a file module imports from the packet
// runtime, at COMPILE time (writeModuleDir's rule, over three units).
func finiteModuleDir(t *testing.T, dir string, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	first := func(n string) bool {
		return strings.HasSuffix(n, "_"+BuildVersionModule+".ex") || strings.HasSuffix(n, "_PacketRuntime.ex")
	}
	var head, rest []string
	for _, n := range names {
		if first(n) {
			head = append(head, n)
		} else {
			rest = append(rest, n)
		}
	}
	slices.SortStableFunc(head, func(a, b string) int {
		return strings.Compare(boolKey(strings.HasSuffix(a, "_PacketRuntime.ex")), boolKey(strings.HasSuffix(b, "_PacketRuntime.ex")))
	})
	return append(head, rest...)
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// runFiniteProbe runs the probe over the generated modules, edited first by
// `edit` when it is not nil, and returns the PASS and FAIL rows by name.
func runFiniteProbe(t *testing.T, edit func(map[string][]byte)) (pass, fail []string, out string) {
	t.Helper()
	elixirBin := elixirBinary(t)
	files := finiteGenerate(t)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	names := finiteModuleDir(t, dir, files)
	out = runProbe(t, elixirBin, dir, names, finiteProbe)
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "PASS "):
			pass = append(pass, strings.TrimPrefix(line, "PASS "))
		case strings.HasPrefix(line, "FAIL "):
			name, _, _ := strings.Cut(strings.TrimPrefix(line, "FAIL "), ": ")
			fail = append(fail, name)
		}
	}
	return pass, fail, out
}

// finiteRowCount is every row the probe prints: the variable form's 11 slots
// at five patterns, its 5 retaining-read slots and its 12 other rows; the
// message form's ten patterns and two rows; the fixed form's five slots at
// five patterns and its five other rows.
const finiteRowCount = 11*5 + 5 + 12 + 10 + 2 + 5*5 + 5

func TestElixirFiniteFloat(t *testing.T) {
	t.Parallel()
	pass, fail, out := runFiniteProbe(t, nil)
	if len(fail) > 0 {
		t.Fatalf("%d row(s) failed:\n%s", len(fail), out)
	}
	if len(pass) != finiteRowCount {
		t.Fatalf("%d rows passed, want %d: the probe did not run every row\n%s", len(pass), finiteRowCount, out)
	}
}

// finiteSabotage is one reversed witness: the emitted text it replaces, the
// file it must patch exactly once, and the row-name prefixes it must red. Every
// other row must stay green, so a control that reds too much is caught too.
type finiteSabotage struct {
	name     string
	file     string
	old, new string
	reds     []string
}

var finiteSabotages = []finiteSabotage{
	{
		// the variable read's float32 bit test: a NaN or an infinity lands 0.0
		name: "variable read float32",
		file: "finitevar_Table.ex",
		old:  "    do: throw({:non_finite_float, name})\n\n  defp float_of(<<_::48, hi::little-16>>",
		new:  "    do: 0.0\n\n  defp float_of(<<_::48, hi::little-16>>",
		reds: []string{"variable read a ", "variable read q ", "variable read ys ", "variable read zs ",
			"variable read pair.s ", "variable read keyed ", "variable retain read a",
			"variable retain read pair.s"},
	},
	{
		// the variable read's float64 bit test
		name: "variable read float64",
		file: "finitevar_Table.ex",
		old:  "    do: throw({:non_finite_float, name})\n\n  defp float_of(<<v::little-float-32>>",
		new:  "    do: 0.0\n\n  defp float_of(<<v::little-float-32>>",
		reds: []string{"variable read b ", "variable read xs ", "variable read pair.d ",
			"variable read u.r ", "variable read child.c ", "variable retain read b",
			"variable retain read pair.d", "variable retain read child.c"},
	},
	{
		// the retained unknown's test (ruling 3): the entry is captured whole
		name: "retained unknown",
		file: "finiteold_Table.ex",
		old:  "            if kind in [10, 11] do\n",
		new:  "            if false do\n",
		reds: []string{"retained unknown float refuses"},
	},
	{
		// the message read's bit test: the BEAM's own float match raises
		name: "message read",
		file: "finitefix_TableMessage.ex",
		old:  "  defp f32_from_bits(v) when (v &&& 0x7F800000) == 0x7F800000, do: throw(:non_finite_float)\n",
		new:  "",
		reds: []string{"message read float32 "},
	},
	{
		// the fixed read's bit test: a NaN or an infinity reaches the bounds
		// and the struct
		name: "fixed read",
		file: "finitefix_FixedRuntime.ex",
		old:  "  def f64_value(bits) when (bits >>> 52 &&& 0x7FF) == 0x7FF,\n    do: throw({:refuse, :non_finite_float})\n",
		new:  "  def f64_value(bits) when (bits >>> 52 &&& 0x7FF) == 0x7FF,\n    do: 0.0\n",
		reds: []string{"fixed read b ", "fixed read ys "},
	},
	{
		// the variable writer's clause: a float32 past its range writes infinity
		name: "variable writer",
		file: "finitevar_Table.ex",
		old:  "  defp float_bytes(v, 4) when v >= 3.4028235677973366e38 or v <= -3.4028235677973366e38,\n    do: throw(:non_finite_float)\n\n",
		new:  "",
		reds: []string{"variable writer refuses a float32", "variable writer refuses an element",
			"variable writer refuses a keyed", "variable retaining writer refuses"},
	},
}

// TestElixirFiniteFloatNegativeControl is the gate's reversed witness: each
// sabotage in finiteSabotages, applied alone to the emitted modules, must red
// exactly the rows it names and leave every other row green.
func TestElixirFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	for _, s := range finiteSabotages {
		t.Run(strings.ReplaceAll(s.name, " ", "_"), func(t *testing.T) {
			t.Parallel()
			_, fail, out := runFiniteProbe(t, func(files map[string][]byte) {
				data, ok := files[s.file]
				if !ok {
					t.Fatalf("NEGATIVE CONTROL FAILED: no generated %s among %v", s.file, slices.Sorted(maps.Keys(files)))
				}
				if n := strings.Count(string(data), s.old); n != 1 {
					t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in %s, want 1:\n%s", n, s.file, s.old)
				}
				files[s.file] = []byte(strings.Replace(string(data), s.old, s.new, 1))
			})
			if len(fail) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: sabotaging the %s left every row GREEN\n%s", s.name, out)
			}
			for _, row := range fail {
				if !slices.ContainsFunc(s.reds, func(p string) bool { return strings.HasPrefix(row, p) }) {
					t.Errorf("NEGATIVE CONTROL FAILED: sabotaging the %s redded %q, a row it does not own\n%s", s.name, row, out)
				}
			}
			for _, p := range s.reds {
				if !slices.ContainsFunc(fail, func(row string) bool { return strings.HasPrefix(row, p) }) {
					t.Errorf("NEGATIVE CONTROL FAILED: sabotaging the %s left %q GREEN\n%s", s.name, p, out)
				}
			}
		})
	}
}
