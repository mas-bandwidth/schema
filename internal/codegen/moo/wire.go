// wire.go is the type wire both ways (docs/PORT-MOO.md, THE WIRE): for every
// packet type and union, Write<Name>(stream, value) and Read<Name>(stream,
// value) against the vendored serialize.moo streams. There is no private bit
// writer or reader: every bit is the runtime's. The bytes are SPEC §4.3's, the
// same ones the C++ target pins in testdata/wire/*.bin.
//
// The error idiom is the §6.3 bool early-out. A value outside its declared
// contract (a wrong storage type, an out-of-range integer, a non-finite float
// pattern, malformed text) makes a write return false BEFORE anything of it
// rides, unlatched, as the JavaScript and Go legs do; serialize.moo itself
// would trap such a write (IMPLEMENTING.md U9), and the guards keep the trap
// for real misuse of the stream (a buffer too small). A read refuses through
// the stream's one latch (stream:error() names the reason) or, for a schema
// rule the runtime does not know (const, reserved, wstring validation), by
// returning false unlatched.
package moo

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	minInt32 = big.NewInt(math.MinInt32)
	maxInt32 = big.NewInt(math.MaxInt32)
	minInt64 = new(big.Int).SetInt64(math.MinInt64)
	maxInt64 = new(big.Int).SetInt64(math.MaxInt64)
)

func fitsInt32(lo, hi *big.Int) bool { return lo.Cmp(minInt32) >= 0 && hi.Cmp(maxInt32) <= 0 }
func fitsInt64(lo, hi *big.Int) bool { return lo.Cmp(minInt64) >= 0 && hi.Cmp(maxInt64) <= 0 }

// call emits the C++-style early-out around one stream call.
func (g *gen) call(ind, expr, comment string) {
	if comment != "" {
		comment = " -- " + comment
	}
	g.pf("%sif not %s then%s\n%s\treturn false\n%send\n", ind, expr, comment, ind, ind)
}

// refuse emits a guard that returns false, unlatched, when cond holds.
func (g *gen) refuse(ind, cond, comment string) {
	if comment != "" {
		comment = " -- " + comment
	}
	g.pf("%sif %s then%s\n%s\treturn false\n%send\n", ind, cond, comment, ind, ind)
}

// ---------------------------------------------------------------------------
// Write.

func (g *gen) emitWriteStruct(d *ir.Struct) error {
	g.pf("-- Write%s writes the type wire (SPEC §4.3, §6.1: the declared order).\n", d.Name)
	g.pf("function M.Write%s(stream: any, value: %s): boolean\n", d.Name, d.Name)
	if err := g.writeItems(d.Items, "\t"); err != nil {
		return err
	}
	g.pf("\treturn true\nend\n\n")
	return nil
}

func (g *gen) writeItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if err := g.writeField("value."+ir.GoExportName(item.F.Name), item.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			g.writeRawBits(ind, item.Value, item.Bits, fmt.Sprintf("const(%s, %d) — SPEC §4.3", item.Value, item.Bits))
		case *ir.ReservedItem:
			g.writeRawBits(ind, big.NewInt(0), item.Bits, fmt.Sprintf("reserved(%d): zeros on the wire", item.Bits))
		case *ir.AlignItem:
			g.call(ind, "stream:serializeAlign()", "")
		case *ir.Branch:
			cond := "value." + ir.GoExportName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			g.pf("%sif %s then\n", ind, cond)
			if err := g.writeItems(item.Then, ind+"\t"); err != nil {
				return err
			}
			if item.Else != nil {
				g.pf("%selse\n", ind)
				if err := g.writeItems(item.Else, ind+"\t"); err != nil {
					return err
				}
			}
			g.pf("%send\n", ind)
		}
	}
	return nil
}

func (g *gen) writeRawBits(ind string, v *big.Int, bits int64, comment string) {
	if bits > 32 {
		g.pf("%sWIDE.value = %s\n", ind, g.k(wideNew("Uint64", v, 64)))
		g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", bits), comment)
		return
	}
	g.pf("%sNUM.value = %s\n", ind, v)
	g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", bits), comment)
}

