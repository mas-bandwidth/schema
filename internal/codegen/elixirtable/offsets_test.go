package elixirtable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func filterModuleFiles(files map[string][]byte, keep func(name string) bool) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, src := range files {
		if keep(name) {
			out[name] = src
		}
	}
	return out
}

// TestOffsetsComposeFromHalvesElixir pins docs/PORTING.md M2 on the Elixir
// leg for both halves of THE TASK:
//
//   - Part (a): On ordinary open and every-row / every-pointer dereference of
//     valid block fixtures (block_render.bin, block_padded.bin) and valid cook
//     fixtures (graph_tree.cook, graph_shared.cook, graph_deep.cook,
//     graph_empty.cook, plus backward negative-delta regions), the block's
//     offset_of, count, and stride and the cook's self-relative signed 64-bit
//     delta are composed from two 32-bit halves into immediate small integers
//     (:erts_debug.flat_size/1 == 0), allocating zero heap words and triggering
//     zero extra minor collections compared to an immediate-offset or
//     immediate-delta twin (whereas a whole-word 64-bit bignum control path
//     allocates on the heap and triggers minor collections).
//   - Part (b): A forged offset_of word or cook delta word (high half
//     0x7FFFFFFF, and the delta's negative twin -0x7FFFFFFF) is refused from
//     the 32-bit high half before any 64-bit bignum is formed, allocating zero
//     heap words and triggering no more minor collections than a refusal whose
//     high half is small.
//
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestOffsetsComposeFromHalvesElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	heaps := `
gcs = fn ->
  {:garbage_collection, p} = :erlang.process_info(self(), :garbage_collection)
  p[:minor_gcs]
end
`
	probeSetup := `
import Bitwise

defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def count_gcs(iters, fun) do
    parent = self()
    pid = :erlang.spawn_opt(
      fn ->
        _ = fun.()
        :erlang.garbage_collect()
        {:garbage_collection, p0} = :erlang.process_info(self(), :garbage_collection)
        g0 = p0[:minor_gcs]
        loop(iters, fun)
        {:garbage_collection, p1} = :erlang.process_info(self(), :garbage_collection)
        g1 = p1[:minor_gcs]
        send(parent, {:gcs, self(), g1 - g0})
      end,
      [{:min_heap_size, 233}, {:fullsweep_after, 65535}]
    )
    receive do
      {:gcs, ^pid, delta} -> delta
    after
      60_000 ->
        IO.puts(:stderr, "FAIL: count_gcs timed out")
        System.halt(1)
    end
  end

  def loop(0, _fun), do: :ok
  def loop(n, fun) do
    _ = :erts_internal.no_aux_work_threads()
    _ = fun.()
    loop(n - 1, fun)
  end

  def loop2(0, _f, _a, _b), do: :ok
  def loop2(n, f, a, b) do
    _ = f.(a, b)
    loop2(n - 1, f, a, b)
  end

  def count_gcs2(iters, f, a, b) do
    parent = self()
    pid = :erlang.spawn_opt(
      fn ->
        :erlang.garbage_collect()
        {:garbage_collection, p0} = :erlang.process_info(self(), :garbage_collection)
        g0 = p0[:minor_gcs]
        loop2(iters, f, a, b)
        {:garbage_collection, p1} = :erlang.process_info(self(), :garbage_collection)
        g1 = p1[:minor_gcs]
        send(parent, {:gcs, self(), g1 - g0})
      end,
      [{:min_heap_size, 233}, {:fullsweep_after, 65535}]
    )
    receive do
      {:gcs, ^pid, delta} -> delta
    after
      60_000 ->
        IO.puts(:stderr, "FAIL: count_gcs2 timed out")
        System.halt(1)
    end
  end

  def whole_word_u64_control(data, at) do
    <<_::binary-size(^at), w::little-unsigned-64, _::binary>> = data
    bignum = w ||| (0x7FFFFFFF <<< 32)
    if bignum == 0, do: :error, else: bignum
  end

  def whole_word_i64_control(data, at) do
    <<_::binary-size(^at), w::little-signed-64, _::binary>> = data
    bignum = w ||| (0x7FFFFFFF <<< 32)
    if bignum == 0, do: :error, else: bignum
  end
end
`
	t.Run("block", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "block", "Render.schema"),
			filepath.Join(root, "tables", "block", "Padded.schema"),
		)
		allFiles := generateElixir(t, unit)
		files := filterModuleFiles(allFiles, func(name string) bool {
			return name == "BuildVersion.ex" || name == "BlockRuntime.ex" || strings.HasSuffix(name, "Block.ex")
		})
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		goldenRender := filepath.Join(root, "testdata", "wire", "tables", "block_render.bin")
		goldenPadded := filepath.Join(root, "testdata", "wire", "tables", "block_padded.bin")
		if _, err := os.ReadFile(goldenRender); err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadFile(goldenPadded); err != nil {
			t.Fatal(err)
		}

		probe := probeSetup + `
alias Blockdemo.BlockRuntime, as: B
alias Blockdemo.RenderBlock, as: RB
alias Blockdemo.PaddedBlock, as: PB

defmodule BlockBench do
  alias Blockdemo.BlockRuntime, as: B
  alias Blockdemo.RenderBlock, as: RB

  # Immediate-offset twin of RB.render_frame_cameras_at/2: identical except it
  # reads a 32-bit immediate offset via B.u32 instead of composing the 64-bit
  # offset_of from two 32-bit halves via B.u64halves.
  def twin_cameras_at(block, index) do
    data = block.base
    B.row(data, B.u32(data, 32), B.u32(data, 40), B.u32(data, 44), index)
  end

  def run(image, block, off) do
    composed_gcs = Probe.count_gcs(50_000, fn -> B.u64halves(image, off) end)
    bignum_gcs = Probe.count_gcs(50_000, fn -> Probe.whole_word_u64_control(image, off) end)
    row_gcs = Probe.count_gcs2(50_000, &RB.render_frame_cameras_at/2, block, 0)
    twin_gcs = Probe.count_gcs2(50_000, &BlockBench.twin_cameras_at/2, block, 0)
    {composed_gcs, bignum_gcs, row_gcs, twin_gcs}
  end
end

image = File.read!(` + elixirString(goldenRender) + `)
padded_image = File.read!(` + elixirString(goldenPadded) + `)
parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
  # -------------------------------------------------------------------------
  # Part (b): Forged high half 0x7FFFFFFF refused before bignum formation.
  # Run first on the fresh process heap so minor_gcs sensitivity is maximal.
  # -------------------------------------------------------------------------
  arrays = RB.render_frame_block_arrays()
  [first_array | _] = arrays
  off = first_array.offset_of_offset
  <<head::binary-size(^off), _::binary-size(8), tail::binary>> = image
  forged = head <> <<0::little-unsigned-32, 0x7FFFFFFF::little-unsigned-32>> <> tail
  small = head <> <<0::little-unsigned-32, 1::little-unsigned-32>> <> tail
  Probe.check(RB.block_open_render_frame(forged) == :error, "a forged offset_of high half is refused")
  Probe.check(RB.block_open_render_frame(small) == :error, "an out-of-extent offset_of is refused")
  count = fn img ->
    :erlang.garbage_collect()
    g0 = gcs.()
    Enum.each(1..50_000, fn _ -> RB.block_open_render_frame(img) end)
    gcs.() - g0
  end
  small_gcs = count.(small)
  forged_gcs = count.(forged)
  Probe.check(forged_gcs <= small_gcs, "forged offset_of refusal allocated: " <> Integer.to_string(forged_gcs) <> " collections against " <> Integer.to_string(small_gcs))

  # -------------------------------------------------------------------------
  # Part (a): Ordinary open and every-row dereference across block fixtures.
  # -------------------------------------------------------------------------
  {:ok, block} = RB.block_open_render_frame(image)
  Probe.check(is_map(block) and is_binary(block.base), "ordinary open of block_render.bin is accepted")
  Probe.check(length(arrays) == 9, "RenderFrame declares 9 out-of-line arrays")

  total_rows = Enum.reduce(arrays, 0, fn a, acc ->
    a_off = B.u64halves(block.base, a.offset_of_offset)
    cnt = apply(RB, String.to_atom("render_frame_#{a.name}_count"), [block])
    str = B.u32(block.base, a.stride_offset)
    Probe.check(is_integer(a_off) and :erts_debug.flat_size(a_off) == 0, "composed offset_of is an immediate small int (flat_size == 0)")
    Probe.check(is_integer(cnt) and :erts_debug.flat_size(cnt) == 0 and cnt > 0 and cnt <= a.max, "count is an immediate small int within declared max")
    Probe.check(str == a.stride and :erts_debug.flat_size(str) == 0, "stride matches declared pitch as an immediate small int")

    Enum.each(0..(cnt - 1), fn idx ->
      row_off = a_off + idx * str
      Probe.check(:erts_debug.flat_size(row_off) == 0, "composed row byte offset is an immediate small int")
      row = apply(RB, String.to_atom("render_frame_#{a.name}_at"), [block, idx])
      Probe.check(is_binary(row) and byte_size(row) == a.stride, "ordinary row deref returns expected slice")
    end)
    Probe.check(apply(RB, String.to_atom("render_frame_#{a.name}_at"), [block, cnt]) == nil, "out-of-range row index returns nil")

    stream_rows = apply(RB, String.to_atom("render_frame_#{a.name}"), [block]) |> Enum.to_list()
    Probe.check(length(stream_rows) == cnt, "lazy stream yields all rows of array")
    acc + cnt
  end)
  Probe.check(total_rows == 33, "walked all 33 rows across all 9 arrays in block_render.bin")

  {:ok, padded} = PB.block_open_padded_frame(padded_image)
  Probe.check(is_map(padded) and is_binary(padded.base), "ordinary open of block_padded.bin is accepted")
  padded_cnt = PB.padded_frame_rows_count(padded)
  Probe.check(padded_cnt > 0 and :erts_debug.flat_size(padded_cnt) == 0, "block_padded rows count is positive immediate")
  Enum.each(0..(padded_cnt - 1), fn idx ->
    row = PB.padded_frame_rows_at(padded, idx)
    Probe.check(is_binary(row) and byte_size(row) == PB.padded_frame_rows_stride(), "block_padded row deref returns expected slice")
  end)

  {composed_gcs, bignum_gcs, row_gcs, twin_gcs} = BlockBench.run(image, block, off)
  Probe.check(composed_gcs == 0, "u64halves on valid block header triggers 0 minor GCs")
  Probe.check(bignum_gcs > composed_gcs, "whole-word 64-bit bignum control triggers minor GCs")
  Probe.check(row_gcs <= twin_gcs, "ordinary row deref triggers no more minor GCs than immediate-offset twin")

  IO.puts("block: composed_gcs=#{composed_gcs} bignum_gcs=#{bignum_gcs} row_gcs=#{row_gcs} twin_gcs=#{twin_gcs} forged_gcs=#{forged_gcs} small_gcs=#{small_gcs}")
  send(parent, :done)
end, [:monitor])
receive do
  :done -> IO.puts("ok")
  {:DOWN, ^ref, _, _, reason} -> IO.puts(:stderr, "probe process died: " <> inspect(reason)); System.halt(1)
after 120_000 -> IO.puts(:stderr, "probe timed out"); System.halt(1)
end
`
		t.Log(runProbe(t, elixirBin, dir, names, probe))
	})

	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "pointers", "Graph.schema"),
			filepath.Join(root, "tables", "pointers", "Marks.schema"),
			filepath.Join(root, "tables", "pointers", "Parts.schema"),
		)
		allFiles := generateElixir(t, unit)
		files := filterModuleFiles(allFiles, func(name string) bool {
			return name == "BuildVersion.ex" || name == "CookRuntime.ex" || strings.HasSuffix(name, "Cook.ex")
		})
		var mod string
		for _, data := range files {
			if m := cookRuntimeModuleRE.FindStringSubmatch(string(data)); m != nil {
				mod = m[1]
			}
		}
		if mod == "" {
			t.Fatal("no CookRuntime module in the generated Elixir")
		}
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)

		fixtureDir := filepath.Join(root, "testdata", "conformance", "tables", "cook-write")
		for _, name := range []string{"graph_tree.cook", "graph_shared.cook", "graph_deep.cook", "graph_empty.cook"} {
			if _, err := os.ReadFile(filepath.Join(fixtureDir, name)); err != nil {
				t.Fatal(err)
			}
		}

		probe := probeSetup + `
rt = ` + mod + `.CookRuntime
gc = ` + mod + `.GraphCook
fixture_dir = ` + elixirString(fixtureDir) + `

defmodule CookBench do
  def imm_low(_region, _slot, _width), do: 0
  def imm_high(_region, _slot, _width), do: 0
  def deref_twin(region, slot, imm_d) do
    low = imm_low(region, slot, 4)
    high = imm_high(region, slot + 4, 4)
    d = high * 0x100000000 + low + imm_d
    if d == 0 do
      :null
    else
      target = slot + d
      if target < 0 or target >= byte_size(region), do: :error, else: {:ok, target}
    end
  end

  def run(rt, tree_region, tree_slot, tree_delta, neg_region) do
    pos_gcs = Probe.count_gcs(50_000, fn ->
      lo = rt.uint(tree_region, tree_slot, 4)
      hi = rt.int(tree_region, tree_slot + 4, 4)
      hi * 0x100000000 + lo
    end)
    neg_gcs = Probe.count_gcs(50_000, fn ->
      lo = rt.uint(neg_region, 16, 4)
      hi = rt.int(neg_region, 20, 4)
      hi * 0x100000000 + lo
    end)
    bignum_gcs = Probe.count_gcs(50_000, fn ->
      Probe.whole_word_i64_control(tree_region, tree_slot)
    end)
    deref_gcs = Probe.count_gcs(50_000, fn ->
      {:ok, t} = rt.deref(tree_region, tree_slot)
      t
    end)
    twin_gcs = Probe.count_gcs(50_000, fn ->
      {:ok, t} = deref_twin(tree_region, tree_slot, tree_delta)
      t
    end)
    {pos_gcs, neg_gcs, bignum_gcs, deref_gcs, twin_gcs}
  end
end

parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
  # -------------------------------------------------------------------------
  # Part (b): Forged high half 0x7FFFFFFF / -0x7FFFFFFF refused before bignum.
  # Run first on the fresh process heap so minor_gcs sensitivity is maximal.
  # -------------------------------------------------------------------------
  region = <<0::little-unsigned-32, 0::little-unsigned-32, 0::unit(8)-size(24)>>
  fwd = <<0::little-unsigned-32, 0x7FFFFFFF::little-signed-32, 0::unit(8)-size(24)>>
  fwd_neg = <<0::little-unsigned-32, -0x7FFFFFFF::little-signed-32, 0::unit(8)-size(24)>>
  small = <<0::little-unsigned-32, 1::little-signed-32, 0::unit(8)-size(24)>>
  small_neg = <<0::little-unsigned-32, -1::little-signed-32, 0::unit(8)-size(24)>>
  Probe.check(rt.deref(region, 0) == :null, "a zero delta is null")
  Probe.check(rt.deref(fwd, 0) == :error, "a forged cook delta high half is refused")
  Probe.check(rt.deref(fwd_neg, 0) == :error, "a forged negative cook delta high half is refused")
  Probe.check(rt.deref(small, 0) == :error, "an out-of-region cook delta is refused")
  Probe.check(rt.deref(small_neg, 0) == :error, "a negative out-of-region cook delta is refused")
  count = fn img ->
    :erlang.garbage_collect()
    g0 = gcs.()
    Enum.each(1..50_000, fn _ -> rt.deref(img, 0) end)
    gcs.() - g0
  end
  small_gcs = count.(small)
  forged_gcs = count.(fwd)
  Probe.check(forged_gcs <= small_gcs, "forged cook delta refusal allocated: " <> Integer.to_string(forged_gcs) <> " collections against " <> Integer.to_string(small_gcs))
  small_neg_gcs = count.(small_neg)
  forged_neg_gcs = count.(fwd_neg)
  Probe.check(forged_neg_gcs <= small_neg_gcs, "forged negative cook delta refusal allocated: " <> Integer.to_string(forged_neg_gcs) <> " collections against " <> Integer.to_string(small_neg_gcs))

  # -------------------------------------------------------------------------
  # Part (a): Ordinary open and pointer-slot dereference on valid cook graphs.
  # -------------------------------------------------------------------------
  walk_record = fn walk, region_bin, at, {info_mod, info_fn}, seen ->
    info = apply(info_mod, info_fn, [])
    Probe.check(:erts_debug.flat_size(at) == 0, "record offset is an immediate small int")
    Probe.check(at >= 0 and at + info.size <= byte_size(region_bin), "record sits inside region")
    if MapSet.member?(seen, {info.name, at}) do
      seen
    else
      seen = MapSet.put(seen, {info.name, at})
      Enum.reduce(info.fields, seen, fn f, acc_seen ->
        present =
          if f.present_offset >= 0 do
            rt.bool(region_bin, at + f.present_offset)
          else
            true
          end

        if not present do
          acc_seen
        else
          cnt =
            cond do
              not f.is_array -> 1
              f.count_offset >= 0 ->
                c = rt.i32(region_bin, at + f.count_offset)
                Probe.check(:erts_debug.flat_size(c) == 0 and c >= 0 and c <= f.array_bound, "array count is an immediate small int within bound")
                c
              true -> f.array_bound
            end

          if cnt == 0 do
            acc_seen
          else
            Enum.reduce(0..(cnt - 1), acc_seen, fn idx, s ->
              slot = at + f.offset + idx * f.elem_size
              cond do
                f.is_pointer ->
                  lo = rt.uint(region_bin, slot, 4)
                  hi = rt.int(region_bin, slot + 4, 4)
                  delta = hi * 0x100000000 + lo
                  Probe.check(:erts_debug.flat_size(lo) == 0 and :erts_debug.flat_size(hi) == 0 and :erts_debug.flat_size(delta) == 0, "composed cook delta is an immediate small int (flat_size == 0)")
                  case rt.deref(region_bin, slot) do
                    :null ->
                      Probe.check(delta == 0, "null pointer slot has zero delta")
                      s
                    {:ok, target} ->
                      Probe.check(target == slot + delta and :erts_debug.flat_size(target) == 0, "deref target matches slot + composed delta as an immediate small int")
                      walk.(walk, region_bin, target, f.record, s)
                    other ->
                      Probe.check(false, "unexpected deref result on valid cook fixture: " <> inspect(other))
                      s
                  end
                f.storage == :record ->
                  walk.(walk, region_bin, slot, f.record, s)
                true ->
                  s
              end
            end)
          end
        end
      end)
    end
  end

  Enum.each(["graph_tree.cook", "graph_shared.cook", "graph_deep.cook", "graph_empty.cook"], fn file ->
    bytes = File.read!(Path.join(fixture_dir, file))
    {:ok, handle} = gc.cook_open_scene(bytes)
    Probe.check(is_binary(handle.region) and :erts_debug.flat_size(handle.region_length) == 0, file <> " opened with immediate region_length")
    seen = walk_record.(walk_record, handle.region, 0, handle.info, MapSet.new())
    Probe.check(MapSet.size(seen) >= 2, file <> " walked root and nested records")
  end)

  # Backward negative 64-bit self-relative delta (-16 bytes: hi = -1, lo = 0xFFFFFFF0).
  neg_region = <<
    0xAA, 0xBB, 0xCC, 0xDD, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0xF0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF
  >>
  neg_lo = rt.uint(neg_region, 16, 4)
  neg_hi = rt.int(neg_region, 20, 4)
  neg_delta = neg_hi * 0x100000000 + neg_lo
  Probe.check(neg_delta == -16 and :erts_debug.flat_size(neg_delta) == 0, "backward negative 64-bit delta (-16) composes from halves as an immediate small int")
  Probe.check(rt.deref(neg_region, 16) == {:ok, 0}, "backward negative 64-bit delta derefs to {:ok, 0}")

  tree_bytes = File.read!(Path.join(fixture_dir, "graph_tree.cook"))
  {:ok, tree_handle} = gc.cook_open_scene(tree_bytes)
  tree_region = tree_handle.region
  tree_field = Enum.find(gc.cook_info_scene().fields, fn f -> f.name == "tree" end)
  tree_slot = tree_field.offset
  {:ok, tree_target} = gc.scene_node_tree(tree_region, 0)
  Probe.check(rt.deref(tree_region, tree_slot) == {:ok, tree_target}, "scene_node_tree matches CookRuntime.deref")
  tree_delta = tree_target - tree_slot

  {pos_gcs, neg_gcs, bignum_gcs, deref_gcs, twin_gcs} =
    CookBench.run(rt, tree_region, tree_slot, tree_delta, neg_region)

  Probe.check(pos_gcs == 0 and neg_gcs == 0, "composed positive and negative cook deltas trigger 0 minor GCs")
  Probe.check(bignum_gcs > pos_gcs, "whole-word 64-bit signed bignum control triggers minor GCs")
  Probe.check(deref_gcs <= twin_gcs, "CookRuntime.deref on valid cook pointer triggers no more minor GCs than immediate-delta twin")

  IO.puts("cook: pos_gcs=#{pos_gcs} neg_gcs=#{neg_gcs} bignum_gcs=#{bignum_gcs} deref_gcs=#{deref_gcs} twin_gcs=#{twin_gcs} forged=#{forged_gcs} immediate=#{small_gcs} forged_neg=#{forged_neg_gcs} immediate_neg=#{small_neg_gcs}")
  send(parent, :done)
end, [:monitor])
receive do
  :done -> IO.puts("ok")
  {:DOWN, ^ref, _, _, reason} -> IO.puts(:stderr, "probe process died: " <> inspect(reason)); System.halt(1)
after 120_000 -> IO.puts(:stderr, "probe timed out"); System.halt(1)
end
`
		t.Log(runProbe(t, elixirBin, dir, names, probe))
	})
}

var cookRuntimeModuleRE = regexp.MustCompile(`defmodule ([A-Za-z0-9_.]+)\.CookRuntime do`)
