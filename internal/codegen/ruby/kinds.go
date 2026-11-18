package ruby

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	float32Max = floatLiteral(math.MaxFloat32)
	float64Max = floatLiteral(math.MaxFloat64)
)

func fieldPath(pathExpr, name string) string {
	if name == "" {
		return pathExpr
	}
	if pathExpr == "path" {
		return fmt.Sprintf("\"#{%s}.%s\"", pathExpr, name)
	}
	return fmt.Sprintf("%s + %q", pathExpr, "."+name)
}

func elementPath(pathExpr, name, idx string) string {
	if name == "" {
		return fmt.Sprintf("\"#{%s}[#{%s}]\"", pathExpr, idx)
	}
	if pathExpr == "path" {
		return fmt.Sprintf("\"#{%s}.%s[#{%s}]\"", pathExpr, name, idx)
	}
	return fmt.Sprintf("\"#{%s}.%s[#{%s}]\"", pathExpr, name, idx)
}

func arrayElem(f *ir.Field) *ir.Field {
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	elem.Name = ""
	return &elem
}

func elementInPlace(elem *ir.Field) bool {
	k := fieldKind(elem)
	return k == "type" || k == "union"
}

func intBounds(f *ir.Field) (min, max *big.Int) {
	if f.HasIntRange {
		return f.IntMin, f.IntMax
	}
	w := f.Type.Width
	if f.Type.Signed {
		max = new(big.Int).Lsh(big.NewInt(1), uint(w-1))
		min = new(big.Int).Neg(max)
		max.Sub(max, big.NewInt(1))
		return min, max
	}
	max = new(big.Int).Lsh(big.NewInt(1), uint(w))
	max.Sub(max, big.NewInt(1))
	return big.NewInt(0), max
}

func fixedRawBounds(f *ir.Field) (lo, hi *big.Int) {
	fb := uint(f.Type.FracBits)
	return new(big.Int).Lsh(f.IntMin, fb), new(big.Int).Lsh(f.IntMax, fb)
}

func fixedBits(f *ir.Field) int64 {
	lo, hi := fixedRawBounds(f)
	return int64(new(big.Int).Sub(hi, lo).BitLen())
}

func compressedDelta(f *ir.Field) string {
	return floatLiteral(float64(float32(f.FMax) - float32(f.FMin)))
}

func compressedMin(f *ir.Field) string {
	return floatLiteral(float32Value(f.FMin))
}

