package elixirtable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookMapsRow is one cook-write row of the conformance manifest that carries a
// map or a list (docs/PORTING.md M19): the instance's unit, root and wire, and
// the reference's two cooks.
type cookMapsRow struct {
	name, unit, root, wire, little, big string
}

// cookMapsRows reads testdata/conformance/tables/MANIFEST.txt for every
// cook-write row named map_*, list_* or widelist_*, grouped by the unit's
// source path, and fails when it finds none of the three.
func cookMapsRows(t *testing.T, root string) map[string][]cookMapsRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string]string{}
	instances := map[string][]string{}
	var cooks [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) == 0 {
			continue
		}
		switch {
		case w[0] == "unit" && len(w) >= 3:
			units[w[1]] = w[2]
		case w[0] == "instance" && len(w) >= 5:
			instances[w[1]] = w[2:5]
		case w[0] == "cook-write" && len(w) >= 4:
			cooks = append(cooks, w[1:4])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	out := map[string][]cookMapsRow{}
	for _, c := range cooks {
		name := c[0]
		if !strings.HasPrefix(name, "map_") && !strings.HasPrefix(name, "list_") && !strings.HasPrefix(name, "widelist_") {
			continue
		}
		inst, ok := instances[name]
		if !ok {
			t.Fatalf("cook-write %s names no instance", name)
		}
		path, ok := units[inst[0]]
		if !ok {
			t.Fatalf("instance %s names unit %s, which the manifest does not declare", name, inst[0])
		}
		out[path] = append(out[path], cookMapsRow{name: name, unit: inst[0], root: inst[1], wire: inst[2], little: c[1], big: c[2]})
	}
	for _, prefix := range []string{"map_", "list_", "widelist_"} {
		found := false
		for _, rows := range out {
			for _, r := range rows {
				found = found || strings.HasPrefix(r.name, prefix)
			}
		}
		if !found {
			t.Fatalf("the manifest has no %s* cook-write row: the selection, not the writer, broke", prefix)
		}
	}
	return out
}

// generateElixirUnit compiles one unit's sources through the compiler path
// (`schema generate --lang elixir`) into dir and answers the module load order
// and the Table module's name.
func generateElixirUnit(t *testing.T, root, path, dir string) ([]string, string) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, path)})
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
		t.Fatalf("%s: %v", path, err)
	}
	return elixirtable.WriteModuleDirForTest(t, dir, files), ir.GoExportName(unit.Package) + ".Table"
}

const cookMapsProbeHead = `
defmodule Probe do
  def check(true, _name, _what), do: :ok

  def check(false, name, what) do
    IO.puts(:stderr, "FAIL " <> name <> ": " <> what)
    Process.put(:failed, true)
  end
end

try do
`

const cookMapsProbeTail = `
rescue
  e -> Probe.check(false, "probe", Exception.format(:error, e, __STACKTRACE__))
end

if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
`

// TestCookWriteMapsAndListsElixir is the M19 cook-write gate on the Elixir leg
// (docs/PORTING.md M19, docs/SPEC-TABLES.md §2.8, §2.9, §7.6): every
// cook-write row of the conformance manifest that carries a map or a list —
// the mapdemo rows, the listdemo rows and widelist_wide, the 128-bit list
// whose node aligns sixteen — is loaded from its wire and cooked by
// cook_from, and both byte orders equal the C++ reference's cooks, which
// `schema cook` lands on too. The measure agrees with the bytes and a buffer
// one byte short writes nothing. A row this leg answers absent is a failure,
// so the count printed is every row. It runs the leg's own toolchain, so it is
// a FUNCTIONAL test.
func TestCookWriteMapsAndListsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	byUnit := cookMapsRows(t, root)
	paths := make([]string, 0, len(byUnit))
	total := 0
	for p, rows := range byUnit {
		paths = append(paths, p)
		total += len(rows)
	}
	sort.Strings(paths)
	t.Logf("cook-write: %d map and list rows, both byte orders, across %v", total, paths)
	for _, p := range paths {
		rows := byUnit[p]
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			names, mod := generateElixirUnit(t, root, p, dir)
			var probe strings.Builder
			probe.WriteString(cookMapsProbeHead)
			for _, r := range rows {
				fmt.Fprintf(&probe, `
{name, table, wire, little, big} = {%q, %q, %q, %q, %q}

case %s.load(table, File.read!(wire)) do
  {:ok, value, %%{malformed: false}} ->
    case %s.cook_measure_reason(table, value) do
      {size, :ok} when size > 0 ->
        short = :binary.copy(<<0xA5>>, size - 1)
        Probe.check(%s.cook_from(table, value, short, :little) == false, name, "a short buffer writes nothing")

        for {order, want, suffix} <- [{:little, little, ""}, {:big, big, "-be"}] do
          case %s.cook_from(table, value, :binary.copy(<<0xA5>>, size), order) do
            {true, out} ->
              Probe.check(out == File.read!(want), name <> suffix, "the reference's cook byte for byte")

            other ->
              Probe.check(false, name <> suffix, "cook_from answered " <> inspect(other))
          end
        end

      other ->
        Probe.check(false, name, "absent: the measure answered " <> inspect(other))
    end

  other ->
    Probe.check(false, name, "absent: the load answered " <> inspect(other))
end
`, r.name, r.root, filepath.Join(root, r.wire), filepath.Join(root, r.little), filepath.Join(root, r.big), mod, mod, mod, mod)
			}
			probe.WriteString(cookMapsProbeTail)
			out := elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
			if !strings.Contains(out, "ok") {
				t.Fatalf("probe did not finish:\n%s", out)
			}
			t.Logf("cook-write %s: %d/%d rows, both byte orders", p, len(rows), len(rows))
		})
	}
}

