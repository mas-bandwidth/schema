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
			local := c.local()
			c.m.needSerialize("Ref")
			c.line("%s = Ref(0)", local)
			c.line("if not stream.serialize_bits(%s, %d):", local, ref.WireBits)
			c.line("    return False")
			c.line("%s = %s.value", target, local)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ref := f.Type.Ref.(*ir.Flags)
			if ref.WireBits == 0 {
				return
			}
			c.m.needSerialize("Ref")
			c.line("stream.serialize_bits(Ref(%s), %d)", expr, ref.WireBits)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			ref := f.Type.Ref.(*ir.Flags)
			c.m.needSchema("validate_int")
			max := new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))
			max.Sub(max, big.NewInt(1))
			c.line("validate_int(%s, %s, 0, %s)", expr, fieldPath(path, f.Name), intLiteral(max))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})
}
