package elixirtable

// AN ENUM-KEYED ARRAY NAMES EACH SLOT AT MOST ONCE (docs/SPEC-TABLES.md §3.2,
// §4). A second pair whose key resolves to a slot the body already placed is
// damage: the whole load refuses, on the plain read, the strict framing read
// and the retaining read alike. Two keys that resolve to one variant are the
// same slot however they reached it.
//
// The rows are built from the committed map_slots wire: the lawful wire reads
// whole and re-saves byte for byte, and the one-byte variant whose second
// keyed pair names the first pair's slot is refused by the Go oracle and by
// every generated Elixir read.

import (
	"bytes"
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

// keyedSlotRepeatedWire changes the committed wire at one byte: inside the
// first enum-keyed body, the second pair's key reference is changed to name
// the slot the first pair already placed.
func keyedSlotRepeatedWire(t *testing.T, wire []byte) []byte {
	t.Helper()
	// The first two-slot int32 keyed body: element kind, count, first key
	// reference, first length, then the first value and the second key.
	marker := []byte{byte(ir.TableKindI32), 2, 4, 4}
	at := bytes.Index(wire, marker)
	if at < 0 {
		t.Fatalf("map_slots.bin carries no two-slot int32 enum-keyed body: the fixture, not the rule, changed")
	}
	second := at + len(marker) + 4
	if second >= len(wire) || wire[second] != 5 {
		t.Fatalf("map_slots.bin's second keyed pair does not carry the expected key reference: the fixture, not the rule, changed")
	}
	out := append([]byte(nil), wire...)
	out[second] = 4
	return out
}

func TestElixirKeyedSlotAppearsOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "maps", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("tables/maps: %v", err)
	}
	u := loadUnit(t, paths...)
	model := tabletext.NewModel(u)

	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "map_slots.bin"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		name string
		wire []byte
		want bool
	}{
		{"lawful-map-slots", wire, true},
		{"second-pair-names-the-first-slot", keyedSlotRepeatedWire(t, wire), false},
	}
	for _, row := range rows {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(u.Tables["Slots"]), row.wire, &report)
		if ok != row.want {
			t.Fatalf("%s: the oracle answered ok=%v err=%v report=%+v, want %v", row.name, ok, err, report, row.want)
		}
		if !row.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: the oracle refusal is not a read refusal: %v", row.name, err)
		}
	}

	mod := ir.GoExportName(u.Package) + ".Table"
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
  def check(true, _label), do: :ok

  def check(false, label) do
    IO.puts(:stderr, "FAIL: " <> label)
    System.halt(1)
  end
end

store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
`)
	for _, row := range rows {
		fmt.Fprintf(&probe, `
wire = Base.decode16!(%q, case: :lower)
label = %q
want = %t

case %s.load("Slots", wire) do
  {:ok, value, report} ->
    Probe.check(want, label <> ": load/2 accepted a damaged wire")
    if want do
      Probe.check(report.reason == "", label <> ": load/2 carries a reason on a lawful wire")
      case %s.save("Slots", value) do
        {:ok, out} -> Probe.check(out == wire, label <> ": load/2 does not re-save byte for byte")
        other -> Probe.check(false, label <> ": load/2 save answered " <> inspect(other))
      end
    end
  {:error, _reason, _report} ->
    Probe.check(not want, label <> ": load/2 refused a lawful wire")
  other ->
    Probe.check(false, label <> ": load/2 answered " <> inspect(other))
end

case %s.variable_load("Slots", wire) do
  {:ok, value, report} ->
    Probe.check(want, label <> ": variable_load/2 accepted a damaged wire")
    if want do
      Probe.check(report.reason == "", label <> ": variable_load/2 carries a reason on a lawful wire")
      case %s.save("Slots", value) do
        {:ok, out} -> Probe.check(out == wire, label <> ": variable_load/2 does not re-save byte for byte")
        other -> Probe.check(false, label <> ": variable_load/2 save answered " <> inspect(other))
      end
    end
  {:error, _reason, _report} ->
    Probe.check(not want, label <> ": variable_load/2 refused a lawful wire")
  other ->
    Probe.check(false, label <> ": variable_load/2 answered " <> inspect(other))
end

case %s.load_retain("Slots", wire, store, zero) do
  {:ok, value, report, retained} ->
    Probe.check(want, label <> ": load_retain/4 accepted a damaged wire")
    if want do
      Probe.check(report.reason == "", label <> ": load_retain/4 carries a reason on a lawful wire")
      case %s.save_retain("Slots", value, retained, report) do
        {:ok, out, _report, _retained} -> Probe.check(out == wire, label <> ": load_retain/4 does not re-save byte for byte")
        other -> Probe.check(false, label <> ": load_retain/4 save answered " <> inspect(other))
      end
    end
  {:error, _reason, _report, _retained} ->
    Probe.check(not want, label <> ": load_retain/4 refused a lawful wire")
  other ->
    Probe.check(false, label <> ": load_retain/4 answered " <> inspect(other))
end
`, hex.EncodeToString(row.wire), row.name, row.want, mod, mod, mod, mod, mod, mod)
	}
	probe.WriteString(`IO.puts("KEYED_SLOT_ONCE_OK")`)

	out, err := runArmProbe(t, elixirBin, generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "KEYED_SLOT_ONCE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("keyed slot once: %v\n%s", err, out)
	}
}
