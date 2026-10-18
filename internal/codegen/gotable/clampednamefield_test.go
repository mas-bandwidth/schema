package gotable

import "testing"

// TestGoClampedRefusalNamesField pins that the strict read's clamped refusal
// for a SCALAR clamp names the clamped field (docs/SPEC-TABLES.md §6.5): a
// strict refusal of a wire past a scalar's bound answers TableClampedField
// with that field's path, as the other refusals name theirs.
func TestGoClampedRefusalNamesField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Root { n int32 = 1 | min = 0, max = 10 }
`, `package probe
import("errors";"testing")
func clampedN(payload []byte)[]byte{var ids TableIds;buf:=make([]byte,64);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(4);for _,b:=range payload{w.Put8(b)};w.Put8(0);w.Trailer();return buf[:w.Offset]}
func TestClampedNamesField(t *testing.T){
 wire:=clampedN([]byte{100,0,0,0})
 got,err:=RootReadStrict(wire);if got!=nil{t.Fatal("ReadStrict answered a value for a past-bound wire")};if !errors.Is(err,TableRefuseClamped){t.Fatalf("ReadStrict err %v, want the clamped refusal",err)}
 var c TableClampedField;if !errors.As(err,&c){t.Fatalf("ReadStrict err %T %v, want TableClampedField",err,err)};if c.Field!="Root.n"{t.Fatalf("clamped field %q, want Root.n",c.Field)};if got:=err.Error();got!="clamped: Root.n"{t.Fatalf("error text %q, want clamped: Root.n",got)}
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport;if RootLoadBuilder(&b,wire,&report){t.Fatal("LoadBuilder accepted a past-bound wire")};if !errors.Is(report.Err(),TableRefuseClamped){t.Fatalf("report.Err %v, want the clamped refusal",report.Err())}
}
`)
}
