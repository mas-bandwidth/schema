package js

// GlobalNames is every JavaScript and TypeScript global the generated modules
// and declarations reference, packet and table alike (internal/codegen/js,
// internal/codegen/jstable). Each is written globalThis.X — in a module, and
// in a .d.ts as a type — because a schema type of the same name becomes a
// class in that module's scope and would capture the plain name. The checker
// claims globalThis in every unit (internal/check), so nothing shadows the
// qualifier.
//
// It is the one list: compiler/jsglobals_test.go scans the generated output
// of every corpus unit and fails on a global named plainly or one missing
// here, and generates a unit declaring every name below to run under node
// and tsc --strict.
var GlobalNames = []string{
	"Array", "ArrayBuffer", "BigInt", "BigUint64Array", "Boolean", "DataView", "Date", "Error",
	"Float32Array", "Float64Array", "Infinity", "Int32Array", "Map", "Math", "Number", "Object",
	"RangeError", "Readonly", "Set", "String", "TypeError", "Uint32Array", "Uint8Array", "WeakMap",
	"parseInt",
}
