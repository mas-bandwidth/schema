package elixirtable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestBlobIsNumberedByItsSlotElixir pins docs/SPEC-TABLES.md §2.5 and §3.1 on
// the Elixir leg: a blob is a node of the ONE slot that holds it, never a node
// per content. A BEAM binary has no identity, so the slot is the identity.
//
//   - blob_equal_bytes: two slots holding equal bytes are two nodes; the load
//     counts no sharing and the save writes the Go engine's two records byte
//     for byte, and so does a FRESH value built with equal binaries.
//   - blob_shared and blob_shared_string: one blob record named from two
//     slots. Every read refuses it (§2.5; docs/plans/strict-only.md: the
//     tolerant read that kept the value and counted blob_shared is retired).
//     A value whose second slot is marked {:blob_shared, bytes} is still
//     refused by the save and the text writer, by name, rather than written
//     as a copy per slot.
//
// It runs the leg's own toolchain, so it is a FUNCTIONAL test.
func TestBlobIsNumberedByItsSlotElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	units := []struct {
		name, schema string
		probe        func(mod string) string
	}{
		{"blobdemo", "tables/blobs", func(mod string) string {
			equal := filepath.Join(root, "testdata", "wire", "tables", "blob_equal_bytes.bin")
			shared := filepath.Join(root, "testdata", "wire", "tables", "blob_shared.bin")
			return fmt.Sprintf(`
wire = File.read!(%[1]q)
{:ok, value, report} = %[3]s.load("Catalog", wire)
Probe.check(report.blob_shared == 0, "blob_equal_bytes", "no sharing counted: " <> inspect(report))
Probe.check(is_binary(value["thumb"]) and value["thumb"] == value["alias"], "blob_equal_bytes", "both slots read the bytes: " <> inspect(value))
Probe.check(%[3]s.save("Catalog", value) == {:ok, wire}, "blob_equal_bytes", "two nodes, byte for byte")
fresh = Map.merge(value, %%{"thumb" => <<1, 2, 3>>, "alias" => <<1, 2, 3>>})
{:ok, out} = %[3]s.save("Catalog", fresh)
{:ok, back, _} = %[3]s.load("Catalog", out)
{:ok, again} = %[3]s.save("Catalog", back)
Probe.check(again == out and back["alias"] == <<1, 2, 3>>, "fresh equal blobs", "round trip as two nodes")

swire = File.read!(%[2]q)
{:error, _, sreport} = %[3]s.load("Catalog", swire)
Probe.check(sreport.reason != "", "blob_shared", "the read refuses: " <> inspect(sreport))
svalue = Map.put(value, "alias", {:blob_shared, value["thumb"]})
Probe.check(%[3]s.save("Catalog", svalue) == {:error, :blob_shared}, "blob_shared", "save refuses by name")
Probe.check(%[3]s.to_json("Catalog", svalue) == {:error, :blob_shared}, "blob_shared", "text refuses by name")
`, equal, shared, mod)
		}},
		{"mapdemo", "tables/maps", func(mod string) string {
			shared := filepath.Join(root, "testdata", "wire", "tables", "blob_shared_string.bin")
			return fmt.Sprintf(`
swire = File.read!(%[1]q)
{:error, _, sreport} = %[2]s.load("Docs", swire)
Probe.check(sreport.reason != "", "blob_shared_string", "the read refuses: " <> inspect(sreport))
`, shared, mod)
		}},
	}
	for _, uu := range units {
		u := uu
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			paths, err := compiler.GatherPaths([]string{filepath.Join(root, u.schema)})
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
			mod := ir.GoExportName(unit.Package) + ".Table"
			var probe strings.Builder
			probe.WriteString(`
defmodule Probe do
  def check(true, _name, _what), do: :ok
  def check(false, name, what) do
    IO.puts(:stderr, "FAIL " <> name <> ": " <> what)
    Process.put(:failed, true)
  end
end

try do
`)
			probe.WriteString(u.probe(mod))
			probe.WriteString(`
rescue
  e -> Probe.check(false, "probe", Exception.format(:error, e, __STACKTRACE__))
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
`)
			elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
		})
	}
}
