package lobstertable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func TestLobsterVariableFramingArray(t *testing.T) {
	g := generatePublicLobster(t, []string{filepath.Join(root, "test/tables/Framing.schema")})
	inputs := map[string][]byte{"input": []byte(`{"items":[{"v":7},{"v":9}],"tag":41}`)}
	for _, name := range []string{"array_elem_terminator_early", "array_elem_L_past_body"} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = wire
	}
	const program = `import .FramingTable
import .json
import .schema_save
let input = read_file("input")
let early = read_file("array_elem_terminator_early")
let past = read_file("array_elem_L_past_body")
assert input and early and past
let rep = schema_rt.LoadReport {}
let rec = schema_rt.json_read("Bounded", input, rep)
assert rec
let wire, why = schema_rt.save_reasoned(rec)
assert why == "" and wire[0] == 1
let loaded = schema_rt.load("Bounded", wire, rep)
assert loaded and loaded.fields[0].count == 2 and loaded.fields[1].cell.i == 41
let first = loaded.fields[0].elems[0].tab
let second = loaded.fields[0].elems[1].tab
assert first and second and first.fields[0].cell.i == 7 and second.fields[0].cell.i == 9
let back, back_reason = schema_rt.save_reasoned(loaded)
assert back_reason == "" and back == wire
assert not schema_rt.load("Bounded", early, rep)
assert not schema_rt.load("Bounded", past, rep)
print("framing array pass")
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "framing array pass\n" {
		t.Fatal(out)
	}
}