// writeField writes one field with its array framing (SPEC §4.6, §4.7).
func (g *gen) writeField(val string, f *ir.Field, ind string) error {
	if f.Array == ir.ArrayNone {
		return g.writeScalar(val, val+"Length", f, ind)
	}
	limit := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		g.refuse(ind, fmt.Sprintf("not isInt(%s, %d, %d)", count, f.ArrayMin, f.ArrayBound), "the count guards the loop (SPEC §6.3)")
		g.pf("%sNUM.value = %s\n", ind, count)
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, %d, %d)", f.ArrayMin, f.ArrayBound), "the count (SPEC §4.6)")
		limit = count
	}
	e := *f
	e.Array = ir.ArrayNone
	g.pf("%sfor i = 1, %s do\n", ind, limit)
	var err error
	switch {
	case isByteElem(f):
		err = g.writeScalar(fmt.Sprintf("buffer.readu8(%s, i - 1)", val), "", &e, ind+"\t")
	case isBuffer(f.Type):
		err = g.writeScalar(val+"[i]", val+"Length[i]", &e, ind+"\t")
	default:
		err = g.writeScalar(val+"[i]", "", &e, ind+"\t")
	}
	g.pf("%send\n", ind)
	return err
}

// writeScalar writes one value of a field's kind (SPEC §4.3's table).
func (g *gen) writeScalar(val, length string, f *ir.Field, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		g.refuse(ind, fmt.Sprintf("type(%s) ~= \"boolean\"", val), "")
		g.pf("%sBOOL.value = %s\n", ind, val)
		g.call(ind, "stream:serializeBool(BOOL)", "")
	case ir.TInt:
		g.writeInt(val, f, ind)
	case ir.TBits:
		if t.Width > 32 {
			g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint64, %s, %s, %s)", val,
				g.k(wideNew("Uint64", big.NewInt(0), 64)), g.k(wideNew("Uint64", mask(t.Width), 64))), "")
			g.pf("%sWIDE.value = %s\n", ind, val)
			g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", t.Width), "")
		} else {
			g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, %s)", val, mask(t.Width)), "")
			g.pf("%sNUM.value = %s\n", ind, val)
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "")
		}
	case ir.TFixed:
		g.writeFixed(val, f, ind)
	case ir.TFloat32:
		g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, 0xFFFFFFFF) or bit32.band(%s, 0x7F800000) == 0x7F800000", val, val),
			"a float32 is its finite binary32 pattern (SPEC §4.3)")
		g.pf("%sNUM.value = %s\n", ind, val)
		if f.HasFloatRange {
			g.call(ind, g.compressedCall(f), "the step index (SPEC §4.3)")
		} else {
			g.call(ind, "stream:serializeFloatBits(NUM)", "")
		}
	case ir.TFloat64:
		g.refuse(ind, fmt.Sprintf("not serialize.Uint64.is(%s) or bit32.band(%s[2], 0x7FF00000) == 0x7FF00000", val, val),
			"a float64 is its finite binary64 pattern, a Uint64 (SPEC §4.3)")
		g.pf("%sWIDE.value = %s\n", ind, val)
		g.call(ind, "stream:serializeDoubleBits(WIDE)", "")
	case ir.TString:
		g.refuse(ind, fmt.Sprintf("type(%s) ~= \"string\" or #%s > %d or not validText(%s)", val, val, t.Size, val),
			"well-formed UTF-8, no zero byte, at most N bytes (SPEC §4.7)")
		g.pf("%sSTR.value = %s\n", ind, val)
		g.call(ind, fmt.Sprintf("stream:serializeString(STR, %d)", t.Size+1), "length, align, the bytes (SPEC §4.7)")
	case ir.TBytes:
		g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, %d) or buffer.len(%s) < %s", length, t.Size, val, length),
			"the length guards the slice (SPEC §6.3)")
		g.pf("%sNUM.value = %s\n", ind, length)
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", t.Size), "")
		g.call(ind, fmt.Sprintf("stream:serializeBytes(%s, 0, %s)", val, length), "align, then the used bytes (SPEC §4.7)")
	case ir.TWString:
		g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, %d) or buffer.len(%s) < %s * 2 or not validWide(%s, %s)", length, t.Size, val, length, val, length),
			"UTF-16, no zero unit, surrogates paired (SPEC §4.12)")
		g.pf("%sNUM.value = %s\n", ind, length)
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", t.Size), "the length, no alignment (SPEC §4.12)")
		g.pf("%sfor u = 0, %s - 1 do\n", ind, length)
		g.pf("%s\tNUM.value = buffer.readu16(%s, u * 2)\n", ind, val)
		g.call(ind+"\t", "stream:serializeBits(NUM, 32)", "one 32-bit group per code unit")
		g.pf("%send\n", ind)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, %d)", val, ref.Max), "")
			g.pf("%sNUM.value = %s\n", ind, val)
			g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", ref.Max), "")
		case *ir.Flags:
			g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint64, %s, %s, %s)", val,
				g.k(wideNew("Uint64", big.NewInt(0), 64)), g.k(wideNew("Uint64", mask(ref.WireBits), 64))), "")
			if ref.WireBits > 0 {
				g.pf("%sWIDE.value = %s\n", ind, val)
				g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", ref.WireBits), "the flags wire width")
			}
		case *ir.Struct, *ir.Union:
			g.call(ind, fmt.Sprintf("%s.Write%s(stream, %s)", g.mod(t.Name), t.Name, val), "")
		default:
			return fmt.Errorf("field %s: named type %s is not carried on the packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s: kind %d is not carried on the packet wire", f.Name, t.Kind)
	}
	return nil
}

