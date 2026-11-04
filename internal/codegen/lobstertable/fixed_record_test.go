package lobstertable

import (
	"encoding/binary"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

func TestLobsterFixedRecordBridgeLineage(t *testing.T) {
	older := "package bridge\nfixed table Row { n uint8\n flag bool\n}\n"
	newer := "package bridge\nfixed table Row { n uint16\n flag bool\n added int32 = 7\n}\n"
	old := lobsterUnitOf(t, older)
	entry, ok := FixedLineageOf(old, "Row")
	if !ok {
		t.Fatal("missing old Row layout")
	}
	wire := make([]byte, 16)
	wire[0] = 3
	binary.LittleEndian.PutUint64(wire[8:], entry.Wire)
	wire = binary.LittleEndian.AppendUint32(wire, uint32(len(entry.Layout)))
	wire = append(wire, entry.Layout...)
	wire = binary.LittleEndian.AppendUint64(wire, entry.Wire)
	wire = append(wire, 42, 1)
	bad := append([]byte(nil), wire...)
	bad[len(bad)-1] = 2
	inputs := map[string][]byte{"good.bin": wire, "bad.bin": bad}
	out := runLobsterVersionProbe(t, newer, []string{older}, 0, `
    let report = schema_rt.LoadReport {}
    let rec = schema_rt.load("Row", read_in("good.bin"), report)
    assert_true(rec != nil, "generic fixed read evolves the old layout")
    if rec:
        let expected = bridge.NewRow()
        expected.N = 42
        expected.Flag = true
        let saved = schema_rt.save(rec)
        assert_true(saved == bridge.RowFixedSave([expected]), "evolved fields and defaults save in the current native layout")
    assert_true(not schema_rt.load("Row", read_in("bad.bin"), report), "old layout boolean remains strict")
    print("PASS")
`, inputs)
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("generic lineage bridge: %s", out)
	}
	out = runLobsterVersionProbe(t, newer, []string{older}, 1, `
    let report = schema_rt.LoadReport {}
    assert_true(not schema_rt.load("Row", read_in("good.bin"), report), "retired old layout refuses")
    assert_true(report.reason == "layout_unsupported", "native retirement diagnostic")
    print("PASS")
`, inputs)
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("generic retired lineage bridge: %s", out)
	}
}

