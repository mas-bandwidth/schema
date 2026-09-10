package gotable

import (
	"testing"
)

const structArrayWireSchema = `package probe
enum Flavor { Vanilla, Chocolate, Strawberry }
type Leaf { flavor Flavor
 note uint32 = 7 }
type Child { flavor Flavor
 payload bytes(130)
 nested Leaf }
table Root {
 children [..128]Child
 pair [2]Child
 large [..129]Child
 optional ?[..2]Child
}
`

// The reference writer builds the wire independently, including vocabulary
// first-use order and default elision. The compiled generated runtime is also
// exercised over 1, 2, 128 and 129 elements, with reused load targets.
func TestStructArrayWire(t *testing.T) {
	runGenerated(t, structArrayWireSchema, structArrayWireTest)
}

func TestNestedStructArrayWire(t *testing.T) {
	// A struct array nested under unions and under other arrays writes and
	// reads back the same bytes, with no allocation, on the variable form.
	runGenerated(t, `package probe
type Leaf { n int32 }
type Child { leaves [..2]Leaf }
union Choice { small [..3]Child
 large [..5]Child }
table Root { direct [2]Child
 choice Choice
 choices [..2]Choice
 after [3]Child }
`, `package probe
import("bytes";"testing")
func TestNestedScratch(t *testing.T) {
 texts:=[]string{
  "{}",
  "{\"direct\":[{\"leaves\":[{\"n\":1},{\"n\":2}]},{}],\"choice\":{\"large\":[{},{\"leaves\":[{\"n\":3}]}]},\"choices\":[{\"small\":[{\"leaves\":[{\"n\":4}]}]},{\"large\":[{},{},{\"leaves\":[{\"n\":5},{\"n\":6}]}]}],\"after\":[{},{\"leaves\":[{\"n\":7}]},{}]}",
  "{\"choice\":{\"small\":[]},\"choices\":[{\"large\":[]},{\"small\":[{},{}]}]}",
 }
 var loaded Root
 for _,text:=range texts {
  var value Root;var report TableReport
  if !RootFromJson(&value,[]byte(text),&report)||report!=(TableReport{}) {t.Fatalf("input: %+v",report)}
  wire:=make([]byte,RootMeasure(&value));out:=make([]byte,len(wire))
  if RootSave(&value,wire)!=int64(len(wire))||!RootLoad(&loaded,wire,&report)||report!=(TableReport{}) {t.Fatalf("nested scratch changed value: %+v",report)}
  // Dormant union arms are reused storage, not logical fields. Compare the
  // public text/wire forms rather than inactive slots from the previous load.
  a,b:=make([]byte,RootToJsonMeasure(&value)),make([]byte,RootToJsonMeasure(&loaded))
  if RootToJson(&value,a)!=int64(len(a))||RootToJson(&loaded,b)!=int64(len(b))||!bytes.Equal(a,b) {t.Fatal("nested scratch changed logical fields")}
  if RootSave(&loaded,out)!=int64(len(wire))||!bytes.Equal(out,wire) {t.Fatal("nested scratch changed wire")}
  if n:=testing.AllocsPerRun(50,func(){RootMeasure(&value);RootSave(&value,wire);RootLoad(&loaded,wire,&report)});n!=0 {t.Fatalf("nested scratch allocated %v",n)}
 }
}
`)
}