func mask(bits int) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
}

// compressedCall is the precomputed compressed float call: the declaration's
// float32 constants and step parameters come from serialize.compressedFloatParams
// once, at module load (the runtime's own float32 arithmetic, so the steps
// are every port's).
func (g *gen) compressedCall(f *ir.Field) string {
	min := g.k(fmt.Sprintf("serialize.Float32.fromBits(0x%08x)", float32Bits(f.FMin)))
	max := g.k(fmt.Sprintf("serialize.Float32.fromBits(0x%08x)", float32Bits(f.FMax)))
	res := g.k(fmt.Sprintf("serialize.Float32.fromBits(0x%08x)", float32Bits(f.Resolution)))
	params := g.k(fmt.Sprintf("table.freeze(table.pack(serialize.compressedFloatParams(%s, %s, %s)))", min, max, res))
	return fmt.Sprintf("stream:serializeCompressedFloatPrecomputedBits(NUM, %s[1], %s[2], %s[3], %s)", params, params, params, min)
}

func (g *gen) writeInt(val string, f *ir.Field, ind string) {
	t := f.Type
	c := wideClass(f)
	if !f.HasIntRange {
		switch {
		case c == "Uint128":
			g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint128, %s)", val), "")
			g.pf("%sWIDE.value = %s\n", ind, val)
			g.call(ind, "stream:serializeUint128(WIDE)", "uint128 raw (SPEC §4.3)")
		case c == "Uint64":
			g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint64, %s)", val), "")
			g.pf("%sWIDE.value = %s\n", ind, val)
			g.call(ind, "stream:serializeUint64(WIDE)", "")
		case c == "Int64":
			g.refuse(ind, fmt.Sprintf("not isWide(serialize.Int64, %s)", val), "")
			g.pf("%sWIDE.value = serialize.Int64.toUint64(%s)\n", ind, val)
			g.call(ind, "stream:serializeBits64(WIDE, 64)", "two's complement, the storage width (SPEC §4.3)")
		case t.Signed:
			half := new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1))
			g.refuse(ind, fmt.Sprintf("not isInt(%s, -%s, %s)", val, half, new(big.Int).Sub(half, big.NewInt(1))), "")
			g.pf("%sNUM.value = if %s < 0 then %s + %s else %s\n", ind, val, val, new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), val)
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "two's complement (SPEC §4.3)")
		default:
			g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, %s)", val, mask(t.Width)), "")
			g.pf("%sNUM.value = %s\n", ind, val)
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "")
		}
		return
	}
	lo, hi := f.IntMin, f.IntMax
	switch {
	case c == "Int128" || c == "Uint128":
		cls := "Int128"
		klo, khi := g.k(wideNew(cls, lo, 128)), g.k(wideNew(cls, hi, 128))
		g.refuse(ind, fmt.Sprintf("not isWide(serialize.%s, %s, %s, %s)", c, val, g.k(wideNew(c, lo, 128)), g.k(wideNew(c, hi, 128))), "")
		v := val
		if c == "Uint128" {
			v = fmt.Sprintf("serialize.Uint128.toInt128(%s)", val)
		}
		g.pf("%sWIDE.value = %s\n", ind, v)
		g.call(ind, fmt.Sprintf("stream:serializeInt128(WIDE, %s, %s)", klo, khi), "int128, offset from min (SPEC §4.3)")
	case fitsInt32(lo, hi) && c == "":
		g.refuse(ind, fmt.Sprintf("not isInt(%s, %s, %s)", val, lo, hi), "")
		g.pf("%sNUM.value = %s\n", ind, val)
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, %s, %s)", lo, hi), "int, offset from min (SPEC §4.3)")
	case c == "":
		// a 32-bit storage whose range does not fit int32 (a full uint32):
		// value - min in bits_required(min, max) bits, exact in a moo number
		g.refuse(ind, fmt.Sprintf("not isInt(%s, %s, %s)", val, lo, hi), "")
		if bits := ir.BitsRequired(lo, hi); bits > 0 {
			g.pf("%sNUM.value = %s - %s\n", ind, val, paren(lo))
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", bits), "the offset from min (SPEC §4.3)")
		}
	default:
		cls := c
		g.refuse(ind, fmt.Sprintf("not isWide(serialize.%s, %s, %s, %s)", cls, val, g.k(wideNew(cls, lo, 64)), g.k(wideNew(cls, hi, 64))), "")
		bits := ir.BitsRequired(lo, hi)
		if bits == 0 {
			return
		}
		if fitsInt64(lo, hi) {
			v := val
			if cls == "Uint64" {
				v = fmt.Sprintf("serialize.Uint64.toInt64(%s)", val)
				g.pf("%sWIDE.value = if type(%s) == \"number\" then %s else %s\n", ind, val, val, v)
			} else {
				g.pf("%sWIDE.value = %s\n", ind, v)
			}
			g.call(ind, fmt.Sprintf("stream:serializeInt64(WIDE, %s, %s)", g.k(wideNew("Int64", lo, 64)), g.k(wideNew("Int64", hi, 64))), "int64, offset from min (SPEC §4.3)")
			return
		}
		g.pf("%sWIDE.value = serialize.Uint64.sub(%s, %s)\n", ind, val, g.k(wideNew("Uint64", lo, 64)))
		g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", bits), "the offset from min (SPEC §4.3)")
	}
}

