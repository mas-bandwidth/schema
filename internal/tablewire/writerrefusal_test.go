package tablewire

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// writerRefusalUnit holds one field of every shape a writer can be handed out
// of its declaration.
const writerRefusalUnit = `package writerrefusal

enum Mode { Idle, Run, Fly }

flags Cap { Trace, Verbose }

type Child
{
    n int32 | min = -50, max = 50
}

union Arm
{
    c Child
    k uint8
}

fixed table Root
{
    mode   Mode
    cap    Cap
    ratio  float32 | min = 0, max = 1, resolution = 0.01
    small  int8
    counts [..4]uint8
    bi     bits(9)
    wide   int128 | min = -1000, max = 1000
    q      fixed(8, 8) | min = -100, max = 100
    kid    Child
    arm    Arm
    name   string(6)
    wtext  wstring(4)
}
`

// TestWritersRefuseValuesOutsideTheirDeclaration holds docs/SPEC-TABLES.md §5
// and §16.3 for every writer of the engine: the variable form (Encode of a
// variable twin), the fixed form (Encode and EncodeFixed), the message form
// (EncodeMessage) and the text form (Write). Handed a value outside its
// declaration (off its range, too wide for its storage or its bits(n), an enum
// ordinal or a flags bit that names no variant, a union tag past its arms, a
// count or length past its bound, ill-formed UTF-8, an unpaired surrogate),
// each refuses *tabletext.InvalidValue and writes nothing. A writer never
// clamps and never truncates.
func TestWritersRefuseValuesOutsideTheirDeclaration(t *testing.T) {
	t.Parallel()
	fixed := covModel(t, writerRefusalUnit)
	variable := covVariableTwin(t, writerRefusalUnit)
	cell := func(inst *tabletext.Instance, name string) *tabletext.Field {
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				return &inst.Fields[i]
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	cases := []struct {
		name  string
		spoil func(inst *tabletext.Instance)
	}{
		{"ratio=1.5", func(i *tabletext.Instance) { cell(i, "ratio").Cell.F = 1.5 }},
		{"ratio=-1", func(i *tabletext.Instance) { cell(i, "ratio").Cell.F = -1 }},
		{"small=200", func(i *tabletext.Instance) { cell(i, "small").Cell.U = 200 }},
		{"bi=600", func(i *tabletext.Instance) { cell(i, "bi").Cell.U = 600 }},
		{"wide=1001", func(i *tabletext.Instance) { cell(i, "wide").Cell.Wide = big.NewInt(1001) }},
		{"q=101", func(i *tabletext.Instance) { cell(i, "q").Cell.Wide = big.NewInt(101 << 8) }},
		{"mode=9", func(i *tabletext.Instance) { cell(i, "mode").Cell.U = 9 }},
		{"cap bit 2", func(i *tabletext.Instance) { cell(i, "cap").Cell.U = 1 << 2 }},
		{"counts count 5", func(i *tabletext.Instance) { cell(i, "counts").Count = 5 }},
		{"counts element 256", func(i *tabletext.Instance) {
			f := cell(i, "counts")
			f.Count = 1
			f.Elems[0].U = 256
		}},
		{"kid.n=51", func(i *tabletext.Instance) { cell(cell(i, "kid").Cell.Tab, "n").Cell.U = 51 }},
		{"arm tag 3", func(i *tabletext.Instance) { cell(i, "arm").Cell.U = 3 }},
		{"name length 7", func(i *tabletext.Instance) { cell(i, "name").Cell.Str = []byte("abcdefg") }},
		{"name ill-formed text", func(i *tabletext.Instance) { cell(i, "name").Cell.Str = []byte{'a', 0xff} }},
		{"wtext unpaired surrogate", func(i *tabletext.Instance) { cell(i, "wtext").Cell.Units = []uint16{'a', 0xd800} }},
	}
	writers := []struct {
		name  string
		model *tabletext.Model
		write func(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error)
	}{
		{"Encode (variable)", variable, Encode},
		{"Encode (fixed)", fixed, Encode},
		{"EncodeFixed", fixed, EncodeFixed},
		{"EncodeMessage", fixed, EncodeMessage},
		{"Write (text)", fixed, func(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) { return m.Write(inst) }},
	}
	for _, w := range writers {
		clean := w.model.New(w.model.Lookup("Root"))
		if _, err := w.write(w.model, clean); err != nil {
			t.Fatalf("%s: the clean value was refused: %v", w.name, err)
		}
		for _, c := range cases {
			inst := w.model.New(w.model.Lookup("Root"))
			c.spoil(inst)
			out, err := func() (out []byte, err error) {
				defer func() {
					if p := recover(); p != nil {
						err = fmt.Errorf("panic: %v", p)
					}
				}()
				return w.write(w.model, inst)
			}()
			var iv *tabletext.InvalidValue
			if !errors.As(err, &iv) || out != nil {
				t.Errorf("%s: %s: the writer answered %x %v, want invalid_value", w.name, c.name, out, err)
			}
		}
	}
}

// TestWritersRefuseValuesNegativeControl removes each of three rules from the
// writers' one check (tabletext.CheckValue) and requires the refusal test red
// on that rule's own cases: the whole check, the declared range, and the UTF-8
// rule for text.
func TestWritersRefuseValuesNegativeControl(t *testing.T) {
	t.Parallel()
	const run = "^TestWritersRefuseValuesOutsideTheirDeclaration$"
	for _, c := range []struct {
		name     string
		sabotage slowtest.Sabotage
		red      []string
		green    []string
	}{
		{"the whole check", slowtest.Sabotage{File: "internal/tabletext/check.go",
			From: "\treturn m.checkInstance(inst, map[*Instance]bool{})", To: "\treturn nil"},
			[]string{"Encode (variable): ratio=1.5", "EncodeMessage: bi=600", "Write (text): mode=9"}, nil},
		{"the declared range", slowtest.Sabotage{File: "internal/tabletext/check.go",
			From: "if lo, hi, ok := ir.TableRawRange(f); ok && (", To: "if lo, hi, ok := ir.TableRawRange(f); false && ok && ("},
			[]string{"EncodeFixed: kid.n=51", "EncodeMessage: wide=1001"}, []string{"bi=600", "mode=9"}},
		{"the UTF-8 rule", slowtest.Sabotage{File: "internal/tabletext/check.go",
			From: "\t\tif !utf8.Valid(cell.Str) {", To: "\t\tif false {"},
			[]string{"Encode (variable): name ill-formed text", "Write (text): name ill-formed text"}, []string{"ratio=1.5"}},
	} {
		out, failed := slowtest.RunSabotaged(t, "../..", "./internal/tablewire", run, c.sabotage)
		if !failed {
			t.Errorf("negative control (%s): the rule is gone and the refusal test stayed green\n%s", c.name, out)
			continue
		}
		for _, want := range c.red {
			if !strings.Contains(out, want+": the writer answered") {
				t.Errorf("negative control (%s): %q did not go red\n%s", c.name, want, out)
			}
		}
		for _, still := range c.green {
			if strings.Contains(out, still+": the writer answered") {
				t.Errorf("negative control (%s): %q went red, but this sabotage does not touch it\n%s", c.name, still, out)
			}
		}
	}
}
