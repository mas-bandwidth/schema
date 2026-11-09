package elixirtable

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestWireFuzzRegressionsElixir pins the five variable-form and retaining
// wire-fuzz regressions found on the Elixir default roster:
//  1. Array header count LEB128 validation precedes element-kind comparison
//     (streamdemo.Feed mutant 137,262).
//  2. Stopped bodies decode their framed field prefix before discarding the
//     unterminated body, without running require_seen on the stopped body
//     (mapdemo.Chunks retain mutant 68,210).
//  3. Retaining capture requires exact content consumption in nested bodies
//     and arrays (listdemo.Save retain mutant 87,889).
//  4. A damaged child table element refuses the whole load with a named
//     reason and no value
//     (docs/SPEC-TABLES.md §4; listdemo.Save retain mutant 91,390,
//     testdata/wire/tables/fuzz-vectors/list_damaged_child_reads_on.bin).
//  5. A bool payload byte above 1 refuses the whole load with a named reason
//     (tabledemo.ProfileConfig mutant 162,022,
//     testdata/wire/tables/fuzz-vectors/bool_byte_above_one.bin).
func TestWireFuzzRegressionsElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir wire-fuzz construct regressions")

	rootDir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	damagedChild, err := os.ReadFile(filepath.Join(rootDir, "testdata", "wire", "tables", "fuzz-vectors", "list_damaged_child_reads_on.bin"))
	if err != nil {
		t.Fatal(err)
	}
	listUnit := loadUnit(t, filepath.Join(rootDir, "tables", "lists", "Save.schema"))
	listModel := tabletext.NewModel(listUnit)
	listRoot := listModel.Lookup("Save")
	var listRep tabletext.Report
	listRetain := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: (1 << 20) / 8}
	ok, err := tablewire.DecodeRetain(listModel, listModel.New(listRoot), damagedChild, listRetain, &listRep)
	if ok || err == nil || !listRep.Refused || listRep.Reason == "" || !tablewire.ReadRefusal(err) {
		t.Fatalf("oracle DecodeRetain(list_damaged_child_reads_on) = (%v, %v, %+v), want ok=false and a named refusal", ok, err, listRep)
	}

	u := unitFrom(t, `package fuzzprobe

type Item
{
    x     int32 = 0
    score float32 = 0.0
}

union Choice
{
    a int32
}

table Node
{
    v int32 = 0
}

table Root
{
    flag    bool = false
    items   []Item
    score   float32 = 0.0
    choices [..2]Choice
    ptr     *Node
}
`)
	files := generateElixir(t, u)
	listFiles := generateElixir(t, listUnit)
	files["ListTable.ex"] = listFiles["Table.ex"]

	probe := `
empty = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %{records: [], used: 0, cap: 65536, ids: [], id_cap: 8192}

items_id = Enum.find(Fuzzprobe.Table.descriptors()["Root"].fields, &(&1.name == "items")).id
flag_id = Enum.find(Fuzzprobe.Table.descriptors()["Root"].fields, &(&1.name == "flag")).id
score_id = Enum.find(Fuzzprobe.Table.descriptors()["Root"].fields, &(&1.name == "score")).id

# 1. Non-canonical array count LEB128 (0x80, 0x00) with mismatched element kind (2 != 13)
# must refuse as malformed with a named reason.
noncanon_array_wire = <<1, 1, 14, 3, 2, 0x80, 0x00, 0, items_id::little-64, 1::little-64>>
case Fuzzprobe.Table.load("Root", noncanon_array_wire) do
  {:error, :malformed, %{reason: reason}} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected malformed on non-canonical array count LEB128, got " <> inspect(other))
end

# 2. Stopped body prefix decoding: score=NaN (0x7FC00000) followed by an invalid field ref 99
# must refuse as :non_finite_float even though entries/4 stops on ref 99.
nan_then_stop_wire = <<1, 1, 10, 0x00, 0x00, 0xC0, 0x7F, 99, 8, 1, 0, 0, 0, 0, score_id::little-64, 1::little-64>>
case Fuzzprobe.Table.load("Root", nan_then_stop_wire) do
  {:error, :non_finite_float, %{reason: "non_finite_float: Root.score"}} -> :ok
  other -> raise("expected non_finite_float on stopped body prefix, got " <> inspect(other))
end
case Fuzzprobe.Table.load_retain("Root", nan_then_stop_wire, store, empty) do
  {:error, :non_finite_float, %{reason: "non_finite_float: Root.score"}, _} -> :ok
  other -> raise("expected retain non_finite_float on stopped body prefix, got " <> inspect(other))
end

# 3. Retaining capture requires exact consumption: an unknown kind-13 body with
# trailing byte after its 0 terminator counts retain_lost=1, retained=0.
unknown_id = 0x1122334455667788
early_term_unknown_wire = <<1, 1, 13, 2, 0, 0xFF, 0, unknown_id::little-64, 1::little-64>>
case Fuzzprobe.Table.load_retain("Root", early_term_unknown_wire, store, empty) do
  {:ok, _v, %{unknown: 1, retained: 0, retain_lost: 1, reason: ""}, _rt} -> :ok
  other -> raise("expected retain_lost=1 on early-terminated unknown body, got " <> inspect(other))
end

# 4. list_damaged_child_reads_on vector: a damaged child body in a table array
# refuses the whole load with a named reason.
damaged_child_wire = Base.decode16!("` + hex.EncodeToString(damagedChild) + `", case: :lower)
case Listdemo.Table.load("Save", damaged_child_wire) do
  {:error, _, %{reason: reason}} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected plain refusal on list_damaged_child_reads_on, got " <> inspect(other))
end
case Listdemo.Table.load_retain("Save", damaged_child_wire, store, empty) do
  {:error, _, %{reason: reason}, _rt} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected retain refusal on list_damaged_child_reads_on, got " <> inspect(other))
end

# 5. Bool payload byte above 1 (0x11) refuses the load with a named reason.
bad_bool_wire = <<1, 1, 1, 0x11, 0, flag_id::little-64, 1::little-64>>
case Fuzzprobe.Table.load("Root", bad_bool_wire) do
  {:error, :malformed, %{reason: reason}} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected plain malformed on bool byte 0x11, got " <> inspect(other))
end
case Fuzzprobe.Table.load_retain("Root", bad_bool_wire, store, empty) do
  {:error, :malformed, %{reason: reason}, _rt} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected retain malformed on bool byte 0x11, got " <> inspect(other))
end

# 6. Node table riding twice (framing N7): last occurrence wins even if earlier
# occurrence had a mismatched inner node count.
node_id = 0xFFFFFFFFFFFFFFFF
node_table_twice_wire = <<1, 1, 12, 1, 99, 1, 12, 1, 0, 0, node_id::little-64, 1::little-64>>
case Fuzzprobe.Table.load("Root", node_table_twice_wire) do
  {:ok, _v, %{reason: ""}} -> :ok
  other -> raise("expected clean load when second node_table overwrites damaged first, got " <> inspect(other))
end

# 7. Unknown union arm inside a union array with L past remaining bytes is malformed.
choices_id = Enum.find(Fuzzprobe.Table.descriptors()["Root"].fields, &(&1.name == "choices")).id
bad_unknown_arm_wire = <<1, 1, 14, 6, 15, 1, 2, 8, 55, 0, 0, choices_id::little-64, unknown_id::little-64, 2::little-64>>
case Fuzzprobe.Table.load("Root", bad_unknown_arm_wire) do
  {:error, :malformed, %{reason: reason}} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected plain malformed on truncated unknown union arm, got " <> inspect(other))
end
case Fuzzprobe.Table.load_retain("Root", bad_unknown_arm_wire, store, empty) do
  {:error, :malformed, %{reason: reason}, _rt} when is_binary(reason) and reason != "" -> :ok
  other -> raise("expected retain malformed on truncated unknown union arm, got " <> inspect(other))
end

IO.puts("WIRE_FUZZ_REGRESSIONS_OK")
`

	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil || !strings.Contains(out, "WIRE_FUZZ_REGRESSIONS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("wire-fuzz regression probe failed: %v\n%s", err, out)
	}

	// Negative control: accepting any nonzero byte as a clean bool leaves bad_bool_wire green.
	const target = `  defp read_payload(%{t: :bool}, <<b, _::binary>>, _nodes, _ids, report) do
    report = if b > 1, do: throw(:wire_damaged), else: report
    {:cont, b != 0, report}
  end`
	const sabotage = `  defp read_payload(%{t: :bool}, <<b, _::binary>>, _nodes, _ids, report) do
    {:cont, b != 0, report}
  end`
	tableEx := string(files["Table.ex"])
	if !strings.Contains(tableEx, target) {
		t.Fatal("negative control target not found in generated Table.ex")
	}
	badFiles := make(map[string][]byte, len(files))
	maps.Copy(badFiles, files)
	badFiles["Table.ex"] = []byte(strings.Replace(tableEx, target, sabotage, 1))
	if badOut, badErr := runArmProbe(t, elixirBinary(t), badFiles, probe); badErr == nil {
		t.Fatalf("negative control passed unexpectedly:\n%s", badOut)
	}
}
