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
