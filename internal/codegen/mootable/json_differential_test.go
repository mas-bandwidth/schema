package mootable

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// I13, THE TEXT DIFFERENTIAL (docs/PORTING.md): random instances, the text the
// compiler's own engine (internal/tabletext, written from the spec and from
// neither backend) writes for each, and moo's text of the same value, byte
// compared; then the other direction, moo reading the engine's text back to the
// same value under a silent report. Pinned goldens never reach a float tie; a
// random differential does.
//
// The instances are the corpus's own shapes with every scalar redrawn at
// random inside its declared domain (floats from their whole bit space), plus
// the float-only unit every I13 leg runs. The value enters moo as a LITERAL
// (json_literal_test.go) rather than through moo's table wire, which is the
// tables layer's and is held by its own gates; the comparison is text against
// text, as I13 states it.

// i13Schema is the float-only unit of the Go and C# legs, byte for byte.
const i13Schema = `package probe
type Sample {
 s float32
 d float64
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

type redraw struct {
	r *rand.Rand
	m *tabletext.Model
}

func (d *redraw) float32Bits() uint32 {
	for {
		var b uint32
		switch d.r.IntN(3) {
		case 0:
			b = d.r.Uint32()
		case 1:
			b = math.Float32bits(float32(float64(d.r.IntN(2000001)-1000000) / math.Pow(10, float64(d.r.IntN(9)))))
		default:
			// a value with few significant bits: where the shortest spellings tie
			b = math.Float32bits(float32(math.Ldexp(float64(d.r.IntN(1<<12)), d.r.IntN(80)-40)))
		}
		if b&0x7f800000 != 0x7f800000 {
			return b
		}
	}
}

func (d *redraw) float64Bits() uint64 {
	for {
		var b uint64
		switch d.r.IntN(3) {
		case 0:
			b = d.r.Uint64()
		case 1:
			b = math.Float64bits(float64(d.r.Int64N(2e15)-1e15) / math.Pow(10, float64(d.r.IntN(18))))
		default:
			b = math.Float64bits(math.Ldexp(float64(d.r.IntN(1<<20)), d.r.IntN(200)-100))
		}
		if b&0x7ff0000000000000 != 0x7ff0000000000000 {
			return b
		}
	}
}

// between draws a big integer in [lo, hi], edges often.
func (d *redraw) between(lo, hi *big.Int) *big.Int {
	switch d.r.IntN(6) {
	case 0:
		return new(big.Int).Set(lo)
	case 1:
		return new(big.Int).Set(hi)
	}
	span := new(big.Int).Sub(hi, lo)
	span.Add(span, big.NewInt(1))
	n := new(big.Int)
	for range 5 {
		n.Lsh(n, 32)
		n.Or(n, new(big.Int).SetUint64(uint64(d.r.Uint32())))
	}
	n.Mod(n, span)
	return n.Add(n, lo)
}

func (d *redraw) text(limit int) []byte {
	pool := []rune{'a', 'Z', '0', ' ', '"', '\\', '/', '\n', '\t', 0x01, 0x1f, 0x7f, 'é', '€', '中', 0x1F600, 0xFFFD}
	var out []byte
	n := d.r.IntN(limit + 1)
	for len(out) < n {
		c := pool[d.r.IntN(len(pool))]
		if len(out)+utf8.RuneLen(c) > limit {
			break
		}
		out = utf8.AppendRune(out, c)
	}
	return out
}

// scalar redraws one cell of field f (element semantics); false leaves it.
func (d *redraw) scalar(f *ir.Field, cell *tabletext.Cell) {
	t := f.Type
	if t.Pointer || t.TextUnbounded() {
		return
	}
	if t.Timestamp {
		cell.I = d.r.Int64() >> uint(d.r.IntN(30))
		cell.U = uint64(cell.I)
		return
	}
	switch t.Kind {
	case ir.TBool:
		cell.B = d.r.IntN(2) == 0
		return
	case ir.TFloat32:
		if f.HasFloatRange {
			cell.F = float64(float32(f.FMin + d.r.Float64()*(f.FMax-f.FMin)))
			if cell.F < f.FMin || cell.F > f.FMax {
				cell.F = f.FMin
			}
			return
		}
		cell.F = float64(math.Float32frombits(d.float32Bits()))
		return
	case ir.TFloat64:
		if f.HasFloatRange {
			return
		}
		cell.F = math.Float64frombits(d.float64Bits())
		return
	case ir.TString:
		cell.Str = d.text(int(t.Size))
		return
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			var live []uint64
			live = append(live, 0)
			for i := range r.Variants {
				if !tabletext.VariantDeprecated(r, int64(i+1)) {
					live = append(live, uint64(i+1))
				}
			}
			cell.U = live[d.r.IntN(len(live))]
		case *ir.Flags:
			var mask uint64
			for i := range r.Variants {
				if d.r.IntN(2) == 0 {
					mask |= 1 << uint(i)
				}
			}
			cell.U = mask
		case *ir.Struct:
			if cell.Tab != nil {
				d.instance(cell.Tab)
			}
		}
		return
	}
	kind := ir.TableScalarKind(f)
	if ir.TableKindWide(kind) {
		lo, hi, ok := ir.TableRawRange(f)
		if !ok {
			return
		}
		cell.Wide = d.between(lo, hi)
		return
	}
	if t.Kind != ir.TInt && t.Kind != ir.TBits {
		return
	}
	var lo, hi *big.Int
	switch {
	case f.HasIntRange && f.IntMin != nil:
		lo, hi = f.IntMin, f.IntMax
	case t.Kind == ir.TBits:
		lo, hi = new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))
	case t.Signed:
		lo = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1)))
		hi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1)), big.NewInt(1))
	default:
		lo, hi = new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))
	}
	v := d.between(lo, hi)
	if t.Kind == ir.TInt && t.Signed {
		cell.I = v.Int64()
		cell.U = uint64(cell.I)
	} else {
		cell.U = v.Uint64()
		cell.I = int64(cell.U)
	}
}

func (d *redraw) instance(inst *tabletext.Instance) {
	// a field the writer elides keeps the value it has: the reader cannot
	// bring back what the text never carried. A guard's own bool keeps its
	// value too, so the elision is the golden's.
	guards := tabletext.Guards(inst.Def)
	conds := map[string]bool{}
	for _, terms := range guards {
		for _, term := range terms {
			conds[term.Field] = true
		}
	}
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if conds[f.Name] || (f.Type.Optional && !fv.Present) {
			continue
		}
		if terms, ok := guards[f.Name]; ok && !inst.GuardHolds(terms) {
			continue
		}
		switch {
		case f.IsMap():
			for _, e := range fv.Entries {
				v := &e.Tab.Fields[1]
				if v.Def.Array == ir.ArrayNone && !v.Def.Type.Pointer {
					d.scalar(v.Def, &v.Cell)
				}
			}
		case f.IsTextList(), f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		case f.Array != ir.ArrayNone:
			e := *f
			e.Array = ir.ArrayNone
			for j := range fv.Elems {
				d.scalar(&e, &fv.Elems[j])
			}
		default:
			if un, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
				_ = un
				if fv.Cell.Tab != nil {
					d.instance(fv.Cell.Tab)
				}
				continue
			}
			d.scalar(f, &fv.Cell)
		}
	}
}

// differentialCases is K redrawn copies of every textual instance of the
// corpus, plus n instances of the float-only unit, each with the engine's
// text; a draw the engine refuses to write is dropped and counted.
func differentialCases(t *testing.T, seed uint64, k, n int) (map[string]*ir.Unit, map[string][]gateCase, int) {
	t.Helper()
	m := readTextManifest(t)
	units := map[string]*ir.Unit{}
	cases := map[string][]gateCase{}
	dropped := 0
	r := rand.New(rand.NewPCG(seed, 13))
	for _, in := range m.instances {
		u := units[in.unit]
		if u == nil {
			u = loadUnit(t, m.units[in.unit])
			units[in.unit] = u
		}
		if !unitCarriesJson(u) {
			continue
		}
		golden := readGolden(t, in.name)
		model := tabletext.NewModel(u)
		for j := range k {
			inst := model.New(model.Lookup(in.root))
			var rep tabletext.Report
			if !model.Read(inst, golden, &rep) {
				t.Fatalf("the engine refuses %s", in.name)
			}
			(&redraw{r: r, m: model}).instance(inst)
			text, err := model.Write(inst)
			if err != nil {
				dropped++
				continue
			}
			cases[in.unit] = append(cases[in.unit], gateCase{fmt.Sprintf("%s#%d", in.name, j), in.root, append(text, '\n'), inst})
		}
	}
	model := oracleModel(t, i13Schema)
	units["i13"] = model.Unit
	for j := range n {
		inst := model.New(model.Lookup("Root"))
		d := &redraw{r: r, m: model}
		d.instance(inst)
		for _, name := range []string{"many", "deep"} {
			for i := range inst.Fields {
				if inst.Fields[i].Def.Name == name {
					inst.Fields[i].Count = r.IntN(9)
				}
			}
		}
		text, err := model.Write(inst)
		if err != nil {
			t.Fatalf("the engine refuses an I13 float draw: %v", err)
		}
		cases["i13"] = append(cases["i13"], gateCase{fmt.Sprintf("i13#%d", j), "Root", append(text, '\n'), inst})
	}
	return units, cases, dropped
}

func i13Run(t *testing.T, runtime map[string][]byte) (writeBad, readBad []string, total int) {
	t.Helper()
	units, cases, dropped := differentialCases(t, 20261028, 3, 300)
	if dropped > 0 {
		t.Logf("%d draws the engine refused to write were dropped", dropped)
	}
	for _, key := range sortedKeys(cases) {
		res := gateRun(t, units[key], cases[key], runtime)
		for i, c := range cases[key] {
			total++
			if res[i].write != string(c.text) {
				writeBad = append(writeBad, fmt.Sprintf("%s (%s): wrote\n%s\nthe engine\n%s", c.name, key, res[i].write, c.text))
			}
			if res[i].read != "ok" {
				readBad = append(readBad, fmt.Sprintf("%s (%s): %s\n%s", c.name, key, res[i].read, c.text))
			}
		}
	}
	return writeBad, readBad, total
}

// TestMooJsonTextDifferential is I13's moo cell: moo's text of every draw is
// the engine's, and moo reads the engine's text back to the draw, silent.
func TestMooJsonTextDifferential(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form)")
	t.Parallel()
	writeBad, readBad, total := i13Run(t, nil)
	for i, b := range writeBad {
		if i < 8 {
			t.Errorf("write: %s", b)
		}
	}
	for i, b := range readBad {
		if i < 8 {
			t.Errorf("read: %s", b)
		}
	}
	if len(writeBad)+len(readBad) > 0 {
		t.Fatalf("%d write and %d read differences over %d draws", len(writeBad), len(readBad), total)
	}
	t.Logf("%d draws, both directions", total)
}

// TestMooJsonTextDifferentialNegativeControl restores the magnitude
// tie-break I13 names (a tie rounded away from zero, not to even) in the
// writer's digit rounding, and requires the differential to go red on the
// write side.
func TestMooJsonTextDifferentialNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	const tie = "if nextDigit > 5 or (nextDigit == 5 and (rest or lastOdd)) then"
	if !strings.Contains(src, tie) {
		t.Fatal("the control's site moved: json.moo no longer rounds at " + tie)
	}
	broken := strings.Replace(src, tie, "if nextDigit >= 5 then", 1)
	writeBad, _, total := i13Run(t, map[string][]byte{"json.moo": []byte(broken)})
	if len(writeBad) == 0 {
		t.Fatalf("the magnitude tie-break wrote every one of %d draws as the engine does: the differential reaches no tie", total)
	}
	t.Logf("the magnitude tie-break differs on %d of %d draws", len(writeBad), total)
}
