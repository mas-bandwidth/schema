package gotable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The bound probes of docs/SPEC-TABLES.md §16.11 ("The bound probes"): for
// every bounded field a root table reaches, a wire one past the bound, written
// byte by byte in the field's own wire kind by the generated
// <Name>GenPastBound, and a subtest that requires the tolerant read to clamp
// it to the bound counting one clamped and the strict read to refuse it.

// tgEmit writes Go statements that write wire bytes into the writer named w;
// d is the nesting depth, which names the writer a framed part opens.
type tgEmit func(b *strings.Builder, w string, d int)

// tgVal is one past-bound value: the payload its kind carries after the kind
// byte, the JSON value the read must keep, and an upper bound on its bytes.
type tgVal struct {
	name    string // the suffix the probe's name takes: ":above", ".k:below", "[0]:above"
	payload tgEmit
	json    func(j *tgJSON)
	size    int64
}

// tgFill is a FILLER (§16.11): a value inside every bound, its payload and JSON.
type tgFill struct {
	payload tgEmit
	json    string
	size    int64
}

// tgMember is one field line of a body that a probe writes: a header and a
// payload, and its JSON member.
type tgMember struct {
	line tgEmit
	json func(j *tgJSON)
	size int64
}

// tgBounds walks a unit's root tables for their probes.
type tgBounds struct {
	notes []string
}

// tgBodyProbe is one probe of a body: the field lines it writes (guards,
// required fillers, the probed field) and their JSON members.
type tgBodyProbe struct {
	name    string
	members []tgMember
}

func (m tgBodyProbe) size() int64 {
	n := int64(1)
	for _, x := range m.members {
		n += x.size
	}
	return n
}

func (m tgBodyProbe) emitFields(b *strings.Builder, w string, d int) {
	for _, x := range m.members {
		x.line(b, w, d)
	}
}

// json writes the body as its JSON object, or as its one field's array for a
// `json = "array"` table (§16.10).
func (m tgBodyProbe) emitJSON(j *tgJSON, st *ir.Struct) {
	if st.JsonArray {
		m.members[len(m.members)-1].json(j)
		return
	}
	j.raw("{")
	for i, x := range m.members {
		if i > 0 {
			j.raw(",")
		}
		x.json(j)
	}
	j.raw("}")
}

// body lists the probes of every field line of st (§16.11): each with the
// guards that select it and the required fields beside it.
func (g *tgBounds) body(st *ir.Struct) ([]tgBodyProbe, error) {
	var out []tgBodyProbe
	for _, f := range st.Fields {
		if f.Deprecated {
			continue
		}
		vals, err := g.field(st, f)
		if err != nil {
			return nil, err
		}
		if len(vals) == 0 {
			continue
		}
		var around []tgMember
		guards, err := tgGuardMembers(st, f)
		if err != nil {
			return nil, err
		}
		around = append(around, guards...)
		for _, rf := range st.Fields {
			if rf == f || !rf.Required || rf.Deprecated || rf.Guard != "" {
				continue
			}
			fill, ok := tgFiller(rf)
			if !ok || rf.Array != ir.ArrayNone || rf.KeyEnum != "" || rf.IsMap() {
				return nil, fmt.Errorf("testgen: required field %s.%s has no bound-probe filler, and every probe of %s carries it (docs/SPEC-TABLES.md §16.11)", st.Name, rf.Name, st.Name)
			}
			around = append(around, tgFieldMember(rf, ir.TableWireFieldKind(rf), fill.payload, fill.json, fill.size))
		}
		for _, v := range vals {
			v := v
			members := append(append([]tgMember(nil), around...), tgMember{
				line: func(b *strings.Builder, w string, d int) {
					tgHeader(b, w, f, ir.TableWireFieldKind(f))
					v.payload(b, w, d)
				},
				json: func(j *tgJSON) {
					j.raw(strconv.Quote(ir.TableFieldJsonKey(f)) + ":")
					v.json(j)
				},
				size: v.size + 11,
			})
			out = append(out, tgBodyProbe{name: f.Name + v.name, members: members})
		}
	}
	return out, nil
}

