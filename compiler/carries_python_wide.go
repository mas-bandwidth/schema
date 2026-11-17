package compiler

// THE WIDE TABLE KINDS (int128, uint128, fixed(I, F) and ufixed(I, F)) register
// no construct here: docs/SPEC-TABLES.md §2's carry vocabulary has a key per
// optional surface (timestamp, required, forms, integrity, text, arrays,
// optional closures), not per scalar kind. The table core's integer codec
// carries these widths (internal/codegen/pythontable/runtime/_schema/
// kind_int.py), and TestPythonVtWide proves them, so pythonCarried gains no
// line. A key outside compiler/carries.go's carryConstructs would refuse the
// construct on every other leg.
