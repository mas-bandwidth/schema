package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 ON EVERY GO WRITE PATH
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero: "a compressed float's negative zero reads as +0
// and is written as +0"). Each case loads the +0 twin, sets the one cell to -0
// IN THE NATIVE VALUE after the load (so no reader normalizes it first), saves,
// and owes the twin's bytes. The plain float beside it is the CONTROL: its -0
// rides bit-exact, so a writer that normalized every float fails it.
//
// The write sites this holds, each through tabletext.StoredFloat: the
// variable form's encodeElement (a scalar field, a [N] or counted element, an
// enum-keyed slot, a union arm, a nested body's field), the fixed form's
// encodeFixedElement (the same shapes, positional), the cook's region store
// (tablecook) and the text writer. The message form quantizes, and -0 takes the
// index of 0 there already; its rows are here so it stays that way.

const negZeroWriteSchema = `package nzg

enum Level { Low, High }

union Pick
{
    v float32 | min = 0, max = 0.1, resolution = 0.01
    n uint8
}

table Var
{
    a    float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z    float32 | min = 0, max = 0.1, resolution = 0.01
    xs   [2]float32 | min = 0, max = 0.1, resolution = 0.01
    ys   [..3]float32 | min = 0, max = 0.1, resolution = 0.01
    ks   [Level]float32 | min = 0, max = 0.1, resolution = 0.01
    pick Pick
    p    float32
}

fixed table Fix
{
    a    float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z    float32 | min = 0, max = 0.1, resolution = 0.01
    xs   [2]float32 | min = 0, max = 0.1, resolution = 0.01
    ys   [..3]float32 | min = 0, max = 0.1, resolution = 0.01
    ks   [Level]float32 | min = 0, max = 0.1, resolution = 0.01
    pick Pick
    p    float32
}
`

func negZeroWriteModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Nzg.schema", []byte(negZeroWriteSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Nzg.schema", Name: "Nzg.schema", Base: "Nzg", Bytes: []byte(negZeroWriteSchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func negZeroWriteRead(t *testing.T, m *tabletext.Model, root, text string) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s %s did not read: %+v", root, text, r)
	}
	return inst
}

var minusZero = math.Copysign(0, -1)

func TestCompressedNegZeroWrittenAsPlusZero(t *testing.T) {
	t.Parallel()
	m := negZeroWriteModel(t)
	forms := []struct {
		name   string
		encode func(*tabletext.Model, *tabletext.Instance) ([]byte, error)
	}{
		{"variable", tablewire.EncodeVariable},
		{"fixed", tablewire.EncodeFixed},
		{"message", tablewire.EncodeMessage},
		{"text", func(m *tabletext.Model, in *tabletext.Instance) ([]byte, error) { return m.Write(in) }},
		{"cook", func(m *tabletext.Model, in *tabletext.Instance) ([]byte, error) {
			return tablecook.Cook(m, in, tablecook.Options{})
		}},
	}
	cases := []struct {
		name, base string
		set        func(*testing.T, *tabletext.Instance)
		plain      bool // the control: a plain float keeps its -0
	}{
		{"leaf_nonzero_default", `{"a": 0}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "a").Cell.F = minusZero
		}, false},
		{"leaf_zero_default_elided", `{}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "z").Cell.F = minusZero
		}, false},
		{"array_element_forced", `{"xs": [0, 0.05]}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "xs").Elems[0].F = minusZero
		}, false},
		{"counted_element", `{"ys": [0.05, 0]}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "ys").Elems[1].F = minusZero
		}, false},
		{"keyed_slot", `{"ks": {"High": 0.05}}`, func(t *testing.T, in *tabletext.Instance) {
			fv := oracleField(t, in, "ks")
			fv.Elems[tabletext.KeyedValueSlot(fv.Def, tabletext.EnumValue(fv.Def.KeyEnumRef, "Low"))].F = minusZero
		}, false},
		{"union_arm", `{"pick": {"v": 0}}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "pick").Cell.Arm.Cell.F = minusZero
		}, false},
		{"plain_keeps_negzero", `{}`, func(t *testing.T, in *tabletext.Instance) {
			oracleField(t, in, "p").Cell.F = minusZero
		}, true},
	}
	for _, form := range forms {
		for _, root := range []string{"Var", "Fix"} {
			if (form.name == "fixed" || form.name == "message") && root == "Var" {
				continue // the fixed and message forms are a fixed table's alone (§3.3, §3.4)
			}
			for _, c := range cases {
				t.Run(form.name+"/"+root+"/"+c.name, func(t *testing.T) {
					twin, err := form.encode(m, negZeroWriteRead(t, m, root, c.base))
					if err != nil {
						t.Fatal(err)
					}
					in := negZeroWriteRead(t, m, root, c.base)
					c.set(t, in)
					got, err := form.encode(m, in)
					if err != nil {
						t.Fatal(err)
					}
					if c.plain {
						if bytes.Equal(got, twin) {
							t.Errorf("a plain float's -0 did not ride bit-exact:\n -0 %s\n +0 %s", hex.EncodeToString(got), hex.EncodeToString(twin))
						}
						return
					}
					if !bytes.Equal(got, twin) {
						t.Errorf("a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", hex.EncodeToString(got), hex.EncodeToString(twin))
					}
				})
			}
		}
	}
}
