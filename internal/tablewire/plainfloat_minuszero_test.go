package tablewire_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A PLAIN FLOAT'S -0 RIDES THE REFERENCE ENCODER TOO (docs/SPEC-TABLES.md
// §3.3, A PLAIN FLOAT'S SPECIAL VALUES: elision compares bit patterns at every
// elision a writer makes). The generated Go Save already holds the rule; this
// pins the pack path (tabletext.Read then tablewire.Encode) to it, at the
// scalar, the [N] all-default and the enum-keyed slot elisions, in the
// variable and the message form. A +0 is the default and still elides.

const plainMinusZeroSchema = `package pmz

enum Level { Low, High }

fixed table Z
{
    x float32
    y float64
    xs [..3]float64
    ys [2]float64
    ks [Level]float64
}
`

func plainMinusZeroModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Pmz.schema", []byte(plainMinusZeroSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Pmz.schema", Name: "Pmz.schema", Base: "Pmz", Bytes: []byte(plainMinusZeroSchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func plainMinusZeroRead(t *testing.T, m *tabletext.Model, text string) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup("Z"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s did not read: %+v", text, r)
	}
	return inst
}

const negZero64 = uint64(0x8000000000000000)

func TestPackPathPlainFloatMinusZeroRidesTheWire(t *testing.T) {
	t.Parallel()
	m := plainMinusZeroModel(t)
	empty := plainMinusZeroRead(t, m, `{}`)
	emptyWire, err := tablewire.Encode(m, empty)
	if err != nil {
		t.Fatal(err)
	}
	emptyMsg, err := tablewire.EncodeMessage(m, empty)
	if err != nil {
		t.Fatal(err)
	}

	// +0 is the declared default: every site elides it, wire equal to {}.
	plus := plainMinusZeroRead(t, m, `{"x": 0, "y": 0, "ys": [0, 0], "ks": {"High": 0}}`)
	if w, err := tablewire.Encode(m, plus); err != nil || !bytes.Equal(w, emptyWire) {
		t.Errorf("+0 did not elide on the variable wire: %x vs {} %x (%v)", w, emptyWire, err)
	}
	if w, err := tablewire.EncodeMessage(m, plus); err != nil || !bytes.Equal(w, emptyMsg) {
		t.Errorf("+0 did not elide on the message wire: %x vs {} %x (%v)", w, emptyMsg, err)
	}

	for _, c := range []struct {
		name, text string
		check      func(back *tabletext.Instance) uint64
	}{
		{"scalar float32", `{"x": -0}`, func(b *tabletext.Instance) uint64 { return math.Float64bits(oracleField(t, b, "x").Cell.F) }},
		{"scalar float64", `{"y": -0}`, func(b *tabletext.Instance) uint64 { return math.Float64bits(oracleField(t, b, "y").Cell.F) }},
		{"[..3]float64", `{"xs": [-0]}`, func(b *tabletext.Instance) uint64 { return math.Float64bits(oracleField(t, b, "xs").Elems[0].F) }},
		{"[2]float64 all-default", `{"ys": [-0, 0]}`, func(b *tabletext.Instance) uint64 { return math.Float64bits(oracleField(t, b, "ys").Elems[0].F) }},
		{"enum-keyed slot", `{"ks": {"High": -0}}`, func(b *tabletext.Instance) uint64 {
			fv := oracleField(t, b, "ks")
			return math.Float64bits(fv.Elems[tabletext.KeyedValueSlot(fv.Def, tabletext.EnumValue(fv.Def.KeyEnumRef, "High"))].F)
		}},
	} {
		inst := plainMinusZeroRead(t, m, c.text)
		// the text read keeps the sign of a plain -0 (§3.3)
		if got := c.check(inst); got != negZero64 {
			t.Errorf("%s: the text read landed %#016x, want -0", c.name, got)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if bytes.Equal(wire, emptyWire) {
			t.Errorf("%s: -0 was elided: the variable wire equals {}'s %x", c.name, wire)
		}
		back := m.New(m.Lookup("Z"))
		var r tabletext.Report
		if _, err := tablewire.Decode(m, back, wire, &r); err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if got := c.check(back); got != negZero64 {
			t.Errorf("%s: decode landed %#016x, want -0", c.name, got)
		}
		msg, err := tablewire.EncodeMessage(m, inst)
		if err != nil {
			t.Fatalf("%s: encode message: %v", c.name, err)
		}
		if bytes.Equal(msg, emptyMsg) {
			t.Errorf("%s: -0 was elided: the message wire equals {}'s %x", c.name, msg)
		}
	}
}