func paren(v *big.Int) string {
	if v.Sign() < 0 {
		return "(" + v.String() + ")"
	}
	return v.String()
}

// rawBounds is a fixed field's raw range, (A << F, B << F).
func rawBounds(f *ir.Field) (*big.Int, *big.Int) {
	F := uint(f.Type.FracBits)
	return new(big.Int).Lsh(f.IntMin, F), new(big.Int).Lsh(f.IntMax, F)
}

func (g *gen) writeFixed(val string, f *ir.Field, ind string) {
	t := f.Type
	lo, hi := f.IntMin, f.IntMax
	rlo, rhi := rawBounds(f)
	if fixedIsNumber(f) {
		g.refuse(ind, fmt.Sprintf("type(%s) ~= \"number\" or %s < %s or %s > %s", val, val, lo, val, hi), "whole units, moo's own Q48.16")
		g.pf("%sNUM.value = %s\n", ind, val)
		g.call(ind, fmt.Sprintf("stream:serializeFixedNumber(NUM, %s, %s)", lo, hi), "the Q format's raw offset (SPEC §4.3)")
		return
	}
	klo, khi := g.k(wideNew("Int64", lo, 64)), g.k(wideNew("Int64", hi, 64))
	fn := "serializeFixed64"
	if t.Width == 128 {
		fn = "serializeFixed128"
	}
	call := fmt.Sprintf("stream:%s(WIDE, %d, %d, %s, %s)", fn, t.IntBits, t.FracBits, klo, khi)
	switch c := wideClass(f); c {
	case "":
		g.refuse(ind, fmt.Sprintf("not isInt(%s, %s, %s)", val, rlo, rhi), "the raw value at its storage width")
		g.pf("%sWIDE.value = %s\n", ind, val)
	case "Int64", "Int128":
		g.refuse(ind, fmt.Sprintf("not isWide(serialize.%s, %s, %s, %s)", c, val, g.k(wideNew(c, rlo, classWidth(c))), g.k(wideNew(c, rhi, classWidth(c)))), "the raw value")
		g.pf("%sWIDE.value = %s\n", ind, val)
	case "Uint64":
		g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint64, %s, %s, %s)", val, g.k(wideNew(c, rlo, 64)), g.k(wideNew(c, rhi, 64))), "the raw value")
		g.pf("%sWIDE.value = serialize.Uint64.toInt64(if type(%s) == \"number\" then serialize.Uint64.fromNumber(%s) else %s)\n", ind, val, val, val)
	case "Uint128":
		g.refuse(ind, fmt.Sprintf("not isWide(serialize.Uint128, %s, %s, %s)", val, g.k(wideNew(c, rlo, 128)), g.k(wideNew(c, rhi, 128))), "the raw value")
		g.pf("%sWIDE.value = serialize.Uint128.toInt128(if type(%s) == \"number\" then serialize.Uint128.fromNumber(%s) else %s)\n", ind, val, val, val)
	}
	g.call(ind, call, "the Q format's raw offset (SPEC §4.3)")
}

