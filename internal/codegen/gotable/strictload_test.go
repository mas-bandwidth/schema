package gotable

import "testing"

func TestGoStrictLoadRefusesEveryLossByName(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
table Root { n int32 = 1 | min = 0, max = 10 }
`, `package probe
import("encoding/binary";"errors";"testing")
func putFile(kind uint8,payload []byte,unknown bool)[]byte{var ids TableIds;buf:=make([]byte,64);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);if unknown{w.Id(0x1111111111111111);w.Put8(4);w.Put32(7)};w.Id(RootTableFields[0].Id);w.Put8(kind);for _,b:=range payload{w.Put8(b)};w.Put8(0);w.Trailer();return buf[:w.Offset]}
func onlyUnknown()[]byte{var ids TableIds;buf:=make([]byte,64);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(0x1111111111111111);w.Put8(4);w.Put32(7);w.Put8(0);w.Trailer();return buf[:w.Offset]}
func damaged()[]byte{id:=[]byte{1,0,0,0,0,0,0,0};out:=[]byte{1,0};out=append(out,id...);out=append(out,id...);out=binary.LittleEndian.AppendUint64(out,2);return out}
func malformed()[]byte{var ids TableIds;buf:=make([]byte,64);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(0xfffffffffffffffe);w.Put8(4);w.Put32(1);w.Put8(0);w.Trailer();return buf[:w.Offset]}
func agree(t *testing.T,wire []byte,want error){t.Helper();got,err:=RootReadStrict(wire);if want==nil{if err!=nil||got==nil{t.Fatalf("clean read: %v %+v",err,got)};}else if got!=nil||!errors.Is(err,want){t.Fatalf("ReadStrict %v want %v",err,want)}
var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport;RootLoadBuilder(&b,wire,&report);if !errors.Is(report.Err(),want)||(report.Err()==nil)!=(want==nil){t.Fatalf("report.Err %v want %v",report.Err(),want)}}
func TestStrict(t *testing.T){
if RootTableFields[0].Kind!=4{t.Fatal("kind")}
var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();b.GetRoot().N=3;n:=RootMeasure(b.GetRoot(),&b.Arena);clean:=make([]byte,n);if RootSave(b.GetRoot(),clean,&b.Arena)!=n{t.Fatal("save")}
agree(t,clean,nil)
if got,err:=RootReadStrict(clean);err!=nil||got==nil||got.N!=3{t.Fatalf("clean value %v %+v",err,got)}
agree(t,putFile(4,[]byte{100,0,0,0},false),TableRefuseClamped)
agree(t,putFile(9,[]byte{1,0,0,0,0,0,0,0},false),TableRefuseKindMismatch)
agree(t,[]byte{1},TableRefuseWireDamaged)
agree(t,[]byte{2},TableRefuseUnknownForm)
agree(t,[]byte{255},TableRefuseUnknownForm)
agree(t,damaged(),TableRefuseWireDamaged)
agree(t,malformed(),TableRefuseMalformed)
agree(t,onlyUnknown(),nil)
if got,err:=RootReadStrict(onlyUnknown());err!=nil||got==nil||got.N!=1{t.Fatalf("unknown field %v %+v",err,got)}
wide:=putFile(2,[]byte{3},false);got,err:=RootReadStrict(wide);if err!=nil||got==nil||got.N!=3{t.Fatalf("widened %v %+v",err,got)}
}
`)
}
