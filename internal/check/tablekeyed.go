// The enum-keyed array's spellings in the checker (docs/SPEC-TABLES.md §2.4):
// `[E]T` is the table form, whose slots ride by variant NAME, and a POSITIONAL
// array whose bound FOLDS FROM an enum is what a table body and a union arm
// refuse. The refusal reads the bound's PROVENANCE rather than its spelling,
// so `[E.Max]T`, `[E.Count]T` and `[N]T` under a `const N` that folds from
// either are one rule (schema#605).
package check

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// enumBound is what the provenance walk found under a bound: the enum the
// bound folds from, the selector at the far end of the fold, and the constant
// the FIELD spells where it reaches the enum through one. `through` is what
// the diagnostic names, because a reader looking at the field sees the
// constant and nothing about the enum.
type enumBound struct {
	enum    string
	sel     string // "Max" or "Count"
	through string // "" where the bound names the enum itself
}

// enumBoundProvenance follows a bound expression to the enum it folds from,
// through named constants and through constant arithmetic at any depth
// (SPEC.md §4.2). A bound that folds from an enum is ONE BOUND however it is
// spelled, so this walk and not the field's own text is what the refusal below
// reads.
//
// It looks at an ENUM alone. A `flags` declaration carries Max and Count too
// and is refused as a bound by its own rule, a mask naming no single slot
// (§2.4, §11); a bound that reaches neither is a plain positional array and
// stands wherever it is spelled.
//
// `visiting` guards the constant graph. A reference cycle among constants is
// its own compile error (SPEC.md §4.2), reported where the constants resolve,
// and this walk runs over the same graph, so it carries its own guard rather
// than recursing forever on a unit that is already refused.
func (c *checker) enumBoundProvenance(e ast.Expr, visiting map[string]bool) (enumBound, bool) {
	switch e := e.(type) {
	case *ast.MaxExpr:
		if e.Sel != "Max" && e.Sel != "Count" {
			return enumBound{}, false
		}
		if _, isEnum := c.astDecls[e.Enum].(*ast.EnumDecl); !isEnum {
			return enumBound{}, false
		}
		return enumBound{enum: e.Enum, sel: e.Sel}, true
	case *ast.IdentExpr:
		entry := c.constant[e.Name]
		if entry == nil || entry.decl == nil || visiting[e.Name] {
			return enumBound{}, false
		}
		visiting[e.Name] = true
		found, ok := c.enumBoundProvenance(entry.decl.Expr, visiting)
		delete(visiting, e.Name)
		if !ok {
			return enumBound{}, false
		}
		// the OUTERMOST constant is the one the FIELD spells, and the walk
		// returns through every level, so each assignment overwrites the
		// deeper one and the name a reader can see is what survives
		found.through = e.Name
		return found, true
	case *ast.ParenExpr:
		return c.enumBoundProvenance(e.X, visiting)
	case *ast.UnaryExpr:
		return c.enumBoundProvenance(e.X, visiting)
	case *ast.BinaryExpr:
		if found, ok := c.enumBoundProvenance(e.X, visiting); ok {
			return found, true
		}
		return c.enumBoundProvenance(e.Y, visiting)
	}
	return enumBound{}, false
}

