package lobstertable

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

func TestLobsterFixedCheckedIntegerStorage(t *testing.T) {
	const source = `package checkedint
fixed table Signed8 { value int8 }
fixed table Unsigned8 { value uint8 }
fixed table Signed16 { value int16 }
fixed table Unsigned16 { value uint16 }
fixed table Signed32 { value int32 }
fixed table Unsigned32 { value uint32 }
fixed table Full64 { value uint64 }
type Leaf { value int8 }
union Choice { value uint16 }
fixed table Containers {
    leaf Leaf
    optional ?int8
    fixed_values [2]int16
    live [..2]uint32
    choice Choice
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
	var main strings.Builder
	main.WriteString("import .ProbeTable\nvar passed = 0\n")
	cases := []struct {
		name, values string
	}{
		{"Signed8", "[-129, 128, -128, 127]"},
		{"Unsigned8", "[-1, 256, 0, 255]"},
		{"Signed16", "[-32769, 32768, -32768, 32767]"},
		{"Unsigned16", "[-1, 65536, 0, 65535]"},
		{"Signed32", "[-2147483649, 2147483648, -2147483648, 2147483647]"},
		{"Unsigned32", "[-1, 4294967296, 0, 4294967295]"},
	}
	for _, tc := range cases {
		fmt.Fprintf(&main, `for(%[2]s) value, index:
    let row = checkedint.New%[1]s()
    row.Value = value
    let sentinel = schema_rt.zeros(checkedint.%[1]sFixedMeasure(1))
    let before = copy(sentinel)
    let n, wire, why = checkedint.%[1]sFixedSaveChecked([row], sentinel)
    if index < 2:
        if n == -1 and why == "invalid_value" and wire == before: passed += 1
    elif n > 0 and why == "":
        let rows = [checkedint.New%[1]s()]
        let plan:[schema_rt.TableFixedEntry] = []
        for(checkedint.%[1]sFixedPlanMax): push(plan, schema_rt.TableFixedEntry {})
        if checkedint.%[1]sFixedLoad(rows, wire, plan, nil) == 1 and rows[0].Value == value: passed += 1
`, tc.name, tc.values)
	}
	main.WriteString(`for([0, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff]) value:
    let row = checkedint.NewFull64()
    row.Value = value
    let n, wire, why = checkedint.Full64FixedSaveChecked([row], schema_rt.zeros(checkedint.Full64FixedMeasure(1)))
    let rows = [checkedint.NewFull64()]
    let plan:[schema_rt.TableFixedEntry] = []
    for(checkedint.Full64FixedPlanMax): push(plan, schema_rt.TableFixedEntry {})
    if n > 0 and why == "" and checkedint.Full64FixedLoad(rows, wire, plan, nil) == 1 and rows[0].Value == value: passed += 1
for(5) index:
    let row = checkedint.NewContainers()
    if index == 0: row.Leaf.Value = 128
    elif index == 1:
        row.OptionalPresent = true
        row.Optional = -129
    elif index == 2: row.FixedValues[1] = 32768
    elif index == 3:
        row.LiveCount = 1
        row.Live[0] = 4294967296
    else:
        row.Choice.Type = checkedint.ChoiceTypeValue
        row.Choice.Value = -1
    let n, _, why = checkedint.ContainersFixedSaveChecked([row], schema_rt.zeros(checkedint.ContainersFixedMeasure(1)))
    if n == -1 and why == "invalid_value": passed += 1
let dead = checkedint.NewContainers()
dead.Optional = 128
dead.Live[0] = -1
let n, _, why = checkedint.ContainersFixedSaveChecked([dead], schema_rt.zeros(checkedint.ContainersFixedMeasure(1)))
if n > 0 and why == "": passed += 1
print(passed)
`)
	out, _ := lobsterEvolutionRun(t, files, main.String(), nil)
	if strings.TrimSpace(out) != "34" {
		t.Fatalf("checked native integer storage: got %q, want 34", out)
	}
}
