package gotable

// The file loader holds validated wire indices in table pointer slots until
// this check succeeds. Kahn's algorithm visits every placed table node,
// including disconnected nodes, and every stored slot, including false guards.
// A directory stays 16 bytes: Offset temporarily carries a type ordinal above
// bit 47; TypeID carries a 32-bit in-degree and stack link. No allocation,
// reverse lookup, graph recursion, or writer-side map sorting is needed.
const tableCycleSource = `
func tableCycleCheck(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo)bool{
 const mask=uint64(1)<<48-1
 if uint64(len(dir))>math.MaxUint32{return false}
 vertices:=0
 for i:=range dir {
  e:=&dir[i];if e.Offset==^uint64(0){continue};t:=nodeType(e.TypeID);if i==0{t=root};if t==nil{continue}
  if e.Offset>mask||t.CycleIndex==0||t.CycleIndex>=65535{return false}
  e.Offset|=t.CycleIndex<<48;e.TypeID=0;vertices++
 }
 each:=func(i int,visit func(*TableRef,*TableFieldInfo)bool)bool{
  e:=&dir[i];ordinal:=e.Offset>>48;if ordinal==0||ordinal==65535{return true}
  t:=root.CycleType(ordinal);if t==nil{return false}
  var p unsafe.Pointer;if a!=nil{p=a.At(int64(e.Offset&mask))}else{p=unsafe.Add(base,e.Offset&mask)}
  return tableCycleEdges(p,t,visit)
 }
 count:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};index:=uint64(*slot);if index>uint64(len(dir)){return false};e:=&dir[index-1]
  if e.Offset>>48==0||e.Offset>>48==65535||e.TypeID==math.MaxUint32{return false};e.TypeID++;return true
 }
 for i:=range dir{if !each(i,count){return false}}
 head:=uint32(0)
 push:=func(i int){dir[i].TypeID=uint64(head)<<32;head=uint32(i+1)}
 for i:=range dir{ordinal:=dir[i].Offset>>48;if ordinal!=0&&ordinal!=65535&&dir[i].TypeID==0{push(i)}}
 release:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};i:=int(*slot-1);if uint32(dir[i].TypeID)==0{return false}
  dir[i].TypeID--;if uint32(dir[i].TypeID)==0{push(i)};return true
 }
 visited:=0
 for head!=0{i:=int(head-1);head=uint32(dir[i].TypeID>>32);visited++;if !each(i,release){return false}}
 if visited!=vertices{return false}
 resolve:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};off:=dir[int(*slot-1)].Offset&mask
  if a!=nil{*slot=int64(off)}else{*slot=int64(uintptr(unsafe.Add(base,off))-uintptr(unsafe.Pointer(slot)))}
  return true
 }
 for i:=range dir{if !each(i,resolve){return false}}
 for i:=range dir{e:=&dir[i];ordinal:=e.Offset>>48;if ordinal!=0&&ordinal!=65535{e.TypeID=root.CycleType(ordinal).ID;e.Offset&=mask}}
 return true
}
`
