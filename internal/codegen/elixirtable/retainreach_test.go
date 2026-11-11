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
	"encoding/binary"
	"encoding/hex"
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
	unknown := appendRootPointerField(t, unreached, ir.TableWireId("future_tree"), 3)

	model := tabletext.NewModel(u)
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

// appendRootPointerField adds one field to the root body, naming an existing
// node record by an id the reader does not declare. The field rides after the
// directory and before the body's terminator, so the wire's only path to the
// node is a skipped pointer payload.
func appendRootPointerField(t *testing.T, wire []byte, id, index uint64) []byte {
	t.Helper()
	if len(wire) < 8 {
		t.Fatal("the wire carries no id count")
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	idsStart := len(wire) - 8 - int(count)*8
	if idsStart < 2 || wire[idsStart-1] != 0 {
		t.Fatal("the wire's root body does not end in its terminator")
	}
	for i := 0; i < int(count); i++ {
		if binary.LittleEndian.Uint64(wire[idsStart+i*8:]) == id {
			t.Fatal("the unknown field's id is already in the wire's id table")
		}
	}
	field := binary.AppendUvarint(nil, count+1)
	field = append(field, byte(ir.TableKindPointer))
	field = binary.AppendUvarint(field, index)
	out := make([]byte, 0, len(wire)+len(field)+8)
	out = append(out, wire[:idsStart-1]...)
	out = append(out, field...)
	out = append(out, 0)
	out = append(out, wire[idsStart:len(wire)-8]...)
	out = binary.LittleEndian.AppendUint64(out, id)
	return binary.LittleEndian.AppendUint64(out, count+1)
}
