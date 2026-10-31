package moo

// THE INSTANCE GENERATOR of the packet-wire fuzz differential (fuzz_test.go).
//
// A random instance is drawn from a packet type's IR, inside every contract
// the type declares (ranges, bounds, enum and union extents, the finite float
// rule, UTF-8 text without a zero byte), and rendered TWICE from the one draw:
// as moo statements against the value New<Name>() built (THE VALUE MAPPING of
// docs/PORT-MOO.md: a number, a wide class, a binary32 pattern, a buffer), and
// as `set <path> <leaf>` lines the generic Go harness applies by reflection to
// the Go target's value. So the two writers are handed the same value built
// independently in each language, and the bytes they emit must be identical.

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// instance is one drawn value: the moo statements that build it from
// New<Name>() bound to `v`, and the Go harness's set lines.
type instance struct {
	moo []string
	set []string
}

type drawer struct {
	t testing.TB
	r *rand.Rand
	u *ir.Unit
}

// draw returns a random instance of the packet type or union named root.
func (d *drawer) draw(root string) instance {
	var in instance
	if st, ok := d.u.Structs[root]; ok {
		d.structBody(&in, st, "", "v")
		return in
	}
	if un, ok := d.u.Unions[root]; ok {
		d.unionBody(&in, un, "", "v")
		return in
	}
	d.t.Fatalf("fuzz: %s is neither a packet type nor a union", root)
	return in
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// structBody draws every item in wire order, so an `if` reads the bool its
// condition names from this draw (SPEC §4.5); the untaken side keeps what
// New<Name>() built, which neither writer reads.
func (d *drawer) structBody(in *instance, st *ir.Struct, gp, mp string) {
	bools := map[string]bool{}
	d.items(in, st, st.Items, bools, gp, mp)
}

func (d *drawer) items(in *instance, st *ir.Struct, items []ir.Item, bools map[string]bool, gp, mp string) {
	for _, it := range items {
		switch x := it.(type) {
		case *ir.FieldItem:
			if b, ok := d.field(in, x.F, ir.GoExportName(x.F.Name), gp, mp); ok {
				bools[x.F.Name] = b
			}
		case *ir.Branch:
			cond, ok := bools[x.Cond]
			if !ok {
				d.t.Fatalf("fuzz: %s: the branch on %q has no bool drawn before it", st.Name, x.Cond)
			}
			if cond != x.Neg {
				d.items(in, st, x.Then, bools, gp, mp)
			} else {
				d.items(in, st, x.Else, bools, gp, mp)
			}
		case *ir.ConstItem, *ir.ReservedItem, *ir.AlignItem:
			// storage-less: the writers own them
		default:
			d.t.Fatalf("fuzz: %s: an item of kind %T the generator does not know", st.Name, it)
		}
	}
}

func (d *drawer) unionBody(in *instance, un *ir.Union, gp, mp string) {
	tag := d.r.IntN(int(un.Max) + 1)
	in.set = append(in.set, fmt.Sprintf("set %s i:%d", join(gp, "Type"), tag))
	in.moo = append(in.moo, fmt.Sprintf("%s.Type = %d", mp, tag))
	if tag == 0 {
		return
	}
	arm := un.Variants[tag-1]
	if arm.Void() {
		return
	}
	d.field(in, arm.F, ir.GoExportName(arm.Name), gp, mp)
}

// field draws one field (its array framing included). It returns the drawn
// bool for a scalar bool, the value a later branch may read.
func (d *drawer) field(in *instance, f *ir.Field, name, gp, mp string) (bool, bool) {
	g, m := join(gp, name), mp+"."+name
	switch f.Array {
	case ir.ArrayNone:
		return d.scalar(in, f, g, m)
	case ir.ArrayFixed, ir.ArrayCounted:
		n := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			n = f.ArrayMin + d.below(f.ArrayBound-f.ArrayMin)
			in.set = append(in.set, fmt.Sprintf("set %sCount i:%d", g, n))
			in.moo = append(in.moo, fmt.Sprintf("%sCount = %d", m, n))
		}
		for i := int64(0); i < n; i++ {
			gi := fmt.Sprintf("%s.%d", g, i)
			switch {
			case isByteElem(f):
				v := d.r.IntN(256)
				in.set = append(in.set, fmt.Sprintf("set %s i:%d", gi, v))
				in.moo = append(in.moo, fmt.Sprintf("buffer.writeu8(%s, %d, %d)", m, i, v))
			case isBuffer(f.Type), f.Type.Kind == ir.TString:
				d.t.Fatalf("fuzz: %s: an array of text or bytes, which no packet corpus carries yet; extend the generator", f.Name)
			default:
				d.scalar(in, f, gi, fmt.Sprintf("%s[%d]", m, i+1))
			}
		}
		return false, false
	}
	d.t.Fatalf("fuzz: %s: array kind %d has no packet wire", f.Name, f.Array)
	return false, false
}

