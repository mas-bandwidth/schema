package gotable

import "testing"

func TestBlockBuilderStorageAndParallelFill(t *testing.T) {
	runGenerated(t, `package probe
table Row { value uint64
 flag bool }
table Root { stamp uint64
 rows [..65]Row
 other [..3]Row }
`, `package probe
import("testing";"bytes";"unsafe";"sync")
func TestBuild(t *testing.T) {
 allocs,frees:=0,0
 var original []byte
 allocator:=TableBlockAllocator{Alloc:func(n int64) []byte {allocs++;if n!=RootBlockMaxBytes+63 {t.Fatal("allocation size")};original=bytes.Repeat([]byte{0xa5},int(n)+9);return original},Free:func(p []byte){frees++;if len(p)!=len(original)||&p[0]!=&original[0] {t.Fatal("free changed allocation")}}}
 var storage RootBlockStorage
 if !storage.Create(allocator)||allocs!=1||storage.Create(allocator)||allocs!=1 {t.Fatal("create lifetime")}
 defer storage.Destroy()
 var block RootBlock;var refusal TableBlockRefusal
 counts:=RootCounts{Rows:17,Other:2}
 if !RootBlockBegin(&block,&storage,counts,&refusal)||refusal!=(TableBlockRefusal{}) {t.Fatal("begin")}
 if uintptr(block.Base)%64!=0||block.Bytes!=448||RootBlockBytes(&block)!=448||block.Projection.Rows.OffsetOf!=64||block.Projection.Other.OffsetOf!=384||block.Projection.Rows.Stride!=16 {t.Fatalf("independent layout: %+v",block)}
 if block.Rows().At(0).Value!=0xa5a5a5a5a5a5a5a5||block.Projection.Stamp!=0xa5a5a5a5a5a5a5a5 {t.Fatal("begin touched caller fields")}
 snapshot:=append([]byte(nil),original...)
 previous:=block
 if RootBlockBegin(&block,&storage,RootCounts{Rows:66},&refusal)||block!=previous||refusal.Field!="rows"||refusal.Count!=66||refusal.Maximum!=65||!bytes.Equal(snapshot,original) {t.Fatal("maximum refusal changed storage")}
 if RootBlockBegin(&block,&storage,RootCounts{Other:-1},&refusal)||refusal.Field!="other"||refusal.Count!=-1 {t.Fatal("negative count")}
 clear(original)
 if !RootBlockBegin(&block,&storage,counts,nil){t.Fatal("begin again")}
 fill:=func(start,end int){rows:=block.RowsSpan();for i:=start;i<end;i++{rows[i].Value=uint64(i*i+7);rows[i].Flag=i%2==0}}
 fill(0,17)
 serial:=append([]byte(nil),unsafe.Slice((*byte)(block.Base),block.Bytes)...)
 clear(original);RootBlockBegin(&block,&storage,counts,nil)
 var wg sync.WaitGroup
 for worker:=0;worker<4;worker++ {start,end:=worker*17/4,(worker+1)*17/4;wg.Go(func(){fill(start,end)})};wg.Wait()
 if !bytes.Equal(serial,unsafe.Slice((*byte)(block.Base),block.Bytes)){t.Fatal("parallel fill differs")}
 var reopened RootBlock
 if !RootBlockOpen(&reopened,block.Base,block.Bytes)||reopened.Rows().At(16).Value!=263 {t.Fatal("open built block")}
 if n:=testing.AllocsPerRun(20,func(){if !RootBlockBegin(&block,&storage,counts,nil){panic("begin")};fill(0,17);RootBlockBytes(&block)});n!=0||allocs!=1{t.Fatalf("frame allocated: heap=%v pair=%d",n,allocs)}
 if !RootBlockBegin(&block,&storage,RootCounts{Rows:65,Other:3},nil)||block.Bytes!=RootBlockMaxBytes{t.Fatal("allocate max")}
 previous=block
 storage.Destroy();storage.Destroy();if frees!=1 {t.Fatal("release lifetime")}
 if RootBlockBegin(&block,&storage,counts,nil)||block!=previous {t.Fatal("destroyed storage")}
 var empty RowBlockStorage;if !empty.Create(TableBlockDefaultAllocator()){t.Fatal("empty create")};defer empty.Destroy();var one RowBlock
 if !RowBlockBegin(&one,&empty,RowCounts{},nil)||one.Bytes!=RowBlockMaxBytes{t.Fatal("no arrays")}
}
func TestBadAllocator(t *testing.T){
 var storage RootBlockStorage;calls,frees:=0,0
 a:=TableBlockAllocator{Alloc:func(n int64) []byte{calls++;return make([]byte,n-1)},Free:func([]byte){frees++}}
 if storage.Create(a)||calls!=1||frees!=1||storage.base!=nil{t.Fatal("short allocation")}
 a.Free=nil;if storage.Create(a)||calls!=1{t.Fatal("unpaired allocator")}
}
`)
}
