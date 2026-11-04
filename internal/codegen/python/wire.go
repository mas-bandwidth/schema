package python

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// wire.go is the type wire both ways (docs/PORT-PYTHON.md, THE BIT LAYER):
// for every type and union, _s_w_<Name>(w, v, R) writes it and
// _s_r_<Name>(r, v, R) reads it, over the vendored serialize.py streams. There
// is no private bit writer or reader: every bit is serialize.py's, and the
// bytes are SPEC §4.3's, the ones the C++ target pins in testdata/wire.
//
// The shape is straight-line code with the stream's methods bound to locals
// once per function, the shape D1's measurement kept (docs/PORT-PYTHON.md,
// THE DECISIONS). One Ref, R, carries every scalar through a whole write or
// read.
//
// A write refuses a value outside its contract by raising: serialize.py's
// WriteMisuse for what the bit layer checks, and the same exception for what
// only the schema knows (a float field holding an int, an array of the wrong
// length, a nested value of the wrong class). Each function turns it into
// InvalidValue naming the field, and a nesting function prefixes its own
// field, so the caller sees Type.field.inner and the rule. The stream is the
// write's own and is dropped, so no byte of a refused value leaves.
//
// A read returns False at the first refusal: the stream latches serialize.py's
// reason, and a schema rule the bit layer does not know (const, reserved, the
// wide text rules) latches its reason through r.fail.

var (
	minInt32 = big.NewInt(math.MinInt32)
	maxInt32 = big.NewInt(math.MaxInt32)
	minInt64 = new(big.Int).SetInt64(math.MinInt64)
	maxInt64 = new(big.Int).SetInt64(math.MaxInt64)
)

func fitsInt32(lo, hi *big.Int) bool { return lo.Cmp(minInt32) >= 0 && hi.Cmp(maxInt32) <= 0 }
func fitsInt64(lo, hi *big.Int) bool { return lo.Cmp(minInt64) >= 0 && hi.Cmp(maxInt64) <= 0 }

func mask(bits int) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
}

// fn is one codec function under construction: its body, the stream
// methods it binds to locals, and its numbered temporaries (numbered so no
// local takes two types, which mypy refuses).
type fn struct {
	g     *gen
	body  strings.Builder
	binds map[string]bool
	tmp   int
	write bool
}

func (c *fn) pf(ind, format string, args ...any) {
	c.body.WriteString(ind)
	fmt.Fprintf(&c.body, format, args...)
	c.body.WriteString("\n")
}

// op binds the stream method serialize_<name> as a local and returns it.
func (c *fn) op(name string) string {
	c.binds[name] = true
	if c.write {
		return "w_" + name
	}
	return "r_" + name
}

func (c *fn) temp(prefix string) string {
	c.tmp++
	return fmt.Sprintf("%s%d", prefix, c.tmp)
}