// below is a value in [0, span], the ends drawn often.
func (d *drawer) below(span int64) int64 {
	switch d.r.IntN(6) {
	case 0:
		return 0
	case 1:
		return span
	}
	return d.r.Int64N(span + 1)
}

// bigIn is a value in [lo, hi]: the ends, their neighbours, zero when inside,
// values of every bit length, and uniform ones.
func (d *drawer) bigIn(lo, hi *big.Int) *big.Int {
	span := new(big.Int).Sub(hi, lo)
	one := big.NewInt(1)
	switch d.r.IntN(10) {
	case 0:
		return new(big.Int).Set(lo)
	case 1:
		return new(big.Int).Set(hi)
	case 2:
		if span.Sign() > 0 {
			return new(big.Int).Add(lo, one)
		}
	case 3:
		if span.Sign() > 0 {
			return new(big.Int).Sub(hi, one)
		}
	case 4:
		if lo.Sign() <= 0 && hi.Sign() >= 0 {
			return new(big.Int)
		}
	case 5, 6:
		// an offset of a random bit length
		bits := span.BitLen()
		if bits == 0 {
			return new(big.Int).Set(lo)
		}
		k := d.r.IntN(bits) + 1
		off := d.randBits(k)
		if off.Cmp(span) > 0 {
			off.Mod(off, new(big.Int).Add(span, one))
		}
		return off.Add(off, lo)
	}
	off := d.randBits(span.BitLen() + 8)
	off.Mod(off, new(big.Int).Add(span, one))
	return off.Add(off, lo)
}

func (d *drawer) randBits(k int) *big.Int {
	v := new(big.Int)
	for k > 0 {
		take := min(k, 32)
		v.Lsh(v, uint(take))
		v.Or(v, new(big.Int).SetUint64(uint64(d.r.Uint32())>>(32-take)))
		k -= take
	}
	return v
}

func intLimits(signed bool, width int) (*big.Int, *big.Int) {
	if signed {
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width-1)), big.NewInt(1))
		return new(big.Int).Neg(new(big.Int).Add(hi, big.NewInt(1))), hi
	}
	return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
}

// mooInt renders an integer the way the field stores it in moo: a number, or
// the field's wide class.
func mooInt(f *ir.Field, v *big.Int) string {
	if c := wideClass(f); c != "" {
		return wideNew(c, v, classWidth(c))
	}
	return v.String()
}

