package python

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The bool kind: one wire bit, a reusable Ref[bool], and a write that refuses
// an int.
func init() {
	registerKind(&kindEmitter{
		name: "bool",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rb := c.ref("_s_rb", "False")
			op := c.streamOp("serialize_bool")
			c.line("if not %s(%s):", op, rb)
			c.line("    return False")
			c.line("%s = %s.value", target, rb)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rb := c.ref("_s_rb", "False")
			op := c.streamOp("serialize_bool")
			c.line("%s.value = %s", rb, expr)
			c.line("%s(%s)", op, rb)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_bool")
			tp := c.bind("_s_type", "type")
			bt := c.bind("_s_bool", "bool")
			v := c.local()
			c.line("%s = %s", v, expr)
			c.line("if %s(%s) is not %s:", tp, v, bt)
			c.line("    validate_bool(%s, %s)", v, fieldPath(path, f.Name))
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = False", target)
		},
	})
}

func fieldPath(path, name string) string {
	return fmt.Sprintf("%s + %q", path, "."+name)
}
