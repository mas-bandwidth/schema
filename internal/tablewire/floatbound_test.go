package tablewire_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A FLOAT32 FIELD'S DECLARED RANGE IS HELD AT FLOAT32 (docs/SPEC.md §4.6: the
// triple is evaluated "at float32, where every runtime evaluates" it;
// docs/SPEC-TABLES.md §3.3: a decoded value never leaves [min, max]). An end
// that float32 cannot spell exactly, such as max = 0.1 (float32 0.100000001)
// or min = 0.7 (float32 0.699999988), is the float32 nearest it. The value AT
// that end is written and read in every form; the value one float32 step past
// it is refused by the writer and by every read. Before the fix the engine
// compared the float32 value against the unrounded float64 end, so it refused
// bytes it wrote itself and the bytes the C++ reference and the generated Go
// write for the same value.

const floatBoundUnit = `package floatbound

table Top
{
    x float32 | min = 0, max = 0.1, resolution = 0.01
}

table Low
{
    y float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
}

fixed table FixedTop
{
    x float32 | min = 0, max = 0.1, resolution = 0.01
}

fixed table FixedLow
{
    y float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
}
`

// floatBoundCase is one end of one range: the root, its field, the end as
// float32 holds it, and the float32 one step outside it.
type floatBoundCase struct {
	root, field string
	end, past   float32
	inside      float32 // a value well inside the range, the patch marker
}

func floatBoundCases() []floatBoundCase {
	top := float32(0.1)
	low := float32(0.7)
	return []floatBoundCase{
		{"Top", "x", top, math.Nextafter32(top, 1), 0.05},
		{"Low", "y", low, math.Nextafter32(low, 0), 0.85},
		{"FixedTop", "x", top, math.Nextafter32(top, 1), 0.05},
		{"FixedLow", "y", low, math.Nextafter32(low, 0), 0.85},
	}
}

func floatBoundWrite(m *tabletext.Model, root string, inst *tabletext.Instance) ([]byte, error) {
	if tablewire.FixedRoot(m, m.Lookup(root)) {
		return tablewire.EncodeFixed(m, inst)
	}
	return tablewire.Encode(m, inst)
}

// TestFloat32RangeAtTheEndBinary: the variable (form 1) and fixed (form 3)
// paths. The end writes and reads back as the same float32 bits; one step past
// is refused by the writer, and by the read when forged into a written wire.
func TestFloat32RangeAtTheEndBinary(t *testing.T) {
	t.Parallel()
	m := floatBoundModel(t, floatBoundUnit)
	for _, c := range floatBoundCases() {
		inst := m.New(m.Lookup(c.root))
		floatField(t, inst, c.field).Cell.F = float64(c.end)
		wire, err := floatBoundWrite(m, c.root, inst)
		if err != nil {
			t.Fatalf("%s.%s = %v (the end as float32): the writer refused it: %v", c.root, c.field, c.end, err)
		}
		back := m.New(m.Lookup(c.root))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, back, wire, &report); !ok || err != nil || report.Refused {
			t.Fatalf("%s.%s = %v: the engine refused its own bytes: %v %v %+v", c.root, c.field, c.end, ok, err, report)
		}
		if got := math.Float32bits(float32(floatField(t, back, c.field).Cell.F)); got != math.Float32bits(c.end) {
			t.Fatalf("%s.%s: landed %#08x, want %#08x", c.root, c.field, got, math.Float32bits(c.end))
		}

		// THE WRITER REFUSES ONE STEP PAST
		floatField(t, inst, c.field).Cell.F = float64(c.past)
		if _, err := floatBoundWrite(m, c.root, inst); err == nil {
			t.Fatalf("%s.%s = %v (one float32 step past the end): the writer accepted it", c.root, c.field, c.past)
		}

		// THE READ REFUSES ONE STEP PAST, forged over an in-range value
		floatField(t, inst, c.field).Cell.F = float64(c.inside)
		marked, err := floatBoundWrite(m, c.root, inst)
		if err != nil {
			t.Fatalf("%s.%s = %v: %v", c.root, c.field, c.inside, err)
		}
		bad := replaced(t, marked, le32(math.Float32bits(c.inside)), le32(math.Float32bits(c.past)))
		report = tabletext.Report{}
		ok, err := tablewire.Decode(m, m.New(m.Lookup(c.root)), bad, &report)
		if ok || tablewire.RefusalReason(err) != tablewire.ReasonOutOfRange {
			t.Fatalf("%s.%s = %v forged: want out_of_range, got %v %v", c.root, c.field, c.past, ok, err)
		}
	}
}

// TestFloat32RangeAtTheEndText: the JSON path of the engine. The text every
// leg's writer emits for the end ("0.1", "0.7") reads, writes back as the same
// bytes, and the shortest text of one step past is refused out_of_range.
func TestFloat32RangeAtTheEndText(t *testing.T) {
	t.Parallel()
	m := floatBoundModel(t, floatBoundUnit)
	for _, c := range floatBoundCases() {
		for _, tc := range []struct {
			value float32
			ok    bool
		}{{c.end, true}, {c.past, false}} {
			text := `{"` + c.field + `": ` + formatF32(tc.value) + `}`
			inst := m.New(m.Lookup(c.root))
			var report tabletext.Report
			read := m.Read(inst, []byte(text), &report)
			if !tc.ok {
				if read && !report.Refused {
					t.Fatalf("%s %s: one step past the end was read", c.root, text)
				}
				continue
			}
			if !read || report.Refused {
				t.Fatalf("%s %s: the end was refused: %+v", c.root, text, report)
			}
			if got := math.Float32bits(float32(floatField(t, inst, c.field).Cell.F)); got != math.Float32bits(tc.value) {
				t.Fatalf("%s %s: landed %#08x", c.root, text, got)
			}
			if _, err := floatBoundWrite(m, c.root, inst); err != nil {
				t.Fatalf("%s %s: the value the text read does not write: %v", c.root, text, err)
			}
			if _, err := m.Write(inst); err != nil {
				t.Fatalf("%s %s: the text writer refused the value its reader landed: %v", c.root, text, err)
			}
		}
	}
}

func formatF32(v float32) string {
	return strconv.FormatFloat(float64(v), 'g', -1, 32)
}

func floatBoundModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}