func (d *drawer) scalar(in *instance, f *ir.Field, g, m string) (bool, bool) {
	t := f.Type
	setInt := func(v *big.Int, moo string) {
		in.set = append(in.set, fmt.Sprintf("set %s i:%s", g, v))
		in.moo = append(in.moo, fmt.Sprintf("%s = %s", m, moo))
	}
	switch t.Kind {
	case ir.TBool:
		b := d.r.IntN(2) == 1
		bit := 0
		if b {
			bit = 1
		}
		in.set = append(in.set, fmt.Sprintf("set %s b:%d", g, bit))
		in.moo = append(in.moo, fmt.Sprintf("%s = %t", m, b))
		return b, true
	case ir.TInt, ir.TBits:
		var lo, hi *big.Int
		switch {
		case f.HasIntRange:
			lo, hi = f.IntMin, f.IntMax
		case t.Kind == ir.TBits:
			lo, hi = intLimits(false, t.Width)
		default:
			lo, hi = intLimits(t.Signed, t.Width)
		}
		v := d.bigIn(lo, hi)
		setInt(v, mooInt(f, v))
	case ir.TFixed:
		lo, hi := rawBounds(f)
		v := d.bigIn(lo, hi)
		switch {
		case fixedIsNumber(f):
			setInt(v, fixedNumberLit(v))
		default:
			setInt(v, mooInt(f, v))
		}
	case ir.TFloat32:
		bits := d.float32Bits(f)
		in.set = append(in.set, fmt.Sprintf("set %s f32:%08x", g, bits))
		in.moo = append(in.moo, fmt.Sprintf("%s = 0x%08x", m, bits))
	case ir.TFloat64:
		if f.HasFloatRange {
			d.t.Fatalf("fuzz: %s: a compressed float64, which the generator does not draw", f.Name)
		}
		bits := d.float64Bits()
		in.set = append(in.set, fmt.Sprintf("set %s f64:%016x", g, bits))
		in.moo = append(in.moo, fmt.Sprintf("%s = %s", m, wideNew("Uint64", new(big.Int).SetUint64(bits), 64)))
	case ir.TString:
		s := d.text(int(t.Size))
		in.set = append(in.set, fmt.Sprintf("set %s x:%s", g, hex.EncodeToString(s)))
		in.set = append(in.set, fmt.Sprintf("set %sLength i:%d", g, len(s)))
		in.moo = append(in.moo, fmt.Sprintf("%s = %s", m, luaString(s)))
	case ir.TBytes:
		n := int(d.below(t.Size))
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(d.r.IntN(256))
		}
		in.set = append(in.set, fmt.Sprintf("set %s x:%s", g, hex.EncodeToString(b)))
		in.set = append(in.set, fmt.Sprintf("set %sLength i:%d", g, n))
		if n > 0 {
			in.moo = append(in.moo, fmt.Sprintf("buffer.writestring(%s, 0, %s)", m, luaString(b)))
		}
		in.moo = append(in.moo, fmt.Sprintf("%sLength = %d", m, n))
	case ir.TWString:
		units := d.wide(int(t.Size))
		var w []string
		for i, c := range units {
			w = append(w, fmt.Sprintf("%04x", c))
			in.moo = append(in.moo, fmt.Sprintf("buffer.writeu16(%s, %d, 0x%04x)", m, 2*i, c))
		}
		in.set = append(in.set, fmt.Sprintf("set %s w:%s", g, strings.Join(w, "")))
		in.set = append(in.set, fmt.Sprintf("set %sLength i:%d", g, len(units)))
		in.moo = append(in.moo, fmt.Sprintf("%sLength = %d", m, len(units)))
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			v := big.NewInt(d.below(r.Max))
			setInt(v, v.String())
		case *ir.Flags:
			_, hi := intLimits(false, r.WireBits)
			v := d.bigIn(new(big.Int), hi)
			setInt(v, wideNew("Uint64", v, 64))
		case *ir.Struct:
			d.structBody(in, r, g, m)
		case *ir.Union:
			d.unionBody(in, r, g, m)
		default:
			d.t.Fatalf("fuzz: %s: a named type %T the generator does not know", f.Name, t.Ref)
		}
	default:
		d.t.Fatalf("fuzz: %s: field kind %d the generator does not draw (wstring has no moo packet codec yet)", f.Name, t.Kind)
	}
	return false, false
}

