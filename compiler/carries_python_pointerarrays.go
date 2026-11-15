//go:build schema_leg_python

package compiler

// POINTER ARRAYS ([]*T, [N]*T, [..N]*T, [Min..N]*T) AND TABLE FIELD GUARDS
// (docs/SPEC-TABLES.md §2.1, §2.9, §3.1, §4; docs/PORT-PYTHON.md) ride on the
// Python table leg without adding a key to carryConstructs: unbounded value
// arrays []T remain refused by refuseNonPointerLists in target_python.go until
// the value-array card lands, and neither pointer arrays nor table field
// guards is a separate carryConstructs key in compiler/carries.go.
