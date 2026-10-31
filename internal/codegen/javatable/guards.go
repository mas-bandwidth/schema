package javatable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// variableGuard is the Java condition under which a guarded field rides
// (SPEC §4.5): the conjunction of every enclosing `if` on a bool field, each
// negated under `if !cond` or in an `else`. An unguarded field answers "".
//
// The wire elides a field whose guard does not hold (docs/SPEC-TABLES.md §4),
// so the numbering, the save and the text writer all skip it, exactly as the
// engine does (internal/tablewire encodeBodyFields and its numbering walk,
// internal/tabletext's writer): a load keeps what the wire carried, and the
// next save says the same thing a text written from that value says.
func variableGuard(st *ir.Struct, f *ir.Field) string {
	var found []string
	var walk func(items []ir.Item, cond []string) bool
	walk = func(items []ir.Item, cond []string) bool {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if item.F == f || item.F.Name == f.Name {
					found = cond
					return true
				}
			case *ir.Branch:
				member := "v." + guardMember(st, item.Cond)
				pos, neg := member, "!"+member
				if item.Neg {
					pos, neg = neg, pos
				}
				if walk(item.Then, append(cond[:len(cond):len(cond)], pos)) {
					return true
				}
				if walk(item.Else, append(cond[:len(cond):len(cond)], neg)) {
					return true
				}
			}
		}
		return false
	}
	walk(st.Items, nil)
	return strings.Join(found, " && ")
}

// guardMember is the Java member of the bool field a branch reads.
func guardMember(st *ir.Struct, name string) string {
	for _, g := range st.Fields {
		if g.Name == name {
			return varMember(g)
		}
	}
	return name
}

// openGuard and closeGuard bracket one field's emission in its guard.
func openGuard(b *strings.Builder, st *ir.Struct, f *ir.Field) bool {
	cond := variableGuard(st, f)
	if cond == "" {
		return false
	}
	// written compact, so the canonical pass lays the block it opens out afresh
	// at the formatter's indentation (java.Canonical's relayout)
	b.WriteString("        if(" + strings.ReplaceAll(cond, " && ", "&&") + "){\n")
	return true
}

func closeGuard(b *strings.Builder, open bool) {
	if open {
		b.WriteString("        }\n")
	}
}
