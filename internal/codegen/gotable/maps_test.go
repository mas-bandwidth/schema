package gotable

import "testing"

func TestRegionMaps(t *testing.T) {
	runGenerated(t, `package probe
table Child { n int32 = 7 }
table Root {
 names map[string(16)]Child
 numbers map[int64]uint64
 nested map[uint32]map[uint8]Child
 lists map[string(8)][]int16
 pointers map[uint64]*Child
}
`, `package probe
import("testing";"bytes";"unsafe";"math")
func TestMap(t *testing.T){var b RootBuilder;b.Init();defer b.Shutdown();r:=b.GetRoot();first:=RootNamesInsert(&b.Main,&r.Names,"z");if first.N!=7{t.Fatal("default")};first.N=5;RootNamesInsert(&b.Main,&r.Names,"a").N=2;if RootNamesFind(&b.Arena,&r.Names,"z")!=first{t.Fatal("stable find")};if RootNamesInsert(&b.Main,&r.Names,"z")!=first||first.N!=7||r.Names.Count!=2{t.Fatal("replace")};if RootNamesInsert(&b.Main,&r.Names,"a too long key here")!=nil{t.Fatal("bound")};*RootNumbersInsert(&b.Main,&r.Numbers,math.MinInt64)=math.MaxUint64;*RootNumbersInsert(&b.Main,&r.Numbers,math.MaxInt64)=1
 nested:=RootNestedInsert(&b.Main,&r.Nested,9);RootNestedEntryValueInsert(&b.Main,nested,3).N=8
 list:=RootListsInsert(&b.Main,&r.Lists,"key");*RootListsEntryValueAdd(&b.Main,list)=12
 slot:=RootPointersInsert(&b.Main,&r.Pointers,5);ChildEmplace(&b.Main,slot).N=11;*RootPointersInsert(&b.Main,&r.Pointers,2)=*slot
 wire:=make([]byte,RootMeasure(r,&b.Arena));if RootSave(r,wire,&b.Arena)!=int64(len(wire)){t.Fatal("save")};size:=RootLoadMeasure(wire);if size<0{t.Fatal("measure")};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;region:=raw[off:off+uintptr(size)];var report TableReport;got:=RootLoad(region,wire,&report);if got==nil||report!=(TableReport{}){t.Fatalf("load %+v",report)};if string(got.Names.At(0).Key[:got.Names.At(0).KeyLength])!="a"||*RootNumbersFind(nil,&got.Numbers,math.MinInt64)!=math.MaxUint64{t.Fatal("order")};if !b.Lock()||!bytes.Equal(b.Region(),region[:len(b.Region())]){t.Fatal("pack identity")};out:=make([]byte,len(wire));if RootSave(got,out)!=int64(len(out))||!bytes.Equal(out,wire){t.Fatal("wire identity")}
 memory:=make([]byte,RootNamesIndexMeasure(&got.Names));idx:=RootNamesIndex(&got.Names,memory);if RootNamesIndexFind(idx,&got.Names,"z")!=RootNamesFind(nil,&got.Names,"z")||RootNamesIndexFind(idx,&got.Names,"missing")!=nil {t.Fatal("index")};if a:=testing.AllocsPerRun(5,func(){report=TableReport{};RootLoadMeasure(wire);RootLoad(region,wire,&report);RootNamesIndex(&got.Names,memory)});a!=0{t.Fatalf("read allocations %v",a)}
 text:=make([]byte,RootToJsonMeasure(got));if RootToJson(got,text)!=int64(len(text)){t.Fatal("json write")};var edit RootBuilder;edit.Init();defer edit.Shutdown();report=TableReport{};if !RootFromJson(&edit,text,&report)||report!=(TableReport{})||!edit.Lock(){t.Fatalf("json read %+v %s",report,text)};if RootSave(edit.AsConst(),out)!=int64(len(out))||!bytes.Equal(out,wire){t.Fatal("json identity")}
 var loaded RootBuilder;loaded.Init();defer loaded.Shutdown();report=TableReport{};if !RootLoadBuilder(&loaded,wire,&report)||report!=(TableReport{}) {t.Fatalf("builder read %+v",report)};dead:=RootNamesFind(&loaded.Arena,&loaded.GetRoot().Names,"z");if !RootNamesErase(&loaded.Arena,&loaded.GetRoot().Names,"z")||RootNamesErase(&loaded.Arena,&loaded.GetRoot().Names,"z"){t.Fatal("erase")};if RootNamesInsert(&loaded.Main,&loaded.GetRoot().Names,"z")==dead{t.Fatal("dead storage reused")};if !loaded.Lock(){t.Fatal("edited lock")}
}
`)
}

