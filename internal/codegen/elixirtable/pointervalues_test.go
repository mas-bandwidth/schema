package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestElixirPointerValuesAreANodeDirectory holds the public shape of a loaded
// pointered value on the Elixir leg (docs/SPEC-TABLES.md §3.1): the value is
// {root, nodes}, a root map whose pointer fields hold node numbers (nil for
// null) beside a tuple of the table nodes in index order. That shape keeps the
// value's flat size linear in the wire's bytes, so a reader can copy it across
// a process boundary without the runtime re-expanding a shared graph.
// The writers hold the rules the directory makes theirs: a data cycle and one
// node number reached as two table types are refused by save, the cook writer
// and the text writer, and the writers' work is linear in the node count.
func TestElixirPointerValuesAreANodeDirectory(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pointer schema paths: %v (%d files)", err, len(paths))
	}
	u := loadUnit(t, paths...)
	files := generateElixir(t, u)
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(framing, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	// The canonical re-encode of the committed wire is the reference's own
	// writer output; the committed framing vector may carry a noncanonical id
	// order (§3.1). Save must agree with the reference, not with the fixture.
	model := tabletext.NewModel(u)
	canonical := func(name, table string) []byte {
		value := model.New(model.Lookup(table))
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, value, read(name), &report); !ok || err != nil || report.Refused {
			t.Fatalf("canonical %s decode: ok=%v err=%v report=%+v", name, ok, err, report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("canonical %s encode: %v", name, err)
		}
		return wire
	}
	diamondCanonical := canonical("node_diamond_chain", "Scene")
	chainCanonical := canonical("node_chain_two", "ListNode")

	dir := t.TempDir()
	diamondCanonicalPath := filepath.Join(dir, "diamond_canonical.bin")
	chainCanonicalPath := filepath.Join(dir, "chain_canonical.bin")
	if err := os.WriteFile(diamondCanonicalPath, diamondCanonical, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chainCanonicalPath, chainCanonical, 0o600); err != nil {
		t.Fatal(err)
	}

	var probe strings.Builder
	probe.WriteString(pointerDirectoryProbeHelpers)
	fmt.Fprintf(&probe, `
diamond_wire = File.read!(%q)
{:ok, diamond, _r} = Graphdemo.Table.load("Scene", diamond_wire)
Probe.check(is_tuple(diamond), "diamond: value is not {root, nodes}")
{root, nodes} = diamond
Probe.check(is_map(root) and is_tuple(nodes), "diamond: value is not root + directory")
Probe.check(tuple_size(nodes) == 81, "diamond: directory has #{tuple_size(nodes)} nodes, want 81")

# A pointer field holds the node number; deref follows it to the one value.
tree_ref = root["tree"]
Probe.check(is_integer(tree_ref), "diamond: tree is not a node number")
tree = Graphdemo.Table.deref(diamond, tree_ref)
Probe.check(is_map(tree), "diamond: deref did not follow the tree pointer")

# Every pointer slot of the root and of every directory node holds a node
# number or nil, never a node: the structural walk stops at the first slot
# that holds a map, so an inlined child fails here at once (§3.1).
Probe.check(Probe.pointer_slots_are_numbers?(diamond),
  "diamond: a pointer slot holds a node, not a node number")

# The flat size is linear in the wire's bytes: copyable across a process. The
# words are first counted by a walk that stops at the bound, so a value whose
# sharing would expand fails at the bound instead of running the runtime's
# flat size of the expansion to the end; the runtime's count then confirms it.
budget = 8 * byte_size(diamond_wire)
Probe.check(Probe.flat_words_within?(diamond, budget),
  "diamond: flat size is not linear in #{byte_size(diamond_wire)} bytes")
flat = :erts_debug.flat_size(diamond)
Probe.check(flat <= budget,
  "diamond: flat size #{flat} is not linear in #{byte_size(diamond_wire)} bytes")

# The two guards tell sharing from a directory: the same chain held as nested
# maps that share each level (the shape a pointer field holding its child would
# have) is refused by both, and the bounded walk stops at the bound.
inlined = Enum.reduce(1..40, nil, fn _, child ->
  %%{"label" => "n", "left" => child, "right" => child}
end)
Probe.check(not Probe.pointer_slots_are_numbers?({%%{"tree" => inlined}, {}}),
  "control: the structural walk passed an inlined child")
Probe.check(not Probe.flat_words_within?(inlined, budget),
  "control: the bounded walk passed an inlined chain")

# Both parents name the shared node by the same number (§3.1): the children
# are numbers, compared as numbers, and they name one directory entry.
left_ref = tree["left"]
right_ref = tree["right"]
Probe.check(is_integer(left_ref) and is_integer(right_ref),
  "diamond: tree's children are not node numbers")
left = Graphdemo.Table.deref(diamond, left_ref)
right = Graphdemo.Table.deref(diamond, right_ref)
Probe.check(is_integer(left["left"]) and is_integer(left["right"]) and
  left["left"] == right["left"] and left["right"] == right["right"],
  "diamond: shared children do not share a node number")

# Round-trips: save byte for byte, text to the committed reference.
{:ok, saved} = Graphdemo.Table.save("Scene", diamond)
Probe.check(saved == File.read!(%q), "diamond: save did not match the canonical writer")
{:ok, text} = Graphdemo.Table.to_json("Scene", diamond)
Probe.check(text == File.read!(%q), "diamond: text did not match the committed reference")
{:ok, back, _br} = Graphdemo.Table.from_json("Scene", text)
{:ok, back_saved} = Graphdemo.Table.save("Scene", back)
Probe.check(back_saved == saved, "diamond: text round trip moved bytes")

# A pointered value with no shared nodes has the same shape.
{:ok, chain, _cr} = Graphdemo.Table.load("ListNode", File.read!(%q))
Probe.check(is_tuple(chain), "chain: value is not {root, nodes}")
{chain_root, chain_nodes} = chain
Probe.check(is_map(chain_root) and is_tuple(chain_nodes), "chain: value is not root + directory")
Probe.check(chain_root["next"] == 2, "chain: next is not a node number")
Probe.check(is_map(Graphdemo.Table.deref(chain, 2)), "chain: deref did not follow next")
{:ok, chain_saved} = Graphdemo.Table.save("ListNode", chain)
Probe.check(chain_saved == File.read!(%q), "chain: save did not match the canonical writer")

# The cook writer numbers nodes by these numbers too: the loaded value cooks
# and the cooked region opens with the cook reader (§7.6).
{size, :ok} = Graphdemo.Table.cook_measure_reason("Scene", diamond)
Probe.check(size > 0, "diamond: cook measure refused")
{true, cook} = Graphdemo.Table.cook_from("Scene", diamond, :binary.copy(<<0>>, size), :little)
Probe.check(Graphdemo.GraphCook.cook_open_scene(cook) != :error, "diamond: the cook did not open")

`, filepath.Join(framing, "node_diamond_chain.bin"), diamondCanonicalPath, filepath.Join(root, "testdata", "conformance", "tables", "framing", "scene_diamond_chain.json"), filepath.Join(framing, "node_chain_two.bin"), chainCanonicalPath)
	probe.WriteString(pointerDirectoryWriterRows)
	probe.WriteString("\nProbe.finish()\nIO.puts(\"ok: Elixir pointer values are a node directory\")\n")
	names := writeModuleDir(t, dir, files)
	out := runProbe(t, elixirBin, dir, names, probe.String())
	if !strings.Contains(out, "ok: Elixir pointer values are a node directory") {
		t.Fatalf("native pointer-directory probe: %s", out)
	}
	t.Log(strings.TrimSpace(out))
}