func (g *gen) emitWriteUnion(d *ir.Union) error {
	g.pf("-- Write%s: the tag in minimal bits for [0, %d], validated before it rides,\n", d.Name, d.Max)
	g.pf("-- then the selected arm only (SPEC §4.8).\n")
	g.pf("function M.Write%s(stream: any, value: %s): boolean\n", d.Name, d.Name)
	g.refuse("\t", fmt.Sprintf("not isInt(value.Type, 0, %d)", d.Max), "the tag validates before it rides (SPEC §4.8)")
	g.pf("\tNUM.value = value.Type\n")
	g.call("\t", fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", d.Max), "")
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		g.pf("\tif value.Type == %d then\n", i+1)
		if err := g.writeField("value."+ir.GoExportName(v.Name), armField(v), "\t\t"); err != nil {
			return err
		}
		g.pf("\tend\n")
	}
	g.pf("\treturn true -- None, or a payload-free arm: the tag is the whole wire\nend\n\n")
	return nil
}

// ---------------------------------------------------------------------------
// Read.

func (g *gen) emitReadStruct(d *ir.Struct) error {
	g.pf("-- Read%s reads the type wire; false refuses (stream:error() names a runtime\n", d.Name)
	g.pf("-- refusal; a schema rule's refusal returns false unlatched).\n")
	g.pf("function M.Read%s(stream: any, value: %s): boolean\n", d.Name, d.Name)
	if err := g.readItems(d.Items, "\t"); err != nil {
		return err
	}
	g.pf("\treturn true\nend\n\n")
	return nil
}

