package gotable

import "testing"

func TestRegionLists(t *testing.T) {
	runGenerated(t, `package probe
table Child { n int32 = 7 }
table Inner { values []int16 }
table Root { numbers []int32
 children []Child
 nested []Inner
 pointers []*Child
 head *Child
}
`, `package probe
import("testing";"bytes";"unsafe")
func TestList(t *testing.T){var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot();first:=RootNumbersAdd(&b.Main,v);*first=7;for i:=0;i<500;i++ {*RootNumbersAdd(&b.Main,v)=int32(i)};if *first!=7||*v.Numbers.At(400,&b.Arena)!=399{t.Fatal("stable handles")};if c:=RootChildrenAdd(&b.Main,v);c.N!=7{t.Fatal("defaults")};inner:=RootNestedAdd(&b.Main,v);*InnerValuesAdd(&b.Main,inner)=23;c:=ChildEmplace(&b.Main,&v.Head);c.N=9;*RootPointersAdd(&b.Main,v)=v.Head
 dead:=v.Numbers.At(10,&b.Arena);if !RootNumbersErase(&b.Arena,v,dead)||RootNumbersErase(&b.Arena,v,dead)||*dead!=9{t.Fatal("erase")};*RootNumbersAdd(&b.Main,v)=500
 n:=RootMeasure(v,&b.Arena);if n<0{t.Fatal("measure")};wire:=make([]byte,n);if RootSave(v,wire,&b.Arena)!=n{t.Fatal("save")};size:=RootLoadMeasure(wire);if size<0{t.Fatal("load measure")};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[off:off+uintptr(size)];var report TableReport;r:=RootLoad(region,wire,&report);if r==nil||report!=(TableReport{}){t.Fatalf("load %+v",report)};if r.Numbers.Count!=501||*r.Numbers.At(400)!=400||*r.Nested.At(0).Values.At(0)!=23||ChildAt(r.Pointers.At(0))!=ChildAt(&r.Head){t.Fatal("values")};if !b.Lock(){t.Fatal("lock")};if !bytes.Equal(b.Region(),region[:size-32]){t.Fatal("pack differs")};out:=make([]byte,n);if RootSave(b.AsConst(),out)!=n||!bytes.Equal(out,wire){t.Fatal("round trip")}
 text:=make([]byte,RootToJsonMeasure(r));if RootToJson(r,text)!=int64(len(text)){t.Fatal("text write")};var b2 RootBuilder;b2.Init();defer b2.Shutdown();report=TableReport{};if !RootFromJson(&b2,text,&report)||report!=(TableReport{})||!b2.Lock(){t.Fatalf("text read %+v",report)};if RootSave(b2.AsConst(),out)!=n||!bytes.Equal(out,wire){t.Fatal("text round trip")}
 var ids TableIds;small:=make([]byte,128);w:=TableWriter{Buffer:small,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(14);w.PutLeb(5);w.Put8(2);w.PutLeb(3);w.Put8(128);w.Put8(0);w.Put8(127);w.Put8(0);w.Trailer();small=small[:w.Offset];needed:=RootLoadMeasure(small);if needed<0{t.Fatal("widen measure")};report=TableReport{};wide:=RootLoad(region,small,&report);if wide==nil||report.Widened!=1||report.Malformed||wide.Numbers.Count!=3||*wide.Numbers.At(0)!=-128||*wide.Numbers.At(2)!=127{t.Fatalf("widen list %+v",report)}
 if allocs:=testing.AllocsPerRun(10,func(){report=TableReport{};RootLoadMeasure(wire);RootLoad(region,wire,&report)});allocs!=0{t.Fatalf("load allocations %v",allocs)}
}
`)
}

func TestListsThroughUnionArrays(t *testing.T) {
	runGenerated(t, `package probe
table Holder { values []int32 }
table Holders { items [..2]Holder }
union Choice {
 one Holder
 many Holders
 ping
}
table Root {
 choices []Choice
 value Choice
 tail [..2]Holder
}
`, `package probe
import("testing";"unsafe";"bytes")
func TestNested(t *testing.T){var b RootBuilder;b.Init();defer b.Shutdown();text:=[]byte("{\"choices\":[{\"many\":{\"items\":[{\"values\":[1,2]},{\"values\":[3]}]}}],\"value\":{\"one\":{\"values\":[4]}},\"tail\":[{\"values\":[5]}]}");var report TableReport;if !RootFromJson(&b,text,&report)||report!=(TableReport{}){t.Fatalf("json %+v",report)};wire:=make([]byte,RootMeasure(b.GetRoot(),&b.Arena));if RootSave(b.GetRoot(),wire,&b.Arena)!=int64(len(wire)){t.Fatal("save")};size:=RootLoadMeasure(wire);if !b.Lock(){t.Fatal("early lock")};if size!=int64(len(b.Region()))+16{t.Fatalf("measure %d vs pack %d",size,len(b.Region())+16)};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[off:off+uintptr(size)];report=TableReport{};got:=RootLoad(region,wire,&report);if got==nil||report!=(TableReport{}){t.Fatalf("load %+v",report)};out:=make([]byte,len(wire));if RootSave(got,out)!=int64(len(out))||!bytes.Equal(out,wire){t.Fatal("round trip")};if !b.Lock()||!bytes.Equal(b.Region(),region[:size-16]){t.Fatal("pack extent")}
 var invalid RootBuilder;invalid.Init();defer invalid.Shutdown();*HolderValuesAdd(&invalid.Main,&invalid.GetRoot().Tail[1])=9;if invalid.Lock(){t.Fatal("unreached list packed")}
}
`)
}
