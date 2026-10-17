package gotable

import "testing"

// A write (measure, save, text, json, cook, retain) that meets an
// ARENA-encoded reference and was handed no arena refuses by name
// (arena_required) instead of writing zeros. The same root, with its arena,
// and a locked root, with none, both write the same 104-byte wire.
func TestGoWriteOfUnlockedBuilderWithoutArenaRefuses(t *testing.T) {
	runGenerated(t, `package probe
table Root { n int32
 s *string
 xs []int32
 names []string(8) }
`, `package probe
import("bytes";"errors";"testing")
func TestGoWriteOfUnlockedBuilderWithoutArenaRefuses(t *testing.T){
 t.Parallel()
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 var report TableReport
 if !RootFromJson(&b,[]byte(`+"`"+`{"n":7,"s":"hello","xs":[1,2,3],"names":["a","b"]}`+"`"+`),&report){t.Fatalf("fromjson %+v",report)}
 root:=b.GetRoot()
 want:=TableRefuseArenaRequired
 if got:=RootMeasure(root);got!=-1{t.Errorf("Measure=%d want -1",got)}
 if _,err:=RootMeasureReason(root);!errors.Is(err,want){t.Errorf("MeasureReason err=%v want %v",err,want)}
 buf:=make([]byte,256)
 if got:=RootSave(root,buf);got!=-1{t.Errorf("Save=%d want -1",got)}
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
`)
}

// A table with no references writes unlocked with no arena exactly as it does
// with its arena: there is no arena-encoded reference to refuse.
func TestGoWriteOfNoReferenceBuilderIgnoresArena(t *testing.T) {
	runGenerated(t, `package probe
table Plain { n int32 }
`, `package probe
import "testing"
func TestGoWriteOfNoReferenceBuilderIgnoresArena(t *testing.T){
 t.Parallel()
 var b PlainBuilder
 if !b.Init(){t.Fatal("init")}
 defer b.Shutdown()
 b.GetRoot().N=7
 root:=b.GetRoot()
 size:=RootMeasure(root)
 if size<=0{t.Fatalf("measure=%d",size)}
 w:=make([]byte,size)
 if n:=RootSave(root,w);n!=size{t.Errorf("save=%d want %d",n,size)}
 if n:=RootMeasure(root,&b.Arena);n!=size{t.Errorf("measure with arena=%d want %d",n,size)}
 if !b.Lock(){t.Fatal("lock")}
 lw:=make([]byte,size)
 if n:=RootSave(b.AsConst(),lw);n!=size||!bytesEqual(w,lw){t.Errorf("locked save differs: %d",n)}
}
`)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
