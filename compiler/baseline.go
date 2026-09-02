package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TablesBaselineText renders the unit's TABLES BASELINE (SPEC-TABLES.md §18.1):
// a canonical text projection of its table closure — every closure member's
// fields with their wire ids, kinds, array shapes, evaluated defaults and
// `was` aliases, and every enum, flags and union it reaches. One fact per
// line, stable and diffable, exactly as `schema tables-baseline` prints it.
//
// It carries no history: the history section belongs to the committed file,
// and [UpdateTablesBaseline] is what writes it.
func TablesBaselineText(u *ir.Unit) string {
	return baseline.Render(u).Text()
}

// UpdateTablesBaseline rewrites the unit's committed baseline — the
// tables.baseline beside its schema files — and appends a dated entry to the
// history section inside it naming every edit that changed what already-written
// data means, and the reason.
//
// The reason is mandatory: moving the baseline is the declaration that a break
// is intentional, and an undeclared move would defeat the whole point of
// keeping the file. It is idempotent — when the projection has not moved, the
// file is untouched and rewrote is false.
//
// paths are the unit's *.schema files, as [GatherPaths] returns them; the
// baseline lives in their directory.
func UpdateTablesBaseline(u *ir.Unit, paths []string, reason string) (path string, rewrote bool, err error) {
	return baseline.Update(u, paths, reason)
}
