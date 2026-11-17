package lobstertable

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

// TestLobsterFixedSaveHoldsTheWriteContract pins the fixed-form writer that
// callers reach by the shared verb name, <T>FixedSave, as the checked writer
// (docs/SPEC.md §5.1, the FixedSave column; docs/rulings.json write-refusal).
// Lobster table writes refuse in every build: each off-contract value answers
// "" before any byte, and never stops the VM; each lawful boundary value is
// written and reads back through <T>FixedLoad equal.
func TestLobsterFixedSaveHoldsTheWriteContract(t *testing.T) {
	t.Parallel()
	u := lobsterUnitOf(t, `package fixedsave
enum Color { red, green, blue }
union Shape { small int8 }
fixed table Row {
    c float32 | min = -1, max = 1, resolution = 0.01
    r int32 | min = 0, max = 10
    s int8
    b bits(5)
    a [..3]int16
    t string(4)
    e Color
    u Shape
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

	// Each case is one mutation of a lawful row.
	refused := []struct{ name, set string }{
		{"compressed float below its range", "row.C = -1.01"},
		{"compressed float above its range", "row.C = 1.01"},
		{"compressed float not finite", "row.C = 0.0 / 0.0"},
		{"ranged int32 below its range", "row.R = -1"},
		{"ranged int32 above its range", "row.R = 11"},
		{"int8 below its storage width", "row.S = -129"},
		{"int8 above its storage width", "row.S = 128"},
		{"bits(5) at 2^5", "row.B = 32"},
		{"bits(5) negative", "row.B = -1"},
		{"array count K+1", "row.ACount = 4"},
		{"array count negative", "row.ACount = -1"},
		{"array element past its storage width", "row.ACount = 1\n    row.A[0] = 32768"},
		{"string used length N+1", "row.T = \"abcd\"\n    row.TLength = 5"},
		{"string used length negative", "row.TLength = -1"},
		{"ill-formed text", "row.T = write_int8_le(schema_rt.zeros(4), 0, -1)\n    row.TLength = 1"},
		{"text holding a NUL", "row.T = schema_rt.zeros(4)\n    row.TLength = 1"},
		{"enum ordinal past the set", "row.E = fixedsave.Color(4)"},
		{"union tag past the arms", "row.U.Type = fixedsave.ShapeType(2)"},
		{"union arm past its storage width", "row.U.Type = fixedsave.ShapeTypeSmall\n    row.U.Small = 128"},
	}
	lawful := []struct{ name, set, same string }{
		{"low boundaries",
			"row.C = -1.0\n    row.R = 0\n    row.S = -128\n    row.B = 0\n    row.ACount = 0\n    row.TLength = 0\n    row.E = fixedsave.ColorNone\n    row.U.Type = fixedsave.ShapeTypeSmall\n    row.U.Small = -128",
			"back.C == -1.0 and back.R == 0 and back.S == -128 and back.B == 0 and back.ACount == 0 and back.TLength == 0 and back.E == fixedsave.ColorNone and back.U.Type == fixedsave.ShapeTypeSmall and back.U.Small == -128"},
		{"high boundaries",
			"row.C = 1.0\n    row.R = 10\n    row.S = 127\n    row.B = 31\n    row.ACount = 3\n    row.A[0] = -32768\n    row.A[2] = 32767\n    row.T = \"abcd\"\n    row.TLength = 4\n    row.E = fixedsave.ColorBlue\n    row.U.Type = fixedsave.ShapeTypeSmall\n    row.U.Small = 127",
			"back.C == 1.0 and back.R == 10 and back.S == 127 and back.B == 31 and back.ACount == 3 and back.A[0] == -32768 and back.A[2] == 32767 and back.TLength == 4 and substring(back.T, 0, 4) == \"abcd\" and back.E == fixedsave.ColorBlue and back.U.Type == fixedsave.ShapeTypeSmall and back.U.Small == 127"},
	}

	var program strings.Builder
	program.WriteString("import .ProbeTable\n")
	for i, c := range refused {
		fmt.Fprintf(&program, `def refuse_%[1]d():
    let row = fixedsave.NewRow()
    %[2]s
    let lawful = fixedsave.NewRow()
    let wire = fixedsave.RowFixedSave([lawful, row])
    print("refused " + string(wire == ""))
refuse_%[1]d()
`, i, c.set)
	}
	for i, c := range lawful {
		fmt.Fprintf(&program, `def lawful_%[1]d():
    let row = fixedsave.NewRow()
    %[2]s
    let wire = fixedsave.RowFixedSave([row])
    let rows = [fixedsave.NewRow()]
    let plan:[schema_rt.TableFixedEntry] = []
    for(fixedsave.RowFixedPlanMax): push(plan, schema_rt.TableFixedEntry {})
    let n = if wire == "": -1 else: fixedsave.RowFixedLoad(rows, wire, plan, nil)
    let back = rows[0]
    print("lawful " + string(n == 1 and %[3]s))
lawful_%[1]d()
`, i, c.set, c.same)
	}

	out, _ := lobsterEvolutionRun(t, files, program.String(), nil)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(refused)+len(lawful) {
		t.Fatalf("<T>FixedSave: got %d result lines, want %d:\n%s", len(lines), len(refused)+len(lawful), out)
	}
	for i, c := range refused {
		if lines[i] != "refused true" {
			t.Errorf("<T>FixedSave wrote a batch holding an off-contract value (%s): %q", c.name, lines[i])
		}
	}
	for i, c := range lawful {
		if got := lines[len(refused)+i]; got != "lawful true" {
			t.Errorf("<T>FixedSave did not write and read back the %s: %q", c.name, got)
		}
	}
}
