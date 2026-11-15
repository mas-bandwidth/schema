//go:build schema_leg_python

package compiler

// THE BLOCK FORM carries no construct from the §2 carry vocabulary
// (docs/SPEC-TABLES.md §2.3, §2.8, §2.11-§2.14). Its rows are the table's own
// by-value records and its out-of-line arrays its own bounded arrays, so there
// is no optional spelling for it to register. A later card that carries one
// adds its own line here, in the skeleton's pattern:
//
//	var _ = carryPython(carryForms)
