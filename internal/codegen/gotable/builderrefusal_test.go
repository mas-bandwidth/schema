package gotable

import "testing"

func TestBuilderRefusalThroughUnion(t *testing.T) {
	runGenerated(t, `package probe
table Child { entries map[int32]int32 }
union Force { one Child }
table Root { arm Force
 next int32 }
`, `package probe
import "testing"
func TestUnionCountRefusal(t *testing.T){
 wire:=make([]byte,256);var ids TableIds;w:=TableWriter{Buffer:wire,Ids:&ids}
 w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(15);w.Id(RootTableFields[0].VariantId(1));w.Put8(13)
 arm:=w.Offset;w.Put8(0);w.Id(ChildTableFields[0].Id);w.Put8(14);body:=w.Offset;w.Put8(0);w.Put8(13);w.PutLeb(1<<31)
 wire[body]=byte(w.Offset-body-1);w.Put8(0);wire[arm]=byte(w.Offset-arm-1)
 w.Id(RootTableFields[1].Id);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer()
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();report:=TableReport{Unknown:3}
 if RootLoadBuilder(&b,wire[:w.Offset],&report)||report.Malformed||report.Unknown!=3||report.KindMismatch!=0||b.GetRoot().Next!=0{t.Fatalf("union count refusal became damage or continued: %+v",report)}
}
`)
}
