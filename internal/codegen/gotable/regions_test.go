package gotable

import "testing"

func TestRegionGraphs(t *testing.T) {
	runGenerated(t, `package probe
type Colour { r uint8 }
fixed table Leaf { value int32 = 7 }
table Node {
 value int32
 next *Node
 leaf *Leaf
}
table Root {
 colour Colour
 head *Node
 alias *Node
 leaves [..3]*Leaf
}
`, `package probe
import("testing";"bytes";"unsafe";"sync")
func aligned(n int64) []byte { raw:=make([]byte,n+tableRegionAlign);off:=(-uintptr(unsafe.Pointer(&raw[0])))&uintptr(tableRegionAlign-1);return raw[off:off+uintptr(n)] }
func TestGraphs(t *testing.T) {
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();r:=b.GetRoot();r.Colour.R=2
 n:=NodeEmplace(&b.Main,&r.Head);n.Value=12
 tail:=NodeEmplace(&b.Main,&n.Next);tail.Value=19
 r.Alias=n.Next
 leaf:=LeafEmplace(&b.Main,&tail.Leaf);leaf.Value=21;r.Leaves[0]=tail.Leaf;r.LeavesCount=1
 want:=RootMeasure(r,&b.Arena);wire:=make([]byte,want);if RootSave(r,wire,&b.Arena)!=want{t.Fatal("save")}
 count:=RootLoadMeasure(wire);expected:=tableRegionRound(int64(unsafe.Sizeof(Root{})))+2*tableRegionRound(int64(unsafe.Sizeof(Node{})))+tableRegionRound(int64(unsafe.Sizeof(Leaf{})))+4*16
 if count!=expected{t.Fatalf("region %d want %d",count,expected)}
 region:=aligned(count);var report TableReport;loaded:=RootLoad(region,wire,&report)
 if loaded==nil||report!=(TableReport{}) {t.Fatalf("load %+v",report)}
 a:=NodeAt(&loaded.Head);z:=NodeAt(&a.Next)
 if a.Value!=12||z.Value!=19||NodeAt(&loaded.Alias)!=z||LeafAt(&z.Leaf)!=LeafAt(&loaded.Leaves[0]){t.Fatal("sharing")}
 moved:=aligned(count);copy(moved,region);relocated:=(*Root)(unsafe.Pointer(&moved[0]));if NodeAt(&NodeAt(&relocated.Head).Next)!=NodeAt(&relocated.Alias){t.Fatal("relocation")}
 if RootMeasure(loaded)!=want {t.Fatal("measure loaded")};out:=make([]byte,want);if RootSave(loaded,out)!=want||!bytes.Equal(out,wire){t.Fatal("roundtrip")}
 if !b.Lock()||b.GetRoot()!=nil||NodeAt(&NodeAt(&b.AsConst().Head).Next)!=NodeAt(&b.AsConst().Alias){t.Fatal("lock")}
 if !bytes.Equal(b.Region(),region[:count-4*16]){t.Fatal("packed data differs from loaded data")}
 text:=make([]byte,RootToJsonMeasure(loaded));if RootToJson(loaded,text)!=int64(len(text)){t.Fatal("graph text write")}
 var from RootBuilder;from.Init();defer from.Shutdown();report=TableReport{};if !RootFromJson(&from,text,&report)||report!=(TableReport{})||!from.Lock(){t.Fatalf("graph text read %+v: %s",report,text)};if NodeAt(&NodeAt(&from.AsConst().Head).Next)!=NodeAt(&from.AsConst().Alias){t.Fatal("text sharing")}
 if allocs:=testing.AllocsPerRun(20,func(){report=TableReport{};RootLoadMeasure(wire);RootLoad(region,wire,&report)});allocs!=0{t.Fatalf("load allocations: %v",allocs)}
 var cycle RootBuilder;cycle.Init();defer cycle.Shutdown();c:=NodeEmplace(&cycle.Main,&cycle.GetRoot().Head);c.Next=cycle.GetRoot().Head
 if RootMeasure(cycle.GetRoot(),&cycle.Arena)!=-1||cycle.Lock(){t.Fatal("cycle accepted")}
 var wg sync.WaitGroup;var arena TableArena;arena.Init();defer arena.Shutdown();for k:=0;k<4;k++{wg.Go(func(){w:=TableWorker{Arena:&arena};for i:=0;i<9000;i++{p,ref:=w.Alloc(24);if p==nil||arena.At(ref)!=p{t.Error("worker allocation");return};*(*uint64)(p)=uint64(i)}})};wg.Wait()
}
`)
}

