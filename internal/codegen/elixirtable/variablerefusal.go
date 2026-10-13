package elixirtable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseCardUnsupportedVariableConstructs is docs/SPEC-TABLES.md §11's refusal
// at generation: it refuses, BY NAME, every variable-class construct this
// backend does not carry on the table wire, while the card record's slice
// (plain `table` roots and tables as list elements; bool/int32/int64/enum with
// their defaults and ranges; string(N); *string; and []T lists of tables)
// passes. The rest of the class stays absent and is refused rather than
// mis-encoded or silently dropped.
//
// A construct this backend does not reach is named as `<table>.<field>: <what>`
// so the message names the table, the field and the construct together, which
// is what a refusal by name owes (§11).
func refuseCardUnsupportedVariableConstructs(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for n := range variable {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			continue
		}
		if err := refuseCardUnsupportedFields(st); err != nil {
			return err
		}
	}
	return nil
}

// closureHoldsFixedTable reports whether the unit's table closure holds a
// `fixed table` (docs/SPEC-TABLES.md §2.2). Such a unit is served by the block,
// cook and fixed forms, so refuseCardUnsupportedVariableConstructs — the
// variable form's own boundary — does not bind it (docs/SPEC-TABLES.md §11):
// this backend emits no form-`1` byte today (schema#515), so a variable table
// beside a fixed one is not mis-encoded by anything the leg writes.
func closureHoldsFixedTable(u *ir.Unit, closure map[string]bool) bool {
	variable := ir.VariableTables(u)
	for name := range closure {
		if st := u.Tables[name]; st != nil && !variable[name] {
			return true
		}
	}
	return false
}

// refuseCardUnsupportedFields walks one variable table's fields in declared
// order and returns the first construct the card slice does not carry.
func refuseCardUnsupportedFields(st *ir.Struct) error {
	for _, f := range st.Fields {
		if why := cardUnsupportedVariableConstruct(f); why != "" {
			return fmt.Errorf("elixir variable form: %s.%s: %s (docs/SPEC-TABLES.md §11)",
				st.Name, f.Name, why)
		}
	}
	return nil
}

// cardUnsupportedVariableConstruct returns the construct's name on a field the
// card slice does not carry, and "" on every field it does: a scalar, a
// string(N), a *string blob, an enum, or an unbounded list of tables.
func cardUnsupportedVariableConstruct(f *ir.Field) string {
	switch {
	case f.IsMap():
		return "map[K]V is outside the card constructs (§2.8)"
	case f.Type.Optional:
		return "optional ?T is outside the card constructs (§2.3)"
	case f.KeyEnum != "":
		return "enum-keyed array [E]T is outside the card constructs (§2.4)"
	case f.Type.Kind == ir.TInt && f.Type.Width == 128:
		return "the 128-bit integer is outside the card constructs (§15)"
	case f.Type.Kind == ir.TFixed:
		return "the fixed-point kind is outside the card constructs (§15)"
	}
	if _, ok := f.Type.Ref.(*ir.Union); ok {
		return "union arms are outside the card constructs (§2.6)"
	}
	return ""
}
