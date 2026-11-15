package elixirtable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestElixirGraphCheckedOverEveryPointer holds the Elixir strict read to the
// declaration-free graph walk in docs/SPEC-TABLES.md §3.1 and §6.5: the
// graph conditions are checked over every kind-17 payload in the input,
// whether this reader declares the field or the record type or not. A cycle,
// an unreachable record, a blob named from two slots and an ill-formed string
// blob each refuse the load; a record of a type this reader cannot name,
// reached through any pointer, still reads. Every entry point (load/2,
// variable_load/2 and the retaining load) gives the same verdict, and only
// the verdict is compared, never a refusal's name.
func TestElixirGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pointer schema paths: %v (%d files)", err, len(paths))
	}
	files := generateElixir(t, loadUnit(t, paths...))
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	cases := []struct {
		name  string
		table string
		reads bool
	}{
		{"graph_unknown_type_self", "Scene", false},
		{"graph_unknown_type_pair", "Scene", false},
		{"graph_listnode_unknown_self", "Scene", false},
		{"graph_unknown_to_root", "Scene", false},
		{"graph_blob_two_unknown_fields", "Scene", false},
		{"graph_blob_two_unknown_elements", "Scene", false},
		{"graph_string_blob_ill_formed", "Scene", false},
		{"graph_string_blob_zero_byte", "Scene", false},
		{"graph_unknown_type_unreached", "Scene", false},
		{"graph_string_blob_unreached", "Scene", false},
		{"graph_reach_unknown_field", "Scene", true},
		{"graph_reach_unknown_nested", "Scene", true},
		{"graph_reach_unknown_union_arm", "Scene", true},
		{"graph_reach_unknown_keyed_slot", "Scene", true},
		{"node_chain_two", "ListNode", true},
	}
	var probe strings.Builder
	probe.WriteString(graphEveryPointerProbe)
	for _, c := range cases {
		fmt.Fprintf(&probe, "GraphProbe.verdict(Graphdemo.Table, %q, File.read!(%q), %q, %v)\n",
			c.table, filepath.Join(framing, c.name+".bin"), c.name, c.reads)
	}
	probe.WriteString("GraphProbe.done()\n")
	out, err := runArmProbe(t, elixirBin, files, probe.String())
	if err != nil || !strings.Contains(out, "ok: Elixir graph checked over every pointer") {
		t.Fatalf("native graph probe: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(out))
}

const graphEveryPointerProbe = `
defmodule GraphProbe do
  def retain, do: %{records: [{[], 1, 4, <<7, 0, 0, 0>>}], used: 13,
                    cap: 1_048_576, ids: [1], id_cap: 131_072}
  def report, do: %{unknown: 0, widened: 0, retained: 0, retain_lost: 0,
                    reason: "", missing_required: 0}

  def verdict(module, root, wire, label, reads) do
    got = [
      {"load", plain(module.load(root, wire))},
      {"variable_load", plain(module.variable_load(root, wire))},
      {"load_retain", retaining(module.load_retain(root, wire, retain(), report()))}
    ]
    for {entry, read} <- got, read != reads do
      want = if reads, do: "read", else: "refusal"
      IO.puts("FAIL: #{label}: #{entry} did not give the #{want} verdict")
      Process.put(:graph_probe_failed, true)
    end
  end

  defp plain({:ok, {root, nodes}, %{reason: ""}}) when is_map(root) and is_tuple(nodes), do: true
  defp plain({:error, _why, %{reason: reason}}) when reason != "", do: false
  defp plain(_other), do: :unexpected

  defp retaining({:ok, {root, nodes}, %{reason: ""}, _retain}) when is_map(root) and is_tuple(nodes), do: true
  defp retaining({:error, _why, %{reason: reason}, %{records: [], used: 0, ids: []}}) when reason != "", do: false
  defp retaining(_other), do: :unexpected

  def done do
    if Process.get(:graph_probe_failed), do: System.halt(1)
    IO.puts("ok: Elixir graph checked over every pointer")
  end
end
`
