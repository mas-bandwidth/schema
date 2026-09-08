package gotable

import "testing"

func TestMeasureRefusalReasons(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	runGenerated(t, `package probe
 table Child { values []int32 }
 table Root { values []int32
 entries map[int32]int32
 child Child
 next *Root }
 `, `package probe
 import("testing";"unsafe")
 var measureSizeSink int64
 var measureErrorSink error
 func arrayFile(field int,count uint64)[]byte{var ids TableIds;wire:=make([]byte,128);w:=TableWriter{Buffer:wire,Ids:&ids};w.Put8(1);w.Id(RootTableFields[field].Id);w.Put8(14);w.PutLeb(uint64(1+tableLebBytes(count)));kind:=uint8(4);if field==1{kind=13};w.Put8(kind);w.PutLeb(count);w.Put8(0);w.Trailer();return wire[:w.Offset]}
 func TestMeasureReasons(t *testing.T){
 if TableRefuseWireDamaged.Error()!="wire_damaged"{t.Fatal("damage spelling")};for reason:=TableRefuseOk;reason<=TableRefuseWireDamaged;reason++{if n:=testing.AllocsPerRun(20,func(){measureErrorSink=reason});n!=0{t.Fatalf("escaping reason %v allocated %v",reason,n)}}
 if tableRefuseError(TableRefuseOk)!=nil{t.Fatal("zero reason became non-nil error")}
 for _,field:=range []int{0,1}{for _,tc:=range []struct{count uint64;reason TableRefuseReason}{{1,TableRefuseCountOverLength},{1<<31,TableRefuseCountOverExtentCap},{1<<32,TableRefuseCountOverExtentCap}}{
 wire:=arrayFile(field,tc.count);if n,err:=RootLoadMeasureReason(wire);n!=-1||err!=tc.reason||RootLoadMeasure(wire)!=n{t.Fatalf("field %d count %d: size %d reason %v",field,tc.count,n,err)}
 if n:=testing.AllocsPerRun(20,func(){measureSizeSink,measureErrorSink=RootLoadMeasureReason(wire)});n!=0{t.Fatalf("file refusal allocated %v",n)}
 }}
 for _,tc:=range []struct{wire []byte;reason TableRefuseReason}{{nil,TableRefuseWireDamaged},{[]byte{1},TableRefuseWireDamaged},{[]byte{2},TableRefuseUnknownForm},{[]byte{255},TableRefuseUnknownForm}}{if n,err:=RootLoadMeasureReason(tc.wire);n!=-1||err!=tc.reason{t.Fatalf("file reason %d %v",n,err)}}
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();root:=b.GetRoot();*RootValuesAdd(&b.Main,&root.Values)=9
 wire:=make([]byte,RootMeasure(root,&b.Arena));RootSave(root,wire,&b.Arena)
 if size,err:=RootLoadMeasureReason(wire);err!=nil||size!=RootLoadMeasure(wire){t.Fatalf("successful file size %d %v",size,err)}
 if n:=testing.AllocsPerRun(20,func(){measureSizeSink,measureErrorSink=RootLoadMeasureReason(wire)});n!=0{t.Fatalf("file measure allocated %v",n)}
 if n,err:=RootMeasureReason(root,&b.Arena);err!=nil||n!=int64(len(wire)){t.Fatal("wire success",n,err)}
 if n,err:=RootCookMeasureReason(root,&b.Arena);err!=nil||n!=RootCookMeasure(root,&b.Arena){t.Fatal("cook success",n,err)}
 if n,err:=b.PackMeasureReason();err!=nil||n!=b.PackMeasure(){t.Fatal("pack success",n,err)}
 root.Next=b.root
 for _,op:=range []struct{name string;call func()(int64,error);plain func()int64}{{"wire",func()(int64,error){return RootMeasureReason(root,&b.Arena)},func()int64{return RootMeasure(root,&b.Arena)}},{"cook",func()(int64,error){return RootCookMeasureReason(root,&b.Arena)},func()int64{return RootCookMeasure(root,&b.Arena)}},{"pack",b.PackMeasureReason,b.PackMeasure}}{if n,err:=op.call();n!=-1||err!=TableRefuseDataCycle||op.plain()!=n{t.Fatalf("%s cycle: %d %v",op.name,n,err)}}
 root.Next=0;pair:=TableAllocator{Alloc:func(int64)[]byte{return nil},Free:func([]byte){}}
 if _,err:=RootMeasureReason((*Root)(unsafe.Pointer(nil)),pair);err!=TableRefuseInvalidValue{t.Fatal("nil root",err)}
 var empty Root;if _,err:=RootMeasureReason(&empty,&pair);err!=TableRefuseAllocationFailed{t.Fatal("allocator reason",err)}
 }
 `)
}