// prelude binds every stream method the body uses, in a fixed order.
func (c *fn) prelude(ind string) string {
	names := make([]string, 0, len(c.binds))
	for n := range c.binds {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	stream, prefix := "r", "r_"
	if c.write {
		stream, prefix = "w", "w_"
	}
	for _, n := range names {
		fmt.Fprintf(&b, "%s%s%s = %s.serialize_%s\n", ind, prefix, n, stream, n)
	}
	return b.String()
}

// cfParams is the module constant holding a compressed float declaration's
// precomputed constants (_s_rt.compressed_float, serialize.py's float32
// arithmetic), one per distinct declaration.
func (g *gen) cfParams(f *ir.Field) string {
	line := fmt.Sprintf("_s_rt.compressed_float(%s, %s, %s)", pyFloat32(f.FMin), pyFloat32(f.FMax), pyFloat32(f.Resolution))
	for i, k := range g.consts {
		if strings.HasSuffix(k, "= "+line+"\n") {
			return fmt.Sprintf("_s_cf%d", i)
		}
	}
	name := fmt.Sprintf("_s_cf%d", len(g.consts))
	g.consts = append(g.consts, fmt.Sprintf("%s: _s_t.Final = %s\n", name, line))
	return name
}

// misuse raises the bit layer's exception for a rule only the schema knows;
// the enclosing function names the field.
func misuse(rule string) string {
	return fmt.Sprintf("raise _s_ser.WriteMisuse(%q)", rule)
}

// ---------------------------------------------------------------------------
// Write.

func (g *gen) emitWriteStruct(d *ir.Struct) error {
	c := &fn{g: g, binds: map[string]bool{}, write: true}
	if err := c.writeItems(d.Items, "        "); err != nil {
		return err
	}
	g.pf("def _s_w_%s(w: _s_ser.WriteStream, v: %s, R: _s_ser.Ref[_s_t.Any]) -> None:\n", d.Name, d.Name)
	g.writeFn(c)
	return nil
}

// writeFn emits a write function's prelude and its body inside the one try
// that names the refused field.
func (g *gen) writeFn(c *fn) {
	body := c.body.String()
	if body == "" {
		g.pf("    pass\n\n\n")
		return
	}
	g.pf("%s", c.prelude("    "))
	g.pf("    at = \"\"\n")
	g.pf("    try:\n")
	g.pf("%s", body)
	g.pf("    except _s_ser.WriteMisuse as e:\n")
	g.pf("        raise _s_rt.InvalidValue(at, str(e)) from None\n")
	g.pf("    except _s_rt.InvalidValue as e:\n")
	g.pf("        raise e.within(at) from None\n\n\n")
}

func (c *fn) writeItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			c.pf(ind, "at = %q", item.F.Name)
			if err := c.writeField("v."+item.F.Name, item.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			c.writeRawBits(ind, item.Value, item.Bits)
		case *ir.ReservedItem:
			c.writeRawBits(ind, big.NewInt(0), item.Bits)
		case *ir.AlignItem:
			c.pf(ind, "%s()", c.op("align"))
		case *ir.Branch:
			cond := "v." + item.Cond
			if item.Neg {
				cond = "not " + cond
			}
			c.pf(ind, "if %s:", cond)
			if len(item.Then) == 0 {
				c.pf(ind+"    ", "pass")
			}
			if err := c.writeItems(item.Then, ind+"    "); err != nil {
				return err
			}
			if item.Else != nil {
				c.pf(ind, "else:")
				if len(item.Else) == 0 {
					c.pf(ind+"    ", "pass")
				}
				if err := c.writeItems(item.Else, ind+"    "); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// writeRawBits writes a constant (const or reserved) low bits first, in
// groups of at most 64 bits.
func (c *fn) writeRawBits(ind string, v *big.Int, bits int64) {
	rest := new(big.Int).Set(v)
	for bits > 0 {
		n := min(bits, 64)
		chunk := new(big.Int).And(rest, mask(int(n)))
		c.pf(ind, "R.value = %s", chunk)
		c.pf(ind, "%s(R, %d)", c.op("bits"), n)
		rest.Rsh(rest, uint(n))
		bits -= n
	}
}

func (c *fn) writeField(val string, f *ir.Field, ind string) error {
	if f.Array == ir.ArrayNone {
		return c.writeScalar(val, f, ind)
	}
	xs := c.temp("xs")
	c.pf(ind, "%s = %s", xs, val)
	if f.Array == ir.ArrayCounted {
		c.pf(ind, "if type(%s) is not list:", xs)
		c.pf(ind+"    ", "%s", misuse("a counted array is a list"))
		c.pf(ind, "R.value = len(%s)", xs)
		c.pf(ind, "%s(R, %d, %d)", c.op("int"), f.ArrayMin, f.ArrayBound)
	} else {
		c.pf(ind, "if type(%s) is not list or len(%s) != %d:", xs, xs, f.ArrayBound)
		c.pf(ind+"    ", "%s", misuse(fmt.Sprintf("a [%d] array is a list of exactly %d elements", f.ArrayBound, f.ArrayBound)))
	}
	e := *f
	e.Array = ir.ArrayNone
	x := c.temp("x")
	c.pf(ind, "for %s in %s:", x, xs)
	return c.writeScalar(x, &e, ind+"    ")
}

// rangedOp is the serialize.py ranged-integer operation whose bounds hold
// [lo, hi]; every one writes value - lo in bits_required(lo, hi) bits.
func rangedOp(lo, hi *big.Int) string {
	switch {
	case fitsInt32(lo, hi):
		return "int"
	case fitsInt64(lo, hi):
		return "int64"
	}
	return "int128"
}

func (c *fn) writeScalar(val string, f *ir.Field, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		c.pf(ind, "R.value = %s", val)
		c.pf(ind, "%s(R)", c.op("bool"))
	case ir.TInt:
		if f.HasIntRange {
			c.pf(ind, "R.value = %s", val)
			c.pf(ind, "%s(R, %s, %s)", c.op(rangedOp(f.IntMin, f.IntMax)), f.IntMin, f.IntMax)
			return nil
		}
		switch {
		case !t.Signed && t.Width <= 64:
			c.pf(ind, "R.value = %s", val)
			c.pf(ind, "%s(R, %d)", c.op("bits"), t.Width)
		case !t.Signed && t.Width == 128:
			c.pf(ind, "R.value = %s", val)
			c.pf(ind, "%s(R)", c.op("uint128"))
		case t.Signed && t.Width <= 64:
			x := c.temp("x")
			half := new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1))
			c.pf(ind, "%s = %s", x, val)
			c.pf(ind, "if type(%s) is not int or not -%s <= %s < %s:", x, half, x, half)
			c.pf(ind+"    ", "%s", misuse(fmt.Sprintf("an int%d is an int in [-2^%d, 2^%d)", t.Width, t.Width-1, t.Width-1)))
			c.pf(ind, "R.value = %s & %s", x, mask(t.Width))
			c.pf(ind, "%s(R, %d)", c.op("bits"), t.Width)
		default:
			return fmt.Errorf("field %s: an unranged int%d is not carried on the packet wire", f.Name, t.Width)
		}
	case ir.TBits:
		if t.Width > 64 {
			return fmt.Errorf("field %s: bits(%d) is not carried on the packet wire", f.Name, t.Width)
		}
		c.pf(ind, "R.value = %s", val)
		c.pf(ind, "%s(R, %d)", c.op("bits"), t.Width)
	case ir.TFixed:
		c.pf(ind, "R.value = %s", val)
		c.pf(ind, "%s(R, %d, %d, %s, %s)", c.op("fixed"), t.IntBits, t.FracBits, f.IntMin, f.IntMax)
	case ir.TFloat32, ir.TFloat64:
		x := c.temp("x")
		c.pf(ind, "%s = %s", x, val)
		c.pf(ind, "if type(%s) is not float:", x)
		c.pf(ind+"    ", "%s", misuse("a float field holds a float"))
		c.pf(ind, "R.value = %s", x)
		switch {
		case t.Kind == ir.TFloat64:
			c.pf(ind, "%s(R)", c.op("double"))
		case f.HasFloatRange:
			k := c.g.cfParams(f)
			c.pf(ind, "%s(R, %s[0], %s[1], %s[2], %s[3])", c.op("compressed_float_precomputed"), k, k, k, k)
		default:
			c.pf(ind, "%s(R)", c.op("float"))
		}
	case ir.TString:
		c.pf(ind, "R.value = %s", val)
		c.pf(ind, "%s(R, %d)", c.op("string"), t.Size+1)
	case ir.TBytes:
		b := c.temp("b")
		c.pf(ind, "%s = _s_rt.packet_bytes(%s, %d)", b, val, t.Size)
		c.pf(ind, "R.value = len(%s)", b)
		c.pf(ind, "%s(R, 0, %d)", c.op("int"), t.Size)
		c.pf(ind, "R.value = %s", b)
		c.pf(ind, "%s(R, len(%s))", c.op("bytes"), b)
	case ir.TWString:
		u := c.temp("u")
		k := c.temp("k")
		c.pf(ind, "%s = _s_rt.wide_units(%s, %d)", u, val, t.Size)
		c.pf(ind, "R.value = len(%s)", u)
		c.pf(ind, "%s(R, 0, %d)", c.op("int"), t.Size)
		c.pf(ind, "for %s in %s:", k, u)
		c.pf(ind+"    ", "R.value = %s", k)
		c.pf(ind+"    ", "%s(R, 32)", c.op("bits"))
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			c.pf(ind, "R.value = %s", val)
			c.pf(ind, "%s(R, 0, %d)", c.op("int"), ref.Max)
		case *ir.Flags:
			if ref.WireBits == 0 {
				c.pf(ind, "if %s != 0:", val)
				c.pf(ind+"    ", "%s", misuse("a flags set with no bits on the wire holds 0"))
				return nil
			}
			c.pf(ind, "R.value = %s", val)
			c.pf(ind, "%s(R, %d)", c.op("bits"), ref.WireBits)
		case *ir.Struct, *ir.Union:
			x := c.temp("x")
			c.pf(ind, "%s = %s", x, val)
			c.pf(ind, "if not isinstance(%s, %s):", x, c.g.ref(t.Name))
			c.pf(ind+"    ", "%s", misuse(fmt.Sprintf("the value is not a %s", t.Name)))
			c.pf(ind, "%s(w, %s, R)", c.g.fn("_s_w_", t.Name), x)
		default:
			return fmt.Errorf("field %s: named type %s is not carried on the packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s: kind %d is not carried on the packet wire", f.Name, t.Kind)
	}
	return nil
}

