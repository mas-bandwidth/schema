package elixirtable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixturePaths returns every .schema file in a fixture directory, in sorted
// order, so loadUnit sees the whole unit (a unit is a directory of files).
func fixturePaths(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("no .schema files under %s", dir)
	}
	return matches
}

// oracleWire is the reference bytes for one instance: the id-table wire from
// the Go oracle (internal/tablewire), and the text form from internal/tabletext
// (§16.7). It reads the canonical text into a model instance and encodes it, so
// the Elixir leg is held byte-for-byte to the same value the reference writes.
func oracleWire(t *testing.T, u *ir.Unit, table, json string) (wire, text []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(table))
	var r tabletext.Report
	m.Read(inst, []byte(json), &r)
	if !r.Silent() {
		t.Fatalf("oracle read %s: %+v", table, r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode %s: %v", table, err)
	}
	text, err = m.Write(inst)
	if err != nil {
		t.Fatalf("oracle text %s: %v", table, err)
	}
	return wire, text
}

// TestVariableClassBeyondCardConstructsElixir pins the variable form's carry of
// the class beyond the card slice (docs/SPEC-TABLES.md §2.1, §2.5, §2.9, §3,
// §3.1, §15, §16.7): pointers to tables and the flat node table, *bytes blobs,
// unbounded arrays (scalar, string, enum and pointer elements), arrays of
// pointers, the remaining scalar kinds, the escape kind and the text form. Each
// fixture round-trips through the generated Elixir byte-for-byte against the Go
// oracle's wire, and its text form matches the oracle's text.
func TestVariableClassBeyondCardConstructsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		dir     string
		ns      string
		table   string
		json    string
	}{
		{
			name:  "blobs",
			dir:   filepath.Join(root, "tables", "blobs"),
			ns:    "Blobdemo",
			table: "Catalog",
			json: `{
  "name": "catalog",
  "thumb": "aGVsbG8=",
  "note": "a note",
  "alias": "d29ybGQ="
}`,
		},
		{
			name:  "lists",
			dir:   filepath.Join(root, "tables", "lists"),
			ns:    "Listdemo",
			table: "Ints",
			json: `{
  "values": [1, 2, 3],
  "after": 7
}`,
		},
		{
			name:  "pointers",
			dir:   filepath.Join(root, "tables", "pointers"),
			ns:    "Graphdemo",
			table: "ListNode",
			json: `{
  "value": 7,
  "name": "node",
  "next": { "value": 8, "name": "next" }
}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, fixturePaths(t, tc.dir)...)
			files := generateElixir(t, u)
			if _, ok := files["Table.ex"]; !ok {
				names := make([]string, 0, len(files))
				for k := range files {
					names = append(names, k)
				}
				t.Fatalf("%s generated no variable form module; got %v", tc.name, names)
			}
			wire, text := oracleWire(t, u, tc.table, tc.json)
			dir := t.TempDir()
			names := writeModuleDir(t, dir, files)
			wirePath := filepath.Join(dir, "oracle.wire")
			if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
				t.Fatal(err)
			}
			textPath := filepath.Join(dir, "oracle.json")
			if err := os.WriteFile(textPath, text, 0o600); err != nil {
				t.Fatal(err)
			}
			probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wirePath) + `)
text = File.read!(` + elixirString(textPath) + `)
{:ok, value, report} = ` + tc.ns + `.Table.load(` + elixirString(tc.table) + `, wire)
Probe.check(report.malformed == false, "load is clean: #{inspect(report)}")
{:ok, out} = ` + tc.ns + `.Table.save(` + elixirString(tc.table) + `, value)
Probe.check(out == wire, "re-save is byte for byte")
{:ok, json} = ` + tc.ns + `.Table.to_json(` + elixirString(tc.table) + `, value)
Probe.check(json == text, "to_json matches the oracle text byte for byte")
IO.puts("ok")
`
			runProbe(t, elixirBin, dir, names, probe)
		})
	}
}
