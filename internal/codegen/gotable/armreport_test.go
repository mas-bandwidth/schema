package gotable

import (
	"os"
	"testing"
)

func TestPointerArmLengthBeforeResolution(t *testing.T) {
	schema, err := os.ReadFile("../../../tables/stream/Stream.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), `package streamdemo
 import("testing";"encoding/hex";"unsafe")
 func TestArmReport(t *testing.T){wire,_:=hex.DecodeString("01010806000000020f031108010500000000c558f9a1a79d0102040e0411020302050c13020607070e03060107000607070e03060108070e030601080000c03a5cb5072eb7082652b3285633d4d809484f69ad9b4bbfc558f9a1a79d510cffffffffffffffff82e051f9dc6843cf054aa33067555b850700000000000000")
 size:=FeedLoadMeasure(wire);raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)];var r TableReport;check:=func(v *Feed){if v==nil||!r.Malformed||r.KindMismatch!=0||v.Frame.Type!=FrameTypeNone{t.Fatalf("arm framing must precede resolution: %+v",r)}}
 check(FeedLoad(region,wire,&r));r=TableReport{};retain:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainId,64)};check(FeedLoadRetain(region,wire,&retain,&r));r=TableReport{};var b FeedBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();FeedLoadBuilder(&b,wire,&r);check(b.GetRoot())
 }
 `)
}
