package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const elixirCookLinearSchema = `package cooklinear
table Leaf
{
    value int32
}
table Root
{
    leaves []*Leaf
}
`

// TestElixirCookWriteCostIsLinearInCookedSize holds docs/SPEC-TABLES.md §7.6
// on the Elixir leg: cook_from's work is linear in the cooked size. A root
// holding one list of pointers to N distinct leaves is one node whose extent
// grows with N, so a writer that rewrites a node's bytes once per field it
// writes costs N times that node's size. Work is the runtime's reduction
// count around cook_from in a fresh process, never a clock: each fourfold
// step in leaves (8,000, 32,000, 128,000) costs at most 4.6 times the
// reductions.
func TestElixirCookWriteCostIsLinearInCookedSize(t *testing.T) {
	// Intentionally serial (no t.Parallel): ERTS charges monotonic GC wall time
	// into :reductions (erl_gc.c), so running before the row's ~88 parallel BEAM
	// VMs keeps OS scheduler preemption out of the reduction counter.
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "CookLinear.schema")
	if err := os.WriteFile(path, []byte(elixirCookLinearSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, path)
	files := generateElixir(t, u)
	run := t.TempDir()
	names := writeModuleDir(t, run, files)
	out := runProbe(t, elixirBin, run, names, elixirCookLinearProbe)
	t.Log(strings.TrimSpace(out))
	if !strings.Contains(out, "ok: Elixir cook write is linear in the cooked size") {
		t.Fatalf("cook write cost probe: %s", out)
	}
}

const elixirCookLinearProbe = `
t = Cooklinear.Table

value = fn n ->
  {%{"leaves" => Enum.to_list(2..(n + 1))}, List.to_tuple(for i <- 1..n, do: %{"value" => i})}
end

# cost runs one cook_from in a fresh process and answers its reductions
cost = fn n ->
  v = value.(n)
  {size, :ok} = t.cook_measure_reason("Root", v)
  buffer = :binary.copy(<<0>>, size)
  parent = self()

  {pid, ref} =
    spawn_monitor(fn ->
      :erlang.garbage_collect()
      {:total_heap_size, th} = :erlang.process_info(self(), :total_heap_size)
      :erlang.process_flag(:min_heap_size, th + max(div(th, 2), 65_536))
      :erlang.process_flag(:min_bin_vheap_size, max(th, 1_048_576))
      :erlang.garbage_collect()
      {:reductions, before} = :erlang.process_info(self(), :reductions)
      result = t.cook_from("Root", v, buffer, :little)
      {:reductions, later} = :erlang.process_info(self(), :reductions)
      send(parent, {:cost, self(), match?({true, _}, result), later - before})
    end)

  receive do
    {:cost, ^pid, true, r} ->
      Process.demonitor(ref, [:flush])
      r

    {:cost, ^pid, false, _} ->
      IO.puts(:stderr, "FAIL: cook_from refused #{n} leaves")
      System.halt(1)

    {:DOWN, ^ref, _, _, why} ->
      IO.puts(:stderr, "FAIL: cook_from of #{n} leaves died: #{inspect(why)}")
      System.halt(1)
  end
end

[a, b, c] = for n <- [8_000, 32_000, 128_000], do: cost.(n)
IO.puts("reductions: 8000 leaves #{a}, 32000 leaves #{b}, 128000 leaves #{c}")
IO.puts("factors: #{Float.round(b / a, 2)} and #{Float.round(c / b, 2)} for each 4x")

if b > 4.6 * a or c > 4.6 * b do
  IO.puts(:stderr, "FAIL: a fourfold step in leaves cost more than 4.6 times the reductions")
  System.halt(1)
end

IO.puts("ok: Elixir cook write is linear in the cooked size")
`
