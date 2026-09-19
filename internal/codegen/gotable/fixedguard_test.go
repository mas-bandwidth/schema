package gotable

// THE GUARD AT ITS WIDTH, THE WRITER'S BOUNDS, AND THE REFUSALS OWED
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3, §5.4, §5.9). Each test below is one
// hostile file or one forged plan and one assertion by name.

import (
	"fmt"
	"strings"
	"testing"
)

// ITEM 1. A union tag is compared WHOLE. A 256-arm union's tag is two bytes, so
// a foreign 0x0101 is the ordinal 257 and names no arm; compared at ONE byte it
// is 1 and fires arm 1.
func TestFixedGuardIsComparedAtItsFullWidth(t *testing.T) {
	var b strings.Builder
	b.WriteString("package probe\ntype Cell { n int32 }\nunion Wide\n{\n")
	for i := range 256 {
		fmt.Fprintf(&b, "    a%d Cell\n", i)
	}
	b.WriteString("}\nfixed table Host\n{\n    pick Wide\n    tail int32 = 7\n}\n")
	runGenerated(t, b.String(), `package probe
import "testing"

func TestForeignTwoByteTagFiresNoArm(t *testing.T) {
	if HostFixedBodyBytes != 10 {
		t.Fatalf("the record body moved: %d bytes, so the offsets below are stale", HostFixedBodyBytes)
	}
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{{Tail: 7}}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	body := buf[len(buf)-HostFixedRecordBytes+8:]
	// 0x0101: the ordinal 257, which is not 1 at two bytes and IS 1 at one
	body[0], body[1] = 0x01, 0x01
	body[2], body[3], body[4], body[5] = 0x39, 0x05, 0, 0 // an arm payload to land
	got := make([]Host, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	if n := HostFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("load n=%d %+v", n, r)
	}
	if got[0].Pick.Type != WideTypeNone {
		t.Fatalf("a foreign two-byte tag landed arm %d", got[0].Pick.Type)
	}
	if got[0].Pick.A0.N != 0 {
		t.Fatalf("arm 1 was filled by a tag that is not 1: N=%d", got[0].Pick.A0.N)
	}
	if r.Clamped != 1 {
		t.Fatalf("a tag past the arm count counts clamped once, got %d (%+v)", r.Clamped, r)
	}
}
`)
}

