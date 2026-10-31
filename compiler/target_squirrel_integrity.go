// The Squirrel target carries the `integrity` trailer (docs/SPEC-TABLES.md
// §2.14), so the central refusal in generate.go does not block it. The carry
// joins the registration carries_squirrel.go makes, so the target's
// registerCarries call stays the one source of its identity. The descriptor
// key the variable-form runtime reads is emitted by squirreltable's own
// emitter (squirreltable.go), not here.
package compiler

func init() {
	registerCarried("squirrel", carryIntegrity) // §2.14
}
