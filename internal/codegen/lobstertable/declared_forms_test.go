package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// Exercise the public target registry as well as the native descriptor runtime.
func generatePublicLobster(t *testing.T, paths []string) *loadUnit {
	t.Helper()
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "lobster", nil)
	if err != nil {
		t.Fatal(err)
	}
	g := &loadUnit{u: u, dir: t.TempDir()}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.lobster") {
			g.mods = append(g.mods, strings.TrimSuffix(name, ".lobster"))
		}
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(g.mods)
	return g
}

func TestLobsterDeclaredForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Forms.schema")
	const source = `package formprobe
table WireOnly | forms="wire" { value int32 = 7 }
table Envelope { child WireOnly }
fixed table NoBlock | forms="wire,json,cook" { value int32 }
fixed table BlockOnly | forms="wire,block" { value int32 }
table Array | forms="wire,json", json="array" { values []int32 }
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	g := generatePublicLobster(t, []string{path})
	emitted, err := os.ReadFile(filepath.Join(g.dir, "FormsTable.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"WireOnly", "BlockOnly", "Array"} {
		for _, verb := range []string{"CookMeasure", "Cook", "ReadCook", "Open", "OpenReason"} {
			if strings.Contains(string(emitted), "def "+name+verb+"(") {
				t.Fatalf("disabled public form: %s%s", name, verb)
			}
		}
	}
	const program = `import .FormsTable
import .json
import .text_form
import .schema_save
import .block_form
import .cook
let rep = schema_rt.LoadReport {}
let input_wire = read_file("wire_json")
let input_envelope = read_file("envelope_json")
let empty = read_file("empty_json")
assert input_wire and input_envelope and empty
let wire_only = schema_rt.new_record(schema_rt.table_of("WireOnly"))
assert wire_only.fields[0].cell.i == 7
assert not schema_rt.json_read_into(wire_only, input_wire, rep)
assert rep.refused and wire_only.fields[0].cell.i == 7
assert not schema_rt.json_read("WireOnly", empty, rep)
let forbidden, why = schema_rt.json_write(wire_only)
assert forbidden == "" and why != ""
assert not schema_rt.text_load("WireOnly", "(:value 42)")
let wire, wire_reason = schema_rt.save_reasoned(wire_only)
assert wire_reason == ""
let loaded = schema_rt.load("WireOnly", wire, rep)
assert loaded and loaded.fields[0].cell.i == 7
let hidden_cook = schema_rt.cook_table_of("WireOnly")
assert hidden_cook
assert schema_rt.cook_measure(wire_only, hidden_cook.build) < 0
assert schema_rt.cook_write(wire_only, hidden_cook.build, false) == ""
// Produce a valid fixture independently of the disabled read entry points.
wire_only.d.cook_form = true
let hidden_bytes = schema_rt.cook_write(wire_only, hidden_cook.build, false)
wire_only.d.cook_form = false
assert not schema_rt.cook_read(hidden_bytes, hidden_cook.build, wire_only.d, hidden_cook)
assert schema_rt.cook_open_named("WireOnly", hidden_bytes, 0, false) != "ok"
let hidden_dump, hidden_reason = schema_rt.cook_dump(hidden_bytes, "WireOnly")
assert hidden_dump == "" and hidden_reason != ""
assert not schema_rt.lookup_block("WireOnly")
assert not schema_rt.lookup_block("NoBlock")
assert schema_rt.cook_table_of("NoBlock")
assert schema_rt.lookup_block("BlockOnly")
assert not schema_rt.table_of("BlockOnly").cook_form
assert not schema_rt.json_read("BlockOnly", empty, rep)
// A root's form declaration does not prohibit a containing root's traversal.
let envelope = schema_rt.json_read("Envelope", input_envelope, rep)
assert envelope
let nested = envelope.fields[0].cell.tab
assert nested and nested.fields[0].cell.i == 42
let enclosing_cook = schema_rt.cook_table_of("Envelope")
assert enclosing_cook
let enclosing_bytes = schema_rt.cook_write(envelope, enclosing_cook.build, false)
assert schema_rt.cook_read(enclosing_bytes, enclosing_cook.build, envelope.d, enclosing_cook)
let text, reason = schema_rt.json_write(envelope)
assert reason == "" and schema_rt.json_read("Envelope", text, rep)
let array = schema_rt.json_read("Array", "[1,2,3]", rep)
assert array and array.fields[0].count == 3 and array.fields[0].elems[2].i == 3
let array_text, array_reason = schema_rt.json_write(array)
assert array_reason == "" and schema_rt.json_read("Array", array_text, rep)
assert not schema_rt.table_of("Array").cook_form
print("forms pass")
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"wire_json": []byte(`{"value":42}`), "envelope_json": []byte(`{"child":{"value":42}}`), "empty_json": []byte(`{}`)}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "forms pass\n" {
		t.Fatal(out)
	}
}

func TestLobsterWireOnlyForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Only.schema")
	if err := os.WriteFile(path, []byte(`package onlyforms
table Only | forms="wire" { value int32 = 9 }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	g := generatePublicLobster(t, []string{path})
	const program = `import .OnlyTable
import .json
import .cook
import .schema_save
let rec = schema_rt.new_record(schema_rt.table_of("Only"))
let wire, why = schema_rt.save_reasoned(rec)
assert why == "" and schema_rt.load("Only", wire, schema_rt.LoadReport {})
let text, reason = schema_rt.json_write(rec)
assert text == "" and reason != ""
assert not schema_rt.cook_table_of("Only")
print("wire only pass")
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", nil)); out != "wire only pass\n" {
		t.Fatal(out)
	}
}

func TestLobsterPublicFeatureCook(t *testing.T) {
	g := generatePublicLobster(t, []string{filepath.Join(root, "testdata/conformance/feature-witnesses/full/Features.schema")})
	inputs := map[string][]byte{}
	for name, path := range map[string]string{
		"input": "testdata/conformance/tables/json/feature_full.json",
		"wire":  "testdata/conformance/feature-witnesses/full/Root.bin",
		"le":    "testdata/conformance/tables/cook-write/feature_full.cook",
		"be":    "testdata/conformance/tables/cook-write/feature_full-be.cook",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
import .schema_save
import .cook
let input = read_file("input")
let wire = read_file("wire")
let little = read_file("le")
let big = read_file("be")
assert input and wire and little and big
let rep = schema_rt.LoadReport {}
let rec = schema_rt.json_read("Root", input, rep)
assert rec
let saved, why = schema_rt.save_reasoned(rec)
assert why == "" and saved == wire
assert schema_rt.load("Root", wire, rep)
let damaged = write_int8_le(copy(wire), length(wire) - 1, wire[length(wire) - 1] ^ 1)
assert not schema_rt.load("Root", damaged, rep)
let ct = schema_rt.cook_table_of("Root")
assert ct
assert schema_rt.cook_write(rec, ct.build, false) == little
assert schema_rt.cook_write(rec, ct.build, true) == big
print("feature pass")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "feature pass\n" {
		t.Fatal(out)
	}
}
