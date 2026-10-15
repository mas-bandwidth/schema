package gotable

import (
	"testing"
)

// TestFixedLoadAllocatesNothingPerCall holds §4.3's prefill rule on the load
// path: the hole list and the default image are per plan and computed once —
// at init, never per call. The generated probe warms the load once and asserts
// testing.AllocsPerRun(100, load) == 0 for BOTH the identity plan and a
// compiled lineage plan, so a per-call construction of either (holes or reset
// image) is red.
func TestFixedLoadAllocatesNothingPerCall(t *testing.T) {
	t.Parallel()
	old := `package oldpkg

fixed table Rec
{
	x int32 = 7
}
`
	newer := `package newpkg

fixed table Rec
{
	x int32 = 7
	y int32 = 42
}
`
	src := `package probe

import ("encoding/binary"; "testing")

func TestNoAllocsPerCall(t *testing.T) {
	// IDENTITY PLAN: a current-format file of three records.
	vals := []Rec{{X: 1, Y: 2}, {X: 3, Y: 4}, {X: 5, Y: 6}}
	need := RecFixedMeasure(int64(len(vals)))
	buf := make([]byte, need)
	if RecFixedSave(vals, buf) != need {
		t.Fatal("save")
	}
	back := make([]Rec, 4)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	if n := RecFixedLoad(back, buf, plan, &r); n != 3 {
		t.Fatalf("identity load n=%d %+v", n, r)
	}
	if a := testing.AllocsPerRun(100, func() { RecFixedLoad(back, buf, plan, &r) }); a != 0 {
		t.Fatalf("identity plan allocates %v per call", a)
	}

	// LINEAGE PLAN: an older writer's file, built by hand from the emitted lock.
	old := &RecFixedKnown[0]
	if old.Hash == RecFixedHash {
		t.Fatal("expected the lineage's first entry to be an older hash")
	}
	f := make([]byte, TableFixedHeaderBytes+4+len(old.Layout)+int(old.Record))
	f[0] = TableFixedForm
	binary.LittleEndian.PutUint64(f[TableFixedHashAt:], old.Hash)
	binary.LittleEndian.PutUint32(f[TableFixedHeaderBytes:], uint32(len(old.Layout)))
	copy(f[TableFixedHeaderBytes+4:], old.Layout)
	rec := f[TableFixedHeaderBytes+4+len(old.Layout):]
	binary.LittleEndian.PutUint64(rec, old.Hash)
	binary.LittleEndian.PutUint32(rec[8:], 777) // the old body's x
	r = TableReport{}
	if n := RecFixedLoad(back, f, plan, &r); n != 1 || back[0].Y != 42 || back[0].X != 777 {
		t.Fatalf("lineage load n=%d back=%+v %+v", n, back[0], r)
	}
	if a := testing.AllocsPerRun(100, func() { RecFixedLoad(back, f, plan, &r) }); a != 0 {
		t.Fatalf("lineage plan allocates %v per call", a)
	}
}
`
	out, err := runVersionProbe(t, newer, []string{old}, src)
	if err != nil {
		t.Fatalf("generated AllocsPerRun probe: %v\n%s", err, out)
	}
}