func TestRegionUnionAndBlobValues(t *testing.T) {
	runGenerated(t, `package probe
enum Mode { Fast, Slow }
type Point { x int32 }
fixed table Child { score int32 = 7 }
union Inner { n int32
 text string(8)
 ping
}
union Value {
 tally int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
 scale fixed(52,12) | min = -100, max = 100
 label string(8)
 blob bytes(8)
 samples [..3]float32
 pair [2]int16
 mode Mode
 point Point
 child Child
 origin Inner
 nested [..2]Inner
 link *Child
 ping
}
table Root {
 value Value
 other Inner
 entries [..3]Value
 head *Child
 blob *bytes
 title *string
}
`, `package probe
import("testing";"bytes";"unsafe")
func TestValues(t *testing.T){
 texts:=[]string{
 "{\"value\":{\"tally\":-170141183460469231731687303715884105728}}",
 "{\"value\":{\"scale\":12.25}}",
 "{\"value\":{\"label\":\"hi\"}}",
 "{\"value\":{\"blob\":\"YWJj\"}}",
 "{\"value\":{\"samples\":[1.25,2.5]}}",
 "{\"value\":{\"pair\":[7,9]}}",
 "{\"value\":{\"mode\":\"Slow\"}}",
 "{\"value\":{\"point\":{\"x\":7}}}",
 "{\"value\":{\"child\":{\"score\":9}}}",
 "{\"value\":{\"origin\":{\"text\":\"ok\"}}}",
 "{\"value\":{\"nested\":[{\"n\":5},{\"ping\":null}]}}",
 "{\"value\":{\"link\":{\"&node\":1,\"score\":9}},\"head\":{\"&node\":1}}",
 "{\"value\":{\"link\":null}}",
 "{\"entries\":[{\"ping\":null},{\"pair\":[2,4]}],\"blob\":\"YWI=\",\"title\":\"hi\"}",
 }
 for _,text:=range texts {
 var b RootBuilder;b.Init();var r TableReport
 if !RootFromJson(&b,[]byte(text),&r)||r!=(TableReport{})||!b.Lock(){t.Fatalf("read %s: %+v",text,r)}
 wire:=make([]byte,RootMeasure(b.AsConst()));if RootSave(b.AsConst(),wire)!=int64(len(wire)){t.Fatal("save")}
 size:=RootLoadMeasure(wire);region:=make([]byte,size+16);offset:=(-uintptr(unsafe.Pointer(&region[0])))&15;region=region[offset:offset+uintptr(size)]
 r=TableReport{};v:=RootLoad(region,wire,&r);if v==nil||r!=(TableReport{}){t.Fatalf("load %s: %+v",text,r)}
 out:=make([]byte,RootMeasure(v));if RootSave(v,out)!=int64(len(out))||!bytes.Equal(out,wire){t.Fatal("wire differs")}
 canonical:=make([]byte,RootToJsonMeasure(v));if RootToJson(v,canonical)!=int64(len(canonical)){t.Fatal("json write")}
 var again RootBuilder;again.Init();r=TableReport{};if !RootFromJson(&again,canonical,&r)||r!=(TableReport{})||!again.Lock(){t.Fatalf("reread: %s %+v",canonical,r)}
 if allocs:=testing.AllocsPerRun(10,func(){r=TableReport{};RootLoad(region,wire,&r)});allocs!=0{t.Fatalf("read allocations: %v",allocs)}
 again.Shutdown();b.Shutdown()
 }
 var b RootBuilder;b.Init();defer b.Shutdown();b.GetRoot().Value.Type=ValueTypeLink;ChildEmplace(&b.Main,&b.GetRoot().Value.Link().Value)
 wire:=make([]byte,RootMeasure(b.GetRoot(),&b.Arena));RootSave(b.GetRoot(),wire,&b.Arena)
 // A pointer arm's payload is exactly its index, with no trailing byte.
 wire[5]=2;size:=RootLoadMeasure(wire);raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;var r TableReport;v:=RootLoad(raw[off:off+uintptr(size)],wire,&r)
 if v==nil||!r.Malformed||v.Value.Type!=ValueTypeNone{t.Fatalf("pointer arm length: %+v %+v",v,r)}
}
`)
}
