//go:build schema_leg_python

package compiler

// THE ID-TABLE WIRE's enum and escape kinds carry no key in
// compiler/carries.go's carryConstructs (docs/SPEC-TABLES.md §2, §3): the
// table-wire kinds are the frame's own, not a §2 construct, so this card
// registers nothing. The enum field kind, the escape (31) and the
// payload-free kind (32) are held by internal/codegen/pythontable's
// TestPythonVtIdtable and its negative control. A later Python card that
// carries a §2 construct adds its own carries_python_<feature>.go file with a
// carryPython line, as the skeleton's pattern says.
