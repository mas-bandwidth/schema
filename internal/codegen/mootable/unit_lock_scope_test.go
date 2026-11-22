package mootable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v3/internal/lockfile"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// THE WRITER LOOKUP IS SCOPED TO ONE GENERATION. A lineage entry handed in
// without its writer is resolved from the unit's lock, and that lock is read
// for the generation that asks, never kept past it: a long-lived process that
// generates the same *ir.Unit again sees the lock as it is now, and holds no
// unit or lock once the call returns. Pure generation: no interpreter runs.
func TestMooWriterLookupIsScopedToTheGeneration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	write := func(src string) {
		if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	load := func() *ir.Unit {
		u, err := compiler.New().Load([]string{schema})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	lock := func(u *ir.Unit) {
		if _, _, err := lockfile.Update(u, []string{schema}); err != nil {
			t.Fatal(err)
		}
	}
	// two builds of one fixed table: the older one declared `a` ranged, so
	// its lineage entry carries writer ranges once its writer is resolved
	write("package probe\nfixed table X\n{\n    a int32 | min = 0, max = 10\n}\n")
	lock(load())
	write("package probe\nfixed table X\n{\n    a int32 | min = 0, max = 10\n    b int32\n}\n")
	u := load()
	lock(u)
	opened, ok, err := lockfile.Open([]string{schema})
	if err != nil || !ok {
		t.Fatalf("the lock does not open: %v", err)
	}
	var entries []mootable.FixedLineageEntry
	for _, e := range lockfile.Lineage(opened, u.Tables["X"].WireName()) {
		// the writer left out, so generation resolves it from the lock
		entries = append(entries, mootable.FixedLineageEntry{Wire: e.Wire, Layout: e.Layout, Record: e.Record + 8, Retired: e.Retired, Reason: e.Reason})
	}
	if len(entries) != 2 {
		t.Fatalf("the lock holds %d lineage entries for X, want 2", len(entries))
	}
	lineage := map[string][]mootable.FixedLineageEntry{"X": entries}
	gen := func(u *ir.Unit) []byte {
		files, err := mootable.GenerateLineage(u, lineage)
		if err != nil {
			t.Fatal(err)
		}
		return files["ProbeTable.moo"]
	}

	// the lock moved away: the older entry has no writer to resolve
	lockPath := filepath.Join(dir, lockfile.FileName)
	if err := os.Rename(lockPath, lockPath+".away"); err != nil {
		t.Fatal(err)
	}
	without := gen(u)
	if err := os.Rename(lockPath+".away", lockPath); err != nil {
		t.Fatal(err)
	}
	fresh := gen(load())
	if bytes.Equal(without, fresh) {
		t.Fatal("the lock's writer changes nothing in the output: the test cannot see the lookup")
	}
	if again := gen(u); !bytes.Equal(again, fresh) {
		t.Error("generating the same unit again, with its lock back, writes what the lockless generation wrote: the writer lookup outlived the call that made it")
	}
}