const pointerDirectoryProbeHelpers = `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  # row runs one claim in its own process under a heap cap, so a writer that
  # recurses without end is killed by the cap and fails the row; every row
  # runs, and finish/0 fails the probe naming each failed row.
  def row(name, want, fun) do
    parent = self()

    {pid, ref} =
      spawn_monitor(fn ->
        Process.flag(:max_heap_size, %{size: 20_000_000, kill: true, error_logger: false})

        got =
          try do
            fun.()
          rescue
            e -> {:raised, e}
          catch
            kind, why -> {kind, why}
          end

        send(parent, {:row, self(), got})
      end)

    got =
      receive do
        {:row, ^pid, got} ->
          Process.demonitor(ref, [:flush])
          got

        {:DOWN, ^ref, _, _, why} ->
          {:down, why}
      end

    if got != want do
      IO.puts(:stderr, "FAIL: #{name}: want #{inspect(want)}, got #{inspect(got, limit: 8)}")
      Process.put(:probe_failed, true)
    end
  end

  def finish do
    if Process.get(:probe_failed, false), do: System.halt(1)
  end

  # reductions counts the work one call does, by the runtime's own operation
  # count: no clock.
  def reductions(fun) do
    {:reductions, before} = :erlang.process_info(self(), :reductions)
    fun.()
    {:reductions, later} = :erlang.process_info(self(), :reductions)
    later - before
  end

  # The pointer fields of the tables these rows load (tables/pointers): a
  # slot of one of these names, in the root, in a directory node or in a
  # by-value table inside either, must hold a node number or nil.
  @pointer_slots ~w(next left right head tree settings alias)

  def pointer_slots_are_numbers?({root, nodes}) when is_map(root) and is_tuple(nodes) do
    slots_ok?(root) and Enum.all?(Tuple.to_list(nodes), &(&1 == nil or slots_ok?(&1)))
  end

  def pointer_slots_are_numbers?(_), do: false

  defp slots_ok?(m) when is_map(m) do
    Enum.all?(m, fn
      {k, v} when k in @pointer_slots -> v == nil or is_integer(v)
      {_k, v} when is_map(v) -> slots_ok?(v)
      {_k, v} when is_list(v) -> Enum.all?(v, fn e -> not is_map(e) or slots_ok?(e) end)
      _ -> true
    end)
  end

  # flat_words_within? counts a term's words as a copy would hold them,
  # visiting a shared subterm once per path exactly as the copy does, and
  # stops as soon as the count passes the budget: a term whose copy would
  # expand fails after about budget steps, never after the expansion.
  def flat_words_within?(term, budget) do
    try do
      words(term, budget) >= 0
    catch
      :over_budget -> false
    end
  end

  defp words(_term, left) when left < 0, do: throw(:over_budget)

  defp words(term, left) when is_map(term),
    do: :maps.fold(fn k, v, left -> words(v, words(k, left - 2)) end, left - 1, term)

  defp words(term, left) when is_tuple(term),
    do: Enum.reduce(Tuple.to_list(term), left - 1 - tuple_size(term), &words/2)

  defp words([h | t], left), do: words(t, words(h, left - 2))
  defp words(term, left) when is_binary(term), do: left - 1
  defp words(_term, left), do: left
end
`