func (g *gen) emitWriteUnion(d *ir.Union, arms []unionArm) error {
	c := &fn{g: g, binds: map[string]bool{}, write: true}
	ind := "        "
	c.pf(ind, "at = \"type\"")
	tag := c.temp("t")
	c.pf(ind, "%s = v.type", tag)
	c.pf(ind, "R.value = %s", tag)
	c.pf(ind, "%s(R, 0, %d)", c.op("int"), d.Max)
	for i, a := range arms {
		kw := "elif"
		if i == 0 {
			kw = "if"
		}
		c.pf(ind, "%s %s == %d:", kw, tag, a.tag)
		c.pf(ind+"    ", "at = %q", a.f.Name)
		if err := c.writeField("v."+a.f.Name, a.f, ind+"    "); err != nil {
			return err
		}
	}
	g.pf("def _s_w_%s(w: _s_ser.WriteStream, v: %s, R: _s_ser.Ref[_s_t.Any]) -> None:\n", d.Name, d.Name)
	g.writeFn(c)
	return nil
}

// ---------------------------------------------------------------------------
// Read.

func (g *gen) emitReadStruct(d *ir.Struct) error {
	c := &fn{g: g, binds: map[string]bool{}}
	if err := c.readItems(d.Items, "    "); err != nil {
		return err
	}
	g.pf("def _s_r_%s(r: _s_ser.ReadStream, v: %s, R: _s_ser.Ref[_s_t.Any]) -> bool:\n", d.Name, d.Name)
	g.pf("%s", c.prelude("    "))
	g.pf("%s", c.body.String())
	g.pf("    return True\n\n\n")
	return nil
}