func (g *gen) readItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if err := g.readField("value."+ir.GoExportName(item.F.Name), item.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			g.readRawBits(ind, item.Value, item.Bits, fmt.Sprintf("const(%s, %d): a read rejects any other value (SPEC §4.3)", item.Value, item.Bits))
		case *ir.ReservedItem:
			g.readRawBits(ind, big.NewInt(0), item.Bits, fmt.Sprintf("reserved(%d): a read rejects nonzero (SPEC §4.3)", item.Bits))
		case *ir.AlignItem:
			g.call(ind, "stream:serializeAlign()", "rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "not "
			}
			g.pf("%sif %svalue.%s then\n", ind, neg, ir.GoExportName(item.Cond))
			if err := g.readItems(item.Then, ind+"\t"); err != nil {
				return err
			}
			g.zeroItems(item.Else, ind+"\t")
			g.pf("%selse\n", ind)
			if err := g.readItems(item.Else, ind+"\t"); err != nil {
				return err
			}
			g.zeroItems(item.Then, ind+"\t")
			g.pf("%send\n", ind)
		}
	}
	return nil
}

// zeroItems resets the untaken side of a branch to its zero form (SPEC §5).
func (g *gen) zeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitReset(item.F, "value", ind, false)
		case *ir.Branch:
			g.zeroItems(item.Then, ind)
			g.zeroItems(item.Else, ind)
		}
	}
}

func (g *gen) readRawBits(ind string, v *big.Int, bits int64, comment string) {
	if bits > 32 {
		g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", bits), "")
		g.refuse(ind, fmt.Sprintf("serialize.Uint64.cmp(WIDE.value, %s) ~= 0", g.k(wideNew("Uint64", v, 64))), comment)
		return
	}
	g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", bits), "")
	g.refuse(ind, fmt.Sprintf("NUM.value ~= %s", v), comment)
}

func (g *gen) readField(val string, f *ir.Field, ind string) error {
	if f.Array == ir.ArrayNone {
		return g.readScalar(val, val+"Length", f, ind)
	}
	limit := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, %d, %d)", f.ArrayMin, f.ArrayBound), "the count guards the loop (SPEC §6.3)")
		g.pf("%s%sCount = NUM.value\n", ind, val)
		limit = val + "Count"
	}
	e := *f
	e.Array = ir.ArrayNone
	g.pf("%sfor i = 1, %s do\n", ind, limit)
	var err error
	switch {
	case isByteElem(f):
		g.call(ind+"\t", "stream:serializeBits(NUM, 8)", "")
		g.pf("%s\tbuffer.writeu8(%s, i - 1, NUM.value)\n", ind, val)
	case isBuffer(f.Type):
		err = g.readScalar(val+"[i]", val+"Length[i]", &e, ind+"\t")
	default:
		err = g.readScalar(val+"[i]", "", &e, ind+"\t")
	}
	g.pf("%send\n", ind)
	return err
}

