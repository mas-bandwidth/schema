package gotable

// tableWireCheckSource is the read's wire-alone half (docs/SPEC-TABLES.md §4,
// §6.5, "TWO KINDS OF CONDITION"): every condition decidable from the wire
// alone, applied to every byte of the input before any body is decoded, in
// fields and records this reader declares and in those it does not. The
// generated body reads then hold the input to the declaration. Together they
// are the one read, and a refusal by either is the whole load's
// (tla/ReadOrder.tla, OneVerdict; tla/Framing.tla).
//
// The walk is one function of the bytes, the id table and the node count. A
// body is walked twice: its headers first, for the repeated-id check against
// a stamp per id-table entry, then each payload. A kind 17 payload is bounded
// by the node count here; the pointer graph itself is judged over the decoded
// nodes (cycle.go).
const tableWireCheckSource = `
// tableBinaryMaxDepth is the binary nesting bound (docs/SPEC-TABLES.md §4,
// binary_max_depth): a root body or a node record is level 0, each table,
// array or keyed body one level deeper, and level 129 refuses.
const tableBinaryMaxDepth = 128

type tableWireCheck struct {
 depth int32
 ids uint64
 nodes uint64
 gen uint32
 reason TableRefuseReason
 mark func(uint64)
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
  if ref>c.ids || !probe.Has(1) { return 0,c.fail(TableRefuseWireDamaged) }
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
  if c.mark!=nil { c.mark(index) }
 case 30:
  ref,ok:=r.Leb()
  if !ok || ref>c.ids { return 0,c.fail(TableRefuseWireDamaged) }
  if ref!=0 { r.meetFirstUse(ref) }
 case 15:
  return c.union(r)
 case 12, 13, 14, 16, 31, 32:
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
 if sub.Offset<0 || count>uint64(len(sub.Buffer))-uint64(sub.Offset) { return 0,c.fail(TableRefuseWireDamaged) }
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
  if !ok || key==0 || key>c.ids { return 0,c.fail(TableRefuseWireDamaged) }
  if c.stamp[key]==gen { return 0,c.fail(TableRefuseWireDamaged) }
  c.stamp[key]=gen
  if _,ok:=probe.Body(); !ok { return 0,c.fail(TableRefuseWireDamaged) }
 }
 for range count {
  key,_:=sub.Leb()
  sub.meetFirstUse(key)
  elem,_:=sub.Body()
  if !c.framed(elem,kind) { return 0,false }
 }
 return sub.Offset,true
}

// union walks one union payload: the arm reference, and where it is not 0 the
// arm's kind and its L-framed payload.
func (c *tableWireCheck) union(r TableReader) (int64, bool) {
 ref,ok:=r.Leb()
 if !ok || ref>c.ids { return 0,c.fail(TableRefuseWireDamaged) }
 if ref==0 { return r.Offset,true }
 r.meetFirstUse(ref)
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

// tableWireSkipValue walks one unknown payload with the wire-alone rules while
// the body read skips it, so a field this reader cannot name is still held to
// every condition the wire alone decides (docs/SPEC-TABLES.md §4, §6.5).
func tableWireSkipValue(r *TableReader, kind uint8) bool {
 var c tableWireCheck
 c.ids=uint64(len(r.Ids))/8
 c.depth=r.Depth
 c.nodes=0 // tableWireSkipNodes
 at,ok:=c.value(*r,kind)
 if !ok { r.Report.Refuse(c.failed(),""); return false }
 r.Offset=at
 return true
}
func tableWireSkipFramed(r *TableReader, kind uint8, arm bool) bool {
 var c tableWireCheck
 c.ids=uint64(len(r.Ids))/8
 c.depth=r.Depth; if arm && (kind==13||kind==14||kind==16) { c.depth-- }
 c.nodes=0 // tableWireSkipNodes
 if !c.framed(*r,kind) { r.Report.Refuse(c.failed(),""); return false }; return true
}

// tableWireRecordCheck walks one node record whose type this reader does not
<<<<<<< HEAD
// name with the same wire-alone rules the body read applies to a record it
// does name (docs/SPEC-TABLES.md §4, §6.5). A blob record is its bytes in every
// reader's walk (§2.5, §3.1), its text held by the storage scan. nodes bounds
// every pointer index; mark, when not nil, names a reachable record for the graph.
func tableWireRecordCheck(id uint64, body TableReader, ids uint64, nodes uint64, mark func(uint64)) TableRefuseReason {
 c:=tableWireCheck{ids:ids,nodes:nodes,mark:mark}
 if id==tableWireBytesID || id==tableWireStringID { return TableRefuseOk }
=======
// name, so no LoadBody reaches it, with the same wire-alone rules the body read
// applies to a record it does name (docs/SPEC-TABLES.md §4, §6.5). nodes is the
// table's record count, which bounds every pointer index; mark, when not nil,
// names a record a pointer reaches so the graph walk starts there too.
func tableWireRecordCheck(id uint64, body TableReader, ids uint64, nodes uint64, mark func(uint64)) TableRefuseReason {
 if id==tableWireBytesID || id==tableWireStringID { return TableRefuseOk }
 var c tableWireCheck
 c.ids=ids
 c.nodes=nodes
 c.mark=mark
>>>>>>> 12486bef9e7a3c8341158d3a08349c987780a157
 end,ok:=c.body(body,false)
 if !ok || end!=int64(len(body.Buffer)) { return c.failed() }
 return c.reason
}

// tableOpenChecked opens a form-1 file and answers the report as a refusal for
// every verdict but TableOpenOk (docs/SPEC-TABLES.md §3, §4, §6.5). The wire
// conditions are the body read's one walk, as it decodes each field; there is
// no separate verification prepass. A variable root's node records are held to
// the same rules as they are decoded, a record of a type this reader cannot
// name is walked by tableWireRecordCheck where it is skipped, and the graph is
// judged over the decoded nodes. A fixed root's body refuses the reserved id
// in its own walk.
func tableOpenChecked(data []byte, report *TableReport) (TableReader, TableOpenVerdict) {
 r,verdict:=tableOpen(data,report)
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
 r.firstUse=tableFirstUse{}
}
`