func (c *fn) call(ind, expr string) {
	c.pf(ind, "if not %s:", expr)
	c.pf(ind+"    ", "return False")
}

func (c *fn) refuse(ind, cond, reason string) {
	c.pf(ind, "if %s:", cond)
	c.pf(ind+"    ", "return r.fail(_s_ser.Refusal.%s)", reason)
}

func (c *fn) readItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if err := c.readField("v."+item.F.Name, item.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			c.readRawBits(ind, item.Value, item.Bits)
		case *ir.ReservedItem:
			c.readRawBits(ind, big.NewInt(0), item.Bits)
		case *ir.AlignItem:
			c.call(ind, c.op("align")+"()")
		case *ir.Branch:
			cond := "v." + item.Cond
			if item.Neg {
				cond = "not " + cond
			}
			c.pf(ind, "if %s:", cond)
			c.pf(ind+"    ", "pass")
			if err := c.readItems(item.Then, ind+"    "); err != nil {
				return err
			}
			c.zeroItems(item.Else, ind+"    ")
			c.pf(ind, "else:")
			c.pf(ind+"    ", "pass")
			if err := c.readItems(item.Else, ind+"    "); err != nil {
				return err
			}
			c.zeroItems(item.Then, ind+"    ")
		}
	}
	return nil
}

