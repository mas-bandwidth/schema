package compiler

// A table-side union has no key in compiler/carries.go's §2 construct
// vocabulary (docs/SPEC-TABLES.md §2.6): the wire format is not optional, so
// there is nothing for a target to register as a carry. The union form is
// claimed by the compiler's table-arm list (target_python.go's registerBuiltin
// call, which also passes refuseVoidArms) and the union-array list below, so
// the shared refusal chain refuseUnported passes both by registration. Its
// codec is internal/codegen/pythontable/runtime/_schema/kind_union.py.
//
// An array of PACKET unions (a union whose arms are all packet types) still
// waits for the packet layer: pythonRefusePacketUnionArrays names it.
func init() {
	unionArrayTargets = append(unionArrayTargets, "python")
}
