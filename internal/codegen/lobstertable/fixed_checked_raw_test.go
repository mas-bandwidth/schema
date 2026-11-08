package lobstertable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

func TestLobsterFixedCheckedRawStorageAndFloatRange(t *testing.T) {
	t.Parallel()
	u := lobsterUnitOf(t, `package rawcheck
fixed table Row {
    q fixed(4, 4) | min = -8, max = 7
    b bits(32)
    f float32 | min = 0, max = 10, resolution = 0.01
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
	program := `import .ProbeTable
import .schema_save
for(6) index:
    let row = rawcheck.NewRow()
    let rec = schema_rt.new_record(rawcheck.RowDescriptor)
    if index < 2:
        let v = if index == 0: -129 else: 128
        row.Q = v
        schema_rt.slot_named(rec, "q").cell.w = schema_rt.Int128 { lo: v, hi: if v < 0: -1 else: 0 }
    elif index < 4:
        let v = if index == 2: -1 else: 4294967296
        row.B = v
        schema_rt.slot_named(rec, "b").cell.i = v
    else:
        let v = if index == 4: -1.0 else: 11.0
        row.F = v
        schema_rt.slot_named(rec, "f").cell.f = v
    let buffer = schema_rt.zeros(rawcheck.RowFixedMeasure(1))
    let before = copy(buffer)
    let n, wire, why = rawcheck.RowFixedSaveChecked([row], buffer)
    print("typed " + string(n == -1 and why != "" and wire == before))
    let generic, reason = schema_rt.save_reasoned(rec)
    print("generic " + string(generic == "" and reason != ""))
for(2) index:
    let row = rawcheck.NewRow()
    row.Q = if index == 0: -128 else: 112
    row.B = if index == 0: 0 else: 4294967295
    row.F = if index == 0: 0.0 else: 10.0
    let n, wire, why = rawcheck.RowFixedSaveChecked([row], schema_rt.zeros(rawcheck.RowFixedMeasure(1)))
    let rec = schema_rt.load("Row", wire, schema_rt.LoadReport {})
    assert rec
    let generic, reason = schema_rt.save_reasoned(rec)
    print("boundary " + string(n > 0 and why == "" and reason == "" and generic == wire))
`
	out, _ := lobsterEvolutionRun(t, files, program, nil)
	want := strings.Repeat("typed true\ngeneric true\n", 6) + "boundary true\nboundary true"
	if strings.TrimSpace(out) != want {
		t.Fatalf("checked raw storage and range:\n%s\nwant:\n%s", out, want)
	}
}