func (g *gen) readScalar(val, length string, f *ir.Field, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		g.call(ind, "stream:serializeBool(BOOL)", "")
		g.pf("%s%s = BOOL.value\n", ind, val)
	case ir.TInt:
		g.readInt(val, f, ind)
	case ir.TBits:
		if t.Width > 32 {
			g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", t.Width), "")
			g.pf("%s%s = WIDE.value\n", ind, val)
		} else {
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "")
			g.pf("%s%s = NUM.value\n", ind, val)
		}
	case ir.TFixed:
		g.readFixed(val, f, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.call(ind, g.compressedCall(f), "refuses a step above the count (SPEC §4.3)")
		} else {
			g.call(ind, "stream:serializeFloatBits(NUM)", "refuses NaN and the infinities (SPEC §4.3)")
		}
		g.pf("%s%s = NUM.value\n", ind, val)
	case ir.TFloat64:
		g.call(ind, "stream:serializeDoubleBits(WIDE)", "refuses NaN and the infinities (SPEC §4.3)")
		g.pf("%s%s = WIDE.value\n", ind, val)
	case ir.TString:
		g.call(ind, fmt.Sprintf("stream:serializeString(STR, %d)", t.Size+1), "refuses a zero byte, then malformed UTF-8 (SPEC §4.7)")
		g.pf("%s%s = STR.value\n", ind, val)
	case ir.TBytes:
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", t.Size), "the length guards the payload (SPEC §6.3)")
		g.pf("%s%s = NUM.value\n", ind, length)
		g.call(ind, fmt.Sprintf("stream:serializeBytes(%s, 0, %s)", val, length), "")
	case ir.TWString:
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", t.Size), "the length bounds the loop (SPEC §4.12)")
		g.pf("%s%s = NUM.value\n", ind, length)
		g.pf("%sfor u = 0, %s - 1 do\n", ind, length)
		g.call(ind+"\t", "stream:serializeBits(NUM, 32)", "")
		g.refuse(ind+"\t", "NUM.value > 0xFFFF", "a group above 0xFFFF is not a code unit (SPEC §4.12)")
		g.pf("%s\tbuffer.writeu16(%s, u * 2, NUM.value)\n", ind, val)
		g.pf("%send\n", ind)
		g.refuse(ind, fmt.Sprintf("not validWide(%s, %s)", val, length), "no zero unit, surrogates paired (SPEC §4.12)")
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", ref.Max), "rejects a value above max (SPEC §4.3)")
			g.pf("%s%s = NUM.value\n", ind, val)
		case *ir.Flags:
			if ref.WireBits == 0 {
				g.pf("%s%s = %s\n", ind, val, g.k(wideNew("Uint64", big.NewInt(0), 64)))
				return nil
			}
			g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", ref.WireBits), "")
			g.pf("%s%s = WIDE.value\n", ind, val)
		case *ir.Struct, *ir.Union:
			g.call(ind, fmt.Sprintf("%s.Read%s(stream, %s)", g.mod(t.Name), t.Name, val), "")
		default:
			return fmt.Errorf("field %s: named type %s is not carried on the packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s: kind %d is not carried on the packet wire", f.Name, t.Kind)
	}
	return nil
}

func (g *gen) readInt(val string, f *ir.Field, ind string) {
	t := f.Type
	c := wideClass(f)
	if !f.HasIntRange {
		switch {
		case c == "Uint128":
			g.call(ind, "stream:serializeUint128(WIDE)", "")
			g.pf("%s%s = WIDE.value\n", ind, val)
		case c == "Uint64":
			g.call(ind, "stream:serializeUint64(WIDE)", "")
			g.pf("%s%s = WIDE.value\n", ind, val)
		case c == "Int64":
			g.call(ind, "stream:serializeBits64(WIDE, 64)", "")
			g.pf("%s%s = serialize.Uint64.toInt64(WIDE.value)\n", ind, val)
		case t.Signed:
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "")
			half := new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1))
			g.pf("%s%s = if NUM.value >= %s then NUM.value - %s else NUM.value\n", ind, val, half, new(big.Int).Lsh(half, 1))
		default:
			g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", t.Width), "")
			g.pf("%s%s = NUM.value\n", ind, val)
		}
		return
	}
	lo, hi := f.IntMin, f.IntMax
	switch {
	case c == "Int128" || c == "Uint128":
		g.call(ind, fmt.Sprintf("stream:serializeInt128(WIDE, %s, %s)", g.k(wideNew("Int128", lo, 128)), g.k(wideNew("Int128", hi, 128))), "rejects above max (SPEC §4.3)")
		if c == "Uint128" {
			g.pf("%s%s = serialize.Int128.toUint128(WIDE.value)\n", ind, val)
		} else {
			g.pf("%s%s = WIDE.value\n", ind, val)
		}
	case fitsInt32(lo, hi) && c == "":
		g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, %s, %s)", lo, hi), "rejects out of range (SPEC §4.3)")
		g.pf("%s%s = NUM.value\n", ind, val)
	case c == "":
		bits := ir.BitsRequired(lo, hi)
		if bits == 0 {
			g.pf("%s%s = %s\n", ind, val, lo)
			return
		}
		g.call(ind, fmt.Sprintf("stream:serializeBits(NUM, %d)", bits), "")
		if diff := new(big.Int).Sub(hi, lo); diff.Cmp(mask(int(bits))) != 0 {
			g.refuse(ind, fmt.Sprintf("NUM.value > %s", diff), "a read rejects out of range (SPEC §4.3)")
		}
		g.pf("%s%s = NUM.value + %s\n", ind, val, paren(lo))
	default:
		bits := ir.BitsRequired(lo, hi)
		if bits == 0 {
			g.pf("%s%s = %s\n", ind, val, g.k(wideNew(c, lo, 64)))
			return
		}
		if fitsInt64(lo, hi) {
			g.call(ind, fmt.Sprintf("stream:serializeInt64(WIDE, %s, %s)", g.k(wideNew("Int64", lo, 64)), g.k(wideNew("Int64", hi, 64))), "rejects out of range (SPEC §4.3)")
			if c == "Uint64" {
				g.pf("%s%s = serialize.Int64.toUint64(WIDE.value)\n", ind, val)
			} else {
				g.pf("%s%s = WIDE.value\n", ind, val)
			}
			return
		}
		g.call(ind, fmt.Sprintf("stream:serializeBits64(WIDE, %d)", bits), "")
		if diff := new(big.Int).Sub(hi, lo); diff.Cmp(mask(int(bits))) != 0 {
			g.refuse(ind, fmt.Sprintf("serialize.Uint64.cmp(WIDE.value, %s) > 0", g.k(wideNew("Uint64", diff, 64))), "a read rejects out of range (SPEC §4.3)")
		}
		g.pf("%s%s = serialize.Uint64.add(WIDE.value, %s)\n", ind, val, g.k(wideNew("Uint64", lo, 64)))
	}
}

