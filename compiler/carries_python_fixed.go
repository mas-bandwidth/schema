//go:build schema_leg_python

package compiler

// The Python FIXED FORM's carried constructs (docs/PORT-PYTHON.md, THE LAYERS:
// P5; internal/codegen/python/fixed.go): wstring(N) in a fixed table (kind 33),
// string and bytes defaults in a table, and `was` renames, whose ids are the
// source names' (ir.TableFieldWireId, VariantWireName, WireName). Each is held
// by the fixed-form corpus (internal/codegen/python/fixedform_test.go: fxw,
// fx1, fx2, pack).
func init() {
	tableWideTextTargets["python"] = true
	valueDefaultTargets = append(valueDefaultTargets, "python")
	wasRowTargets = append(wasRowTargets, "python")
}
