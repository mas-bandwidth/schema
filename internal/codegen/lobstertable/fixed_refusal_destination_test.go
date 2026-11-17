package lobstertable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// REFUSE is total (docs/FIXED-FORM-ALGORITHM.md §5.3, §7 item 3): a fixed
// load refused before any record lands leaves every value of the caller's
// batch exactly as it was before the load, never reset to its defaults. The
// cases are a file with no bytes, a wrong form byte, and five two-record
// files whose SECOND record is refused by the CHECK pass: a wrong record
// hash, a bool byte of 2 in its last field, a text length past its cap, a
// bool byte of 2 inside a nested record, a bool byte of 2 inside a counted
// array's second element, and a count past the array's bound. The first
// record of each file is valid and differs from what the caller filled, so a
// record that landed before the refusal shows in the comparison. The whole
// destination is compared through its fixed-form image: every field of all
// three records, nested and array fields included.
func TestFixedRefusalLeavesDestination(t *testing.T) {
	t.Parallel()
	const source = `package untouched
fixed table Inner {
    id int32
    on bool
}
fixed table Row {
    seq uint32 = 7
    flag bool
    values [2]int32
    inner Inner
    items [..3]Inner
    note string(4)
    done bool
}
`
	u := lobsterUnitOf(t, source)
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	const main = `import .ProbeTable
def same_bytes(a:string, b:string) -> bool:
    if length(a) != length(b):
        return false
    var i = 0
    while i < length(a):
        if a[i] != b[i]:
            return false
        i += 1
    return true
def first_diff(a:string, b:string) -> int:
    var i = 0
    while i < length(a) and a[i] == b[i]:
        i += 1
    assert(i < length(a))
    return i
def fill(back:[untouched.Row]):
    var i = 0
    for(back) r:
        r.Seq = 500 + i
        r.Flag = true
        r.Values = [600 + i, 700 + i]
        r.Inner.Id = 800 + i
        r.Inner.On = true
        r.ItemsCount = 3
        var j = 0
        for(r.Items) e:
            e.Id = 900 + i * 10 + j
            e.On = true
            j += 1
        r.Note = write_int8_le(schema_rt.zeros(4), 0, 102 + i)
        r.NoteLength = 1
        r.Done = true
        i += 1
let good = untouched.NewRow()
good.Seq = 99
good.Flag = true
good.Values = [11, 22]
good.Inner.Id = 33
good.ItemsCount = 2
good.Items[0].Id = 44
good.Items[1].Id = 55
good.Note = write_int8_le(write_int8_le(schema_rt.zeros(4), 0, 111), 1, 107)
good.NoteLength = 2
let wire = untouched.RowFixedSave([good, good])
assert(length(wire) > 0)
let records_start = 20 + read_uint32_le(wire, 16)
let record_size = 8 + untouched.RowFixedRecordBytes
let second_hash = records_start + record_size
// field_at answers where, in the second record of wire, the byte lies that
// one change (which) to a probe row moves first: the offset is measured,
// never assumed from the layout.
def field_at(which:int) -> int:
    let probe = untouched.NewRow()
    probe.ItemsCount = 2
    let a = untouched.RowFixedSave([probe])
    if which == 0:
        probe.Done = true
    elif which == 1:
        probe.NoteLength = 1
    elif which == 2:
        probe.Inner.On = true
    elif which == 3:
        probe.Items[1].On = true
    else:
        probe.ItemsCount = 3
    let b = untouched.RowFixedSave([probe])
    return second_hash + first_diff(a, b) - records_start
let done_at = field_at(0)
let note_at = field_at(1)
let inner_on_at = field_at(2)
let item_on_at = field_at(3)
let count_at = field_at(4)
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
let back = [untouched.NewRow(), untouched.NewRow(), untouched.NewRow()]
if untouched.RowFixedLoad(back, wire, plan, report) != 2:
    print("valid load failed: " + report.reason)
let cases:[string] = [
    "",
    write_int8_le(copy(wire), 0, 6),
    write_int64_le(copy(wire), second_hash, read_uint64_le(wire, second_hash) + 1),
    write_int8_le(copy(wire), done_at, 2),
    write_int32_le(copy(wire), note_at, 5),
    write_int8_le(copy(wire), inner_on_at, 2),
    write_int8_le(copy(wire), item_on_at, 2),
    write_int32_le(copy(wire), count_at, 4)
]
let names = ["empty", "form", "record_hash", "last_field", "text_length", "nested_record", "array_element", "array_count"]
var passed = 0
var case_index = 0
for(cases) bad:
    fill(back)
    let before = untouched.RowFixedSave(back)
    let n = untouched.RowFixedLoad(back, bad, plan, report)
    let after = untouched.RowFixedSave(back)
    if n == -1 and same_bytes(before, after):
        passed += 1
    else:
        print(names[case_index] + " returned " + string(n) + " (" + report.reason + "): destination changed, seq " + string(back[0].Seq) + " " + string(back[1].Seq) + " " + string(back[2].Seq))
    case_index += 1
print(passed)
`
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
	if got != "8" {
		t.Fatalf("refused fixed loads must leave the destination untouched: got %q, want 8", got)
	}
}
