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
			local := c.local()
			c.m.needSerialize("Ref")
			c.line("%s = Ref(0)", local)
			c.line("if not stream.serialize_bits(%s, %d):", local, w)
			c.line("    return False")
			if f.Type.Signed {
				threshold := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
				modulus := new(big.Int).Lsh(big.NewInt(1), uint(w))
				c.line("if %s.value >= %s:", local, intLiteral(threshold))
				c.line("    %s.value -= %s", local, intLiteral(modulus))
			}
			c.line("%s = %s.value", target, local)
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
			c.m.needSerialize("Ref")
			mask := new(big.Int).Lsh(big.NewInt(1), uint(w))
			mask.Sub(mask, big.NewInt(1))
			c.line("stream.serialize_bits(Ref(%s & %s), %d)", expr, intLiteral(mask), w)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_int")
			min, max := intBounds(f)
			c.line("validate_int(%s, %s, %s, %s)", expr, fieldPath(path, f.Name), intLiteral(min), intLiteral(max))
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
