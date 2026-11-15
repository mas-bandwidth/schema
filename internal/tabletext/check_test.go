package tabletext_test

import (
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
// like any field. The variable, message and text forms never write a deprecated
// field, so their walks still skip it.
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
	if err := fixed.CheckValue(inst); err != nil {
		t.Fatalf("the text walk checked a deprecated field: %v", err)
	}
	if err := fixed.CheckBinaryValue(inst); err != nil {
		t.Fatalf("the message walk checked a deprecated field: %v", err)
	}
}
