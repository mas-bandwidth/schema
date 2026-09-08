package gotable

import "testing"

func TestAcceleratorTypedRefusals(t *testing.T) {
	runGenerated(t, blockBuilderSchema, `package probe
import("testing";"unsafe";"encoding/binary")
func TestTypedOpen(t *testing.T){
 var storage RootBlockStorage;if !storage.Create(TableBlockDefaultAllocator()){t.Fatal("storage")};defer storage.Destroy()
 var built,block RootBlock;if !RootBlockBegin(&built,&storage,RootCounts{Rows:1},nil){t.Fatal("begin")}
 if err:=block.Open(built.Base,built.Bytes);err!=nil{t.Fatal(err)}
 if n:=testing.AllocsPerRun(50,func(){if block.Open(built.Base,built.Bytes)!=nil{panic("open")}});n!=0{t.Fatalf("successful block open allocated %v",n)}
 raw:=make([]byte,built.Bytes+65);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;bad:=raw[off+1:off+1+uintptr(built.Bytes)];copy(bad,unsafe.Slice((*byte)(built.Base),built.Bytes));base:=unsafe.Pointer(&bad[0])
 if err:=block.Open(base,built.Bytes);err!=TableRefuseUnalignedBase||block.Base!=nil{t.Fatalf("block base %v",err)}
 binary.NativeEndian.PutUint32(bad[unsafe.Offsetof(RootBlockProjection{}.Rows)+12:],0)
 if err:=block.Open(base,built.Bytes);err!=TableRefuseBadLayout{t.Fatalf("layout precedes base: %v",err)}
 var root Root;RootReset(&root);size:=RootCookMeasure(&root);raw=make([]byte,size+65);off=(-uintptr(unsafe.Pointer(&raw[0])))&63;data:=raw[off:off+uintptr(size)];if !RootCookFrom(&root,data,TableByteOrder(TableCookByteOrder)){t.Fatal("cook")}
 var cook RootCook;if err:=cook.Open(unsafe.Pointer(&data[0]),size);err!=nil{t.Fatal(err)}
 if n:=testing.AllocsPerRun(50,func(){if cook.Open(unsafe.Pointer(&data[0]),size)!=nil{panic("open")}});n!=0{t.Fatalf("successful cook open allocated %v",n)}
 for _,tc:=range []struct{base unsafe.Pointer;size int64;want TableRefuseReason}{{nil,0,TableRefuseUnalignedBase},{unsafe.Pointer(&data[0]),0,TableRefuseTruncated}}{if err:=cook.Open(tc.base,tc.size);err!=tc.want||cook.Region!=nil{t.Fatalf("cook %v want %v",err,tc.want)}}
 binary.NativeEndian.PutUint64(data[8:],BuildVersion^1);binary.NativeEndian.PutUint64(data[40:],0)
 if err:=cook.Open(unsafe.Pointer(&data[0]),size);err!=TableRefuseWrongBuildVersion{t.Fatalf("version precedes alignment: %v",err)}
 var err error=TableRefuseBadLayout;if reason,ok:=err.(TableRefuseReason);!ok||reason.Error()!="bad_layout"{t.Fatal("typed error")}
}
`)
}
