// Squirrel CARRIES the byte and string buffers (docs/SPEC-TABLES.md §2.5): a
// `*bytes` and a `*string` table field are pointers to a BLOB NODE held at
// exactly the bytes it was given, framed at its length on the wire and pointed
// at as the §3.1 blob record, which is also what the variable table wire's
// descriptor walk reads. The table surface already walks the construct
// (internal/codegen/squirreltable), so this file only registers squirrel as a
// carrier; every refuser that names the construct reads that registration, so
// target_squirrel.go's chain (through refuseUnported) lets it through once
// this init has run.
package compiler

// The registration is by the per-target lists compiler/builtin.go keeps:
// blobTargets for `*bytes`, stringTargets for `*string` (§2.5 names the card
// record's blob, which is both). These are the two lists registerBuiltin's
// blobs flag appends to; the squirrel target registers with that flag off
// (its table surface lives in these feature files), so it joins both here and
// the two lists still agree. The carries registry (compiler/carries.go) names
// no key for this construct, so there is no registerCarried call to make.
func init() {
	blobTargets = append(blobTargets, "squirrel")
	stringTargets = append(stringTargets, "squirrel")
}
