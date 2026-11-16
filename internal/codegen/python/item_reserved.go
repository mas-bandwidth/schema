package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// reserved(Bits): zeros on the wire, and a read refuses a nonzero reserved
// bit (SPEC §4.3). It carries no storage, so there is nothing to validate or
// zero.
func init() {
	registerItem(&itemEmitter{
		name: "reserved",
		read: func(m *pyModule, c *emitCtx, item ir.Item) {
			reserved := item.(*ir.ReservedItem)
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("if not %s(%s, %d):", op, ri, reserved.Bits)
			c.line("    return False")
			c.line("if %s.value != 0:", ri)
			c.line("    return False")
		},
		write: func(m *pyModule, c *emitCtx, item ir.Item) {
			reserved := item.(*ir.ReservedItem)
			ri := c.ref("_s_ri", "0")
			op := c.streamOp("serialize_bits")
			c.line("%s.value = 0", ri)
			c.line("%s(%s, %d)", op, ri, reserved.Bits)
		},
		validate: func(m *pyModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *pyModule, c *emitCtx, item ir.Item) {},
	})
}
