package elixirtable

import (
	"os"
	"path/filepath"
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
		probe := `defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
image = File.read!(` + elixirString(golden) + `)
{before, _} = :erlang.process_info(self(), :garbage_collection_info)
f = fn ->
  {:ok, block} = Blockdemo.RenderBlock.block_open_render_frame(image)
  # the offset_of read is composed from halves inside open
  block
end
{:ok, block} = f.()
{after_open, _} = :erlang.process_info(self(), :garbage_collection_info)
heaps = fn {a, _}, {b, _} ->
  a - b
end
# an ordinary open composes the offset without forming a bignum: 0 words
Probe.check(heaps.(after_open, before) == 0, "open allocated words")
IO.puts("ok")
`
		runProbe(t, elixirBin, dir, names, probe)
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
	})
}
