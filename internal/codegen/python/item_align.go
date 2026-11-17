package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// align pads to the next byte boundary with zeros; a read refuses nonzero
// padding (SPEC §4.3). It carries no storage, so there is nothing to validate
// or zero.
func init() {
	registerItem(&itemEmitter{
		name: "align",
		read: func(m *pyModule, c *emitCtx, item ir.Item) {
			op := c.streamOp("serialize_align")
			c.line("if not %s():", op)
			c.line("    return False")
		},
		write: func(m *pyModule, c *emitCtx, item ir.Item) {
			op := c.streamOp("serialize_align")
			c.line("%s()", op)
		},
		validate: func(m *pyModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *pyModule, c *emitCtx, item ir.Item) {},
	})
}
