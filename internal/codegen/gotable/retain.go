package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"regexp"
)

// Retention has its own readers, writers and id interner. The ordinary family
// carries no path, scratch records or branches for this opt-in.
func (g *tableGen) emitRetainRuntime() {
	g.pf("const tableRetainPathDepth = %d\n", retainDepth(g.unit))
	g.pf("var tableRetainKnown = [...]uint64{")
	for _, id := range ir.TableWireIds(g.unit) {
		g.pf("0x%016x,", id)
	}
	g.pf("}\n%s", goRetainRuntime)
	g.emitRetainMessageRuntime()
	source := tableSourceSpan(tableWireSource, "type TableWriter struct", "// TableOpenVerdict")
	source = tableSourceReplace(source, "TableWriter", "TableRetainWriter", -1)
	source = tableSourceReplace(source, "TableIds", "TableRetainIds", -1)
	start := tableSourceIndex(source, "func (w *TableRetainWriter) Trailer()")
	source = source[:start] + "func (w *TableRetainWriter) Trailer(){w.Ids.trailer(w)}\n"
	g.pf("%s", source)
	load := tableRegionSource
	if unitHasContainers(g.unit) {
		load = containerRegionSource(load)
	}
	load = tableSourceFunction(load, "func tableRegionLoad(")
	load = tableSourceReplace(load, "func tableRegionLoad(", "func tableRegionLoadRetain(", 1)
	load = tableSourceReplace(load, "report *TableReport) unsafe.Pointer {", "report *TableReport,retain *TableRetain) unsafe.Pointer {\nretain.reset(nil,nil)", 1)
	load = tableSourceReplace(load, "root.Reset(base)", "root.Reset(base);retain.reset(base,directory)", 1)
	load = tableSourceReplace(load, "report.Unknown+=unknown", "report.Unknown+=unknown;if retain!=nil{report.RetainLost+=unknown}", 1)
	load = tableSourceReplace(load, "t.LoadBody(body,p)", "t.LoadBodyRetain(TableRetainReader{tableRetainPlainReader:body,Retain:retain,Path:tableRetainPath{at:p,node:uint32(k+1)}},p)", 1)
	load = tableSourceReplace(load, "root.LoadBody(r,base)", "root.LoadBodyRetain(TableRetainReader{tableRetainPlainReader:r,Retain:retain,Path:tableRetainPath{at:base,node:1}},base)", 1)
	g.pf("%s", load)
	save := tableRegionSource
	save = tableSourceFunction(save, "func tableRegionSaveReason(")
	save = tableSourceReplace(save, "return -1,frame.numbering.reason", "return -1", 1)
	save = tableSourceReplace(save, "return -1,TableRefuseInvalidValue", "return -1", -1)
	save = tableSourceReplace(save, "return w.Offset,TableRefuseOk", "return w.Offset", 1)
	save = tableSourceReplace(save, "a *TableArena,allocator ...TableAllocator) (int64,TableRefuseReason) {", "retain *TableRetain,report *TableReport,allocator ...TableAllocator) int64 {\nif retain!=nil{retain.idUsed=0;retain.clearPlaced()}", 1)
	save = tableSourceReplace(save, "tableNumber(root,info,a,allocator...)", "tableNumber(root,info,nil,allocator...)", 1)
	save = tableSourceReplace(save, "*ids=TableIds{Numbering:n}", "*ids=TableRetainIds{Numbering:n,Retain:retain,Path:tableRetainPath{at:root,node:1}}", 1)
	save = tableSourceRewriter(save, "tableRegionSaveReason(", "tableRegionSaveRetain(", "tableWireFrame", "tableRetainFrame", "TableWriter", "TableRetainWriter", "info.SaveBody(", "info.SaveBodyRetain(", "tableNodeMeasure(", "tableNodeMeasureRetain(", "tableNodeWrite(", "tableNodeWriteRetain(")
	save = tableSourceReplace(save, "return w.Offset", "if !measure{retain.countLost(report)};return w.Offset", 1)
	g.pf("%s", save)
	g.pf("func tableNodeWriteRetain(w *TableRetainWriter,e *tableNodeEntry)bool{if e.info!=nil{path:=w.Ids.Path;w.Ids.Path=tableRetainPath{at:e.node};ok:=e.info.SaveBodyRetain(w,e.node);w.Ids.Path=path;return ok};length:=(*TableBlob)(e.node).Length;w.Raw(unsafe.Slice((*byte)(unsafe.Add(e.node,8)),int(length)));return !w.Overflow}\n")
	g.pf("func tableNodeMeasureRetain(w *TableRetainWriter,ids *TableRetainIds,e *tableNodeEntry)int64{*w=TableRetainWriter{Measuring:true,Ids:ids};if !tableNodeWriteRetain(w,e){return -1};return w.Offset}\n")
	if unitHasContainers(g.unit) {
		g.pf("func(r *TableRetainReader)Take(sub TableRetainReader){r.tableRetainPlainReader.Take(sub.tableRetainPlainReader)}\n")
	}
}

