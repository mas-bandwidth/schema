package elixirtable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCyclicWireIsDamageElixir holds docs/SPEC-TABLES.md §3.1 and §4 at
// both native entry points. The graph is the FINAL stored slots: a cycle
// outside the root's reach is damage, and the same field id twice in one body
// is damage too (§4). The forty-level diamond has 2^40 paths over 82 nodes;
// checking the shared node numbers along one path avoids expanding those
// paths in the test itself. There is no wall-clock cutoff on the native probe.
func TestCyclicWireIsDamageElixir(t *testing.T) {
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
	derived := t.TempDir()
	put := func(name string, wire []byte) string {
		p := filepath.Join(derived, name+".bin")
		if err := os.WriteFile(p, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Framing vectors are legal hand-built read inputs, not writer pins:
	// they can carry unused ids and noncanonical field order. Derive each
	// expected writer result once through the independent shared wire model.
	model := tabletext.NewModel(u)
	canonical := func(name, table string) string {
		value := model.New(model.Lookup(table))
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, value, read(name), &report); !ok || err != nil || report.Refused {
			t.Fatalf("canonical %s decode: ok=%v err=%v report=%+v", name, ok, err, report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("canonical %s encode: %v", name, err)
		}
		return put("canonical_"+name, wire)
	}
	chainCanonical := canonical("node_chain_two", "ListNode")
	diamondCanonical := canonical("node_diamond_chain", "Scene")

	unreached := bytes.Clone(read("node_cycle_mid"))
	if len(unreached) < 10 || !bytes.Equal(unreached[7:10], []byte{3, 17, 2}) {
		t.Fatal("node_cycle_mid root-next fixture moved")
	}
	unreached[9] = 0 // the remaining directory records still contain a cycle
	unreachedPath := put("unreached_cycle", unreached)

	base := read("node_cycle_self")
	if len(base) < 10 || !bytes.Equal(base[7:10], []byte{3, 17, 2}) {
		t.Fatal("node_cycle_self root-next fixture moved")
	}
	// Root next=self followed by next=null: the same field id twice in one
	// body is damage (§4), so the whole load is discarded before any cycle
	// check runs. The original node's self edge is cleared as well.
	overwritten := append(bytes.Clone(base[:7]), 3, 17, 1, 3, 17, 0)
	tail := bytes.Clone(base[10:])
	self := []byte{3, 17, 2}
	if bytes.Count(tail, self) != 1 {
		t.Fatal("node_cycle_self directory self-edge fixture moved")
	}
	tail[bytes.Index(tail, self)+2] = 0
	overwritten = append(overwritten, tail...)
	overwrittenPath := put("overwritten_self_cycle", overwritten)

	var probe strings.Builder
	probe.WriteString(elixirCycleProbeHelpers)
	for _, name := range []string{"node_cycle_self", "node_cycle_to_root", "node_cycle_mid"} {
		fmt.Fprintf(&probe, "CycleProbe.damage(Graphdemo.Table, \"ListNode\", File.read!(%q), %q)\n", filepath.Join(framing, name+".bin"), name)
	}
	fmt.Fprintf(&probe, "CycleProbe.damage(Graphdemo.Table, \"ListNode\", File.read!(%q), \"unreached_cycle\")\n", unreachedPath)
	fmt.Fprintf(&probe, "CycleProbe.damage(Graphdemo.Table, \"ListNode\", File.read!(%q), \"overwritten_self_cycle\")\n", overwrittenPath)
	fmt.Fprintf(&probe, `
for value <- CycleProbe.clean(Graphdemo.Table, "ListNode", File.read!(%q), "node_chain_two") do
  {root, _nodes} = value
  CycleProbe.check(is_integer(root["next"]), "node_chain_two: next was lost")
end
started = System.monotonic_time()
diamond_values = CycleProbe.clean(Graphdemo.Table, "Scene", File.read!(%q), "node_diamond_chain")
for value <- diamond_values, do: CycleProbe.diamond(value, 40)
elapsed = System.convert_time_unit(System.monotonic_time() - started, :native, :microsecond)
IO.puts("diamond: both loads preserve shared nodes across 2^40 paths in #{elapsed} us")
IO.puts("ok: Elixir cycle and final-slot graph contract")
`, filepath.Join(framing, "node_chain_two.bin"), filepath.Join(framing, "node_diamond_chain.bin"))
	fmt.Fprintf(&probe, `
# A pointered value is {root, nodes}; node numbers are deterministic across
# independent loads (§3.1): one node, one number.
small_wire = File.read!(%q)
{:ok, first, _first_report} = Graphdemo.Table.load("ListNode", small_wire)
{:ok, second, _second_report} = Graphdemo.Table.load("ListNode", small_wire)
CycleProbe.check(first == second, "identity: identical loads changed public value equality")
{first_root, first_nodes} = first
CycleProbe.check(is_tuple(first_nodes), "identity: loaded value has no node directory")
CycleProbe.check(first_root["next"] == 2, "identity: first pointee has no deterministic number")
CycleProbe.check(Graphdemo.Table.deref(first, 2)["next"] == 3, "identity: second pointee has no deterministic number")
CycleProbe.check(Graphdemo.Table.save("ListNode", first) == {:ok, File.read!(%q)}, "identity: numbers changed canonical chain bytes")

# Two fields naming one number is sharing (§3.1); an edited copy is a distinct
# number, and the distinction survives save and text.
node = Graphdemo.Table.deref(first, 2)
edited = Map.put(node, "value", 777)
edited_nodes = Tuple.insert_at(first_nodes, tuple_size(first_nodes), edited)
scene = %%{"head" => 2, "alias" => 4}
{:ok, edited_wire} = Graphdemo.Table.save("Scene", {scene, edited_nodes})
{:ok, shared_wire} = Graphdemo.Table.save("Scene", {%%{"head" => 2, "alias" => 2}, first_nodes})
CycleProbe.check(edited_wire != shared_wire, "identity: distinct node numbers were collapsed")
{:ok, edited_text} = Graphdemo.Table.to_json("Scene", {scene, edited_nodes})
{:ok, parsed_edit, _} = Graphdemo.Table.from_json("Scene", edited_text)
CycleProbe.check(Graphdemo.Table.save("Scene", parsed_edit) == {:ok, edited_wire},
  "edited_copy: text round trip moved bytes")
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", edited_wire, "edited_copy") do
  {r, _n} = value
  alias_ = Graphdemo.Table.deref(value, r["alias"])
  CycleProbe.check(r["head"] == 2 and r["alias"] == 4 and alias_["value"] == 777,
    "edited_copy: distinct nodes lost their numbers or content")
end
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", shared_wire, "shared_number") do
  {r, _n} = value
  CycleProbe.check(r["head"] == r["alias"], "identity: shared node number was copied")
end

# The large shared graph reaches save and text. Their result sizes are bounded
# by nodes, and the saved graph still names one node by one number.
diamond = hd(diamond_values)
{:ok, diamond_text} = Graphdemo.Table.to_json("Scene", diamond)
CycleProbe.check(byte_size(diamond_text) < 100_000 and String.contains?(diamond_text, "&node"),
  "diamond: text expanded paths rather than naming shared nodes")
CycleProbe.check(diamond_text == File.read!(%q), "diamond: text disagrees with the canonical reference")
{:ok, diamond_saved} = Graphdemo.Table.save("Scene", diamond)
CycleProbe.check(byte_size(diamond_saved) < 100_000, "diamond: saved wire expanded paths")
CycleProbe.check(diamond_saved == File.read!(%q), "diamond: wire disagrees with the independent canonical writer")
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", diamond_saved, "diamond_saved") do
  CycleProbe.diamond(value, 40)
  CycleProbe.check(Graphdemo.Table.save("Scene", value) == {:ok, diamond_saved}, "diamond: save round trip moved bytes")
  CycleProbe.check(Graphdemo.Table.to_json("Scene", value) == {:ok, diamond_text}, "diamond: text changed after save round trip")
end
IO.puts("ok: Elixir graph save/text identity")
`, filepath.Join(framing, "node_chain_two.bin"), chainCanonical, filepath.Join(root, "testdata", "conformance", "tables", "framing", "scene_diamond_chain.json"), diamondCanonical)
	if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok: Elixir cycle") {
		t.Fatalf("native cycle probe: %v\n%s", err, out)
	} else {
		t.Log(strings.TrimSpace(out))
	}
}

const elixirCycleProbeHelpers = `
defmodule CycleProbe do
  def check(true, _), do: :ok
  def check(false, what), do: raise("FAIL: " <> what)

  # Both stores start occupied: refusal must clear them, not merely keep an
  # already-empty test buffer empty. Capacities still belong to the caller.
  def retain, do: %{records: [{[], 1, 4, <<7, 0, 0, 0>>}], used: 13,
                    cap: 1_048_576, ids: [1], id_cap: 131_072}
  def report, do: %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0,
                    malformed: false, retained: 0, retain_lost: 0}

  def damage(module, root, wire, label) do
    case module.load(root, wire) do
      {:error, :malformed, %{reason: "wire_damaged"}} -> :ok
      _ -> check(false, label <> ": plain load did not discard")
    end
    case module.load_retain(root, wire, retain(), report()) do
      {:error, :malformed, %{reason: "wire_damaged"}, %{records: [], used: 0, ids: [], cap: 1_048_576, id_cap: 131_072}} -> :ok
      _ -> check(false, label <> ": retaining load did not discard")
    end
  end

  def clean(module, root, wire, label) do
    plain = case module.load(root, wire) do
      {:ok, value, %{reason: ""}} -> value
      _ -> check(false, label <> ": plain load refused")
    end
    retained = case module.load_retain(root, wire, retain(), report()) do
      {:ok, value, %{reason: ""}, %{records: [], used: 0, ids: []}} -> value
      _ -> check(false, label <> ": retaining load refused or kept prior state")
    end
    [plain, retained]
  end

  # At each level L and R name the SAME two next-level nodes, so their node
  # numbers are equal (§3.1). Recurse down one branch only: the witness costs
  # O(levels), not O(paths).
  def diamond(value, levels) when levels > 0 do
    {root, _nodes} = value
    diamond(value, Graphdemo.Table.deref(value, root["tree"]), levels)
  end

  def diamond(value, node, levels) when levels > 0 do
    left = Graphdemo.Table.deref(value, node["left"])
    right = Graphdemo.Table.deref(value, node["right"])
    check(is_map(left) and is_map(right), "diamond: missing branch")

    if levels == 1 do
      check(left["left"] == nil and left["right"] == nil and
            right["left"] == nil and right["right"] == nil, "diamond: wrong leaf level")
    else
      check(left["left"] == right["left"], "diamond: left child was copied")
      check(left["right"] == right["right"], "diamond: right child was copied")
      diamond(value, left, levels - 1)
    end
  end
end
`

const elixirCycleSlotSchema = `package cycleslots
union Link
{
    one *Node
}
table Node
{
    on     bool
    left   *Node
    right  *Node
    items  []*Node
    lookup map[int32]*Node
    array  [2]*Node
    choice Link
    if on
    {
        side *Node
    }
}
`

// TestCycleSlotKindsElixir gives each stored-slot shape a clean twin and
// changes only its final referent from node 4 to node 2 to close a cycle.
// The false guard is deliberately decoded with its stored pointer intact.
func TestCycleSlotKindsElixir(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	for _, kind := range []string{"list", "map", "guard", "array", "union"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "CycleSlots.schema")
			schema := elixirCycleSlotSchema
			if kind != "union" {
				schema = strings.Replace(schema, "union Link\n{\n    one *Node\n}\n", "", 1)
				schema = strings.Replace(schema, "    choice Link\n", "", 1)
			}
			if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
				t.Fatal(err)
			}
			u := loadUnit(t, path)
			model := tabletext.NewModel(u)
			files := generateElixir(t, u)
			var probe strings.Builder
			probe.WriteString(elixirCycleProbeHelpers)
			nodes := make([]*tabletext.Instance, 4)
			for i := range nodes {
				nodes[i] = model.New(model.Lookup("Node"))
			}
			field := func(node int, key string) *tabletext.Field {
				f, ok := nodes[node].FieldByKey(key)
				if !ok {
					t.Fatalf("missing field %s", key)
				}
				return f
			}
			field(0, "left").Cell.Node = nodes[1]
			field(1, "left").Cell.Node = nodes[2]
			pattern := []byte{byte(ir.TableKindPointer), 4, 0}
			at := 1
			switch kind {
			case "list":
				f := field(2, "items")
				f.Elems, f.Count = []tabletext.Cell{{Node: nodes[3]}}, 1
				pattern, at = []byte{byte(ir.TableKindPointer), 1, 4}, 2
			case "map":
				f := field(2, "lookup")
				entry := model.NewMapEntry(f.Def)
				entry.Fields[0].Cell.I = 1
				entry.Fields[1].Cell.Node = nodes[3]
				f.Entries = []tabletext.Cell{{Tab: entry}}
			case "guard":
				field(2, "on").Cell.B = true
				field(2, "side").Cell.Node = nodes[3]
			case "array":
				field(2, "array").Elems[0].Node = nodes[3]
				pattern, at = []byte{byte(ir.TableKindPointer), 2, 4, 0}, 2
			case "union":
				f := field(2, "choice")
				arm := tabletext.UnionOf(f.Def).Variants[0]
				f.Cell.U = 1 // the first declared arm; zero is None
				f.Cell.Arm = model.NewArm(arm)
				f.Cell.Arm.Cell.Node = nodes[3]
				pattern, at = []byte{byte(ir.TableKindPointer), 1, 4}, 2
			}
			twin, err := tablewire.Encode(model, nodes[0])
			if err != nil {
				t.Fatalf("%s twin encode: %v", kind, err)
			}
			if kind == "guard" {
				// The two-byte bool prefix can also occur in framing or id hashes.
				// Select its one semantic occurrence: the third node's flag becomes
				// false while its stored pointer remains present, with no damage or
				// vocabulary change. This is a fixture check, not a decoder oracle
				// that can silently accept the wrong mutation.
				var guarded []byte
				matches := 0
				for i := 0; i+1 < len(twin); i++ {
					if twin[i] != byte(ir.TableKindBool) || twin[i+1] != 1 {
						continue
					}
					candidate := bytes.Clone(twin)
					candidate[i+1] = 0
					decoded := model.New(model.Lookup("Node"))
					var r tabletext.Report
					ok, err := tablewire.Decode(model, decoded, candidate, &r)
					if !ok || err != nil || r.Refused || r.Unknown != 0 {
						continue
					}
					first, _ := decoded.FieldByKey("left")
					if first.Cell.Node == nil {
						continue
					}
					second, _ := first.Cell.Node.FieldByKey("left")
					if second.Cell.Node == nil {
						continue
					}
					on, _ := second.Cell.Node.FieldByKey("on")
					side, _ := second.Cell.Node.FieldByKey("side")
					if !on.Cell.B && side.Cell.Node != nil {
						guarded, matches = candidate, matches+1
					}
				}
				if matches != 1 {
					t.Fatalf("guard twin has %d semantic flag mutations, want one", matches)
				}
				twin = guarded
			}
			back := model.New(model.Lookup("Node"))
			var report tabletext.Report
			if ok, err := tablewire.Decode(model, back, twin, &report); !ok || err != nil || report.Refused {
				t.Fatalf("%s clean twin is not valid: ok=%v err=%v report=%+v", kind, ok, err, report)
			}
			if bytes.Count(twin, pattern) != 1 {
				t.Fatalf("%s has no unique final pointer fixture %x in %x", kind, pattern, twin)
			}
			cycle := bytes.Clone(twin)
			cycle[bytes.Index(cycle, pattern)+at] = 2
			for _, sample := range []struct {
				name string
				data []byte
				bad  bool
			}{{kind + "-twin", twin, false}, {kind + "-cycle", cycle, true}} {
				file := filepath.Join(dir, sample.name+".bin")
				if err := os.WriteFile(file, sample.data, 0o600); err != nil {
					t.Fatal(err)
				}
				if sample.bad {
					fmt.Fprintf(&probe, "CycleProbe.damage(Cycleslots.Table, \"Node\", File.read!(%q), %q)\n", file, sample.name)
				} else {
					fmt.Fprintf(&probe, "for value <- CycleProbe.clean(Cycleslots.Table, \"Node\", File.read!(%q), %q), do: CycleProbe.check(is_integer(elem(value, 0)[\"left\"]), %q)\n", file, sample.name, sample.name+": left was lost")
				}
			}
			probe.WriteString("IO.puts(\"ok: Elixir cycle slot kinds\")\n")
			if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok: Elixir cycle slot kinds") {
				t.Fatalf("native slot probe: %v\n%s", err, out)
			}
		})
	}
}
