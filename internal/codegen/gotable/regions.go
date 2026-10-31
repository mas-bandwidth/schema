package gotable

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A variable unit uses the canonical POD rows throughout its table closure.
// Packet types keep their established spelling; a plain type reached by the
// table uses its Row in the table codecs. No Go pointer is stored in a region.
func storageName(u *ir.Unit, n string) string {
	if len(variableTableNames(u)) > 0 && u.Tables[n] == nil {
		return n + "Row"
	}
	return n
}
func (g *tableGen) storageName(n string) string {
	if g.viewPacket != nil {
		return n
	}
	return storageName(g.unit, n)
}
func pointerTargetId(f *ir.Field) uint64 {
	if !f.Type.Pointer {
		return 0
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return ir.TableWireId(st.WireName())
	}
	return ir.TableWireId(tableFieldTypeName(f))
}
func (g *tableGen) typeCodecColumns(st *ir.Struct) string {
	columns := fmt.Sprintf(", Id:0x%016x, Variable:%v, SaveBody:func(w *TableWriter,p unsafe.Pointer) bool { return %sSaveBody(w,(*%s)(p)) }, LoadBody:func(r TableReader,p unsafe.Pointer) bool { return %sLoadBody(&r,(*%s)(p)) }", ir.TableWireId(st.WireName()), ir.VariableTables(g.unit)[st.Name], st.Name, g.storageName(st.Name), st.Name, g.storageName(st.Name))
	if g.regional && g.surfaces.Retain {
		columns += fmt.Sprintf(",SaveBodyRetain:func(w *TableRetainWriter,p unsafe.Pointer)bool{return %sSaveBodyRetain(w,(*%s)(p))},LoadBodyRetain:func(r TableRetainReader,p unsafe.Pointer)bool{return %sLoadBodyRetain(&r,(*%s)(p))}", st.Name, g.storageName(st.Name), st.Name, g.storageName(st.Name))
	}
	ml := ir.RecordLayout(g.unit, st)
	columns += fmt.Sprintf(", CookSize:%d, CookAlign:%d", ml.Size, ml.Align)

	if g.regional && ir.VariableTables(g.unit)[st.Name] {
		var nodes strings.Builder
		nodes.WriteString(", NodeType:func(id uint64)*TableTypeInfo{switch id {")
		for _, node := range ir.PointerReachable(st) {
			fmt.Fprintf(&nodes, "case 0x%016x:return %sTableType();", ir.TableWireId(node.WireName()), node.Name)
		}
		nodes.WriteString("};return nil}")
		columns += nodes.String()
	}
	return columns
}

// emitMessageDescriptorInit links the descriptor's MESSAGE columns from the
// message form's own file. They are the closures the descriptor literal carried
// before the forms were split, spelled as they were; they are assigned in an
// init() of <Base>TableMessage.go because a core literal that named
// <Name>SaveMessageBody would not compile without the form. Go runs a package's
// init functions file by file in the order the files are presented, which the
// go command makes lexical, and <Base>Table.go sorts before
// <Base>TableMessage.go ('.' < 'M'), so the core's literal is in place first
// (docs/SPEC-TABLES.md, "GO's optional forms").
func (g *tableGen) emitMessageDescriptorInit(st *ir.Struct) {
	n := st.Name
	typ := g.storageName(st.Name)
	g.pf("func init() {\n")
	g.pf("%sTableInfo.SaveMessageBody = func(w *TableMessageWriter,p unsafe.Pointer)bool{return %sSaveMessageBody(w,(*%s)(p))}\n", n, n, typ)
	g.pf("%sTableInfo.LoadMessageBody = func(r TableMessageReader,p unsafe.Pointer)(TableMessageReader,bool){ok:=%sLoadMessageBody(&r,(*%s)(p));return r,ok}\n", n, n, typ)
	g.pf("}\n\n")
}
func (g *tableGen) emitUnitBlittableRows(blocks *ir.BlockUnit) {
	if blocks != nil && blockHome(g.unit, blocks) != "" {
		bg := &blockGen{unit: g.unit, blocks: blocks}
		for _, name := range blocks.Order {
			bg.emitBlittable(name)
		}
		g.types.WriteString(bg.structs.String())
	}
	ck := cookUnitOf(g.unit)
	cg := &cookGen{unit: g.unit, cook: ck, blocks: blocks}
	for _, name := range ck.order {
		if cg.blockHasRecord(name) {
			continue
		}
		cg.emitBlittable(name)
	}
	g.types.WriteString(cg.structs.String())
	if g.regional {
		missing := map[string]*ir.MemberLayout{}
		for n := range ir.TableClosure(g.unit) {
			if ck.members[n] == nil && (blocks == nil || blocks.Layout(n) == nil) {
				if st := cookMember(g.unit, n); st != nil {
					missing[n] = ir.RecordLayout(g.unit, st)
				}
			}
		}
		mg := &cookGen{unit: g.unit, cook: &cookUnit{members: missing}}
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			mg.emitBlittable(n)
		}
		g.types.WriteString(mg.structs.String())
	}
}
func (g *tableGen) emitRegionRuntime(blocks *ir.BlockUnit) {
	names := make([]string, 0, len(g.unionArmSlot))
	for n := range g.unionArmSlot {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		un := g.unit.Unions[n]
		if un == nil {
			un = g.unit.TableUnions[n]
		}
		g.emitUnionRow(un)
	}
	g.pf("const tableRegionAlign int64 = %d\n", ir.TableRegionAlign(g.unit))
	g.pf("const tableBytesTypeID uint64=0x%016x\nconst tableStringTypeID uint64=0x%016x\n", ir.TableWireId("bytes"), ir.TableWireId("string"))
	source := tableRegionSource + tableRegionBlobSource + tableBuilderLoadSource + tableWireExtentSource
	if unitHasContainers(g.unit) {
		source = containerRegionSource(source)
		source += tableContainerSource + tableMapSource
	}
	source = unboundedTextRegion(g.unit, source)
	// §2.3: an absent ?T over a variable closure owns no node, so Lock's pack
	// writes it at its declared defaults, every pointer null, exactly as load
	// leaves it, never the stale arena reference its storage may still hold;
	// emitted only where the construct is, so no other unit moves
	if len(ir.TableOptionalPointerClosures(g.unit)) > 0 {
		reset := tableRegionClearAbsentSource
		if unitHasContainers(g.unit) {
			// the container pack places each node's containers in the region
			// first, then the clear walks their elements there (a nil arena)
			source = tableSourceReplace(source, ";if !tableRegionEdges(p,e.info,nil,func(slot *TableRef,f *TableFieldInfo) bool {", ";tableRegionClearAbsent(p,e.info);if !tableRegionEdges(p,e.info,nil,func(slot *TableRef,f *TableFieldInfo) bool {", 1)
			reset = tableSourceReplace(reset, "p:=unsafe.Add(base,f.Offset)\n", "p:=unsafe.Add(base,f.Offset)\n  if f.List||f.Map {h:=(*tableContainer)(p);if h.Count<=0{continue};cursor:=tableMapCursor(h,f,nil);for range h.Count {elem:=cursor.Next();if elem==nil{break};tableRegionClearAbsentValue(elem,f)};cursor.Release();continue}\n", 1)
		} else {
			source = tableSourceReplace(source, ";if !tableRegionEdges(p,e.info,func(slot *TableRef,f *TableFieldInfo) bool {", ";tableRegionClearAbsent(p,e.info);if !tableRegionEdges(p,e.info,func(slot *TableRef,f *TableFieldInfo) bool {", 1)
		}
		source += reset
	}
	g.pf("%s", source)
}

