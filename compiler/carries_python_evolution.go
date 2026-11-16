package compiler

// THE EVOLUTION KINDS — string, bytes and flags defaults, a table, table
// field and enum-variant rename under `was` — carry no construct from the §2
// carry vocabulary (docs/SPEC-TABLES.md §2.3, §2.8, §2.11-§2.14). The keys in
// compiler/carries.go name the constructs a target carries; a declared value
// default and a `was` alias are not among them, so there is no key to register
// and this card adds no line. The table emitter carries the kinds directly
// (internal/codegen/pythontable); the driver's whole-unit refusal lists stay
// as they are until the rows cards land, because their unit also holds a
// packet type or a union this kind card does not carry.
//
// The table frame's empty §2 registration stays in compiler/carries_python.go;
// a later kind card adds its line in its own file, in the skeleton's pattern:
//
//	var _ = carryPython(carryForms)