// Generic binary and JSON callers must use the native fixed codec, including
// its strict refusal, integrity, and exactly-one-record caller capacity.
func TestLobsterFixedRecordBridge(t *testing.T) {
	u := lobsterUnitOf(t, `package bridge
enum Key { First, Second }
flags Mask { A, B }
type Leaf { x int16 = 3 }
union Choice {
    body Leaf
    number int32
    values [2]uint32
    label string(6)
    empty
}
fixed table Row {
    flag bool
    small int8
    large uint64
    wide uint128
    scaled fixed(8, 8) | min = -10, max = 10
    mask Mask
    key Key
    ratio float32
    label string(6)
    data bytes(3)
    wide_text wstring(4)
    optional ?Leaf
    absent ?int32
    fixed_values [2]int32
    live [..2]Leaf
    keyed [Key]uint32
    choices [5]Choice
}
fixed table Sealed | integrity = "crc32c" { flag bool }
fixed table Huge { data bytes(65536) }
table Variable { flag bool }
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
	main := `import .ProbeTable
import .load
import .schema_save
import .json
import .retain_unknown
let row = bridge.NewRow()
row.Flag = true
row.Small = -4
row.Large = 0xffffffffffffffff
row.Wide = schema_rt.Int128 { lo: 11, hi: 1 }
row.Scaled = -384
row.Mask = 3
row.Key = bridge.KeySecond
row.Ratio = 0.25
row.Label = "hello!"
row.LabelLength = 5
row.Data = "abc"
row.DataLength = 2
row.WideText = write_int16_le(row.WideText, 0, 0x03bb)
row.WideTextLength = 1
row.OptionalPresent = true
row.Optional.X = 7
row.FixedValues = [-8, 19]
row.LiveCount = 1
row.Live[0].X = 23
row.Keyed = [31, 41]
row.Choices[0].Type = bridge.ChoiceTypeBody
row.Choices[0].Body.X = 52
row.Choices[1].Type = bridge.ChoiceTypeNumber
row.Choices[1].Number = -63
row.Choices[2].Type = bridge.ChoiceTypeValues
row.Choices[2].Values = [74, 85]
row.Choices[3].Type = bridge.ChoiceTypeLabel
row.Choices[3].Label = "choice"
row.Choices[3].LabelLength = 6
row.Choices[4].Type = bridge.ChoiceTypeEmpty
let wire = bridge.RowFixedSave([row])
let report = schema_rt.LoadReport {}
let rec = schema_rt.load("Row", wire, report)
if rec:
    let saved, why = schema_rt.save_reasoned(rec)
    print("binary " + string(saved == wire and why == ""))
    assert(schema_rt.slot_named(rec, "label").count == 5)
    assert(schema_rt.slot_named(rec, "data").count == 2)
    assert(schema_rt.slot_named(rec, "wide_text").count == 1)
    let text, text_why = schema_rt.json_write(rec)
    let back = schema_rt.json_read("Row", text, report)
    if back:
        let json_wire, json_why = schema_rt.save_reasoned(back)
        print("json " + string(text_why == "" and json_why == "" and json_wire == wire))
    else: print("json false " + report.reason)
    schema_rt.slot_named(rec, "key").cell.i = 3
    let invalid, invalid_why = schema_rt.save_reasoned(rec)
    print("invalid " + string(invalid == "" and invalid_why != ""))
else:
    print("binary false " + report.reason)
    print("json false")
    print("invalid false")
let authored_text = read_file("authored.json")
assert authored_text
let authored = schema_rt.json_read("Row", authored_text, report)
assert authored
let authored_wire, authored_why = schema_rt.save_reasoned(authored)
let expected = bridge.NewRow()
expected.Flag = true
expected.Small = -4
print("authored " + string(authored_why == "" and authored_wire == bridge.RowFixedSave([expected])))
let body = 20 + read_uint32_le(wire, 16) + 8
let bad = write_int8_le(copy(wire), body, 2)
print("malformed " + string(not schema_rt.load("Row", bad, report) and report.refused))
print("empty " + string(not schema_rt.load("Row", bridge.RowFixedSave([]::bridge.Row), report)))
print("batch " + string(not schema_rt.load("Row", bridge.RowFixedSave([row, row]), report)))
let f1 = write_int8_le(schema_rt.zeros(10), 0, 1)
let refused = not schema_rt.load("Row", f1, report)
print("previous " + string(refused and report.reason == "previous_form"))
let sealed = bridge.NewSealed()
sealed.Flag = true
let _, sealed_wire, sealed_why = bridge.SealedFixedSaveChecked([sealed], schema_rt.zeros(bridge.SealedFixedMeasure(1)))
let sealed_rec = schema_rt.load("Sealed", sealed_wire, report)
if sealed_rec:
    let out, why = schema_rt.save_reasoned(sealed_rec)
    print("integrity " + string(sealed_why == "" and why == "" and out == sealed_wire))
else: print("integrity false " + report.reason)
let damaged = write_int8_le(copy(sealed_wire), length(sealed_wire) - 1, sealed_wire[length(sealed_wire) - 1] ^ 1)
print("crc " + string(not schema_rt.load("Sealed", damaged, report)))
let retention = schema_rt.Retain { used: 17, ids: [7], known: [9] }
let retained = schema_rt.load_retain("Row", wire, retention, report)
print("retain " + string(retained == nil and report.refused and retention.used == 0 and length(retention.ids) == 0 and length(retention.known) == 0))
let retained_wire = schema_rt.save_retain(authored, retention, report)
print("retain_save " + string(retained_wire == "" and report.refused))
let huge = schema_rt.new_record(bridge.HugeDescriptor)
let huge_wire, huge_why = schema_rt.save_reasoned(huge)
print("unsupported " + string(huge_wire == "" and huge_why != ""))
let variable = schema_rt.new_record(bridge.VariableDescriptor)
let variable_wire, variable_why = schema_rt.save_reasoned(variable)
let variable_back = schema_rt.load("Variable", variable_wire, report)
print("variable " + string(variable_why == "" and variable_wire[0] == 1 and variable_back != nil))
`
	out, _ := lobsterEvolutionRun(t, files, main, map[string][]byte{"authored.json": []byte(`{"flag":true,"small":-4}`)})
	want := "binary true\njson true\ninvalid true\nauthored true\nmalformed true\nempty true\nbatch true\nprevious true\nintegrity true\ncrc true\nretain true\nretain_save true\nunsupported true\nvariable true"
	if strings.TrimSpace(out) != want {
		t.Fatalf("fixed Record bridge:\n%s\nwant:\n%s", out, want)
	}
}
