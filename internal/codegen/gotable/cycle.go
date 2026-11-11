package gotable

// The file loader holds validated wire indices in table pointer slots until
// this check succeeds. Kahn's algorithm starts at the root and visits every
// node record the descent reaches, placed or not, and every stored slot,
// including false guards. A directory stays 16 bytes. A record whose edges
// all sit in its decoded slots is walked in memory: Offset carries its type
// ordinal above bit 47, and TypeID its 32-bit in-degree, then its stack link.
// A record of a type this reader cannot name, and one whose body carries a
// pointer the reader cannot name (bit 63, set by the decode walk), is walked
// from the wire: bit 63 of Offset marks it, bits 48 to 62 and the high half of
// TypeID carry its offset in the node table, the low 48 bits keep its offset
// in memory (all ones when it has none), and the low half of TypeID carries
// the count and the link. Bit 62 marks a blob record a slot has already named.
// No allocation, reverse lookup, graph recursion, or writer-side map sorting
// is needed.
//
// The same walk carries the graph conditions the read owes (docs/SPEC-TABLES.md
// §4, §6.5): a record no pointer reaches, a record a cycle holds back and a
// blob record are damage, and one blob record named from two slots is
// blob_shared. Every pointer is an edge, a slot this reader cannot name
// included, so the load's one byte walk is the body read and a record of a
// type it cannot name is a vertex like any other. A declared slot naming one
// holds the index until the walk is over and then reads null.
const tableCycleSource = `
const tableNodeWireNamed uint64 = 1 << 63
const tableNodeBlobSeen uint64 = 1 << 62

func tableCycleCheck(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo,r TableReader)bool{
 return tableCycleCheckReason(base,root,a,dir,nodeType,r)==TableRefuseOk
}

func tableCycleCheckReason(base unsafe.Pointer,root *TableTypeInfo,a *TableArena,dir []TableNodeDirEntry,nodeType func(uint64)*TableTypeInfo,r TableReader)TableRefuseReason{
 const mask=uint64(1)<<48-1
 const wire=uint64(1)<<63
 if uint64(len(dir))>math.MaxUint32{return TableRefuseWireDamaged}
 // a blob record no pointer names is unreachable; a named one clears its mark
 walked:=false
 for i:=range dir {
  e:=&dir[i];walked=walked||e.Offset&wire!=0;if e.Offset==^uint64(0){continue}
  if e.TypeID==tableBytesTypeID||e.TypeID==tableStringTypeID {
   if e.Offset&tableNodeWireNamed!=0{return TableRefuseBlobShared};if e.Offset&tableNodeBlobSeen==0{return TableRefuseWireDamaged}
   e.Offset&^=tableNodeBlobSeen
  }
 }
 records:=tableNodeScanBegin(r).payload;rec:=records;vertices:=0
 for i:=range dir {
  e:=&dir[i];at:=uint64(0);off:=mask
  if walked&&i>0 {at=uint64(rec.Offset);ref,ok:=rec.Leb();if !ok{return TableRefuseWireDamaged};if _,ok:=rec.Resolve(ref);!ok{return TableRefuseWireDamaged};if _,ok:=rec.Body();!ok{return TableRefuseWireDamaged}}
  if e.Offset!=^uint64(0) {
   t:=nodeType(e.TypeID);if i==0{t=root};if t==nil{continue}
   off=e.Offset&mask
   if e.Offset&tableNodeWireNamed==0 {
    if t.CycleIndex==0||t.CycleIndex>=1<<15{return TableRefuseWireDamaged}
    e.Offset=off|t.CycleIndex<<48;e.TypeID=0;vertices++;continue
   }
  }
  if at>=1<<47{return TableRefuseWireDamaged}
  e.Offset=wire|at>>32<<48|off;e.TypeID=at<<32;vertices++
 }
 // a record walked from the wire: its body, and its type id
 body:=func(i int)(uint64,TableReader){
  e:=&dir[i];rec:=records;rec.Offset=int64(e.Offset>>48&0x7fff<<32|e.TypeID>>32);ref,_:=rec.Leb();id,_:=rec.Resolve(ref);b,_:=rec.Body();return id,b
 }
 typ:=func(i int)*TableTypeInfo{
  e:=&dir[i];ordinal:=e.Offset>>48;if ordinal==0||e.Offset&mask==mask{return nil};if i==0{return root};if e.Offset&wire==0{return root.CycleType(ordinal)}
  id,_:=body(i);return nodeType(id)
 }
 each:=func(i int,visit func(*TableRef,*TableFieldInfo)bool)bool{
  t:=typ(i);if t==nil{return true}
  var p unsafe.Pointer;if a!=nil{p=a.At(int64(dir[i].Offset&mask))}else{p=unsafe.Add(base,dir[i].Offset&mask)}
  return tableCycleEdges(p,t,visit)
 }
 // the edges of a record walked from the wire: each pointer its body
 // carries, a blob's excepted (its mark is the blob's count)
 bad:=false
 step:=func(i int,slot func(*TableRef,*TableFieldInfo)bool,mark func(uint64))bool{
  if dir[i].Offset&wire==0{return each(i,slot)}
  if i==0{var c tableWireCheck;c.ids=len(r.Ids)/8;c.nodes=uint64(len(dir)-1);c.mark=mark;r.Offset=0;_,ok:=c.body(r,true);return ok&&!bad}
  _,b:=body(i);return tableWireRecordCheck(b,len(r.Ids)/8,uint64(len(dir)-1),mark)==TableRefuseOk&&!bad
 }
 in:=func(index uint64)bool{
  e:=&dir[index-1];if e.Offset>>48==0||uint32(e.TypeID)==math.MaxUint32{return false};e.TypeID++;return true
 }
 count:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};index:=uint64(*slot);if index>uint64(len(dir)){return false};return in(index)
 }
 countWire:=func(index uint64){if index!=0&&!bad&&dir[index-1].Offset>>48!=0&&!in(index){bad=true}}
 for i:=range dir{if !step(i,count,countWire){return TableRefuseWireDamaged}}
 // Kahn from the root: anything left back is unreachable or held by a cycle,
 // and both are damage
 head:=uint32(0)
 push:=func(i int){dir[i].TypeID=dir[i].TypeID&^math.MaxUint32|uint64(head);head=uint32(i+1)}
 out:=func(index uint64)bool{
  i:=int(index-1);if uint32(dir[i].TypeID)==0{return false}
  dir[i].TypeID--;if uint32(dir[i].TypeID)==0{push(i)};return true
 }
 release:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};return out(uint64(*slot))
 }
 releaseWire:=func(index uint64){if index!=0&&!bad&&dir[index-1].Offset>>48!=0&&!out(index){bad=true}}
 push(0);visited:=0
 for head!=0{i:=int(head-1);head=uint32(dir[i].TypeID);dir[i].TypeID&^=math.MaxUint32;visited++;if !step(i,release,releaseWire){return TableRefuseWireDamaged}}
 if visited!=vertices{return TableRefuseWireDamaged}
 // a slot naming a record of a type this reader cannot name reads null
 resolve:=func(slot *TableRef,f *TableFieldInfo)bool{
  if *slot==0||f.Table==nil{return true};off:=dir[int(*slot-1)].Offset&mask
  if off==mask{*slot=0;return true}
  if a!=nil{*slot=int64(off)}else{*slot=int64(uintptr(unsafe.Add(base,off))-uintptr(unsafe.Pointer(slot)))}
  return true
 }
 for i:=range dir{if !each(i,resolve){return TableRefuseWireDamaged}}
 for i:=range dir{
  e:=&dir[i];if e.Offset>>48==0{continue}
  off:=e.Offset&mask;if off==mask{e.TypeID,_=body(i);e.Offset=^uint64(0);continue}
  e.TypeID=typ(i).ID;e.Offset=off
 }
 return TableRefuseOk
}
`
