package python

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The bool kind: one wire bit, a Ref[bool], and a write that refuses an int.
func init() {
	registerKind(&kindEmitter{
		name: "bool",
		read: func(c *emitCtx, f *ir.Field, target string) {
			local := c.local()
			c.m.needSerialize("Ref")
			c.line("%s = Ref(False)", local)
			c.line("if not stream.serialize_bool(%s):", local)
			c.line("    return False")
			c.line("%s = %s.value", target, local)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			c.m.needSerialize("Ref")
			c.line("stream.serialize_bool(Ref(%s))", expr)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_bool")
			c.line("validate_bool(%s, %s)", expr, fieldPath(path, f.Name))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = False", target)
		},
	})
}

func fieldPath(path, name string) string {
	return fmt.Sprintf("%s + %q", path, "."+name)
}
