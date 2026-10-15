package elixirtable

import "github.com/mas-bandwidth/schema/v2/ir"

// refuseCardUnsupportedVariableConstructs remains the variable-form backend
// boundary while the card slice grows to the complete table field grammar.
// SPEC-TABLES §2.3, §2.6, §3 and §4.12 are now handled by descriptors and
// the variable runtime; unsupported cases are refused at the compiler gates.
func refuseCardUnsupportedVariableConstructs(_ *ir.Unit) error { return nil }

// closureHoldsFixedTable reports whether this unit also declares a fixed
// table. The variable form still emits a codec for that closure (§2.3, §3).
func closureHoldsFixedTable(u *ir.Unit, closure map[string]bool) bool {
	variable := ir.VariableTables(u)
	for name := range closure {
		if st := u.Tables[name]; st != nil && !variable[name] {
			return true
		}
	}
	return false
}
