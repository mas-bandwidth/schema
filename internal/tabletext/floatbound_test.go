package tabletext_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A FLOAT32 FIELD'S DECLARED RANGE IS HELD AT FLOAT32 ON THE TEXT SIDE TOO
// (docs/SPEC.md §4.6; docs/SPEC-TABLES.md §3.3, §16.2). For max = 0.1 and
// min = 0.7, ends float32 cannot spell exactly, the JSON read lands the text
// "0.1" / "0.7" as the float32 end, the writer check (CheckValue, the required
// check) admits that value, and one float32 step past the end is refused by
// the read and by the writer. Before the fix the text read parsed the token at
// float32 and then compared it with the float64 end, refusing the text every
// leg's writer emits for the value, and the field's own declared default.
func TestFloat32RangeEndsHeldAtFloat32(t *testing.T) {
	t.Parallel()
	const src = `package probe

table Root
{
    top float32 | min = 0, max = 0.1, resolution = 0.01
    low float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
    need float32 | min = 0, max = 0.1, resolution = 0.01, required
}
`
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f}})
	if len(cerrs) > 0 {
		t.Fatal(cerrs)
	}
	m := tabletext.NewModel(u)
	field := func(inst *tabletext.Instance, name string) *tabletext.Field {
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				return &inst.Fields[i]
			}
		}
		t.Fatalf("no field %q", name)
		return nil
	}
	top, low := float32(0.1), float32(0.7)
	for _, c := range []struct {
		name  string
		value float32
		ok    bool
	}{
		{"top", top, true},
		{"top", math.Nextafter32(top, 1), false},
		{"low", low, true},
		{"low", math.Nextafter32(low, 0), false},
		{"need", top, true},
		{"need", math.Nextafter32(top, 1), false},
	} {
		token := strconv.FormatFloat(float64(c.value), 'g', -1, 32)
		t.Run(c.name+"/"+token, func(t *testing.T) {
			// THE TEXT READ
			inst := m.New(u.Tables["Root"])
			var rep tabletext.Report
			text := `{"need": 0.05, "` + c.name + `": ` + token + `}`
			if c.name == "need" {
				text = `{"need": ` + token + `}`
			}
			read := m.Read(inst, []byte(text), &rep) && !rep.Refused
			if read != c.ok {
				t.Fatalf("%s: read %v, want %v (%+v)", text, read, c.ok, rep)
			}
			if read {
				if got := math.Float32bits(float32(field(inst, c.name).Cell.F)); got != math.Float32bits(c.value) {
					t.Fatalf("%s: landed %#08x, want %#08x", text, got, math.Float32bits(c.value))
				}
			}

			// THE WRITER CHECK, on the value held as the float32 it is
			value := m.New(u.Tables["Root"])
			field(value, "need").Cell.F = 0.05
			field(value, c.name).Cell.F = float64(c.value)
			if err := m.CheckValue(value); (err == nil) != c.ok {
				t.Fatalf("CheckValue %s = %v: %v, want ok %v", c.name, c.value, err, c.ok)
			}
			if _, err := m.Write(value); (err == nil) != c.ok {
				t.Fatalf("Write %s = %v: %v, want ok %v", c.name, c.value, err, c.ok)
			}
			if c.name == "need" {
				if err := tabletext.ValidateRequiredValue(field(value, "need")); (err == nil) != c.ok {
					t.Fatalf("ValidateRequiredValue need = %v: %v, want ok %v", c.value, err, c.ok)
				}
			}
		})
	}
}
