package elixirtable

// The strict variable-table read (docs/plans/strict-only.md; SPEC-TABLES
// §4, §6.5): one read returns the whole value or refuses the whole load.
// Damage inside a nested payload, a kind mismatch, an out-of-range value
// and a truncated wire each refuse with {:error, reason, report} and land
// no value, while lawful evolution — an unknown field and a widened kind —
// still reads whole and counts its event. The negative control plants the
// retired clamp back into the real generated artifact and requires this
// probe to go red.

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const slowVTReaderSchema = `package slowvt
table Child {
 flag bool
 number int32
}
table Root {
 n int32 = 0 | min = 0, max = 10
 child Child
}
`

const slowVTWriterSchema = `package slowvt
table Child {
 flag bool
 number int32
}
table Root {
 n int32 = 0
 child Child
 extra int32 = 0
}
`

func slowVTWire(t *testing.T, src, text string) []byte {
	t.Helper()
	u := unitFrom(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("the text did not place: %+v", r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func slowVTProbe(clean, over, unknown, trunc string) string {
	return `
clean = Base.decode16!("` + clean + `", case: :lower)
over = Base.decode16!("` + over + `", case: :lower)
unknown = Base.decode16!("` + unknown + `", case: :lower)
trunc = Base.decode16!("` + trunc + `", case: :lower)
store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
case Slowvt.Table.load("Root", clean) do
  {:ok, v, r} -> check.(v["n"] == 5 and v["child"]["number"] == 7 and r.unknown == 0 and r.widened == 0, "clean wire did not read whole: " <> inspect({v, r}))
  other -> check.(false, "clean wire refused: " <> inspect(other))
end
for {label, wire} <- [{"over", over}, {"trunc", trunc}] do
  case Slowvt.Table.load("Root", wire) do
    {:error, reason, report} when is_atom(reason) -> check.(report.reason != "", label <> " refusal carries no reason")
    other -> check.(false, label <> " landed a partial value: " <> inspect(other))
  end
  case Slowvt.Table.load_retain("Root", wire, store, zero) do
    {:error, reason, _report, %{records: []}} when is_atom(reason) -> :ok
    other -> check.(false, label <> " retaining landed a partial value: " <> inspect(other))
  end
end
case Slowvt.Table.load("Root", unknown) do
  {:ok, _v, %{unknown: 1}} -> :ok
  other -> check.(false, "unknown field is evolution and must read: " <> inspect(other))
end
if Process.get(:failed, false), do: System.halt(1)
IO.puts("SLOW_VT_STRICT_OK")
`
}

func slowVTWires(t *testing.T) (clean, over, unknown, trunc []byte) {
	t.Helper()
	clean = slowVTWire(t, slowVTReaderSchema, `{"n":5,"child":{"flag":true,"number":7}}`)
	over = slowVTWire(t, slowVTWriterSchema, `{"n":99,"child":{"flag":true,"number":7}}`)
	unknown = slowVTWire(t, slowVTWriterSchema, `{"n":5,"child":{"flag":true,"number":7},"extra":3}`)
	// Truncate inside the nested Child payload: the framing stays valid at
	// the root, so only the strict walk can refuse it (the
	// TestTruncatedFieldPayloadElixir cell).
	u := unitFrom(t, slowVTReaderSchema)
	m := tabletext.NewModel(u)
	inner := append([]byte{2, 10}, make([]byte, 2)...)
	payload := append([]byte{13}, inner...)
	wire := binary.AppendUvarint([]byte{1, 1, 13}, uint64(len(payload)))
	wire = append(wire, payload...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("child"))
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(u.Tables["Root"]), wire, &report)
	if ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("truncated nested fixture: ok=%v err=%v report=%+v", ok, err, report)
	}
	return clean, over, unknown, wire
}

func TestElixirVariableSlowTestsStrict(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	clean, over, unknown, trunc := slowVTWires(t)
	probe := slowVTProbe(hex.EncodeToString(clean), hex.EncodeToString(over), hex.EncodeToString(unknown), hex.EncodeToString(trunc))
	u := unitFrom(t, slowVTReaderSchema)
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "SLOW_VT_STRICT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("strict variable read: %v\n%s", err, out)
	}
}

// TestElixirVariableSlowTestsStrictNegativeControl plants the retired
// clamp into the real generated artifact — clamp_int returns the bound
// instead of throwing — and requires the strict probe to go red on the
// out-of-range row. The fault sits in the generated reader the probe
// actually runs, not in a mock or in the test's expectations.
func TestElixirVariableSlowTestsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	clean, over, unknown, trunc := slowVTWires(t)
	probe := slowVTProbe(hex.EncodeToString(clean), hex.EncodeToString(over), hex.EncodeToString(unknown), hex.EncodeToString(trunc))
	u := unitFrom(t, slowVTReaderSchema)
	files := generateElixir(t, u)
	elixirPatch(t, files, "do: {hi, throw(:wire_damaged)}", "do: {hi, _report}")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("planted clamp stayed green: the strict probe does not see the rule\n%s", out)
	}
	if !strings.Contains(out, "FAIL:") {
		t.Fatalf("planted clamp failed for a reason other than the strict probe's own checks: %v\n%s", err, out)
	}
}