// zeroItems sets the untaken side of a branch to its zero values (SPEC §5:
// zero values, not the declared defaults).
func (c *fn) zeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			c.pf(ind, "v.%s = %s", item.F.Name, c.g.zeroExpr(item.F))
		case *ir.Branch:
			c.zeroItems(item.Then, ind)
			c.zeroItems(item.Else, ind)
		}
	}
}

// readRawBits reads a constant (const or reserved) and refuses any other
// value (SPEC §4.3).
func (c *fn) readRawBits(ind string, v *big.Int, bits int64) {
	rest := new(big.Int).Set(v)
	for bits > 0 {
		n := min(bits, 64)
		chunk := new(big.Int).And(rest, mask(int(n)))
		c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("bits"), n))
		c.refuse(ind, fmt.Sprintf("R.value != %s", chunk), "VALUE_OUT_OF_RANGE")
		rest.Rsh(rest, uint(n))
		bits -= n
	}
}

func (c *fn) readField(dst string, f *ir.Field, ind string) error {
	if f.Array == ir.ArrayNone {
		if isComposite(f.Type) {
			c.call(ind, fmt.Sprintf("%s(r, %s, R)", c.g.fn("_s_r_", f.Type.Name), dst))
			return nil
		}
		return c.readScalar(f, ind, func(expr string) { c.pf(ind, "%s = %s", dst, expr) })
	}
	count := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		c.call(ind, fmt.Sprintf("%s(R, %d, %d)", c.op("int"), f.ArrayMin, f.ArrayBound))
		count = c.temp("n")
		c.pf(ind, "%s = R.value", count)
	}
	e := *f
	e.Array = ir.ArrayNone
	xs := c.temp("xs")
	c.pf(ind, "%s: list[%s] = []", xs, c.g.pyElemType(f.Type))
	c.pf(ind, "for _ in range(%s):", count)
	in := ind + "    "
	if isComposite(f.Type) {
		el := c.temp("e")
		c.pf(in, "%s = %s()", el, c.g.ref(f.Type.Name))
		c.call(in, fmt.Sprintf("%s(r, %s, R)", c.g.fn("_s_r_", f.Type.Name), el))
		c.pf(in, "%s.append(%s)", xs, el)
	} else if err := c.readScalar(&e, in, func(expr string) { c.pf(in, "%s.append(%s)", xs, expr) }); err != nil {
		return err
	}
	c.pf(ind, "%s = %s", dst, xs)
	return nil
}