// tgFieldMember is a field line at a fixed payload: a guard bool or a filler.
func tgFieldMember(f *ir.Field, kind int, payload tgEmit, value string, size int64) tgMember {
	return tgMember{
		line: func(b *strings.Builder, w string, d int) {
			tgHeader(b, w, f, kind)
			payload(b, w, d)
		},
		json: func(j *tgJSON) { j.raw(strconv.Quote(ir.TableFieldJsonKey(f)) + ":" + value) },
		size: size + 11,
	}
}

// tgGuardMembers sets the bools of every `if` branch on f's path so the
// branch holding f is the one taken (§16.11).
func tgGuardMembers(st *ir.Struct, f *ir.Field) ([]tgMember, error) {
	if f.Guard == "" {
		return nil, nil
	}
	var out []tgMember
	for _, term := range strings.Split(f.Guard, "&&") {
		term = strings.TrimSpace(term)
		want := !strings.HasPrefix(term, "!")
		name := strings.TrimPrefix(term, "!")
		var gf *ir.Field
		for _, c := range st.Fields {
			if c.Name == name {
				gf = c
			}
		}
		if gf == nil || gf.Type.Kind != ir.TBool {
			return nil, fmt.Errorf("testgen: field %s.%s is guarded by %q, which testgen cannot select (docs/SPEC-TABLES.md §16.11)", st.Name, f.Name, f.Guard)
		}
		bit := "0"
		if want {
			bit = "1"
		}
		out = append(out, tgFieldMember(gf, ir.TableKindBool, func(b *strings.Builder, w string, d int) {
			fmt.Fprintf(b, "%s.Put8(%s)\n", w, bit)
		}, strconv.FormatBool(want), 1))
	}
	return out, nil
}

func tgHeader(b *strings.Builder, w string, f *ir.Field, kind int) {
	fmt.Fprintf(b, "%s.Id(0x%016x)\n%s.Put8(%d)\n", w, ir.TableFieldWireId(f), w, kind)
}

// tgFramed writes `L`, then what inner writes, through a writer of its own
// that shares the id table (§3: every length is LEB128).
func tgFramed(size int64, inner tgEmit) tgEmit {
	return func(b *strings.Builder, w string, d int) {
		s := "w" + strconv.Itoa(d+1)
		fmt.Fprintf(b, "framed(%s, %d, func(%s *TableWriter) {\n", w, size, s)
		inner(b, s, d+1)
		b.WriteString("})\n")
	}
}

// field lists the past-bound values of one field line (§16.11): its own
// bound, an array's count, and every bound its elements, nested type bodies
// and union arms reach.
func (g *tgBounds) field(st *ir.Struct, f *ir.Field) ([]tgVal, error) {
	where := st.Name + "." + f.Name
	if f.IsMap() {
		g.notes = append(g.notes, where+" (a map)")
		return nil, nil
	}
	if f.Type.Pointer && !f.Type.Blob() {
		g.notes = append(g.notes, where+" (a pointer)")
		return nil, nil
	}
	if f.TextTotal > 0 {
		g.notes = append(g.notes, where+" (a text list total)")
	}
	if f.KeyEnumRef != nil {
		return g.keyed(f, where)
	}
	if f.Array == ir.ArrayNone {
		return g.value(f, where)
	}
	elemKind := ir.TableWireElemKind(f)
	var out []tgVal
	fill, fillable := tgFiller(f)
	if f.Array == ir.ArrayCounted {
		if !fillable {
			g.notes = append(g.notes, where+" count (no filler for its element)")
		} else {
			n := f.ArrayBound
			size := 12 + (n+1)*fill.size
			out = append(out, tgVal{
				name: ":count",
				payload: tgFramed(size, func(b *strings.Builder, w string, d int) {
					fmt.Fprintf(b, "%s.Put8(%d)\n%s.PutLeb(%d)\nfor i := 0; i < %d; i++ {\n", w, elemKind, w, n+1, n+1)
					fill.payload(b, w, d)
					b.WriteString("}\n")
				}),
				json: func(j *tgJSON) { tgJSONRepeat(j, fill.json, n) },
				size: size + 10,
			})
		}
	}
	elems, err := g.value(f, where)
	if err != nil {
		return nil, err
	}
	if len(elems) > 0 && !fillable {
		g.notes = append(g.notes, where+" elements (no filler for its element)")
		return out, nil
	}
	n := int64(1)
	switch f.Array {
	case ir.ArrayCounted:
		n = max(1, f.ArrayMin)
	case ir.ArrayFixed:
		n = f.ArrayBound
	}
	for _, e := range elems {
		e := e
		size := 12 + e.size + (n-1)*fill.size
		out = append(out, tgVal{
			name: "[0]" + e.name,
			payload: tgFramed(size, func(b *strings.Builder, w string, d int) {
				fmt.Fprintf(b, "%s.Put8(%d)\n%s.PutLeb(%d)\n", w, elemKind, w, n)
				e.payload(b, w, d)
				if n > 1 {
					fmt.Fprintf(b, "for i := 0; i < %d; i++ {\n", n-1)
					fill.payload(b, w, d)
					b.WriteString("}\n")
				}
			}),
			json: func(j *tgJSON) {
				j.raw("[")
				e.json(j)
				if n > 1 {
					j.raw(",")
					tgJSONList(j, fill.json, n-1)
				}
				j.raw("]")
			},
			size: size + 10,
		})
	}
	return out, nil
}