// tableRegionClearAbsentSource resets, in a packed node, every absent scalar
// ?T to its declared defaults (docs/SPEC-TABLES.md §2.3): a table by Reset, a
// union by its zero (no arm) storage. It descends the way tableRegionEdges
// does — present nested tables, their arrays, a union's live arm — so an
// absent ?U nested in a present value is reset too.
const tableRegionClearAbsentSource = `func tableRegionClearAbsent(base unsafe.Pointer,info *TableTypeInfo){
 for i:=range info.Fields {f:=&info.Fields[i];if !tableJSONGuardHolds(base,info,f.Guard){continue};p:=unsafe.Add(base,f.Offset)
  if f.Optional && !*(*bool)(unsafe.Add(base,f.PresentOffset)) {if !f.IsArray&&!f.List&&!f.Map {if f.Kind==13&&f.Table!=nil {f.Table().Reset(p)} else if f.Kind==15 {clear(unsafe.Slice((*byte)(p),f.ElemSize))}};continue}
  if f.List||f.Map {continue}
  count:=int32(1);if f.IsArray {count=tableRegionCount(base,f);if count<0 || count>f.ArrayBound{continue}};for j := range count {tableRegionClearAbsentValue(unsafe.Add(p,uintptr(j)*uintptr(f.ElemSize)),f)}
 }
}
func tableRegionClearAbsentValue(p unsafe.Pointer,f *TableFieldInfo){
 if f.Pointer {return}
 if f.Kind==13 && f.Table!=nil {tableRegionClearAbsent(p,f.Table());return}
 if f.Kind==15 && f.Arms!=nil {arms:=f.Arms();tag:=tableJSONGetRaw(unsafe.Add(p,arms.TagOffset),arms.TagSize);if tag==0 || tag>=uint64(len(arms.Arms)){return};arm:=&arms.Arms[tag];if arm.Void{return};af:=&arm.Field;at:=unsafe.Add(p,af.Offset);count:=int32(1);if af.IsArray {count=tableRegionCount(p,af);if count<0 || count>af.ArrayBound{return}};for j := range count {tableRegionClearAbsentValue(unsafe.Add(at,uintptr(j)*uintptr(af.ElemSize)),af)}}
}
`

