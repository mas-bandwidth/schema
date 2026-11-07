package elixirtable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCookNumbersABlobByItsSlotElixir pins docs/SPEC-TABLES.md §2.5, §3.1 and
// §7.6 on the Elixir cook writer: a blob is a node of the ONE slot that holds
// it, as the wire writer numbers it, never a node per content.
//
//   - blob_equal_bytes: two slots holding equal bytes are two blob nodes, and
//     the cook is `schema cook`'s byte for byte in both orders; so is a FRESH
//     value built with equal binaries, which writes three directory entries.
//   - blob_shared: the tolerant read marks the second slot {:blob_shared,
//     bytes}, and the cook packs it as the one node the wire names, the tool's
//     bytes in both orders.
//
// The reference cooks come from the tool's own driver in process. It runs the
// leg's own toolchain, so it is a FUNCTIONAL test.
func TestCookNumbersABlobByItsSlotElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	names := elixirtable.WriteModuleDirForTest(t, dir, files)

	// the tool's cooks of each wire, both orders
	for _, name := range []string{"blob_equal_bytes", "blob_shared"} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		for _, big := range []bool{false, true} {
			cooked, _, _, err := c.Cook(unit, "Catalog", wire, compiler.CookOptions{Big: big})
			if err != nil {
				t.Fatalf("%s: tool cook: %v", name, err)
			}
			suffix := ""
			if big {
				suffix = "-be"
			}
			if err := os.WriteFile(filepath.Join(dir, name+suffix+".cook"), cooked, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	mod := ir.GoExportName(unit.Package) + ".Table"
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
  def check(true, _name, _what), do: :ok
  def check(false, name, what) do
    IO.puts(:stderr, "FAIL " <> name <> ": " <> what)
    Process.put(:failed, true)
  end

  def cook(mod, value, order) do
    {size, :ok} = mod.cook_measure_reason("Catalog", value)
    {true, out} = mod.cook_from("Catalog", value, :binary.copy(<<0>>, size), order)
    out
  end

  # the directory's entry count: the header's attribution length over 16
  def nodes(cook), do: div(:binary.decode_unsigned(binary_part(cook, 32, 8), :little), 16)
end

try do
`)
	fmt.Fprintf(&probe, `
for name <- ["blob_equal_bytes", "blob_shared"] do
  {:ok, value, _} = %[1]s.load("Catalog", File.read!(Path.join(%[2]q, name <> ".bin")))

  for {order, suffix} <- [{:little, ""}, {:big, "-be"}] do
    want = File.read!(Path.join(%[3]q, name <> suffix <> ".cook"))
    Probe.check(Probe.cook(%[1]s, value, order) == want, name <> suffix, "the tool's cook byte for byte")
  end
end

{:ok, shared, _} = %[1]s.load("Catalog", File.read!(Path.join(%[2]q, "blob_shared.bin")))
Probe.check(match?({:blob_shared, _}, shared["alias"]), "blob_shared", "the second slot is marked")

fresh = %%{"name" => "two", "thumb" => <<1, 2, 3>>, "alias" => <<1, 2, 3>>}
Probe.check(Probe.nodes(Probe.cook(%[1]s, fresh, :little)) == 3, "fresh equal blobs", "root and two blob nodes")
one = Map.put(fresh, "alias", {:blob_shared, <<1, 2, 3>>})
Probe.check(Probe.nodes(Probe.cook(%[1]s, one, :little)) == 2, "fresh shared blob", "root and one blob node")
`, mod, filepath.Join(root, "testdata", "wire", "tables"), dir)
	probe.WriteString(`
rescue
  e -> Probe.check(false, "probe", Exception.format(:error, e, __STACKTRACE__))
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
`)
	elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
}
