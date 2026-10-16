package elixirtable

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestOffsetsComposeFromHalvesElixir pins docs/PORTING.md M2 on the Elixir
// leg: the block's offset_of and the cook's self-relative delta are composed
// from two 32-bit halves, so no bignum is formed before the bound that refuses
// it runs. A plain open is accepted, and a FORGED offset_of word or cook delta
// word (high half 0x7FFFFFFF, and the delta's negative twin) is refused with no
// more minor collections than the same refusal of a word whose high half is
// small: the refusal is decided from the high half before any 64-bit integer
// is formed. The block and the cook are measured the same way.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestOffsetsComposeFromHalvesElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// HOW ALLOCATION IS MEASURED. The first version of this test bracketed the
	// open with garbage_collection_info heap_size inside a process with a huge
	// min_heap_size. Measured on OTP 29: heap_size and total_heap_size are the
	// heap BLOCK size and do not move as terms are allocated until a collection
	// runs, so a bignum formed by a whole-word read changed nothing and the
	// negative control could never go red. What does move is the number of
	// minor collections, so the probe runs in a spawned process with a small
	// heap and counts minor_gcs over many calls. A refusal of a forged offset
	// whose high half is 0x7FFFFFFF is compared with a refusal of a forged offset
	// whose high half is 1 (an immediate in either reader): the same code runs
	// and the same garbage is made, except that only a reader that forms the
	// 64-bit integer allocates a bignum for the first. Equal collection counts
	// mean the high half was refused before any 64-bit integer was formed.
	heaps := `
gcs = fn ->
  {:garbage_collection, p} = :erlang.process_info(self(), :garbage_collection)
  p[:minor_gcs]
end
`
	probeSetup := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
`
	t.Run("block", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "block", "Render.schema"),
			filepath.Join(root, "tables", "block", "Padded.schema"),
		)
		files := generateElixir(t, unit)
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		golden := filepath.Join(root, "testdata", "wire", "tables", "block_render.bin")
		if _, err := os.ReadFile(golden); err != nil {
			t.Fatal(err)
		}
		probe := probeSetup + `image = File.read!(` + elixirString(golden) + `)
parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
Probe.check(is_map(Blockdemo.RenderBlock.block_open_render_frame(image)) or
  is_tuple(Blockdemo.RenderBlock.block_open_render_frame(image)) or
  is_struct(Blockdemo.RenderBlock.block_open_render_frame(image)), "an ordinary open is accepted")
[array | _] = Blockdemo.RenderBlock.render_frame_block_arrays()
off = array.offset_of_offset
<<head::binary-size(off), _::binary-size(8), tail::binary>> = image
# a FORGED offset_of word: high half 0x7FFFFFFF (>= 0x08000000, past a BEAM
# immediate) must be refused from the high half alone, before any 64-bit
# integer is formed
forged = head <> <<0::little-unsigned-32, 0x7FFFFFFF::little-unsigned-32>> <> tail
# the control: an out-of-extent offset that is an immediate in either reader
small = head <> <<0::little-unsigned-32, 1::little-unsigned-32>> <> tail
Probe.check(Blockdemo.RenderBlock.block_open_render_frame(forged) == :error, "a forged offset_of high half is refused")
Probe.check(Blockdemo.RenderBlock.block_open_render_frame(small) == :error, "an out-of-extent offset_of is refused")
count = fn img ->
  :erlang.garbage_collect()
  g0 = gcs.()
  Enum.each(1..50_000, fn _ -> Blockdemo.RenderBlock.block_open_render_frame(img) end)
  gcs.() - g0
end
small_gcs = count.(small)
forged_gcs = count.(forged)
IO.puts("minor gcs over 50000 refusals: forged=#{forged_gcs} immediate=#{small_gcs}")
Probe.check(forged_gcs <= small_gcs, "forged offset_of refusal allocated: " <> Integer.to_string(forged_gcs) <> " collections against " <> Integer.to_string(small_gcs))
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
			filepath.Join(root, "tables", "arms", "Gate.schema"),
		)
		files := generateElixir(t, unit)
		// the cook runtime module that holds deref/2
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
		// The same measurement as the block's: a forged delta word (high half
		// 0x7FFFFFFF, and its negative twin) is refused against a twin whose
		// high half is 1 (or -1), an immediate in either reader. Both resolve
		// outside the region and answer :error; only a reader that forms the
		// 64-bit integer allocates a bignum for the forged one, and that shows
		// as extra minor collections over many refusals.
		probe := probeSetup + `rt = ` + mod + `.CookRuntime
parent = self()
{_pid, ref} = :erlang.spawn_opt(fn ->
` + heaps + `
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
IO.puts("minor gcs over 50000 refusals: forged=#{forged_gcs} immediate=#{small_gcs} forged_neg=#{forged_neg_gcs} immediate_neg=#{small_neg_gcs}")
Probe.check(forged_neg_gcs <= small_neg_gcs, "forged negative cook delta refusal allocated: " <> Integer.to_string(forged_neg_gcs) <> " collections against " <> Integer.to_string(small_neg_gcs))
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
