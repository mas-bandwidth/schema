package gotable

import "testing"

func TestRegionLists(t *testing.T) {
	runGenerated(t, `package probe
fixed table Child { n int32 = 7 }
table Inner { values []int16 }
table Root { numbers []int32
 children []Child
 nested []Inner
 pointers []*Child
 head *Child
}
`, `package probe
import("testing";"bytes";"unsafe")
func TestList(t *testing.T){var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot();first:=RootNumbersAdd(&b.Main,&v.Numbers);*first=7;for i:=0;i<500;i++ {*RootNumbersAdd(&b.Main,&v.Numbers)=int32(i)};if *first!=7||*v.Numbers.At(400,&b.Arena)!=399{t.Fatal("stable handles")};if c:=RootChildrenAdd(&b.Main,&v.Children);c.N!=7{t.Fatal("defaults")};inner:=RootNestedAdd(&b.Main,&v.Nested);*InnerValuesAdd(&b.Main,&inner.Values)=23;c:=ChildEmplace(&b.Main,&v.Head);c.N=9;*RootPointersAdd(&b.Main,&v.Pointers)=v.Head
 dead:=v.Numbers.At(10,&b.Arena);if !RootNumbersErase(&b.Arena,&v.Numbers,dead)||RootNumbersErase(&b.Arena,&v.Numbers,dead)||*dead!=9{t.Fatal("erase")};*RootNumbersAdd(&b.Main,&v.Numbers)=500
 n:=RootMeasure(v,&b.Arena);if n<0{t.Fatal("measure")};wire:=make([]byte,n);if RootSave(v,wire,&b.Arena)!=n{t.Fatal("save")};size:=RootLoadMeasure(wire);if size<0{t.Fatal("load measure")};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[off:off+uintptr(size)];var report TableReport;r:=RootLoad(region,wire,&report);if r==nil||report!=(TableReport{}){t.Fatalf("load %+v",report)};if r.Numbers.Count!=501||*r.Numbers.At(400)!=400||*r.Nested.At(0).Values.At(0)!=23||ChildAt(r.Pointers.At(0))!=ChildAt(&r.Head){t.Fatal("values")};if !b.Lock(){t.Fatal("lock")};if !bytes.Equal(b.Region(),region[:size-32]){t.Fatal("pack differs")};out:=make([]byte,n);if RootSave(b.AsConst(),out)!=n||!bytes.Equal(out,wire){t.Fatal("round trip")}
 var edit RootBuilder;edit.Init();defer edit.Shutdown();report=TableReport{};if !RootLoadBuilder(&edit,wire,&report)||report!=(TableReport{}){t.Fatalf("builder load %+v",report)};loaded:=edit.GetRoot();if loaded.Numbers.Count!=501||ChildAt(loaded.Pointers.At(0,&edit.Arena),&edit.Arena)!=ChildAt(&loaded.Head,&edit.Arena){t.Fatal("builder values")};if !RootNumbersErase(&edit.Arena,&loaded.Numbers,loaded.Numbers.At(0,&edit.Arena)){t.Fatal("loaded erase")};*RootNumbersAdd(&edit.Main,&loaded.Numbers)=1234;if !edit.Lock()||*edit.AsConst().Numbers.At(500)!=1234 {t.Fatal("loaded edit and lock")}
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
 var invalid RootBuilder;invalid.Init();defer invalid.Shutdown();*HolderValuesAdd(&invalid.Main,&invalid.GetRoot().Tail[1].Values)=9;if invalid.Lock(){t.Fatal("unreached list packed")}
}
`)
}

func TestBuilderCountRecovery(t *testing.T) {
	runGenerated(t, `package probe
table Child { n int32 = 7
 values []int32 }
table Root { values []int32
 next int32
 children []Child
 child *Child }
`, `package probe
import("testing";"math")
func file(count uint64) []byte {var ids TableIds;b:=make([]byte,256);w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(14);w.PutLeb(uint64(1+tableLebBytes(count)+4));w.Put8(4);w.PutLeb(count);w.Put32(19);w.Id(RootTableFields[1].Id);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();return b[:w.Offset]}
func TestCount(t *testing.T){var b RootBuilder;b.Init();defer b.Shutdown();var report TableReport;wire:=file(1<<30);if RootLoadMeasure(wire)!=-1 {t.Fatal("region framing cap")};if !RootLoadBuilder(&b,wire,&report)||!report.Malformed||b.GetRoot().Values.Count!=1||*b.GetRoot().Values.At(0,&b.Arena)!=19||b.GetRoot().Next!=42 {t.Fatalf("prefix %+v, %+v",report,b.GetRoot())};report=TableReport{Unknown:3};if RootLoadBuilder(&b,file(uint64(math.MaxInt32)+1),&report)||report.Malformed||report.Unknown!=3||report.KindMismatch!=0 {t.Fatalf("count refusal %+v",report)} }
`)
}
func TestNestedTerminationAndStringDefault(t *testing.T) {
	runGenerated(t, `package probe
enum Key { first }
fixed table Child { n int32 = 7 }
table Root { name string(16) = "default"
 values [..2]Child
 slots [Key]Child
 list []Child
 next int32 }
`, `package probe
import("testing")
func TestRecovery(t *testing.T){var ids TableIds;wire:=make([]byte,512);w:=TableWriter{Buffer:wire,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(12);w.PutLeb(1);w.Put8(255)
 for _,field:=range []int{1,2,3}{w.Id(RootTableFields[field].Id);if field==2 {w.Put8(16);w.PutLeb(12)}else{w.Put8(14);w.PutLeb(11)};w.Put8(13);w.PutLeb(1);if field==2{id,_:=Key(1).TableEnumId();w.Id(id)};w.PutLeb(8);w.Id(ChildTableFields[0].Id);w.Put8(4);w.Put32(99);w.Put8(0);w.Put8(0)}
 w.Id(RootTableFields[4].Id);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();var b RootBuilder;b.Init();defer b.Shutdown();var report TableReport;if !RootLoadBuilder(&b,wire[:w.Offset],&report)||!report.Malformed {t.Fatalf("load %+v",report)};r:=b.GetRoot();if string(r.Name[:r.NameLength])!="default"||r.Values[0].N!=7||r.Slots[0].N!=7||r.List.At(0,&b.Arena).N!=7||r.Next!=42 {t.Fatalf("recovery %+v",r)} }
`)
}

func TestRegionCountCrossesLength(t *testing.T) {
	runGenerated(t, `package listdemo
 fixed table Photo { width uint32
 height uint32 }
 table Album { photos []*Photo
 cover *Photo }
 `, `package listdemo
 import("testing";"encoding/hex")
 func TestCountCrossesLength(t *testing.T) {
 wire,_:=hex.DecodeString("01010e0511b1968cb6ab8703020200470c1001030d0408800200000000000030b13aff4ad9b140ffffffffffffffffc3648950d28da7f1bfe9d12f93cddadb2272347df60b72170500000000000000")
 size:=AlbumLoadMeasure(wire);if size!=40 {t.Fatalf("measure %d",size)}
 var report TableReport
 root:=AlbumLoad(make([]byte,size),wire,&report)
 if root==nil||root.Photos.Count!=0||report!=(TableReport{Malformed:true}) {t.Fatalf("region %v %+v",root,report)}
 var builder AlbumBuilder;builder.Init();defer builder.Shutdown();report=TableReport{Unknown:3}
 if AlbumLoadBuilder(&builder,wire,&report)||report.Malformed||report.Unknown!=3 {t.Fatalf("builder %+v",report)}
 }
 `)
}