func (g *tableGen) emitRegionMember(st *ir.Struct) {
	n, typ := st.Name, g.storageName(st.Name)
	if ir.PointerTargets(g.unit)[n] {
		g.pf("func %sEmplace(worker *TableWorker,slot *TableRef) *%s { p,ref:=worker.Alloc(int64(unsafe.Sizeof(%s{}))); *slot=ref;if p==nil{return nil};value:=(*%s)(p);%sReset(value);return value }\n", n, typ, typ, typ, n)
	}
	if st.IsMapEntry() || !ir.VariableTables(g.unit)[n] {
		return
	}
	bytes, str := ir.PointerReachableBlobs(st)
	blobmask := 0
	if bytes {
		blobmask |= 1
	}
	if str {
		blobmask |= 2
	}
	if st.Integrity != "" {
		g.emitIntegrityMember(st, blobmask)
	} else {
		g.pf("func %sLoadMeasure(wire []byte) int64 { n,_,_:=tableRegionMeasure(wire,%sTableType(),%sTableType().NodeType,%d);return n }\n", n, n, n, blobmask)
		g.pf("func %sLoadMeasureReason(wire []byte)(int64,error){size,_,_,reason:=tableRegionMeasureReason(wire,%sTableType(),%sTableType().NodeType,%d);return size,tableRefuseError(reason)}\n", n, n, n, blobmask)
		// THE PLAIN NAME IS THE STRICT READ, THE TOLERANT ONE IS NAMED (docs/SPEC-TABLES.md §6.5).
		g.pf("// %sLoadTolerant is the TOLERANT region read (docs/SPEC-TABLES.md §6.5): it accepts every wire the\n// framing walk accepts and returns the recovered root, with the counters counted onto the report passed.\n", n)
		g.pf("func %sLoadTolerant(region,wire []byte,report *TableReport) *%s { return (*%s)(tableRegionLoad(region,wire,%sTableType(),%sTableType().NodeType,%d,report)) }\n", n, typ, typ, n, n, blobmask)
		g.pf("func %sMeasure(value *%s,context ...TableWriteContext) int64 { arena,allocator:=tableWriteOptions(context);return tableRegionSave(unsafe.Pointer(value),%sTableType(),nil,true,arena,allocator) }\n", n, typ, n)
		g.pf("func %sMeasureReason(value *%s,context ...TableWriteContext)(int64,error){arena,allocator:=tableWriteOptions(context);size,reason:=tableRegionSaveReason(unsafe.Pointer(value),%sTableType(),nil,true,arena,allocator);return size,tableRefuseError(reason)}\n", n, typ, n)
		g.pf("func %sSave(value *%s,buffer []byte,context ...TableWriteContext) int64 { arena,allocator:=tableWriteOptions(context);return tableRegionSave(unsafe.Pointer(value),%sTableType(),buffer,false,arena,allocator) }\n", n, typ, n)
		g.pf("// %sLoadBuilderTolerant is the TOLERANT builder read (docs/SPEC-TABLES.md §6.5): it keeps the recovered\n// value in the builder, with the counters counted onto the report passed.\n", n)
		g.pf("func %sLoadBuilderTolerant(b *%sBuilder,wire []byte,report *TableReport) bool {return tableBuilderLoad(unsafe.Pointer(b.GetRoot()),b.root,&b.Main,wire,%sTableType(),%sTableType().NodeType,%d,report)}\n", n, n, n, n, blobmask)
	}
	g.pf("// %sLoad is the plain region read, and it is STRICT (docs/SPEC-TABLES.md §6.5): a nil root for whatever\n// %sReadStrict refuses; a supplied report receives THIS read's report, and report.Err() is the reason.\n", n, n)
	g.pf("func %sLoad(region,wire []byte,report *TableReport) *%s {read:=report;if read==nil{read=&TableReport{}}else{*read=TableReport{}};root:=%sLoadTolerant(region,wire,read);if root==nil||read.Err()!=nil{return nil};return root}\n", n, typ, n)
	g.pf("// %sLoadBuilder is the plain builder read, and it is STRICT (docs/SPEC-TABLES.md §6.5): false for whatever\n// %sReadStrict refuses, and the builder is then only shut down; a supplied report receives THIS read's\n// report, and report.Err() is the reason.\n", n, n)
	g.pf("func %sLoadBuilder(b *%sBuilder,wire []byte,report *TableReport) bool {read:=report;if read==nil{read=&TableReport{}}else{*read=TableReport{}};ok:=%sLoadBuilderTolerant(b,wire,read);return ok&&read.Err()==nil}\n", n, n, n)
	// ReadStrict is §6.5's STRICT read: one call, its own builder, one typed reason.
	g.pf("// %sReadStrict is the STRICT read (docs/SPEC-TABLES.md §6.5): one call, its own builder, one typed reason.\n", n)
	g.pf("func %sReadStrict(wire []byte)(*%s,error){var b %sBuilder;size:=%sLoadMeasure(wire);size=max(size,0);if !b.initSized(size){return nil,TableRefuseAllocationFailed};var report TableReport;ok:=%sLoadBuilderTolerant(&b,wire,&report);if err:=report.Err();err!=nil||!ok{b.Shutdown();if err==nil{err=TableRefuseWireDamaged};return nil,err};if !b.Lock(){b.Shutdown();return nil,TableRefuseAllocationFailed};return b.AsConst(),nil}\n", n, typ, n, n, n)
	g.pf("type %sBuilder struct { Arena TableArena; Main TableWorker; root TableRef; region,allocation []byte }\n", n)
	g.pf("func (b *%sBuilder) Init(allocator ...TableAllocator) bool { if b.region!=nil||!b.Arena.Init(allocator...){return false};b.Main=TableWorker{Arena:&b.Arena};p,ref:=b.Main.Alloc(int64(unsafe.Sizeof(%s{})));b.root=ref;if p==nil{b.Arena.Shutdown();b.root=0;return false};%sReset((*%s)(p));return true }\n", n, typ, n, typ)
	g.pf("func (b *%sBuilder) initSized(first int64, allocator ...TableAllocator) bool { if b.region!=nil||!b.Arena.initSized(first, allocator...){return false};b.Main=TableWorker{Arena:&b.Arena};p,ref:=b.Main.Alloc(int64(unsafe.Sizeof(%s{})));b.root=ref;if p==nil{b.Arena.Shutdown();b.root=0;return false};%sReset((*%s)(p));return true }\n", n, typ, n, typ)
	g.pf("func (b *%sBuilder) GetRoot() *%s { if b.Arena.Locked {return nil};return (*%s)(b.Arena.At(b.root)) }\n", n, typ, typ)
	g.pf("func (b *%sBuilder) AsConst() *%s { if len(b.region)==0{return nil};return (*%s)(unsafe.Pointer(&b.region[0])) }\n", n, typ, typ)
	g.pf("func (b *%sBuilder) Region() []byte { return b.region }\n", n)
	g.pf("func (b *%sBuilder) PackMeasure() int64 {if b.Arena.Locked{return int64(len(b.region))};return tableRegionPackMeasure(unsafe.Pointer(b.GetRoot()),%sTableType(),&b.Arena)}\n", n, n)
	g.pf("func (b *%sBuilder) PackMeasureReason()(int64,error){if b.Arena.Locked{return int64(len(b.region)),nil};size,reason:=tableRegionPackMeasureReason(unsafe.Pointer(b.GetRoot()),%sTableType(),&b.Arena);return size,tableRefuseError(reason)}\n", n, n)
	g.pf("func (b *%sBuilder) Lock() bool { if b.Arena.Locked { return true };region,allocation,ok:=tableRegionPack(unsafe.Pointer(b.GetRoot()),%sTableType(),&b.Arena);if !ok{return false};b.region=region;b.allocation=allocation;b.Arena.Shutdown();b.Arena.Locked=true;return true }\n", n, n)
	g.pf("func (b *%sBuilder) Shutdown() { b.Arena.Shutdown();b.Arena.Allocator.free(b.allocation);b.region=nil;b.allocation=nil;b.root=0 }\n", n)
}

