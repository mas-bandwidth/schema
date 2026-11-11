package python

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A flags set is the uint64 mask over its declared wire bits (SPEC §4.2).
func init() {
	registerKind(&kindEmitter{
		name: "flags",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ref := f.Type.Ref.(*ir.Flags)
			if ref.WireBits == 0 {
				c.line("%s = 0", target)
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("if not %s(%s, %d):", op, ri, ref.WireBits)
			c.line("    return False")
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ref := f.Type.Ref.(*ir.Flags)
			if ref.WireBits == 0 {
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("%s.value = %s", ri, expr)
			c.line("%s(%s, %d)", op, ri, ref.WireBits)
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
}
