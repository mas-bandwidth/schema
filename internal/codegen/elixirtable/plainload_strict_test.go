package elixirtable

// TestElixirPlainLoadHoldsStrictFraming holds the one strict variable-form read
// on every entry point (docs/SPEC-TABLES.md §3, §4): load/2 and load_retain/4
// refuse every framing fault variable_load/2 refuses. Four damages are built
// from committed wires:
//
//  1. an array whose element kind names a narrower kind, leaving bytes inside
//     its L (list_tables.bin with the scores element kind respelled);
//  2. an unknown field whose payload does not walk (frame_unknown_id_length);
//  3. a second field that resolves to a name already placed
//     (frame_repeat_last_wins);
//  4. an array body under two bytes (array_L0_inert).
//
// And the id table's first-use order (docs/SPEC-TABLES.md §3): an
// engine-written wire with an unused entry after the last, an unused entry in
// the middle, and two used entries swapped, each refused by load/2,
// variable_load/2 and load_retain/4, the engine refusing the same bytes for
// that reason first (firstUseBreaks).
//
// The Go engine is the oracle on the same bytes before the generated Elixir is
// asked, and every unmodified wire still loads and re-saves byte for byte.

import (
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

type plainStrictCase struct {
	label string
	wire  []byte
	want  bool
	// roundTrip asks the save to reproduce the wire byte for byte.
	roundTrip bool
	// retain asks load_retain/4 for the same verdict.
	retain bool
}

func plainStrictProbe(module, root string, cases []plainStrictCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "alias %s, as: T\n", module)
	b.WriteString(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
`)
	hasRetain := false
	for _, c := range cases {
		hasRetain = hasRetain || c.retain
	}
	if hasRetain {
		b.WriteString(`
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
`)
	}
	for _, c := range cases {
		fmt.Fprintf(&b, `
wire = Base.decode16!(%q, case: :lower)
label = %q
want = %t
round_trip = %t
for {name, result, save} <- [
  {"load", T.load(%q, wire), fn v -> T.save(%q, v) end},
  {"variable_load", T.variable_load(%q, wire), fn v -> T.save(%q, v) end}
] do
  case result do
    {:ok, v, _report} ->
      check.(want, label <> ": " <> name <> " landed a damaged wire")
      if round_trip do
        case save.(v) do
          {:ok, out} -> check.(out == wire, label <> ": " <> name <> " save does not reproduce the wire")
          other -> check.(false, label <> ": " <> name <> " save failed " <> inspect(other))
        end
      end
    {:error, reason, report} when is_atom(reason) ->
      check.(not want, label <> ": " <> name <> " refused a lawful wire")
      check.(report.reason != "", label <> ": " <> name <> " refusal carries no reason")
    other -> check.(false, label <> ": " <> name <> " answered " <> inspect(other))
  end
end
`, hex.EncodeToString(c.wire), c.label, c.want, c.roundTrip, root, root, root, root)
		if c.retain {
			b.WriteString(`
case T.load_retain(` + fmt.Sprintf("%q", root) + `, wire, store, zero) do
  {:ok, _v, _report, _retain} ->
    check.(want, label <> ": retaining load landed a damaged wire")
  {:error, reason, report, retain} when is_atom(reason) ->
    check.(not want, label <> ": retaining load refused a lawful wire")
    check.(report.reason != "", label <> ": retaining refusal carries no reason")
    check.(retain.records == [], label <> ": refusal left the retaining store populated")
  other -> check.(false, label <> ": retaining load answered " <> inspect(other))
end
`)
		}
	}
	b.WriteString(`
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_PLAIN_LOAD_STRICT_OK")
`)
	return b.String()
}

func plainStrictOracle(t *testing.T, m *tabletext.Model, root string, c plainStrictCase) {
	t.Helper()
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), c.wire, &report)
	if ok != c.want {
		t.Fatalf("%s oracle: ok=%v err=%v report=%+v, want %v", c.label, ok, err, report, c.want)
	}
	if !c.want && !tablewire.ReadRefusal(err) {
		t.Fatalf("%s oracle refusal is not a ReadRefusal: %v", c.label, err)
	}
}

func readPlainStrictWire(t *testing.T, root, name string) []byte {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", name))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestElixirPlainLoadHoldsStrictFraming(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	listUnit := loadUnit(t, filepath.Join(root, "tables", "lists", "Save.schema"))
	listModel := tabletext.NewModel(listUnit)
	listWire := readPlainStrictWire(t, root, "list_tables.bin")

	narrowed := append([]byte(nil), listWire...)
	if narrowed[71] != byte(ir.TableKindI32) {
		t.Fatalf("list_tables.bin byte 71 is kind %d, not the scores int32 kind", narrowed[71])
	}
	narrowed[71] = byte(ir.TableKindI8)

	listCases := []plainStrictCase{
		{label: "list_tables", wire: listWire, want: true, roundTrip: true},
		{label: "list_tables_narrowed_element", wire: narrowed, want: false, retain: true},
	}
	for _, c := range listCases {
		plainStrictOracle(t, listModel, "Save", c)
	}
	listOut, err := runArmProbe(t, elixirBinary(t), generateElixir(t, listUnit), plainStrictProbe("Listdemo.Table", "Save", listCases))
	if err != nil || !strings.Contains(listOut, "ELIXIR_PLAIN_LOAD_STRICT_OK") || strings.Contains(listOut, "warning:") {
		t.Fatalf("the plain list read does not hold the strict framing: %v\n%s", err, listOut)
	}

	src, err := os.ReadFile(filepath.Join(root, "test", "tables", "R1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cfgUnit := unitFrom(t, strings.ReplaceAll(string(src), "fixed table Cfg", "table Cfg"))
	cfgModel := tabletext.NewModel(cfgUnit)

	cfgCases := []plainStrictCase{
		{label: "r1_cfg", wire: readPlainStrictWire(t, root, "r1_cfg.bin"), want: true, roundTrip: true},
		{label: "unknown_id_length", wire: readPlainStrictWire(t, root, "framing/frame_unknown_id_length.bin"), want: false},
		{label: "repeat_last_wins", wire: readPlainStrictWire(t, root, "framing/frame_repeat_last_wins.bin"), want: false},
		{label: "array_L0_inert", wire: readPlainStrictWire(t, root, "framing/array_L0_inert.bin"), want: false},
	}
	for _, c := range cfgCases {
		plainStrictOracle(t, cfgModel, "Cfg", c)
	}
	cfgOut, err := runArmProbe(t, elixirBinary(t), generateElixir(t, cfgUnit), plainStrictProbe("Tblr1.Table", "Cfg", cfgCases))
	if err != nil || !strings.Contains(cfgOut, "ELIXIR_PLAIN_LOAD_STRICT_OK") || strings.Contains(cfgOut, "warning:") {
		t.Fatalf("the plain framing read does not hold the strict framing: %v\n%s", err, cfgOut)
	}

	// THE ID TABLE IS IN FIRST-USE ORDER (docs/SPEC-TABLES.md §3): a lawful
	// wire the engine writes, then broken one way each, every break refused by
	// the plain and the retaining load alike, and by the engine first.
	orderUnit := unitFrom(t, "package idorder\n\ntable Order\n{\n    a int32\n    b int32\n    c int32\n}\n")
	orderModel := tabletext.NewModel(orderUnit)
	inst := orderModel.New(orderModel.Lookup("Order"))
	var text tabletext.Report
	if !orderModel.Read(inst, []byte(`{"a": 1, "b": 2, "c": 3}`), &text) {
		t.Fatalf("the lawful order value does not read: %+v", text)
	}
	lawful, err := tablewire.Encode(orderModel, inst)
	if err != nil {
		t.Fatal(err)
	}
	trailing, middle, swapped := firstUseBreaks(t, lawful)
	orderCases := []plainStrictCase{
		{label: "first_use_lawful", wire: lawful, want: true, roundTrip: true, retain: true},
		{label: "first_use_unused_trailing", wire: trailing, want: false, retain: true},
		{label: "first_use_unused_middle", wire: middle, want: false, retain: true},
		{label: "first_use_swapped", wire: swapped, want: false, retain: true},
	}
	for _, c := range orderCases {
		plainStrictOracle(t, orderModel, "Order", c)
		var report tabletext.Report
		ok, err := tablewire.DecodeRetain(orderModel, orderModel.New(orderModel.Lookup("Order")), c.wire, &tablewire.Retain{Capacity: 4096, IdCapacity: 256}, &report)
		if ok != c.want {
			t.Fatalf("%s retaining oracle: ok=%v err=%v, want %v", c.label, ok, err, c.want)
		}
		if !c.want && (tablewire.RefusalReason(err) != "wire_damaged" || !strings.Contains(err.Error(), "first-use order")) {
			t.Fatalf("%s: the engine refuses for another reason: %v", c.label, err)
		}
	}
	orderOut, err := runArmProbe(t, elixirBinary(t), generateElixir(t, orderUnit), plainStrictProbe("Idorder.Table", "Order", orderCases))
	if err != nil || !strings.Contains(orderOut, "ELIXIR_PLAIN_LOAD_STRICT_OK") || strings.Contains(orderOut, "warning:") {
		t.Fatalf("the variable read does not hold the id table to first-use order: %v\n%s", err, orderOut)
	}
}

// firstUseBreaks takes an engine-written wire of three int32 fields (each a
// one-byte reference, kind 4 and four bytes, references 1, 2, 3 in first-use
// order) and breaks its id table three ways, each a table no conforming
// writer produces: an unused entry after the last, an unused entry between
// two used ones (the later references renumbered past it), and the first two
// entries swapped (their references swapped with them, so every field keeps
// its identity and payload and only the table's order differs).
func firstUseBreaks(t *testing.T, lawful []byte) (trailing, middle, swapped []byte) {
	t.Helper()
	n := int(binary.LittleEndian.Uint64(lawful[len(lawful)-8:]))
	start := len(lawful) - 8 - 8*n
	body := lawful[1:start]
	var ids []uint64
	for i := range n {
		ids = append(ids, binary.LittleEndian.Uint64(lawful[start+8*i:]))
	}
	if n != 3 || len(body) != 19 || body[18] != 0 {
		t.Fatalf("the lawful wire is not three fields and a terminator: % x", lawful)
	}
	for i := range 3 {
		if body[6*i] != byte(i+1) || body[6*i+1] != byte(ir.TableKindI32) {
			t.Fatalf("field %d is not reference %d at kind int32: % x", i, i+1, body)
		}
	}
	unused := ir.TableWireId("first_use_unused_entry")
	for unused == ids[0] || unused == ids[1] || unused == ids[2] {
		unused++
	}
	wire := func(body []byte, ids ...uint64) []byte {
		w := append([]byte{ir.TableWireForm}, body...)
		for _, id := range ids {
			w = binary.LittleEndian.AppendUint64(w, id)
		}
		return binary.LittleEndian.AppendUint64(w, uint64(len(ids)))
	}
	trailing = wire(body, ids[0], ids[1], ids[2], unused)
	shifted := append([]byte(nil), body...)
	shifted[6], shifted[12] = 3, 4
	middle = wire(shifted, ids[0], unused, ids[1], ids[2])
	crossed := append([]byte(nil), body...)
	crossed[0], crossed[6] = 2, 1
	swapped = wire(crossed, ids[1], ids[0], ids[2])
	return trailing, middle, swapped
}
