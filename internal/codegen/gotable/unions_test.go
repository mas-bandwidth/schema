package gotable

import "testing"

// TestGoUnionArmNamedCountBuilds holds the table union tag to the tag shape: a
// member named Count is an ordinary arm, so the tag carries no Count member
// beside it (docs/SPEC-TABLES.md §4.2, §4.11).
func TestGoUnionArmNamedCountBuilds(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, `package probe
type Inner { n int32 }
union Pick
{
    inner Inner
    count int32
}
fixed table Root { pick Pick }
`, `package probe
import "testing"

type Inner struct {
	N int32
}

func TestGoUnionArmNamedCountBuilds(t *testing.T) {
	if PickTypeMax != PickTypeCount {
		t.Fatalf("PickTypeMax is %d, want the last arm PickTypeCount (%d)", PickTypeMax, PickTypeCount)
	}
	var v Root
	RootReset(&v)
	v.Pick.Type = PickTypeCount
	v.Pick.Count = 7
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save wrote %d, want %d", n, len(buf))
	}
	back := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load returned %d, report %+v", n, r)
	}
	if back[0].Pick.Type != PickTypeCount || back[0].Pick.Count != 7 {
		t.Fatalf("count arm round-trip: %+v", back[0].Pick)
	}
}
`)
}