// Kept separate from the fixed file runtime: a fixed-only unit pays for none
// of the arena, numbering map, node scan or region directory.
const tableRegionSource = tableAllocatorSource + `
type TableRef = int64

const tableArenaSegmentBytes int64 = 1<<20
const tableArenaSlabBytes int64 = 1<<16
const tableArenaSegments = 4096

type tableArenaSegment struct { data []byte; owned bool }
type TableArena struct {
 Allocator TableAllocator
 Locked bool
 next atomic.Uint64
 segments [tableArenaSegments]atomic.Pointer[tableArenaSegment]
 storage [tableArenaSegments]tableArenaSegment
 initialized bool
 mutex sync.Mutex
 first int64
}
func (a *TableArena) Init(allocator ...TableAllocator) bool {if a==nil||a.initialized{return false};pair:=TableAllocator{};if len(allocator)>0{pair=allocator[0]};if !pair.valid(){return false};a.Allocator=pair;a.Locked=false;a.initialized=true;a.first=0;a.next.Store(uint64(tableArenaSlabBytes));return true}
// initSized is Init with segment 0 sized to the input. A strict read's arena
// then costs its input rather than a whole megabyte. Nothing ever moves
// (docs/SPEC-TABLES.md §6.4, "Building on many threads").
func (a *TableArena) initSized(first int64, allocator ...TableAllocator) bool {if !a.Init(allocator...){return false};if first<=0{return true};first=(first+tableArenaSlabBytes-1)& -tableArenaSlabBytes;first=max(first,tableArenaSlabBytes);if first>=tableArenaSegmentBytes{return true};a.first=first;a.next.Store(uint64(tableRegionAlign));return true}
func (a *TableArena) Shutdown() {for i:=range a.segments{if s:=a.segments[i].Swap(nil);s!=nil {if s.owned{a.Allocator.free(s.data)};*s=tableArenaSegment{}}};a.initialized=false;a.next.Store(0)}
func (a *TableArena) At(ref TableRef) unsafe.Pointer { if ref<=0 || ref>=tableArenaSegmentBytes*tableArenaSegments {return nil};s:=a.segments[ref/tableArenaSegmentBytes].Load();if s==nil{return nil};return unsafe.Pointer(&s.data[ref%tableArenaSegmentBytes]) }
func (a *TableArena) grab(bytes int64) (int64,int64) {
 if !a.initialized || a.Locked || bytes<=0 {return 0,0}
 span:=bytes>tableArenaSlabBytes
 size:=tableArenaSlabBytes;if span {size=(bytes+tableArenaSegmentBytes-1)& -tableArenaSegmentBytes}
 var at,end uint64
 for {at=a.next.Load();if at==0 {return 0,0};if span {at=(at+uint64(tableArenaSegmentBytes)-1)& ^uint64(tableArenaSegmentBytes-1)};end=at+uint64(size);if a.first>0 && !span && at<uint64(tableArenaSegmentBytes) && end>uint64(a.first) {if at<uint64(tableArenaSlabBytes) {end=uint64(a.first)} else {at=uint64(tableArenaSegmentBytes);end=at+uint64(size)}};if end>uint64(tableArenaSegmentBytes*tableArenaSegments){return 0,0};old:=a.next.Load();candidate:=old;if span {candidate=(old+uint64(tableArenaSegmentBytes)-1)& ^uint64(tableArenaSegmentBytes-1)};if a.first>0 && !span && candidate<uint64(tableArenaSegmentBytes) && candidate+uint64(size)>uint64(a.first) && candidate>=uint64(tableArenaSlabBytes) {candidate=uint64(tableArenaSegmentBytes)};if candidate!=at {continue};if a.next.CompareAndSwap(old,end){break}}
 first,last:=int64(at)/tableArenaSegmentBytes,(int64(end)-1)/tableArenaSegmentBytes
 if a.segments[first].Load()==nil { a.mutex.Lock();if a.segments[first].Load()==nil { n:=(last-first+1)*tableArenaSegmentBytes;if first==0 && a.first>0 && last==0 {n=a.first};data:=a.Allocator.alloc(n);if data==nil {a.mutex.Unlock();return 0,0};clear(data);for i:=first;i<=last;i++ {a.storage[i]=tableArenaSegment{data:data[(i-first)*tableArenaSegmentBytes:],owned:i==first};a.segments[i].Store(&a.storage[i])} };a.mutex.Unlock() }
 return int64(at),int64(end)
}
type TableWorker struct { Arena *TableArena; front,end int64; refused bool }
func (w *TableWorker) Alloc(n int64) (unsafe.Pointer,TableRef) { if w==nil || w.Arena==nil || w.Arena.Locked || n<=0 {return nil,0};n=tableRegionRound(n);if n>tableArenaSlabBytes {at,_:=w.Arena.grab(n);return w.Arena.At(at),at};if w.front+n>w.end {w.front,w.end=w.Arena.grab(n);if w.front==0{return nil,0}};at:=w.front;w.front+=n;return w.Arena.At(at),at }
func tableOptionalArena(a []*TableArena) *TableArena { if len(a)>0 {return a[0]};return nil }
func tableRefAt(slot *TableRef,a *TableArena) unsafe.Pointer { if slot==nil || *slot==0 {return nil};if a!=nil{return a.At(*slot)};return unsafe.Add(unsafe.Pointer(slot),*slot) }
func tableRegionCount(base unsafe.Pointer,f *TableFieldInfo) int32 {if f.Counted {return *(*int32)(unsafe.Add(base,f.CountOffset))};return f.ArrayBound}
func tableRegionRound(n int64) int64 {return (n+tableRegionAlign-1)& -tableRegionAlign}

type tableNodeEntry struct { json tableJSONGraphNode; node unsafe.Pointer; info *TableTypeInfo; id uint64; open bool; offset int64 }
// The root and arena keep the source storage live while pointer-free backing
// allocations hold the index and entries. Temporary storage is released by caller.
type TableNumbering struct { shared bool; reason TableRefuseReason; arena *TableArena; root unsafe.Pointer; allocator TableAllocator; entries []tableNodeEntry; buckets []uint64; memory,indexMemory []byte }
func (n *TableNumbering) Release() {n.allocator.free(n.memory);n.allocator.free(n.indexMemory);n.memory=nil;n.indexMemory=nil;n.entries=nil;n.buckets=nil;n.root=nil}
func (n *TableNumbering) find(p unsafe.Pointer)(int,bool) {if p==nil||len(n.buckets)==0{return 0,false};h:=uint64(uintptr(p));h^=h>>33;h*=0xff51afd7ed558ccd;h^=h>>33;mask:=uint64(len(n.buckets)-1);for slot:=h&mask;;slot=(slot+1)&mask {v:=n.buckets[slot];if v==0{return int(slot),false};if n.entries[v-1].node==p{return int(v-1),true}}}
func (n *TableNumbering) grow()bool {
 capacity:=cap(n.entries)*2;capacity=max(capacity,16);if capacity>int(^uint(0)>>1)/int(unsafe.Sizeof(tableNodeEntry{})){return false}
 memory:=n.allocator.alloc(int64(capacity)*int64(unsafe.Sizeof(tableNodeEntry{})));if memory==nil{return false}
 index:=n.allocator.alloc(int64(capacity)*16);if index==nil{n.allocator.free(memory);return false};clear(index)
 entries:=unsafe.Slice((*tableNodeEntry)(unsafe.Pointer(&memory[0])),capacity);copy(entries,n.entries);length:=len(n.entries)
 oldMemory,oldIndex:=n.memory,n.indexMemory;n.memory,n.indexMemory=memory,index;n.entries=entries[:length];n.buckets=unsafe.Slice((*uint64)(unsafe.Pointer(&index[0])),capacity*2)
 for i:=range n.entries {slot,_:=n.find(n.entries[i].node);n.buckets[slot]=uint64(i+1)}
 n.allocator.free(oldMemory);n.allocator.free(oldIndex);return true
}
func (n *TableNumbering) Index(slot *TableRef)(uint64,bool){if n==nil{return 0,false};i,ok:=n.find(tableRefAt(slot,n.arena));return uint64(i+1),ok}
// A blob is owned by ONE slot (docs/SPEC-TABLES.md §2.5, §16.7): visit marks a
// second slot naming a blob as shared, and the wire and text writers refuse
// blob_shared; Lock and Cook pack the one node as they find it.
func (n *TableNumbering) visit(p unsafe.Pointer,t *TableTypeInfo,id uint64)bool {
 if p==nil{return true};if i,ok:=n.find(p);ok{if n.entries[i].open{n.reason=TableRefuseDataCycle;return false};if n.entries[i].id!=id{n.reason=TableRefuseInvalidValue;return false};if n.entries[i].info==nil{n.shared=true};return true}
 if len(n.entries)==cap(n.entries)&&!n.grow(){n.reason=TableRefuseAllocationFailed;return false};slot,_:=n.find(p);i:=len(n.entries);n.entries=n.entries[:i+1];n.entries[i]=tableNodeEntry{node:p,info:t,id:id,open:true};n.buckets[slot]=uint64(i+1);a:=n.arena
 if t!=nil&&!tableRegionEdges(p,t,func(slot *TableRef,f *TableFieldInfo)bool {var target *TableTypeInfo;if f.Table!=nil{target=f.Table()};return n.visit(tableRefAt(slot,a),target,f.TargetId)}){return false};n.entries[i].open=false;return true
}
func tableNumber(root unsafe.Pointer,info *TableTypeInfo,a *TableArena,allocator ...TableAllocator)(TableNumbering,bool){n:=TableNumbering{arena:a,root:root};if a!=nil{n.allocator=a.Allocator};if len(allocator)>0{n.allocator=allocator[0]};if root==nil||!n.visit(root,info,info.ID){if n.reason==TableRefuseOk{n.reason=TableRefuseInvalidValue};n.Release();return n,false};return n,true}
func tableRegionEdges(base unsafe.Pointer,info *TableTypeInfo,visit func(*TableRef,*TableFieldInfo) bool) bool {
 for i:=range info.Fields {f:=&info.Fields[i];if !tableJSONGuardHolds(base,info,f.Guard) || f.Optional && !*(*bool)(unsafe.Add(base,f.PresentOffset)){continue};p:=unsafe.Add(base,f.Offset);count:=int32(1);if f.IsArray {count=tableRegionCount(base,f);if count<0 || count>f.ArrayBound{return false}};for j := range count {at:=unsafe.Add(p,uintptr(j)*uintptr(f.ElemSize));if !tableRegionValueEdges(at,f,visit){return false}} };return true
}
func tableRegionValueEdges(p unsafe.Pointer,f *TableFieldInfo,visit func(*TableRef,*TableFieldInfo) bool) bool {
 if f.Pointer {return visit((*TableRef)(p),f)}
 if f.Kind==13 && f.Table!=nil {return tableRegionEdges(p,f.Table(),visit)}
 if f.Kind==15 && f.Arms!=nil {arms:=f.Arms();tag:=tableJSONGetRaw(unsafe.Add(p,arms.TagOffset),arms.TagSize);if tag>=uint64(len(arms.Arms)){return false};if tag!=0 {arm:=&arms.Arms[tag];if arm.Void{return true};af:=&arm.Field;at:=unsafe.Add(p,af.Offset);count:=int32(1);if af.IsArray {count=tableRegionCount(p,af);if count<0 || count>af.ArrayBound{return false}};for j := range count {if !tableRegionValueEdges(unsafe.Add(at,uintptr(j)*uintptr(af.ElemSize)),af,visit){return false}}} };return true
}
func tableNodeWrite(w *TableWriter,e *tableNodeEntry) bool { if e.info!=nil{return e.info.SaveBody(w,e.node)};length:=uint64(*(*uint32)(e.node));w.Raw(unsafe.Slice((*byte)(unsafe.Add(e.node,8)),int(length)));return !w.Overflow }
func tableNodeMeasure(w *TableWriter,ids *TableIds,e *tableNodeEntry)int64 {*w=TableWriter{Measuring:true,Ids:ids};if !tableNodeWrite(w,e){return -1};return w.Offset}
// A typed frame keeps its pointers visible to Go's collector. The descriptor
// callbacks make it escape once per call; node-proportional storage uses the pair.
type tableWireFrame struct {numbering TableNumbering;ids TableIds;writer,measure TableWriter}
// Save refuses a blob named from two slots by name, blob_shared, and never
// writes a copy per slot (docs/SPEC-TABLES.md §2.5, §16.7).
func tableRegionSave(root unsafe.Pointer,info *TableTypeInfo,buffer []byte,measure bool,a *TableArena,allocator ...TableAllocator)int64{n,_:=tableRegionSaveReason(root,info,buffer,measure,a,allocator...);return n}
func tableRegionSaveReason(root unsafe.Pointer,info *TableTypeInfo,buffer []byte,measure bool,a *TableArena,allocator ...TableAllocator) (int64,TableRefuseReason) {
 frame:=tableWireFrame{};var ok bool;frame.numbering,ok=tableNumber(root,info,a,allocator...);if !ok||frame.numbering.shared{if ok{frame.numbering.Release();frame.numbering.reason=TableRefuseBlobShared};return -1,frame.numbering.reason};n:=&frame.numbering;defer n.Release();ids:=&frame.ids;*ids=TableIds{Numbering:n};w:=&frame.writer;*w=TableWriter{Buffer:buffer,Measuring:measure,Ids:ids};w.Put8(1);if !info.SaveBody(w,root){return -1,TableRefuseInvalidValue};w.Offset--
 if len(n.entries)>1 {w.ID(0xffffffffffffffff);w.Put8(12);payload:=tableLebBytes(uint64(len(n.entries)-1));for i:=1;i<len(n.entries);i++ {e:=&n.entries[i];payload+=tableLebBytes(ids.Ref(e.id));size:=tableNodeMeasure(&frame.measure,ids,e);if size<0{return -1,TableRefuseInvalidValue};payload+=tableLebBytes(uint64(size))+size};w.PutLeb(uint64(payload));w.PutLeb(uint64(len(n.entries)-1));for i:=1;i<len(n.entries);i++ {e:=&n.entries[i];w.ID(e.id);size:=tableNodeMeasure(&frame.measure,ids,e);if size<0{return -1,TableRefuseInvalidValue};w.PutLeb(uint64(size));if w.Measuring {w.Advance(size)} else if !tableNodeWrite(w,e){return -1,TableRefuseInvalidValue}} }
 w.Put8(0);w.Trailer();if w.Overflow||ids.Overflow{return -1,TableRefuseInvalidValue};return w.Offset,TableRefuseOk
}

// Resolve under Once (the file loads) marks a blob's directory entry when a
// slot first names it; a second slot names the same node, never a copy, and
// counts BlobShared, which the strict read refuses (docs/SPEC-TABLES.md §2.5,
// §6.5). The message form's region reader resolves under Once too.
type TableNodeDirEntry struct { Offset,TypeID uint64 }
type TableNodeMap struct { Base unsafe.Pointer; Entries []TableNodeDirEntry; Good bool; Arena bool; Once bool }
func (m TableNodeMap) Resolve(slot *TableRef,index,target uint64,r *TableReport) { *slot=0;if index==0||!m.Good{return};if index>uint64(len(m.Entries)){r.Malformed=true;return};entry:=m.Entries[index-1];if entry.Offset==^uint64(0){return};if entry.TypeID!=target{if !m.Once||entry.TypeID!=^target||target!=tableBytesTypeID&&target!=tableStringTypeID{r.KindMismatch++;return};r.BlobShared++}else if m.Once&&(target==tableBytesTypeID||target==tableStringTypeID){m.Entries[index-1].TypeID=^target};if m.Arena {*slot=int64(entry.Offset)} else {*slot=int64(uintptr(unsafe.Add(m.Base,entry.Offset))-uintptr(unsafe.Pointer(slot)))} }

type tableNodeScan struct { payload TableReader; declared,records uint64; present,bad bool }
func tableNodeScanBegin(r TableReader) tableNodeScan {
 s:=tableNodeScan{};r.Offset=0
 for {ref,ok:=r.Leb();if !ok||ref==0 {break};id,ok:=r.Resolve(ref);if !ok||!r.Has(1){break};kind:=r.Get8();if id==0xffffffffffffffff {s.present=true;if kind!=12{s.bad=true;return s};sub,ok:=r.Body();if !ok{s.bad=true;return s};s.payload=sub} else if !r.Skip(kind){break} }
 if s.present {var ok bool;s.declared,ok=s.payload.Leb();if !ok{s.bad=true}};return s
}
func (s *tableNodeScan) next() (uint64,TableReader,bool) { r:=&s.payload;if s.bad || r.Offset>=int64(len(r.Buffer)){return 0,TableReader{},false};ref,ok:=r.Leb();if !ok{s.bad=true;return 0,TableReader{},false};id,ok:=r.Resolve(ref);if !ok{s.bad=true;return 0,TableReader{},false};body,ok:=r.Body();if !ok{s.bad=true;return 0,TableReader{},false};s.records++;return id,body,true }
func (s *tableNodeScan) whole() bool {return !s.bad && (!s.present||s.declared==s.records)}
func tableNodeStorage(id uint64,body TableReader,nodeType func(uint64)*TableTypeInfo,blobs int)int64{n,_:=tableNodeStorageReason(id,body,nodeType,blobs);return n}
func tableNodeStorageReason(id uint64,body TableReader,nodeType func(uint64)*TableTypeInfo,blobs int)(int64,TableRefuseReason){if t:=nodeType(id);t!=nil{return tableRegionRound(int64(t.Size)),TableRefuseOk};if id==tableBytesTypeID&&blobs&1!=0||id==tableStringTypeID&&blobs&2!=0{extra:=int64(0);if id==tableStringTypeID{extra=1};if int64(len(body.Buffer))>0xffffffff{return -2,TableRefuseBlobOverSizeCap};return tableRegionRound(8+int64(len(body.Buffer))+extra),TableRefuseOk};return -1,TableRefuseOk}
func tableRegionMeasure(wire []byte,root *TableTypeInfo,nodeType func(uint64)*TableTypeInfo,blobs int)(int64,int64,TableOpenVerdict){n,a,v,_:=tableRegionMeasureReason(wire,root,nodeType,blobs,true);return n,a,v}
// tableRegionMeasureReason is the one measure body; a second copy would leave the
// lists.go:154 container patch on the first only. Strict by default, it refuses any
// framing damage. A trailing true makes it tolerant: it skips the framing walk and the
// whole-scan check, so tableRegionMeasure and tableRegionLoad keep the decoded prefix
// of a damaged wire (docs/SPEC-TABLES.md section 4). No new package-scope name.
func tableRegionMeasureReason(wire []byte,root *TableTypeInfo,nodeType func(uint64)*TableTypeInfo,blobs int,tolerant ...bool)(int64,int64,TableOpenVerdict,TableRefuseReason){strict:=len(tolerant)==0||!tolerant[0];var ignored TableReport;r,verdict:=tableOpenFramed(wire,&ignored);if verdict!=TableOpenOk{reason:=TableRefuseWireDamaged;if verdict==TableOpenRefused{reason=TableRefuseUnknownForm};return -1,0,verdict,reason};if strict{probe:=r;probe.Offset=0;framed:=true;for{ref,ok:=probe.Leb();if !ok{framed=false;break};if ref==0{break};id,ok:=probe.Resolve(ref);if !ok||!probe.Has(1){framed=false;break};kind:=probe.Get8();if id==0xffffffffffffffff{if kind!=12{framed=false;break};if _,ok:=probe.Body();!ok{framed=false;break}}else if !probe.Skip(kind){framed=false;break}};if !framed||probe.Offset!=int64(len(probe.Buffer)){return -1,0,TableOpenDamaged,TableRefuseWireDamaged}};data:=tableRegionRound(int64(root.Size));s:=tableNodeScanBegin(r);for{id,body,ok:=s.next();if !ok{break};n,reason:=tableNodeStorageReason(id,body,nodeType,blobs);if n== -2{return -1,0,TableOpenRefused,reason};if n>0{data+=n}};if strict&&!s.whole(){return -1,0,TableOpenDamaged,TableRefuseWireDamaged};if strict{w:=TableReader{Buffer:r.Buffer,Ids:r.Ids};at:=int64(0);nodes:=int64(s.records);scan:=tableNodeScanBegin(w);for{id,body,ok:=scan.next();if !ok{break};if id==tableStringTypeID&&blobs&2!=0&&!tableUTF8Valid(body.Buffer){return -1,0,TableOpenDamaged,TableRefuseWireDamaged};if t:=nodeType(id);t!=nil&&tableWireExtent(&body,t,&at,nodes)!=TableRefuseOk{return -1,0,TableOpenDamaged,TableRefuseWireDamaged}};if tableWireExtent(&w,root,&at,nodes)!=TableRefuseOk{return -1,0,TableOpenDamaged,TableRefuseWireDamaged}};attribution:=int64(s.records+1)*16;return data+attribution,attribution,TableOpenOk,TableRefuseOk}

func tableRegionLoad(region,wire []byte,root *TableTypeInfo,nodeType func(uint64)*TableTypeInfo,blobs int,report *TableReport) unsafe.Pointer {
 if report==nil {var ignored TableReport;report=&ignored};r,verdict:=tableOpenFramed(wire,report);report.Verdict=verdict;if verdict!=TableOpenOk {if verdict==TableOpenDamaged{report.Malformed=true}else {report.Reason="newer_form";if len(wire)>0{if wire[0]==2{report.Reason="message_form_as_file"}else if wire[0]==0{report.Reason="previous_form"}}};return nil} // docs/SPEC-TABLES.md §3 (THE FIRST BYTE), §6.5
 n,attr,_:=tableRegionMeasure(wire,root,nodeType,blobs);if n<0||int64(len(region))<n||len(region)==0||uintptr(unsafe.Pointer(&region[0]))%uintptr(tableRegionAlign)!=0 {report.Malformed=true;return nil};clear(region);data:=n-attr;base:=unsafe.Pointer(&region[0]);directory:=unsafe.Slice((*TableNodeDirEntry)(unsafe.Add(base,data)),int(attr/16));directory[0]=TableNodeDirEntry{0,root.ID};nodes:=TableNodeMap{Base:base,Entries:directory};nodes.Once=true;root.Reset(base)
 s:=tableNodeScanBegin(r);used:=tableRegionRound(int64(root.Size));k:=1;unknown:=int32(0)
 for {id,body,ok:=s.next();if !ok{break};size:=tableNodeStorage(id,body,nodeType,blobs);directory[k].TypeID=id;if id==tableStringTypeID && blobs&2!=0 && !tableUTF8Valid(body.Buffer){directory[k].Offset=^uint64(0);report.Malformed=true} else if size<=0 {directory[k].Offset=^uint64(0);unknown++} else {directory[k].Offset=uint64(used);p:=unsafe.Add(base,used);if t:=nodeType(id);t!=nil{t.Reset(p)}else{*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(len(body.Buffer)))}};used+=size};k++ }
 nodes.Good=s.whole();if nodes.Good {report.Unknown+=unknown}else {report.Malformed=true}
 if nodes.Good {s=tableNodeScanBegin(r);k=1;for {id,body,ok:=s.next();if !ok{break};if directory[k].Offset!=^uint64(0){p:=unsafe.Add(base,directory[k].Offset);if t:=nodeType(id);t!=nil{body.Nodes=nodes;t.LoadBody(body,p)}else{copy(unsafe.Slice((*byte)(unsafe.Add(p,8)),len(body.Buffer)),body.Buffer)}};k++} }
 r.Nodes=nodes;r.Nested=false;root.LoadBody(r,base);if tableNonFiniteRefused(report){return nil};return base
}
func tablePackNodeBytes(e *tableNodeEntry,a *TableArena)int64 {if e.info!=nil {return tableRegionRound(int64(e.info.Size))};extra:=int64(0);if e.id==tableStringTypeID{extra=1};return tableRegionRound(8+int64(*(*uint32)(e.node))+extra)}
func tablePackOffsets(n *TableNumbering)int64 {size:=int64(0);for i:=range n.entries{e:=&n.entries[i];extent:=tablePackNodeBytes(e,n.arena);if extent<0||size>math.MaxInt64-extent{return -1};e.offset=size;size+=extent};return size}
func tableRegionPackMeasure(root unsafe.Pointer,info *TableTypeInfo,a *TableArena)int64{n,_:=tableRegionPackMeasureReason(root,info,a);return n}
func tableRegionPackMeasureReason(root unsafe.Pointer,info *TableTypeInfo,a *TableArena)(int64,TableRefuseReason){n,ok:=tableNumber(root,info,a);if !ok{return -1,n.reason};defer n.Release();size:=tablePackOffsets(&n);if size<0{return -1,TableRefuseInvalidValue};return size,TableRefuseOk}
func tableRegionPack(root unsafe.Pointer,info *TableTypeInfo,a *TableArena) ([]byte,[]byte,bool) {
 total:=tableRegionPackMeasure(root,info,a);if total<0{return nil,nil,false};n,ok:=tableNumber(root,info,a);if !ok{return nil,nil,false};defer n.Release();size:=tablePackOffsets(&n);if size<0||size!=total{return nil,nil,false};allocation:=a.Allocator.alloc(size);if allocation==nil{return nil,nil,false};region:=allocation[:size];clear(region);base:=unsafe.Pointer(&region[0]);for i:=range n.entries {e:=&n.entries[i];p:=unsafe.Add(base,e.offset);if e.info==nil {copy(unsafe.Slice((*byte)(p),8+int(uint64(*(*uint32)(e.node)))),unsafe.Slice((*byte)(e.node),8+int(uint64(*(*uint32)(e.node)))));continue};copy(unsafe.Slice((*byte)(p),int(e.info.Size)),unsafe.Slice((*byte)(e.node),int(e.info.Size)));if !tableRegionEdges(p,e.info,func(slot *TableRef,f *TableFieldInfo) bool {if *slot==0{return true};target:=a.At(*slot);idx,ok:=n.find(target);if !ok{return false};*slot=int64(uintptr(unsafe.Add(base,n.entries[idx].offset))-uintptr(unsafe.Pointer(slot)));return true}){a.Allocator.free(allocation);return nil,nil,false} };return region,allocation,true
}
`

