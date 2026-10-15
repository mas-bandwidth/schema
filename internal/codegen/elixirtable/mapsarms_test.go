package elixirtable_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireElixir pins docs/SPEC-TABLES.md §2.3,
// §2.6, §3 and §4.12 through the compiler path used by schema generate. The
// committed Go-oracle wires and JSON documents must survive an Elixir read,
// write, text read and text write without losing a card field or changing a
// byte. This is a functional test: SCHEMA_REQUIRE_CORPUS=1 requires Elixir.
func TestMapsAndUnionArmsOnTheTableWireElixir(t *testing.T) {
	t.Parallel()
	elixir := os.Getenv("ELIXIR")
	if elixir == "" {
		elixir = "elixir"
	}
	elixir, err := exec.LookPath(elixir)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
			t.Fatalf("required Elixir toolchain: %v", err)
		}
		t.Skipf("Elixir toolchain unavailable: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, schema, table, wire, json string
	}{
		{"mapdemo", "tables/maps", "Fleet", "map_full", "map_full"},
		{"armdemo", "tables/arms", "Gate", "arms_gate", ""},
		{"messagedemo", "tables/messages", "ToolMessage", "message_arms", "message_arms"},
		{"scalars", "tables/scalars", "SimState", "scalars_full", "scalars_full"},
		{"widedemo", "examples-wide", "Caption", "wide_sites", "wide_sites"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// GatherPaths + Load + Generate is the schema generate --lang elixir
			// compiler route. GenerateLineage alone bypasses target refusals.
			paths, err := compiler.GatherPaths([]string{filepath.Join(root, tc.schema)})
			if err != nil {
				t.Fatalf("schema generate paths: %v", err)
			}
			c := compiler.New()
			u, err := c.Load(paths)
			if err != nil {
				t.Fatalf("schema generate load: %v", err)
			}
			files, err := c.Generate(u, "elixir", compiler.Options{})
			if err != nil {
				t.Fatalf("schema generate --lang elixir %s: %v", tc.schema, err)
			}
			if _, ok := files["Table.ex"]; !ok {
				t.Fatal("schema generate omitted Table.ex")
			}
			dir := t.TempDir()
			var names []string
			for name, data := range files {
				if !strings.HasSuffix(name, ".ex") || strings.Contains(name, "/") {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
				names = append(names, name)
			}
			sort.Strings(names)
			for i, name := range names {
				if name == "BuildVersion.ex" {
					names = append([]string{name}, append(names[:i], names[i+1:]...)...)
					break
				}
			}
			wire := filepath.Join(root, "testdata", "wire", "tables", tc.wire+".bin")
			mod := ir.GoExportName(u.Package) + ".Table"
			probe := fmt.Sprintf(`
wire = File.read!(%q)
{:ok, value, report} = %s.load(%q, wire)
if report.malformed, do: raise("malformed oracle wire: #{inspect(report)}")
{:ok, out} = %s.save(%q, value)
if out != wire, do: raise("wire differs from Go oracle")
{:ok, text} = %s.to_json(%q, value)
{:ok, back, text_report} = %s.from_json(%q, text)
if text_report.malformed, do: raise("malformed text: #{inspect(text_report)}")
{:ok, back_wire} = %s.save(%q, back)
if back_wire != wire, do: raise("text round-trip changed Go oracle wire")
`, wire, mod, tc.table, mod, tc.table, mod, tc.table, mod, tc.table, mod, tc.table)
			if tc.json != "" {
				golden := filepath.Join(root, "testdata", "conformance", "tables", "json", tc.json+".json")
				probe += fmt.Sprintf("if text != File.read!(%q), do: raise(\"text differs from Go oracle\")\n", golden)
			}
			if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			args := make([]string, 0, len(names)*2+1)
			for _, name := range names {
				args = append(args, "-r", name)
			}
			args = append(args, "probe.exs")
			cmd := exec.Command(elixir, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Elixir oracle probe: %v\n%s", err, out)
			}
		})
	}
}
