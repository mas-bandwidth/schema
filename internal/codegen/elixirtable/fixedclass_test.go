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

// TestFixedClassOnTableWireElixir pins docs/SPEC-TABLES.md §3 (the id-table
// wire, form byte 1), §4.2 (an absent field reads its declared default) and §5
// (the `was` rows) for the fixed class:
//
//   - a closure that HOLDS a fixed table still rides form 1 (tblw1's Fleet
//     reaches Vessel by pointer and by value; tblr1's Cfg IS a fixed table);
//   - the string, bytes and flags defaults on W1 (name = "untitled",
//     tag = "ab", caps = { Jump, Fly }, hull = 100 and a `type`'s label =
//     "new") ride the wire and the text, so a dropped default is red here;
//   - the four renames read across the rename the way the Go leg does: W2's
//     Vessel→Ship table rename, and R2's Silver→Argent variant, ward→shield
//     and ping→pong arm renames and Buff.multiplier→mult type-field rename.
//
// The COMMITTED oracle bytes are the reference's own wire, so every instance
// is loaded, re-saved byte for byte and (where the corpus pins one) round
// tripped through the committed JSON text. It runs the leg's own toolchain, so
// it is a FUNCTIONAL test and SCHEMA_REQUIRE_CORPUS=1 turns a missing toolchain
// into a failure, never a skip.
func TestFixedClassOnTableWireElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	type instance struct{ name, root string }
	units := []struct {
		name, schema string
		instances    []instance
	}{
		{"tblw1", "test/tables/W1.schema", []instance{
			{"w1_fleet", "Fleet"}, {"w1_fleet_default", "Fleet"},
		}},
		{"tblw2", "test/tables/W2.schema", []instance{
			{"w2_fleet", "Fleet"},
		}},
		{"tblr1", "test/tables/R1.schema", []instance{
			{"r1_cfg", "Cfg"}, {"r1_ping", "Cfg"},
		}},
		{"tblr2", "test/tables/R2.schema", []instance{
			{"r2_cfg", "Cfg"},
		}},
	}

	// Generate every unit through the COMPILER path (`schema generate --lang
	// elixir`): GatherPaths, Load and Generate, so a target refusal the backend
	// never sees — the `was` rows of §5 — fails here rather than passing by
	// bypassing the target.
	dir := t.TempDir()
	mods := map[string]string{}
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
		// one directory per unit: every unit writes its own BuildVersion.ex,
		// and the modules are namespaced, so the directories compile together.
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
	// check is one instance read against the reference bytes (§3): form 1, a
	// clean report, a byte-identical re-save, and the committed text both ways.
	check := func(name, wireName, mod, table string, withText bool) {
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
`, name, wire, mod, table)
		golden := filepath.Join(root, "testdata", "conformance", "tables", "json", wireName+".json")
		if _, err := os.Stat(golden); err == nil && withText {
			fmt.Fprintf(&probe, `
  json = File.read!(%[1]q)
  {:ok, text} = %[2]s.to_json(%[3]q, value)
  Probe.check(text == json, name, "to_json matches the Go oracle's text:\n" <> text)
  {:ok, back, breport} = %[2]s.from_json(%[3]q, json)
  Probe.check(breport.malformed == false and breport.unknown == 0 and breport.kind_mismatch == 0, name, "clean text read: " <> inspect(breport))
  {:ok, out2} = %[2]s.save(%[3]q, back)
  Probe.check(out2 == wire, name, "the text read back re-saves byte for byte: " <> inspect(out2, limit: :infinity))
`, golden, mod, table)
		}
		probe.WriteString(`
rescue
  e -> Probe.check(false, ` + fmt.Sprintf("%q", name) + `, Exception.format(:error, e, __STACKTRACE__))
end
`)
	}

	for _, uu := range units {
		for _, in := range uu.instances {
			check(in.name, in.name, mods[uu.name], in.root, true)
		}
	}
	// THE RENAMES (§5), each direction the manifest's report rows name: the
	// NEW side reads the OLD side's bytes in silence and re-saves them byte for
	// byte, so a rename that did not hash the WireName would count unknown or
	// write different bytes. W1 default carries all three defaults, so the
	// table rename is checked over them too.
	for _, cross := range []struct{ name, wire, unit string }{
		{"w1_fleet_as_w2", "w1_fleet", "tblw2"},
		{"w1_fleet_default_as_w2", "w1_fleet_default", "tblw2"},
		{"w2_fleet_as_w1", "w2_fleet", "tblw1"},
		{"r1_cfg_as_r2", "r1_cfg", "tblr2"},
		{"r1_ping_as_r2", "r1_ping", "tblr2"},
		{"r2_cfg_as_r1", "r2_cfg", "tblr1"},
	} {
		table := "Fleet"
		if strings.HasPrefix(cross.name, "r") {
			table = "Cfg"
		}
		check(cross.name, cross.wire, mods[cross.unit], table, false)
	}

	probe.WriteString("\nif Process.get(:failed), do: System.halt(1), else: IO.puts(\"ok\")\n")
	elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
}
