package elixirtable

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

// TestElixirKeyedBodyEndsAtItsLastSlot holds an enum-keyed array's body to its
// last counted pair (docs/SPEC-TABLES.md §3, §4): bytes left inside its L are
// damage, so the whole load refuses. The rows are the committed map_slots wire
// with each keyed pair count lowered by one and to zero; the engine is the
// oracle, and the unmodified wire reads and re-saves through all three reads.
func TestElixirKeyedBodyEndsAtItsLastSlot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "maps", "*.schema"))
	if err != nil {
		t.Fatalf("gather tables/maps: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("tables/maps declares no schema files")
	}
	u := loadUnit(t, paths...)
	m := tabletext.NewModel(u)
	slots := m.Lookup("Slots")
	if slots == nil {
		t.Fatal("tables/maps declares no Slots table")
	}

	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "map_slots.bin"))
	if err != nil {
		t.Fatal(err)
	}

	offsets := keyedPairCountOffsets(t, wire)
	lowered := func(to byte) []byte {
		out := append([]byte(nil), wire...)
		for _, at := range offsets {
			out[at] = to
		}
		return out
	}

	rows := []struct {
		name string
		wire []byte
		want bool
	}{
		{"unmodified", wire, true},
		{"one-pair-short", lowered(1), false},
		{"no-pairs", lowered(0), false},
	}
	for _, row := range rows {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(slots), row.wire, &rep)
		if ok != row.want {
			t.Fatalf("%s: oracle verdict ok=%v err=%v rep=%+v, want %v", row.name, ok, err, rep, row.want)
		}
		if !row.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: oracle refusal is not a ReadRefusal: %v", row.name, err)
		}
	}

	mod := ir.GoExportName(u.Package) + ".Table"
	var probe strings.Builder
	probe.WriteString(`
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
`)
	for _, row := range rows {
		fmt.Fprintf(&probe, `
wire = Base.decode16!(%q, case: :lower)
want = %t
label = %q
store = %%{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
case %s.load("Slots", wire) do
  {:ok, value, report} ->
    check.(want, label <> ": load refused a lawful wire " <> inspect(report))
    if want do
      check.(report.reason == "", label <> ": clean load carries a reason")
      case %s.save("Slots", value) do
        {:ok, out} -> check.(out == wire, label <> ": save does not reproduce the wire")
        other -> check.(false, label <> ": save failed " <> inspect(other))
      end
    end
  {:error, _reason, _report} ->
    check.(not want, label <> ": load landed a damaged wire")
  other -> check.(false, label <> ": load answered " <> inspect(other))
end
case %s.variable_load("Slots", wire) do
  {:ok, _value, _report} -> check.(want, label <> ": variable_load refused a lawful wire")
  {:error, _reason, _report} -> check.(not want, label <> ": variable_load landed a damaged wire")
  other -> check.(false, label <> ": variable_load answered " <> inspect(other))
end
case %s.load_retain("Slots", wire, store, zero) do
  {:ok, value, report, retain} ->
    check.(want, label <> ": load_retain refused a lawful wire")
    if want do
      case %s.save_retain("Slots", value, retain, report) do
        {:ok, out, _report, _retain} -> check.(out == wire, label <> ": retaining save does not reproduce the wire")
        other -> check.(false, label <> ": retaining save failed " <> inspect(other))
      end
    end
  {:error, _reason, _report, retain} ->
    check.(not want, label <> ": load_retain landed a damaged wire")
    check.(retain.records == [], label <> ": refusal left the retaining store populated")
  other -> check.(false, label <> ": load_retain answered " <> inspect(other))
end
`, hex.EncodeToString(row.wire), row.want, row.name, mod, mod, mod, mod, mod)
	}
	probe.WriteString(`
if Process.get(:failed), do: System.halt(1)
IO.puts("ELIXIR_KEYED_EXACT_OK")
`)

	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBin, files, probe.String())
	if err != nil || !strings.Contains(out, "ELIXIR_KEYED_EXACT_OK") {
		t.Fatalf("the keyed body does not end at its last slot: %v\n%s", err, out)
	}
}

// keyedPairCountOffsets finds the pair-count byte of every keyed body in the
// committed map_slots wire: the keyed kind, its L, then the i32 element kind
// and the count. The corpus holds two such bodies, each with two pairs.
func keyedPairCountOffsets(t *testing.T, wire []byte) []int {
	t.Helper()
	var offsets []int
	for i := 0; i+3 < len(wire); i++ {
		if wire[i] != byte(ir.TableKindKeyed) {
			continue
		}
		length, w := binary.Uvarint(wire[i+1:])
		if w <= 0 {
			continue
		}
		body := i + 1 + w
		if body+2 > len(wire) || int(wire[body]) != ir.TableKindI32 {
			continue
		}
		count, cw := binary.Uvarint(wire[body+1:])
		if cw != 1 || count != 2 || uint64(body)+length > uint64(len(wire)) {
			continue
		}
		offsets = append(offsets, body+1)
	}
	if len(offsets) != 2 {
		t.Fatalf("map_slots carries %d keyed pair counts, want 2", len(offsets))
	}
	return offsets
}
