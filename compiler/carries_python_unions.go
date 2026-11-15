//go:build schema_leg_python

package compiler

// A table-side union has no key in compiler/carries.go's §2 construct
// vocabulary (docs/SPEC-TABLES.md §2.6): the wire format is not optional, so
// there is nothing for a target to register. The union form is claimed by the
// compiler's table-arm and union-array target lists instead
// (compiler/target_python.go's registerBuiltin call), and its codec is
// internal/codegen/pythontable/runtime/_schema/kind_union.py.