func TestMapReadReports(t *testing.T) {
	runGenerated(t, `package probe
table Root { entries map[int32]int32
 after int32 }
`, `package probe
import("testing";"unsafe")
func makeWire(keys []byte,kinds []byte)[]byte {var ids TableIds;b:=make([]byte,512);w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);w.Id(RootTableFields[0].Id);w.Put8(14);w.PutLeb(uint64(2+len(keys)*8));w.Put8(13);w.PutLeb(uint64(len(keys)));for i,k:=range keys {w.PutLeb(7);w.Id(RootEntriesEntryTableFields[0].Id);w.Put8(kinds[i]);w.Put8(k);w.Id(RootEntriesEntryTableFields[1].Id);w.Put8(2);w.Put8(9);w.Put8(0)};w.Id(RootTableFields[1].Id);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();return b[:w.Offset]}
func TestReport(t *testing.T){for _,tc:=range []struct {keys,kinds []byte;count,widen,duplicate,mismatch int32;bad bool}{{[]byte{253,2},[]byte{2,2},2,3,0,0,false},{[]byte{2,253},[]byte{2,2},1,2,0,0,true},{[]byte{2,2},[]byte{2,2},1,3,1,0,false},{[]byte{253,2},[]byte{2,6},0,2,0,1,false}} {wire:=makeWire(tc.keys,tc.kinds);size:=RootLoadMeasure(wire);if size<0{t.Fatal("measure")};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;var report TableReport;r:=RootLoad(raw[off:off+uintptr(size)],wire,&report);if r==nil||r.Entries.Count!=tc.count||r.After!=42||report.Widened!=tc.widen||report.Duplicate!=tc.duplicate||report.KindMismatch!=tc.mismatch||report.Malformed!=tc.bad {t.Fatalf("case %+v got %+v root %+v",tc,report,r)}}}
`)
}

func TestMapJsonKeyDomains(t *testing.T) {
	runGenerated(t, `package probe
table Root { keys map[int8]int32
 wide map[uint64]int32
 names map[string(300)]int32
 tiny map[string(3)]int32 }
`, `package probe
import("testing";"strings";"math")
func TestKeys(t *testing.T){long:=strings.Repeat("a",280);text:=[]byte("{\"keys\":{\"1.0\":2,\"1e0\":3,\"-1e+2\":4,\"128\":1,\"-129\":1,\"0.5\":1},\"wide\":{\"18446744073709551615\":9,\"18446744073709551616\":1,\"-1\":1,\"-0\":2},\"names\":{\""+long+"\":5},\"tiny\":{\"😀\":1,\"a\":2}}");var b RootBuilder;b.Init();defer b.Shutdown();var report TableReport;if !RootFromJson(&b,text,&report)||report.KindMismatch!=5||report.Duplicate!=1||report.Clamped!=1||report.Malformed {t.Fatalf("keys %+v",report)};if !b.Lock(){t.Fatal("lock")};r:=b.AsConst();if r.Keys.Count!=2||*RootKeysFind(nil,&r.Keys,1)!=3||*RootKeysFind(nil,&r.Keys,-100)!=4||*RootWideFind(nil,&r.Wide,math.MaxUint64)!=9||r.Wide.Count!=2||*RootNamesFind(nil,&r.Names,long)!=5||r.Tiny.Count!=1 {t.Fatal("key identities")}}
`)
}
