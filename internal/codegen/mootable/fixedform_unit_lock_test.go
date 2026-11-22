package mootable_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The fixed form's writer lookup, which reads the unit's lock for a lineage
// entry handed in by hash alone, keeps nothing about the unit once the call
// returns: a unit the caller has dropped is collectable. A lookup keyed by the
// unit at package level would keep every unit a long-lived process generated,
// with the lock it read, for ever.
func TestMooFixedWriterLookupKeepsNoUnit(t *testing.T) {
	t.Parallel()
	older, ok := mootable.FixedLineageOf(unitFromSource(t, "package probe\nfixed table Root { a int32 = 5 }\n"), "Root")
	if !ok {
		t.Fatal("no lineage entry for the older Root")
	}
	older.Writer = nil // named by hash alone, so the writer comes from the unit's lock
	wp := func() weak.Pointer[ir.Unit] {
		u := unitFromSource(t, "package probe\nfixed table Root { a int64 = 5 }\n")
		own, ok := mootable.FixedLineageOf(u, "Root")
		if !ok || own.Wire == older.Wire {
			t.Fatal("the fixture's two layouts are not told apart")
		}
		if _, err := mootable.GenerateLineage(u, map[string][]mootable.FixedLineageEntry{"Root": {older}}); err != nil {
			t.Fatalf("GenerateLineage: %v", err)
		}
		return weak.Make(u)
	}()
	for i := 0; i < 20 && wp.Value() != nil; i++ {
		runtime.GC()
	}
	if wp.Value() != nil {
		t.Fatal("a unit the caller dropped is still reachable after its fixed form was generated: the writer lookup outlived its call")
	}
}