// keyed probes an enum-keyed array's elements through its first slot (§3.2):
// one triple of the key's variant id, `L`, and the element, a table's body
// directly and any other element's payload.
func (g *tgBounds) keyed(f *ir.Field, where string) ([]tgVal, error) {
	elems, err := g.value(f, where)
	if err != nil || len(elems) == 0 {
		return nil, err
	}
	e := f.KeyEnumRef
	slot := e.Variants[0]
	id := ir.TableWireId(e.VariantWireName(0))
	kind := ir.TableWireElemKind(f)
	table := ir.TableWireScalarKind(f) == ir.TableKindTable
	var out []tgVal
	for _, v := range elems {
		v := v
		size := 32 + v.size
		out = append(out, tgVal{
			name: "[" + slot + "]" + v.name,
			payload: tgFramed(size, func(b *strings.Builder, w string, d int) {
				fmt.Fprintf(b, "%s.Put8(%d)\n%s.PutLeb(1)\n%s.Id(0x%016x)\n", w, kind, w, w, id)
				if table {
					v.payload(b, w, d) // a table's payload is already `L`, then its body
				} else {
					tgFramed(v.size, v.payload)(b, w, d)
				}
			}),
			json: func(j *tgJSON) {
				j.raw("{" + strconv.Quote(slot) + ":")
				v.json(j)
				j.raw("}")
			},
			size: size + 10,
		})
	}
	return out, nil
}

// value lists the past-bound values of one value of f's element type: a
// scalar's own bound, a type body's fields, a union's arms.
func (g *tgBounds) value(f *ir.Field, where string) ([]tgVal, error) {
	t := f.Type
	if elem := f.TextListElem(); elem != nil {
		if !ir.TextListPlain(f) {
			g.notes = append(g.notes, where+" elements (an optional text list element)")
			return nil, nil
		}
		return tgStringPast(ir.TextListValue(f)), nil
	}
	switch t.Kind {
	case ir.TInt, ir.TBits, ir.TFixed:
		if t.Timestamp {
			return nil, nil
		}
		return tgIntPast(f), nil
	case ir.TString, ir.TWString:
		if t.Pointer {
			return nil, nil
		}
		return tgStringPast(f), nil
	case ir.TBytes:
		if t.Blob() {
			return nil, nil
		}
		n := t.Size
		size := int64(12 + n + 1)
		return []tgVal{{
			name: ":above",
			payload: tgFramed(size, func(b *strings.Builder, w string, d int) {
				fmt.Fprintf(b, "%s.Put8(%d)\n%s.PutLeb(%d)\n%s.Raw(bytes.Repeat([]byte{0x5a}, %d))\n", w, ir.TableKindU8, w, n+1, w, n+1)
			}),
			json: func(j *tgJSON) { (&tgGen{}).bytesValue(j, n) },
			size: size + 10,
		}}, nil
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Struct:
			if ref.IsTable {
				return nil, nil // probed as its own root (§16.11)
			}
			inner, err := g.body(ref)
			if err != nil {
				return nil, err
			}
			var out []tgVal
			for _, p := range inner {
				p := p
				size := p.size() + 10
				out = append(out, tgVal{
					name: "." + p.name,
					payload: tgFramed(p.size(), func(b *strings.Builder, w string, d int) {
						p.emitFields(b, w, d)
						fmt.Fprintf(b, "%s.Put8(0)\n", w)
					}),
					json: func(j *tgJSON) { p.emitJSON(j, ref) },
					size: size,
				})
			}
			return out, nil
		case *ir.Union:
			return g.union(ref, where)
		}
	}
	return nil, nil
}