func (c *fn) readScalar(f *ir.Field, ind string, assign func(string)) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		c.call(ind, c.op("bool")+"(R)")
		assign("R.value")
	case ir.TInt:
		if f.HasIntRange {
			c.call(ind, fmt.Sprintf("%s(R, %s, %s)", c.op(rangedOp(f.IntMin, f.IntMax)), f.IntMin, f.IntMax))
			assign("R.value")
			return nil
		}
		switch {
		case !t.Signed && t.Width <= 64:
			c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("bits"), t.Width))
			assign("R.value")
		case !t.Signed && t.Width == 128:
			c.call(ind, c.op("uint128")+"(R)")
			assign("R.value")
		case t.Signed && t.Width <= 64:
			c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("bits"), t.Width))
			x := c.temp("x")
			c.pf(ind, "%s = R.value", x)
			half := new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1))
			assign(fmt.Sprintf("%s - %s if %s >= %s else %s", x, new(big.Int).Lsh(half, 1), x, half, x))
		default:
			return fmt.Errorf("field %s: an unranged int%d is not carried on the packet wire", f.Name, t.Width)
		}
	case ir.TBits:
		c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("bits"), t.Width))
		assign("R.value")
	case ir.TFixed:
		c.call(ind, fmt.Sprintf("%s(R, %d, %d, %s, %s)", c.op("fixed"), t.IntBits, t.FracBits, f.IntMin, f.IntMax))
		assign("R.value")
	case ir.TFloat32:
		if f.HasFloatRange {
			k := c.g.cfParams(f)
			c.call(ind, fmt.Sprintf("%s(R, %s[0], %s[1], %s[2], %s[3])", c.op("compressed_float_precomputed"), k, k, k, k))
		} else {
			c.call(ind, c.op("float")+"(R)")
		}
		assign("R.value")
	case ir.TFloat64:
		c.call(ind, c.op("double")+"(R)")
		assign("R.value")
	case ir.TString:
		c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("string"), t.Size+1))
		assign("R.value")
	case ir.TBytes:
		c.call(ind, fmt.Sprintf("%s(R, 0, %d)", c.op("int"), t.Size))
		c.call(ind, fmt.Sprintf("%s(R, R.value)", c.op("bytes")))
		assign("R.value")
	case ir.TWString:
		c.call(ind, fmt.Sprintf("%s(R, 0, %d)", c.op("int"), t.Size))
		u := c.temp("u")
		s := c.temp("s")
		c.pf(ind, "%s: list[int] = []", u)
		c.pf(ind, "for _ in range(R.value):")
		c.call(ind+"    ", fmt.Sprintf("%s(R, 32)", c.op("bits")))
		c.refuse(ind+"    ", "R.value > 0xFFFF", "VALUE_OUT_OF_RANGE")
		c.pf(ind+"    ", "%s.append(R.value)", u)
		c.pf(ind, "%s = _s_rt.wide_text(%s)", s, u)
		c.refuse(ind, s+" is None", "INVALID_UTF8")
		assign(s)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			c.call(ind, fmt.Sprintf("%s(R, 0, %d)", c.op("int"), ref.Max))
			assign("R.value")
		case *ir.Flags:
			if ref.WireBits == 0 {
				assign("0")
				return nil
			}
			c.call(ind, fmt.Sprintf("%s(R, %d)", c.op("bits"), ref.WireBits))
			assign("R.value")
		default:
			return fmt.Errorf("field %s: named type %s is not carried on the packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s: kind %d is not carried on the packet wire", f.Name, t.Kind)
	}
	return nil
}

func (g *gen) emitReadUnion(d *ir.Union, arms []unionArm) error {
	c := &fn{g: g, binds: map[string]bool{}}
	ind := "    "
	c.call(ind, fmt.Sprintf("%s(R, 0, %d)", c.op("int"), d.Max))
	tag := c.temp("t")
	c.pf(ind, "%s = R.value", tag)
	c.pf(ind, "v.type = %s", tag)
	for i, a := range arms {
		kw := "elif"
		if i == 0 {
			kw = "if"
		}
		c.pf(ind, "%s %s == %d:", kw, tag, a.tag)
		if err := c.readField("v."+a.f.Name, a.f, ind+"    "); err != nil {
			return err
		}
	}
	g.pf("def _s_r_%s(r: _s_ser.ReadStream, v: %s, R: _s_ser.Ref[_s_t.Any]) -> bool:\n", d.Name, d.Name)
	g.pf("%s", c.prelude("    "))
	g.pf("%s", c.body.String())
	g.pf("    return True\n\n\n")
	return nil
}
