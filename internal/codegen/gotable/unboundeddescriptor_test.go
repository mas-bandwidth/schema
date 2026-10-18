package gotable

import (
	"testing"
)

// TestGoUnboundedDescriptorIsExplicit pins that a container with no declared
// count bound — a map, an unbounded array []T, and an unbounded text list
// []string(M) — reports UnboundedContainer:true with ArrayBound 0 in its
// descriptor (docs/SPEC-TABLES.md §8.1), distinct from an unbounded-text
// field's Unbounded column (§2.13.1), and that a bounded [..N]T reports false
// and N. It also round-trips 100000 map entries and list elements through the
// binary, base64 and JSON carriers and reads every one back.
func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table M {
    tags map[int32]int32
    xs []int32
    words []string(8)
    caps [..4]int32
    lines []string
}
`, `package probe
import(
 "encoding/base64"
 "testing"
 "unsafe"
)
func TestGoUnboundedDescriptorIsExplicit(t *testing.T){
 const N int32 = 100000
 ti:=MTableType()
 want:=map[string]struct{c bool;b int32}{
  "tags":{true,0},"xs":{true,0},"words":{true,0},"lines":{true,0},"caps":{false,4},
 }
 for i:=range ti.Fields {
  f:=&ti.Fields[i]
  w,ok:=want[f.Name];if !ok{continue}
  if f.UnboundedContainer!=w.c||f.ArrayBound!=w.b{t.Fatalf("field %s: UnboundedContainer=%v ArrayBound=%d want %+v",f.Name,f.UnboundedContainer,f.ArrayBound,w)}
  delete(want,f.Name)
 }
 if len(want)!=0{t.Fatalf("missing fields %v",want)}
 var b MBuilder
 if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot()
 var wantMap,wantList int64
 for i:=int32(0);i<N;i++{*MTagsInsert(&b.Main,&v.Tags,i)=int32(i*3);wantMap+=int64(i)*3}
 for i:=int32(0);i<N;i++{*MXsAdd(&b.Main,&v.Xs)=i;wantList+=int64(i)}
  check:=func(what string,r *M,arena ...*TableArena){
   if r.Tags.Count!=N||r.Xs.Count!=N{t.Fatalf("%s: counts %d,%d",what,r.Tags.Count,r.Xs.Count)}
   var sm,sl int64
   MTagsEach(&r.Tags,func(k int32,pv *int32)bool{sm+=int64(*pv);return true},arena...)
   for i:=int32(0);i<r.Xs.Count;i++{sl+=int64(*r.Xs.At(i,arena...))}
   if sm!=wantMap||sl!=wantList{t.Fatalf("%s: sums %d,%d want %d,%d",what,sm,sl,wantMap,wantList)}
  }
  check("builder",v,&b.Arena)
 wire:=make([]byte,MMeasure(v,&b.Arena));if MSave(v,wire,&b.Arena)!=int64(len(wire)){t.Fatal("save")}
 size:=MLoadMeasure(wire);if size<0{t.Fatal("load measure")}
 raw:=make([]byte,size+64);at:=0;for uintptr(unsafe.Pointer(&raw[at]))%64!=0{at++}
 var report TableReport;r:=MLoad(raw[at:],wire,&report);if r==nil||report!=(TableReport{}){t.Fatalf("load %+v",report)}
 check("binary",r)
 b64,err:=MSaveText(v,&b.Arena);if err!=nil||b64!=base64.StdEncoding.EncodeToString(wire){t.Fatalf("SaveText %v",err)}
 r2,err:=MLoadText(b64,nil);if err!=nil{t.Fatalf("LoadText %v",err)};check("base64",r2)
 j:=make([]byte,MToJsonMeasure(r));if MToJson(r,j)!=int64(len(j)){t.Fatal("ToJson")}
 var jb MBuilder;jb.Init();defer jb.Shutdown();report=TableReport{}
  if !MFromJson(&jb,j,&report)||report!=(TableReport{}){t.Fatalf("FromJson %+v",report)}
  check("json",jb.GetRoot(),&jb.Arena)
}
`)
}
