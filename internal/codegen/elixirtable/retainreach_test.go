package elixirtable

// THE RETAINING READ REFUSES A NODE RECORD NO POINTER REACHES
// (docs/SPEC-TABLES.md §3.1, §4). Reachability is judged over the wire's own
// pointer payloads, known and unknown fields alike, and every read holds it:
// plain, ReadStrict, retaining, region and builder alike. A refusal discards
// the whole load, so the retaining read returns no value and an empty store.
//
// The graph corpus' shared wire carries one node record its root names only
// through `tree`; nulling that pointer leaves the subtree in the directory
// with no pointer reaching it. The same wire with an unknown field naming the
// node is lawful again, because a node reached only through a skipped field
// still counts as reached. Each wire's verdict is the Go engine's before the
// generated Elixir is asked.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestElixirRetainingLoadRefusesUnreachedNode(t *testing.T) {
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

	clean, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// The root's `tree` field is {ref, kind pointer, index 3}; nulling the
	// index leaves node 3 and its subtree in the directory, unreached.
	if len(clean) < 16 || !bytes.Equal(clean[13:16], []byte{3, 17, 3}) {
		t.Fatal("graph_shared's root tree fixture moved")
	}
	unreached := bytes.Clone(clean)
	unreached[15] = 0
	unknown := movedToUnknownPointerField(t, paths, clean, "future_tree")

	model := tabletext.NewModel(u)
	// the unknown field is the wire's only path to the subtree: this reader
	// lands `tree` null and counts one field it does not declare
	{
		v := model.New(u.Tables["Scene"])
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, v, unknown, &report); !ok || err != nil || report.Unknown != 1 || instanceField(t, v, "tree").Cell.Node != nil {
			t.Fatalf("unknown-field-reached: ok=%v err=%v report=%+v, want the subtree reached only through one unknown field", ok, err, report)
		}
	}
	cases := []struct {
		name string
		wire []byte
		want bool
	}{
		{name: "unreached-node", wire: unreached, want: false},
		{name: "clean", wire: clean, want: true},
		{name: "unknown-field-reached", wire: unknown, want: true},
	}
	for _, tc := range cases {
		var plain tabletext.Report
		ok, err := tablewire.Decode(model, model.New(u.Tables["Scene"]), tc.wire, &plain)
		if ok != tc.want || (!tc.want && !tablewire.ReadRefusal(err)) {
			t.Fatalf("%s: engine plain verdict ok=%v err=%v report=%+v, want %v", tc.name, ok, err, plain, tc.want)
		}
		retain := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 16}
		var retained tabletext.Report
		ok, err = tablewire.DecodeRetain(model, model.New(u.Tables["Scene"]), tc.wire, retain, &retained)
		if ok != tc.want || (!tc.want && !tablewire.ReadRefusal(err)) {
			t.Fatalf("%s: engine retaining verdict ok=%v err=%v report=%+v, want %v", tc.name, ok, err, retained, tc.want)
		}
	}

	probe := fmt.Sprintf(`
defmodule ReachProbe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
occupied = %%{records: [{[], 1, 4, <<7, 0, 0, 0>>}], used: 13, cap: 1_048_576,
             ids: [1], id_cap: 131_072}
zero = %%{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
unreached = Base.decode16!(%q, case: :lower)
clean = Base.decode16!(%q, case: :lower)
unknown = Base.decode16!(%q, case: :lower)

case Graphdemo.Table.load("Scene", unreached) do
  {:error, _, _} -> :ok
  other -> ReachProbe.check(false, "unreached-node: plain load landed a damaged wire " <> inspect(other))
end
case Graphdemo.Table.load_retain("Scene", unreached, occupied, zero) do
  {:error, _, _, rt} ->
    ReachProbe.check(rt.records == [] and rt.used == 0 and rt.ids == [],
      "unreached-node: refusal kept retaining state " <> inspect(rt))
  other -> ReachProbe.check(false, "unreached-node: retaining load landed a damaged wire " <> inspect(other))
end

case Graphdemo.Table.load_retain("Scene", clean, occupied, zero) do
  {:ok, value, report, rt} ->
    case Graphdemo.Table.save_retain("Scene", value, rt, report) do
      {:ok, out, _, _} -> ReachProbe.check(out == clean, "clean: save_retain moved the wire's bytes")
      other -> ReachProbe.check(false, "clean: save_retain failed " <> inspect(other))
    end
  other -> ReachProbe.check(false, "clean: retaining load refused the lawful wire " <> inspect(other))
end

case Graphdemo.Table.load("Scene", unknown) do
  {:ok, _, _} -> :ok
  other -> ReachProbe.check(false, "unknown-field-reached: plain load refused the lawful wire " <> inspect(other))
end
case Graphdemo.Table.load_retain("Scene", unknown, occupied, zero) do
  {:ok, _, _, _} -> :ok
  other -> ReachProbe.check(false, "unknown-field-reached: retaining load refused the lawful wire " <> inspect(other))
end

IO.puts("ELIXIR_RETAIN_REACH_OK")
`,
		hex.EncodeToString(unreached),
		hex.EncodeToString(clean),
		hex.EncodeToString(unknown),
	)

	out, err := runArmProbe(t, elixirBin, generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "ELIXIR_RETAIN_REACH_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("the retaining read does not hold reachability: %v\n%s", err, out)
	}
}

// movedToUnknownPointerField is the clean wire with the root's `tree` subtree
// named instead by a pointer field `name` that the reader does not declare,
// and `tree` null, so the wire's only path to the subtree is a skipped pointer
// payload. The engine's writer spells it, over the same unit with `name`
// declared beside `tree`: the field rides in the root body before the node
// table, which is the root body's last field (docs/SPEC-TABLES.md §3.1), and
// the id table is in first-use order (§3).
func movedToUnknownPointerField(t *testing.T, paths []string, clean []byte, name string) []byte {
	t.Helper()
	dir := t.TempDir()
	var widened []string
	declared := false
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if filepath.Base(p) == "Graph.schema" {
			const tree = "    tree     *TreeNode\n"
			if !strings.Contains(text, tree) {
				t.Fatal("Graph.schema's Scene.tree declaration moved")
			}
			text = strings.Replace(text, tree, tree+"    "+name+" *TreeNode\n", 1)
			declared = true
		}
		out := filepath.Join(dir, filepath.Base(p))
		if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		widened = append(widened, out)
	}
	if !declared {
		t.Fatal("no Graph.schema among the pointer schemas")
	}
	u := loadUnit(t, widened...)
	model := tabletext.NewModel(u)
	v := model.New(u.Tables["Scene"])
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, v, clean, &report); !ok || err != nil {
		t.Fatalf("the widened unit reads graph_shared: ok=%v err=%v report=%+v", ok, err, report)
	}
	tree, moved := instanceField(t, v, "tree"), instanceField(t, v, name)
	if tree.Cell.Node == nil {
		t.Fatal("graph_shared's root names no tree")
	}
	moved.Cell.Node, tree.Cell.Node = tree.Cell.Node, nil
	wire, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// instanceField is the storage of the field declared as `name`.
func instanceField(t *testing.T, v *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range v.Fields {
		if v.Fields[i].Def.Name == name {
			return &v.Fields[i]
		}
	}
	t.Fatalf("%s declares no field %s", v.Def.Name, name)
	return nil
}
