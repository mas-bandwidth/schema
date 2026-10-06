package gotable

import "testing"

// TestFixedRoadmapT01Framing is the go leg's harness for the roadmap node
// `fixed-tables`, tasks of the rows file-envelope, batch-capacity and
// plan-selection (docs/FIXED-FORM-ALGORITHM.md §5.3, the read procedure).
// One subtest per task id. A task another test already holds is named by that
// test in the verdict, not repeated here: go/F4 by
// TestFixedFormLayoutMalformedTruncated, go/F7 by TestFixedFormUnder20Bytes and
// go/F8 by TestFixedFormRaggedTail. go/F12 has no subtest: this leg has no
// layout announcement for a second layout to arrive on.
func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`
	for _, c := range []struct{ id, probe string }{
		{"go/F9", f9BatchTooLarge},
	} {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, schema, c.probe)
		})
	}
}

// the shared probe prelude: one clean Point file, a poisoned destination, a
// dirty report.
const t01Prelude = `package probe
import ("encoding/binary"; "testing"; "unsafe")

var _ = binary.LittleEndian
var _ = unsafe.Sizeof(0)

const poison = 0x5A

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func poisoned(n int) ([]Point, []byte) {
	v := make([]Point, n)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), int(unsafe.Sizeof(v[0]))*n)
	for i := range b {
		b[i] = poison
	}
	return v, b
}

func untouched(b []byte) bool {
	for _, c := range b {
		if c != poison {
			return false
		}
	}
	return true
}

func dirty() TableReport {
	return TableReport{Widened: 1, Unknown: 2, KindMismatch: 3, Clamped: 4, Duplicate: 5, Malformed: true, Retained: 6, RetainLost: 7}
}
`

// go/F9, "batch_too_large". §5.3 step 6: "If `rest / record_bytes` passes the
// caller's capacity, `REFUSE batch_too_large`". A caller's capacity of exactly
// the file's records reads; one short refuses by name, with Malformed clear, no
// counter moved and not one destination byte written.
const f9BatchTooLarge = t01Prelude + `
func TestF9(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	for _, records := range []int{1, 2, 5} {
		file := cleanFile(t, records)
		exact := make([]Point, records)
		r := dirty()
		if n := PointFixedLoad(exact, file, plan, &r); n != int64(records) || r != (TableReport{}) {
			t.Fatalf("capacity == records (%d) must read: n=%d %+v", records, n, r)
		}
		for short := 0; short < records; short++ {
			dst, bytes := poisoned(short + 1)
			dst = dst[:short]
			r = dirty()
			if n := PointFixedLoad(dst, file, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "batch_too_large"}) {
				t.Fatalf("records=%d capacity=%d: n=%d %+v", records, short, n, r)
			}
			if !untouched(bytes) {
				t.Fatalf("records=%d capacity=%d: batch_too_large wrote destination bytes", records, short)
			}
		}
	}
}
`
