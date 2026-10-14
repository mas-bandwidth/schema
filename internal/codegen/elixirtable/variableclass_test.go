package elixirtable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVariableClassBeyondCardConstructsElixir pins the rows this card carries:
// pointers to tables and the flat node table (graphdemo, tblp2; docs/SPEC-TABLES.md
// §2.1, §3) and blobs, *bytes and *string (blobdemo; §2.5). Each of those units
// round-trips through the generated Elixir against the Go oracle: the committed
// wire loads, re-saves byte for byte, and its committed JSON text round-trips
// identically. listdemo still round-trips here. Unbounded arrays, arrays of
// pointers, the id-table wire and the variable-class text form are not this
// card's rows and stay todo. Functional (elixirBinary): run with
// SCHEMA_REQUIRE_CORPUS=1.
func TestVariableClassBeyondCardConstructsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	units := []struct {
		name   string
		schema []string
		root   string
		wire   string
		json   string
	}{
		{
			name:   "graphdemo",
			schema: []string{
				filepath.Join("tables", "pointers", "Graph.schema"),
				filepath.Join("tables", "pointers", "Marks.schema"),
				filepath.Join("tables", "pointers", "Parts.schema"),
			},
			root: "Scene",
			wire: "graph_tree.bin",
			json: "graph_tree.json",
		},
		{
			name:   "blobdemo",
			schema: []string{filepath.Join("tables", "blobs", "Assets.schema")},
			root:   "Catalog",
			wire:   "blob_small.bin",
			json:   "blob_small.json",
		},
		{
			name:   "listdemo",
			schema: []string{filepath.Join("tables", "lists", "Save.schema")},
			root:   "Save",
			wire:   "list_scalars.bin",
			json:   "list_scalars.json",
		},
		{
			name:   "tblp2",
			schema: []string{filepath.Join("test", "tables", "P2.schema")},
			root:   "Chain",
			wire:   "chain_pointer.bin",
			json:   "",
		},
	}

	for _, uu := range units {
		u := uu
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			paths := make([]string, len(u.schema))
			for i, p := range u.schema {
				paths[i] = filepath.Join(root, p)
			}
			unit := loadUnit(t, paths...)
			files := generateElixir(t, unit)
			if _, ok := files["Table.ex"]; !ok {
				t.Fatalf("%s generated no variable form module", u.name)
			}
			ns := ir.GoExportName(unit.Package)
			mod := ns + ".Table"
			dir := t.TempDir()
			// BuildVersion is read at cook-module compile time. Alphabetical
			// order loads AssetsCook before it (docs/SPEC-TABLES.md §20).
			names := sortedRuntimeFirst(writeModuleDir(t, dir, files))
			wire := filepath.Join(root, "testdata", "wire", "tables", u.wire)

			var probe strings.Builder
			probe.WriteString(`
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wire) + `)
{:ok, value, report} = ` + mod + `.load("` + u.root + `", wire)
Probe.check(report.malformed == false, "clean load: " <> inspect(report))
{:ok, out} = ` + mod + `.save("` + u.root + `", value)
Probe.check(out == wire, "re-save byte for byte")
`)
			if u.json != "" {
				json := filepath.Join(root, "testdata", "conformance", "tables", "json", u.json)
				probe.WriteString("\njson = File.read!(" + elixirString(json) + `)
{:ok, text} = ` + mod + `.to_json("` + u.root + `", value)
Probe.check(text == json, "to_json matches the committed JSON byte for byte")
{:ok, back, _breport} = ` + mod + `.from_json("` + u.root + `", json)
{:ok, out2} = ` + mod + `.save("` + u.root + `", back)
Probe.check(out2 == wire, "the text read back re-saves byte for byte")
`)
			}
			probe.WriteString("\nIO.puts(\"ok\")\n")
			runProbe(t, elixirBin, dir, names, probe.String())
		})
	}
}
