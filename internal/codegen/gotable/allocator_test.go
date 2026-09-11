package gotable

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestAllocatorOwnershipAndStandaloneWriters(t *testing.T) {
	// The emitter may be tested by any Go patch release; allocation evidence
	// belongs to the compiler running the generated probe, pinned separately.
	toolchain := os.Getenv("SCHEMA_GO_ALLOC_TOOLCHAIN")
	if toolchain == "" {
		toolchain = "go1.26.0"
		if os.Getenv("SCHEMA_GO_ALLOC_ANY_GO") == "1" {
			toolchain = runtime.Version()
		}
	}
	t.Setenv("GOTOOLCHAIN", toolchain)
	if os.Getenv("SCHEMA_GO_ALLOC_ANY_GO") == "1" {
		fmt.Fprintf(os.Stderr, "Go allocation observations on %s; NOT CERTIFIED\n", toolchain)
	}

	runGenerated(t, `package probe
 fixed table Child { n int32 }
 table Root { head *Child
 alias *Child
 data []uint16
 entries map[int32]Child
 blob *bytes }
 table Plain { child *Child }
 `, `package probe
import("testing";"unsafe";"bytes";"runtime";"os";"strconv")
func alignedAllocation(n int64)[]byte{raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}
func TestPairLifetime(t *testing.T){
 active:=map[unsafe.Pointer][]byte{};calls,frees,fail:=0,0,0
 pair:=TableAllocator{Alloc:func(n int64)[]byte{calls++;if calls==fail{return nil};b:=alignedAllocation(n+9);active[unsafe.Pointer(&b[0])]=b;return b},Free:func(b []byte){p:=unsafe.Pointer(&b[0]);old,ok:=active[p];if !ok||len(b)!=len(old)||cap(b)!=cap(old){t.Fatalf("Free must receive original slice, got %d",len(b))};delete(active,p);frees++}}
 var b PlainBuilder;if !b.Init(pair){t.Fatal("init")};root:=b.GetRoot();ChildEmplace(&b.Main,&root.Child).N=17
 if b.Init(pair)||b.GetRoot()!=root||len(active)!=1{t.Fatal("double init changed storage")}
 before:=calls;if got:=b.PackMeasure();got!=tableRegionRound(int64(unsafe.Sizeof(Plain{})))+tableRegionRound(int64(unsafe.Sizeof(Child{}))){t.Fatalf("pack measure %d",got)};if calls-before!=2||len(active)!=1{t.Fatal("measurement did not release its numbering")}
 for refused:=1;refused<=5;refused++{fail=calls+refused;if b.Lock()||b.GetRoot()!=root||len(active)!=1{t.Fatalf("pack failure %d changed source or leaked: %d",refused,len(active))};fail=0}
 before=calls;if !b.Lock()||calls-before!=5||ChildAt(&b.AsConst().Child).N!=17||len(active)!=1{t.Fatalf("two independent pack passes: allocations %d live %d",calls-before,len(active))}
 if len(b.Region())!=int(b.PackMeasure())||int64(len(b.allocation))!=b.PackMeasure()+9{t.Fatal("used extent includes excess allocation")}
 b.Shutdown();b.Shutdown();if len(active)!=0||frees>=calls{t.Fatalf("release %d calls %d frees %d",len(active),calls,frees)}
 if !b.Init(pair){t.Fatal("reuse after shutdown")};b.Shutdown();if len(active)!=0{t.Fatal("reuse leaked")}
 var malformed PlainBuilder;before=calls;if malformed.Init(TableAllocator{Alloc:pair.Alloc})||malformed.Init(TableAllocator{Free:pair.Free})||calls!=before{t.Fatal("unpaired allocator used")}
 for _,kind:=range []string{"short","unaligned"}{var returned []byte;released:=false;a:=TableAllocator{Alloc:func(n int64)[]byte{if kind=="short"{returned=make([]byte,n-1)}else{raw:=alignedAllocation(n+1);returned=raw[1:]};return returned},Free:func(b []byte){released=len(b)==len(returned)&&&b[0]==&returned[0]}};if malformed.Init(a)||!released{t.Fatalf("%s allocation not released",kind)}}
}
func TestStandalonePair(t *testing.T){
 if os.Getenv("SCHEMA_GO_ALLOC_ANY_GO")!="1"&&runtime.Version()!="go1.26.0"{t.Fatalf("allocation certification requires go1.26.0, running %s",runtime.Version())}
 if os.Getenv("SCHEMA_GO_ALLOC_ANY_GO")=="1" {t.Logf("allocation observations on %s; NOT CERTIFIED",runtime.Version())}
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport
 if !RootFromJson(&b,[]byte("{\"head\":{\"&node\":1,\"n\":7},\"alias\":{\"&node\":1},\"data\":[2,3],\"entries\":{\"9\":{\"n\":4},\"-2\":{\"n\":5}},\"blob\":\"YWJj\"}"),&report)||!b.Lock(){t.Fatal("source")};root:=b.AsConst()
 wire:=make([]byte,RootMeasure(root));RootSave(root,wire);text:=make([]byte,RootToJsonMeasure(root));RootToJson(root,text);cook:=make([]byte,RootCookMeasure(root));RootCookFrom(root,cook,TableByteOrderLittle);roots:=[]*Root{root};message:=make([]byte,RootMeasureMessages(roots,nil));RootSaveMessages(roots,message,nil)
 var pool [32][]byte;var live [32]int;for i:=range pool{pool[i]=alignedAllocation(65536)};calls,frees,fail:=0,0,0
 pair:=TableAllocator{Alloc:func(n int64)[]byte{calls++;if calls==fail{return nil};for i:=range pool{if live[i]==0&&n+9<=int64(len(pool[i])){live[i]=int(n)+9;clear(pool[i]);return pool[i][:n+9]}};panic("pool exhausted")},Free:func(b []byte){for i:=range pool{if &b[0]==&pool[i][0]{if len(b)!=live[i]||live[i]==0{panic("wrong allocation released")};live[i]=0;frees++;return}};panic("foreign allocation")}}
 wireOut,textOut,cookOut,messageOut:=make([]byte,len(wire)),make([]byte,len(text)),make([]byte,len(cook)),make([]byte,len(message))
 operations:=[]struct{name string;call func()bool}{
 {"wire measure",func()bool{return RootMeasure(root,&pair)==int64(len(wire))}},
 {"wire save",func()bool{return RootSave(root,wireOut,&pair)==int64(len(wire))&&bytes.Equal(wireOut,wire)}},
 {"json measure",func()bool{return RootToJsonMeasure(root,pair)==int64(len(text))}},
 {"json save",func()bool{return RootToJson(root,textOut,pair)==int64(len(text))&&bytes.Equal(textOut,text)}},
 {"cook measure",func()bool{return RootCookMeasure(root,&pair)==int64(len(cook))}},
 {"cook save",func()bool{return RootCookFrom(root,cookOut,TableByteOrderLittle,&pair)&&bytes.Equal(cookOut,cook)}},
 {"message measure",func()bool{return RootMeasureMessages(roots,nil,&pair)==int64(len(message))}},
 {"message save",func()bool{return RootSaveMessages(roots,messageOut,nil,&pair)==int64(len(message))&&bytes.Equal(messageOut,message)}},
 {"retain measure",func()bool{return RootMeasureRetain(root,nil,pair)==int64(len(wire))}},
 {"retain save",func()bool{return RootSaveRetain(root,nil,wireOut,&report,pair)==int64(len(wire))&&bytes.Equal(wireOut,wire)}},
 }
 for _,op:=range operations{t.Run(op.name,func(t *testing.T){calls,frees=0,0;if !op.call()||calls==0||calls!=frees{t.Fatalf("standalone pair not used or leaked %d/%d",calls,frees)};count:=calls
  for refused:=1;refused<=count;refused++{calls,frees,fail=0,0,refused;if op.call()||calls-frees!=1{t.Fatalf("failure %d accepted or leaked %d/%d",refused,calls,frees)};for _,n:=range live{if n!=0{t.Fatal("failure leaked")}}};fail=0
  runs:=20;if text:=os.Getenv("SCHEMA_GO_ALLOC_RUNS");text!=""{n,err:=strconv.Atoi(text);if err!=nil||n<20{t.Fatal("SCHEMA_GO_ALLOC_RUNS must be at least 20")};runs=n};allocations:=testing.AllocsPerRun(runs,func(){if !op.call(){panic("write")}});want:=float64(1);if op.name=="json measure"||op.name=="json save"||op.name=="cook measure"||op.name=="cook save"{want=0};if allocations!=want{t.Errorf("Go allocations outside caller-backed scratch: %v want %v",allocations,want)}
 })}
}
`)
}
