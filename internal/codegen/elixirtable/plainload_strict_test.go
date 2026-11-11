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
// The Go engine is the oracle on the same bytes before the generated Elixir is
// asked, and every unmodified wire still loads and re-saves byte for byte.

import (
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
      check.(want, label <> ": " <> name <> " refused a lawful wire")
      if round_trip do
        case save.(v) do
          {:ok, out} -> check.(out == wire, label <> ": " <> name <> " save does not reproduce the wire")
          other -> check.(false, label <> ": " <> name <> " save failed " <> inspect(other))
        end
      end
    {:error, reason, report} when is_atom(reason) ->
      check.(not want, label <> ": " <> name <> " landed a damaged wire")
      check.(report.reason != "", label <> ": " <> name <> " refusal carries no reason")
    other -> check.(false, label <> ": " <> name <> " answered " <> inspect(other))
  end
end
`, hex.EncodeToString(c.wire), c.label, c.want, c.roundTrip, root, root, root, root)
		if c.retain {
			b.WriteString(`
case T.load_retain(` + fmt.Sprintf("%q", root) + `, wire, store, zero) do
  {:error, reason, report, retain} when is_atom(reason) ->
    check.(not want, label <> ": retaining load landed a damaged wire")
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
}
