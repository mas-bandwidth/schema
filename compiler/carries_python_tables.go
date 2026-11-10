//go:build schema_leg_python

package compiler

// THE TABLE FRAME carries no construct from the §2 carry vocabulary yet
// (docs/SPEC-TABLES.md §2.3, §2.8, §2.11-§2.14). Its fields are bool, the
// full-width integers, by-value tables and *T pointers — the constructs every
// table has — so there is no optional spelling for it to register. A later
// table card adds its own line here, in the skeleton's pattern:
//
//	var _ = carryPython(carryForms)
//
// and the registration stays empty until that card honours the construct.