// TestCookMapWriterHoldsTheSortElixir pins the WRITER HOLDS THE SORT and the
// cooked reader's FIND (docs/PORTING.md M19, docs/SPEC-TABLES.md §2.8). An
// Elixir map past thirty-two keys iterates in hash order, and the loaded
// value's integer keys are strings, whose term order is bytewise ("-1" before
// "-10" before "2"), so neither hands the writer its keys ascending: the
// precondition checks that, so the test cannot pass vacuously. Fleet's
// map[int16]Item of sixty-four signed keys and its map[string(32)]ShipConfig
// of forty names are cooked, opened with the cooked reader, and:
//
//   - the entries read through the holder's span lie ascending by key, each
//     value the one its key was given;
//   - <field>_find answers every key's own entry by binary search in place and
//     :error for a key the map does not hold;
//   - the value spelled with integer keys cooks to the same bytes.
//
// map_spans' cook adds a list inside an entry: find each key, then read the
// entry's list through its own span. It runs the leg's own toolchain, so it is
// a FUNCTIONAL test.
func TestCookMapWriterHoldsTheSortElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	names, mod := generateElixirUnit(t, root, "tables/maps", dir)
	var probe strings.Builder
	probe.WriteString(cookMapsProbeHead)
	fmt.Fprintf(&probe, `
mod = %s
c = Mapdemo.FleetCook

cook = fn table, value ->
  {size, :ok} = mod.cook_measure_reason(table, value)
  {true, out} = mod.cook_from(table, value, :binary.copy(<<0>>, size), :little)
  out
end

tiers = Map.new(-32..31, fn k -> {Integer.to_string(k), %%{"count" => k * 3}} end)
ships = Map.new(0..39, fn i -> {"s" <> Integer.to_string(i), %%{"name" => "n", "health" => i}} end)
value = %%{"tiers" => tiers, "ships" => ships}

as_ints = Enum.map(Map.keys(tiers), &String.to_integer/1)
Probe.check(as_ints != Enum.sort(as_ints), "precondition", "the map hands its keys out of order")
Probe.check(Map.keys(ships) != Enum.sort(Map.keys(ships)), "precondition", "the names come out of order")

bytes = cook.("Fleet", value)
{:ok, %%{region: region}} = c.cook_open_fleet(bytes)

{:ok, start, 64} = c.fleet_node_tiers(region, 0)
step = c.fleet_node_tiers_stride()
at = fn i -> start + i * step end
keys = Enum.map(0..63, fn i -> c.fleet_tiers_entry_node_key(region, at.(i)) end)
Probe.check(keys == Enum.to_list(-32..31), "tiers", "entries ascending by signed key: " <> inspect(keys))

counts = Enum.map(0..63, fn i -> c.item_node_count(region, c.fleet_tiers_entry_node_value(region, at.(i))) end)
Probe.check(counts == Enum.map(-32..31, &(&1 * 3)), "tiers", "each entry holds its own key's value")

for k <- -32..31 do
  case c.fleet_node_tiers_find(region, 0, k) do
    {:ok, e} -> Probe.check(c.fleet_tiers_entry_node_key(region, e) == k, "tiers find", "#{k} finds its own entry")
    other -> Probe.check(false, "tiers find", "#{k} answered #{inspect(other)}")
  end
end

for k <- [-33, 32, 1000] do
  Probe.check(c.fleet_node_tiers_find(region, 0, k) == :error, "tiers find", "#{k} is not held")
end

{:ok, sstart, 40} = c.fleet_node_ships(region, 0)
sstep = c.fleet_node_ships_stride()
names = Enum.map(0..39, fn i -> c.fleet_ships_entry_node_key(region, sstart + i * sstep) end)
Probe.check(names == Enum.sort(Map.keys(ships)), "ships", "entries ascending by key bytes: " <> inspect(names))

for n <- Map.keys(ships) do
  case c.fleet_node_ships_find(region, 0, n) do
    {:ok, e} -> Probe.check(c.fleet_ships_entry_node_key(region, e) == n, "ships find", n <> " finds its own entry")
    other -> Probe.check(false, "ships find", n <> " answered " <> inspect(other))
  end
end

Probe.check(c.fleet_node_ships_find(region, 0, "s") == :error, "ships find", "a prefix is not held")
Probe.check(c.fleet_node_ships_find(region, 0, "zz") == :error, "ships find", "zz is not held")

ints = Map.new(tiers, fn {k, v} -> {String.to_integer(k), v} end)
Probe.check(cook.("Fleet", %%{value | "tiers" => ints}) == bytes, "tiers", "integer keys cook the same bytes")

{:ok, spans, _} = mod.load("Spans", File.read!(%q))
{:ok, %%{region: sregion}} = Mapdemo.SpansCook.cook_open_spans(cook.("Spans", spans))
Probe.check(map_size(spans["tracks"]) > 1, "precondition", "map_spans holds more than one track")

for {k, items} <- spans["tracks"] do
  {:ok, e} = Mapdemo.SpansCook.spans_node_tracks_find(sregion, 0, String.to_integer(k))
  {:ok, lstart, n} = Mapdemo.SpansCook.spans_tracks_entry_node_value(sregion, e)
  istep = Mapdemo.SpansCook.spans_tracks_entry_node_value_stride()

  got =
    if n == 0,
      do: [],
      else: Enum.map(0..(n - 1), fn i -> Mapdemo.FleetCook.item_node_count(sregion, lstart + i * istep) end)

  Probe.check(got == Enum.map(items, & &1["count"]), "spans", "key #{k}'s list read in place")
end
`, mod, filepath.Join(root, "testdata", "wire", "tables", "map_spans.bin"))
	probe.WriteString(cookMapsProbeTail)
	elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
}
