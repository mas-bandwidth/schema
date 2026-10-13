package rusttable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseCardUnsupportedVariableConstructs is docs/SPEC-TABLES.md §11's refusal
// at generation: it refuses, BY NAME, every variable-class construct this
// backend does not carry on the table wire, while the variable class it does
// carry (table roots and pointers *T, the flat node table, *string and *bytes
// blobs, bool and every int/uint/float/bits width, string(N) and bytes(N),
// enums, and unbounded arrays of scalar, string, enum and pointer elements)
// passes. The rest of the class stays absent and is refused rather than
// mis-encoded or silently dropped.
//
// A construct this backend does not reach is named as `<table>.<field>: <what>`
// so the message names the table, the field and the construct together, which
// is what a refusal by name owes (§11).
//
// SCOPED TO THE CARD'S VARIABLE-WIRE PATH, not to GenerateLineage: it guards
// [cardVariableModules] — the card unit's own module emitter, which runs the
// refusal FIRST (§11) — while [scopedVariableModules], GenerateLineage's entry
// point, leaves a variable table whose fields the card boundary does not carry
// ABSENT rather than refusing the unit whole. The scoping is what keeps the
// fixed-form corpus (rusttable_test.go's `pointered`) green: it declares
// variable tables with pointers and unions the variable wire does not carry,
// and refusing them at the top of GenerateLineage breaks the fixed form's own
// tests.
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

// refuseCardUnsupportedFields walks one variable table's fields in declared
// order and returns the first construct the card slice does not carry.
func refuseCardUnsupportedFields(st *ir.Struct) error {
	for _, f := range st.Fields {
		if why := cardUnsupportedVariableConstruct(f); why != "" {
			return fmt.Errorf("rust variable form: %s.%s: %s (docs/SPEC-TABLES.md §11)",
				st.Name, f.Name, why)
		}
	}
	return nil
}

// cardUnsupportedVariableConstruct returns the construct's name on a field the
// variable class does not carry, and "" on every field it does: a scalar of any
// int/uint/float/bits width, a string(N), a bytes(N), a *string or *bytes blob,
// an enum, a pointer to a table, or an unbounded array of scalar, string, enum
// or pointer elements.
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
