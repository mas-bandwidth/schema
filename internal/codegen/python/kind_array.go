package python

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Fixed arrays [N]T, a list of exactly N, and counted arrays [A..B]T, a list
// whose length is the count, in [A, B] on write and on read (SPEC §4.6). The
// count rides first, as the ranged integer it is; then each element through
// its own kind's emitter, so a kind added later composes with no change here.
func init() {
	registerKind(&kindEmitter{
		name: "array",
		read: func(c *emitCtx, f *ir.Field, target string) {
			elem := arrayElem(f)
			list := c.local()
			if f.Array == ir.ArrayCounted {
				// the count guards the element loop (SPEC §6.3): it is read
				// and range-checked before any element is built
				count := c.local()
				emitRangeReadBig(c, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), count)
				c.line("%s = %s = %s", target, list, c.m.arrayFill(f, count))
			} else {
				if f.ArrayBound == 0 {
					return
				}
				c.line("%s = %s", list, target)
			}
			emitElementRead(c, elem, list)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			elem := arrayElem(f)
			list := c.local()
			c.line("%s = %s", list, expr)
			if f.Array == ir.ArrayCounted {
				emitRangeWriteBig(c, "len("+list+")", big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
			}
			sub := c.sub(c.indent + "    ")
			e := c.local()
			sub.locals = c.locals
			emitKindWrite(sub, elem, e)
			c.locals = sub.locals
			if sub.b.Len() == 0 {
				return
			}
			c.line("for %s in %s:", e, list)
			c.b.WriteString(sub.b.String())
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_array")
			elem := arrayElem(f)
			tp := c.bind("_s_type", "type")
			lt := c.bind("_s_list", "list")
			list := c.local()
			min, max := f.ArrayBound, f.ArrayBound
			if f.Array == ir.ArrayCounted {
				min = f.ArrayMin
			}
			c.line("%s = %s", list, expr)
			if min == max {
				c.line("if %s(%s) is not %s or len(%s) != %d:", tp, list, lt, list, max)
			} else {
				c.line("if %s(%s) is not %s or not %d <= len(%s) <= %d:", tp, list, lt, min, list, max)
			}
			c.line("    validate_array(%s, %s, %d, %d)", list, fieldPath(path, f.Name), min, max)
			idx := c.local()
			e := c.local()
			sub := c.sub(c.indent + "    ")
			sub.locals = c.locals
			emitKindValidate(sub, elem, e, elementPath(path, f.Name, idx))
			c.locals = sub.locals
			if sub.b.Len() == 0 {
				return
			}
			c.line("for %s, %s in enumerate(%s):", idx, e, list)
			c.b.WriteString(sub.b.String())
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			// the SPEC §5 zero form: a counted array holds no element, and a
			// fixed array holds N elements at their zero form
			if f.Array == ir.ArrayCounted {
				c.line("%s = []", target)
				return
			}
			elem := arrayElem(f)
			if elementInPlace(elem) {
				e := c.local()
				c.line("for %s in %s:", e, target)
				c.line("    %s._s_zero()", e)
				return
			}
			c.line("%s = [%s] * %d", target, c.m.zeroExpr(elem), f.ArrayBound)
		},
	})
}

// arrayElem is one element of an array field: the same field, unnamed, with no
// array form. Its emitters name it by the path the array hands them.
func arrayElem(f *ir.Field) *ir.Field {
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	elem.Name = ""
	return &elem
}

// elementInPlace reports an element kind that is read, written and zeroed
// through its own object's methods rather than assigned.
func elementInPlace(elem *ir.Field) bool {
	k := fieldKind(elem)
	return k == "type" || k == "union"
}

// emitElementRead reads every element of list in place: an object element
// through its own reader, a scalar element into its slot.
func emitElementRead(c *emitCtx, elem *ir.Field, list string) {
	e := c.local()
	sub := c.sub(c.indent + "    ")
	sub.locals = c.locals
	if elementInPlace(elem) {
		emitKindRead(sub, elem, e)
		c.locals = sub.locals
		c.line("for %s in %s:", e, list)
	} else {
		emitKindRead(sub, elem, list+"["+e+"]")
		c.locals = sub.locals
		c.line("for %s in range(len(%s)):", e, list)
	}
	c.b.WriteString(sub.b.String())
}

// elementPath is the path expression of one element, Fleet.ships[3].
func elementPath(path, name, idx string) string {
	return fmt.Sprintf("f\"{%s}.%s[{%s}]\"", path, name, idx)
}

// arrayFill is a fresh list of n elements, each at its construction value
// (SPEC §4.2); n is a Python expression.
func (m *pyModule) arrayFill(f *ir.Field, n string) string {
	elem := *f
	elem.Array = ir.ArrayNone
	value := m.defaultExpr(&elem)
	if isScalarZero(value) {
		return fmt.Sprintf("[%s] * %s", value, n)
	}
	return fmt.Sprintf("[%s for _s_i in range(%s)]", value, n)
}

// arrayBits is an array's widest wire: the count's bits, then every element
// at its widest.
func (m *pyModule) arrayBits(f *ir.Field, seen map[string]bool) int64 {
	bits := m.fieldBits(arrayElem(f), seen) * f.ArrayBound
	if f.Array == ir.ArrayCounted {
		bits += rangeBits64(f.ArrayMin, f.ArrayBound)
	}
	return bits
}
