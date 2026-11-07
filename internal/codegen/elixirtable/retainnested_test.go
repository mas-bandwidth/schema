package elixirtable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRetainedRecordsReturnToTheirOwnBody pins docs/SPEC-TABLES.md §6.6 at
// depth: RT2's wire (testdata/wire/tables/retain_rt2.bin) carries fields RT1
// cannot name inside the root, a nested table by value, array and keyed-array
// elements, a union arm, a map value, a list element and a pointer's node.
// RT1's retaining load keeps them and its retaining save must re-emit each one
// at the end of the body it was read from, byte-equal to the Go and C++
// references' pinned save (testdata/wire/tables/retain_rt1_save.bin).
//
// The load's counters are the manifest's: nine retained, two lost, eleven
// unknown. A record whose body the caller removed, here the array's second
// element, is lost at the save and counted there, never placed in a sibling.
//
// The REVERSED WITNESS plants the defect the test exists for: a save that
// looks up retained records for the root body only. The same probe over that
// runtime must NOT reproduce the reference's bytes, and must not count the
// seven nested records it left behind.
func TestRetainedRecordsReturnToTheirOwnBody(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "elixir (it compiles and runs the generated unit)")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "test", "tables", "RT1.schema"))
	files := generateElixir(t, u)
	table, ok := files[VariableTableModule+".ex"]
	if !ok {
		t.Fatalf("no %s.ex for RT1", VariableTableModule)
	}
	input := filepath.Join(root, "testdata", "wire", "tables", "retain_rt2.bin")
	want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, files map[string][]byte) (string, []byte) {
		t.Helper()
		dir := t.TempDir()
		names := writeModuleDir(t, dir, files)
		out := filepath.Join(dir, "save.wire")
		probe := `
data = File.read!(` + elixirString(input) + `)
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
zero = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
{:ok, value, load, retain} = Tblrt1.Table.load_retain("Node", data, retain, zero)
{:ok, bytes, save, retain} = Tblrt1.Table.save_retain("Node", value, retain, zero)
size = Tblrt1.Table.measure_retain("Node", value, retain)
File.write!(` + elixirString(out) + `, bytes)
# the caller removes the array's second element before the save
{:ok, _bytes, edited, _} =
  Tblrt1.Table.save_retain("Node", Map.update!(value, "items", &Enum.take(&1, 1)), retain, zero)
IO.puts("load=#{load.retained},#{load.retain_lost},#{load.unknown} save=#{save.retain_lost} size=#{size == byte_size(bytes)} edited=#{edited.retain_lost}")
`
		text := runProbe(t, elixirBinary(t), dir, names, probe)
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(text), got
	}

	text, got := run(t, files)
	if text != "load=9,2,11 save=0 size=true edited=1" {
		t.Fatalf("RT1's retaining counters: %q", text)
	}
	if string(got) != string(want) {
		t.Fatalf("RT1's retaining save is %d bytes, the reference's %d: a retained record left its body", len(got), len(want))
	}

	const found = "records = Map.get(retain.by_path, path, [])"
	const planted = "records = if path == [], do: Map.get(retain.by_path, path, []), else: []"
	if strings.Count(string(table), found) != 1 {
		t.Fatal("the witness cannot find the body lookup in the generated runtime")
	}
	sabotaged := map[string][]byte{}
	maps.Copy(sabotaged, files)
	sabotaged[VariableTableModule+".ex"] = []byte(strings.Replace(string(table), found, planted, 1))
	text, old := run(t, sabotaged)
	if string(old) == string(want) || text == "load=9,2,11 save=0 size=true edited=1" {
		t.Fatalf("NEGATIVE CONTROL FAILED: a save that finds the root's records only reproduced the reference (%q)", text)
	}
}
