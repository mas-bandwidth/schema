package ruby

import (
	"github.com/mas-bandwidth/schema/v3/ir"
)

func init() {
	registerItem(&itemEmitter{
		name: "align",
		read: func(m *rbModule, c *emitCtx, item ir.Item) {
			c.line("return false unless stream.serialize_align")
		},
		write: func(m *rbModule, c *emitCtx, item ir.Item) {
			c.line("stream.serialize_align")
		},
		validate: func(m *rbModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *rbModule, c *emitCtx, item ir.Item) {},
	})

	registerItem(&itemEmitter{
		name: "const",
		read: func(m *rbModule, c *emitCtx, item ir.Item) {
			constant := item.(*ir.ConstItem)
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_bits(%s, %d)", ri, constant.Bits)
			c.line("return false if %s.value != %s", ri, intLiteral(constant.Value))
		},
		write: func(m *rbModule, c *emitCtx, item ir.Item) {
			constant := item.(*ir.ConstItem)
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = %s", ri, intLiteral(constant.Value))
			c.line("stream.serialize_bits(%s, %d)", ri, constant.Bits)
		},
		validate: func(m *rbModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *rbModule, c *emitCtx, item ir.Item) {},
	})

	registerItem(&itemEmitter{
		name: "reserved",
		read: func(m *rbModule, c *emitCtx, item ir.Item) {
			reserved := item.(*ir.ReservedItem)
			ri := c.ref("_s_ri", "0")
			c.line("return false unless stream.serialize_bits(%s, %d)", ri, reserved.Bits)
			c.line("return false if %s.value != 0", ri)
		},
		write: func(m *rbModule, c *emitCtx, item ir.Item) {
			reserved := item.(*ir.ReservedItem)
			ri := c.ref("_s_ri", "0")
			c.line("%s.value = 0", ri)
			c.line("stream.serialize_bits(%s, %d)", ri, reserved.Bits)
		},
		validate: func(m *rbModule, c *emitCtx, item ir.Item) {},
		zero:     func(m *rbModule, c *emitCtx, item ir.Item) {},
	})

	registerItem(&itemEmitter{
		name: "branch",
		read: func(m *rbModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := fieldRead("self", branch.Cond)
			if branch.Neg {
				cond = "!" + cond
			}
			thenSub := c.sub(c.indent + "  ")
			m.readItems(thenSub, branch.Then)
			m.zeroItems(thenSub, branch.Else)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "  ")
			m.readItems(elseSub, branch.Else)
			m.zeroItems(elseSub, branch.Then)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s", cond)
			c.b.WriteString(thenSub.b.String())
			if elseSub.b.Len() > 0 {
				c.line("else")
				c.b.WriteString(elseSub.b.String())
			}
			c.line("end")
		},
		write: func(m *rbModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := fieldRead("self", branch.Cond)
			if branch.Neg {
				cond = "!" + cond
			}
			thenSub := c.sub(c.indent + "  ")
			m.writeItems(thenSub, branch.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "  ")
			m.writeItems(elseSub, branch.Else)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s", cond)
			c.b.WriteString(thenSub.b.String())
			if elseSub.b.Len() > 0 {
				c.line("else")
				c.b.WriteString(elseSub.b.String())
			}
			c.line("end")
		},
		validate: func(m *rbModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := fieldRead("self", branch.Cond)
			if branch.Neg {
				cond = "!" + cond
			}
			thenSub := c.sub(c.indent + "  ")
			m.validateItems(thenSub, branch.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "  ")
			m.validateItems(elseSub, branch.Else)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s", cond)
			c.b.WriteString(thenSub.b.String())
			if elseSub.b.Len() > 0 {
				c.line("else")
				c.b.WriteString(elseSub.b.String())
			}
			c.line("end")
		},
		zero: func(m *rbModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			m.zeroItems(c, branch.Then)
			m.zeroItems(c, branch.Else)
		},
	})
}
