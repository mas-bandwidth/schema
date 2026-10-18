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

// An abandoned unlocked builder is not retained by any process-global arena
// registry (docs/SPEC-TABLES.md §6.3): the marked slots are self-describing and
// need an arena only while the caller holds the builder.
func TestGoAbandonedBuilderIsCollectable(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root { s *string
 xs []int32 }
`, `package probe
import("runtime";"testing";"weak")
func abandonBuilder() weak.Pointer[RootBuilder] {
 b:=new(RootBuilder)
 if !b.Init(){panic("init")}
 var report TableReport
 if !RootFromJson(b,[]byte("{\"s\":\"hello\",\"xs\":[1,2,3]}"),&report){panic("fromjson")}
 p:=weak.Make(b)
 runtime.KeepAlive(b)
 return p
}
func TestGoAbandonedBuilderIsCollectable(t *testing.T) {
 t.Parallel()
 p:=abandonBuilder()
 for i:=0;i<10;i++ {
  runtime.GC()
  if p.Value()==nil{return}
 }
 t.Fatal("abandoned builder remains reachable after repeated garbage collection")
}
`)
}

// A root whose first reference is a list or map header refuses arena_required
// too (docs/SPEC-TABLES.md §6.3): a list-only root, a list of tables holding
// a *string, and a map root, through the wire, cook, JSON and message writers.
// A no-arena Find on an unlocked builder's map finds nothing and never reads an
// arena offset as a distance; with the arena it finds the entry. A LOCKED map
// handed an arena (its own, shut down, or a live builder's) measures and finds
// exactly as with none: its header is self-relative (§6.3).
func TestGoContainerWritesWithoutArenaRefuse(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Inner { s *string }
table Lst { xs []int32 }
table Tl { items []Inner }
table Mp { m map[int32]uint64 }
`, `package probe
import("errors";"testing";"unsafe")
func message(p unsafe.Pointer,t *TableTypeInfo,a *TableArena)bool{w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};return tableMessageRegionWrite(&w,p,t,a)}
func refused(t *testing.T,name string,size int64,err error){t.Helper();if size!=-1||!errors.Is(err,TableRefuseArenaRequired){t.Errorf("%s: %d %v want -1 arena_required",name,size,err)}}
func TestGoContainerWritesWithoutArenaRefuse(t *testing.T){
 t.Parallel()
 var l LstBuilder
 if !l.Init(){t.Fatal("init")}
 defer l.Shutdown()
 for i:=0;i<3;i++{p:=LstXsAdd(&l.Main,&l.GetRoot().Xs);if p==nil{t.Fatal("add")};*p=int32(i)}
 lr:=l.GetRoot()
 n,err:=LstMeasureReason(lr);refused(t,"list measure",n,err)
 if got:=LstSave(lr,make([]byte,64));got!=-1{t.Errorf("list save=%d",got)}
 n,err=LstCookMeasureReason(lr);refused(t,"list cook",n,err)
 if got:=LstToJsonMeasure(lr);got!=-1{t.Errorf("list json=%d",got)}
 if message(unsafe.Pointer(lr),LstTableType(),nil){t.Errorf("list message write without arena succeeded")}
 if !message(unsafe.Pointer(lr),LstTableType(),&l.Arena){t.Errorf("list message write with arena failed")}
 if got:=LstMeasure(lr,&l.Arena);got<=0{t.Errorf("list measure with arena=%d",got)}
 var tl TlBuilder
 if !tl.Init(){t.Fatal("init")}
 defer tl.Shutdown()
 item:=TlItemsAdd(&tl.Main,&tl.GetRoot().Items)
 if item==nil{t.Fatal("add")}
 text,ref:=tl.Main.AllocString(2)
 if text==nil{t.Fatal("alloc")}
 copy(text,"hi");item.S=ref
 tr:=tl.GetRoot()
 n,err=TlMeasureReason(tr);refused(t,"list of tables measure",n,err)
 n,err=TlCookMeasureReason(tr);refused(t,"list of tables cook",n,err)
 if message(unsafe.Pointer(tr),TlTableType(),nil){t.Errorf("list of tables message write without arena succeeded")}
 if got:=TlMeasure(tr,&tl.Arena);got<=0{t.Errorf("list of tables measure with arena=%d",got)}
 var m MpBuilder
 if !m.Init(){t.Fatal("init")}
 defer m.Shutdown()
 v:=MpMInsert(&m.Main,&m.GetRoot().M,1)
 if v==nil{t.Fatal("insert")}
 *v=5
 mr:=m.GetRoot()
 n,err=MpMeasureReason(mr);refused(t,"map measure",n,err)
 n,err=MpCookMeasureReason(mr);refused(t,"map cook",n,err)
 if got:=MpToJsonMeasure(mr);got!=-1{t.Errorf("map json=%d",got)}
 if message(unsafe.Pointer(mr),MpTableType(),nil){t.Errorf("map message write without arena succeeded")}
 if MpMFind(nil,&mr.M,1)!=nil{t.Errorf("no-arena Find on an unlocked map found an entry")}
 if p:=MpMFind(&m.Arena,&mr.M,1);p==nil||*p!=5{t.Errorf("Find with the arena: %v",p)}
 storage:=make([]byte,MpMIndexMeasure(&mr.M))
 index:=MpMIndex(&mr.M,storage)
 if MpMIndexFind(index,&mr.M,1)!=nil{t.Errorf("no-arena IndexFind on an unlocked map found an entry")}
 if got:=MpMeasure(mr,&m.Arena);got<=0{t.Errorf("map measure with arena=%d",got)}
 if !m.Lock(){t.Fatal("lock map")}
 want,err:=MpMeasureReason(m.AsConst())
 if err!=nil||want<=0{t.Fatalf("locked map measure=%d %v",want,err)}
 if got,err:=MpMeasureReason(m.AsConst(),&m.Arena);err!=nil||got!=want{t.Errorf("locked map handed its locked arena: %d %v want %d",got,err,want)}
 var live MpBuilder
 if !live.Init(){t.Fatal("init live")}
 defer live.Shutdown()
 if v:=MpMInsert(&live.Main,&live.GetRoot().M,2);v==nil{t.Fatal("insert live")}
 if got,err:=MpMeasureReason(m.AsConst(),&live.Arena);err!=nil||got!=want{t.Errorf("locked map handed a live arena: %d %v want %d",got,err,want)}
 if p:=MpMFind(&live.Arena,&m.AsConst().M,1);p==nil||*p!=5{t.Errorf("locked map Find with a live arena: %v",p)}
}
`)
}

