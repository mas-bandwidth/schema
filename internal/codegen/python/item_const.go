package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// const(Value, Bits): the constant rides the wire and a read refuses any other
// value (SPEC §4.3). It carries no storage, so there is nothing to validate or
// zero.
func init() {
	registerItem(&itemEmitter{
		name: "const",
		read: func(m *pyModule, c *emitCtx, item ir.Item) {
			constant := item.(*ir.ConstItem)
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("if not %s(%s, %d):", op, ri, constant.Bits)
			c.line("    return False")
			c.line("if %s.value != %s:", ri, intLiteral(constant.Value))
			c.line("    return False")
		},
		write: func(m *pyModule, c *emitCtx, item ir.Item) {
			constant := item.(*ir.ConstItem)
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("%s.value = %s", ri, intLiteral(constant.Value))
			c.line("%s(%s, %d)", op, ri, constant.Bits)
		},
		validate: func(m *pyModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *pyModule, c *emitCtx, item ir.Item) {},
	})
}