// union probes each arm's bounds (§3: `arm id reference`, `kind`, `L`, the
// ARM PAYLOAD): a type body's fields under the arm's `L`, or a fixed-width
// scalar's bytes.
func (g *tgBounds) union(un *ir.Union, where string) ([]tgVal, error) {
	var out []tgVal
	for _, v := range un.Variants {
		if v.Void() {
			continue
		}
		arm := v.F
		id := ir.TableWireId(v.WireName())
		kind := ir.TableWireScalarKind(arm)
		var vals []tgVal
		if st, ok := arm.Type.Ref.(*ir.Struct); ok && arm.Array == ir.ArrayNone && !arm.Type.Pointer && !st.IsTextListElem() {
			if st.IsTable {
				continue
			}
			inner, err := g.body(st)
			if err != nil {
				return nil, err
			}
			for _, p := range inner {
				p := p
				vals = append(vals, tgVal{
					name: "." + p.name,
					payload: func(b *strings.Builder, w string, d int) {
						p.emitFields(b, w, d)
						fmt.Fprintf(b, "%s.Put8(0)\n", w)
					},
					json: func(j *tgJSON) { p.emitJSON(j, st) },
					size: p.size(),
				})
			}
		} else if ir.ArmWireFixedWidth(arm) > 0 && (arm.Type.Kind == ir.TInt || arm.Type.Kind == ir.TBits || arm.Type.Kind == ir.TFixed) {
			vals = tgIntPast(arm)
		} else {
			if past, _ := g.value(arm, where+"."+v.Name); len(past) > 0 {
				g.notes = append(g.notes, where+"."+v.Name+" (a union arm of this payload)")
			}
			continue
		}
		for _, p := range vals {
			p := p
			name := v.Name
			size := p.size + 32
			out = append(out, tgVal{
				name: "." + name + p.name,
				payload: func(b *strings.Builder, w string, d int) {
					fmt.Fprintf(b, "%s.Id(0x%016x)\n%s.Put8(%d)\n", w, id, w, kind)
					tgFramed(p.size, p.payload)(b, w, d)
				},
				json: func(j *tgJSON) {
					j.raw("{" + strconv.Quote(name) + ":")
					p.json(j)
					j.raw("}")
				},
				size: size,
			})
		}
	}
	return out, nil
}

// tgKindRange is the range a fixed-width integer kind can spell.
func tgKindRange(kind int) (lo, hi *big.Int) {
	bits := uint(8 * ir.TableKindWidth(kind))
	if ir.TableKindSigned(kind) {
		return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1)), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}

// tgIntPast is an integer's probes (§16.11): a declared range's ends, fixed
// point on its raw scale, and a bits(N)'s width, each only where the field's
// own kind spells the value past it.
func tgIntPast(f *ir.Field) []tgVal {
	kind := ir.TableWireScalarKind(f)
	klo, khi := tgKindRange(kind)
	var lo, hi *big.Int
	if l, h, ok := ir.TableRawRange(f); ok {
		lo, hi = l, h
	} else if f.Type.Kind == ir.TBits {
		lo, hi = testgenIntRange(f)
	} else {
		return nil
	}
	var out []tgVal
	end := func(name string, past, bound *big.Int) {
		out = append(out, tgVal{
			name:    name,
			payload: func(b *strings.Builder, w string, d int) { tgPutInt(b, w, kind, past) },
			json:    func(j *tgJSON) { j.raw(tgIntJSON(f, bound)) },
			size:    int64(ir.TableKindWidth(kind)),
		})
	}
	if lo.Cmp(klo) > 0 {
		end(":below", new(big.Int).Sub(lo, big.NewInt(1)), lo)
	}
	if hi.Cmp(khi) < 0 {
		end(":above", new(big.Int).Add(hi, big.NewInt(1)), hi)
	}
	return out
}

