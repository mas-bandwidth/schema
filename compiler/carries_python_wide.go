//go:build schema_leg_python

package compiler

// The wide table kinds — fixed(I, F), ufixed(I, F), int128 and uint128 — are
// SCALAR KINDS of the table wire (docs/SPEC-TABLES.md §3, kinds 18-29), not
// one of the §2 carry constructs in compiler/carries.go: a unit uses them by
// declaring a field, and there is no optional spelling for the compiler to
// refuse or register. The table frame carries them as its own runtime module,
// _schema/kind_wide.py, exactly as it carries bool and the 8- to 64-bit
// integers (docs/PORT-PYTHON.md, THE TABLE FRAME, "Wide scalars"). So this
// file adds no carryPython line: the Python carry set stays empty for the
// table frame, and the shared carry checks before table dispatch are unchanged.
//
// A later §2 construct the wide kinds acquire would register in this file:
//
//	var _ = carryPython(<construct>)
