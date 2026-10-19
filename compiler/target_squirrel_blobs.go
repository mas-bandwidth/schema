//go:build schema_leg_squirrel

// Squirrel CARRIES the byte and string buffers (docs/SPEC-TABLES.md §2.5): a
// `*bytes` and a `*string` table field are pointers to a BLOB NODE held at
// exactly the bytes it was given, framed at its length on the wire and pointed
// at as the §3.1 blob record, which is also what the variable table wire's
// descriptor walk reads. The table surface already walks the construct
// (internal/codegen/squirreltable), so this file only registers squirrel as a
// carrier; it lifts no refusal in target_squirrel.go's chain, which was the
// skeleton's and is not edited here.
package compiler

// The registration is by the per-target lists compiler/builtin.go keeps:
// blobTargets for `*bytes`, registerStringCarrier for `*string` (§2.5 names
// the card record's blob, which is both). The schema-go-target-names-what-it-
// carries registry this card depends on does not name this construct at the
// base, so there is no registerCarried call to make here; when it lands, it
// replaces these two registrations and no list is appended to by hand.
func init() {
	blobTargets = append(blobTargets, "squirrel")
	registerStringCarrier("squirrel")
}
