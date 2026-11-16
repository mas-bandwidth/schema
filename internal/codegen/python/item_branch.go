package python

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The if/else branch: both sides are stored, the taken side rides the wire and
// a read sets the untaken side to its zero values, not its defaults (SPEC §5).
func init() {
	registerItem(&itemEmitter{
		name: "branch",
		read: func(m *pyModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := "self." + branch.Cond
			if branch.Neg {
				cond = "not " + cond
			}
			thenSub := c.sub(c.indent + "    ")
			m.readItems(thenSub, branch.Then)
			m.zeroItems(thenSub, branch.Else)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			m.readItems(elseSub, branch.Else)
			m.zeroItems(elseSub, branch.Then)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
			}
		},
		write: func(m *pyModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := "self." + branch.Cond
			if branch.Neg {
				cond = "not " + cond
			}
			thenSub := c.sub(c.indent + "    ")
			m.writeItems(thenSub, branch.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			m.writeItems(elseSub, branch.Else)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
			}
		},
		validate: func(m *pyModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			cond := "self." + branch.Cond
			if branch.Neg {
				cond = "not " + cond
			}
			thenSub := c.sub(c.indent + "    ")
			m.validateItems(thenSub, branch.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			m.validateItems(elseSub, branch.Else)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				return
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
			}
		},
		zero: func(m *pyModule, c *emitCtx, item ir.Item) {
			branch := item.(*ir.Branch)
			m.zeroItems(c, branch.Then)
			m.zeroItems(c, branch.Else)
		},
	})
}
