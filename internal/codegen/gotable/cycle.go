package gotable

// The file loader holds validated wire indices in table pointer slots until
// this check succeeds. Kahn's algorithm starts at the root and at every record
// an unknown slot names, and visits every node record the descent reaches,
// placed or not, and every stored slot, including false guards. A directory
// stays 16 bytes: Offset temporarily carries a type ordinal above bit 47;
// TypeID carries a 32-bit in-degree and stack link. Two bits above the offset
// carry the marks the decode walk sets: bit 63 on a named record an unknown
// slot reaches, bit 62 on a blob record a slot has already named. No
// allocation, reverse lookup, graph recursion, or writer-side map sorting is
// needed.
//
// The same in-memory walk carries the graph conditions the read owes
// (docs/SPEC-TABLES.md §4, §6.5): a record no pointer reaches, a record a cycle
// holds back and a blob record are damage, and one blob record named from two
// slots is blob_shared. It reads the decoded graph, so the load's one byte
// walk is the body read; a slot the reader cannot name marks its target as it
// is skipped. A record of a type this reader cannot name is a vertex too: it
// enters the walk at directory offset ^0 with its seed mark in TypeID, takes
// ordinal 65535 over its offset in the node table, and its own pointers, which
// live on the wire only, are walked from its record when it is counted and
// when it is released. A declared slot naming it holds the index until the
// walk is over and then reads null.
const tableCycleSource = `
const tableNodeWireNamed uint64 = 1 << 63
const tableNodeBlobSeen uint64 = 1 << 62
const tableNodeSeed uint64 = 1 << 63

func tableCycleCheck(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo,records TableReader)bool{
 return tableCycleCheckReason(base,root,a,dir,nodeType,records)==TableRefuseOk
}

func tableCycleCheckReason(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo,records TableReader)TableRefuseReason{
 const mask=uint64(1)<<48-1
 const unnamed=uint64(65535)
 if uint64(len(dir))>math.MaxUint32{return TableRefuseWireDamaged}
 // a blob record no pointer names is unreachable; a named one clears its mark
 for i:=range dir {
  e:=&dir[i];if e.Offset==^uint64(0){continue}
  if e.TypeID==tableBytesTypeID||e.TypeID==tableStringTypeID {
   if e.Offset&tableNodeWireNamed!=0{return TableRefuseBlobShared};if e.Offset&tableNodeBlobSeen==0{return TableRefuseWireDamaged}
   e.Offset&^=tableNodeBlobSeen
  }
 }
 vertices:=0;strangers:=0
 for i:=range dir {
  e:=&dir[i];if e.Offset==^uint64(0){e.TypeID&=tableNodeSeed;strangers++;continue};t:=nodeType(e.TypeID);if i==0{t=root};if t==nil{continue}
  off:=e.Offset&mask;seed:=e.Offset&tableNodeWireNamed!=0
  if off>mask||t.CycleIndex==0||t.CycleIndex>=65535{return TableRefuseWireDamaged}
  e.Offset=off|t.CycleIndex<<48;e.TypeID=0;if seed{e.TypeID=tableNodeSeed};vertices++
 }
 // a record of a type this reader cannot name keeps its place in the node
 // table, so its own pointers can be walked from the wire
 if strangers>0 {
  rec:=records
  for i:=1;i<len(dir);i++ {
   at:=uint64(rec.Offset);ref,ok:=rec.Leb();if !ok{return TableRefuseWireDamaged};if _,ok:=rec.Resolve(ref);!ok{return TableRefuseWireDamaged};if _,ok:=rec.Body();!ok{return TableRefuseWireDamaged}
   if e:=&dir[i];e.Offset==^uint64(0){if at>=mask{return TableRefuseWireDamaged};e.Offset=at|unnamed<<48;vertices++}
  }
 }
 each:=func(i int,visit func(*TableRef,*TableFieldInfo)bool)bool{
  e:=&dir[i];ordinal:=e.Offset>>48;if ordinal==0||ordinal==unnamed{return true}
  t:=root.CycleType(ordinal);if t==nil{return false}
  var p unsafe.Pointer;if a!=nil{p=a.At(int64(e.Offset&mask))}else{p=unsafe.Add(base,e.Offset&mask)}
  return tableCycleEdges(p,t,visit)
 }
 // the edges of a record this reader cannot name: each pointer its body
 // carries, a blob's excepted (its mark is the blob's count)
 bad:=false
 stranger:=func(i int,mark func(uint64))bool{
  e:=&dir[i];if e.Offset>>48!=unnamed{return true}
  rec:=records;rec.Offset=int64(e.Offset&mask);ref,_:=rec.Leb();rec.Resolve(ref);body,_:=rec.Body()
  return tableWireRecordCheck(body,len(records.Ids)/8,uint64(len(dir)-1),mark)==TableRefuseOk&&!bad
 }
 in:=func(index uint64)bool{
  e:=&dir[index-1];if e.Offset>>48==0||uint32(e.TypeID)==math.MaxUint32{return false};e.TypeID++;return true
 }
 count:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};index:=uint64(*slot);if index>uint64(len(dir)){return false};return in(index)
 }
 countWire:=func(index uint64){if index!=0&&!bad&&dir[index-1].Offset>>48!=0&&!in(index){bad=true}}
 for i:=range dir{if !each(i,count)||!stranger(i,countWire){return TableRefuseWireDamaged}}
 // Kahn from the root and from every record an unknown slot named: anything
 // left back is unreachable or held by a cycle, and both are damage
 head:=uint32(0)
 push:=func(i int){dir[i].TypeID=uint64(head)<<32;head=uint32(i+1)}
 push(0)
 for i:=range dir {
  if i==0||dir[i].TypeID&tableNodeSeed==0{continue}
  dir[i].TypeID&^=tableNodeSeed
  if dir[i].TypeID==0{push(i)}
 }
 out:=func(index uint64)bool{
  i:=int(index-1);if uint32(dir[i].TypeID)==0{return false}
  dir[i].TypeID--;if uint32(dir[i].TypeID)==0{push(i)};return true
 }
 release:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};return out(uint64(*slot))
 }
 releaseWire:=func(index uint64){if index!=0&&!bad&&dir[index-1].Offset>>48!=0&&!out(index){bad=true}}
 visited:=0
 for head!=0{i:=int(head-1);head=uint32(dir[i].TypeID>>32);visited++;if !each(i,release)||!stranger(i,releaseWire){return TableRefuseWireDamaged}}
 if visited!=vertices{return TableRefuseWireDamaged}
 // a slot naming a record of a type this reader cannot name reads null
 resolve:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};e:=&dir[int(*slot-1)];off:=e.Offset&mask
  if e.Offset>>48==unnamed{*slot=0;return true}
  if a!=nil{*slot=int64(off)}else{*slot=int64(uintptr(unsafe.Add(base,off))-uintptr(unsafe.Pointer(slot)))}
  return true
 }
 for i:=range dir{if !each(i,resolve){return TableRefuseWireDamaged}}
 for i:=range dir{
  e:=&dir[i];ordinal:=e.Offset>>48
  if ordinal==unnamed{rec:=records;rec.Offset=int64(e.Offset&mask);ref,_:=rec.Leb();e.TypeID,_=rec.Resolve(ref);e.Offset=^uint64(0);continue}
  if ordinal!=0{e.TypeID=root.CycleType(ordinal).ID;e.Offset&=mask}
 }
 return TableRefuseOk
}
`