// tgIntJSON spells an integer-like raw value as its JSON token (§16.2).
func tgIntJSON(f *ir.Field, raw *big.Int) string {
	if f.Type.Kind == ir.TFixed {
		return testgenFixedDecimal(raw, f.Type.FracBits)
	}
	return raw.String()
}

// tgPutInt writes v at the kind's width, two's complement, little-endian.
func tgPutInt(b *strings.Builder, w string, kind int, v *big.Int) {
	width := ir.TableKindWidth(kind)
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), uint(8*width)))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	low := new(big.Int).And(u, mask).Uint64()
	switch width {
	case 1:
		fmt.Fprintf(b, "%s.Put8(0x%02x)\n", w, low)
	case 2:
		fmt.Fprintf(b, "%s.Put16(0x%04x)\n", w, low)
	case 4:
		fmt.Fprintf(b, "%s.Put32(0x%08x)\n", w, low)
	case 8:
		fmt.Fprintf(b, "%s.Put64(0x%016x)\n", w, low)
	default:
		high := new(big.Int).Rsh(u, 64).Uint64()
		fmt.Fprintf(b, "%s.Put128(0x%016x, 0x%016x)\n", w, low, high)
	}
}

// tgStringPast is a string(N) or wstring(N) one byte or code unit past N.
func tgStringPast(f *ir.Field) []tgVal {
	n := f.Type.Size
	unit := "'a'"
	width := int64(1)
	if f.Type.Kind == ir.TWString {
		unit, width = "'a', 0", 2
	}
	return []tgVal{{
		name: ":above",
		payload: func(b *strings.Builder, w string, d int) {
			fmt.Fprintf(b, "%s.PutLeb(%d)\n%s.Raw(bytes.Repeat([]byte{%s}, %d))\n", w, (n+1)*width, w, unit, n+1)
		},
		json: func(j *tgJSON) { (&tgGen{}).text(j, n) },
		size: 10 + (n+1)*width,
	}}
}

// tgFiller is the FILLER of f's element type (§16.11), false where none is
// spelled.
func tgFiller(f *ir.Field) (tgFill, bool) {
	t := f.Type
	put := func(kind int, v *big.Int, json string) (tgFill, bool) {
		return tgFill{func(b *strings.Builder, w string, d int) { tgPutInt(b, w, kind, v) }, json, int64(ir.TableKindWidth(kind))}, true
	}
	if f.TextListElem() != nil {
		if !ir.TextListPlain(f) {
			return tgFill{}, false
		}
		return tgFill{func(b *strings.Builder, w string, d int) { fmt.Fprintf(b, "%s.PutLeb(0)\n", w) }, `""`, 1}, true
	}
	if t.Pointer {
		return tgFill{}, false
	}
	kind := ir.TableWireScalarKind(f)
	switch t.Kind {
	case ir.TBool:
		return put(kind, big.NewInt(0), "false")
	case ir.TInt, ir.TBits, ir.TFixed:
		if t.Timestamp {
			return put(kind, big.NewInt(0), `"1970-01-01T00:00:00Z"`)
		}
		lo, _ := testgenIntRange(f)
		return put(kind, lo, tgIntJSON(f, lo))
	case ir.TFloat32, ir.TFloat64:
		v := 0.0
		if f.HasFloatRange {
			v = f.FMin
		}
		if t.Kind == ir.TFloat32 {
			return put(kind, new(big.Int).SetUint64(uint64(math.Float32bits(float32(v)))), strconv.FormatFloat(v, 'g', -1, 32))
		}
		return put(kind, new(big.Int).SetUint64(math.Float64bits(v)), strconv.FormatFloat(v, 'g', -1, 64))
	case ir.TString, ir.TWString:
		return tgFill{func(b *strings.Builder, w string, d int) { fmt.Fprintf(b, "%s.PutLeb(0)\n", w) }, `""`, 1}, true
	case ir.TBytes:
		return tgFill{func(b *strings.Builder, w string, d int) {
			fmt.Fprintf(b, "%s.PutLeb(2)\n%s.Put8(%d)\n%s.PutLeb(0)\n", w, w, ir.TableKindU8, w)
		}, `""`, 3}, true
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			id := ir.TableWireId(ref.VariantWireName(0))
			return tgFill{func(b *strings.Builder, w string, d int) { fmt.Fprintf(b, "%s.Id(0x%016x)\n", w, id) }, strconv.Quote(ref.Variants[0]), 10}, true
		case *ir.Flags:
			return put(kind, big.NewInt(0), "[]")
		case *ir.Struct:
			for _, sf := range ref.Fields {
				if sf.Required && !sf.Deprecated {
					return tgFill{}, false
				}
			}
			return tgFill{func(b *strings.Builder, w string, d int) { fmt.Fprintf(b, "%s.PutLeb(1)\n%s.Put8(0)\n", w, w) }, "{}", 2}, true
		}
	}
	return tgFill{}, false
}

