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
// cases are a file with no bytes, a wrong form byte, and a two-record file
// whose second record carries a wrong record hash, so the batch check covers
// the record before the bad one as well as the bad one.
func TestFixedRefusalLeavesDestination(t *testing.T) {
	t.Parallel()
	const source = `package untouched
fixed table Row {
    seq uint32 = 7
    flag bool
    values [2]int32
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
let good = untouched.NewRow()
good.Seq = 99
good.Flag = true
good.Values = [11, 22]
let wire = untouched.RowFixedSave([good, good])
assert(length(wire) > 0)
let records_start = 20 + read_uint32_le(wire, 16)
let second_hash = records_start + 8 + untouched.RowFixedRecordBytes
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
let back = [untouched.NewRow(), untouched.NewRow(), untouched.NewRow()]
if untouched.RowFixedLoad(back, wire, plan, report) != 2:
    print("valid load failed: " + report.reason)
let cases:[string] = [
    "",
    write_int8_le(copy(wire), 0, 6),
    write_int64_le(copy(wire), second_hash, read_uint64_le(wire, second_hash) + 1)
]
let names = ["empty", "form", "record_hash"]
var passed = 0
var case_index = 0
for(cases) bad:
    var i = 0
    for(back) r:
        r.Seq = 500 + i
        r.Flag = true
        r.Values = [600 + i, 700 + i]
        i += 1
    let n = untouched.RowFixedLoad(back, bad, plan, report)
    var held = n == -1
    i = 0
    for(back) r:
        if r.Seq != 500 + i or not r.Flag or r.Values[0] != 600 + i or r.Values[1] != 700 + i:
            held = false
        i += 1
    if held:
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
	if got != "3" {
		t.Fatalf("refused fixed loads must leave the destination untouched: got %q, want 3", got)
	}
}
