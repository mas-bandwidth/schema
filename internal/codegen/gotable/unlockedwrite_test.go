package gotable

import "testing"

// A write (measure, save, text, json, cook, retain) that meets an
// ARENA-encoded reference and was handed no arena refuses by name
// (arena_required) instead of writing zeros (docs/SPEC-TABLES.md §6.3). The
// same root, with its arena, and a locked root, with none, both write the same
// 104-byte wire. The encoding is told by the slot's own form, never by the
// size of a reference: a locked region whose string sits 80000 bytes past
// its slot (Big: 20000 xs, then s = "hello") writes its 80065-byte wire, locked and
// after a wire read-back.
func TestGoWriteOfUnlockedBuilderWithoutArenaRefuses(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Root { n int32
 s *string
 xs []int32
 names []string(8) }
table Big { xs []int32
 s *string }
`, `package probe
import("bytes";"errors";"testing")
const input=`+"`"+`{"n":7,"s":"hello","xs":[1,2,3],"names":["a","b"]}`+"`"+`
const pretty="{\n  \"n\": 7,\n  \"s\": \"hello\",\n  \"xs\": [\n    1,\n    2,\n    3\n  ],\n  \"names\": [\n    \"a\",\n    \"b\"\n  ]\n}\n"
func TestGoWriteOfUnlockedBuilderWithoutArenaRefuses(t *testing.T){
 t.Parallel()
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 var report TableReport
 if !RootFromJson(&b,[]byte(input),&report){t.Fatalf("fromjson %+v",report)}
 root:=b.GetRoot()
 want:=TableRefuseArenaRequired
 if got:=RootMeasure(root);got!=-1{t.Errorf("Measure=%d want -1",got)}
 if _,err:=RootMeasureReason(root);!errors.Is(err,want){t.Errorf("MeasureReason err=%v want %v",err,want)}
 buf:=make([]byte,256)
 if got:=RootSave(root,buf);got!=-1{t.Errorf("Save=%d want -1",got)}
 if !bytes.Equal(buf,make([]byte,256)){t.Errorf("a refused Save wrote into the buffer")}
 if _,err:=RootSaveText(root);!errors.Is(err,want){t.Errorf("SaveText err=%v",err)}
 if got:=RootToJsonMeasure(root);got!=-1{t.Errorf("ToJsonMeasure=%d",got)}
 if got:=RootToJson(root,buf);got!=-1{t.Errorf("ToJson=%d",got)}
 if got:=RootCookMeasure(root);got!=-1{t.Errorf("CookMeasure=%d",got)}
 if _,err:=RootCookMeasureReason(root);!errors.Is(err,want){t.Errorf("CookMeasureReason err=%v",err)}
 if got:=RootMeasureRetain(root,nil);got!=-1{t.Errorf("MeasureRetain=%d",got)}
 if got:=RootSaveRetain(root,nil,buf,&report);got!=-1{t.Errorf("SaveRetain=%d",got)}
 size:=RootMeasure(root,&b.Arena)
 if size!=104{t.Errorf("unlocked+arena measure=%d want 104",size)}
 w:=make([]byte,104)
 if n:=RootSave(root,w,&b.Arena);n!=104{t.Errorf("unlocked+arena save=%d",n)}
 back,err:=RootReadStrict(w)
 if err!=nil{t.Fatalf("readstrict: %v",err)}
 js:=make([]byte,256)
 if n:=RootToJson(back,js);n<0||string(js[:n])!=pretty{t.Errorf("read back %d %q",n,js[:max(n,0)])}
 if got:=RootMeasure(back);got!=104{t.Errorf("recovered measure=%d",got)}
 rw:=make([]byte,104)
 if n:=RootSave(back,rw);n!=104||!bytes.Equal(w,rw){t.Errorf("recovered wire does not round-trip: %d",n)}
 if !b.Lock(){t.Fatal("lock")}
 lc:=b.AsConst()
 if got:=RootMeasure(lc);got!=104{t.Errorf("locked measure=%d",got)}
 lw:=make([]byte,104)
 if n:=RootSave(lc,lw);n!=104{t.Errorf("locked save=%d",n)}
 if !bytes.Equal(w,lw){t.Errorf("locked wire differs from unlocked+arena wire")}
}
func TestGoLockedWriteOfFarReferenceIsNotRefused(t *testing.T){
 t.Parallel()
 var b BigBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 for i:=0;i<20000;i++{if p:=BigXsAdd(&b.Main,&b.GetRoot().Xs);p==nil{t.Fatal("add")}else{*p=int32(i)}}
 text,ref:=b.Main.AllocString(5)
 if text==nil{t.Fatal("alloc string")}
 copy(text,"hello")
 b.GetRoot().S=ref
 if !b.Lock(){t.Fatal("lock")}
 size,err:=BigMeasureReason(b.AsConst())
 if err!=nil||size!=80065{t.Fatalf("locked measure=%d %v want 80065",size,err)}
 w:=make([]byte,size)
 if n:=BigSave(b.AsConst(),w);n!=size{t.Fatalf("locked save=%d",n)}
 back,err:=BigReadStrict(w)
 if err!=nil{t.Fatalf("readstrict: %v",err)}
 again,err:=BigMeasureReason(back)
 if err!=nil||again!=80065{t.Fatalf("read-back measure=%d %v want 80065",again,err)}
 rw:=make([]byte,again)
 if n:=BigSave(back,rw);n!=again||!bytes.Equal(w,rw){t.Errorf("read-back wire differs: %d",n)}
}
`)
}

// A locked write never consults builder state (docs/SPEC-TABLES.md §6.3: a
// region reference is self-relative and needs no arena). The locked region is
// copied away by memcpy and the builder is shut down and built again, unlocked
// and holding other values; the copy writes the locked wire byte for byte,
// handed no arena and handed the rebuilt builder's arena alike.
func TestGoLockedWriteNeverConsultsBuilderState(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Root { n int32
 s *string
 xs []int32
 names []string(8) }
`, `package probe
import("bytes";"testing";"unsafe")
const input=`+"`"+`{"n":7,"s":"hello","xs":[1,2,3],"names":["a","b"]}`+"`"+`
const pretty="{\n  \"n\": 7,\n  \"s\": \"hello\",\n  \"xs\": [\n    1,\n    2,\n    3\n  ],\n  \"names\": [\n    \"a\",\n    \"b\"\n  ]\n}\n"
func TestGoLockedWriteNeverConsultsBuilderState(t *testing.T){
 t.Parallel()
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}
 var report TableReport
 if !RootFromJson(&b,[]byte(input),&report){t.Fatalf("fromjson %+v",report)}
 if !b.Lock(){t.Fatal("lock")}
 want:=make([]byte,104)
 if n:=RootSave(b.AsConst(),want);n!=104{t.Fatalf("locked save=%d",n)}
 words:=make([]uint64,(len(b.Region())+7)/8)
 moved:=unsafe.Slice((*byte)(unsafe.Pointer(&words[0])),len(b.Region()))
 copy(moved,b.Region())
 b.Shutdown()
 if !b.Init(){t.Fatal("re-init")}
 defer b.Shutdown()
 if !RootFromJson(&b,[]byte(`+"`"+`{"n":9,"s":"other","xs":[4],"names":["z"]}`+"`"+`),&report){t.Fatalf("fromjson again %+v",report)}
 root:=(*Root)(unsafe.Pointer(&moved[0]))
 got:=make([]byte,104)
 if n:=RootSave(root,got);n!=104||!bytes.Equal(got,want){t.Errorf("moved region, no arena: %d bytes, equal=%v",n,bytes.Equal(got,want))}
 clear(got)
 if n:=RootSave(root,got,&b.Arena);n!=104||!bytes.Equal(got,want){t.Errorf("moved region, a live builder's arena: %d bytes, equal=%v",n,bytes.Equal(got,want))}
 js:=make([]byte,256)
 if n:=RootToJson(root,js);n<0||string(js[:n])!=pretty{t.Errorf("moved region json %d %q",n,js[:max(n,0)])}
}
`)
}

// A table with no references writes unlocked with no arena exactly as it does
// with its arena: there is no arena-encoded reference to refuse.
func TestGoWriteOfNoReferenceBuilderIgnoresArena(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Plain { n int32 }
`, `package probe
import("bytes";"testing")
func TestGoWriteOfNoReferenceBuilderIgnoresArena(t *testing.T){
 t.Parallel()
 var b PlainBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 b.GetRoot().N=7
 root:=b.GetRoot()
 size:=PlainMeasure(root)
 if size<=0{t.Fatalf("measure=%d",size)}
 w:=make([]byte,size)
 if n:=PlainSave(root,w);n!=size{t.Errorf("save=%d want %d",n,size)}
 if n:=PlainMeasure(root,&b.Arena);n!=size{t.Errorf("measure with arena=%d want %d",n,size)}
 if !b.Lock(){t.Fatal("lock")}
 lw:=make([]byte,size)
 if n:=PlainSave(b.AsConst(),lw);n!=size||!bytes.Equal(w,lw){t.Errorf("locked save differs: %d",n)}
}
`)
}
