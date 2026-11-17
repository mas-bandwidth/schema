package compiler

// THE WIDENING RULE (docs/SPEC-TABLES.md §4) registers no construct in the §2
// carry vocabulary: the keys in compiler/carries.go name optional surfaces
// (timestamp, required, forms, integrity, text, arrays, optional closures,
// optional maps), and a kind that grew since the writer is a read rule, not a
// declaration the compiler refuses. The table emitter carries the kind-pair
// ladder directly in internal/codegen/pythontable/runtime/_schema/
// kind_array.py (`_widens` and the widened-width read), and the field, union
// arm, keyed array, optional array and map key sites call it, so pythonCarried
// gains no line. A key outside compiler/carries.go's carryConstructs would
// refuse the construct on every other leg.
//
// TestPythonVtWidening holds every manifest report row the engine widens,
// with its negative control.