func (g *gen) readFixed(val string, f *ir.Field, ind string) {
	t := f.Type
	lo, hi := f.IntMin, f.IntMax
	if fixedIsNumber(f) {
		g.call(ind, fmt.Sprintf("stream:serializeFixedNumber(NUM, %s, %s)", lo, hi), "rejects above the raw range (SPEC §4.3)")
		g.pf("%s%s = NUM.value\n", ind, val)
		return
	}
	fn := "serializeFixed64"
	if t.Width == 128 {
		fn = "serializeFixed128"
	}
	g.call(ind, fmt.Sprintf("stream:%s(WIDE, %d, %d, %s, %s)", fn, t.IntBits, t.FracBits, g.k(wideNew("Int64", lo, 64)), g.k(wideNew("Int64", hi, 64))), "rejects above the raw range (SPEC §4.3)")
	switch wideClass(f) {
	case "":
		g.pf("%s%s = serialize.Int64.toNumber(WIDE.value)\n", ind, val)
	case "Uint64":
		g.pf("%s%s = serialize.Int64.toUint64(WIDE.value)\n", ind, val)
	case "Uint128":
		g.pf("%s%s = serialize.Int128.toUint128(WIDE.value)\n", ind, val)
	default:
		g.pf("%s%s = WIDE.value\n", ind, val)
	}
}

func (g *gen) emitReadUnion(d *ir.Union) error {
	g.pf("-- Read%s: a tag above %d is refused; a selected arm takes fresh declared\n", d.Name, d.Max)
	g.pf("-- defaults, then its payload (SPEC §4.8).\n")
	g.pf("function M.Read%s(stream: any, value: %s): boolean\n", d.Name, d.Name)
	g.call("\t", fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", d.Max), "rejects a tag above the count (SPEC §4.8)")
	g.pf("\tvalue.Type = NUM.value\n")
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		af := armField(v)
		g.pf("\tif value.Type == %d then\n", i+1)
		g.emitReset(af, "value", "\t\t", true)
		if err := g.readField("value."+ir.GoExportName(v.Name), af, "\t\t"); err != nil {
			return err
		}
		g.pf("\tend\n")
	}
	g.pf("\treturn true\nend\n\n")
	return nil
}
