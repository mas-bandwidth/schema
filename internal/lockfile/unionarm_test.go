package lockfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// TestUnionArmTablesPassTheirOwnLineageLayouts pins the four tables that hold
// the Origin or EditBody union — Edit, InsertText, ToolMessage and Transaction
// — whose arms are DIRECT FIELDS (pid uint32, note string(24), marks, blob)
// rather than nested records. The fixed-form corpus build once refused exactly
// these four lineages with layout_size_mismatch, and the suspects were the
// validator's union rule (kind 15, size = an ordinal width plus the widest arm)
// and its counted-array rule (kind 14, size = a whole number of elements behind
// a count or not). Both are correct on this base, so this test pins the fact
// directly: every one of the four tables' locked lineage layouts passes
// ValidateLayout against §1.1 (docs/FIXED-FORM-ALGORITHM.md), and a regression
// in checkPostChildren's union or counted-array arm fails here by name
// (docs/SPEC-TABLES.md §2.10).
func TestUnionArmTablesPassTheirOwnLineageLayouts(t *testing.T) {
	t.Parallel()
	path := filepath.Join("../../tables/messages", lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tables/messages carries a committed lock: %v", err)
	}
	lock, err := lockfile.Parse(path, data)
	if err != nil {
		t.Fatalf("the committed lock parses: %v", err)
	}
	for _, name := range []string{"Edit", "InsertText", "ToolMessage", "Transaction"} {
		tbl := lock.Table(name)
		if tbl == nil {
			t.Fatalf("the lock carries fixed table %s", name)
		}
		for _, e := range tbl.Lineage {
			if reason := lockfile.ValidateLayout(e.Layout); reason != "" {
				t.Errorf("fixed table %s lineage wire=0x%016x carries malformed layout: %s", name, e.Wire, reason)
			}
		}
	}
}
