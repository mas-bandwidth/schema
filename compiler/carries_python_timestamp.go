//go:build schema_leg_python

package compiler

// SPEC.md §4.13, docs/SPEC-TABLES.md §2.11: the Python target carries the
// `timestamp` kind, an int64 count of nanoseconds since the epoch, on the
// form-1 table wire. Its text forms are the packet and table text cards'
// (py-json-2, py-handtext-2); the fixed form is py-fixed-form-2's.
var _ = carryPython(carryTimestamp)
