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
// outside the root's reach is damage, but a pointer overwritten by null is
// not an edge. The forty-level diamond has 2^40 paths over 82 nodes; checking
// actual BEAM term sharing along one path avoids expanding those paths in
// the test itself. There is no wall-clock cutoff on the native probe.
func TestCyclicWireIsDamageElixir(t *testing.T) {
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
	// Root next=self followed by next=null. Also clear the original node's
	// self edge, so this fixture has only the overwritten root cycle.
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
	fmt.Fprintf(&probe, `
for value <- CycleProbe.clean(Graphdemo.Table, "ListNode", File.read!(%q), "overwritten_self_cycle") do
  CycleProbe.check(value["next"] == nil, "overwritten_self_cycle: final next is not null")
end
for value <- CycleProbe.clean(Graphdemo.Table, "ListNode", File.read!(%q), "node_chain_two") do
  CycleProbe.check(is_map(value["next"]), "node_chain_two: next was lost")
end
started = System.monotonic_time()
diamond_values = CycleProbe.clean(Graphdemo.Table, "Scene", File.read!(%q), "node_diamond_chain")
for value <- diamond_values, do: CycleProbe.diamond(value["tree"], 40)
elapsed = System.convert_time_unit(System.monotonic_time() - started, :native, :microsecond)
IO.puts("diamond: both loads preserve shared nodes across 2^40 paths in #{elapsed} us")
IO.puts("ok: Elixir cycle and final-slot graph contract")
`, overwrittenPath, filepath.Join(framing, "node_chain_two.bin"), filepath.Join(framing, "node_diamond_chain.bin"))
	fmt.Fprintf(&probe, `
# Public map equality stays deterministic across independent loads. Use the
# small chain here: comparing independent forty-level DAGs structurally would
# make the witness itself expand paths, contrary to the graph contract.
small_wire = File.read!(%q)
{:ok, first, first_report} = Graphdemo.Table.load("ListNode", small_wire)
{:ok, second, second_report} = Graphdemo.Table.load("ListNode", small_wire)
CycleProbe.check(not first_report.malformed and not second_report.malformed, "identity: chain refused")
CycleProbe.check(first == second, "identity: identical loads changed public value equality")
CycleProbe.check(not Map.has_key?(first, :__wire_identity__), "identity: root was tagged")
CycleProbe.check(first["next"][:__wire_identity__] == 2, "identity: first pointee has no deterministic index")
CycleProbe.check(first["next"]["next"][:__wire_identity__] == 3, "identity: second pointee has no deterministic index")
CycleProbe.check(Graphdemo.Table.save("ListNode", first) == {:ok, File.read!(%q)}, "identity: metadata changed canonical chain bytes")

# The same metadata bucket cannot make an edited copy alias its original.
node = first["next"]
edited = Map.put(node, "value", 777)
scene = %%{"head" => node, "alias" => edited}
{:ok, edited_wire} = Graphdemo.Table.save("Scene", scene)
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", edited_wire, "edited_copy") do
  CycleProbe.check(value["head"]["value"] == 2 and value["alias"]["value"] == 777,
    "edited_copy: same metadata aliased different content")
  CycleProbe.check(:erts_debug.same(value["head"]["next"], value["alias"]["next"]),
    "edited_copy: unchanged shared tail was duplicated")
end
{:ok, edited_text} = Graphdemo.Table.to_json("Scene", scene)
{:ok, parsed_edit, parsed_report} = Graphdemo.Table.from_json("Scene", edited_text)
CycleProbe.check(not parsed_report.malformed and parsed_edit["head"]["value"] == 2 and
  parsed_edit["alias"]["value"] == 777, "edited_copy: text lost an edited value")
CycleProbe.check(Graphdemo.Table.save("Scene", parsed_edit) == {:ok, edited_wire},
  "edited_copy: text round trip moved bytes")

# Different directory indices name DISTINCT loaded nodes, even when their
# contents agree. Their distinction survives save and text output (§3.1).
retagged = Map.put(node, :__wire_identity__, 999)
same = %%{"head" => node, "alias" => node}
different_tags = %%{"head" => node, "alias" => retagged}
{:ok, same_wire} = Graphdemo.Table.save("Scene", same)
{:ok, distinct_wire} = Graphdemo.Table.save("Scene", different_tags)
CycleProbe.check(distinct_wire != same_wire, "identity: different directory indices were collapsed")
CycleProbe.check(Graphdemo.Table.to_json("Scene", different_tags) != Graphdemo.Table.to_json("Scene", same),
  "identity: distinct loaded nodes lost their text distinction")
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", distinct_wire, "distinct_indices") do
  CycleProbe.check(not :erts_debug.same(value["head"], value["alias"]), "identity: distinct nodes were aliased")
  CycleProbe.check(value["head"]["value"] == 2 and value["alias"]["value"] == 2,
    "identity: preserving distinct nodes changed their content")
end

# Untagged authored maps keep the language's content-sharing behavior. The
# fixture copies come from separate small loads and have every private marker
# removed; they are physically different maps with the same authored value.
fresh_a = CycleProbe.untag_small(first["next"])
fresh_b = CycleProbe.untag_small(second["next"])
CycleProbe.check(not :erts_debug.same(fresh_a, fresh_b), "authored fixture did not create two maps")
authored = %%{"head" => fresh_a, "alias" => fresh_b}
CycleProbe.check(Graphdemo.Table.save("Scene", authored) == {:ok, same_wire}, "authored: equal untagged content stopped sharing")
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", same_wire, "equal_content") do
  CycleProbe.check(:erts_debug.same(value["head"], value["alias"]), "identity: same node was not shared")
end

# The large shared graph also reaches the save and text writers. Their result
# sizes are bounded by nodes, and the saved graph still has actual sharing.
# Never inspect or compare the complete DAG recursively in a failure message.
diamond = hd(diamond_values)
CycleProbe.check(not Map.has_key?(diamond, :__wire_identity__), "diamond: root was tagged")
{:ok, diamond_text} = Graphdemo.Table.to_json("Scene", diamond)
CycleProbe.check(byte_size(diamond_text) < 100_000 and String.contains?(diamond_text, "&node"),
  "diamond: text expanded paths rather than naming shared nodes")
CycleProbe.check(not String.contains?(diamond_text, "__wire_identity__"), "diamond: metadata escaped into text")
CycleProbe.check(diamond_text == File.read!(%q), "diamond: text disagrees with the canonical reference")
{:ok, diamond_saved} = Graphdemo.Table.save("Scene", diamond)
CycleProbe.check(byte_size(diamond_saved) < 100_000, "diamond: saved wire expanded paths")
CycleProbe.check(diamond_saved == File.read!(%q), "diamond: wire disagrees with the independent canonical writer")
for value <- CycleProbe.clean(Graphdemo.Table, "Scene", diamond_saved, "diamond_saved") do
  CycleProbe.diamond(value["tree"], 40)
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
      {:error, :malformed, %{malformed: true}} -> :ok
      _ -> check(false, label <> ": plain load did not discard")
    end
    case module.load_retain(root, wire, retain(), report()) do
      {:error, :malformed, %{malformed: true}, %{records: [], used: 0, ids: [], cap: 1_048_576, id_cap: 131_072}} -> :ok
      _ -> check(false, label <> ": retaining load did not discard")
    end
  end

  def clean(module, root, wire, label) do
    plain = case module.load(root, wire) do
      {:ok, value, %{malformed: false}} -> value
      _ -> check(false, label <> ": plain load refused")
    end
    retained = case module.load_retain(root, wire, retain(), report()) do
      {:ok, value, %{malformed: false}, %{records: [], used: 0, ids: []}} -> value
      _ -> check(false, label <> ": retaining load refused or kept prior state")
    end
    [plain, retained]
  end

  # This helper is used only on the tiny authored-value fixture, never the
  # large shared DAG. It deliberately returns ordinary untagged maps.
  def untag_small(value) when is_map(value) do
    value |> Map.delete(:__wire_identity__) |> Map.new(fn {key, child} -> {key, untag_small(child)} end)
  end
  def untag_small(value), do: value

  # At each level L and R name the SAME two next-level nodes. Compare BEAM
  # identity, not structural equality (which itself expands a diamond).
  # Recurse down one branch only: the witness costs O(levels), not O(paths).
  def diamond(node, levels) when levels > 0 do
    left = node["left"]
    right = node["right"]
    check(is_map(left) and is_map(right), "diamond: missing branch")
    if levels == 1 do
      check(left["left"] == nil and left["right"] == nil and
            right["left"] == nil and right["right"] == nil, "diamond: wrong leaf level")
    else
      check(:erts_debug.same(left["left"], right["left"]), "diamond: left child was copied")
      check(:erts_debug.same(left["right"], right["right"]), "diamond: right child was copied")
      diamond(left, levels - 1)
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
					fmt.Fprintf(&probe, "for value <- CycleProbe.clean(Cycleslots.Table, \"Node\", File.read!(%q), %q), do: CycleProbe.check(is_map(value[\"left\"]), %q)\n", file, sample.name, sample.name+": left was lost")
				}
			}
			probe.WriteString("IO.puts(\"ok: Elixir cycle slot kinds\")\n")
			if out, err := runArmProbe(t, elixirBin, files, probe.String()); err != nil || !strings.Contains(out, "ok: Elixir cycle slot kinds") {
				t.Fatalf("native slot probe: %v\n%s", err, out)
			}
		})
	}
}
