package elixirtable

// A BOOL BYTE OR A PRESENT BYTE IS 0 OR 1 (docs/FIXED-FORM-ALGORITHM.md §4.5,
// last row, and §5.3 step 11; docs/SPEC-TABLES.md §3.4; docs/rulings.json
// strict-only-read): a byte of 0 or 1 lands as false or true, and any other
// byte refuses the WHOLE load, before any record lands. Nothing is normalized.
//
// The probe saves a record, finds the one byte a bool or a present flag sits
// in by saving the same record with only that value flipped, writes a byte
// that is neither 0 nor 1 there and loads the file. Every position is held: a
// top-level bool (2 and 0xff), the present byte of an optional, a bool in a
// nested type, a bool in a union arm, an array element, and a bool in the
// SECOND record of a batch whose first record is clean. The verdict is the
// contract (docs/rulings.json refusal-verdict-only), so a row asserts only
// that the load refused, never its reason. Bytes 0 and 1 read.
//
// TestFixedBoolBytesStrictNegativeControl takes the refusal out of the
// runtime's flag/1, so a byte past 1 lands as true again, and requires every
// refusal row to go red and every read row to stay green.
//
// It runs elixir, so it is a functional test: under SCHEMA_REQUIRE_CORPUS=1 a
// missing toolchain fails by name; without SCHEMA_SLOW it skips by name.

import (
	"slices"
	"strings"
	"testing"
)

const boolStrictSchema = `package boolstrict

type Inner
{
    k bool
}

type Lit
{
    on bool
}

type Num
{
    n int32
}

union Pick
{
    lit   Lit
    count Num
}

fixed table Rec
{
    b bool
    o ?bool
    inner Inner
    pick Pick
    bs [2]bool
}
`

const boolStrictProbe = `
defmodule BoolStrictProbe do
  alias Boolstrict.ProbeFixed, as: F

  @rec %Boolstrict.Rec{
    b: false,
    o_present: true,
    o: false,
    inner: %Boolstrict.Inner{k: false},
    pick: {:lit, %Boolstrict.Lit{on: false}},
    bs: [false, false]
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

  # the ONE byte two saves differ in: the byte the flipped value sits in
  def diff_at(a, b) do
    true = byte_size(a) == byte_size(b)

    case Enum.filter(0..(byte_size(a) - 1), fn i -> :binary.at(a, i) != :binary.at(b, i) end) do
      [i] -> i
      found -> raise "the flipped value moved #{length(found)} bytes, not one"
    end
  end

  def put(bin, at, byte) do
    <<head::binary-size(^at), _, tail::binary>> = bin
    <<head::binary, byte, tail::binary>>
  end

  def run do
    one = F.rec_fixed_save([@rec])
    two = F.rec_fixed_save([@rec, @rec])

    # each position: the clean file, and the same records with the value at
    # that position flipped from false to true
    positions = [
      {"a top-level bool", one, F.rec_fixed_save([%{@rec | b: true}])},
      {"a present byte", one, F.rec_fixed_save([%{@rec | o_present: false}])},
      {"a nested bool", one, F.rec_fixed_save([%{@rec | inner: %Boolstrict.Inner{k: true}}])},
      {"an arm's bool", one, F.rec_fixed_save([%{@rec | pick: {:lit, %Boolstrict.Lit{on: true}}}])},
      {"an array element", one, F.rec_fixed_save([%{@rec | bs: [false, true]}])},
      {"a second record's bool", two, F.rec_fixed_save([@rec, %{@rec | b: true}])}
    ]

    for {what, clean, flipped} <- positions do
      at = diff_at(clean, flipped)

      row("#{what} of 0 or 1 reads", fn ->
        {:ok, a, _} = F.rec_fixed_load(clean)
        {:ok, b, _} = F.rec_fixed_load(flipped)

        expect(
          F.rec_fixed_save(a) == clean and F.rec_fixed_save(b) == flipped and a != b,
          "the clean bytes did not read back as written"
        )
      end)

      bytes = if what == "a top-level bool", do: [2, 0xFF], else: [2]

      for byte <- bytes do
        row("#{what} of #{byte} refuses the whole load", fn ->
          case F.rec_fixed_load(put(clean, at, byte)) do
            {:error, _, _} -> :ok
            other -> {:fail, "loaded: #{inspect(other)}"}
          end
        end)
      end
    end
  end
end

BoolStrictProbe.run()
`

// boolStrictRows is every row the probe prints: six positions that read, and
// seven refusals (the top-level bool at two bytes, every other position at one).
const boolStrictRows = 6 + 7

// runBoolStrictProbe runs the probe over the generated modules, edited first
// by `edit` when it is not nil, and returns the PASS and FAIL rows by name.
func runBoolStrictProbe(t *testing.T, edit func(map[string][]byte)) (pass, fail []string, out string) {
	t.Helper()
	elixirBin := elixirBinary(t)
	files := map[string][]byte{}
	for name, data := range generateElixir(t, unitFrom(t, boolStrictSchema)) {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".ex") {
			files["boolstrict_"+name] = data
		}
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	names := finiteModuleDir(t, dir, files)
	out = runProbe(t, elixirBin, dir, names, boolStrictProbe)
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

func TestFixedBoolBytesStrict(t *testing.T) {
	t.Parallel()
	pass, fail, out := runBoolStrictProbe(t, nil)
	if len(fail) > 0 {
		t.Fatalf("%d row(s) failed:\n%s", len(fail), out)
	}
	if len(pass) != boolStrictRows {
		t.Fatalf("%d rows passed, want %d: the probe did not run every row\n%s", len(pass), boolStrictRows, out)
	}
}

func TestFixedBoolBytesStrictNegativeControl(t *testing.T) {
	t.Parallel()
	const file = "boolstrict_" + FixedRuntimeModule + ".ex"
	const anchor = "def flag(_byte), do: throw({:refuse, :wire_damaged})"
	const sabotage = "def flag(byte), do: byte != 0"
	_, fail, out := runBoolStrictProbe(t, func(files map[string][]byte) {
		data, ok := files[file]
		if !ok {
			t.Fatalf("NEGATIVE CONTROL FAILED: no generated %s", file)
		}
		if n := strings.Count(string(data), anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor appears %d times in %s, want 1: %q", n, file, anchor)
		}
		files[file] = []byte(strings.Replace(string(data), anchor, sabotage, 1))
	})
	var refusals int
	for _, row := range fail {
		if !strings.HasSuffix(row, "refuses the whole load") {
			t.Errorf("NEGATIVE CONTROL FAILED: normalizing flag/1 redded %q, a row it does not own\n%s", row, out)
		}
		refusals++
	}
	if refusals != 7 || !slices.ContainsFunc(fail, func(r string) bool { return strings.HasPrefix(r, "a second record's bool") }) {
		t.Fatalf("NEGATIVE CONTROL FAILED: normalizing flag/1 redded %d refusal row(s), want 7\n%s", refusals, out)
	}
}
