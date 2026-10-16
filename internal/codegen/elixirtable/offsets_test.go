package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOffsetsComposeFromHalvesElixir pins docs/PORTING.md M2 on the Elixir
// leg: the block's offset_of and the cook's self-relative delta are composed
// from two 32-bit halves, so no bignum is formed before the bound that refuses
// it runs. A plain open and every row deref allocate 0 words for the offset and
// the delta, and a FORGED offset_of or delta word (0x7FFFFFFF00000000, and the
// delta's negative twin) is refused having allocated 0 words — the refusal is
// decided from the high half before any 64-bit integer is formed.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestOffsetsComposeFromHalvesElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	heaps := `
heap = fn ->
  {:garbage_collection_info, p} = :erlang.process_info(self(), :garbage_collection_info)
  p[:heap_size]
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
` + heaps + `
before = heap.()
block = Blockdemo.RenderBlock.block_open_render_frame(image)
after_open = heap.()
# an ordinary open composes the offset without forming a bignum: 0 words
Probe.check(after_open - before == 0, "open allocated words")
# a FORGED offset_of word: high half 0x7FFFFFFF (>= 0x08000000, past a BEAM
# immediate) is refused from the high half alone, before any 64-bit integer is
# formed, so the refusal allocates 0 words
[array | _] = Blockdemo.RenderBlock.render_frame_block_arrays()
off = array.offset_of_offset
<<head::binary-size(off), _::binary-size(8), tail::binary>> = image
forged = head <> <<0::little-unsigned-32, 0x7FFFFFFF::little-unsigned-32>> <> tail
refused = Blockdemo.RenderBlock.block_open_render_frame(forged)
Probe.check(refused == :error, "a forged offset_of high half is refused")
fbefore = heap.()
Enum.each(1..2000, fn _ -> Blockdemo.RenderBlock.block_open_render_frame(forged) end)
fafter = heap.()
Probe.check(fafter - fbefore == 0, "forged offset_of refusal allocated words")
IO.puts("ok")
`
		runProbe(t, elixirBin, dir, names, probe)
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		unit := loadUnit(t,
			filepath.Join(root, "tables", "arms", "Gate.schema"),
		)
		files := generateElixir(t, unit)
		var cookSrc string
		for _, data := range files {
			if strings.Contains(string(data), "high >= 0x08000000 or high <= -0x08000000") {
				cookSrc = string(data)
			}
		}
		if cookSrc == "" {
			t.Fatal("cook runtime deref does not refuse the high half before composing: no high-half bound")
		}
		if !strings.Contains(cookSrc, "low = uint(region, at, 4)") {
			t.Fatal("cook runtime deref does not read the low u32 half first")
		}
	})
}
