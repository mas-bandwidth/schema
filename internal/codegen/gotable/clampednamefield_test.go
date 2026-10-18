package gotable

import "testing"

// TestGoClampedRefusalNamesField pins docs/SPEC-TABLES.md §6.5's rule that the
// strict read's clamped refusal names the field it clamped: a scalar clamp in a
// nested table reports `clamped: <Table>.<field>`, so the error can say which
// field was clamped.
func TestGoClampedRefusalNamesField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Inner { n int32 | min = 0, max = 10 }
table Root { sub Inner }
`, `package probe
import("errors";"testing")
func putPastBound() []byte {
	var ids TableIds
	ids.Ref(RootTableFields[0].Id)
	ids.Ref(InnerTableFields[0].Id)
	body:=make([]byte,64)
	bw:=TableWriter{Buffer:body,Ids:&ids}
	bw.Id(InnerTableFields[0].Id)
	bw.Put8(4)
	bw.Put32(100)
	bw.Put8(0)
	nb:=bw.Offset
	buf:=make([]byte,128)
	w:=TableWriter{Buffer:buf,Ids:&ids}
	w.Put8(1)
	w.Id(RootTableFields[0].Id)
	w.Put8(13)
	w.PutLeb(uint64(nb))
	w.Raw(body[:nb])
	w.Put8(0)
	w.Trailer()
	return buf[:w.Offset]
}
func TestNamesField(t *testing.T) {
	wire:=putPastBound()
	got,err:=RootReadStrict(wire)
	if err==nil||got!=nil{t.Fatalf("strict read accepted a past-bound wire: %v %+v",err,got)}
	var ce TableClampedField
	if !errors.As(err,&ce){t.Fatalf("error %v is not a TableClampedField",err)}
	if ce.Field!="Inner.n"{t.Fatalf("clamped refusal names %q, want %q",ce.Field,"Inner.n")}
	if got:=err.Error();got!="clamped: Inner.n"{t.Fatalf("error text %q, want clamped: Inner.n",got)}
}
`)
}
