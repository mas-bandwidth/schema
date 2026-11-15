package python

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The fixed point kinds fixed(I, F) and ufixed(I, F): the value is the raw
// scaled integer, as every leg stores it, and the wire is its offset from the
// declared minimum, in the bits the raw span needs (SPEC §4.3). serialize.py's
// serialize_fixed carries the Q format at every storage width, the degenerate
// zero-bit range included; the writer holds the raw value to
// [min << F, max << F] first (docs/PORT-PYTHON.md, THE VALUE MAPPING).
func init() {
	registerKind(&kindEmitter{
		name: "fixed",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_fixed")
			c.line("if not %s(%s, %d, %d, %s, %s):", op, ri, f.Type.IntBits, f.Type.FracBits,
				intLiteral(f.IntMin), intLiteral(f.IntMax))
			c.line("    return False")
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_fixed")
			c.line("%s.value = %s", ri, expr)
			c.line("%s(%s, %d, %d, %s, %s)", op, ri, f.Type.IntBits, f.Type.FracBits,
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
}

// fixedRawBounds is the raw value range of a fixed point field: its declared
// whole-unit range scaled by 2^F.
func fixedRawBounds(f *ir.Field) (lo, hi *big.Int) {
	fb := uint(f.Type.FracBits)
	return new(big.Int).Lsh(f.IntMin, fb), new(big.Int).Lsh(f.IntMax, fb)
}

// fixedBits is a fixed point field's wire width: the bits of its raw span.
func fixedBits(f *ir.Field) int64 {
	lo, hi := fixedRawBounds(f)
	return int64(new(big.Int).Sub(hi, lo).BitLen())
}
