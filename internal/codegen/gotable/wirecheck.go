package gotable

// tableWireCheckSource is the read's wire-alone half (docs/SPEC-TABLES.md §4,
// §6.5, "TWO KINDS OF CONDITION"): every condition decidable from the wire
// alone, applied to every byte of the input before any body is decoded, in
// fields and records this reader declares and in those it does not. The
// generated body reads then hold the input to the declaration. Together they
// are the one read, and a refusal by either is the whole load's
// (tla/ReadOrder.tla, OneVerdict; tla/Framing.tla).
//
// The walk is one function of the bytes, the id table and the node table. A
// body is walked twice: its headers first, for the repeated-id check against
// a stamp per id-table entry, then each payload. The pointer graph is every
// kind 17 payload in the input, and it is checked without recursion over the
// graph: a first pass counts each record's in-degree, a record no pointer
// names is unreachable and a blob record named twice is shared, and Kahn's
// algorithm from the root then visits every record unless a cycle holds one
// back. The per-record state is two words in the caller's scratch: the
// in-degree with the visit stack's link, and the record's offset in the node
// table.
const tableWireCheckSource = `
// tableBinaryMaxDepth is the binary nesting bound (docs/SPEC-TABLES.md §4,
// binary_max_depth): a root body or a node record is level 0, each table,
// array or keyed body one level deeper, and level 129 refuses.
const tableBinaryMaxDepth = 128

type tableWireCheck struct {
 depth int32
 ids int
 nodes uint64
 records TableReader
 graph []uint64
 release bool
 head uint64
 gen uint32
 reason TableRefuseReason
 stamp [4097]uint32
}

func (c *tableWireCheck) fail(reason TableRefuseReason) bool { if c.reason==TableRefuseOk { c.reason=reason }; return false }

// failed is the walk's refusal, wire_damaged where no rule named one.
func (c *tableWireCheck) failed() TableRefuseReason { if c.reason==TableRefuseOk { return TableRefuseWireDamaged }; return c.reason }

// edge is one kind 17 payload: a node index, 0 for null, 1 for the root and
// 2 on for the node table's records in order (docs/SPEC-TABLES.md §3.1). Every
// pointer lies inside the root's descent, so one naming the root is a cycle.
func (c *tableWireCheck) edge(index uint64) bool {
 if index==0 { return true }
 if index==1 || index>c.nodes+1 { return c.fail(TableRefuseWireDamaged) }
 if c.graph==nil { return true }
 at:=&c.graph[2*(index-1)]
 if !c.release { if uint32(*at)==^uint32(0) { return c.fail(TableRefuseWireDamaged) }; *at++; return true }
 if uint32(*at)==0 { return c.fail(TableRefuseWireDamaged) }
 *at--
 if uint32(*at)==0 { *at|=c.head<<32; c.head=index+1 }
 return true
}

// The walk takes each reader BY VALUE and answers the offset it reached: a
// reader passed by pointer down a recursion this deep is moved to the heap by
// the compiler, and the read allocates nothing of its own.

// body walks one table body from r.Offset to its terminator: the reserved ids
// only where they are the body's own transport, and no id twice. It answers
// the offset after the terminator.
func (c *tableWireCheck) body(r TableReader, root bool) (int64, bool) {
 c.gen++
 gen:=c.gen
 probe:=r
 for {
  ref,ok:=probe.Leb()
  if !ok { return 0,c.fail(TableRefuseWireDamaged) }
  if ref==0 { break }
  if ref>uint64(c.ids) || !probe.Has(1) { return 0,c.fail(TableRefuseWireDamaged) }
  if c.stamp[ref]==gen { return 0,c.fail(TableRefuseWireDamaged) }
  c.stamp[ref]=gen
  if !probe.Skip(probe.Get8()) { return 0,c.fail(TableRefuseWireDamaged) }
 }
 for {
  ref,_:=r.Leb()
  if ref==0 { return r.Offset,true }
  id,_:=r.Resolve(ref)
  kind:=r.Get8()
  if id==0xfffffffffffffffe || id==0xfffffffffffffffd { return 0,c.fail(TableRefuseWireDamaged) }
  if id==0xffffffffffffffff {
   if !root || kind!=12 { return 0,c.fail(TableRefuseWireDamaged) }
   r.Body()
   continue
  }
  at,ok:=c.value(r,kind)
  if !ok { return 0,false }
  r.Offset=at
 }
}

// value walks one payload of a kind from r.Offset — a field's, or an
// element's, an element being encoded as a field's payload of its kind — and
// answers the offset after it.
func (c *tableWireCheck) value(r TableReader, kind uint8) (int64, bool) {
 switch kind {
 case 1:
  if !r.Has(1) || r.Get8()>1 { return 0,c.fail(TableRefuseWireDamaged) }
 case 10:
  if !r.Has(4) { return 0,c.fail(TableRefuseWireDamaged) }
  if r.Get32()&0x7f800000==0x7f800000 { return 0,c.fail(TableRefuseNonFiniteFloat) }
 case 11:
  if !r.Has(8) { return 0,c.fail(TableRefuseWireDamaged) }
  if r.Get64()&0x7ff0000000000000==0x7ff0000000000000 { return 0,c.fail(TableRefuseNonFiniteFloat) }
 case 17:
  index,ok:=r.Leb()
  if !ok || !c.edge(index) { return 0,c.fail(TableRefuseWireDamaged) }
 case 30:
  ref,ok:=r.Leb()
  if !ok || ref>uint64(c.ids) { return 0,c.fail(TableRefuseWireDamaged) }
 case 15:
  return c.union(r)
 case 12, 13, 14, 16, 31, 32, 33:
  sub,ok:=r.Body()
  if !ok || !c.framed(sub,kind) { return 0,c.fail(TableRefuseWireDamaged) }
 default:
  n:=tableKindBytes(kind)
  if n==0 || !r.Has(n) { return 0,c.fail(TableRefuseWireDamaged) }
  r.Offset+=n
 }
 return r.Offset,true
}

// framed walks the content of an L-framed payload of a kind — a field's
// L-framed kinds, an arm's and a keyed slot's element — which fills its L.
func (c *tableWireCheck) framed(sub TableReader, kind uint8) bool {
 var at int64
 var ok bool
 switch kind {
 case 12:
  return tableUTF8Valid(sub.Buffer) || c.fail(TableRefuseWireDamaged)
 case 33:
  return tableWireUtf16Valid(sub.Buffer) || c.fail(TableRefuseWireDamaged)
 case 31:
  return true
 case 32:
  return len(sub.Buffer)==0 || c.fail(TableRefuseWireDamaged)
 case 13, 14, 16:
  if c.depth>=tableBinaryMaxDepth { return c.fail(TableRefuseWireDamaged) }
  c.depth++
  if kind==13 { at,ok=c.body(sub,false) } else { at,ok=c.array(sub,kind==16) }
  c.depth--
 case 15:
  at,ok=c.union(sub)
 case 17, 30:
  at,ok=c.value(sub,kind)
 default:
  if n:=tableKindBytes(kind); n==0 || int64(len(sub.Buffer))!=n { return c.fail(TableRefuseWireDamaged) }
  at,ok=c.value(sub,kind)
 }
 if !ok { return false }
 return at==int64(len(sub.Buffer)) || c.fail(TableRefuseWireDamaged)
}

// array walks an array or keyed body: its header inside its own L, then count
// elements of its element kind. A keyed body's element is a slot: a nonzero
// key reference, no key twice, and an element whose L frames its kind's
// payload exactly, as an arm's does.
func (c *tableWireCheck) array(sub TableReader, keyed bool) (int64, bool) {
 if len(sub.Buffer)<2 { return 0,c.fail(TableRefuseWireDamaged) }
 kind:=sub.Get8()
 count,ok:=sub.Leb()
 if !ok { return 0,c.fail(TableRefuseWireDamaged) }
 // a count past the int32 storage cap refuses by its own name (§2.9), and one
 // the body cannot cover (an element is a byte at least) is damage
 if count>math.MaxInt32 { return 0,c.fail(TableRefuseCountOverExtentCap) }
 if count>uint64(len(sub.Buffer))-uint64(sub.Offset) { return 0,c.fail(TableRefuseWireDamaged) }
 if !keyed {
  for range count {
   at,ok:=c.value(sub,kind)
   if !ok { return 0,false }
   sub.Offset=at
  }
  return sub.Offset,true
 }
 c.gen++
 gen:=c.gen
 probe:=sub
 for range count {
  key,ok:=probe.Leb()
  if !ok || key==0 || key>uint64(c.ids) { return 0,c.fail(TableRefuseWireDamaged) }
  if c.stamp[key]==gen { return 0,c.fail(TableRefuseWireDamaged) }
  c.stamp[key]=gen
  if _,ok:=probe.Body(); !ok { return 0,c.fail(TableRefuseWireDamaged) }
 }
 for range count {
  sub.Leb()
  elem,_:=sub.Body()
  if !c.framed(elem,kind) { return 0,false }
 }
 return sub.Offset,true
}

// union walks one union payload: the arm reference, and where it is not 0 the
// arm's kind and its L-framed payload.
func (c *tableWireCheck) union(r TableReader) (int64, bool) {
 ref,ok:=r.Leb()
 if !ok || ref>uint64(c.ids) { return 0,c.fail(TableRefuseWireDamaged) }
 if ref==0 { return r.Offset,true }
 if !r.Has(1) { return 0,c.fail(TableRefuseWireDamaged) }
 kind:=r.Get8()
 arm,ok:=r.Body()
 if !ok { return 0,c.fail(TableRefuseWireDamaged) }
 // a union is a level when its arm is structural: a table, an array, a
 // keyed or map body, or another union; a scalar arm adds none
 structural:=kind==13||kind==14||kind==15||kind==16
 if structural { if c.depth>=tableBinaryMaxDepth { return 0,c.fail(TableRefuseWireDamaged) }; c.depth++ }
 good:=c.framed(arm,kind)
 if structural { c.depth-- }
 if !good { return 0,c.fail(TableRefuseWireDamaged) }
 return r.Offset,true
}

// record walks the node record at offset in the node table: a blob's bytes, or
// a table body of any type, named or not.
func (c *tableWireCheck) record(at int64) bool {
 r:=c.records
 r.Offset=at
 ref,_:=r.Leb()
 id,_:=r.Resolve(ref)
 body,_:=r.Body()
 switch id {
 case tableWireBytesID:
  return true
 case tableWireStringID:
  return tableUTF8Valid(body.Buffer) || c.fail(TableRefuseWireDamaged)
 }
 return c.framed(body,13)
}

func tableWireUtf16Valid(data []byte) bool {
 if len(data)&1!=0 { return false }
 for i:=0;i<len(data);i+=2 {
  u:=binary.LittleEndian.Uint16(data[i:])
  if u==0 || u>=0xdc00 && u<=0xdfff { return false }
  if u>=0xd800 && u<=0xdbff {
   i+=2
   if i>=len(data) { return false }
   if v:=binary.LittleEndian.Uint16(data[i:]); v<0xdc00 || v>0xdfff { return false }
  }
 }
 return true
}

// tableWireVerify holds every wire-alone condition over an opened file
// (docs/SPEC-TABLES.md §4, §6.5): the root body, the node table and every
// record in it, and the pointer graph. scratch holds two words per node,
// at the node index less one (record i of the table is node i+1); a nil or
// short scratch is allocated here, which only a file with a node table needs.
func tableWireVerify(r TableReader, scratch []uint64) TableRefuseReason {
 var c tableWireCheck
 c.ids=len(r.Ids)/8
 // the node table, found by the root's headers: a declared count, then that
 // many records, each a type reference and an L-framed body, filling the field
 probe:=r
 probe.Offset=0
 for {
  ref,ok:=probe.Leb()
  if !ok || ref==0 { break }
  id,ok:=probe.Resolve(ref)
  if !ok || !probe.Has(1) { break }
  kind:=probe.Get8()
  if id==0xffffffffffffffff && kind==12 {
   payload,ok:=probe.Body()
   if !ok { break }
   count,ok:=payload.Leb()
   if !ok || count>uint64(len(payload.Buffer)) { return TableRefuseWireDamaged }
   c.records=payload
   c.nodes=count
   break
  }
  if !probe.Skip(kind) { break }
 }
 if c.nodes>0 {
  if uint64(len(scratch))<2*(c.nodes+1) { scratch=make([]uint64,2*(c.nodes+1)) }
  c.graph=scratch[:2*(c.nodes+1)]
  clear(c.graph)
  rec:=c.records
  for i:=uint64(2); i<=c.nodes+1; i++ {
   c.graph[2*(i-1)+1]=uint64(rec.Offset)
   ref,ok:=rec.Leb()
   if !ok { return TableRefuseWireDamaged }
   if _,ok:=rec.Resolve(ref); !ok { return TableRefuseWireDamaged }
   if _,ok:=rec.Body(); !ok { return TableRefuseWireDamaged }
  }
  if rec.Offset!=int64(len(rec.Buffer)) { return TableRefuseWireDamaged }
 }
 // the first pass: every check, and each record's in-degree
 root:=r
 root.Offset=0
 if end,ok:=c.body(root,true); !ok || end!=int64(len(root.Buffer)) { return c.failed() }
 for i:=uint64(2); i<=c.nodes+1; i++ {
  if !c.record(int64(c.graph[2*(i-1)+1])) { return c.failed() }
 }
 if c.nodes==0 { return TableRefuseOk }
 // a blob named twice is shared, and a record no pointer names is unreachable
 for i:=uint64(2); i<=c.nodes+1; i++ {
  if uint32(c.graph[2*(i-1)])>1 {
   rec:=c.records
   rec.Offset=int64(c.graph[2*(i-1)+1])
   ref,_:=rec.Leb()
   if id,_:=rec.Resolve(ref); id==tableWireBytesID || id==tableWireStringID { return TableRefuseBlobShared }
  }
 }
 for i:=uint64(2); i<=c.nodes+1; i++ {
  if uint32(c.graph[2*(i-1)])==0 { return TableRefuseWireDamaged }
 }
 // Kahn's algorithm from the root: a record a cycle holds is never released
 c.release=true
 if _,ok:=c.body(root,true); !ok { return c.failed() }
 visited:=uint64(0)
 for c.head!=0 {
  i:=c.head-1
  c.head=c.graph[2*(i-1)]>>32
  c.graph[2*(i-1)]&=0xffffffff
  visited++
  if !c.record(int64(c.graph[2*(i-1)+1])) { return c.failed() }
 }
 if visited!=c.nodes { return TableRefuseWireDamaged }
 return TableRefuseOk
}

// tableWireHasNodes answers whether the root's headers carry the node table.
// Only then does the file have records or a pointer graph whose reachability,
// sharing and cycles need a whole-file view; a file without one is checked by
// the body read itself, as it decodes each field (docs/SPEC-TABLES.md §4, §6.5).
func tableWireHasNodes(r TableReader) bool {
 probe:=r
 probe.Offset=0
 for {
  ref,ok:=probe.Leb()
  if !ok || ref==0 { return false }
  id,ok:=probe.Resolve(ref)
  if !ok || !probe.Has(1) { return false }
  kind:=probe.Get8()
  if id==0xffffffffffffffff && kind==12 { return true }
  if !probe.Skip(kind) { return false }
 }
}

// tableWireSkipValue walks one unknown payload with the wire-alone rules while
// the body read skips it, so a field this reader cannot name is still held to
// every condition the wire alone decides (docs/SPEC-TABLES.md §4, §6.5).
func tableWireSkipValue(r *TableReader, kind uint8) bool {
 var c tableWireCheck
 c.ids=len(r.Ids)/8
 at,ok:=c.value(*r,kind)
 if !ok { r.Report.Refuse(c.failed(),""); return false }
 r.Offset=at
 return true
}

// tableOpenChecked opens a form-1 file and holds every wire-alone condition
// before the body read starts (docs/SPEC-TABLES.md §3, §4, §6.5). It answers
// the report as a refusal for every verdict but TableOpenOk. A file without a
// node table is checked in the body read's one walk; only the whole-file graph
// conditions still need this prepass.
func tableOpenChecked(data []byte, report *TableReport) (TableReader, TableOpenVerdict) {
 r,verdict:=tableOpen(data,report)
 if verdict==TableOpenOk && tableWireHasNodes(r) {
  if reason:=tableWireVerify(r,nil); reason!=TableRefuseOk {
   verdict=TableOpenDamaged
   if report!=nil { report.Refuse(reason,"") }
  }
 }
 tableOpenAnswer(report,verdict,data)
 return r,verdict
}

// tableOpenAnswer writes an open's verdict into the report: a form this
// reader does not carry is refused by name, and damage is wire_damaged.
func tableOpenAnswer(report *TableReport, verdict TableOpenVerdict, data []byte) bool {
 if report==nil { return verdict==TableOpenOk }
 report.Verdict=verdict
 switch verdict {
 case TableOpenOk:
  return true
 case TableOpenRefused:
  report.Reason="newer_form"
  if len(data)>0 && data[0]==2 { report.Reason="message_form_as_file" }
  report.Refuse(TableRefuseUnknownForm,"")
 default:
  report.Refuse(TableRefuseWireDamaged,"")
 }
 return false
}

// tableRefused makes a refusing read's report what docs/SPEC-TABLES.md §4 says
// it is: the refusal and no counter, because nothing it decoded is defined.
func tableRefused(r *TableReport) {
 if r==nil { return }
 if r.Err()==nil { r.Refuse(TableRefuseWireDamaged,"") }
 r.Unknown,r.Widened,r.Retained,r.RetainLost=0,0,0,0
}
`
