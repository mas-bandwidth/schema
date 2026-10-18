package gotable

import "testing"

// TestGoClampedRefusalNamesField pins docs/SPEC-TABLES.md §6.5: the strict
// read's clamped refusal names the clamped field as <Table>.<field>, not just
// by subtest. The wire is forged past Inner.n's declared range at its own
// nesting level, and the refusal from the root must carry that name.
func TestGoClampedRefusalNamesField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Inner { n int32 = 1 | min = 0, max = 10 }
table Root { inner Inner }
`, `package probe
import("errors";"strings";"testing")

func TestClampedName(t *testing.T) {
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
		t.Fatalf("clamped refusal does not name Inner.n: %q", err.Error())
	}
}
`)
}
