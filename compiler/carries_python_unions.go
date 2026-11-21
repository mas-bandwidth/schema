package compiler

// A table-side union has no key in compiler/carries.go's §2 construct
// vocabulary (docs/SPEC-TABLES.md §2.6): the wire format is not optional, so
// there is nothing for a target to register as a carry. The union form is
// claimed by the compiler's table-arm list (target_python.go's registerBuiltin
// call, which also passes refuseVoidArms) and the union-array list below, so
// the shared refusal chain refuseUnported passes both by registration. Its
// codec is internal/codegen/pythontable/runtime/_schema/kind_union.py.
//
// The registration covers every array of unions in a table closure: a union
// with a table or general arm, and a PACKET union whose arms are all packet
// types, which the table frame registers through the same UnionDesc
// (pythontable.go, emitUnionDescriptor) and carries through the same codec on
// the variable, fixed, message and text forms (test/tables/packetunions).
func init() {
	unionArrayTargets = append(unionArrayTargets, "python")
}