const structArrayWireTest = `package probe
import ("bytes"; "encoding/binary"; "hash/fnv"; "testing")

type wireOracle struct { ids []uint64; bodyLengths map[int]bool }
func (o *wireOracle) ref(name string) uint64 {
 h:=fnv.New64a();h.Write([]byte(name));id:=h.Sum64()
 for i,old:=range o.ids {if old==id{return uint64(i+1)}}
 o.ids=append(o.ids,id);return uint64(len(o.ids))
}
func (o *wireOracle) field(dst []byte,name string,kind byte) []byte {
 return append(binary.AppendUvarint(dst,o.ref(name)),kind)
}
func (o *wireOracle) flavor(dst []byte,v Flavor) []byte {
 if v==FlavorNone{return dst}
 dst=o.field(dst,"flavor",30)
 return binary.AppendUvarint(dst,o.ref([]string{"","Vanilla","Chocolate","Strawberry"}[int(v)]))
}
func (o *wireOracle) leaf(v *Leaf) []byte {
 b:=o.flavor(nil,v.Flavor)
 if v.Note!=7 {b=o.field(b,"note",8);b=binary.LittleEndian.AppendUint32(b,v.Note)}
 return append(b,0)
}
func (o *wireOracle) child(v *Child) []byte {
 b:=o.flavor(nil,v.Flavor)
 if v.PayloadLength>0 {
  b=o.field(b,"payload",14)
  payload:=binary.AppendUvarint([]byte{6},uint64(v.PayloadLength))
  payload=append(payload,v.Payload[:v.PayloadLength]...)
  b=binary.AppendUvarint(b,uint64(len(payload)));b=append(b,payload...)
 }
 mark:=len(o.ids);field:=o.field(nil,"nested",13);nested:=o.leaf(&v.Nested)
 if len(nested)>1 {b=append(b,field...);b=binary.AppendUvarint(b,uint64(len(nested)));b=append(b,nested...)} else {o.ids=o.ids[:mark]}
 b=append(b,0);o.bodyLengths[len(b)]=true;return b
}
func (o *wireOracle) array(dst []byte,name string,values []Child) []byte {
 dst=o.field(dst,name,14)
 body:=binary.AppendUvarint([]byte{13},uint64(len(values)))
 for i:=range values {child:=o.child(&values[i]);body=binary.AppendUvarint(body,uint64(len(child)));body=append(body,child...)}
 dst=binary.AppendUvarint(dst,uint64(len(body)));return append(dst,body...)
}
func (o *wireOracle) root(v *Root) []byte {
 o.ids=nil;b:=[]byte{1}
 if v.ChildrenCount>0 {b=o.array(b,"children",v.Children[:v.ChildrenCount])}
 b=o.array(b,"pair",v.Pair[:])
 if v.LargeCount>0 {b=o.array(b,"large",v.Large[:v.LargeCount])}
 if v.OptionalPresent {b=o.array(b,"optional",v.Optional[:v.OptionalCount])}
 b=append(b,0)
 for _,id:=range o.ids {b=binary.LittleEndian.AppendUint64(b,id)}
 return binary.LittleEndian.AppendUint64(b,uint64(len(o.ids)))
}
func fillChild(v *Child,seed,size int) {
 ChildReset(v)
 if seed%5==0 {return} // all-default nested element still rides in an array
 v.Flavor=Flavor(seed%4);v.PayloadLength=int32(size)
 for i:=0;i<size;i++ {v.Payload[i]=byte(i+seed)}
 if seed%3!=0 {v.Nested.Flavor=Flavor((seed+1)%4);v.Nested.Note=uint32(seed)}
}
func TestArrayWireAndReuse(t *testing.T) {
 oracle:=wireOracle{bodyLengths:map[int]bool{}}
 var value,loaded Root
 for variant:=0;variant<132;variant++ {
  RootReset(&value)
  counts:=[]int32{0,1,2,127,128};value.ChildrenCount=counts[variant%len(counts)]
  for i:=int32(0);i<value.ChildrenCount;i++ {fillChild(&value.Children[i],variant+int(i),variant%131)}
  // This child supplies every payload length with no other fields, crossing L=127/128.
  value.Pair[0].PayloadLength=int32(variant%131)
  for i:=int32(0);i<value.Pair[0].PayloadLength;i++ {value.Pair[0].Payload[i]=byte(i+1)}
  fillChild(&value.Pair[1],variant+1,(variant*7)%131)
  if variant%11==0 {value.LargeCount=129;for i:=range value.Large {fillChild(&value.Large[i],i+variant,variant%131)}}
  value.OptionalPresent=variant%2==0
  if value.OptionalPresent {value.OptionalCount=int32(variant%3);for i:=int32(0);i<value.OptionalCount;i++ {fillChild(&value.Optional[i],variant+int(i),variant%131)}}
  want:=oracle.root(&value);got:=make([]byte,len(want))
  if RootMeasure(&value)!=int64(len(want)) || RootSave(&value,got)!=int64(len(want)) || !bytes.Equal(got,want) {t.Fatalf("variant %d wire differs",variant)}
  var report TableReport
  if !RootLoad(&loaded,want,&report)||report!=(TableReport{})||loaded!=value {t.Fatalf("variant %d reused load differs: %+v",variant,report)}
  if RootSave(&loaded,got)!=int64(len(want))||!bytes.Equal(got,want) {t.Fatalf("variant %d re-save differs",variant)}
  for _,size:=range []int{0,1,len(want)/2,len(want)-1} {
   short:=bytes.Repeat([]byte{0xa5},len(want)+8)
   if RootSave(&value,short[:size])!=-1 {t.Fatalf("variant %d short buffer %d accepted",variant,size)}
   if !bytes.Equal(short[size:],bytes.Repeat([]byte{0xa5},len(short)-size)) {t.Fatal("wrote past destination")}
  }
 }
 if !oracle.bodyLengths[127]||!oracle.bodyLengths[128] {t.Fatal("did not exercise both body-length LEB widths")}
}
func TestArrayRefusalsAndAllocations(t *testing.T) {
 var value,loaded Root;RootReset(&value)
 value.ChildrenCount=128;value.LargeCount=129;value.OptionalPresent=true;value.OptionalCount=2
 for i:=range value.Children {fillChild(&value.Children[i],i+1,130)}
 for i:=range value.Large {fillChild(&value.Large[i],i+1,130)}
 wire:=make([]byte,RootMeasure(&value));var report TableReport
 if n:=testing.AllocsPerRun(50,func(){report=TableReport{};if RootMeasure(&value)!=int64(len(wire))||RootSave(&value,wire)!=int64(len(wire))||!RootLoad(&loaded,wire,&report)||report!=(TableReport{}) {panic("round trip failed")}});n!=0 {t.Fatalf("round trip allocated %v",n)}
 for _,bad:=range []int32{-1,129} {old:=value.ChildrenCount;value.ChildrenCount=bad;if RootMeasure(&value)!=-1||RootSave(&value,wire)!=-1{t.Fatal("invalid cached count accepted")};value.ChildrenCount=old}
 for _,bad:=range []int32{-1,130} {old:=value.LargeCount;value.LargeCount=bad;if RootMeasure(&value)!=-1||RootSave(&value,wire)!=-1{t.Fatal("invalid fallback count accepted")};value.LargeCount=old}
 value.Children[0].Flavor=Flavor(99)
 if RootMeasure(&value)!=-1||RootSave(&value,wire)!=-1 {t.Fatal("invalid enum accepted")}
 value.Children[0].Flavor=FlavorVanilla;value.Children[0].PayloadLength=131
 if RootMeasure(&value)!=-1||RootSave(&value,wire)!=-1 {t.Fatal("invalid payload length accepted")}
}
`
