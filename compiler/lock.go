package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SchemaLockFileName is the lock's name in a unit directory
// (docs/SPEC-TABLES.md §2.10).
const SchemaLockFileName = lockfile.FileName

// SchemaLockText renders the unit's SCHEMA LOCK (docs/SPEC-TABLES.md §2.10):
// every FIXED table's field sequence in declared order, each entry carrying
// the field's wire id, kind, width in the record and `deprecated` marker,
// plus the hash of the sequence. One fact per line, stable and diffable,
// exactly as the committed file holds it.
func SchemaLockText(u *ir.Unit) string {
	return lockfile.Render(u).Text()
}

// UpdateSchemaLock writes the unit's committed lock — the schema.lock beside
// its schema files. It is the only writer of that file, and it only ever
// appends entries, adds tables and flips `deprecated` on: it runs the check's
// own comparison first and refuses to write a lock the check would refuse.
//
// It is idempotent — when the lock is already current the file is untouched
// and rewrote is false.
//
// paths are the unit's *.schema files, as [GatherPaths] returns them; the
// lock lives in their directory.
func UpdateSchemaLock(u *ir.Unit, paths []string) (path string, rewrote bool, err error) {
	return lockfile.Update(u, paths)
}
