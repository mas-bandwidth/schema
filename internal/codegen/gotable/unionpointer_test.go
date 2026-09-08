package gotable

import "testing"

func TestUnionPointerArrayReaders(t *testing.T) {
	runGenerated(t, `package probe
 table Node { value int32 }
 union Choice { many [..2]*Node
 plain int32 }
 table Root { choices [..2]Choice }
 `, `package probe
 import("bytes";"testing";"unsafe")
 func TestPointers(t *testing.T){var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot();v.ChoicesCount=2
 for i:=0;i<2;i++{v.Choices[i].Type=ChoiceTypeMany;arm:=v.Choices[i].Many();arm.ValueCount=2;for j:=0;j<2;j++{node:=NodeEmplace(&b.Main,&arm.Value[j]);node.Value=int32(10*i+j+1)}}
 wire:=make([]byte,RootMeasure(v,&b.Arena));if RootSave(v,wire,&b.Arena)!=int64(len(wire)){t.Fatal("save")};size:=RootLoadMeasure(wire);raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[off:off+uintptr(size)];var report TableReport
 check:=func(v *Root){if v==nil||report.Malformed||v.ChoicesCount!=2{t.Fatalf("pointer arm load: %+v",report)};for i:=0;i<2;i++{a:=v.Choices[i].Many();if a.ValueCount!=2{t.Fatal("count")};for j:=0;j<2;j++{n:=NodeAt(&a.Value[j]);if n==nil||n.Value!=int32(10*i+j+1){t.Fatal("pointer array arm value")}}}}
 check(RootLoad(region,wire,&report));store:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainId,32)};report=TableReport{};check(RootLoadRetain(region,wire,&store,&report));out:=make([]byte,len(wire));if RootSaveRetain((*Root)(unsafe.Pointer(&region[0])),nil,out,&report)!=int64(len(out))||!bytes.Equal(out,wire){t.Fatal("retaining pointer array save")}
 }
 `)
}

func TestNestedUnionArmReaders(t *testing.T) {
	runGenerated(t, `package probe
 union Inner { number int32 }
 union Outer { nested [..2]Inner }
 table Root { items [..2]Outer
 anchor Inner
 next *Root }
 `, `package probe
 import("testing";"unsafe")
 func TestNestedUnion(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();root:=b.GetRoot();root.ItemsCount=1;root.Items[0].Type=OuterTypeNested;arm:=root.Items[0].Nested();arm.ValueCount=2
 for i:=range 2{inner:=&arm.Value[i];inner.Type=InnerTypeNumber;inner.Number().Value=int32(19+i)}
 wire:=make([]byte,RootMeasure(root));if RootSave(root,wire)!=int64(len(wire)){t.Fatal("save")}
 size:=RootLoadMeasure(wire);if size<=0{t.Fatal("measure")};raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)]
 var report TableReport
 check:=func(value *Root){if value==nil||report.Malformed||value.ItemsCount!=1{t.Fatalf("nested union load: %+v",report)};outer:=&value.Items[0];if outer.Type!=OuterTypeNested||outer.Nested().ValueCount!=2{t.Fatal("outer tag or count")};for i:=range 2{inner:=&outer.Nested().Value[i];if inner.Type!=InnerTypeNumber||inner.Number().Value!=int32(19+i){t.Fatal("nested union arm value")}}}
 check(RootLoad(region,wire,&report));report=TableReport{};store:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainId,32)};check(RootLoadRetain(region,wire,&store,&report))
 var loaded RootBuilder;if !loaded.Init(){t.Fatal("load builder init")};defer loaded.Shutdown();report=TableReport{};if !RootLoadBuilder(&loaded,wire,&report){t.Fatal("builder load")};check(loaded.GetRoot())
 }
 `)
}
