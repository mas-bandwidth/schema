// The Squirrel target carries UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13.1):
// the `[]string`, `[]?string`, `[..N]string` and `[..N]?string` element text
// and the `map[string]V` key. The carrier decision lives in the shared
// registry (compiler/carries.go), one registration per target; this file is
// the Squirrel leg's whole share of the rule.
package compiler

func init() {
	registerCarried("squirrel", carryUnboundedText) // §2.13.1, unbounded text element and map key
}