// float32Bits is a finite binary32 pattern: for a compressed float a value in
// its declared range, otherwise any finite one — zeros of both signs,
// subnormals, the extremes and random magnitudes.
func (d *drawer) float32Bits(f *ir.Field) uint32 {
	if f.HasFloatRange {
		lo, hi := float32(f.FMin), float32(f.FMax)
		if float64(lo) < f.FMin {
			lo = math.Nextafter32(lo, float32(math.Inf(1)))
		}
		if float64(hi) > f.FMax {
			hi = math.Nextafter32(hi, float32(math.Inf(-1)))
		}
		var v float32
		switch d.r.IntN(6) {
		case 0:
			v = lo
		case 1:
			v = hi
		case 2:
			// a whole step, where the quantizer rounds exactly
			steps := float64(f.Steps)
			v = float32(f.FMin + float64(d.r.Int64N(f.Steps+1))*(f.FMax-f.FMin)/steps)
		default:
			v = lo + float32(d.r.Float64())*(hi-lo)
		}
		if v < lo {
			v = lo
		}
		if v > hi {
			v = hi
		}
		return math.Float32bits(v)
	}
	switch d.r.IntN(8) {
	case 0:
		return 0
	case 1:
		return 0x80000000
	case 2:
		return d.r.Uint32() & 0x807fffff // a subnormal (or zero) of either sign
	case 3:
		return 0x7f7fffff | d.r.Uint32()&0x80000000
	}
	for {
		b := d.r.Uint32()
		if b&0x7f800000 != 0x7f800000 {
			return b
		}
	}
}

func (d *drawer) float64Bits() uint64 {
	switch d.r.IntN(8) {
	case 0:
		return 0
	case 1:
		return 1 << 63
	case 2:
		return d.r.Uint64() & 0x800fffffffffffff
	case 3:
		return 0x7fefffffffffffff | d.r.Uint64()&(1<<63)
	case 4:
		return math.Float64bits(float64(d.r.IntN(2001) - 1000))
	}
	for {
		b := d.r.Uint64()
		if b&0x7ff0000000000000 != 0x7ff0000000000000 {
			return b
		}
	}
}

// textPool: ASCII, two- to four-byte characters, U+FFFF and the largest code
// point, so a mutated read meets every UTF-8 refusal. Never a zero byte.
var textPool = []string{"a", "z", "/", " ", "é", "碁", "바", "ا", "￿", "\U0001F600", "\U0010FFFF"}

// text is well-formed UTF-8 of at most limit bytes, without a zero byte.
func (d *drawer) text(limit int) []byte {
	want := int(d.below(int64(limit)))
	var b []byte
	for tries := 0; len(b) < want && tries < 4*limit+4; tries++ {
		c := textPool[d.r.IntN(len(textPool))]
		if len(b)+len(c) <= want {
			b = append(b, c...)
		}
	}
	return b
}

// wide is well-formed UTF-16 of at most limit code units (SPEC §4.12): no
// zero unit, every surrogate paired.
func (d *drawer) wide(limit int) []uint16 {
	want := int(d.below(int64(limit)))
	var u []uint16
	for len(u) < want {
		switch d.r.IntN(4) {
		case 0:
			if len(u)+2 <= want {
				u = append(u, uint16(0xD800+d.r.IntN(0x400)), uint16(0xDC00+d.r.IntN(0x400)))
				continue
			}
			u = append(u, 'a')
		case 1:
			u = append(u, uint16(0xE000+d.r.IntN(0x2000)))
		default:
			u = append(u, uint16(1+d.r.IntN(0xD7FF)))
		}
	}
	return u
}

// roots is every packet type and union of the unit with a wire of its own,
// sorted, so a seed names the same draws on every machine.
func roots(u *ir.Unit) []string {
	var out []string
	for _, f := range u.Files {
		for _, decl := range f.Decls {
			switch x := decl.(type) {
			case *ir.Struct:
				out = append(out, x.Name)
			case *ir.Union:
				out = append(out, x.Name)
			}
		}
	}
	return out
}