// refusePositionalEnumBound is the diagnostic, one shape for both scopes.
//
// AN ORDINAL-INDEXED ARRAY IS A POSITIONAL VOCABULARY AND A TABLE MAY HAVE
// ONLY ONE. Such a field carries its elements by position, so inserting a
// variant in the middle of E lands every later element one slot off, in every
// file already written, with nothing on the wire that could say so: the silent
// class §4.1 names. Keyed slots ride by NAME (§3.2), so a middle insert moves
// no slot, and refusing the positional spelling here is what leaves `flags` as
// the only positional vocabulary a table body and a union arm have, and
// therefore the only exception the reachability-scoped projection needs
// (SPEC.md §3.1).
//
// `where` names the declaration and the field or the arm, `scope` names the
// body the rule is stated on, and `reach` names the table that pulled a union
// in. The diagnostic names the field, the enum the bound folds from, the
// constant where the bound reaches the enum through one, and the fix.
func (c *checker) refusePositionalEnumBound(where, scope, reach string, f *ir.Field) {
	// THE BOUND'S OWN SHAPES ONLY: `[N]T`, the fixed positional array. `[E]T`
	// is the table form and carries a KeyEnum, `[..N]T` and `[A..N]T` are a
	// count rather than an enum's extent, and `[]T` has no bound at all.
	if f.Array != ir.ArrayFixed || f.KeyEnum != "" || f.ArrayExpr == nil {
		return
	}
	found, ok := c.enumBoundProvenance(f.ArrayExpr, map[string]bool{})
	if !ok {
		return
	}
	fold := ""
	if found.through != "" {
		fold = fmt.Sprintf(", because the bound %s folds from %s.%s", found.through, found.enum, found.sel)
	}
	elem := ir.TableTypeSpelling(f)
	c.errf(f.ArrayExpr.ExprPos(), "%s: [%s]%s is refused in %s%s%s. An ordinal-indexed array is a POSITIONAL vocabulary, so inserting a variant in the middle of %s lands every later element one slot off in every file already written, with nothing on the wire that could say so. Instead spell it [%s]%s, whose slots ride by variant name (docs/SPEC-TABLES.md §2.4, §11)",
		where, exprSpelling(f.ArrayExpr), elem, scope, fold, reach, found.enum, found.enum, elem)
}

// checkPositionalEnumBoundInClosure is §2.4's refusal over the scope §2.4
// states: A TABLE BODY AND A UNION ARM. It runs once the closure is known,
// which is what lets a union arm's diagnostic name the table that reaches the
// union, the way #572's closure refusal names the edge that pulled a `type`
// in. An arm is a field line (§2.6), so the arm and the table body's own field
// take one rule and one sentence.
//
// THE WALK IS THE FENCE. It starts at TABLE bodies and descends UNIONS alone,
// so a `type` a table closure reaches is never visited. That case has two
// answers that exclude each other, refusing the shape in every reached `type`
// or keying the table wire for an enum-extent array wherever it is declared,
// and it is ruled on schema#606. Widening this walk to `type` bodies is the
// one edit that decides the ruling, so it is not made here.
//
// The PACKET WIRE is untouched for the same reason: a `type` no table reaches
// is not in the closure, its `[E.Max]T` is a plain positional array whose
// extent every fact of projects, and the connect gate covers a variant insert
// because the protocol id moves with the spelling (SPEC.md §3.1).
func (c *checker) checkPositionalEnumBoundInClosure() {
	// SORTED: map iteration order must not shuffle the diagnostics run to
	// run, and the sort is also what makes the reaching table a STABLE choice
	// where two tables carry one union.
	roots := make([]string, 0, len(c.tables))
	for name := range c.tables {
		roots = append(roots, name)
	}
	sort.Strings(roots)

	// ONE FIELD, ONE DIAGNOSTIC. A union two tables carry resolves once, so
	// its arms are the same fields under both edges and a second visit would
	// refuse one declaration twice.
	reported := map[*ir.Field]bool{}
	for _, name := range roots {
		st := c.tables[name]
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if reported[f] {
				continue
			}
			reported[f] = true
			c.refusePositionalEnumBound(fmt.Sprintf("table %s: field %s", name, f.Name), "a table body", "", f)
			if un, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
				c.checkUnionArmBounds(un, name, f.Name, reported, map[*ir.Union]bool{})
			}
		}
	}
}

// checkUnionArmBounds refuses the same bound in the arms of a union a table
// body carries, descending an arm that is itself a union: such an arm brings
// its own arms onto the table wire (docs/SPEC-TABLES.md §2.6), so it reaches
// the closure through the same edge and takes the same rule.
func (c *checker) checkUnionArmBounds(un *ir.Union, table, edge string, reported map[*ir.Field]bool, seen map[*ir.Union]bool) {
	if seen[un] {
		return
	}
	seen[un] = true
	reach := fmt.Sprintf(", and table %s's field %s reaches %s", table, edge, un.Name)
	for _, v := range un.Variants {
		f := v.F
		if f == nil {
			continue // a PAYLOAD-FREE arm carries no bound (SPEC §4.8)
		}
		if inner, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
			c.checkUnionArmBounds(inner, table, edge, reported, seen)
			continue
		}
		if reported[f] {
			continue
		}
		reported[f] = true
		c.refusePositionalEnumBound(fmt.Sprintf("union %s: arm %s", un.Name, f.Name), "a union arm", reach, f)
	}
}
