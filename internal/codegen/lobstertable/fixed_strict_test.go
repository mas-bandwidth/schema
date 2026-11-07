package lobstertable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The second record is damaged so the range pass has already materialized a
// valid first record. Every failure must reset the complete caller-owned batch.
func TestLobsterFixedOneRead(t *testing.T) {
	t.Parallel()
	const source = `package strictfixed
enum Tier { First, Second }
fixed table Leaf {
    flag bool
    label string(3)
}
fixed table Row {
    seq uint32 = 7
    flag bool
    optional ?Leaf
    live [..2]Leaf
    values [2]int32
    ranged int32 | min = 0, max = 10
    ratio float32 | min = 0, max = 1, resolution = 0.01
    tier Tier
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
	if strings.Contains(string(tables["ProbeTable.lobster"]), "FixedLoadTolerant") {
		t.Fatal("obsolete tolerant fixed-read API emitted")
	}
	maps.Copy(files, tables)
	var row *ir.Struct
	for _, root := range ir.TableFixedRoots(u) {
		if root.Name == "Row" {
			row = root
		}
	}
	if row == nil {
		t.Fatal("Row is not fixed")
	}
	offsets := map[string]int64{}
	var at int64
	for _, field := range row.Fields {
		offsets[field.Name] = at
		at += ir.TableFixedFieldBytes(field)
	}
	main := fmt.Sprintf(`import .ProbeTable
let good = strictfixed.NewRow()
good.Seq = 99
good.Flag = true
good.OptionalPresent = true
good.Optional.Flag = true
good.LiveCount = 1
good.Live[0].Flag = true
good.Values = [11, 22]
good.Ranged = 5
good.Ratio = 0.5
good.Tier = strictfixed.TierFirst
let wire = strictfixed.RowFixedSave([good, good])
assert(length(wire) > 0)
let body = 20 + read_uint32_le(wire, 16) + 8
let second = body + 8 + strictfixed.RowFixedRecordBytes
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let back = [strictfixed.NewRow(), strictfixed.NewRow(), strictfixed.NewRow()]
let report = schema_rt.TableReport {}
var passed = 0
if strictfixed.RowFixedLoad(back, wire, plan, report) == 2 and strictfixed.RowFixedSave([back[0], back[1]]) == wire:
    passed += 1
else:
    print("valid round trip failed: " + report.reason)
let damaged:[string] = [
    write_int8_le(copy(wire), second + %[1]d, 2),
    write_int8_le(copy(wire), second + %[2]d, 2),
    write_int8_le(copy(wire), second + %[2]d + 1, 2),
    write_int8_le(copy(wire), second + %[3]d + 4, 2),
    write_int32_le(copy(wire), second + %[4]d, 11),
    write_float32_le(copy(wire), second + %[5]d, 2.0),
    write_int8_le(copy(wire), second + %[6]d, 3),
    substring(wire, 0, length(wire) - 1),
    ""
]
var case_index = 0
for(damaged) bad:
    for(back) r: r.Seq = 123
    let n = strictfixed.RowFixedLoad(back, bad, plan, report)
    if n == -1 and back[0].Seq == 7 and back[1].Seq == 7 and back[2].Seq == 7:
        passed += 1
    else:
        print("damage case " + string(case_index) + " returned " + string(n) + ": " + report.reason)
    case_index += 1
// Inactive optional payload and counted-array slack are not values.
var slack = write_int8_le(copy(wire), second + %[2]d, 0)
slack = write_int8_le(slack, second + %[2]d + 1, 255)
slack = write_int32_le(slack, second + %[2]d + 2, 99)
slack = write_int8_le(slack, second + %[3]d + 4 + 8, 255)
slack = write_int32_le(slack, second + %[3]d + 4 + 8 + 1, 99)
if strictfixed.RowFixedLoad(back, slack, plan, report) == 2 and not back[1].OptionalPresent and back[1].LiveCount == 1:
    passed += 1
else:
    print("inactive storage failed: " + report.reason)
print(passed)
`, offsets["flag"], offsets["optional"], offsets["live"], offsets["ranged"], offsets["ratio"], offsets["tier"])
	run := func(t *testing.T, files map[string][]byte) string {
		t.Helper()
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0644); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
	}
	if got := run(t, files); got != "11" {
		t.Fatalf("fixed-read checks: got %q, want 11", got)
	}
	t.Run("boolean_validation_mutation", func(t *testing.T) {
		mutant := maps.Clone(files)
		before := string(mutant["fixedevolve.lobster"])
		after := strings.Replace(before, "if v > 1:", "if v > 255:", 1)
		if before == after {
			t.Fatal("boolean mutation did not apply")
		}
		mutant["fixedevolve.lobster"] = []byte(after)
		if got := run(t, mutant); got == "11" {
			t.Fatal("boolean validation deletion escaped the native proof")
		}
	})
}

// A forged old ordinal can be inside the new enum's range. Widening must
// validate the writer's extent before the destination range check sees it.
func TestLobsterFixedOneReadEnumWiden(t *testing.T) {
	t.Parallel()
	schema := func(n int) string {
		variants := []string{"First", "Second"}
		for i := 3; i <= n; i++ {
			variants = append(variants, fmt.Sprintf("V%d", i))
		}
		return "package enumwide\nenum Tier { " + strings.Join(variants, ", ") + " }\nfixed table Row {\n tier Tier\n seq int32 = 7\n}\n"
	}
	older, newer := schema(2), schema(256)
	old := lobsterUnitOf(t, older)
	entry, ok := FixedLineageOf(old, "Row")
	if !ok {
		t.Fatal("old Row is not fixed")
	}
	wire := make([]byte, 16)
	wire[0] = 3
	binary.LittleEndian.PutUint64(wire[8:], entry.Wire)
	wire = binary.LittleEndian.AppendUint32(wire, uint32(len(entry.Layout)))
	wire = append(wire, entry.Layout...)
	wire = binary.LittleEndian.AppendUint64(wire, entry.Wire)
	bodyAt := len(wire)
	wire = append(wire, 2)
	wire = binary.LittleEndian.AppendUint32(wire, 9)
	forged := append([]byte(nil), wire...)
	forged[bodyAt] = 3
	out := runLobsterVersionProbe(t, newer, []string{older}, 0, `
    let back = [enumwide.NewRow()]
    let plan = enumwide.make_plan(4096)
    let report = schema_rt.TableReport {}
    assert_eq(enumwide.RowFixedLoad(back, read_in("good.bin"), plan, report), 1, "valid old enum widens")
    assert_eq(int(back[0].Tier), 2, "valid ordinal preserved")
    assert_eq(back[0].Seq, 9, "following field preserved")
    assert_eq(enumwide.RowFixedLoad(back, read_in("bad.bin"), plan, report), -1, "forged old ordinal refuses")
    assert_eq(back[0].Seq, 7, "refusal restores defaults")
    print "PASS"
`, map[string][]byte{"good.bin": wire, "bad.bin": forged})
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("enum-width read: %q", out)
	}
}
