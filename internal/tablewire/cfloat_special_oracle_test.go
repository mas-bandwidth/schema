package tablewire_test

import (
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A COMPRESSED FLOAT'S SPECIAL VALUES IN THE GO ORACLE (docs/SPEC-TABLES.md
// §3.3, A COMPRESSED FLOAT'S SPECIAL VALUES). The oracle is the wire fuzzer's
// divergence check, so it must take the rule the generated Go takes: a NaN or
// an infinity is REFUSED — by the writer, and by the VARIABLE read on its raw
// bits as non_finite_float, before the bounds pass — and never clamped; a
// finite value past an end clamps to that end and counts; a -0 lands as +0,
// uncounted, on the variable read and on the TEXT read. Two ranges, so "the
// index of 0" and "index 0" part. The plain float beside them is the control:
// a finite value, -0 included, rides bit for bit.

const oracleCFloat = `package cforacle

table Reading
{
    a float32 | min = 0, max = 1, resolution = 0.01
    b float32 | min = -1, max = 1, resolution = 0.01
    plain float32
}
`

func oracleCFloatModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(oracleCFloat))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(oracleCFloat), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func oracleField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("no field %q", name)
	return nil
}

// f32of is a cell's value as the float32 it rides as.
func f32of(t *testing.T, inst *tabletext.Instance, name string) uint32 {
	t.Helper()
	return math.Float32bits(float32(oracleField(t, inst, name).Cell.F))
}

func TestOracleCompressedFloatSpecialValuesVariable(t *testing.T) {
	m := oracleCFloatModel(t)
	for _, c := range []struct {
		name    string
		in      uint32
		a, b    uint32
		clamped int
	}{
		{"minus_zero", 0x80000000, 0x00000000, 0x00000000, 0},
		{"plus_zero", 0x00000000, 0x00000000, 0x00000000, 0},
		{"above_max", 0x40000000, 0x3F800000, 0x3F800000, 2},
		{"below_min", 0xC0000000, 0x00000000, 0xBF800000, 2},
	} {
		inst := m.New(m.Lookup("Reading"))
		v := float64(math.Float32frombits(c.in))
		oracleField(t, inst, "a").Cell.F = v
		oracleField(t, inst, "b").Cell.F = v
		oracleField(t, inst, "plain").Cell.F = v
		// the encoder writes the field whole; a value past an end is the
		// READ's to clamp (§4), which is what this row exercises
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		back := m.New(m.Lookup("Reading"))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, back, wire, &report); err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if got := f32of(t, back, "a"); got != c.a {
			t.Errorf("%s: A [0, 1] landed %#08x, want %#08x", c.name, got, c.a)
		}
		if got := f32of(t, back, "b"); got != c.b {
			t.Errorf("%s: B [-1, 1] landed %#08x, want %#08x", c.name, got, c.b)
		}
		if report.Clamped != c.clamped {
			t.Errorf("%s: clamped %d, want %d", c.name, report.Clamped, c.clamped)
		}
		// THE CONTROL: a plain float carries its finite pattern whole (§3).
		if got := f32of(t, back, "plain"); got != c.in {
			t.Errorf("%s: the plain float came back %#08x, want its own %#08x", c.name, got, c.in)
		}
	}
	// A NaN OR AN INFINITY IS REFUSED, NEVER CLAMPED: the writer refuses it,
	// and a wire that carries one anyway is refused on its raw bits, before
	// the bounds pass, naming the field (§3.3, ruling 1 of 2026-10-27).
	marker := float32(0.5)
	for _, bits := range []uint32{0x7FC00000, 0x7F800001, 0xFFA5A5A5, 0x7F800000, 0xFF800000} {
		inst := m.New(m.Lookup("Reading"))
		oracleField(t, inst, "a").Cell.F = float64(math.Float32frombits(bits))
		if _, err := tablewire.Encode(m, inst); err == nil || err.Error() != "non_finite_float: Reading.a" {
			t.Errorf("%#08x: the encoder answered %v, want the non_finite_float refusal of Reading.a", bits, err)
		}
		oracleField(t, inst, "a").Cell.F = float64(marker)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		bad := replaced(t, wire, le32(math.Float32bits(marker)), le32(bits))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Reading")), bad, &report)
		if ok || report.Clamped != 0 {
			t.Errorf("%#08x: the read landed or clamped it: %v %+v", bits, ok, report)
		}
		refusedAs(t, "compressed", err, report, "Reading.a")
	}
}

func TestOracleCompressedFloatSpecialValuesText(t *testing.T) {
	m := oracleCFloatModel(t)
	inst := m.New(m.Lookup("Reading"))
	var report tabletext.Report
	if !m.Read(inst, []byte("{\"a\": -0, \"b\": -0.0, \"plain\": -0}\n"), &report) {
		t.Fatalf("the text did not read: %+v", report)
	}
	if report.Clamped != 0 || report.KindMismatch != 0 {
		t.Errorf("a -0 counted: %+v", report)
	}
	for _, name := range []string{"a", "b"} {
		if got := math.Float64bits(oracleField(t, inst, name).Cell.F); got != 0 {
			t.Errorf("%s: a -0 landed %#016x, want +0", name, got)
		}
	}
	if got := math.Float64bits(oracleField(t, inst, "plain").Cell.F); got != 0x8000000000000000 {
		t.Errorf("plain: the -0 landed %#016x, want its sign bit kept", got)
	}
	// AND THE WRITER REFUSES A NaN BY NAME (§16.2): nothing lands.
	oracleField(t, inst, "a").Cell.F = math.NaN()
	if _, err := m.Write(inst); err == nil {
		t.Error("the text writer wrote a NaN in a compressed float")
	}
}