func init() {
	registerKind(&kindEmitter{
		name: "bool",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rb := c.refTyped("_s_rb", "false", "bool")
			c.line("return false unless stream.serialize_bool(%s)", rb)
			c.line("%s = %s.value", target, rb)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rb := c.refTyped("_s_rb", "false", "bool")
			c.line("%s.value = %s", rb, expr)
			c.line("stream.serialize_bool(%s)", rb)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			c.line("%s = %s #: untyped", v, expr)
			c.line("unless %s.equal?(true) || %s.equal?(false)", v, v)
			c.line("  ::%s._s_validate_bool(%s, %s)", c.m.mod, v, fieldPath(path, f.Name))
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = false", target)
		},
	})

	registerKind(&kindEmitter{
		name: "int",
		read: func(c *emitCtx, f *ir.Field, target string) {
			if f.HasIntRange {
				emitRangeReadBig(c, f.IntMin, f.IntMax, target)
				return
			}
			w := f.Type.Width
			if w == 0 {
				c.line("%s = 0", target)
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_bits(%s, %d)", ri, w)
			if f.Type.Signed {
				threshold := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
				modulus := new(big.Int).Lsh(big.NewInt(1), uint(w))
				v := c.local()
				c.line("%s = %s.value", v, ri)
				c.line("%s -= %s if %s >= %s", v, intLiteral(modulus), v, intLiteral(threshold))
				c.line("%s = %s", target, v)
				return
			}
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			if f.HasIntRange {
				emitRangeWriteBig(c, expr, f.IntMin, f.IntMax)
				return
			}
			w := f.Type.Width
			if w == 0 {
				return
			}
			ri := c.ref("_s_ri", "0")
			if f.Type.Signed {
				mask := new(big.Int).Lsh(big.NewInt(1), uint(w))
				mask.Sub(mask, big.NewInt(1))
				c.line("%s.value = %s & %s", ri, expr, intLiteral(mask))
			} else {
				c.line("%s.value = %s", ri, expr)
			}
			c.line("stream.serialize_bits(%s, %d)", ri, w)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			min, max := intBounds(f)
			emitIntValidate(c, expr, fieldPath(path, f.Name), intLiteral(min), intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "int128",
		read: func(c *emitCtx, f *ir.Field, target string) {
			if f.HasIntRange {
				emitRangeReadBig(c, f.IntMin, f.IntMax, target)
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_uint128(%s)", ri)
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			if f.HasIntRange {
				emitRangeWriteBig(c, expr, f.IntMin, f.IntMax)
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = %s", ri, expr)
			c.line("stream.serialize_uint128(%s)", ri)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			min, max := intBounds(f)
			emitIntValidate(c, expr, fieldPath(path, f.Name), intLiteral(min), intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "bits",
		read: func(c *emitCtx, f *ir.Field, target string) {
			w := f.Type.Width
			if w == 0 {
				c.line("%s = 0", target)
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_bits(%s, %d)", ri, w)
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			w := f.Type.Width
			if w == 0 {
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = %s", ri, expr)
			c.line("stream.serialize_bits(%s, %d)", ri, w)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			emitIntValidate(c, expr, fieldPath(path, f.Name), "0", intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "enum",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ref := f.Type.Ref.(*ir.Enum)
			if ref.Max == 0 {
				c.line("%s = 0", target)
				return
			}
			emitRangeRead(c, 0, ref.Max, target)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ref := f.Type.Ref.(*ir.Enum)
			if ref.Max == 0 {
				return
			}
			emitRangeWrite(c, expr, 0, ref.Max)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			ref := f.Type.Ref.(*ir.Enum)
			emitIntValidate(c, expr, fieldPath(path, f.Name), "0", strconv.FormatInt(ref.Max, 10))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "flags",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ref := f.Type.Ref.(*ir.Flags)
			if ref.WireBits == 0 {
				c.line("%s = 0", target)
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_bits(%s, %d)", ri, ref.WireBits)
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ref := f.Type.Ref.(*ir.Flags)
			if ref.WireBits == 0 {
				return
			}
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = %s", ri, expr)
			c.line("stream.serialize_bits(%s, %d)", ri, ref.WireBits)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			ref := f.Type.Ref.(*ir.Flags)
			max := new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))
			max.Sub(max, big.NewInt(1))
			emitIntValidate(c, expr, fieldPath(path, f.Name), "0", intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "fixed",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_fixed(%s, %d, %d, %s, %s)", ri, f.Type.IntBits, f.Type.FracBits,
				intLiteral(f.IntMin), intLiteral(f.IntMax))
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = %s", ri, expr)
			c.line("stream.serialize_fixed(%s, %d, %d, %s, %s)", ri, f.Type.IntBits, f.Type.FracBits,
				intLiteral(f.IntMin), intLiteral(f.IntMax))
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			lo, hi := fixedRawBounds(f)
			emitIntValidate(c, expr, fieldPath(path, f.Name), intLiteral(lo), intLiteral(hi))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "float32",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rf := c.ref("_s_rf", "0.0")
			if f.HasFloatRange {
				steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
				c.line("return false unless stream.serialize_compressed_float_precomputed(%s, %d, %d, %s, %s)",
					rf, steps, bits, compressedDelta(f), compressedMin(f))
			} else {
				c.line("return false unless stream.serialize_float(%s)", rf)
			}
			c.line("%s = %s.value", target, rf)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rf := c.ref("_s_rf", "0.0")
			c.line("%s.value = %s", rf, expr)
			if f.HasFloatRange {
				steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
				c.line("stream.serialize_compressed_float_precomputed(%s, %d, %d, %s, %s)",
					rf, steps, bits, compressedDelta(f), compressedMin(f))
				return
			}
			c.line("stream.serialize_float(%s)", rf)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			c.line("%s = %s #: untyped", v, expr)
			if f.HasFloatRange {
				lo, hi := floatLiteral(float32Value(f.FMin)), floatLiteral(float32Value(f.FMax))
				c.line("unless %s.is_a?(::Float) && %s >= %s && %s <= %s", v, v, lo, v, hi)
				c.line("  ::%s._s_validate_compressed_float(%s, %s, %s, %s)", c.m.mod, v, fieldPath(path, f.Name), lo, hi)
				c.line("end")
				return
			}
			c.line("unless %s.is_a?(::Float) && %s >= -%s && %s <= %s", v, v, float32Max, v, float32Max)
			c.line("  ::%s._s_validate_float32(%s, %s)", c.m.mod, v, fieldPath(path, f.Name))
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0.0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "float64",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rf := c.ref("_s_rf", "0.0")
			c.line("return false unless stream.serialize_double(%s)", rf)
			c.line("%s = %s.value", target, rf)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rf := c.ref("_s_rf", "0.0")
			c.line("%s.value = %s", rf, expr)
			c.line("stream.serialize_double(%s)", rf)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			c.line("%s = %s #: untyped", v, expr)
			c.line("unless %s.is_a?(::Float) && %s >= -%s && %s <= %s", v, v, float64Max, v, float64Max)
			c.line("  ::%s._s_validate_float64(%s, %s)", c.m.mod, v, fieldPath(path, f.Name))
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0.0", target)
		},
	})

	registerKind(&kindEmitter{
		name: "string",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rs := c.ref("_s_rs", "\"\"")
			c.line("return false unless stream.serialize_string(%s, %d)", rs, f.Type.Size+1)
			c.line("%s = %s.value", target, rs)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rs := c.ref("_s_rs", "\"\"")
			c.line("%s.value = %s", rs, expr)
			c.line("stream.serialize_string(%s, %d)", rs, f.Type.Size+1)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			n := strconv.FormatInt(f.Type.Size, 10)
			c.line("%s = %s #: untyped", v, expr)
			c.line("unless %s.is_a?(::String) && (%s.encoding == ::Encoding::UTF_8 || %s.encoding == ::Encoding::US_ASCII) && %s.bytesize <= %s && %s.valid_encoding? && !%s.include?(\"\\x00\")",
				v, v, v, v, n, v, v)
			c.line("  ::%s._s_validate_string(%s, %s, %s)", c.m.mod, v, fieldPath(path, f.Name), n)
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = \"\"", target)
		},
	})

	registerKind(&kindEmitter{
		name: "bytes",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ri := c.ref("_s_ri", "0")
			rb := c.ref("_s_rby", "\"\".b")
			c.line("return false unless stream.serialize_int(%s, 0, %d)", ri, f.Type.Size)
			c.line("return false unless stream.serialize_bytes(%s, %s.value)", rb, ri)
			c.line("%s = ::%s._s_copy_bytes(%s.value)", target, c.m.mod, rb)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ri := c.ref("_s_ri", "0")
			rb := c.ref("_s_rby", "\"\".b")
			c.line("%s.value = %s", rb, expr)
			c.line("%s.value = %s.value.bytesize", ri, rb)
			c.line("stream.serialize_int(%s, 0, %d)", ri, f.Type.Size)
			c.line("stream.serialize_bytes(%s, %s.value)", rb, ri)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			n := strconv.FormatInt(f.Type.Size, 10)
			c.line("%s = %s #: untyped", v, expr)
			c.line("unless %s.is_a?(::String) && %s.encoding == ::Encoding::BINARY && %s.bytesize <= %s", v, v, v, n)
			c.line("  ::%s._s_validate_bytes(%s, %s, %s)", c.m.mod, v, fieldPath(path, f.Name), n)
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = \"\".b.freeze", target)
		},
	})

	registerKind(&kindEmitter{
		name: "type",
		read: func(c *emitCtx, f *ir.Field, target string) {
			c.line("return false unless %s._s_read(stream)", target)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			c.line("%s._s_write(stream)", expr)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			name := c.m.typeRef(f.Type.Name)
			fp := fieldPath(path, f.Name)
			c.line("%s = %s #: untyped", v, expr)
			c.line("raise InvalidValue.new(%s, \"expected %s\") unless %s.instance_of?(%s)", fp, f.Type.Name, v, name)
			c.line("%s._s_validate(%s)", expr, fp)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s._s_zero", target)
		},
	})

	registerKind(&kindEmitter{
		name: "union",
		read: func(c *emitCtx, f *ir.Field, target string) {
			c.line("return false unless %s._s_read(stream)", target)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			c.line("%s._s_write(stream)", expr)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			v := c.local()
			name := c.m.typeRef(f.Type.Name)
			fp := fieldPath(path, f.Name)
			c.line("%s = %s #: untyped", v, expr)
			c.line("raise InvalidValue.new(%s, \"expected %s\") unless %s.instance_of?(%s)", fp, f.Type.Name, v, name)
			c.line("%s._s_validate(%s)", expr, fp)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s._s_zero", target)
		},
	})

	registerKind(&kindEmitter{
		name: "array",
		read: func(c *emitCtx, f *ir.Field, target string) {
			elem := arrayElem(f)
			list := c.local()
			if f.Array == ir.ArrayCounted {
				count := c.local()
				emitRangeReadBig(c, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), count)
				c.line("%s = %s = %s", target, list, c.m.arrayFill(f, count))
			} else {
				if f.ArrayBound == 0 {
					return
				}
				c.line("%s = %s", list, target)
			}
			if elementInPlace(elem) {
				e := c.local()
				sub := c.sub(c.indent + "  ")
				sub.locals = c.locals
				emitKindRead(sub, elem, e)
				c.locals = sub.locals
				c.line("%s.each do |%s|", list, e)
				c.b.WriteString(sub.b.String())
				c.line("end")
			} else {
				idx := c.local()
				n := c.local()
				ev := c.local()
				c.line("%s = 0", idx)
				c.line("%s = %s.length", n, list)
				c.line("while %s < %s", idx, n)
				sub := c.sub(c.indent + "  ")
				sub.locals = c.locals
				sub.line("%s = %s", ev, c.m.zeroExpr(elem))
				emitKindRead(sub, elem, ev)
				sub.line("%s[%s] = %s", list, idx, ev)
				sub.line("%s += 1", idx)
				c.locals = sub.locals
				c.b.WriteString(sub.b.String())
				c.line("end")
			}
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			elem := arrayElem(f)
			list := c.local()
			c.line("%s = %s", list, expr)
			if f.Array == ir.ArrayCounted {
				emitRangeWriteBig(c, list+".length", big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
			}
			e := c.local()
			sub := c.sub(c.indent + "  ")
			sub.locals = c.locals
			emitKindWrite(sub, elem, e)
			c.locals = sub.locals
			if sub.b.Len() == 0 {
				return
			}
			c.line("%s.each do |%s|", list, e)
			c.b.WriteString(sub.b.String())
			c.line("end")
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			elem := arrayElem(f)
			v := c.local()
			min, max := f.ArrayBound, f.ArrayBound
			if f.Array == ir.ArrayCounted {
				min = f.ArrayMin
			}
			c.line("%s = %s #: untyped", v, expr)
			if min == max {
				c.line("unless %s.instance_of?(::Array) && %s.length == %d", v, v, max)
			} else {
				c.line("unless %s.instance_of?(::Array) && %s.length >= %d && %s.length <= %d", v, v, min, v, max)
			}
			c.line("  ::%s._s_validate_array(%s, %s, %d, %d)", c.m.mod, v, fieldPath(path, f.Name), min, max)
			c.line("end")
			list := c.local()
			e := c.local()
			idx := c.local()
			sub := c.sub(c.indent + "  ")
			sub.locals = c.locals
			emitKindValidate(sub, elem, e, elementPath(path, f.Name, idx))
			c.locals = sub.locals
			if sub.b.Len() == 0 {
				return
			}
			c.line("%s = %s", list, expr)
			c.line("%s.each_with_index do |%s, %s|", list, e, idx)
			c.b.WriteString(sub.b.String())
			c.line("end")
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			elem := arrayElem(f)
			if f.Array == ir.ArrayCounted {
				c.line("%s = [] #: ::Array[%s]", target, c.m.rbsFieldType(elem))
				return
			}
			if elementInPlace(elem) {
				e := c.local()
				c.line("%s.each { |%s| %s._s_zero }", target, e, e)
				return
			}
			val := c.m.zeroExpr(elem)
			if isImmutableExpr(elem) {
				c.line("%s = ::Array.new(%d, %s)", target, f.ArrayBound, val)
			} else {
				c.line("%s = ::Array.new(%d) { %s }", target, f.ArrayBound, val)
			}
		},
	})
}
