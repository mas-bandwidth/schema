package fixedvector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	manifest := "file=old_range_widen.bin row=range_widen side=old root=RangeWiden records=1 values=r0.lead=1,r0.v=100,r0.trail=2\n" +
		"file=hostile_range_widen.bin row=range_widen side=hostile root=RangeWiden records=1 forged=r0.v@104=150 values=r0.lead=1,r0.v=100,r0.trail=2\n" +
		"file=landed.bin row=range_widen side=hostile root=RangeWiden records=1 forged=r0.v@104=150 expect=r0.lead=1,r0.v=100,r0.trail=2 values=r0.lead=1,r0.v=100,r0.trail=2\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// the one read: the reference refused the forged bytes, so the line
	// carries no expect= and the answer is the refusal
	e, err := Read(dir, RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Refused || len(e.Paths) != 0 {
		t.Fatalf("a forged line with no expect= answered %+v, want Refused and no landed values", e)
	}
	if e.ForgedPath != "r0.v" || e.ForgedAt != 104 {
		t.Fatalf("forged = %s@%d, want r0.v@104", e.ForgedPath, e.ForgedAt)
	}
	// a line that carries expect= is the values the reference landed
	e, err = Read(dir, "landed.bin")
	if err != nil {
		t.Fatal(err)
	}
	if e.Refused {
		t.Fatal("a line with expect= read as a refusal")
	}
	if v, err := e.Int("r0.v"); err != nil || v != 100 {
		t.Fatalf("r0.v = %d (%v), want 100", v, err)
	}
	leaves, err := e.Leaves()
	if err != nil || len(leaves) != 3 || leaves[1].Field != "v" || leaves[1].Index != -1 || leaves[1].Value != 100 {
		t.Fatalf("leaves = %+v (%v), want lead, v = 100, trail", leaves, err)
	}
	if _, err := Read(dir, "old_range_widen.bin"); err == nil {
		t.Fatal("a line with no forged= read as a vector")
	}
	if _, err := Read(dir, "absent.bin"); err == nil {
		t.Fatal("a file the manifest does not name read as a vector")
	}
}

func TestLeavesArray(t *testing.T) {
	e := Expect{File: "x.bin", Values: map[string]int64{"r0.vals[2]": 100, "r0.lead": 1}, Paths: []string{"r0.lead", "r0.vals[2]"}}
	leaves, err := e.Leaves()
	if err != nil || len(leaves) != 2 || leaves[1].Field != "vals" || leaves[1].Index != 2 || leaves[1].Value != 100 {
		t.Fatalf("leaves = %+v (%v), want lead and vals[2] = 100", leaves, err)
	}
	for _, bad := range []string{"r1.v", "r0.a.b", "r0.vals[x]", "r0.vals[2"} {
		if _, err := (Expect{File: "x.bin", Values: map[string]int64{bad: 1}, Paths: []string{bad}}).Leaves(); err == nil {
			t.Errorf("%s split as a root leaf", bad)
		}
	}
}

func TestWriterRangeNamesEveryVector(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range WriterRange {
		if v.File == "" || v.Name == "" || v.Row == "" || v.Root == "" || seen[v.File] || seen[v.Name] {
			t.Errorf("vector %+v is incomplete or repeated", v)
		}
		seen[v.File] = true
		seen[v.Name] = true
	}
	for _, f := range []string{RangeWidenHostile, RangeWidenArrayHostile, RangeWidenU64Hostile} {
		if !seen[f] {
			t.Errorf("%s is not in WriterRange", f)
		}
	}
}
