package elixirtable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// storageCapExtra adds Fat, a record of Big's storage plus a list, and Holder,
// whose wires put damage the measure would reach only by walking past the
// cap: 8192 Fat elements whose first element's list count passes its bytes,
// and 8192 Big node records followed by one such Fat record. An Elixir integer
// never wraps, so the term check and the running node total are what stop the
// walk at the cap; without them the measure walks on and answers malformed.
const storageCapExtra = `
table Fat
{
    filler [16777216]int64
    xs  []int32
}

table Holder
{
    items []Fat
    next  *Big
    fat   *Fat
}
`

// storageCapProbe measures every vector, and reads each over vector through
// load/2 and load_retain/4, each in a process whose heap is capped at 64 MiB:
// a read that builds even one Big before it refuses is killed and answers
// "built", instead of building 8192 lists of 2^24 integers.
const storageCapProbe = `
defmodule Probe do
  def bounded(fun) do
    parent = self()

    {pid, ref} =
      spawn_monitor(fn ->
        Process.flag(:max_heap_size, %{size: 8_388_608, kill: true, error_logger: false})
        send(parent, {:done, self(), fun.()})
      end)

    receive do
      {:done, ^pid, result} ->
        Process.demonitor(ref, [:flush])
        result

      {:DOWN, ^ref, :process, ^pid, _} ->
        :built
    end
  end

  def show({:ok, n}), do: "ok " <> Integer.to_string(n)
  def show({:error, reason}), do: "error " <> Atom.to_string(reason)
  def show({:error, reason, report}), do: "error " <> Atom.to_string(reason) <> " " <> report.reason
  def show({:error, reason, report, _}), do: "error " <> Atom.to_string(reason) <> " " <> report.reason
  def show(:built), do: "built"
  def show(other), do: "read " <> inspect(elem(other, 0))
end

store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
zero = %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

for {name, root, file, read} <- CASES do
  wire = File.read!(file)
  IO.puts(name <> ".measure=" <> Probe.show(Capcheck.Table.load_measure_reason(root, wire)))

  if read do
    IO.puts(name <> ".load=" <> Probe.show(Probe.bounded(fn -> Capcheck.Table.load(root, wire) end)))

    IO.puts(
      name <> ".retain=" <>
        Probe.show(Probe.bounded(fn -> Capcheck.Table.load_retain(root, wire, store, zero) end))
    )
  end
end
`

// storageCapForm1 is a form-1 file: the root body (terminated here) and the
// id table.
func storageCapForm1(root []byte, ids []uint64) []byte {
	wire := append(append([]byte{1}, root...), 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func storageCapFramed(body []byte) []byte {
	return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
}

// storageCapFatWires writes the two Holder wires storageCapExtra describes.
func storageCapFatWires(u *ir.Unit) (list, nodes []byte) {
	holder := u.Tables["Holder"]
	xs := ir.TableFieldWireId(u.Tables["Fat"].Fields[1])
	// a Fat body whose list (id table entry ref) announces five int32s it does
	// not carry
	bad := func(ref byte) []byte {
		return storageCapFramed([]byte{ref, byte(ir.TableKindArray), 2, byte(ir.TableKindI32), 5, 0})
	}
	items := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, 8192)
	items = append(items, bad(2)...)
	for range 8191 {
		items = append(items, 1, 0)
	}
	root := append([]byte{1, byte(ir.TableKindArray)}, storageCapFramed(items)...)
	list = storageCapForm1(root, []uint64{ir.TableFieldWireId(holder.Fields[0]), xs})

	records := binary.AppendUvarint(nil, 8193)
	for range 8192 {
		records = append(records, 2, 1, 0)
	}
	records = append(records, 3)
	records = append(records, bad(4)...)
	root = append([]byte{1, byte(ir.TableKindString)}, storageCapFramed(records)...)
	nodes = storageCapForm1(root, []uint64{math.MaxUint64, ir.TableWireId(u.Tables["Big"].WireName()),
		ir.TableWireId(u.Tables["Fat"].WireName()), xs})
	return list, nodes
}

// storageCapRun generates the unit, applies patch to the emitted Table
// module (nil for none), and runs the probe.
func storageCapRun(t *testing.T, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	repo, err := filepath.Abs(storageCapDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repo, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	schema := filepath.Join(dir, "Cap.schema")
	if err := os.WriteFile(schema, append(src, storageCapExtra...), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schema)
	files := generateElixir(t, u)
	if patch != nil {
		patch(files)
	}
	list, nodes := storageCapFatWires(u)
	var cases strings.Builder
	cases.WriteString("[")
	for _, c := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		fmt.Fprintf(&cases, "{%q, \"Root\", %q, %t}, ", c, filepath.Join(repo, c+".bin"), strings.HasSuffix(c, "_over"))
	}
	for name, wire := range map[string][]byte{"fat_list": list, "fat_nodes": nodes} {
		path := filepath.Join(dir, name+".bin")
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "{%q, \"Holder\", %q, false}, ", name, path)
	}
	cases.WriteString("]")
	out, err := runArmProbe(t, elixirBinary(t), files, strings.Replace(storageCapProbe, "CASES", cases.String(), 1))
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	return got
}

