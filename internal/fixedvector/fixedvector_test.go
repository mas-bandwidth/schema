package fixedvector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	manifest := "file=old_range_widen.bin row=range_widen side=old root=RangeWiden records=1 values=r0.lead=1,r0.v=100,r0.trail=2\n" +
		"file=hostile_range_widen.bin row=range_widen side=hostile root=RangeWiden records=1 forged=r0.v@104=150 expect=r0.lead=1,r0.v=100,r0.trail=2,clamped=1 values=r0.lead=1,r0.v=100,r0.trail=2\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := Read(dir, RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := e.Int("r0.v"); err != nil || v != 100 {
		t.Fatalf("r0.v = %d (%v), want 100", v, err)
	}
	if e.Clamped != 1 {
		t.Fatalf("clamped = %d, want 1", e.Clamped)
	}
	if _, err := Read(dir, "old_range_widen.bin"); err == nil {
		t.Fatal("a line with no expect= read as a vector")
	}
	if _, err := Read(dir, "absent.bin"); err == nil {
		t.Fatal("a file the manifest does not name read as a vector")
	}
}