const tableRegionJSONSource = `
func tableRegionFromJSON(value unsafe.Pointer,info *TableTypeInfo,worker *TableWorker,text []byte,report *TableReport,options TableJSONOptions) bool {
 if report==nil {var ignored TableReport;report=&ignored};if value==nil {report.Malformed=true;return false}
 graph:=tableJSONGraph{alloc:worker.Alloc,allocate:worker.Arena.Allocator.alloc,release:worker.Arena.Allocator.free};defer graph.close()
 in:=tableJSONIn{text:text,report:report,graph:&graph,nullEmpty:options.NullAsEmpty};info.Reset(value);in.byteOrderMark();ok:=tableJSONReadTable(&in,value,info,0);in.space();if !ok||in.bad||in.pos!=len(text){report.Malformed=true;return false};return true
}
// The text writer refuses a blob named from two slots by name, blob_shared:
// there is no spelling to owe, and writing the value at each slot is the
// amplification the standard refuses (docs/SPEC-TABLES.md §2.5, §16.7).
func tableRegionToJSON(value unsafe.Pointer,info *TableTypeInfo,buffer []byte,options TableJSONOptions,allocator ...TableAllocator) (int64,TableRefuseReason) {
 n,ok:=tableNumber(value,info,nil,allocator...);if !ok{return -1,n.reason};defer n.Release();if n.shared{return -1,TableRefuseBlobShared};graph:=tableJSONGraph{node:func(p unsafe.Pointer)*tableJSONGraphNode{i,ok:=n.find(p);if !ok{return nil};return &n.entries[i].json}}
 for i:=range n.entries {e:=&n.entries[i];if e.info!=nil&&!tableRegionEdges(e.node,e.info,func(slot *TableRef,f *TableFieldInfo) bool {p:=tableRefAt(slot,nil);if p!=nil {graph.node(p).refs++};return true}){return -1,TableRefuseInvalidValue}}
 out:=tableJSONOut{buffer:buffer,measure:buffer==nil,graph:&graph,omit:options.OmitDefaults,compact:options.Compact};if !tableJSONWriteValue(&out,value,info,0){return -1,TableRefuseInvalidValue};out.put('\n');if out.overflow{return -1,TableRefuseInvalidValue};return out.offset,TableRefuseOk
}
`

