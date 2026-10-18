package gotable

import "testing"

// TestGoClampedRefusalNamesField pins that the strict read's clamped refusal
// for a SCALAR clamp names the clamped field (docs/SPEC-TABLES.md §6.5): a
// strict refusal of a wire past a scalar's bound answers TableClampedField
// with that field's name, as the other refusals name theirs. The clamped
// scalar lives in a NESTED table, so the name is the nested field's own.
func TestGoClampedRefusalNamesField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Inner { n int32 = 1 | min = 0, max = 10 }
table Root { inside Inner }
`, `package probe
import("errors";"testing")
func clampedWire() []byte{var ids TableIds;buf:=make([]byte,128);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(13);s:=TableWriter{Buffer:make([]byte,64),Ids:&ids};s.Id(InnerTableFields[0].Id);s.Put8(4);s.Put32(100);s.Put8(0);w.PutLeb(uint64(s.Offset));w.Raw(s.Buffer[:s.Offset]);w.Put8(0);w.Trailer();return buf[:w.Offset]}
func TestClampedNamesField(t *testing.T){
 wire:=clampedWire()
 got,err:=RootReadStrict(wire);if got!=nil{t.Fatal("ReadStrict answered a value for a past-bound wire")};if !errors.Is(err,TableRefuseClamped){t.Fatalf("ReadStrict err %v, want the clamped refusal",err)}
 var c TableClampedField;if !errors.As(err,&c){t.Fatalf("ReadStrict err %T %v, want TableClampedField",err,err)};if c.Field!="Inner.n"{t.Fatalf("clamped field %q, want Inner.n",c.Field)};if got:=err.Error();got!="clamped: Inner.n"{t.Fatalf("error text %q, want clamped: Inner.n",got)}
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport;if RootLoadBuilder(&b,wire,&report){t.Fatal("LoadBuilder accepted a past-bound wire")};if !errors.Is(report.Err(),TableRefuseClamped){t.Fatalf("report.Err %v, want the clamped refusal",report.Err())}
}
`)
}
