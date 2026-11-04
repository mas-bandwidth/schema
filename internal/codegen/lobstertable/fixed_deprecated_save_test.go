package lobstertable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

func TestLobsterFixedDeprecatedSavesDefaults(t *testing.T) {
	u := lobsterUnitOf(t, `package depcheck
 type Leaf { live int32 = 11
    old int8 = 7 | deprecated
 }
 union Choice { body Leaf
    number int32
 }
 fixed table Row {
    live int32
    old int8 = 7 | deprecated
    optional ?Leaf | deprecated
    nested Leaf
    discarded Leaf | deprecated
    old_array [2]Leaf | deprecated
    live_array [2]Leaf
    choice Choice | deprecated
    old_float float64 = 1 | deprecated
    old_text string(4) = "ok" | deprecated
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
let expected_row = depcheck.NewRow()
expected_row.Live = 99
expected_row.Nested.Live = 42
expected_row.LiveArray[0].Live = 31
expected_row.LiveArray[1].Live = 41
let expected = depcheck.RowFixedSave([expected_row])
let row = depcheck.NewRow()
row.Live = 99
row.Old = 128
row.OptionalPresent = true
row.Optional.Live = 88
row.Optional.Old = -129
row.Nested.Live = 42
row.Nested.Old = 128
row.Discarded.Live = 77
row.Discarded.Old = 128
row.OldArray[0].Live = 51
row.OldArray[1].Old = 128
row.LiveArray[0].Live = 31
row.LiveArray[1].Live = 41
row.LiveArray[0].Old = 128
row.LiveArray[1].Old = -129
row.Choice.Type = depcheck.ChoiceTypeNumber
row.Choice.Number = 55
row.OldFloat = read_float64_le(write_int64_le(schema_rt.zeros(8), 0, 0x7ff8000000000001), 0)
row.OldText = "too long"
row.OldTextLength = 8
let n, wire, why = depcheck.RowFixedSaveChecked([row], schema_rt.zeros(depcheck.RowFixedMeasure(1)))
print("typed " + string(n > 0 and why == "" and wire == expected))
print("caller " + string(row.Old == 128 and row.OptionalPresent and row.Nested.Old == 128 and row.LiveArray[1].Old == -129))
let rec = schema_rt.new_record(depcheck.RowDescriptor)
schema_rt.slot_named(rec, "live").cell.i = 99
schema_rt.slot_named(rec, "old").cell.i = 128
schema_rt.slot_named(rec, "optional").present = true
schema_rt.slot_named(rec, "optional").cell.tab = nil
let nested = schema_rt.slot_named(rec, "nested").cell.tab
assert nested
schema_rt.slot_named(nested, "live").cell.i = 42
schema_rt.slot_named(nested, "old").cell.i = 128
schema_rt.slot_named(rec, "discarded").cell.tab = nil
schema_rt.slot_named(rec, "old_array").elems = []
for(schema_rt.slot_named(rec, "live_array").elems) item, index:
    let leaf = item.tab
    assert leaf
    schema_rt.slot_named(leaf, "live").cell.i = if index == 0: 31 else: 41
    schema_rt.slot_named(leaf, "old").cell.i = 128
schema_rt.slot_named(rec, "choice").cell.i = 500
schema_rt.slot_named(rec, "old_float").cell.f = row.OldFloat
schema_rt.slot_named(rec, "old_text").cell.s = "too long"
let generic, reason = schema_rt.save_reasoned(rec)
print("generic " + string(reason == "" and generic == expected))
let back = schema_rt.load("Row", expected, schema_rt.LoadReport {})
assert back
let again, again_why = schema_rt.save_reasoned(back)
print("read " + string(again_why == "" and again == expected))
`
	out, _ := lobsterEvolutionRun(t, files, program, nil)
	if strings.TrimSpace(out) != "typed true\ncaller true\ngeneric true\nread true" {
		t.Fatalf("deprecated fixed storage:\n%s", out)
	}
}
