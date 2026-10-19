//go:build schema_leg_squirrel

package compiler

// The Squirrel target carries the `integrity` trailer (docs/SPEC-TABLES.md
// §2.14), so the central refusal in generate.go does not block it.
func init() {
	registerCarries("squirrel", carryIntegrity)
}