func tableRuntimeForUnit(regional bool) string {
	s := tableRuntime()
	if regional {
		s = tableSourceReplace(s, "type TableTypeInfo struct {", "type TableTypeInfo struct {\nSaveBodyRetain func(*TableRetainWriter,unsafe.Pointer)bool\nLoadBodyRetain func(TableRetainReader,unsafe.Pointer)bool\nLoadMessageBodyRetain func(TableRetainMessageReader,unsafe.Pointer)(TableRetainMessageReader,bool)", 1)
	}
	return s
}

func retainDepth(u *ir.Unit) int {
	memo := map[string]int{}
	var record func(*ir.Struct) int
	var field func(*ir.Field) int
	field = func(f *ir.Field) int {
		if f.Type.Pointer {
			return 0
		}
		if f.IsMap() {
			return 1 + record(f.MapEntry)
		}
		switch t := f.Type.Ref.(type) {
		case *ir.Struct:
			return 1 + record(t)
		case *ir.Union:
			best := 0
			for _, v := range t.Variants {
				if v.F != nil {
					best = max(best, field(v.F)+2)
				}
			}
			return best
		}
		return 0
	}
	record = func(t *ir.Struct) int {
		if n, ok := memo[t.Name]; ok {
			return n
		}
		memo[t.Name] = 0
		n := 0
		for _, f := range t.Fields {
			n = max(n, field(f))
		}
		memo[t.Name] = n
		return n
	}
	n := 1
	for _, t := range u.Tables {
		n = max(n, record(t))
	}
	return n
}

var retainBodyName = regexp.MustCompile(`\b([A-Za-z_][A-Za-z_0-9]*)(LoadMessageBody|LoadBody|MeasureBody|SaveBodyFields|SaveBody)\b`)

func (g *tableGen) emitRetainMember(st *ir.Struct) {
	if st.IsTable && !st.IsMapEntry() {
		for _, verb := range []string{"LoadRetainBuilder", "SaveRetainMessages"} {
			g.pf("const %s%s = %q\n", st.Name, verb, st.Name+": retention requires a region round trip through the VARIABLE form")
		}
		if !ir.VariableTables(g.unit)[st.Name] {
			for _, verb := range []string{"LoadRetain", "MeasureRetain", "SaveRetain"} {
				g.pf("const %s%s = %q\n", st.Name, verb, st.Name+": retention requires a variable root and its region directory")
			}
		} else {
			n := st.Name
			typ := g.storageName(n)
			blobs := 0
			a, b := ir.PointerReachableBlobs(st)
			if a {
				blobs |= 1
			}
			if b {
				blobs |= 2
			}
			g.pf("func %sLoadRetain(region,wire []byte,retain *TableRetain,report *TableReport)*%s{return (*%s)(tableRegionLoadRetain(region,wire,%sTableType(),%sTableType().NodeType,%d,report,retain))}\n", n, typ, typ, n, n, blobs)
			g.pf("func %sMeasureRetain(value *%s,retain *TableRetain,allocator ...TableAllocator)int64{return tableRegionSaveRetain(unsafe.Pointer(value),%sTableType(),nil,true,retain,nil,allocator...)}\n", n, typ, n)
			g.pf("func %sSaveRetain(value *%s,retain *TableRetain,buffer []byte,report *TableReport,allocator ...TableAllocator)int64{if report==nil{return -1};return tableRegionSaveRetain(unsafe.Pointer(value),%sTableType(),buffer,false,retain,report,allocator...)}\n", n, typ, n)
		}
	}
	g.emitRetainMessageSurface(st)
	if !g.regional {
		return
	}
	side := &tableGen{unit: g.unit, file: g.file, owner: st, regional: true, retain: true, unionArmSlot: g.unionArmSlot, idOrdinal: g.idOrdinal}
	side.emitTableMeasure(st)
	side.emitTableWrite(st)
	side.emitTableRead(st)
	side.emitMessageRead(st)
	source := side.body.String()
	if !retainBodyName.MatchString(source) {
		panic("retaining body family has no source codecs")
	}
	source = retainBodyName.ReplaceAllString(source, "${1}${2}Retain")
	source = tableSourceRewriter(source, "TableMessageReader", "TableRetainMessageReader", "TableWriter", "TableRetainWriter", "TableReader", "TableRetainReader", "TableIds", "TableRetainIds")
	g.pf("%s", source)
	g.needsMath = g.needsMath || side.needsMath
	g.unsafeUsed = true
}

