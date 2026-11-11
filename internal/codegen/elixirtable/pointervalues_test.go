package elixirtable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestElixirPointerValuesAreANodeDirectory holds the public shape of a loaded
// pointered value on the Elixir leg (docs/SPEC-TABLES.md §3.1): the value is
// {root, nodes}, a root map whose pointer fields hold node numbers (nil for
// null) beside a tuple of the table nodes in index order. That shape keeps the
// value's flat size linear in the wire's bytes, so a reader can copy it across
// a process boundary without the runtime re-expanding a shared graph.
func TestElixirPointerValuesAreANodeDirectory(t *testing.T) {
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
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(framing, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	// The canonical re-encode of the committed wire is the reference's own
	// writer output; the committed framing vector may carry a noncanonical id
	// order (§3.1). Save must agree with the reference, not with the fixture.
	model := tabletext.NewModel(u)
	canonical := func(name, table string) []byte {
		value := model.New(model.Lookup(table))
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, value, read(name), &report); !ok || err != nil || report.Refused {
			t.Fatalf("canonical %s decode: ok=%v err=%v report=%+v", name, ok, err, report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("canonical %s encode: %v", name, err)
		}
		return wire
	}
	diamondCanonical := canonical("node_diamond_chain", "Scene")
	chainCanonical := canonical("node_chain_two", "ListNode")

	dir := t.TempDir()
	diamondCanonicalPath := filepath.Join(dir, "diamond_canonical.bin")
	chainCanonicalPath := filepath.Join(dir, "chain_canonical.bin")
	if err := os.WriteFile(diamondCanonicalPath, diamondCanonical, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chainCanonicalPath, chainCanonical, 0o600); err != nil {
		t.Fatal(err)
	}

	var probe strings.Builder
	probe.WriteString(pointerDirectoryProbeHelpers)
	fmt.Fprintf(&probe, `
diamond_wire = File.read!(%q)
{:ok, diamond, _r} = Graphdemo.Table.load("Scene", diamond_wire)
Probe.check(is_tuple(diamond), "diamond: value is not {root, nodes}")
{root, nodes} = diamond
Probe.check(is_map(root) and is_tuple(nodes), "diamond: value is not root + directory")
Probe.check(tuple_size(nodes) == 81, "diamond: directory has #{tuple_size(nodes)} nodes, want 81")

# A pointer field holds the node number; deref follows it to the one value.
tree_ref = root["tree"]
Probe.check(is_integer(tree_ref), "diamond: tree is not a node number")
tree = Graphdemo.Table.deref(diamond, tree_ref)
Probe.check(is_map(tree), "diamond: deref did not follow the tree pointer")

# The flat size is linear in the wire's bytes: copyable across a process.
Process.flag(:max_heap_size, %%{size: 100_000_000, kill: true, error_logger: false})
flat = :erts_debug.flat_size(diamond)
Probe.check(flat <= 8 * byte_size(diamond_wire),
  "diamond: flat size #{flat} is not linear in #{byte_size(diamond_wire)} bytes")

# Both parents name the shared node by the same number (§3.1).
left = Graphdemo.Table.deref(diamond, tree["left"])
right = Graphdemo.Table.deref(diamond, tree["right"])
Probe.check(left["left"] == right["left"] and left["right"] == right["right"],
  "diamond: shared children do not share a node number")

# Round-trips: save byte for byte, text to the committed reference.
{:ok, saved} = Graphdemo.Table.save("Scene", diamond)
Probe.check(saved == File.read!(%q), "diamond: save did not match the canonical writer")
{:ok, text} = Graphdemo.Table.to_json("Scene", diamond)
Probe.check(text == File.read!(%q), "diamond: text did not match the committed reference")
{:ok, back, _br} = Graphdemo.Table.from_json("Scene", text)
{:ok, back_saved} = Graphdemo.Table.save("Scene", back)
Probe.check(back_saved == saved, "diamond: text round trip moved bytes")

# A pointered value with no shared nodes has the same shape.
{:ok, chain, _cr} = Graphdemo.Table.load("ListNode", File.read!(%q))
Probe.check(is_tuple(chain), "chain: value is not {root, nodes}")
{chain_root, chain_nodes} = chain
Probe.check(is_map(chain_root) and is_tuple(chain_nodes), "chain: value is not root + directory")
Probe.check(chain_root["next"] == 2, "chain: next is not a node number")
Probe.check(is_map(Graphdemo.Table.deref(chain, 2)), "chain: deref did not follow next")
{:ok, chain_saved} = Graphdemo.Table.save("ListNode", chain)
Probe.check(chain_saved == File.read!(%q), "chain: save did not match the canonical writer")

# The cook writer numbers nodes by these numbers too: the loaded value cooks
# and the cooked region opens with the cook reader (§7.6).
{size, :ok} = Graphdemo.Table.cook_measure_reason("Scene", diamond)
Probe.check(size > 0, "diamond: cook measure refused")
{true, cook} = Graphdemo.Table.cook_from("Scene", diamond, :binary.copy(<<0>>, size), :little)
Probe.check(Graphdemo.GraphCook.cook_open_scene(cook) != :error, "diamond: the cook did not open")

IO.puts("ok: Elixir pointer values are a node directory")
`, filepath.Join(framing, "node_diamond_chain.bin"), diamondCanonicalPath, filepath.Join(root, "testdata", "conformance", "tables", "framing", "scene_diamond_chain.json"), filepath.Join(framing, "node_chain_two.bin"), chainCanonicalPath)
	names := writeModuleDir(t, dir, files)
	out := runProbe(t, elixirBin, dir, names, probe.String())
	if !strings.Contains(out, "ok: Elixir pointer values are a node directory") {
		t.Fatalf("native pointer-directory probe: %s", out)
	}
	t.Log(strings.TrimSpace(out))
}

const pointerDirectoryProbeHelpers = `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
`