// tgJSONList writes n copies of one JSON value, comma-separated.
func tgJSONList(j *tgJSON, v string, n int64) {
	if n <= 4 {
		for i := int64(0); i < n; i++ {
			if i > 0 {
				j.raw(",")
			}
			j.raw(v)
		}
		return
	}
	j.raw(v)
	j.code(fmt.Sprintf("strings.Repeat(%q, %d)", ","+v, n-1))
}

// tgJSONRepeat writes a JSON array of n copies of one value.
func tgJSONRepeat(j *tgJSON, v string, n int64) {
	j.raw("[")
	if n > 0 {
		tgJSONList(j, v, n)
	}
	j.raw("]")
}

// testgenBoundProbes writes <Name>GenPastBound and Test<Name>GenBoundProbes
// for one root table (§16.11), and nothing when the table has no probe. The
// notes are the bounds this section does not probe, written as comments.
func testgenBoundProbes(b *strings.Builder, st *ir.Struct, variable bool) error {
	g := &tgBounds{}
	probes, err := g.body(st)
	if err != nil {
		return err
	}
	n := st.Name
	for _, note := range g.notes {
		fmt.Fprintf(b, "\n// not probed: %s — SPEC-TABLES §16.11 probes no bound reached this way\n", note)
	}
	if len(probes) == 0 {
		return nil
	}
	fmt.Fprintf(b, "\n// %sGenPastBound is %s's PAST-BOUND IMAGE BUILDER (SPEC-TABLES §16.11): the\n// variable-form wire of the named probe, one bounded field one past one bound,\n// written byte by byte in the field's own wire kind, never through a writer.\n// It answers nil for a name it does not carry.\n", n, n)
	fmt.Fprintf(b, "func %sGenPastBound(probe string) []byte {\n", n)
	b.WriteString("\tframed := func(w *TableWriter, size int, write func(s *TableWriter)) {\n\t\ts := TableWriter{Buffer: make([]byte, size), Ids: w.Ids}\n\t\twrite(&s)\n\t\tif s.Overflow {\n\t\t\tw.Overflow = true\n\t\t\treturn\n\t\t}\n\t\tw.PutLeb(uint64(s.Offset))\n\t\tw.Raw(s.Buffer[:s.Offset])\n\t}\n")
	b.WriteString("\t_ = framed\n\tvar ids TableIds\n\tvar w0 *TableWriter\n\tswitch probe {\n")
	for _, p := range probes {
		var code strings.Builder
		p.emitFields(&code, "w0", 0)
		// the id table: eight bytes for each id the probe names, and its count.
		// Every id reference is counted at ten bytes or more in p.size(), so
		// one entry per nine bytes of body bounds the table from above.
		size := 1 + p.size() + 8*(p.size()/9+2)
		fmt.Fprintf(b, "\tcase %q:\n\t\tw0 = &TableWriter{Buffer: make([]byte, %d), Ids: &ids}\n\t\tw0.Put8(1)\n", p.name, size)
		for _, line := range strings.Split(strings.TrimSuffix(code.String(), "\n"), "\n") {
			b.WriteString("\t\t" + line + "\n")
		}
	}
	b.WriteString("\tdefault:\n\t\treturn nil\n\t}\n\tw0.Put8(0)\n\tw0.Trailer()\n\tif w0.Overflow || ids.Overflow {\n\t\treturn nil\n\t}\n\treturn w0.Buffer[:w0.Offset]\n}\n")

	fmt.Fprintf(b, "\n// Test%sGenBoundProbes reads every past-bound image of %s: the tolerant read\n// clamps it to the bound, counting one clamped and nothing else, and keeps\n// the value at the bound; the strict read refuses it clamped (SPEC-TABLES §16.11).\n", n, n)
	fmt.Fprintf(b, "func Test%sGenBoundProbes(t *testing.T) {\n\tt.Parallel()\n\tprobes := []struct{ name, want string }{\n", n)
	for _, p := range probes {
		var j tgJSON
		p.emitJSON(&j, st)
		fmt.Fprintf(b, "\t\t{%q, %s},\n", p.name, j.expr())
	}
	b.WriteString("\t}\n\tfor _, p := range probes {\n\t\tt.Run(p.name, func(t *testing.T) {\n\t\t\tt.Parallel()\n")
	fmt.Fprintf(b, "\t\t\twire := %sGenPastBound(p.name)\n\t\t\tif wire == nil {\n\t\t\t\tt.Fatalf(\"%%s: no past-bound image\", p.name)\n\t\t\t}\n", n)
	testgenSave(b, n)
	b.WriteString("\t\t\tvar tr TableReport\n")
	if variable {
		fmt.Fprintf(b, "\t\t\tvar lb %sBuilder\n\t\t\tif !lb.Init() {\n\t\t\t\tt.Fatal(\"builder init\")\n\t\t\t}\n\t\t\tdefer lb.Shutdown()\n\t\t\tok := %sLoadBuilder(&lb, wire, &tr) && lb.Lock()\n", n, n)
	} else {
		fmt.Fprintf(b, "\t\t\tread := new(%s)\n\t\t\tok := %sLoad(read, wire, &tr)\n", n, n)
	}
	b.WriteString("\t\t\tif !ok || tr.Clamped != 1 || tr.Unknown != 0 || tr.KindMismatch != 0 || tr.Widened != 0 || tr.Malformed || !errors.Is(tr.Err(), TableRefuseClamped) {\n\t\t\t\tt.Fatalf(\"%s: the tolerant read: ok=%v report %+v, want one clamped and nothing else\", p.name, ok, tr)\n\t\t\t}\n")
	if variable {
		b.WriteString("\t\t\tread := lb.AsConst()\n")
	}
	b.WriteString("\t\t\tvar r TableReport\n")
	testgenSampleAt(b, "\t\t\t", n, variable, "p.want")
	b.WriteString("\t\t\twantWire, wantJson := save(\"at the bound\", v)\n\t\t\tgotWire, gotJson := save(\"clamped\", read)\n")
	b.WriteString("\t\t\tif !bytes.Equal(gotWire, wantWire) || !bytes.Equal(gotJson, wantJson) {\n\t\t\t\tt.Fatalf(\"%s: the clamped value is not at the bound: read %s, want %s\", p.name, gotJson, wantJson)\n\t\t\t}\n")
	fmt.Fprintf(b, "\t\t\tif got, err := %sReadStrict(wire); got != nil || !errors.Is(err, TableRefuseClamped) {\n\t\t\t\tt.Fatalf(\"%%s: the strict read: %%v, want clamped\", p.name, err)\n\t\t\t}\n", n)
	b.WriteString("\t\t})\n\t}\n}\n")
	return nil
}