// pointerDirectoryWriterRows hold the writers to the rules a directory makes
// theirs (docs/SPEC-TABLES.md §3.1). A pointer field naming a node whose body
// is still being walked is a data cycle: save, the cook measure, cook_from and
// the text writer refuse it by name. One node number named by pointers of two
// table types is not one node, and every writer refuses it. Numbering is a
// constant-time step per node, so the writers' work grows linearly with the
// node count, measured by the runtime's reduction count, never a clock.
const pointerDirectoryWriterRows = `
t = Graphdemo.Table
ln = fn nxt -> %{"value" => 0, "name" => "a", "next" => nxt} end
self_cycle = {ln.(2), {ln.(2)}}
two_cycle = {ln.(2), {ln.(3), ln.(2)}}

for {label, v} <- [{"a self-cycle", self_cycle}, {"a two-node cycle", two_cycle}] do
  Probe.row("save refuses #{label}", {:error, :data_cycle}, fn -> t.save("ListNode", v) end)
  Probe.row("cook_measure refuses #{label}", {-1, :data_cycle}, fn ->
    t.cook_measure_reason("ListNode", v)
  end)
  Probe.row("cook_from refuses #{label}", false, fn ->
    t.cook_from("ListNode", v, :binary.copy(<<0>>, 4096), :little)
  end)
  Probe.row("to_json refuses #{label}", {:error, :data_cycle}, fn -> t.to_json("ListNode", v) end)
end

both = %{"value" => 0, "name" => "x", "next" => nil, "label" => "x", "left" => nil, "right" => nil}
two_types = {%{"name" => "s", "head" => 2, "tree" => 2}, {both}}
Probe.row("save refuses one node reached as two table types", {:error, :invalid_value}, fn ->
  t.save("Scene", two_types)
end)
Probe.row("cook_measure refuses one node reached as two table types", {-1, :invalid_value}, fn ->
  t.cook_measure_reason("Scene", two_types)
end)
Probe.row("to_json refuses one node reached as two table types", {:error, :invalid_value}, fn ->
  t.to_json("Scene", two_types)
end)

# the same number twice as ONE table type is one shared node, and saves
one_type = {%{"name" => "s", "head" => 2, "alias" => 2}, {ln.(nil)}}
Probe.row("one node reached twice as one table type saves", :ok, fn ->
  elem(t.save("Scene", one_type), 0)
end)

chain = fn n -> {ln.(2), List.to_tuple(for i <- 2..n, do: ln.(if(i == n, do: nil, else: i + 1)))} end

for {label, writer} <- [
      {"save", fn v -> t.save("ListNode", v) end},
      {"cook_measure", fn v -> t.cook_measure_reason("ListNode", v) end}
    ] do
  # linear work is about 4 times over 4 times the nodes; a scan of the nodes
  # already collected per node is about 16 times
  Probe.row("#{label} work is linear in the node count", true, fn ->
    small = chain.(2000)
    large = chain.(8000)
    a = Probe.reductions(fn -> writer.(small) end)
    b = Probe.reductions(fn -> writer.(large) end)
    b <= 6 * a or {:reductions, a, b}
  end)
end
`