func (g *tableGen) retainReadPath(child, parent, index string) {
	if g.retain {
		g.pf("%s.Path=%s.Path.step(%d,uint32(%s))\n", child, parent, g.retainOrdinal, index)
	}
}
func (g *tableGen) retainDiscard(reader string) {
	if g.retain {
		g.pf("%s.Retain.discard(%s.Path,true,%d)\n", reader, reader, g.retainOrdinal)
	}
}

func (g *tableGen) retainWritePush(writer, index string) string {
	if index == "" {
		index = "0"
	}
	name := g.nextWireWriter()
	g.pf("{ %s := %s.Ids.Path;%s.Ids.Path=%s.step(%d,uint32(%s))\n", name, writer, writer, name, g.retainOrdinal, index)
	return name
}
func (g *tableGen) retainWritePop(writer, name string) { g.pf("%s.Ids.Path=%s }\n", writer, name) }

const goRetainRuntime = `
// TableRetain owns no storage. The caller supplies both bounded buffers; a
// whole unknown record that does not fit is dropped and counted once.
// Maximum nesting of a retained resolved payload (SPEC-TABLES §6.6).
const tableRetainWalkDepthMax = 64
type TableRetainId struct { Id uint64; Slot int }
type TableRetain struct { Bytes []byte; Ids []TableRetainId; Used int64; Count int; idUsed int; base unsafe.Pointer; directory []TableNodeDirEntry }
type tableRetainStep struct {ordinal,index uint32}
type tableRetainPath struct {at unsafe.Pointer;node uint32;depth int;steps [tableRetainPathDepth]tableRetainStep}
func(p tableRetainPath)step(ordinal,index uint32)tableRetainPath{if p.depth<len(p.steps){p.steps[p.depth]=tableRetainStep{ordinal,index}};p.depth++;return p}
func(t *TableRetain)reset(base unsafe.Pointer,directory []TableNodeDirEntry){if t!=nil{t.Used=0;t.Count=0;t.idUsed=0;t.base=base;t.directory=directory}}
func tableRetainRecordSize(b []byte)int{return int(binary.LittleEndian.Uint32(b))}
func(t *TableRetain)matches(b []byte,p tableRetainPath,under bool,field bool,ordinal uint32)bool{
 node:=binary.LittleEndian.Uint32(b[4:]);depth:=int(binary.LittleEndian.Uint32(b[8:]));if node==0||int(node)>len(t.directory)||t.directory[node-1].Offset==^uint64(0)||unsafe.Add(t.base,t.directory[node-1].Offset)!=p.at{return false}
 if depth<p.depth||!under&&depth!=p.depth||field&&depth==p.depth||p.depth>len(p.steps){return false}
 for i:=0;i<p.depth;i++{if binary.LittleEndian.Uint32(b[26+i*8:])!=p.steps[i].ordinal||binary.LittleEndian.Uint32(b[30+i*8:])!=p.steps[i].index{return false}}
 return !field||binary.LittleEndian.Uint32(b[26+p.depth*8:])==ordinal
}
func(t *TableRetain)discard(p tableRetainPath,field bool,ordinal uint32){if t==nil{return};at,out,count:=0,0,0;for at<int(t.Used){n:=tableRetainRecordSize(t.Bytes[at:]);b:=t.Bytes[at:at+n];if !t.matches(b,p,true,field,ordinal){copy(t.Bytes[out:],b);out+=n;count++};at+=n};t.Used=int64(out);t.Count=count}
func(t *TableRetain)clearPlaced(){if t==nil{return};for at:=0;at<int(t.Used);{t.Bytes[at+25]=0;at+=tableRetainRecordSize(t.Bytes[at:])}}
func(t *TableRetain)countLost(r *TableReport){if t==nil||r==nil{return};for at:=0;at<int(t.Used);{if t.Bytes[at+25]==0{r.RetainLost++};at+=tableRetainRecordSize(t.Bytes[at:])}}

type tableRetainPlainReader = TableReader
type TableRetainReader struct {tableRetainPlainReader;Retain *TableRetain;Path tableRetainPath}
func(r *TableRetainReader)Body()(TableRetainReader,bool){sub,ok:=r.tableRetainPlainReader.Body();return TableRetainReader{tableRetainPlainReader:sub,Retain:r.Retain,Path:r.Path},ok}
func(r *TableRetainReader)capture(id uint64,kind uint8)bool{
 at:=r.Offset;if !r.Skip(kind){return false};if r.Retain==nil{return true};t:=r.Retain;p:=r.Path
 limit:=int64(len(t.Bytes))-t.Used-int64(26+p.depth*8);if limit<0{r.Report.RetainLost++;return true};probe:=tableRetainIn{r:TableReader{Buffer:r.Buffer[at:r.Offset],Ids:r.Ids},w:TableWriter{Measuring:true},limit:limit}
 if p.depth>len(p.steps)||!probe.payload(kind,0)||probe.r.Offset!=int64(len(probe.r.Buffer)){r.Report.RetainLost++;return true}
 need:=int64(26+p.depth*8)+probe.w.Offset;if need>0xffffffff||need>int64(len(t.Bytes))-t.Used{r.Report.RetainLost++;return true}
 b:=t.Bytes[t.Used:t.Used+need];clear(b[:26]);binary.LittleEndian.PutUint32(b,uint32(need));binary.LittleEndian.PutUint32(b[4:],p.node);binary.LittleEndian.PutUint32(b[8:],uint32(p.depth));binary.LittleEndian.PutUint32(b[12:],uint32(probe.w.Offset));binary.LittleEndian.PutUint64(b[16:],id);b[24]=kind
 for i:=0;i<p.depth;i++{binary.LittleEndian.PutUint32(b[26+8*i:],p.steps[i].ordinal);binary.LittleEndian.PutUint32(b[30+8*i:],p.steps[i].index)}
 write:=tableRetainIn{r:probe.r,w:TableWriter{Buffer:b[26+p.depth*8:]},limit:limit};write.r.Offset=0;if !write.payload(kind,0)||write.w.Overflow{r.Report.RetainLost++;return true};t.Used+=need;t.Count++;r.Report.Retained++;return true
}

type tableRetainIn struct {r TableReader;w TableWriter;limit int64}
func(s *tableRetainIn)room(n int64)bool{return n>=0&&n<=s.limit-s.w.Offset}
func tableRetainFileMinimum(kind uint8)int64{if n:=tableKindBytes(kind);n!=0{return n};switch kind{case 13:return 16;case 14,16:return 10;case 15,30:return 8;case 12,31,32,33:return 1};return 0}
func(s *tableRetainIn)ref(zero bool)(uint64,bool){if !s.room(8){return 0,false};v,ok:=s.r.Leb();if !ok{return 0,false};if v==0{if !zero{return 0,false};s.w.Put64(0);return 0,true};id,ok:=s.r.Resolve(v);if !ok||id==0||id>=0xfffffffffffffffd{return 0,false};s.w.Put64(id);return id,true}
func(s *tableRetainIn)raw(n int64)bool{if !s.room(n)||!s.r.Has(n){return false};s.w.Raw(s.r.Buffer[s.r.Offset:s.r.Offset+n]);s.r.Offset+=n;return !s.w.Overflow}
func(s *tableRetainIn)framed(kind uint8,n int64,depth int)bool{if !s.room(8){return false};at:=s.w.Offset;s.w.Put64(0);start:=s.w.Offset;if !s.content(kind,n,depth)||s.w.Offset-start>0xffffffff{return false};if !s.w.Measuring{binary.LittleEndian.PutUint32(s.w.Buffer[at:],uint32(s.w.Offset-start))};return true}
func(s *tableRetainIn)content(kind uint8,n int64,depth int)bool{
 if depth>tableRetainWalkDepthMax||!s.r.Has(n){return false};end:=s.r.Offset+n
 switch kind {
 case 13:for{ref,ok:=s.ref(true);if !ok{return false};if ref==0{break};if !s.room(1)||!s.r.Has(1){return false};k:=s.r.Get8();s.w.Put8(k);if !s.payload(k,depth)||s.r.Offset>end{return false}}
 case 14,16:if !s.room(1)||!s.r.Has(1){return false};k:=s.r.Get8();s.w.Put8(k);count,ok:=s.r.Leb();if !ok||!s.room(tableLebBytes(count)){return false};s.w.PutLeb(count);minimum:=tableRetainFileMinimum(k);if kind==16{minimum=16};if minimum<=0||count>uint64(max(int64(0),s.limit-s.w.Offset)/minimum)||count>uint64(max(int64(0),end-s.r.Offset)){return false};for i:=uint64(0);i<count;i++{if kind==16{if _,ok:=s.ref(false);!ok{return false};length,ok:=s.r.Leb();if !ok||length>uint64(end-s.r.Offset)||!s.framed(k,int64(length),depth+1){return false}}else if !s.payload(k,depth){return false};if s.r.Offset>end{return false}}
 case 15,30:if !s.payload(kind,depth){return false}
 case 17:return false
 default:if !s.raw(n){return false}
 };return s.r.Offset==end
}
func(s *tableRetainIn)payload(kind uint8,depth int)bool{
 if width:=tableKindBytes(kind);width!=0{return s.raw(width)}
 switch kind{
 case 12,31,32,33:n,ok:=s.r.Leb();if !ok||!s.r.Room(n)||!s.room(tableLebBytes(n)){return false};s.w.PutLeb(n);return s.raw(int64(n))
 case 13,14,16:n,ok:=s.r.Leb();return ok&&s.r.Room(n)&&s.framed(kind,int64(n),depth+1)
 case 15:id,ok:=s.ref(true);if !ok{return false};if id==0{return true};if !s.room(1)||!s.r.Has(1){return false};k:=s.r.Get8();s.w.Put8(k);n,ok:=s.r.Leb();return ok&&s.r.Room(n)&&s.framed(k,int64(n),depth+1)
 case 30:_,ok:=s.ref(true);return ok
 };return false
}

type tableRetainFrame struct {numbering TableNumbering;ids TableRetainIds;writer,measure TableRetainWriter}
type TableRetainIds struct {known TableIds;slots [tableIdCapacity]int;Numbering *TableNumbering;Retain *TableRetain;Path tableRetainPath;Count int;Overflow,Lost bool}
func(i *TableRetainIds)Ref(id uint64)uint64{before:=i.known.Count;ref:=i.known.Ref(id);if i.known.Overflow{i.Overflow=true;return 0};if i.known.Count!=before{i.Count++;i.slots[ref-1]=i.Count};return uint64(i.slots[ref-1])}
func(i *TableRetainIds)RefAt(ordinal int,id uint64)uint64{before:=i.known.Count;ref:=i.known.RefAt(ordinal,id);if i.known.Overflow{i.Overflow=true;return 0};if i.known.Count!=before{i.Count++;i.slots[ref-1]=i.Count};return uint64(i.slots[ref-1])}
func(i *TableRetainIds)refAtHit(ordinal int,id uint64)uint64{before:=i.known.Count;ref:=i.known.refAtHit(ordinal,id);if i.known.Overflow{i.Overflow=true;return 0};if i.known.Count!=before{i.Count++;i.slots[ref-1]=i.Count};return uint64(i.slots[ref-1])}
func(i *TableRetainIds)recordRef(id uint64)uint64{lo,hi:=0,len(tableRetainKnown);for lo<hi{m:=lo+(hi-lo)/2;if tableRetainKnown[m]<id{lo=m+1}else{hi=m}};if lo<len(tableRetainKnown)&&tableRetainKnown[lo]==id{return i.Ref(id)};t:=i.Retain;if t==nil{i.Lost=true;return 0};for k:=0;k<t.idUsed;k++{if t.Ids[k].Id==id{return uint64(t.Ids[k].Slot)}};if t.idUsed==len(t.Ids){i.Lost=true;return 0};i.Count++;t.Ids[t.idUsed]=TableRetainId{id,i.Count};t.idUsed++;return uint64(i.Count)}
func(i *TableRetainIds)Truncate(mark int){for i.known.Count>0&&i.slots[i.known.Count-1]>mark{i.known.Truncate(i.known.Count-1)};if t:=i.Retain;t!=nil{for t.idUsed>0&&t.Ids[t.idUsed-1].Slot>mark{t.idUsed--}};i.Count=mark}
func(i *TableRetainIds)trailer(w *TableRetainWriter){a,b,count:=0,0,0;if i.Retain!=nil{count=i.Retain.idUsed};for a<i.known.Count||b<count{if b==count||a<i.known.Count&&i.slots[a]<i.Retain.Ids[b].Slot{w.Put64(i.known.Values[a]);a++}else{w.Put64(i.Retain.Ids[b].Id);b++}};w.Put64(uint64(i.Count))}

type tableRetainOut struct {r TableReader;w *TableRetainWriter;ids *TableRetainIds;size int64}
func(s *tableRetainOut)raw(n int64)bool{if !s.r.Has(n){return false};if s.w!=nil{s.w.Raw(s.r.Buffer[s.r.Offset:s.r.Offset+n])};s.r.Offset+=n;s.size+=n;return true}
func(s *tableRetainOut)leb(v uint64){if s.w!=nil{s.w.PutLeb(v)};s.size+=tableLebBytes(v)}
func(s *tableRetainOut)ref()(uint64,bool){if !s.r.Has(8){return 0,false};id:=s.r.Get64();if id==0{s.leb(0);return 0,true};ref:=s.ids.recordRef(id);if s.ids.Lost||s.ids.Overflow{return id,false};s.leb(ref);return id,true}
func(s *tableRetainOut)framed(kind uint8,depth int)bool{if !s.r.Has(8){return false};at:=s.r.Offset;n:=s.r.Get32();wire:=s.r.Get32();if s.w==nil{before:=s.size;if !s.content(kind,int64(n),depth)||s.size-before>0xffffffff{return false};wire=uint32(s.size-before);binary.LittleEndian.PutUint32(s.r.Buffer[at+4:],wire);s.size+=tableLebBytes(uint64(wire));return true};s.leb(uint64(wire));before:=s.size;return s.content(kind,int64(n),depth)&&s.size-before==int64(wire)}
func(s *tableRetainOut)content(kind uint8,n int64,depth int)bool{
 if depth>tableRetainWalkDepthMax||!s.r.Has(n){return false};end:=s.r.Offset+n
 switch kind{
 case 13:for{id,ok:=s.ref();if !ok{return false};if id==0{break};if !s.r.Has(1){return false};k:=s.r.Buffer[s.r.Offset];if !s.raw(1)||!s.payload(k,depth)||s.r.Offset>end{return false}}
 case 14,16:if !s.r.Has(1){return false};k:=s.r.Buffer[s.r.Offset];if !s.raw(1){return false};count,ok:=s.r.Leb();if !ok{return false};s.leb(count);for i:=uint64(0);i<count;i++{if kind==16{if _,ok:=s.ref();!ok||!s.framed(k,depth+1){return false}}else if !s.payload(k,depth){return false};if s.r.Offset>end{return false}}
 case 15,30:if !s.payload(kind,depth){return false}
 default:if !s.raw(n){return false}
 };return s.r.Offset==end
}
func(s *tableRetainOut)payload(kind uint8,depth int)bool{if n:=tableKindBytes(kind);n!=0{return s.raw(n)};switch kind{
 case 12,31,32,33:n,ok:=s.r.Leb();if !ok||!s.r.Room(n){return false};s.leb(n);return s.raw(int64(n))
 case 13,14,16:return s.framed(kind,depth+1)
 case 15:id,ok:=s.ref();if !ok{return false};if id==0{return true};if !s.r.Has(1){return false};k:=s.r.Buffer[s.r.Offset];return s.raw(1)&&s.framed(k,depth+1)
 case 30:_,ok:=s.ref();return ok
 };return false}
func tableRetainTail(w *TableRetainWriter)bool{
 ids:=w.Ids;t:=ids.Retain;if t==nil{return true};for at:=0;at<int(t.Used);{n:=tableRetainRecordSize(t.Bytes[at:]);b:=t.Bytes[at:at+n];at+=n;if !t.matches(b,ids.Path,false,false,0){continue};mark:=ids.Count;ids.Lost=false;ref:=ids.recordRef(binary.LittleEndian.Uint64(b[16:]));depth:=int(binary.LittleEndian.Uint32(b[8:]));payload:=b[26+8*depth:];probe:=tableRetainOut{r:TableReader{Buffer:payload},ids:ids}
 if ids.Lost||!probe.payload(b[24],0)||probe.r.Offset!=int64(len(payload)){ids.Truncate(mark);ids.Lost=false;continue}
 if w.Measuring{w.Advance(tableLebBytes(ref)+1+probe.size);continue};ids.Truncate(mark);w.PutLeb(ids.recordRef(binary.LittleEndian.Uint64(b[16:])));w.Put8(b[24]);out:=tableRetainOut{r:TableReader{Buffer:payload},ids:ids,w:w};if !out.payload(b[24],0)||out.size!=probe.size{return false};b[25]=1
 };return !w.Overflow&&!ids.Overflow
}
`