const tableRegionBlobSource = `
// Blob ABI follows the established tool and C++ byte contract: u32 length, u32 zero.
// SPEC-TABLES §7.2 currently labels this u64; the big-endian fixtures distinguish them.
type TableBlob struct { Length uint32; Zero uint32 }
// TableBytesView borrows its backing region or arena. Keep that storage alive
// and do not free, reuse, or shut it down while using the view.
type TableBytesView struct { Data []byte; Length int64 }
// TableStringView has the same borrowed lifetime as TableBytesView.
type TableStringView struct { Data []byte; Length int64 }
func TableBlobAt(slot *TableRef,arena ...*TableArena) *TableBlob {return (*TableBlob)(tableRefAt(slot,tableOptionalArena(arena)))}
func TableBytesAt(slot *TableRef,arena ...*TableArena) TableBytesView {p:=TableBlobAt(slot,arena...);if p==nil{return TableBytesView{}};return TableBytesView{unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(p),8)),int(p.Length)),int64(p.Length)}}
func TableStringAt(slot *TableRef,arena ...*TableArena) TableStringView {v:=TableBytesAt(slot,arena...);return TableStringView{v.Data,v.Length}}
func (w *TableWorker) allocBlob(length int64,terminated bool)([]byte,TableRef){if length<0||length>0xffffffff{return nil,0};extra:=int64(0);if terminated{extra=1};p,ref:=w.Alloc(8+length+extra);if p==nil{return nil,0};*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(length))};return unsafe.Slice((*byte)(unsafe.Add(p,8)),int(length)),ref}
func (w *TableWorker) AllocBytes(length int64)([]byte,TableRef){return w.allocBlob(length,false)}
func (w *TableWorker) AllocString(length int64)([]byte,TableRef){return w.allocBlob(length,true)}
func TableBytesEmplace(worker *TableWorker,slot *TableRef,length int64) []byte {data,ref:=worker.AllocBytes(length);*slot=ref;return data}
func TableStringEmplace(worker *TableWorker,slot *TableRef,text []byte) []byte {data,ref:=worker.AllocString(int64(len(text)));*slot=ref;copy(data,text);return data}
`

