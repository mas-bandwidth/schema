package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE PLAN IS PARTITIONED UNGUARDED-FIRST, AND THE IDENTITY PATH EQUALS THE
// COMPILED ONE (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.2, §4.4; rows go/W6 and
// go/P1). One fixture — a fixed table mixing unguarded scalars with fields
// under union arms — proves both rows.
//
// go/W6: the compiled plan carries a split index, every entry before it is
// unguarded, every entry after it is guarded, and no coalesced run crosses it.
// go/P1: the identity path and the compiled plan produce identical bytes for
// one populated record. Assertions on Go plan values in-process are unit
// tests; running the generated unit shells out to the Go toolchain and is a
// functional test behind slowtest.Gate.
func TestFixedRoadmapGoT17PlanPartition(t *testing.T) {
	t.Parallel()

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

	const decls = `
type ArmA struct {
	N int32
}

type ArmB struct {
	S       [8]byte
	SLength int32
	M       int32
}

type PickType uint8

const (
	PickTypeNone  PickType = 0
	PickTypeA     PickType = 1
	PickTypeB     PickType = 2
	PickTypeCount PickType = 2
	PickTypeMax   PickType = 2
)

type Pick struct {
	Type PickType
	A    ArmA
	B    ArmB
}
`

	t.Run("go/W6", func(t *testing.T) {
		t.Parallel()
		runT17TableOnly(t, fixture, `package probe
import "testing"
`+decls+`
// partitionSplit is §4.1's partition held: split is in range, every entry
// below it carries NO guard, every entry at or above it carries one, and the
// pair at the split was NOT coalesced across it. It returns the split.
func partitionSplit(t *testing.T, who string, plan []TableFixedEntry, n int32) int32 {
	t.Helper()
	if n < 0 || int64(len(plan)) < int64(n) {
		t.Fatalf("W6: %s: split is in range (n=%d)", who, n)
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
			t.Fatalf("W6: %s: entry %d is below the split and carries a GUARD", who, i)
		}
		if i >= split && !guarded {
			t.Fatalf("W6: %s: entry %d is at or above the split and carries NO guard", who, i)
		}
	}
	if split > 0 && split < n {
		a, b := plan[split-1], plan[split]
		if a.Op == tableFixedCopy && b.Op == tableFixedCopy &&
			a.Guard == b.Guard && a.Arg == b.Arg &&
			a.Src+a.Size == b.Src && a.Dst+a.Size == b.Dst {
			t.Fatalf("W6: %s: the pair at the split was coalesced across it", who)
		}
	}
	return split
}

func TestGoT17W6(t *testing.T) {
	if RootFixedPlan.Count <= 0 {
		t.Fatalf("W6: the identity plan is empty")
	}
	// The identity plan CARRIES its split (FIXED-FORM-ALGORITHM.md §4.1).
	if RootFixedPlan.Split <= 0 || RootFixedPlan.Split >= RootFixedPlan.Count {
		t.Fatalf("W6: the identity plan carries split=%d of %d, so it is not vacuous", RootFixedPlan.Split, RootFixedPlan.Count)
	}
	split := partitionSplit(t, "Root identity plan", RootFixedPlan.Entries, RootFixedPlan.Count)
	if split != RootFixedPlan.Split {
		t.Fatalf("W6: the plan carries split=%d but the first guarded entry is %d", RootFixedPlan.Split, split)
	}
	// A compiled plan partitions the same way.
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
		t.Parallel()
		runT17TableOnly(t, fixture, `package probe
import (
	"bytes"
	"testing"
	"unsafe"
)
`+decls+`
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
	// The identity path: this build's own hash selects the baked plan.
	id := make([]Root, 1)
	var ri TableReport
	iplan := make([]TableFixedEntry, 4096)
	if n := RootFixedLoad(id, buf, iplan, &ri); n != 1 {
		t.Fatalf("P1: the identity read returned %d (%+v)", n, ri)
	}
	if ri != (TableReport{}) {
		t.Fatalf("P1: a lawful record moved the identity counters: %+v", ri)
	}
	// The compiled path over the same layout bytes.
	parsed, why := tableFixedParseLayout(RootFixedLayout)
	if why != "" {
		t.Fatalf("P1: this build's own layout does not parse: %s", why)
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
	RootFixedClamp(&co, &rc)
	if co != id[0] {
		t.Fatalf("P1: identity and compiled landed different fields:\n identity %+v\n compiled %+v", id[0], co)
	}
	if ri != rc {
		t.Fatalf("P1: identity and compiled moved different counters: identity %+v compiled %+v", ri, rc)
	}
	// The destination BYTES are identical too, not only the decoded fields.
	ib := tableFixedOverlay(unsafe.Pointer(&id[0]), unsafe.Sizeof(id[0]))
	cb := tableFixedOverlay(unsafe.Pointer(&co), unsafe.Sizeof(co))
	if !bytes.Equal(ib, cb) {
		t.Fatalf("P1: identity and compiled landed different destination bytes")
	}
}
`)
	})
}

// runT17TableOnly generates the table emitter's files alone — no packet
// emitter, no sibling serialize.go runtime — and runs the probe against them.
func runT17TableOnly(t *testing.T, schema, testSource string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}
