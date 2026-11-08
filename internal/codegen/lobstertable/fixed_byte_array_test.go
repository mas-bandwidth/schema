package lobstertable

import (
	"encoding/binary"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

// The representative benchmark's TableMixed.loadout is [4]uint8. Lobster
// stores these arrays as byte strings; native fixed and Record callers must
// preserve the same values without trying to assign an immutable string index.
func TestLobsterFixedByteArrays(t *testing.T) {
	t.Parallel()
	u := lobsterUnitOf(t, `package bytearray
enum Key { First, Second }
type Leaf { loadout [4]uint8 }
union Choice { values [4]uint8 }
fixed table Row {
    loadout [4]uint8
    live [1..4]uint8
    keyed [Key]uint8
    nested Leaf
    optional ?Leaf
    choice Choice
    ranged [2]uint8 | min = 0, max = 200
}
`)
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	const program = `import .ProbeTable
import .schema_save
import .json
let bytes = write_int8_le(write_int8_le(write_int8_le(schema_rt.zeros(4), 1, 127), 2, 128), 3, 255)
let row = bytearray.NewRow()
row.Loadout = bytes
row.Live = bytes
row.LiveCount = 2
row.Keyed = substring(bytes, 2, 2)
row.Nested.Loadout = bytes
row.OptionalPresent = true
row.Optional.Loadout = bytes
row.Choice.Type = bytearray.ChoiceTypeValues
row.Choice.Values = bytes
row.Ranged = write_int8_le(write_int8_le(schema_rt.zeros(2), 0, 1), 1, 200)
let n, wire, why = bytearray.RowFixedSaveChecked([row], schema_rt.zeros(bytearray.RowFixedMeasure(1)))
assert n > 0 and why == ""
let rows = [bytearray.NewRow()]
let plan:[schema_rt.TableFixedEntry] = []
for(bytearray.RowFixedPlanMax): push(plan, schema_rt.TableFixedEntry {})
let count = bytearray.RowFixedLoad(rows, wire, plan, schema_rt.TableReport {})
assert count == 1
let back = rows[0]
print("typed " + string(back.Loadout == bytes and back.Live == substring(bytes, 0, 2) + schema_rt.zeros(2) and back.LiveCount == 2 and back.Keyed == substring(bytes, 2, 2) and back.Nested.Loadout == bytes and back.OptionalPresent and back.Optional.Loadout == bytes and back.Choice.Values == bytes and back.Ranged == row.Ranged))
print("trusted " + string(bytearray.RowFixedDecode(wire)[0].Loadout == bytes))
let rec = schema_rt.load("Row", wire, schema_rt.LoadReport {})
assert rec
let values = schema_rt.slot_named(rec, "loadout").elems
let saved, saved_why = schema_rt.save_reasoned(rec)
print("record " + string(values[0].i == 0 and values[1].i == 127 and values[2].i == 128 and values[3].i == 255 and saved_why == "" and saved == wire))
let text, text_why = schema_rt.json_write(rec)
let json = schema_rt.json_read("Row", text, schema_rt.LoadReport {})
assert json
let json_wire, json_why = schema_rt.save_reasoned(json)
print("json " + string(text_why == "" and json_why == "" and json_wire == wire))
for([-1, 256]) invalid:
    values[0].i = invalid
    let refused, refusal = schema_rt.save_reasoned(rec)
    print("invalid " + string(refused == "" and refusal != ""))
row.Loadout = "x"
let short_buffer = schema_rt.zeros(bytearray.RowFixedMeasure(1))
let short_n, short_wire, short_why = bytearray.RowFixedSaveChecked([row], short_buffer)
print("short " + string(short_n == -1 and short_why != "" and short_wire == short_buffer))
row.Loadout = bytes
row.LiveCount = 0
let min_buffer = schema_rt.zeros(bytearray.RowFixedMeasure(1))
let min_n, min_wire, min_why = bytearray.RowFixedSaveChecked([row], min_buffer)
print("minimum " + string(min_n == -1 and min_why != "" and min_wire == min_buffer))
bytearray.RowReset(back)
print("reset " + string(back.Loadout == schema_rt.zeros(4) and back.LiveCount == 1 and back.Live == schema_rt.zeros(4) and not back.OptionalPresent and back.Optional.Loadout == schema_rt.zeros(4)))
`
	out, _ := lobsterEvolutionRun(t, files, program, nil)
	want := "typed true\ntrusted true\nrecord true\njson true\ninvalid true\ninvalid true\nshort true\nminimum true\nreset true"
	if strings.TrimSpace(out) != want {
		t.Fatalf("fixed byte arrays:\n%s\nwant:\n%s", out, want)
	}
}

func TestLobsterFixedByteArrayGrowth(t *testing.T) {
	t.Parallel()
	older := "package bytegrowth\nfixed table Row { loadout [2]uint8 }\n"
	newer := "package bytegrowth\nfixed table Row { loadout [4]uint8 }\n"
	entry, ok := FixedLineageOf(lobsterUnitOf(t, older), "Row")
	if !ok {
		t.Fatal("missing old Row layout")
	}
	wire := make([]byte, 16)
	wire[0] = 3
	binary.LittleEndian.PutUint64(wire[8:], entry.Wire)
	wire = binary.LittleEndian.AppendUint32(wire, uint32(len(entry.Layout)))
	wire = append(wire, entry.Layout...)
	wire = binary.LittleEndian.AppendUint64(wire, entry.Wire)
	wire = append(wire, 128, 255)
	out := runLobsterVersionProbe(t, newer, []string{older}, 0, `
    let rec = schema_rt.load("Row", read_in("old.bin"), schema_rt.LoadReport {})
    assert_true(rec != nil, "byte-array growth accepts the old layout")
    if rec:
        let expected = bytegrowth.NewRow()
        expected.Loadout = write_int8_le(write_int8_le(schema_rt.zeros(4), 0, 128), 1, 255)
        assert_true(schema_rt.save(rec) == bytegrowth.RowFixedSave([expected]), "byte-array growth preserves values and defaults new slots")
    print("PASS")
`, map[string][]byte{"old.bin": wire})
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("byte-array growth: %s", out)
	}
}
