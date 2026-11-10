package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// An enum is a plain int on the wire, range [0, Max] (SPEC §4.2).
func init() {
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
			c.m.needSchema("validate_int")
			c.line("validate_int(%s, %s, 0, %d)", expr, fieldPath(path, f.Name), ref.Max)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = 0", target)
		},
	})
}
