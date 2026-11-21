package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirLoadMessagesWorkIsLinear pins ELIXIR-5 of the 2026-11-21 review:
// load_messages read a batch by first turning the whole batch into one bignum,
// byte by byte (quadratic in the batch), and then shifted that bignum once per
// read (fields times bytes). A hostile batch of a few hundred kilobytes cost
// tens of seconds before a single body was refused. The reader now reads bits
// from the binary at a moving offset, linear in the bytes it reads.
//
// Work is counted, never timed: the words the runtime reclaims while one
// load_messages runs in a fresh process (:erlang.statistics/1
// :garbage_collection, after a forced collection), which is the garbage the
// read allocates. A hostile batch of 32 KiB must allocate at most a fixed
// multiple of its own size, and a lawful batch read four times larger must
// cost at most six times the words (linear is four).
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestElixirLoadMessagesWorkIsLinear(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package msglin

fixed table Note
{
    id   uint32
    text string(64)
}
`)
	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBinary(t), files, elixirLoadMessagesLinearProbe)
	t.Log(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("load_messages work probe: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok: load_messages work is linear") {
		t.Fatalf("load_messages work probe did not finish:\n%s", out)
	}
}

const elixirLoadMessagesLinearProbe = `
m = Msglin.TableMessage
{:ok, announced} = m.announce_read(m.announce())
{:ok, vocab} = m.for_root(announced, "Note")
desc = Msglin.Table.descriptors()["Note"]
ref = fn name ->
  id = Enum.find(desc.fields, &(&1.name == name)).id
  Enum.find_index(vocab.entries, &(&1.id == id)) + 1
end

# words reclaimed by one call in a fresh process, after a forced collection
words = fn fun ->
  parent = self()

  {pid, mref} =
    spawn_monitor(fn ->
      :erlang.garbage_collect()
      {_, before, _} = :erlang.statistics(:garbage_collection)
      result = fun.()
      :erlang.garbage_collect()
      {_, later, _} = :erlang.statistics(:garbage_collection)
      send(parent, {:words, self(), result, later - before})
    end)

  receive do
    {:words, ^pid, result, w} ->
      Process.demonitor(mref, [:flush])
      {result, w}

    {:DOWN, ^mref, _, _, why} ->
      IO.puts(:stderr, "FAIL: the probe process died: #{inspect(why)}")
      System.halt(1)
  end
end

# a hostile batch: one body, then 32 KiB of set bits; it must refuse, and
# the read must allocate at most a fixed multiple of the batch
n = 32 * 1024
hostile = <<2, 0>> <> :binary.copy(<<0xFF>>, n)
{verdict, w} = words.(fn -> m.load_messages(vocab, hostile) end)
IO.puts("hostile #{n} bytes: #{inspect(verdict)}, #{w} words")

if match?({:ok, _}, verdict) do
  IO.puts(:stderr, "FAIL: the hostile batch was accepted")
  System.halt(1)
end

if w > 8 * n + 200_000 do
  IO.puts(:stderr, "FAIL: a #{n}-byte hostile batch allocated #{w} words, past 8 per byte")
  System.halt(1)
end

# lawful batches of 64 and 256 bodies, each with a 64-byte text: the read of
# the larger costs at most six times the words of the smaller (linear is four)
batch = fn count ->
  bodies =
    for i <- 1..count do
      [{ref.("id"), i}, {ref.("text"), String.duplicate(<<?a + rem(i, 26)>>, 64)}]
    end

  {:ok, bytes} = m.save_messages(vocab, bodies)
  {bytes, bodies}
end

{small, small_bodies} = batch.(64)
{large, large_bodies} = batch.(256)
{small_got, ws} = words.(fn -> m.load_messages(vocab, small) end)
{large_got, wl} = words.(fn -> m.load_messages(vocab, large) end)
IO.puts("lawful: #{byte_size(small)} bytes #{ws} words, #{byte_size(large)} bytes #{wl} words")

if small_got != {:ok, small_bodies} or large_got != {:ok, large_bodies} do
  IO.puts(:stderr, "FAIL: a lawful batch did not read back its bodies")
  System.halt(1)
end

if wl > 6 * max(ws, 10_000) do
  IO.puts(:stderr, "FAIL: a fourfold lawful batch cost #{Float.round(wl / max(ws, 1), 1)} times the words")
  System.halt(1)
end

IO.puts("ok: load_messages work is linear")
`