// A LOCKED map's header is an unmarked, self-relative reference: handed an
// arena (its own, shut down by Lock, or a live builder's) the wire, cook, JSON
// and message writers write byte for byte what they write with none
// (docs/SPEC-TABLES.md §6.3). Before tableMapCursor read the mark, the wire
// measure handed &b.Arena SIGSEGVd (the r6 read's repro).
func TestGoLockedMapWithArenaWritesAsWithout(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Mp { m map[int32]uint64 }
`, `package probe
import("bytes";"testing";"unsafe")
func messageBytes(t *testing.T,p unsafe.Pointer,a *TableArena)[]byte{t.Helper();w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};if !tableMessageRegionWrite(&w,p,MpTableType(),a){t.Fatalf("message measure (arena %v)",a!=nil)};buf:=make([]byte,(w.Bits+7)/8);w=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:buf}};if !tableMessageRegionWrite(&w,p,MpTableType(),a)||w.Overflow{t.Fatalf("message write (arena %v)",a!=nil)};return buf}
func TestGoLockedMapWithArenaWritesAsWithout(t *testing.T){
 t.Parallel()
 var b MpBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 for k:=int32(3);k>0;k--{v:=MpMInsert(&b.Main,&b.GetRoot().M,k);if v==nil{t.Fatal("insert")};*v=uint64(k)*10}
 if !b.Lock(){t.Fatal("lock")}
 var live MpBuilder
 if !live.Init(){t.Fatal("init live")}
 defer live.Shutdown()
 if v:=MpMInsert(&live.Main,&live.GetRoot().M,9);v==nil{t.Fatal("insert live")}
 root:=b.AsConst()
 size,err:=MpMeasureReason(root)
 if err!=nil||size<=0{t.Fatalf("locked measure=%d %v",size,err)}
 wire:=make([]byte,size)
 if n:=MpSave(root,wire);n!=size{t.Fatalf("locked save=%d",n)}
 cookSize,err:=MpCookMeasureReason(root)
 if err!=nil||cookSize<=0{t.Fatalf("locked cook measure=%d %v",cookSize,err)}
 cook:=make([]byte,cookSize)
 if !MpCookFrom(root,cook,TableByteOrderLittle){t.Fatal("locked cook")}
 json:=make([]byte,MpToJsonMeasure(root))
 if n:=MpToJson(root,json);n!=int64(len(json)){t.Fatalf("locked json=%d",n)}
 message:=messageBytes(t,unsafe.Pointer(root),nil)
 for _,c:=range []struct{name string;arena *TableArena}{{"its own locked arena",&b.Arena},{"a live builder's arena",&live.Arena}}{
  got,err:=MpMeasureReason(root,c.arena)
  if err!=nil||got!=size{t.Errorf("%s: wire measure %d %v want %d",c.name,got,err,size);continue}
  w:=make([]byte,size)
  if n:=MpSave(root,w,c.arena);n!=size||!bytes.Equal(w,wire){t.Errorf("%s: wire differs (%d)",c.name,n)}
  cs,err:=MpCookMeasureReason(root,c.arena)
  if err!=nil||cs!=cookSize{t.Errorf("%s: cook measure %d %v want %d",c.name,cs,err,cookSize);continue}
  ck:=make([]byte,cookSize)
  if !MpCookFrom(root,ck,TableByteOrderLittle,c.arena)||!bytes.Equal(ck,cook){t.Errorf("%s: cook differs",c.name)}
  if m:=messageBytes(t,unsafe.Pointer(root),c.arena);!bytes.Equal(m,message){t.Errorf("%s: message differs",c.name)}
  if p:=MpMFind(c.arena,&root.M,2);p==nil||*p!=20{t.Errorf("%s: Find: %v",c.name,p)}
 }
 js:=make([]byte,len(json))
 if n:=MpToJson(root,js);n!=int64(len(json))||!bytes.Equal(js,json){t.Errorf("json not stable")}
}
`)
}

// The generated <T>At(slot, arena...) accessor chooses the encoding from the
// slot's mark (docs/SPEC-TABLES.md §6.3), never from whether an arena was
// handed: on a locked root it gives the same row with no arena, with the
// builder's locked arena and with a live builder's arena; on an unlocked root
// with no arena it gives nil, never a wild pointer; with the arena, the row.
func TestGoGeneratedAtChoosesTheEncodingFromTheMark(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Child { n int32 }
table Root { head *Child }
`, `package probe
import "testing"
func TestGoGeneratedAtChoosesTheEncodingFromTheMark(t *testing.T){
 t.Parallel()
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 r:=b.GetRoot()
 c:=ChildEmplace(&b.Main,&r.Head)
 if c==nil{t.Fatal("emplace")}
 c.N=7
 if ChildAt(&r.Head)!=nil{t.Errorf("unlocked slot with no arena gave a row")}
 if got:=ChildAt(&r.Head,&b.Arena);got==nil||got.N!=7{t.Errorf("unlocked slot with its arena: %v",got)}
 var live RootBuilder
 if !live.Init(){t.Fatal("init live")}
 defer live.Shutdown()
 if ChildEmplace(&live.Main,&live.GetRoot().Head)==nil{t.Fatal("emplace live")}
 if !b.Lock(){t.Fatal("lock")}
 lc:=b.AsConst()
 want:=ChildAt(&lc.Head)
 if want==nil||want.N!=7{t.Fatalf("locked slot with no arena: %v",want)}
 if got:=ChildAt(&lc.Head,&b.Arena);got!=want{t.Errorf("locked slot with its locked arena: %p want %p",got,want)}
 if got:=ChildAt(&lc.Head,&live.Arena);got!=want{t.Errorf("locked slot with a live arena: %p want %p",got,want)}
}
`)
}
