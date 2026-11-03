package gotable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStorageCapRefusedAtMeasure holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors in
// testdata/wire/tables/storage-cap: a wire whose region measure passes 2^40
// bytes is refused as storage_over_cap by LoadMeasure, LoadMeasureReason and
// ReadStrict, before any region is sized, and a wire under it measures. The
// list vectors reach the cap through the list extent, the node vectors
// through the node scan.
func TestStorageCapRefusedAtMeasure(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	dir, err := filepath.Abs("../../../testdata/wire/tables/storage-cap")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(dir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), strings.ReplaceAll(`package capcheck
 import ("os";"testing")
 func TestStorageCap(t *testing.T){
 const capBytes=int64(1)<<40
 for _,v:=range []struct{file string;over bool}{{"list_at.bin",false},{"list_over.bin",true},{"nodes_at.bin",false},{"nodes_over.bin",true}}{
  wire,err:=os.ReadFile("DIR/"+v.file);if err!=nil{t.Fatal(err)}
  n,err:=RootLoadMeasureReason(wire)
  if !v.over{if err!=nil||n<=0||n>capBytes||RootLoadMeasure(wire)!=n{t.Fatalf("%s: size %d reason %v plain %d",v.file,n,err,RootLoadMeasure(wire))};continue}
  if n!= -1||err!=TableRefuseStorageOverCap||RootLoadMeasure(wire)!= -1{t.Fatalf("%s: size %d reason %v plain %d",v.file,n,err,RootLoadMeasure(wire))}
  if root,err:=RootReadStrict(wire);root!=nil||err!=TableRefuseStorageOverCap{t.Fatalf("%s: strict read %v",v.file,err)}
 }
 // the extent term is refused before it is added, so no count times an
 // element size is summed past the cap: exactly at the cap reads, one
 // element past it refuses
 field:=TableFieldInfo{List:true,Kind:4,ElemSize:16,ElemAlign:4}
 for _,tc:=range []struct{from int64;reason TableRefuseReason}{{capBytes-16,TableRefuseOk},{capBytes-15,TableRefuseStorageOverCap}}{at:=tc.from;if reason:=tableWireArrayExtent(TableReader{Buffer:[]byte{4,1,0,0,0,0}},&field,false,false,&at,-1);reason!=tc.reason||reason==TableRefuseOk&&at!=capBytes{t.Fatalf("extent from %d: %v at %d",tc.from,reason,at)}}
 if TableRefuseStorageOverCap.Error()!="storage_over_cap"{t.Fatal("spelling")}
 }
 `, "DIR", dir))
}
