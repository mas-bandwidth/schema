package gotable

// The file loader holds validated wire indices in table pointer slots until
// this check succeeds. Kahn's algorithm starts at the root and at every record
// an unknown slot names, and visits every placed table node the descent
// reaches, and every stored slot, including false guards. A directory stays 16
// bytes: Offset temporarily carries a type ordinal above bit 47; TypeID
// carries a 32-bit in-degree and stack link. Two bits above the offset carry
// the marks the decode walk sets: bit 63 on a named record an unknown slot
// reaches, bit 62 on a blob record a slot has already named. No allocation,
// reverse lookup, graph recursion, or writer-side map sorting is needed.
//
// The same in-memory walk carries the graph conditions the read owes
// (docs/SPEC-TABLES.md §4, §6.5): a record no pointer reaches, a record a cycle
// holds back and a blob record are damage, and one blob record named from two
// slots is blob_shared. It reads the decoded graph, not the wire, so the load's
// one byte walk is the body read; a slot the reader cannot name marks its
// target as it is skipped.
const tableCycleSource = `
const tableNodeWireNamed uint64 = 1 << 63
const tableNodeBlobSeen uint64 = 1 << 62
const tableNodeSeed uint64 = 1 << 63

func tableCycleCheck(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo)bool{
 return tableCycleCheckReason(base,root,a,dir,nodeType)==TableRefuseOk
}

func tableCycleCheckReason(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo)TableRefuseReason{
 const mask=uint64(1)<<48-1
 if uint64(len(dir))>math.MaxUint32{return TableRefuseWireDamaged}
 // a blob record no pointer names is unreachable; a named one clears its mark
 for i:=range dir {
  e:=&dir[i];if e.Offset==^uint64(0){continue}
  if e.TypeID==tableBytesTypeID||e.TypeID==tableStringTypeID {
   if e.Offset&tableNodeBlobSeen==0{return TableRefuseWireDamaged}
   e.Offset&^=tableNodeBlobSeen
  }
 }
 vertices:=0
 for i:=range dir {
  e:=&dir[i];if e.Offset==^uint64(0){continue};t:=nodeType(e.TypeID);if i==0{t=root};if t==nil{continue}
  off:=e.Offset&mask;seed:=e.Offset&tableNodeWireNamed!=0
  if off>mask||t.CycleIndex==0||t.CycleIndex>=65535{return TableRefuseWireDamaged}
  e.Offset=off|t.CycleIndex<<48;e.TypeID=0;if seed{e.TypeID=tableNodeSeed};vertices++
 }
 each:=func(i int,visit func(*TableRef,*TableFieldInfo)bool)bool{
  e:=&dir[i];ordinal:=e.Offset>>48;if ordinal==0||ordinal==65535{return true}
  t:=root.CycleType(ordinal);if t==nil{return false}
  var p unsafe.Pointer;if a!=nil{p=a.At(int64(e.Offset&mask))}else{p=unsafe.Add(base,e.Offset&mask)}
  return tableCycleEdges(p,t,visit)
 }
 count:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};index:=uint64(*slot);if index>uint64(len(dir)){return false};e:=&dir[index-1]
  if e.Offset>>48==0||e.Offset>>48==65535||uint32(e.TypeID)==math.MaxUint32{return false};e.TypeID++;return true
 }
 for i:=range dir{if !each(i,count){return TableRefuseWireDamaged}}
 // Kahn from the root and from every record an unknown slot named: anything
 // left back is unreachable or held by a cycle, and both are damage
 head:=uint32(0)
 push:=func(i int){dir[i].TypeID=uint64(head)<<32;head=uint32(i+1)}
 push(0)
 for i:=range dir {
  if i==0||dir[i].Offset==^uint64(0)||dir[i].TypeID&tableNodeSeed==0{continue}
  dir[i].TypeID&^=tableNodeSeed
  if dir[i].TypeID==0{push(i)}
 }
 release:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};i:=int(*slot-1);if uint32(dir[i].TypeID)==0{return false}
  dir[i].TypeID--;if uint32(dir[i].TypeID)==0{push(i)};return true
 }
 visited:=0
 for head!=0{i:=int(head-1);head=uint32(dir[i].TypeID>>32);visited++;if !each(i,release){return TableRefuseWireDamaged}}
 if visited!=vertices{return TableRefuseWireDamaged}
 resolve:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};off:=dir[int(*slot-1)].Offset&mask
  if a!=nil{*slot=int64(off)}else{*slot=int64(uintptr(unsafe.Add(base,off))-uintptr(unsafe.Pointer(slot)))}
  return true
 }
 for i:=range dir{if !each(i,resolve){return TableRefuseWireDamaged}}
 for i:=range dir{e:=&dir[i];ordinal:=e.Offset>>48;if ordinal!=0&&ordinal!=65535{e.TypeID=root.CycleType(ordinal).ID;e.Offset&=mask}}
 return TableRefuseOk
}
`
