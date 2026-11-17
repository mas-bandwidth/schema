package python

import (
	"math"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The float kinds: float32 and float64 ride as their IEEE-754 bit patterns,
// finite only, and a ranged float32 rides as its compressed step index
// (SPEC §4.3). The bit layer does all of it: serialize_float and
// serialize_double refuse a non-finite pattern on read by its raw bits, and a
// compressed float is quantized and dequantized by serialize.py's own
// quantizer, never by generated arithmetic (docs/PORT-PYTHON.md, THE NUMBER
// MODEL). The writer's contract is checked first, in _s_validate, so a refused
// value raises InvalidValue naming its field before a byte is written.
func init() {
	registerKind(&kindEmitter{
		name:     "float32",
		read:     readFloat32,
		write:    writeFloat32,
		validate: validateFloat32,
		zero:     zeroFloat,
	})
	registerKind(&kindEmitter{
		name:     "float64",
		read:     readFloat64,
		write:    writeFloat64,
		validate: validateFloat64,
		zero:     zeroFloat,
	})
}

// float32Max and float64Max bound the inline check: a float inside them is
// finite and representable, so the common value never leaves the method. A
// value outside them goes to the runtime's check, which decides exactly (a
// float64 just above the largest float32 can still round down to it).
var (
	float32Max = floatLiteral(math.MaxFloat32)
	float64Max = floatLiteral(math.MaxFloat64)
)

func readFloat32(c *emitCtx, f *ir.Field, target string) {
	rf := c.ref("_s_rf", "0.0")
	if f.HasFloatRange {
		steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
		op := c.streamOp("serialize_compressed_float_precomputed")
		c.line("if not %s(%s, %d, %d, %s, %s):", op, rf, steps, bits, compressedDelta(f), compressedMin(f))
	} else {
		op := c.streamOp("serialize_float")
		c.line("if not %s(%s):", op, rf)
	}
	c.line("    return False")
	c.line("%s = %s.value", target, rf)
}

func writeFloat32(c *emitCtx, f *ir.Field, expr string) {
	rf := c.ref("_s_rf", "0.0")
	c.line("%s.value = %s", rf, expr)
	if f.HasFloatRange {
		steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
		op := c.streamOp("serialize_compressed_float_precomputed")
		c.line("%s(%s, %d, %d, %s, %s)", op, rf, steps, bits, compressedDelta(f), compressedMin(f))
		return
	}
	op := c.streamOp("serialize_float")
	c.line("%s(%s)", op, rf)
}

func validateFloat32(c *emitCtx, f *ir.Field, expr, path string) {
	tp := c.bind("_s_type", "type")
	ft := c.bind("_s_float", "float")
	v := c.local()
	c.line("%s = %s", v, expr)
	if f.HasFloatRange {
		// the declared range, held on the float32 values of min and max
		// (SPEC §5.1 row 10)
		c.m.needSchema("validate_compressed_float")
		lo, hi := floatLiteral(float32Value(f.FMin)), floatLiteral(float32Value(f.FMax))
		c.line("if %s(%s) is not %s or not %s <= %s <= %s:", tp, v, ft, lo, v, hi)
		c.line("    validate_compressed_float(%s, %s, %s, %s)", v, fieldPath(path, f.Name), lo, hi)
		return
	}
	c.m.needSchema("validate_float32")
	c.line("if %s(%s) is not %s or not -%s <= %s <= %s:", tp, v, ft, float32Max, v, float32Max)
	c.line("    validate_float32(%s, %s)", v, fieldPath(path, f.Name))
}

func readFloat64(c *emitCtx, f *ir.Field, target string) {
	rf := c.ref("_s_rf", "0.0")
	op := c.streamOp("serialize_double")
	c.line("if not %s(%s):", op, rf)
	c.line("    return False")
	c.line("%s = %s.value", target, rf)
}

func writeFloat64(c *emitCtx, f *ir.Field, expr string) {
	rf := c.ref("_s_rf", "0.0")
	op := c.streamOp("serialize_double")
	c.line("%s.value = %s", rf, expr)
	c.line("%s(%s)", op, rf)
}

func validateFloat64(c *emitCtx, f *ir.Field, expr, path string) {
	c.m.needSchema("validate_float64")
	tp := c.bind("_s_type", "type")
	ft := c.bind("_s_float", "float")
	v := c.local()
	c.line("%s = %s", v, expr)
	c.line("if %s(%s) is not %s or not -%s <= %s <= %s:", tp, v, ft, float64Max, v, float64Max)
	c.line("    validate_float64(%s, %s)", v, fieldPath(path, f.Name))
}

func zeroFloat(c *emitCtx, f *ir.Field, target string) {
	c.line("%s = 0.0", target)
}

// compressedDelta and compressedMin are the declaration's float32 constants
// for serialize_compressed_float_precomputed: max - min in float32
// arithmetic, and min rounded to float32, exactly as the runtime's
// compressed_float_params derives them. The runtime checks the precomputed
// tuple against the declaration's shape on every call.
func compressedDelta(f *ir.Field) string {
	return floatLiteral(float64(float32(f.FMax) - float32(f.FMin)))
}

func compressedMin(f *ir.Field) string {
	return floatLiteral(float32Value(f.FMin))
}
