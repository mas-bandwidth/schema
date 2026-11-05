package tablewire

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A VALUE OUTSIDE ITS DOMAIN IS REFUSED ON THE MESSAGE FORM, NEVER MASKED
// (docs/SPEC-TABLES.md §22.6): a ranged integer off its bounds, a bits(N)
// past N bits, a text past its capacity and a count past its bound each refuse
// the batch as invalid_value, where the bit stream used to keep the low bits of
// the width. A bare signed integer is two's complement at its width, and a
// negative one still rides.
const writeRefusalUnit = `package writerefusal

fixed table Probe
{
    ranged int32 = 0 | min = -4, max = 10
    small  bits(3)
    narrow int8
    name   string(4)
    list   [..2]int32
}
`

func writeRefusalModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("WriteRefusal.schema", []byte(writeRefusalUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "WriteRefusal.schema", Name: "WriteRefusal.schema", Base: "WriteRefusal", Bytes: []byte(writeRefusalUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func TestMessageWriteRefusesOutOfDomainValues(t *testing.T) {
	t.Parallel()
	m := writeRefusalModel(t)
	field := func(inst *tabletext.Instance, name string) *tabletext.Field {
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				return &inst.Fields[i]
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	cases := []struct {
		name   string
		set    func(*tabletext.Instance)
		refuse bool
	}{
		{"in domain", func(inst *tabletext.Instance) {
			field(inst, "ranged").Cell.I = -4
			field(inst, "small").Cell.U = 7
			field(inst, "narrow").Cell.I = -128
		}, false},
		{"ranged below min", func(inst *tabletext.Instance) { field(inst, "ranged").Cell.I = -5 }, true},
		{"ranged above max inside the width", func(inst *tabletext.Instance) { field(inst, "ranged").Cell.I = 11 }, true},
		{"bits(3) past three bits", func(inst *tabletext.Instance) { field(inst, "small").Cell.U = 8 }, true},
		{"int8 past its width", func(inst *tabletext.Instance) { field(inst, "narrow").Cell.I = 128 }, true},
		{"string past its capacity", func(inst *tabletext.Instance) {
			fv := field(inst, "name")
			fv.Cell.Str = []byte("abcde")
			fv.Count = 5
		}, true},
		{"count past its bound", func(inst *tabletext.Instance) {
			fv := field(inst, "list")
			fv.Count = 3
			for len(fv.Elems) < 3 {
				fv.Elems = append(fv.Elems, tabletext.Cell{})
			}
		}, true},
	}
	for _, c := range cases {
		inst := m.New(m.Lookup("Probe"))
		c.set(inst)
		out, err := EncodeMessage(m, inst)
		var refusal *MessageRefusal
		refused := errors.As(err, &refusal) && refusal.Reason == ReasonInvalidValue
		if c.refuse && (!refused || out != nil) {
			t.Errorf("%s: want the invalid_value refusal and no bytes, got %d bytes, %v", c.name, len(out), err)
		}
		if !c.refuse && err != nil {
			t.Errorf("%s: want bytes, got %v", c.name, err)
		}
	}
}
