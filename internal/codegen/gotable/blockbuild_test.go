package gotable

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

const blockBuilderSchema = `package probe
table Row { value uint64
 flag bool }
table Root { stamp uint64
 rows [..65]Row
 other [..3]Row }
`
const blockBuilderProbe = `package probe
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
 if RootBlockBegin(&block,&storage,RootCounts{Rows:66},&refusal)||block!=previous||refusal.Array!="rows"||refusal.Count!=66||refusal.Maximum!=65||!bytes.Equal(snapshot,original) {t.Fatal("maximum refusal changed storage")}
 if RootBlockBegin(&block,&storage,RootCounts{Other:-1},&refusal)||refusal.Array!="other"||refusal.Count!=-1 {t.Fatal("negative count")}
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
`

func TestBlockBuilderStorageAndParallelFill(t *testing.T) {
	if out, err := runGeneratedResult(t, blockBuilderSchema, blockBuilderProbe, "-race"); err != nil {
		t.Fatalf("block builder under race detector: %v\n%s", err, out)
	}
}
func TestBlockBuilderRaceNegativeControl(t *testing.T) {
	mutant := strings.Replace(blockBuilderProbe, "worker*17/4,(worker+1)*17/4", "0,17", 1)
	if mutant == blockBuilderProbe {
		t.Fatal("race sabotage did not apply")
	}
	out, err := runGeneratedResult(t, blockBuilderSchema, mutant, "-race", "-run=TestBuild$")
	if err == nil || !strings.Contains(string(out), "WARNING: DATA RACE") {
		t.Fatalf("overlapping fill must fail the race detector: %v\n%s", err, out)
	}
}

func blockFillGate(source string) error {
	const begin = "// ---- block fill path: begin ----"
	const end = "// ---- block fill path: end ----"
	regions := strings.Count(source, begin)
	if regions == 0 || regions != strings.Count(source, end) {
		return fmt.Errorf("missing or unbalanced fill markers")
	}
	forbidden := regexp.MustCompile(`\b(make|new|append)\s*\(|\b(sync|atomic)\s*\.`)
	for _, part := range strings.Split(source, begin)[1:] {
		finish := strings.Index(part, end)
		if finish < 0 {
			return fmt.Errorf("missing end marker")
		}
		if token := forbidden.FindString(part[:finish]); token != "" {
			return fmt.Errorf("fill path contains %s", token)
		}
	}
	return nil
}
func TestBlockFillRefuser(t *testing.T) {
	files, err := Generate(unitFrom(t, blockBuilderSchema))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for name, source := range files {
		if !strings.HasSuffix(name, "Block.go") {
			continue
		}
		checked++
		if err := blockFillGate(string(source)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if checked == 0 {
		t.Fatal("no block source")
	}
}
func TestBlockFillRefuserNegativeControl(t *testing.T) {
	files, err := Generate(unitFrom(t, blockBuilderSchema))
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range files {
		if !strings.HasSuffix(name, "Block.go") {
			continue
		}
		for _, token := range []string{"make([]byte,1)", "new(int)", "append(rows,row)", "sync.Mutex{}", "atomic.AddInt64(&count,1)"} {
			mutant := strings.Replace(string(source), "// ---- block fill path: begin ----", "// ---- block fill path: begin ----\n"+token, 1)
			if err := blockFillGate(mutant); err == nil {
				t.Fatalf("%s accepted %s", name, token)
			}
		}
	}
}
