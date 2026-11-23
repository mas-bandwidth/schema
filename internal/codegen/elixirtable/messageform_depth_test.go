package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirAnnounceReadFirstUseWalkIsDepthBounded pins ELIXIR-6 of the
// 2026-11-21 review: announce_read/2 ran the id table's first-use walk on the
// announcement's bytes before any depth-capped walk, and that walk recursed
// one level per framed content, outside tail position, with no bound. An
// announcement nesting a kind-13 field inside itself a few hundred thousand
// times grew the reading process's stack by megawords before it was refused.
// The walk now answers :error past 512 framed contents, which hands the
// framing to the strict walk to refuse.
//
// Work is bounded, never timed: the read runs in a fresh process whose heap
// (the BEAM counts the stack in it) is capped at a million words with
// kill: true, so an unbounded descent kills the process and fails the test.
//
// It runs elixir, so it is a functional test behind slowtest.Gate.
func TestElixirAnnounceReadFirstUseWalkIsDepthBounded(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package msgdeep

fixed table Note
{
    id uint32
}
`)
	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBinary(t), files, elixirAnnounceDepthProbe)
	t.Log(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("announce_read depth probe: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok: the first-use walk is depth-bounded") {
		t.Fatalf("announce_read depth probe did not finish:\n%s", out)
	}
}

const elixirAnnounceDepthProbe = `
import Bitwise
m = Msgdeep.TableMessage

leb = fn leb, n ->
  if n < 0x80, do: <<n>>, else: <<(n &&& 0x7F) ||| 0x80>> <> leb.(leb, n >>> 7)
end

# body_0 is a bare terminator; body_k is one kind-13 field (reference 1)
# framing body_(k-1), then a terminator. Built as an iolist, outermost first.
levels = 300_000

{prefixes, _len} =
  Enum.reduce(1..levels, {[], 1}, fn _, {acc, len} ->
    prefix = <<1, 13>> <> leb.(leb, len)
    {[prefix | acc], byte_size(prefix) + len + 1}
  end)

body = IO.iodata_to_binary([prefixes, <<0>>, :binary.copy(<<0>>, levels)])
data = <<1>> <> body <> <<77::little-unsigned-64, 1::little-unsigned-64>>
IO.puts("announcement of #{levels} levels, #{byte_size(data)} bytes")

parent = self()

{pid, mref} =
  spawn_monitor(fn ->
    Process.flag(:max_heap_size, %{size: 1_000_000, kill: true, error_logger: false})
    send(parent, {:read, self(), m.announce_read(data)})
  end)

receive do
  {:read, ^pid, verdict} ->
    Process.demonitor(mref, [:flush])

    case verdict do
      {:error, _reason, _vocab} ->
        IO.puts("refused: #{inspect(elem(verdict, 1))}")

      other ->
        IO.puts(:stderr, "FAIL: the nested announcement was not refused: #{inspect(other, limit: 5)}")
        System.halt(1)
    end

  {:DOWN, ^mref, _, _, why} ->
    IO.puts(:stderr, "FAIL: the read died past a million words of heap and stack: #{inspect(why)}")
    System.halt(1)
end

# a lawful announcement still reads
{:ok, _} = m.announce_read(m.announce())
IO.puts("ok: the first-use walk is depth-bounded")
`
