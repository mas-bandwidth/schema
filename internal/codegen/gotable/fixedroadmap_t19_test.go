package gotable

import (
	"strings"
	"testing"
)

const payloadFreeSchema = `package probe

union Pick
{
    alpha int32
    quiet
    signal
}

fixed table Root
{
    lead  int32
    pick  Pick
    trail int32
}
`

// TestFixedGoT19PayloadFreeLines is the unit tier: generate in memory and
// assert only the emitted text. Cites docs/SPEC-TABLES.md §2.6 and
// docs/FIXED-FORM-ALGORITHM.md §3.1.
func TestFixedGoT19PayloadFreeLines(t *testing.T) {
	t.Parallel()
	files := generate(t, payloadFreeSchema)
	src := ""
	for n, b := range files {
		if strings.HasSuffix(n, "Table.go") {
			src = string(b)
			break
		}
	}
	if src == "" {
		t.Fatal("no Table.go emitted")
	}
	if !strings.Contains(src, "func RootFixedSave(") {
		t.Fatal("RootFixedSave not emitted")
	}
	if !strings.Contains(src, "func RootFixedLoad(") {
		t.Fatal("RootFixedLoad not emitted")
	}
	// The tag for the union is always written.
	wb := funcSource(src, "func RootFixedWriteBody(")
	if wb == "" {
		t.Fatal("RootFixedWriteBody not found")
	}
	if !strings.Contains(wb, ".Pick.Type") || !strings.Contains(wb, "tableFixedPut8") {
		t.Fatal("Pick tag is not written in write body")
	}
	// No case emitted for payload-free arms, and no store under them.
	// Since if v.F == nil continue before emitting case, there should be
	// no "case PickTypeQuiet" or "case PickTypeSignal" in the write switch.
	if strings.Contains(wb, "case PickTypeQuiet:") || strings.Contains(wb, "case PickTypeSignal:") {
		t.Fatal("payload-free arm has a case in write switch")
	}
	// We expect only the alpha case.
	if strings.Count(wb, "case PickTypeAlpha:") != 1 {
		t.Fatal("expected exactly one arm case in write")
	}
	// For the bite demonstration: a planted store under a payload-free case
	// would be caught by this (see below runs).
}

// TestFixedRoadmapGoT19PayloadFreeArms is the functional tier through t10Run
// (tableOnly). Cites docs/SPEC-TABLES.md §2.6 and docs/FIXED-FORM-ALGORITHM.md
// §3.1 and §4.6.
func TestFixedRoadmapGoT19PayloadFreeArms(t *testing.T) {
	t.Parallel()

	t.Run("payload-free-arms/go/valid-data", func(t *testing.T) {
		t.Parallel()
		t10Run(t, payloadFreeSchema, t19Prelude+`
func TestPayloadFreeArms(t *testing.T) {
	cases := []struct {
		lead  int32
		tag   PickType
		alpha int32
		trail int32
	}{
		{-2147483648, PickTypeAlpha, -2147483648, 2147483647},
		{1, PickTypeQuiet, 0, -1},
		{-1, PickTypeSignal, 0, 1},
	}
	for _, tc := range cases {
		var v Root
		RootReset(&v)
		v.Lead = tc.lead
		v.Pick.Type = tc.tag
		v.Pick.Alpha = tc.alpha
		v.Trail = tc.trail
		got := roundtrip(t, v)
		if got.Lead != tc.lead || got.Pick.Type != tc.tag || got.Trail != tc.trail {
			t.Fatalf("%v: roundtrip lead/trail/tag: got %+v want lead=%d tag=%d trail=%d", tc.tag, got, tc.lead, tc.tag, tc.trail)
		}
		if tc.tag == PickTypeAlpha && got.Pick.Alpha != tc.alpha {
			t.Fatalf("alpha not roundtripped: got %d", got.Pick.Alpha)
		}
	}
}

func TestPayloadFreeArmsZeroStorage(t *testing.T) {
	for _, tag := range []PickType{PickTypeQuiet, PickTypeSignal} {
		var v Root
		RootReset(&v)
		v.Lead = 0x12345678
		v.Pick.Type = tag
		v.Trail = 0x07654321
		buf := make([]byte, RootFixedMeasure(1))
		if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
			t.Fatalf("save %d", n)
		}
		recBytes := int(RootFixedRecordBytes)
		recStart := len(buf) - recBytes
		// record: [recStart:recStart+8] hash, [+8]lead4, tag@+12, arm@+13:4, trail@+17
		arm := buf[recStart+13 : recStart+17]
		for i, b := range arm {
			if b != 0 {
				t.Fatalf("arm bytes under payload-free tag %d not zero at %d: got 0x%02x (buf around: % x)", tag, i, b, buf[recStart+8:recStart+21])
			}
		}
		back := make([]Root, 1)
		plan := make([]TableFixedEntry, 256)
		var r TableReport
		if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
			t.Fatalf("load for zero: n=%d r=%+v", n, r)
		}
		if back[0].Pick.Type != tag || back[0].Lead != 0x12345678 || back[0].Trail != 0x07654321 {
			t.Fatalf("round for free: %+v", back[0])
		}
	}
}
`, true)
	})

	t.Run("tag-past-the-arm-set", func(t *testing.T) {
		t.Parallel()
		t10Run(t, payloadFreeSchema, t19Prelude+`
func TestTagPastArmSet(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Lead = 7
	v.Pick.Type = PickTypeNone // lawful: the Go writer refuses a tag past the set (§3.1)
	v.Trail = 9
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	// forge the tag byte (record data after its hash: lead at +8, tag at +12)
	recStart := len(buf) - int(RootFixedRecordBytes)
	buf[recStart+12] = 99
	back := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	n := RootFixedLoad(back, buf, plan, &r)
	if n != 1 || back[0].Pick.Type != PickTypeNone || r.Clamped != 1 {
		t.Fatalf("past tag: n=%d type=%v clamped=%d r=%+v", n, back[0].Pick.Type, r.Clamped, r)
	}
}
`, true)
	})
}

// t19Prelude based on t11/t10.
const t19Prelude = `package probe

import "testing"

func roundtrip(t *testing.T, v Root) Root {
	t.Helper()
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save returned %d, want %d", n, len(buf))
	}
	back := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load returned %d, report %+v", n, r)
	}
	return back[0]
}
`
