package gotable

import (
	"bytes"
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
 discarded:=func(v *Hand){if v!=nil||!report.Malformed{t.Fatalf("damage must discard the whole load: %+v report %+v",v,report)}}
 check:=func(v *Hand){if v==nil||!report.Malformed||v.EntriesCount!=0||v.Entries[1].Type!=CarryTypeNone{t.Fatalf("counted tail not reset: %+v report %+v",v,report)}}
 discarded(HandLoadTolerant(region,wire,&report));report=TableReport{};store:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainID,64)};discarded(HandLoadRetain(region,wire,&store,&report))
 var b HandBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();report=TableReport{};if HandLoadBuilderTolerant(&b,wire,&report){t.Fatal("builder: damage must discard the whole load")};check(b.GetRoot());if HandCookMeasure(b.GetRoot(),&b.Arena)<0||!b.Lock(){t.Fatal("stale tail prevents cook or lock")}
 }
 `)
}

// Count reset calls instead of timing: entry reset visits the declared bound
// once; repeated one-element fields may only visit their live prefixes.
func TestCountedTailResetWork(t *testing.T) {
	out, err := runGeneratedEdited(t, `package probe
 fixed table Child { n int32 = 7 }
 fixed table Root { values [..32000]Child }
 `, `package probe
 import("testing";"encoding/binary";"bytes")
 func TestResetWork(t *testing.T){
 var value Root;RootReset(&value);value.ValuesCount=1;value.Values[0].N=9
 base:=make([]byte,RootMeasure(&value));RootSave(&value,base)
 count:=binary.LittleEndian.Uint64(base[len(base)-8:]);tail:=len(base)-8-int(count)*8
 if base[tail-1]!=0 {t.Fatal("source terminator")}
 const repeats=64
 wire:=append([]byte{1},bytes.Repeat(base[1:tail-1],repeats)...);wire=append(wire,0);wire=append(wire,base[tail:]...)
 childResetCalls=0;var got Root;var report TableReport
 if !RootLoadTolerant(&got,wire,&report)||report!=(TableReport{})||got.ValuesCount!=1||got.Values[0].N!=9||got.Values[31999].N!=7{t.Fatalf("load: %+v",report)}
 if childResetCalls!=32000+repeats{t.Fatalf("counted tail reset work: %d calls, want %d",childResetCalls,32000+repeats)}
 }
 `, func(files map[string][]byte) {
		old := []byte("func ChildReset(value *Child) {")
		matches := 0
		for name, source := range files {
			if n := bytes.Count(source, old); n != 0 {
				matches += n
				files[name] = bytes.ReplaceAll(source, old, []byte("var childResetCalls int\nfunc ChildReset(value *Child) {\nchildResetCalls++"))
			}
		}
		if matches != 1 {
			t.Fatalf("reset counter hook matched %d declarations", matches)
		}
	})
	if err != nil {
		t.Fatalf("generated reset work: %v\n%s", err, out)
	}
}

// An accepted shorter occurrence must clear the previously live union tail.
// A damaged replacement cannot prove this: whole-load discard resets it anyway.
func TestRepeatedArrayTailDefaultsAcceptedReplacement(t *testing.T) {
	schema, err := os.ReadFile("../../../tables/arms/Carry.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), `package armdemo
 import("bytes";"encoding/binary";"testing";"unsafe")
 func TestAcceptedTail(t *testing.T){
  var source HandBuilder;if !source.Init(){t.Fatal("init")};defer source.Shutdown()
  value:=source.GetRoot();value.EntriesCount=2
  for i:=range 2 {value.Entries[i].Type=CarryTypePlain;value.Entries[i].Plain().Value=int32(13+i)}
  first:=make([]byte,HandMeasure(value,&source.Arena));if HandSave(value,first,&source.Arena)!=int64(len(first)){t.Fatal("first save")}
  value.EntriesCount=1;value.Entries[0].Plain().Value=19
  second:=make([]byte,HandMeasure(value,&source.Arena));if HandSave(value,second,&source.Arena)!=int64(len(second)){t.Fatal("second save")}
  tail:=func(w []byte)int {n:=binary.LittleEndian.Uint64(w[len(w)-8:]);return len(w)-8-int(n)*8}
  a,z:=tail(first),tail(second)
  if first[a-1]!=0||second[z-1]!=0||!bytes.Equal(first[a:],second[z:]){t.Fatal("record framing")}
  wire:=append([]byte{},first[:a-1]...);wire=append(wire,second[1:z]...);wire=append(wire,first[a:]...)
  n:=HandLoadMeasure(wire);if n<0{t.Fatal("measure")}
  raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
  var report TableReport
  check:=func(v *Hand){if v==nil||report!=(TableReport{})||v.EntriesCount!=1||v.Entries[0].Type!=CarryTypePlain||v.Entries[0].Plain().Value!=19||v.Entries[1].Type!=CarryTypeNone{t.Fatalf("counted tail not reset on accepted replacement: %+v report %+v",v,report)}}
  check(HandLoadTolerant(region,wire,&report))
  report=TableReport{};store:=TableRetain{Bytes:make([]byte,1024),Ids:make([]TableRetainID,64)}
  check(HandLoadRetain(region,wire,&store,&report))
  var b HandBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();report=TableReport{}
  if !HandLoadBuilderTolerant(&b,wire,&report){t.Fatal("accepted builder replacement refused")};check(b.GetRoot())
  if HandCookMeasure(b.GetRoot(),&b.Arena)<0||!b.Lock(){t.Fatal("tail prevents cook or lock")};check(b.AsConst())
 }
 `)
}
