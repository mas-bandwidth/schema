package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirJSONDecimalWorkIsBounded pins ELIXIR-7 of the 2026-11-21 review:
// the JSON reader's decimal path collected a number's fraction into a list of
// one cons per digit, then turned every significant digit into one bignum and
// raised ten to the token's whole length, under a comment that claimed a
// 511-byte token cap this path does not have (docs/SPEC-TABLES.md §16.2: no
// leg may cap a number token). The reader now scans the token in one pass,
// rounds a float from at most 800 significant digits and a sticky digit, and
// decides a fixed field from the digit count and the power before it builds
// any integer.
//
// Work is bounded, never timed: each read of a ten-million-digit token runs
// in a fresh process whose heap is capped at a million words with kill: true,
// so a per-digit list or a token-sized bignum kills it. The long tokens must
// also read as their exact value: a float32 and a float64 halfway point
// followed by a thousand zeros and a 1 round up, the same point exactly ties
// to even, and a fixed field reads 0.5 padded with zeros and refuses it with a
// 1 at the end.
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestElixirJSONDecimalWorkIsBounded(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package jdec

table Tj
{
    narrow float32
    wide   float64
    ratio  fixed(8,8) | min = -2, max = 3
}
`)
	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBinary(t), files, elixirJSONDecimalProbe)
	t.Log(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("JSON decimal probe: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok: the JSON decimal path is bounded") {
		t.Fatalf("JSON decimal probe did not finish:\n%s", out)
	}
}

const elixirJSONDecimalProbe = `
t = Jdec.Table
fails = :counters.new(1, [])

check = fn ok, label ->
  if not ok do
    IO.puts(:stderr, "FAIL: " <> label)
    :counters.add(fails, 1, 1)
  end
end

# one from_json in a fresh process capped at a million words of heap
read = fn text, field ->
  parent = self()

  {pid, mref} =
    spawn_monitor(fn ->
      Process.flag(:max_heap_size, %{size: 1_000_000, kill: true, error_logger: false})

      got =
        case t.from_json("Tj", text) do
          {:ok, v, _report} -> {:ok, Map.get(v, field)}
          {:error, _reason, _report} -> :refused
        end

      send(parent, {:read, self(), got})
    end)

  receive do
    {:read, ^pid, got} ->
      Process.demonitor(mref, [:flush])
      got

    {:DOWN, ^mref, _, _, why} ->
      {:died, why}
  end
end

doc = fn field, token -> ~s({") <> field <> ~s(": ) <> token <> "}" end
zeros = fn n -> :binary.copy("0", n) end
same = fn field, long, short, label ->
  got = read.(doc.(field, long), field)
  want = read.(doc.(field, short), field)
  check.(got === want and match?({:ok, _}, want), label <> ": got " <> inspect(got, limit: 3) <> ", want " <> inspect(want))
end

big = 10_000_000

# ten million fraction digits: read, bounded, and the value is the short token's
same.("narrow", "0.1" <> zeros.(big), "0.1", "a float32 of ten million digits reads as 0.1")
same.("wide", "0.1" <> zeros.(big), "0.1", "a float64 of ten million digits reads as 0.1")
same.("ratio", "0.5" <> zeros.(big), "0.5", "a fixed of ten million digits reads as 0.5")
same.("narrow", "0." <> :binary.copy("3", big), "0.3333333333333333333333333333", "a float32 of ten million significant digits rounds")
same.("wide", "0." <> :binary.copy("3", big), "0.33333333333333333333333333333333333", "a float64 of ten million significant digits rounds")

got = read.(doc.("ratio", "0.5" <> zeros.(big) <> "1"), "ratio")
check.(got === :refused, "a fixed finer than 2^-8 at the ten-millionth place refuses: got " <> inspect(got))

got = read.(doc.("ratio", "1" <> zeros.(big)), "ratio")
check.(got === :refused, "a fixed of ten million whole digits refuses: got " <> inspect(got))

got = read.(doc.("narrow", "1" <> zeros.(big) <> ".5"), "narrow")
check.(got === :refused, "a float32 of ten million whole digits refuses: got " <> inspect(got))

# an exponent of any length: twenty leading zeros still spell e-1
same.("narrow", "1e-" <> zeros.(20) <> "1", "0.1", "a float32 exponent with twenty leading zeros")
same.("wide", "1e-" <> zeros.(20) <> "1", "0.1", "a float64 exponent with twenty leading zeros")
same.("narrow", "1e-" <> :binary.copy("9", 30), "0", "a float32 exponent of thirty nines is zero")

# the sticky digit: a halfway point plus a far 1 rounds up, exactly ties to even
f32_half = "1.000000059604644775390625"
same.("narrow", f32_half <> zeros.(1000) <> "1", "1.00000011920928955078125", "a float32 halfway point and a far 1 rounds up")
same.("narrow", f32_half <> zeros.(1000), "1", "a float32 halfway point exactly ties to even")
f64_half = "1.00000000000000011102230246251565404236316680908203125"
same.("wide", f64_half <> zeros.(1000) <> "1", "1.0000000000000002220446049250313080847263336181640625", "a float64 halfway point and a far 1 rounds up")
same.("wide", f64_half <> zeros.(1000), "1.0", "a float64 halfway point exactly ties to even")

if :counters.get(fails, 1) > 0, do: System.halt(1)
IO.puts("ok: the JSON decimal path is bounded")
`
