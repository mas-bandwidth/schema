package lobstertable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

// TestLobsterFixedDeprecatedFieldsAreCarried pins the fixed form's
// deprecated-field rule (docs/SPEC-TABLES.md §3.4, §21.2, §22.6;
// docs/rulings.json fixed-deprecated-field-is-carried): a deprecated field is
// written as held, read as stored, and refused where any field of its kind
// would be refused. It replaces the earlier assertion that a deprecated slot
// carries its declared default.
func TestLobsterFixedDeprecatedFieldsAreCarried(t *testing.T) {
	t.Parallel()
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
let leaf = schema_rt.table_of("Leaf")
let expected_row = depcheck.NewRow()
expected_row.Live = 99
expected_row.Old = 100
expected_row.OptionalPresent = true
expected_row.Optional.Live = 88
expected_row.Optional.Old = 100
expected_row.Nested.Live = 42
expected_row.Nested.Old = 100
expected_row.Discarded.Live = 77
expected_row.Discarded.Old = 100
expected_row.OldArray[0].Live = 51
expected_row.OldArray[0].Old = 100
expected_row.OldArray[1].Live = 61
expected_row.OldArray[1].Old = 100
expected_row.LiveArray[0].Live = 31
expected_row.LiveArray[0].Old = 100
expected_row.LiveArray[1].Live = 41
expected_row.LiveArray[1].Old = 100
expected_row.Choice.Type = depcheck.ChoiceTypeNumber
expected_row.Choice.Number = 55
expected_row.OldFloat = 2.5
expected_row.OldText = "hi"
expected_row.OldTextLength = 2
let expected = depcheck.RowFixedSave([expected_row])
let row = depcheck.NewRow()
row.Live = 99
row.Old = 100
row.OptionalPresent = true
row.Optional.Live = 88
row.Optional.Old = 100
row.Nested.Live = 42
row.Nested.Old = 100
row.Discarded.Live = 77
row.Discarded.Old = 100
row.OldArray[0].Live = 51
row.OldArray[0].Old = 100
row.OldArray[1].Live = 61
row.OldArray[1].Old = 100
row.LiveArray[0].Live = 31
row.LiveArray[0].Old = 100
row.LiveArray[1].Live = 41
row.LiveArray[1].Old = 100
row.Choice.Type = depcheck.ChoiceTypeNumber
row.Choice.Number = 55
row.OldFloat = 2.5
row.OldText = "hi"
row.OldTextLength = 2
let n, wire, why = depcheck.RowFixedSaveChecked([row], schema_rt.zeros(depcheck.RowFixedMeasure(1)))
print("typed " + string(n > 0 and why == "" and wire == expected))
print("caller " + string(row.Old == 100 and row.OptionalPresent and row.Optional.Old == 100 and row.Discarded.Old == 100 and row.OldArray[1].Old == 100 and row.LiveArray[1].Old == 100 and row.OldFloat == 2.5 and row.OldTextLength == 2))
let rec = schema_rt.new_record(depcheck.RowDescriptor)
schema_rt.slot_named(rec, "live").cell.i = 99
schema_rt.slot_named(rec, "old").cell.i = 100
let optional = schema_rt.new_record(leaf)
schema_rt.slot_named(optional, "live").cell.i = 88
schema_rt.slot_named(optional, "old").cell.i = 100
schema_rt.slot_named(rec, "optional").present = true
schema_rt.slot_named(rec, "optional").cell.tab = optional
let nested = schema_rt.slot_named(rec, "nested").cell.tab
assert nested
schema_rt.slot_named(nested, "live").cell.i = 42
schema_rt.slot_named(nested, "old").cell.i = 100
let discarded = schema_rt.new_record(leaf)
schema_rt.slot_named(discarded, "live").cell.i = 77
schema_rt.slot_named(discarded, "old").cell.i = 100
schema_rt.slot_named(rec, "discarded").cell.tab = discarded
let old_array = schema_rt.slot_named(rec, "old_array")
old_array.elems = []
for(2): push(old_array.elems, schema_rt.Value {})
let old_array_0 = schema_rt.new_record(leaf)
schema_rt.slot_named(old_array_0, "live").cell.i = 51
schema_rt.slot_named(old_array_0, "old").cell.i = 100
old_array.elems[0].tab = old_array_0
let old_array_1 = schema_rt.new_record(leaf)
schema_rt.slot_named(old_array_1, "live").cell.i = 61
schema_rt.slot_named(old_array_1, "old").cell.i = 100
old_array.elems[1].tab = old_array_1
let live_array = schema_rt.slot_named(rec, "live_array")
live_array.elems = []
for(2): push(live_array.elems, schema_rt.Value {})
let live_array_0 = schema_rt.new_record(leaf)
schema_rt.slot_named(live_array_0, "live").cell.i = 31
schema_rt.slot_named(live_array_0, "old").cell.i = 100
live_array.elems[0].tab = live_array_0
let live_array_1 = schema_rt.new_record(leaf)
schema_rt.slot_named(live_array_1, "live").cell.i = 41
schema_rt.slot_named(live_array_1, "old").cell.i = 100
live_array.elems[1].tab = live_array_1
let choice = schema_rt.slot_named(rec, "choice")
choice.cell.i = 2
let choice_union = schema_rt.lookup_union("Choice")
assert choice_union
let choice_arm = schema_rt.new_slot(choice_union.arms[1])
choice.cell.arm = choice_arm
choice_arm.cell.i = 55
schema_rt.slot_named(rec, "old_float").cell.f = 2.5
schema_rt.slot_named(rec, "old_text").cell.s = "hi"
let generic, reason = schema_rt.save_reasoned(rec)
print("generic " + string(reason == "" and generic == expected))
let back = schema_rt.load("Row", expected, schema_rt.LoadReport {})
assert back
let again, again_why = schema_rt.save_reasoned(back)
let back_optional = schema_rt.slot_named(back, "optional").cell.tab
let back_optional_old = if back_optional: schema_rt.slot_named(back_optional, "old").cell.i else: 0
let back_nested = schema_rt.slot_named(back, "nested").cell.tab
assert back_nested
let back_old_array = back.fields[5].elems
let back_old_array_1 = back_old_array[1].tab
assert back_old_array_1
let read = again_why == "" and again == expected and schema_rt.slot_named(back, "old").cell.i == 100 and schema_rt.slot_named(back, "optional").present and back_optional_old == 100 and schema_rt.slot_named(back_nested, "old").cell.i == 100 and schema_rt.slot_named(back_old_array_1, "old").cell.i == 100 and back.fields[8].cell.f == 2.5 and back.fields[9].cell.s == "hi"
print("read " + string(read))
let nan_row = depcheck.NewRow()
nan_row.OldFloat = read_float64_le(write_int64_le(schema_rt.zeros(8), 0, 0x7ff8000000000001), 0)
let nan_n, nan_wire, nan_why = depcheck.RowFixedSaveChecked([nan_row], schema_rt.zeros(depcheck.RowFixedMeasure(1)))
print("nan " + string(nan_n < 0 and nan_why != ""))
let text_row = depcheck.NewRow()
text_row.OldText = "too long"
text_row.OldTextLength = 8
let text_n, text_wire, text_why = depcheck.RowFixedSaveChecked([text_row], schema_rt.zeros(depcheck.RowFixedMeasure(1)))
print("text " + string(text_n < 0 and text_why != ""))
let range_row = depcheck.NewRow()
range_row.Old = 128
let range_n, range_wire, range_why = depcheck.RowFixedSaveChecked([range_row], schema_rt.zeros(depcheck.RowFixedMeasure(1)))
print("range " + string(range_n < 0 and range_why != ""))
`
	out, _ := lobsterEvolutionRun(t, files, program, nil)
	if strings.TrimSpace(out) != "typed true\ncaller true\ngeneric true\nread true\nnan true\ntext true\nrange true" {
		t.Fatalf("deprecated fixed storage:\n%s", out)
	}
}
