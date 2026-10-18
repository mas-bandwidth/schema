package gotable

import "testing"

// TestGoClampedRefusalNamesField pins docs/SPEC-TABLES.md §6.5: the strict
// read's clamped refusal names the clamped field as <Table>.<field>, not just
// by subtest. The wire lands a non-finite float before forging Inner.n past its declared
// range. The clamp must keep its field name despite the earlier report reason.
func TestGoClampedRefusalNamesField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Inner { n int32 = 1 | min = 0, max = 10 }
table Root {
noise float64
inner Inner
}
`, `package probe
import("errors";"math";"strings";"testing")

func TestClampedName(t *testing.T) {
	t.Parallel()
	var ids TableIds
	var innerBuffer [64]byte
	inner := TableWriter{Buffer: innerBuffer[:], Ids: &ids}
	inner.Id(InnerTableFields[0].Id)
	inner.Put8(InnerTableFields[0].Kind)
	inner.Put32(100)
	inner.Put8(0)
	if inner.Overflow { t.Fatal("inner body overflow") }
	body := innerBuffer[:inner.Offset]

	var buffer [128]byte
	w := TableWriter{Buffer: buffer[:], Ids: &ids}
	w.Put8(1)
	w.Id(RootTableFields[0].Id)
	w.Put8(RootTableFields[0].Kind)
	w.Put64(math.Float64bits(math.NaN()))
	w.Id(RootTableFields[1].Id)
	w.Put8(RootTableFields[1].Kind)
	w.PutLeb(uint64(len(body)))
	w.Raw(body)
	w.Put8(0)
	w.Trailer()
	if w.Overflow { t.Fatal("wire overflow") }

	got, err := RootReadStrict(buffer[:w.Offset])
	if got != nil || !errors.Is(err, TableRefuseClamped) {
		t.Fatalf("ReadStrict: got %+v err %v, want nil and clamped", got, err)
	}
	if !strings.Contains(err.Error(), "Inner.n") {
		t.Fatalf("clamped refusal does not name Inner.n after non-finite: %q", err.Error())
	}
}
`)
}
