package elixirtable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDefaultsAndTableRenameElixir pins docs/SPEC-TABLES.md §4.2 and §5 for the
// Elixir variable form (the id-table wire, §3): a declared string, bytes or
// flags default elides on the WRITE, answers an ABSENT field on the READ and is
// the value the text form renders (§16.1); and a table declaration renamed
// under `was` reads an older writer's bytes across the rename exactly as the Go
// engine reads them. W1's Fleet carries the three defaults, W2 renames Vessel to
// Ship, and R2 renames an enum variant, two union arms and a type field, all
// under `was`. Every comparison is against the COMMITTED Go oracle: its wire
// bytes and the text form its engine wrote.
//
// It runs the leg's own toolchain, so it is a FUNCTIONAL test and
// SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain or corpus into a failure,
// never a skip.
func TestDefaultsAndTableRenameElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	// The conformance gate (make/elixir.mk's ELIXIR_TABLE_UNITS and its stamp)
	// must generate and answer tblw1/tblw2/tblr1/tblr2, or the report rows
	// w1_fleet_as_w2 and r1_cfg_as_r2 are silently absent from the run. This
	// check keeps that wiring red-first.
	mk, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"tblw1:", "tblw2:", "tblr1:", "tblr2:"} {
		if !strings.Contains(string(mk), unit) {
			t.Errorf("make/elixir.mk ELIXIR_TABLE_UNITS missing %q: the conformance run will not answer its wire/report instances",
				strings.TrimSuffix(unit, ":"))
		}
	}
	if !strings.Contains(string(mk), "test/tables/R2.schema") {
		t.Error("make/elixir.mk's build/tables-generated-elixir/.stamp does not depend on test/tables/R2.schema")
	}

	units := []struct{ name, schema string }{
		{"tblw1", "test/tables/W1.schema"},
		{"tblw2", "test/tables/W2.schema"},
		{"tblr1", "test/tables/R1.schema"},
		{"tblr2", "test/tables/R2.schema"},
	}

	// Generate every unit through the COMPILER path (`schema generate --lang
	// elixir`), so a target refusal the backend never sees fails here rather
	// than passing by bypassing the target.
	dir := t.TempDir()
	mods := map[string]string{}
	unitsByName := map[string]*ir.Unit{}
	var builds, rest []string
	for _, uu := range units {
		u := uu
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, u.schema)})
		if err != nil {
			t.Fatalf("schema generate %s: %v", u.schema, err)
		}
		c := compiler.New()
		unit, err := c.Load(paths)
		if err != nil {
			t.Fatalf("schema generate %s: load: %v", u.schema, err)
		}
		out, err := c.Generate(unit, "elixir", compiler.Options{})
		if err != nil {
			t.Fatalf("schema generate --lang elixir %s refused: %v", u.schema, err)
		}
		if _, ok := out["Table.ex"]; !ok {
			t.Fatalf("schema generate --lang elixir %s emitted no variable form (Table.ex)", u.schema)
		}
		sub := filepath.Join(dir, u.name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range elixirtable.WriteModuleDirForTest(t, sub, out) {
			rel := filepath.Join(u.name, n)
			if n == "BuildVersion.ex" {
				builds = append(builds, rel)
			} else {
				rest = append(rest, rel)
			}
		}
		mods[u.name] = ir.GoExportName(unit.Package) + ".Table"
		unitsByName[u.name] = unit
	}
	names := append(builds, rest...)

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
	// check is one instance's wire (§3, §4.2) and its text (§16.1): form 1, a
	// clean load, a byte-identical re-save, the Go engine's text both ways, and
	// the text read back re-saving the same bytes.
	check := func(name, wireName, mod, table, expected string) {
		wire := filepath.Join(root, "testdata", "wire", "tables", wireName+".bin")
		fmt.Fprintf(&probe, `
try do
  name = %[1]q
  wire = File.read!(%[2]q)
  Probe.check(binary_part(wire, 0, 1) == <<1>>, name, "not form 1")
  {:ok, value, report} = %[3]s.load(%[4]q, wire)
  Probe.check(report.malformed == false and report.unknown == 0 and report.kind_mismatch == 0, name, "clean load: " <> inspect(report))
  {:ok, out} = %[3]s.save(%[4]q, value)
  Probe.check(out == wire, name, "re-save byte for byte: " <> inspect(out, limit: :infinity) <> " want " <> inspect(wire, limit: :infinity))
  json = %[5]q
  {:ok, text} = %[3]s.to_json(%[4]q, value)
  Probe.check(text == json, name, "to_json equals the engine's read:\ngot:\n" <> text <> "\nwant:\n" <> json)
  {:ok, back, breport} = %[3]s.from_json(%[4]q, json)
  Probe.check(breport.malformed == false and breport.unknown == 0 and breport.kind_mismatch == 0, name, "clean text read: " <> inspect(breport))
  {:ok, out2} = %[3]s.save(%[4]q, back)
  Probe.check(out2 == wire, name, "the text read back re-saves byte for byte: " <> inspect(out2, limit: :infinity))
rescue
  e -> Probe.check(false, `+fmt.Sprintf("%q", name)+`, Exception.format(:error, e, __STACKTRACE__))
end
`, name, wire, mod, table, expected)
	}

	// engineRead is the Go engine's read of a wire under a unit: the text the
	// leg's to_json must equal, and a silent report when the wire reads clean.
	engineRead := func(t *testing.T, unitName, table, wireName string) string {
		t.Helper()
		unit := unitsByName[unitName]
		if unit == nil {
			t.Fatalf("unit %s was not loaded", unitName)
		}
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", wireName+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(unit)
		inst := m.New(m.Lookup(table))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatalf("engine decode %s under %s: %v", wireName, unitName, err)
		}
		if !report.Silent() {
			t.Fatalf("engine's read of %s under %s is not silent: %+v", wireName, unitName, report)
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("engine write %s under %s: %v", wireName, unitName, err)
		}
		// the committed text form ends its one line with a newline; tabletext's
		// Write returns the text alone, so add it for the comparison.
		return string(text) + "\n"
	}

	// THE DEFAULTS (§4.2): w1_fleet_default is Fleet with every field absent,
	// so its wire is the empty form-1 header and its text is every declared
	// default — the string "fleet"/"untitled"/"new", the bytes "ab" (base64
	// "YWI=") and the flags { Jump, Fly }. w1_fleet carries the same fields OFF
	// their defaults beside a node that holds every default.
	for _, in := range []struct{ name, unit, table string }{
		{"w1_fleet", "tblw1", "Fleet"},
		{"w1_fleet_default", "tblw1", "Fleet"},
		{"w2_fleet", "tblw2", "Fleet"},
	} {
		golden, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", in.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		check(in.name, in.name, mods[in.unit], in.table, string(golden))
	}
	// A FRESH value takes the declared defaults on the WRITE side too: saving an
	// empty Fleet is the default instance's wire byte for byte.
	fmt.Fprintf(&probe, `
try do
  wire = File.read!(%[1]q)
  {:ok, out} = %[2]s.save("Fleet", %%{})
  Probe.check(out == wire, "defaults_written", "a fresh Fleet does not write the declared defaults: " <> inspect(out, limit: :infinity) <> " want " <> inspect(wire, limit: :infinity))
rescue
  e -> Probe.check(false, "defaults_written", Exception.format(:error, e, __STACKTRACE__))
end
`, filepath.Join(root, "testdata", "wire", "tables", "w1_fleet_default.bin"), mods["tblw1"])

	// THE RENAMES (§5): the NEW side reads the OLD side's bytes in silence and
	// re-saves them byte for byte, and its text is the ENGINE's read of the old
	// bytes. A table declaration (Vessel -> Ship), an enum variant
	// (Silver -> Argent), two union arms (ward -> shield, ping -> pong) and a
	// type field (multiplier -> mult) that ignored `was` would count unknown or
	// spell different names.
	for _, cross := range []struct{ name, unit, table, wireName string }{
		{"w1_fleet_as_w2", "tblw2", "Fleet", "w1_fleet"},
		{"w2_fleet_as_w1", "tblw1", "Fleet", "w2_fleet"},
		{"w1_fleet_default_as_w2", "tblw2", "Fleet", "w1_fleet_default"},
		{"r1_cfg_as_r2", "tblr2", "Cfg", "r1_cfg"},
		{"r1_ping_as_r2", "tblr2", "Cfg", "r1_ping"},
		{"r2_cfg_as_r1", "tblr1", "Cfg", "r2_cfg"},
	} {
		check(cross.name, cross.wireName, mods[cross.unit], cross.table, engineRead(t, cross.unit, cross.table, cross.wireName))
	}
	// The renamed units' own instances answer the same way.
	for _, in := range []struct{ name, unit string }{
		{"r1_cfg", "tblr1"}, {"r1_ping", "tblr1"}, {"r2_cfg", "tblr2"},
	} {
		golden, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", in.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		check(in.name, in.name, mods[in.unit], "Cfg", string(golden))
	}

	probe.WriteString("\nif Process.get(:failed), do: System.halt(1), else: IO.puts(\"ok\")\n")
	elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
}