// storageCapFailures answers what the probe's answers break of §11: an at
// vector measures in (0, 2^40], every over vector refuses by name at measure
// and through both loads without building a Big, and both Holder wires refuse
// at the cap before the walk reaches their damage.
func storageCapFailures(got map[string]string) []string {
	var bad []string
	for _, c := range []string{"list_at", "nodes_at"} {
		n, err := strconv.ParseInt(strings.TrimPrefix(got[c+".measure"], "ok "), 10, 64)
		if !strings.HasPrefix(got[c+".measure"], "ok ") || err != nil || n <= 0 || n > ir.MaxSizeBytes {
			bad = append(bad, c+" measure "+got[c+".measure"])
		}
	}
	want := map[string]string{
		"list_over.measure":  "error storage_over_cap",
		"list_over.load":     "error storage_over_cap storage_over_cap",
		"list_over.retain":   "error storage_over_cap storage_over_cap",
		"nodes_over.measure": "error storage_over_cap",
		"nodes_over.load":    "error storage_over_cap storage_over_cap",
		"nodes_over.retain":  "error storage_over_cap storage_over_cap",
		"fat_list.measure":   "error storage_over_cap",
		"fat_nodes.measure":  "error storage_over_cap",
	}
	for k, w := range want {
		if got[k] != w {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %q", k, got[k], w))
		}
	}
	return bad
}

// TestElixirStorageCapRefusedAtMeasure holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors: load_measure_reason
// answers a size for each at vector and storage_over_cap for each over
// vector, and load/2 and load_retain/4 refuse each over vector by name before
// building anything.
func TestElixirStorageCapRefusedAtMeasure(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	t.Parallel()
	if bad := storageCapFailures(storageCapRun(t, nil)); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestElixirStorageCapNegativeControl removes each check from the emitted
// Table module and requires the probe to go red: the extent term, the running
// node total, the final total, and the loads' guard.
func TestElixirStorageCapNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	t.Parallel()
	plants := []struct{ name, from, to string }{
		{"term", "if count > 0 and size > 0 and count > div(@storage_cap - start, size),", "if false,"},
		{"node total", "if Map.get(desc, :storage_base, 0) + blobs + bytes > @storage_cap,", "if false,"},
		{"final total", "if total > @storage_cap, do: {:error, :storage_over_cap}, else: {:ok, total}", "{:ok, total}"},
		{"load guard", "    if storage_over_cap?(table, data) do\n", "    if false do\n"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got := storageCapRun(t, func(files map[string][]byte) { elixirPatch(t, files, p.from, p.to) })
			if len(storageCapFailures(got)) == 0 {
				t.Fatalf("the Table module with the %s check removed still holds the cap: %v", p.name, got)
			}
		})
	}
}
