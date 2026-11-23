package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirCountedMinimumHoldsOnEveryPath pins docs/SPEC.md section 5.1 row 3
// and docs/SPEC-TABLES.md section 22.2 on the Elixir variable form (the
// 2026-11-22 silent-failure audit, SF-02, SF-22, SF-28):
//
//   - SF-02: the binary read refuses a counted [A..B] array whose count is
//     below A, and so does the text read (the descriptor carries min_count);
//   - SF-22: save and to_json refuse a value whose list is shorter than A;
//   - SF-28: to_json refuses a list field holding something that is not a list
//     and never writes [] over it.
//
// Each lawful twin (a count at A and at B) still reads and writes. Unit-test
// rules: no real time, no socket, no network.
func TestElixirCountedMinimumHoldsOnEveryPath(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	const source = `package countedmin

table Root
{
    items [2..4]int32
    tail  [..3]int32
}
`
	u := unitFrom(t, source)

	const probe = `defmodule Probe do
  def check(true, _), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def bin({:ok, b}) when is_binary(b), do: b
  def bin(b) when is_binary(b), do: b
  def bin(other), do: other

  def refused(r), do: match?({:error, _}, r) or match?({:error, _, _}, r)
end

tt = Countedmin.Table
wire4 = Probe.bin(tt.save("Root", %{"items" => [1, 2, 3, 4], "tail" => []}))
Probe.check(is_binary(wire4), "a lawful count of 4 saves: #{inspect(wire4)}")

# SF-22: the writers hold the minimum
Probe.check(tt.save("Root", %{"items" => [1], "tail" => []}) == {:error, :invalid_value}, "save: a count of 1 below Min 2")
Probe.check(tt.save("Root", %{"items" => [], "tail" => []}) == {:error, :invalid_value}, "save: a count of 0 below Min 2")
Probe.check(tt.to_json("Root", %{"items" => [1], "tail" => []}) == {:error, :invalid_value}, "to_json: a count of 1 below Min 2")
Probe.check(is_binary(Probe.bin(tt.save("Root", %{"items" => [1, 2], "tail" => []}))), "save: a count at Min saves")
Probe.check(match?({:ok, _}, tt.to_json("Root", %{"items" => [1, 2], "tail" => []})), "to_json: a count at Min writes")

# SF-28: a list field holding a non-list is refused, never written as []
for bad <- ["x", 5, %{"a" => 1}] do
  Probe.check(tt.to_json("Root", %{"items" => [1, 2], "tail" => bad}) == {:error, :invalid_value}, "to_json: a non-list list field #{inspect(bad)}")
end
Probe.check(match?({:ok, _}, tt.to_json("Root", %{"items" => [1, 2], "tail" => nil})), "to_json: an absent list field writes")

# SF-02: the binary read. The array body <<L, ek, n, elements>> of the four-element
# wire is rewritten to a count of k, so the one rule the wire breaks is Min.
elems = fn n -> for i <- 1..n//1, into: <<>>, do: <<i::little-signed-32>> end
{at, _} = :binary.match(wire4, <<4>> <> elems.(4))
<<head::binary-size(^at - 2), _l, ek, _n, _body::binary-size(16), rest::binary>> = wire4
trim = fn k -> head <> <<2 + 4 * k, ek, k>> <> elems.(k) <> rest end

for k <- [0, 1] do
  r = tt.load("Root", trim.(k))
  Probe.check(Probe.refused(r), "load: a count of #{k} below Min 2 was taken: #{inspect(r)}")
end

for k <- [2, 3, 4] do
  r = tt.load("Root", trim.(k))
  Probe.check(match?({:ok, %{"items" => l}, _} when length(l) == k, r), "load: a lawful count of #{k} was refused: #{inspect(r)}")
end

# SF-02: the text read
for k <- [0, 1] do
  text = ~s({"items":#{inspect(Enum.to_list(1..k//1))},"tail":[]})
  r = tt.from_json("Root", text)
  Probe.check(Probe.refused(r), "from_json: a count of #{k} below Min 2 was taken: #{inspect(r)}")
end
for k <- [2, 4] do
  text = ~s({"items":#{inspect(Enum.to_list(1..k))},"tail":[]})
  r = tt.from_json("Root", text)
  Probe.check(match?({:ok, _, _}, r), "from_json: a lawful count of #{k} was refused: #{inspect(r)}")
end

IO.puts("COUNTED_MIN_OK")
`
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "COUNTED_MIN_OK") {
		t.Fatalf("Elixir counted minimum: %v\n%s", err, out)
	}
}
