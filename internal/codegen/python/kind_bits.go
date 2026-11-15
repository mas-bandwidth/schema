package python

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The bits(N) kind: a raw N-bit window, 1 to 64 bits, an int in [0, 2^N)
// (SPEC §4.3, docs/PORT-PYTHON.md, THE VALUE MAPPING).
func init() {
	registerKind(&kindEmitter{
		name: "bits",
		read: func(c *emitCtx, f *ir.Field, target string) {
			w := f.Type.Width
			if w == 0 {
				c.line("%s = 0", target)
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("if not %s(%s, %d):", op, ri, w)
			c.line("    return False")
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			w := f.Type.Width
			if w == 0 {
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("%s.value = %s", ri, expr)
			c.line("%s(%s, %d)", op, ri, w)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			emitIntValidate(c, expr, fieldPath(path, f.Name), "0", intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})
}
