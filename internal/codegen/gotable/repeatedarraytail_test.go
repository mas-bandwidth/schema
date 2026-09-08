package gotable

import (
	"os"
	"testing"
)

// The #725 reference reproducer: a malformed replacement decodes no entries.
func TestRepeatedArrayTailDefaults(t *testing.T) {
	schema, err := os.ReadFile("../../../tables/arms/Carry.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), `package armdemo
 import("testing";"encoding/hex";"unsafe")
 func TestCountedTail(t *testing.T){wire,_:=hex.DecodeString("01010e1e0f0202040401000000030d12040e0e040305000000060000000700000000010e1e0f0202047f01000000030d12040e0e0403050000000600000007000000000504020000000053a245082ca7b2c507b252164e194dfdd50802a2ad84ad246f2c414fbf84783ee9ea716f0f0182bf0500000000000000")
 size:=HandLoadMeasure(wire);if size<0{t.Fatal("measure")};raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(size)];var report TableReport
 check:=func(v *Hand){if v==nil||!report.Malformed||v.EntriesCount!=0||v.Entries[1].Type!=CarryTypeNone{t.Fatalf("counted tail not reset: %+v report %+v",v,report)}}
 check(HandLoad(region,wire,&report));report=TableReport{};store:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainId,64)};check(HandLoadRetain(region,wire,&store,&report))
 var b HandBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();report=TableReport{};if !HandLoadBuilder(&b,wire,&report){t.Fatal("builder")};check(b.GetRoot());if HandCookMeasure(b.GetRoot(),&b.Arena)<0||!b.Lock(){t.Fatal("stale tail prevents cook or lock")}
 }
 `)
}
