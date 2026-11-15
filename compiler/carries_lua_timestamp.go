package compiler

// SPEC.md §4.13, docs/SPEC-TABLES.md §2.11: the Lua target carries the
// `timestamp` kind, an int64 of nanoseconds since the epoch, in every form the
// generator gives a table, and its text form as strict RFC 3339.
var _ = carryLua(carryTimestamp)
