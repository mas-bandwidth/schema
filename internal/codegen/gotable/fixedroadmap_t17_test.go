package gotable

import "testing"

// THE PLAN IS PARTITIONED UNGUARDED-FIRST, AND THE IDENTITY PATH EQUALS THE
// COMPILED ONE (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.2, §4.4; schema#876 rows
// go/W6 and go/P1). One fixture — a fixed table mixing UNGUARDED scalars with
// fields under a UNION'S ARMS — proves both rows.
//
// go/W6: THE PLAN IS PARTITIONED. §4.1 (line 280): "every unguarded entry
// first, then every guarded one, and the plan states where the second half
// starts". Every entry below the split is unguarded, every entry at or above it
// is guarded, and no coalesced run crosses the split (§4.2, "never across the
// split"). Asserted on BOTH the baked identity plan, whose split is the
// `Split` field the plan CARRIES, and a compiled plan, whose split is read off
// the entries at the first guarded index.
//
// go/P1: §4.4 (line 330) READ(plan, split, ...): the identity path and the
// compiled plan, over ONE populated record, land identical destination bytes.
// TestFixedFormIdentityEqualsCompiled proves the same equality of fields and
// counters, so this row pins the BYTES.
func TestFixedRoadmapGoT17PlanPartition(t *testing.T) {
	t.Parallel()

	// One fixture, shared by both rows: a table mixing unguarded scalars around
	// a union whose arms are guarded. The tail past the union is unguarded, so
	// the emitter's field order would land it behind a guarded entry — which is
	// exactly what the partition repairs.
	const fixture = `package probe
type ArmA { n int32 }
type ArmB {
    s string(8)
    m int32
}
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root
{
    head int32
    pick Pick
    tail int32
}
`

	t.Run("go/W6", func(t *testing.T) {
		runGenerated(t, fixture, `package probe
import "testing"

// partitionSplit is §4.1's partition_is_held: split is in range, every entry
// below it carries NO guard, every entry at or above it carries one, and the
// pair at the split was NOT coalesced across it. It returns the split.
func partitionSplit(t *testing.T, who string, plan []TableFixedEntry, n int32) int32 {
	t.Helper()
	if n < 0 || int64(len(plan)) < int64(n) {
		t.Fatalf("W6: %s — split is in range (n=%d)", who, n)
	}
	split := n
	for i := int32(0); i < n; i++ {
		if plan[i].Guard != tableFixedNoGuard {
			split = i
			break
		}
	}
	for i := int32(0); i < n; i++ {
		guarded := plan[i].Guard != tableFixedNoGuard
		if i < split && guarded {
			t.Fatalf("W6: %s — entry %d is below the split and carries a GUARD", who, i)
		}
		if i >= split && !guarded {
			t.Fatalf("W6: %s — entry %d is at or above the split and carries NO guard", who, i)
		}
	}
	if split > 0 && split < n {
		a, b := plan[split-1], plan[split]
		if a.Op == tableFixedCopy && b.Op == tableFixedCopy &&
			a.Guard == b.Guard && a.Arg == b.Arg &&
			a.Src+a.Size == b.Src && a.Dst+a.Size == b.Dst {
			t.Fatalf("W6: %s — the pair at the split was coalesced across it", who)
		}
	}
	return split
}

func TestGoT17W6(t *testing.T) {
	// THE IDENTITY PLAN CARRIES ITS SPLIT. A plan with a guarded half has its
	// split strictly inside, and it is the index of the first guarded entry.
	if RootFixedPlan.Count <= 0 {
		t.Fatalf("W6: the identity plan is empty")
	}
	if RootFixedPlan.Split <= 0 || RootFixedPlan.Split >= RootFixedPlan.Count {
		t.Fatalf("W6: the identity plan carries split=%d of %d, so it is not vacuous", RootFixedPlan.Split, RootFixedPlan.Count)
	}
	split := partitionSplit(t, "Root identity plan", RootFixedPlan.Entries, RootFixedPlan.Count)
	if split != RootFixedPlan.Split {
		t.Fatalf("W6: the plan carries split=%d but the first guarded entry is %d", RootFixedPlan.Split, split)
	}

	// AND A COMPILED PLAN, the half an identity plan cannot speak for: the walk
	// pushes guarded arm entries before the unguarded tail past the union, and
	// the compile partitions them.
	parsed, why := tableFixedParseLayout(RootFixedLayout)
	if why != "" {
		t.Fatalf("W6: the reader's own layout does not parse: %s", why)
	}
	cplan := make([]TableFixedEntry, 256)
	var cr TableReport
	made := tableFixedCompile(parsed, RootFixedLayout, RootFixedDst, cplan, &cr)
	if made <= 0 {
		t.Fatalf("W6: tableFixedCompile made %d (%+v)", made, cr)
	}
	csplit := partitionSplit(t, "Root compiled plan", cplan, made)
	if csplit <= 0 || csplit >= made {
		t.Fatalf("W6: the compiled plan really has guarded entries (split=%d of %d), so the case is not vacuous", csplit, made)
	}
}
`)
	})

	t.Run("go/P1", func(t *testing.T) {
		runGenerated(t, fixture, `package probe
import (
	"bytes"
	"testing"
	"unsafe"
)

// bytesOf returns a copy of the record's destination image, so the two paths
// are compared over the bytes they LAND and not over one shared slice.
func bytesOf(v *Root) []byte {
	img := tableFixedOverlay(unsafe.Pointer(v), unsafe.Sizeof(*v))
	out := make([]byte, len(img))
	copy(out, img)
	return out
}

func TestGoT17P1(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.Head = 41
	one.Tail = 99
	one.Pick.Type = PickTypeB
	copy(one.Pick.B.S[:], "seven77")
	one.Pick.B.SLength = 7
	one.Pick.B.M = 1234

	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{one}, buf); n != int64(len(buf)) {
		t.Fatalf("P1: save wrote %d, want %d", n, len(buf))
	}

	// THE IDENTITY PATH: this build's own hash selects the baked RootFixedPlan.
	id := make([]Root, 1)
	var ri TableReport
	iplan := make([]TableFixedEntry, 4096)
	if n := RootFixedLoad(id, buf, iplan, &ri); n != 1 {
		t.Fatalf("P1: the identity read returned %d (%+v)", n, ri)
	}
	if ri != (TableReport{}) {
		t.Fatalf("P1: a lawful record moved the identity counters: %+v", ri)
	}

	// THE COMPILED PATH: the SAME layout bytes, compiled by tableFixedCompile.
	parsed, why := tableFixedParseLayout(RootFixedLayout)
	if why != "" {
		t.Fatalf("P1: the layout does not parse: %s", why)
	}
	cplan := make([]TableFixedEntry, 4096)
	var rc TableReport
	made := tableFixedCompile(parsed, RootFixedLayout, RootFixedDst, cplan, &rc)
	if made <= 0 {
		t.Fatalf("P1: tableFixedCompile made %d (%+v)", made, rc)
	}
	co := Root{}
	RootReset(&co)
	rc = TableReport{}
	record := buf[len(buf)-RootFixedRecordBytes:]
	tableFixedRun(cplan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&co), unsafe.Sizeof(co)), &rc)

	if !bytes.Equal(bytesOf(&id[0]), bytesOf(&co)) {
		t.Fatalf("P1: identity and compiled landed different bytes")
	}
	if ri != rc {
		t.Fatalf("P1: identity and compiled moved different counters: identity %+v compiled %+v", ri, rc)
	}
}
`)
	})
}
