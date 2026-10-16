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

// TestMapsAndUnionArmsOnTheTableWireElixir pins docs/SPEC-TABLES.md §2.3
// (optional fields and optional arrays), §2.4 and §3.2 (enum-keyed arrays),
// §2.6 (union arms of every field type, arrays of unions), §2.8 (maps), §3
// (the fixed-point and 128-bit kinds, wstring(N)) and §4.12, through the
// COMPILER path `schema generate --lang elixir` takes: GatherPaths, Load and
// Generate, so a target refusal the backend never sees still fails here.
//
// For every committed instance of mapdemo, messagedemo, scalars and widedemo
// in testdata/conformance/tables/MANIFEST.txt, and the tables/arms goldens,
// the generated Elixir loads the Go oracle's wire clean, saves it back byte
// for byte, writes the committed JSON byte for byte, and reads that JSON back
// to the same wire: a card written in one language keeps every field across
// the hop. It runs the leg's own toolchain, so it is a FUNCTIONAL test and
// SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain into a failure.
func TestMapsAndUnionArmsOnTheTableWireElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	type instance struct{ name, table string }
	units := []struct {
		name, schema string
		// backendOnly: the compiler refuses the unit for a reason outside this
		// backend, so the wire and text are proven through elixirtable.Generate
		// and the refusal is pinned by its text.
		backendOnly string
		instances   []instance
	}{
		{"mapdemo", "tables/maps", "", []instance{
			{"map_full", "Fleet"}, {"map_empty", "Fleet"}, {"map_cells", "Cells"}, {"map_chunks", "Chunks"},
			{"map_crews", "Crews"}, {"map_depth", "Depth"}, {"map_docs", "Docs"}, {"map_pairs", "Pairs"},
			{"map_slots", "Slots"}, {"map_spans", "Spans"}, {"map_text", "Text"}, {"map_trails", "Trails"},
			{"map_runs", "Runs"},
		}},
		// tables/arms declares `union Carry` in Carry.schema, and the packet
		// emitter (internal/codegen/elixir) writes Carry.ex for the schema
		// file: the compiler refuses the collision by name before this backend
		// runs. The arms are this card's; the module naming is not.
		{"armdemo", "tables/arms", "collides with the module the Elixir emitter writes", []instance{
			{"arms_gate", "Gate"}, {"arms_holder", "Holder"}, {"arms_hand", "Hand"}, {"arms_chain", "Chain"},
			{"arms_nest", "Nest"}, {"arms_ring", "Ring"}, {"arms_rack", "Rack"}, {"arms_tray", "Tray"},
		}},
		{"messagedemo", "tables/messages", "", []instance{
			{"message_open", "ToolMessage"}, {"message_transaction", "ToolMessage"}, {"message_ping", "ToolMessage"},
			{"message_save", "ToolMessage"}, {"message_empty", "ToolMessage"}, {"message_batch", "ToolMessage"},
			{"message_trace", "ToolMessage"}, {"message_arms", "ToolMessage"}, {"message_arm_edits", "ToolMessage"},
			{"message_ack", "ToolMessage"},
		}},
		{"scalars", "tables/scalars", "", []instance{
			{"scalars_full", "SimState"}, {"scalars_default", "SimState"}, {"scalars_edges", "SimState"},
		}},
		// examples-wide declares `fixed table Caption` in Caption.schema: the
		// fixed form's struct module and the packet emitter's file module are
		// both Wide.Caption, so the compiler refuses the unit by name
		// (elixirtable.go). The wire does not name files, so the same
		// declarations under another file name prove wstring(N) on the
		// committed Go oracle goldens through the compiler path.
		{"widedemo", "examples-wide", "", []instance{
			{"wide_empty", "Caption"}, {"wide_title", "Caption"}, {"wide_sites", "Caption"},
			{"wide_narrow_arm", "Caption"}, {"wide_stamp", "Stamp"},
		}},
	}
	for _, uu := range units {
		u := uu
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			// THE COMPILER PATH (schema generate --lang elixir): GatherPaths,
			// Load, then the target's Generate with its refusals.
			schemaDir := filepath.Join(root, u.schema)
			if u.name == "widedemo" {
				collided, err := compiler.GatherPaths([]string{schemaDir})
				if err != nil {
					t.Fatal(err)
				}
				c := compiler.New()
				unit, err := c.Load(collided)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Generate(unit, "elixir", compiler.Options{}); err == nil || !strings.Contains(err.Error(), "fixed table Caption collides with the module") {
					t.Fatalf("examples-wide: want the Caption module collision refused by name, got %v", err)
				}
				schemaDir = t.TempDir()
				for from, to := range map[string]string{"Caption.schema": "CaptionText.schema", "WideText.schema": "WideText.schema", "schema.lock": "schema.lock"} {
					data, err := os.ReadFile(filepath.Join(root, u.schema, from))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(schemaDir, to), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			paths, err := compiler.GatherPaths([]string{schemaDir})
			if err != nil {
				t.Fatalf("schema generate %s: %v", u.schema, err)
			}
			c := compiler.New()
			unit, err := c.Load(paths)
			if err != nil {
				t.Fatalf("schema generate %s: load: %v", u.schema, err)
			}
			files, err := c.Generate(unit, "elixir", compiler.Options{})
			if u.backendOnly != "" {
				if err == nil || !strings.Contains(err.Error(), u.backendOnly) {
					t.Fatalf("schema generate --lang elixir %s: want the refusal %q, got %v", u.schema, u.backendOnly, err)
				}
				files, err = elixirtable.Generate(unit)
			}
			if err != nil {
				t.Fatalf("schema generate --lang elixir %s refused: %v", u.schema, err)
			}
			if _, ok := files["Table.ex"]; !ok {
				t.Fatalf("schema generate --lang elixir %s emitted no Table.ex", u.schema)
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
`)
			for _, in := range u.instances {
				wire := filepath.Join(root, "testdata", "wire", "tables", in.name+".bin")
				fmt.Fprintf(&probe, `
try do
  name = %[1]q
  wire = File.read!(%[2]q)
  {:ok, value, report} = %[3]s.load(%[4]q, wire)
  Probe.check(report.malformed == false and report.unknown == 0 and report.kind_mismatch == 0, name, "clean load: " <> inspect(report))
  {:ok, out} = %[3]s.save(%[4]q, value)
  Probe.check(out == wire, name, "re-save byte for byte: " <> inspect(out, limit: :infinity) <> " want " <> inspect(wire, limit: :infinity))
`, in.name, wire, mod, in.table)
				golden := filepath.Join(root, "testdata", "conformance", "tables", "json", in.name+".json")
				if _, err := os.Stat(golden); err == nil {
					fmt.Fprintf(&probe, `
  json = File.read!(%[1]q)
  {:ok, text} = %[2]s.to_json(%[3]q, value)
  Probe.check(text == json, name, "to_json matches the Go oracle's text:\n" <> text)
  {:ok, back, breport} = %[2]s.from_json(%[3]q, json)
  Probe.check(breport.malformed == false and breport.unknown == 0 and breport.kind_mismatch == 0 and breport.clamped == 0, name, "clean text read: " <> inspect(breport))
  {:ok, out2} = %[2]s.save(%[3]q, back)
  Probe.check(out2 == wire, name, "the text read back re-saves byte for byte: " <> inspect(out2, limit: :infinity))
`, golden, mod, in.table)
				}
				probe.WriteString(`
rescue
  e -> Probe.check(false, ` + fmt.Sprintf("%q", in.name) + `, Exception.format(:error, e, __STACKTRACE__))
end
`)
			}
			probe.WriteString("\nif Process.get(:failed), do: System.halt(1), else: IO.puts(\"ok\")\n")
			elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
		})
	}
}
