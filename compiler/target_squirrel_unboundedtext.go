//go:build schema_leg_squirrel

// Squirrel carries the unbounded-text element and map key
// (docs/SPEC-TABLES.md §2.13.1). The construct's key in the schema-go registry
// (compiler/carries_go.go) names it, so the target registers its one carried
// construct through [registerCarried] from its own file's init.
package compiler

func init() {
	registerCarried("squirrel", carryUnboundedText)
}
