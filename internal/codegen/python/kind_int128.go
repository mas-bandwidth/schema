package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The 128-bit integer kinds (SPEC §4.3): uint128 is the raw field, always 128
// bits with the low 64-bit half first, and int128 is always ranged, an offset
// from its declared minimum in the fewest bits that hold the span (the same
// bytes as the 64-bit ranged call wherever the range fits). Python's int is
// arbitrary precision, so the declared range is the whole contract and the
// write refuses a value outside it in every build (docs/PORT-PYTHON.md, THE
// VALUE MAPPING).
func init() {
	registerKind(&kindEmitter{
		name: "int128",
		read: func(c *emitCtx, f *ir.Field, target string) {
			if f.HasIntRange {
				emitRangeReadBig(c, f.IntMin, f.IntMax, target)
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_uint128")
			c.line("if not %s(%s):", op, ri)
			c.line("    return False")
			c.line("%s = %s.value", target, ri)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			if f.HasIntRange {
				emitRangeWriteBig(c, expr, f.IntMin, f.IntMax)
				return
			}
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_uint128")
			c.line("%s.value = %s", ri, expr)
			c.line("%s(%s)", op, ri)
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
