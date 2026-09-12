package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SchemaLockFileName is the lock's name in a unit directory
// (docs/SPEC-TABLES.md §2.10).
const SchemaLockFileName = lockfile.FileName

// SchemaLockText renders the unit's SCHEMA LOCK (docs/SPEC-TABLES.md §2.10):
// every FIXED table's field sequence in declared order — each entry carrying
// the field's wire id, kind, width in the record, default declared or
// implicit, declared range and resolution, `?` and `deprecated` marker, the
// named type a slot holds, an array's element kind and width, and the enum
// an enum-keyed array is keyed by
// — then a block for every type those tables REACH: a nested `type` as a
// record of its own, and an `enum`, a `flags` mask or a `union` as its value
// list in declared order, a union arm carrying its payload type. Each block
// carries the hash of its lines. One fact per line, stable and diffable,
// exactly as the committed file holds it.
func SchemaLockText(u *ir.Unit) string {
	return lockfile.Render(u).Text()
}

// RetireSchemaLock is the lock's ONE NON-APPEND EDIT
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4, §11.5): `target` is a locked
// fixed table, or one of its layouts as `Table@0x<hash>`, and the reason is
// required because a retirement DECLARES that no reader will serve that layout
// again. Nothing is removed either way — a retired lineage entry and a retired
// table's whole block stay in the file forever; what the mark buys is
// `layout_unsupported` instead of `layout_newer` for the entry, and permission
// to drop the declaration for the table.
func RetireSchemaLock(u *ir.Unit, paths []string, target, reason string) (path string, rewrote bool, err error) {
	return lockfile.Retire(u, paths, target, reason)
}

// SchemaLockWarnings is the committed lock's advisories — today the bill's
// §11.6 bound on how many layouts one fixed table has had. They are warnings
// and never refusals: COMPILE lays one plan per supported layout down as static
// data in every leg, so a long lineage is a cost a person should see and decide
// about, not a break.
func SchemaLockWarnings(paths []string) []string {
	return lockfile.Warnings(paths)
}

// UpdateSchemaLock writes the unit's committed lock — the schema.lock beside
// its schema files. It is the only writer of that file, and it only ever
// appends entries, adds tables and flips `deprecated` on: it runs the check's
// own comparison first and refuses every break the check refuses. The one
// thing it accepts and the check does not is a declaration the lock has not
// caught up to — writing that down is what this command is for.
//
// It is idempotent — when the lock is already current the file is untouched
// and rewrote is false.
//
// paths are the unit's *.schema files, as [GatherPaths] returns them; the
// lock lives in their directory.
func UpdateSchemaLock(u *ir.Unit, paths []string) (path string, rewrote bool, err error) {
	return lockfile.Update(u, paths)
}

// MergeSchemaLockAtMerge is the lock run AT A MERGE COMMIT
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §8a.1, §11.3): `parentLocks` are the
// `schema.lock` files the merge's parents committed, one per parent, and the
// merged declaration is held to EACH of them.
//
// It differs from [UpdateSchemaLock] in exactly one clause: a parent's fields
// are paired with the merged record's BY WIRE ID rather than by position, so the
// merged record may carry both branches' appends interleaved in any order. Every
// other rule is the same — every parent's field is still present, deprecated in
// place where it is finished, and widened and never narrowed — and WITHIN ONE
// BRANCH `schema lock`'s strict prefix rule is untouched.
//
// The file it writes carries both parents' lineages concatenated in the order
// the parents were given, deduped by wire hash, then the merged layout's own
// entry; a retirement on either side stands, which makes the merged floor the
// higher of the two.
func MergeSchemaLockAtMerge(u *ir.Unit, paths []string, parentLocks []string) (path string, rewrote bool, err error) {
	return lockfile.Merge(u, paths, parentLocks)
}