// Builder loading resolves the complete flat directory before decoding any
// body, preserving forward references and sharing. Temporary metadata uses
// the builder's allocator and never becomes part of the packed region.
const tableBuilderLoadSource = `
func tableBuilderLoad(p unsafe.Pointer,ref TableRef,worker *TableWorker,wire []byte,root *TableTypeInfo,nodeType func(uint64)*TableTypeInfo,blobs int,report *TableReport) bool {
 if report==nil {var ignored TableReport;report=&ignored};r,verdict:=tableOpenFramed(wire,report);report.Verdict=verdict;if verdict!=TableOpenOk {if verdict==TableOpenDamaged {report.Malformed=true}else{report.Reason="newer_form";if len(wire)>0{if wire[0]==2{report.Reason="message_form_as_file"}else if wire[0]==0{report.Reason="previous_form"}}};return false};if p==nil||worker==nil||worker.Arena==nil||worker.Arena.Locked {report.Malformed=true;return false};worker.refused=false // docs/SPEC-TABLES.md §3 (THE FIRST BYTE), §6.5
 scan:=tableNodeScanBegin(r);for {_,_,ok:=scan.next();if !ok{break}};count:=int64(scan.records+1);memory:=worker.Arena.Allocator.alloc(count*16);if memory==nil {report.Malformed=true;return false};defer worker.Arena.Allocator.free(memory);clear(memory);directory:=unsafe.Slice((*TableNodeDirEntry)(unsafe.Pointer(&memory[0])),int(count));directory[0]=TableNodeDirEntry{uint64(ref),root.ID};nodes:=TableNodeMap{Entries:directory,Arena:true};nodes.Once=true
 scan=tableNodeScanBegin(r);k:=1;unknown:=int32(0)
 for {id,body,ok:=scan.next();if !ok{break};directory[k]=TableNodeDirEntry{^uint64(0),id};t:=nodeType(id);isblob:=id==tableBytesTypeID&&blobs&1!=0||id==tableStringTypeID&&blobs&2!=0
 if t==nil&&!isblob {unknown++;k++;continue};if id==tableStringTypeID&&!tableUTF8Valid(body.Buffer) {report.Malformed=true;k++;continue};size:=int64(0);if t!=nil {size=int64(t.Size)}else{if uint64(len(body.Buffer))>math.MaxUint32{return false};size=8+int64(len(body.Buffer));if id==tableStringTypeID{size++}};node,at:=worker.Alloc(size);if node==nil {report.Malformed=true;return false};directory[k].Offset=uint64(at);if t!=nil{t.Reset(node)}else{*(*TableBlob)(node)=TableBlob{Length:uint32(uint64(len(body.Buffer)))};copy(unsafe.Slice((*byte)(unsafe.Add(node,8)),len(body.Buffer)),body.Buffer)};k++ }
 nodes.Good=scan.whole();if nodes.Good{report.Unknown+=unknown}else{report.Malformed=true}
 if nodes.Good {scan=tableNodeScanBegin(r);k=1;for {id,body,ok:=scan.next();if !ok{break};if directory[k].Offset!=^uint64(0) {if t:=nodeType(id);t!=nil {body.Nodes=nodes;t.LoadBody(body,worker.Arena.At(int64(directory[k].Offset)));if worker.refused{return false}}};k++}}
 r.Nodes=nodes;r.Nested=false;ok:=root.LoadBody(r,p);if tableNonFiniteRefused(report){return false};return ok&&!worker.refused
}
`
