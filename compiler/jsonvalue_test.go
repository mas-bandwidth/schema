// The raw JSON value declaration (docs/SPEC-TABLES.md §16.13): the value
// "value" of the table-line key json, its SHAPE held by name, the pointer
// targets it refuses, and the targets that refuse the construct itself.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonValueSchema is §16.13's declaration. Type names are free, so JsonNode
// and JsonKind are the spec's own spellings.
const jsonValueSchema = `package probe

enum JsonKind { Null, False, True, Number, String, Array, Object }

table JsonNode
{
    kind     JsonKind
    count    uint32
    key_len  uint32
    text_len uint32
}

table JsonValue | json = "value", integrity = "crc32c"
{
    nodes []JsonNode
    text  *bytes
}
`

// jsonValueErrors parses and checks one source, returning the diagnostics a
// refusal test names.
func jsonValueErrors(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// jsonValueWant fails unless one diagnostic carries want.
func jsonValueWant(t *testing.T, errs []error, want string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), want) {
			return
		}
	}
	t.Fatalf("no diagnostic contains %q; got: %v", want, errs)
}

func TestJsonValueDeclaration(t *testing.T) {
	t.Parallel()

	t.Run("accepted declaration compiles for Go", func(t *testing.T) {
		t.Parallel()
		u, errs := jsonValueUnit(t)
		if len(errs) > 0 {
			t.Fatalf("accepted declaration refused: %v", errs[0])
		}
		if !u.Tables["JsonValue"].IsJsonValue() {
			t.Fatal(`json = "value" not recorded on the table in the IR`)
		}
		// The marker is a table-carried IR fact: copying the table preserves it,
		// and checking a separate same-named ordinary table does not inherit it.
		copied := *u.Tables["JsonValue"]
		if !copied.IsJsonValue() {
			t.Fatal(`copying a json = "value" table lost its IR marker`)
		}
		plainSrc := "package probe\ntable JsonValue { text *bytes }\n"
		plainFile, parseErrs := parser.Parse("Plain.schema", []byte(plainSrc))
		if len(parseErrs) != 0 {
			t.Fatalf("parse plain unit: %v", parseErrs[0])
		}
		plain, plainErrs := check.Unit([]check.SourceFile{{
			Path: "Plain.schema", Name: "Plain.schema", Base: "Plain", Bytes: []byte(plainSrc), AST: plainFile,
		}})
		if len(plainErrs) != 0 {
			t.Fatalf("check plain unit: %v", plainErrs[0])
		}
		if plain.Tables["JsonValue"].IsJsonValue() {
			t.Fatal(`json = "value" marker leaked to a separately checked same-named table`)
		}
		files, err := New().Generate(u, "go", Options{"runtime": "standalone"})
		if err != nil {
			t.Fatalf("go refused the accepted declaration: %v", err)
		}
		if _, ok := files["ProbeTable.go"]; !ok {
			t.Fatalf("go emitted no ProbeTable.go for the declaration: %d files", len(files))
		}
	})

	shape := []struct{ name, src, want string }{
		{
			"field names order and kinds",
			strings.Replace(jsonValueSchema, "    nodes []JsonNode\n    text  *bytes", "    text  *bytes\n    nodes []JsonNode", 1),
			"json_value_shape: JsonValue: field 1 is named \"text\", not nodes",
		},
		{
			"field kinds",
			strings.Replace(jsonValueSchema, "    nodes []JsonNode", "    nodes [..4]JsonNode", 1),
			"json_value_shape: JsonValue: field nodes is not []JsonNode",
		},
		{
			"the element table's four fields",
			strings.Replace(jsonValueSchema, "    count    uint32", "    count    int64", 1),
			"json_value_shape: JsonValue: element field count is not uint32",
		},
		{
			"the element table's field order",
			strings.Replace(jsonValueSchema, "    kind     JsonKind\n    count    uint32", "    count    uint32\n    kind     JsonKind", 1),
			"json_value_shape: JsonValue: element field 1 is named \"count\", not kind",
		},
		{
			"the enum's seven variants in order",
			strings.Replace(jsonValueSchema, "enum JsonKind { Null, False, True, Number, String, Array, Object }", "enum JsonKind { Null, True, False, Number, String, Array, Object }", 1),
			"json_value_shape: JsonValue: variant 2 of enum JsonKind is \"True\", not False",
		},
		{
			`integrity = "crc32c"`,
			strings.Replace(jsonValueSchema, `table JsonValue | json = "value", integrity = "crc32c"`, `table JsonValue | json = "value"`, 1),
			`json_value_shape: JsonValue: integrity = "crc32c" on the same table line`,
		},
	}
	for _, tc := range shape {
		t.Run("shape "+tc.name, func(t *testing.T) {
			t.Parallel()
			jsonValueWant(t, jsonValueErrors(t, tc.src), tc.want)
		})
	}

	pointers := []struct{ name, decl, where string }{
		{"*T", "table Holder { p *JsonValue }", "Holder.p"},
		{"[]*T", "table Holder { ps []*JsonValue }", "Holder.ps"},
		{"[..N]*T", "table Holder { ps [..4]*JsonValue }", "Holder.ps"},
		{"pointer arm", "union Arm { v *JsonValue }\ntable Holder { a Arm }", "Holder.a.v"},
	}
	for _, tc := range pointers {
		t.Run("pointer "+tc.name, func(t *testing.T) {
			t.Parallel()
			src := jsonValueSchema + "\n" + tc.decl + "\n"
			jsonValueWant(t, jsonValueErrors(t, src),
				`table JsonValue: json = "value" refuses a table any pointer targets, and `+tc.where+" points at it")
		})
	}

	c := New()
	u, errs := jsonValueUnit(t)
	if len(errs) > 0 {
		t.Fatalf("accepted declaration refused: %v", errs[0])
	}
	for _, target := range c.Targets() {
		t.Run("target "+target, func(t *testing.T) {
			t.Parallel()
			_, err := c.Generate(u, target, Options{"runtime": "standalone"})
			if target == "go" {
				if err != nil {
					t.Fatalf("go refused the construct: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("target %s accepted json = \"value\"", target)
			}
			want := `target "` + target + `" refuses the construct json = "value" on table JsonValue`
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("target %s refusal is not the §16.13 sentence: %s", target, err)
			}
		})
	}
}

// jsonValueUnit is the accepted declaration's unit and its diagnostics.
func jsonValueUnit(t *testing.T) (*ir.Unit, []error) {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(jsonValueSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(jsonValueSchema), AST: f,
	}})
	if u == nil {
		t.Fatalf("check returned no unit: %v", cerrs)
	}
	return u, cerrs
}
