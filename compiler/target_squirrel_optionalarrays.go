// The Squirrel carrier of the OPTIONAL ARRAY form (docs/SPEC-TABLES.md §2.3):
// `?[N]T` and `?[..N]T` ride as the plain array's framing plus the one-byte
// presence companion, which squirreltable.Generate already walks, so the
// target registers as a carrier from its own file's init rather than growing
// target_squirrel.go's chain (compiler/tablesoptionalarrays.go).
//
// The registration is the per-target list [registerOptionalArrayCarrier]: the
// construct registry this card depends on names no registerCarried key for this
// form, so squirrel joins optionalArrayTargets beside cpp, c, cs and go.
package compiler

func init() {
	registerOptionalArrayCarrier("squirrel")
}
