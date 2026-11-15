package elixirtable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestElixirNodeTypeReservedIdRefused holds a node record whose TYPE reference
// resolves to one of the three ids the language holds back to the rule that
// refuses it (docs/SPEC-TABLES.md §3.1, §3.3): the record is malformed and the
// whole load is discarded. The wires are the framing vectors; node_chain_two is
// the control, a lawful node table the same root reads.
func TestElixirNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pointer schema paths: %v (%d files)", err, len(paths))
	}
	u := loadUnit(t, paths...)
	files := generateElixir(t, u)
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")

	var probe strings.Builder
	probe.WriteString(`
defmodule ReservedTypeProbe do
  def check(true, _), do: :ok
  def check(false, what), do: raise("FAIL: " <> what)

  def refused(module, root, wire, label) do
    case module.load(root, wire) do
      {:error, :malformed, %{reason: reason}} when reason != "" -> :ok
      other -> check(false, label <> ": load did not refuse: " <> inspect(other))
    end
  end
end
`)
	for _, name := range []string{
		"node_type_ref_zero",
		"node_type_node_table_id",
		"node_type_build_version_id",
		"node_type_vocabulary_id",
	} {
		probe.WriteString("ReservedTypeProbe.refused(Graphdemo.Table, \"Scene\", File.read!(" +
			elixirString(filepath.Join(framing, name+".bin")) + "), \"" + name + "\")\n")
	}
	probe.WriteString(`
case Graphdemo.Table.load("ListNode", File.read!(` +
		elixirString(filepath.Join(framing, "node_chain_two.bin")) + `)) do
  {:ok, value, %{reason: ""}} ->
    ReservedTypeProbe.check(is_map(value["next"]), "node_chain_two: control lost next")
  other -> ReservedTypeProbe.check(false, "node_chain_two: control did not read: " <> inspect(other))
end
IO.puts("ok: reserved node type ids refused and node_chain_two loaded")
`)
	if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil ||
		!strings.Contains(out, "ok: reserved node type ids refused") {
		t.Fatalf("reserved node type probe: %v\n%s", err, out)
	} else {
		t.Log(strings.TrimSpace(out))
	}
}
