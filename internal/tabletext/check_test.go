package tabletext_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const fixedDeprecatedCheckUnit = `package probefix

fixed table Root
{
    live    uint8
    retired uint8 = 3 | min = 1, max = 5, deprecated
}
`

// checkModel parses and checks one source into the tabletext model.
func checkModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("ProbeFix.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "ProbeFix.schema", Name: "ProbeFix.schema", Base: "ProbeFix", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// retiredField is the instance's deprecated field.
func retiredField(t *testing.T, inst *tabletext.Instance) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "retired" {
			return &inst.Fields[i]
		}
	}
	t.Fatal("no retired field")
	return nil
}

// TestFixedWalkChecksADeprecatedField holds docs/SPEC-TABLES.md §3.4 and §22.6
// (docs/rulings.json fixed-deprecated-field-is-carried): the FIXED form carries
// a deprecated field, so its writer's check refuses a value its kind refuses,
// like any field. The TEXT form writes a deprecated field at its line
// (§16.14 W3), so its walk checks it too. The variable and message forms
// never write a deprecated field, so their walks still skip it.
func TestFixedWalkChecksADeprecatedField(t *testing.T) {
	t.Parallel()
	fixed := checkModel(t, fixedDeprecatedCheckUnit)
	inst := fixed.New(fixed.Lookup("Root"))
	retiredField(t, inst).Cell.U = 9
	if err := fixed.CheckFixedValue(inst); err == nil {
		t.Fatal("the fixed walk accepted a deprecated field outside its declared range")
	}

	variable := checkModel(t, strings.ReplaceAll(fixedDeprecatedCheckUnit, "fixed table", "table"))
	vinst := variable.New(variable.Lookup("Root"))
	retiredField(t, vinst).Cell.U = 9
	if err := variable.CheckFileValue(vinst); err != nil {
		t.Fatalf("the variable-form file walk checked a deprecated field: %v", err)
	}
	if err := fixed.CheckValue(inst); err == nil {
		t.Fatal("the text walk accepted a deprecated field outside its declared range")
	}
	if err := fixed.CheckBinaryValue(inst); err != nil {
		t.Fatalf("the message walk checked a deprecated field: %v", err)
	}
}

// TABLETEXT-6. docs/SPEC-TABLES.md §16.14 W3 step 1: "a deprecated field
// (§2.10) is written at its line like any other field", and the text writer
// accepts exactly what the text read accepts (§16.3). A deprecated field
// holding a value its own range refuses is a writer refusal, never a text the
// read then refuses.
func TestJsonrefTextWriterChecksDeprecatedFields(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"fixed table", "table"} {
		m := checkModel(t, "package probe\n"+class+` ShipConfig
{
    armor   uint8 = 1 | min = 1, max = 10, deprecated
    tag     string(8) | deprecated
    shields uint8
}
`)
		st := m.Lookup("ShipConfig")
		for _, c := range []struct {
			name string
			set  func(inst *tabletext.Instance)
		}{
			{"armor past its max", func(inst *tabletext.Instance) { field(t, inst, "armor").Cell.U = 50 }},
			{"tag past its bound", func(inst *tabletext.Instance) { field(t, inst, "tag").Cell.Str = []byte("twelve bytes") }},
			{"tag holding a zero byte", func(inst *tabletext.Instance) { field(t, inst, "tag").Cell.Str = []byte{'a', 0} }},
			{"tag ill-formed", func(inst *tabletext.Instance) { field(t, inst, "tag").Cell.Str = []byte{0xff} }},
		} {
			inst := m.New(st)
			c.set(inst)
			out, err := m.Write(inst)
			if _, ok := errors.AsType[*tabletext.InvalidValue](err); !ok {
				var r tabletext.Report
				readBack := m.Read(m.New(st), out, &r)
				t.Errorf("%s, %s: Write err=%v, want invalid_value (read of the output: %v %+v)", class, c.name, err, readBack, r)
			}
		}
		// the lawful twin writes and reads back
		inst := m.New(st)
		field(t, inst, "armor").Cell.U = 10
		field(t, inst, "tag").Cell.Str = []byte("eight by")
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: lawful deprecated values refused: %v", class, err)
		}
		var r tabletext.Report
		if !m.Read(m.New(st), out, &r) {
			t.Fatalf("%s: lawful deprecated values do not read back: %+v\n%s", class, r, out)
		}
	}
}
