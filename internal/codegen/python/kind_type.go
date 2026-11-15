package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A type by value is a nested generated class, read and written through its
// own methods (SPEC §4.6).
func init() {
	registerKind(&kindEmitter{
		name: "type",
		read: func(c *emitCtx, f *ir.Field, target string) {
			c.line("if not %s._s_read(stream):", target)
			c.line("    return False")
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			c.line("%s._s_write(stream)", expr)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("InvalidValue")
			name := c.m.typeRef(f.Type.Name)
			field := fieldPath(path, f.Name)
			c.line("if not isinstance(%s, %s):", expr, name)
			c.line("    raise InvalidValue(%s, \"expected %s\")", field, f.Type.Name)
			c.line("%s._s_validate(%s)", expr, field)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s._s_zero()", target)
		},
	})
}
