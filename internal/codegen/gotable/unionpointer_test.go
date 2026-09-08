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
