// The enum-keyed array's spellings in the checker (docs/SPEC-TABLES.md §2.4):
// `[E]T` is the table form, whose slots ride by variant NAME, and `[E.Max]T`
// is the positional one the TYPE wire keeps and a table body refuses.
package check

import (
	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// checkPositionalKeyedSpelling refuses `[E.Max]T` IN A TABLE BODY, where
// `[E]T` is the table form (docs/SPEC-TABLES.md §2.4, §11).
//
// AN ORDINAL-INDEXED ARRAY IS A POSITIONAL VOCABULARY AND A TABLE MAY HAVE
// ONLY ONE. A `[E.Max]T` field carries its elements by position, so inserting
// a variant in the middle of E lands every later element one slot off, in
// every file already written, with nothing on the wire that could say so —
// the silent class §4.1 names. Keyed slots ride by NAME (§3.2), so a middle
// insert moves no slot, and refusing the positional spelling here is what
// leaves `flags` as the only positional vocabulary a table has, and therefore
// the only exception the reachability-scoped projection needs (SPEC §3.1).
//
// On the TYPE wire the spelling stays legal and positional, unchanged: a
// `type` body's `[E.Max]T` is a plain array whose extent is the variant count,
// and every fact of it projects. The refusal is the TABLE body's alone.
func (c *checker) checkPositionalKeyedSpelling(f *ast.Field, inTable bool) bool {
	if !inTable || f.Array == nil || f.Array.Kind != ast.ArrayFixed {
		return true
	}
	m, ok := f.Array.Hi.(*ast.MaxExpr)
	if !ok || m.Sel != "Max" {
		return true
	}
	if _, isEnum := c.astDecls[m.Enum].(*ast.EnumDecl); !isEnum {
		return true
	}
	c.errf(m.Pos, "field %s: [%s.Max]%s is refused in a table body. An ordinal-indexed array is a POSITIONAL vocabulary, so inserting a variant in the middle of %s lands every later element one slot off in every file already written, with nothing on the wire that could say so. Instead spell it [%s]%s, whose slots ride by variant name (docs/SPEC-TABLES.md §2.4, §11)",
		f.Name, m.Enum, scalarSpelling(f.Type), m.Enum, m.Enum, scalarSpelling(f.Type))
	return false
}
