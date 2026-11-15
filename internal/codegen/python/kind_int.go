package python

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The integer kinds int8..uint64, bare or ranged. Python's int is arbitrary
// precision, so the declared range is the whole contract (docs/PORT-PYTHON.md,
// THE VALUE MAPPING).
func init() {
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
			op := c.streamOp("serialize_bits")
			c.line("if not %s(%s, %d):", op, ri, w)
			c.line("    return False")
			if f.Type.Signed {
				threshold := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
				modulus := new(big.Int).Lsh(big.NewInt(1), uint(w))
				v := c.local()
				c.line("%s = %s.value", v, ri)
				c.line("if %s >= %s:", v, intLiteral(threshold))
				c.line("    %s -= %s", v, intLiteral(modulus))
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
			op := c.streamOp("serialize_bits")
			if f.Type.Signed {
				mask := new(big.Int).Lsh(big.NewInt(1), uint(w))
				mask.Sub(mask, big.NewInt(1))
				c.line("%s.value = %s & %s", ri, expr, intLiteral(mask))
			} else {
				c.line("%s.value = %s", ri, expr)
			}
			c.line("%s(%s, %d)", op, ri, w)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			min, max := intBounds(f)
			emitIntValidate(c, expr, fieldPath(path, f.Name), intLiteral(min), intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})
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