// ITEM 2. A COUNT'S BOUND IS THE WRITER'S (§5.2 EMIT kind 14). An older writer
// bounded at 4 and a reader grown to 8: a forged 7 lands 4 and COUNTS, because
// the plan holds the writer's bound and not the reader's.
func TestFixedCountBoundIsTheWriters(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host
{
    xs [..8]int32
    tail int32 = 7
}
`, `package probe
import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestForgedCountLandsTheWritersBound(t *testing.T) {
	// THE OLDER WRITER'S LAYOUT: this build's bytes with the array bounded at
	// 4 instead of 8, which is the one edit `+"`"+`array_bounded_grow`+"`"+` makes.
	older := make([]byte, len(HostFixedLayout))
	copy(older, HostFixedLayout)
	const entryAt = 4
	const entryBytes = 17
	size := func(i int) uint32 { return tableFixedGet32(older[entryAt+i*entryBytes+9:]) }
	setSize := func(i int, v uint32) { binary.LittleEndian.PutUint32(older[entryAt+i*entryBytes+9:], v) }
	if size(0) != 40 || size(1) != 36 {
		t.Fatalf("the layout moved: root %d, array %d", size(0), size(1))
	}
	setSize(1, 4+4*4) // the count and four elements
	setSize(0, 20+4)  // the array and the tail
	parsed, why := tableFixedParseLayout(older)
	if why != "" {
		t.Fatalf("the older layout does not parse: %s", why)
	}
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	made := tableFixedCompile(parsed, HostFixedLayout, HostFixedDst, plan, &r)
	if made <= 0 {
		t.Fatalf("compile made %d %+v", made, r)
	}
	// THE OLDER WRITER'S RECORD BODY: a forged count of 7 over a bound of 4.
	record := make([]byte, 24)
	binary.LittleEndian.PutUint32(record, 7)
	var got Host
	HostReset(&got)
	tableFixedRun(plan, made, record, tableFixedOverlay(unsafe.Pointer(&got), unsafe.Sizeof(got)), &r)
	if got.XsCount != 4 {
		t.Fatalf("a forged 7 over the WRITER's bound of 4 landed %d", got.XsCount)
	}
	if r.Clamped != 1 {
		t.Fatalf("the clamp counts once, got %d (%+v)", r.Clamped, r)
	}
}
`)
}

// ITEM 3. An ordinal past the writer's variant count lands None and COUNTS on
// the compiled plan exactly as it does on the identity one (§5.4).
func TestFixedForgedOrdinalCountsOnBothPlans(t *testing.T) {
	runGenerated(t, `package probe
enum Grade
{
    Gold
    Silver
}
fixed table Host
{
    g Grade
    tail int32 = 7
}
`, `package probe
import (
	"testing"
	"unsafe"
)

func TestForgedOrdinalCountsOnBothPlans(t *testing.T) {
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{{Tail: 7}}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	record := buf[len(buf)-HostFixedRecordBytes:]
	record[8] = 9 // an ordinal past the writer's two variants

	got := make([]Host, 1)
	var identity TableReport
	if n := HostFixedLoad(got, buf, make([]TableFixedEntry, 256), &identity); n != 1 {
		t.Fatalf("load n=%d %+v", n, identity)
	}
	if got[0].G != GradeNone || identity.Clamped != 1 {
		t.Fatalf("identity plan: g=%d clamped=%d", got[0].G, identity.Clamped)
	}

	parsed, why := tableFixedParseLayout(HostFixedLayout)
	if why != "" {
		t.Fatalf("my own layout does not parse: %s", why)
	}
	plan := make([]TableFixedEntry, 256)
	var compiled TableReport
	made := tableFixedCompile(parsed, HostFixedLayout, HostFixedDst, plan, &compiled)
	if made <= 0 {
		t.Fatalf("compile made %d", made)
	}
	var v Host
	HostReset(&v)
	tableFixedRun(plan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v)), &compiled)
	if v.G != GradeNone {
		t.Fatalf("compiled plan: a forged ordinal landed %d", v.G)
	}
	if compiled.Clamped != 1 {
		t.Fatalf("compiled plan: a forged ordinal counts clamped once, got %d", compiled.Clamped)
	}
}
`)
}

// ITEMS 7 and 8. The selected plan's entry count is checked on EITHER lane
// (§5.9 #5, #45), and A REFUSAL BY NAME HAS COUNTERS ALL ZERO (§5.3's joint
// table) — a report a caller reuses carries neither the counters nor the name.
func TestFixedRefusalOwesZeroCountersAndTheIdentityLaneOwesPlanTooLarge(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host
{
    on bool
    tail int32 = 7
}
`, `package probe
import "testing"

func TestPlanTooLargeOnTheIdentityLane(t *testing.T) {
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{{Tail: 7}}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	var r TableReport
	if n := HostFixedLoad(make([]Host, 1), buf, make([]TableFixedEntry, 1), &r); n != -1 {
		t.Fatalf("a one-entry plan slice read n=%d", n)
	}
	if r.Reason != "plan_too_large" {
		t.Fatalf("the identity lane owes plan_too_large, got %q", r.Reason)
	}
}

func TestARefusalHasCountersAllZeroAndAReportIsReusable(t *testing.T) {
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{{Tail: 7}}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	// A report carrying a previous read's counters, then a refusal.
	r := TableReport{Clamped: 3, Unknown: 2, Widened: 1}
	form1 := make([]byte, len(buf))
	copy(form1, buf)
	form1[0] = 1
	if n := HostFixedLoad(make([]Host, 1), form1, make([]TableFixedEntry, 256), &r); n != -1 {
		t.Fatalf("a form-1 file read n=%d", n)
	}
	if r.Reason != "previous_form" {
		t.Fatalf("reason %q", r.Reason)
	}
	if r.Clamped != 0 || r.Unknown != 0 || r.Widened != 0 {
		t.Fatalf("a refusal by name has counters all zero: %+v", r)
	}
	// THE SAME REPORT, REUSED: a clean read reports clean.
	if n := HostFixedLoad(make([]Host, 1), buf, make([]TableFixedEntry, 256), &r); n != 1 {
		t.Fatalf("the clean read n=%d %+v", n, r)
	}
	if r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("a reused report kept a stale refusal: %+v", r)
	}
}
`)
}
