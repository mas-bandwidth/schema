package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

func unitHasContainers(u *ir.Unit) bool {
	return len(ir.ListFields(u)) != 0 || len(ir.MapFields(u)) != 0
}
func containerElementType(u *ir.Unit, f *ir.Field) string {
	if f.IsMap() {
		return f.MapEntry.Name + "Row"
	}
	return goCookBlittableType(u, f.Type)
}
func (g *tableGen) emitListSurface(st *ir.Struct, f *ir.Field) {
	name := st.Name + ir.GoExportName(f.Name)
	typ := containerElementType(g.unit, f)
	holder := "*TableList[" + typ + "]"
	g.pf("func %sAdd(worker *TableWorker,holder %s)*%s {p:=(*%s)(tableContainerAppend(worker,(*tableContainer)(unsafe.Pointer(holder)),int64(unsafe.Sizeof(*new(%s)))));if p==nil{return nil};", name, holder, typ, typ, typ)
	if isStructRef(f.Type) && !f.Type.Pointer {
		g.pf("%sReset(p);", f.Type.Name)
	}
	g.pf("return p}\n")
	g.pf("func %sErase(arena *TableArena,holder %s,element *%s)bool {return tableContainerErase(arena,(*tableContainer)(unsafe.Pointer(holder)),unsafe.Pointer(element),int64(unsafe.Sizeof(*element)))}\n", name, holder, typ)
	g.pf("func %sEach(holder %s,visit func(*%s)bool,arena ...*TableArena)bool {cursor:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(holder)),int64(unsafe.Sizeof(*new(%s))),tableOptionalArena(arena));for i:=int32(0);i<holder.Count;i++ {p:=(*%s)(cursor.Next());if p==nil||!visit(p){return false}};return true}\n", name, holder, typ, typ, typ)
}

func (g *tableGen) emitListRead(f *ir.Field, ind string) {
	expr := "value." + member(f)
	typ := containerElementType(g.unit, f)
	kind := ir.TableWireElemKind(f)
	_, align := ir.ListElementLayout(g.unit, f)
	g.pf("%ssub,ok:=r.Body();if !ok {r.Report.Malformed=true;return false};if len(sub.Buffer)>=2 {\n", ind)
	i := ind + "\t"
	g.pf("%sheader:=sub;header.Buffer=r.Buffer[r.Offset-int64(len(sub.Buffer)):];elemKind:=header.Get8();count,ok:=header.Leb();sub.Offset=header.Offset\n", i)
	g.pf("%sif !ok {r.Report.Malformed=true;break};if elemKind!=%d { if !tableKindWidens(elemKind,%d) {r.Report.KindMismatch++;break};r.Report.Widened++ }\n", i, kind, kind)
	g.retainDiscard("r")
	g.pf("%sslot:=(*tableContainer)(unsafe.Pointer(&%s));if !tableContainerFill(&sub.Nodes.Carve,slot,count,int64(unsafe.Sizeof(*new(%s))),%d) {if sub.Nodes.Carve.Refused {r.Take(sub);return false};r.Report.Malformed=true;break};slot.Count=0\n", i, expr, typ, align)
	g.pf("%sfor i:=uint64(0);i<count;i++ { p:=(*%s)(tableContainerFillAt(&sub.Nodes.Carve,slot,int32(i),int64(unsafe.Sizeof(*new(%s)))));if p==nil {r.Report.Malformed=true;break};landed:=false;for once:=true;once;once=false {\n", i, typ, typ)
	j := i + "\t"
	switch kind {
	case tkTable:
		g.pf("%selem,ok:=sub.Body();if !ok {r.Report.Malformed=true;break};\n", j)
		g.retainReadPath("elem", "r", "i")
		g.pf("%sLoadBody(&elem,p);\n", f.Type.Name)
		g.emitCarveReturn("sub", "elem", j)
		g.pf("%sif elem.Offset!=int64(len(elem.Buffer)) {r.Report.Malformed=true;%sReset(p)}\n", j, f.Type.Name)
	case tkUnion:
		g.retainReadPath("sub", "r", "i")
		g.emitReadUnion(f.Type.Ref.(*ir.Union), "(*p)", "sub", j, "break", true)
	default:
		g.emitReadScalar(f, "(*p)", "sub", "elemKind", j, "r.Report.Malformed=true;break")
	}
	g.pf("%slanded=true };if !landed { if sub.Nodes.Carve.Worker!=nil {tableContainerErase(sub.Nodes.Carve.Worker.Arena,slot,unsafe.Pointer(p),int64(unsafe.Sizeof(*p)))};break };if sub.Nodes.Carve.Worker==nil {slot.Count++}\n%s}\n", j, i)
	g.emitCarveReturn("r", "sub", i)
	g.pf("%s}\n", ind)
}
func (g *tableGen) emitListWrite(f *ir.Field, expr, writer, ind string, framed bool) {
	body := g.nextWireWriter()
	typ := containerElementType(g.unit, f)
	kind := ir.TableWireElemKind(f)
	g.pf("%s{\n%s mark:=%s.Ids.Count;%s:=TableWriter{Measuring:true,Ids:%s.Ids}\n", ind, ind, writer, body, writer)
	for pass := range 2 {
		w := body
		if pass == 1 {
			w = writer
		}
		if f.IsMap() {
			g.pf("%s cursor:=tableMapCursor((*tableContainer)(unsafe.Pointer(&%s)),%s,%s.Ids.Numbering.arena);defer cursor.Release()\n", ind, expr, g.mapDescriptor(f), writer)
		} else {
			g.pf("%s cursor:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(&%s)),int64(unsafe.Sizeof(*new(%s))),%s.Ids.Numbering.arena)\n", ind, expr, typ, writer)
		}
		g.pf("%s for i:=int32(0);i<%s.Count;i++ { p:=(*%s)(cursor.Next());if p==nil {return false};\n", ind, expr, typ)
		element := f
		if f.IsMap() {
			copy := *f
			copy.Type = ir.FieldType{Kind: ir.TNamed, Name: f.MapEntry.Name, Ref: f.MapEntry}
			copy.MapEntry = nil
			element = &copy
		}
		g.emitWireArrayElement(element, "(*p)", w, ind+"\t")
		g.pf("%s }\n", ind)
		if pass == 0 {
			g.pf("%s if %s.Overflow {return false};n:=int64(1)+tableLebBytes(uint64(%s.Count))+%s.Offset\n", ind, body, expr, body)
			if framed {
				g.pf("%s %s.PutLeb(uint64(n))\n", ind, writer)
			}
			g.pf("%s if %s.Measuring { %s.Advance(n) } else { %s.Ids.Truncate(mark);%s.Put8(%d);%s.PutLeb(uint64(%s.Count))\n", ind, writer, writer, writer, writer, kind, writer, expr)
		}
	}
	g.pf("%s }\n%s}\n", ind, ind)
}

// The holder is POD in both lives. Arena chunks never move, so appending
// cannot invalidate an element handle. Const storage is one contiguous array.
const tableContainerSource = `
type tableContainer struct { Ref int64; Count int32; _ int32 }
type TableList[T any] struct { Ref int64; Count int32; _ int32 }
func (l *TableList[T]) Clear() { *l=TableList[T]{} }
func (l *TableList[T]) At(index int32,arena ...*TableArena) *T {if l==nil||index<0||index>=l.Count{return nil};c:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(l)),int64(unsafe.Sizeof(*new(T))),tableOptionalArena(arena));return (*T)(c.At(index))}
type tableContainerHead struct { First,Last int64; Live int32; _ [12]byte }
type tableContainerChunk struct { Next int64; Count,Capacity int32; Dead uint64; _ uint64 }
type tableContainerCursor struct { order []int64; memory []byte; p unsafe.Pointer; chunk *tableContainerChunk; arena *TableArena; size int64; index int32 }
func tableContainerCursorBegin(h *tableContainer,size int64,a *TableArena) tableContainerCursor {c:=tableContainerCursor{arena:a,size:size};if h.Ref==0{return c};if a==nil {c.p=unsafe.Add(unsafe.Pointer(h),h.Ref)}else {head:=(*tableContainerHead)(a.At(h.Ref));if head!=nil && head.Live==h.Count {c.chunk=(*tableContainerChunk)(a.At(head.First))}};return c}
func (c *tableContainerCursor) At(index int32) unsafe.Pointer {if c.arena==nil {if c.p==nil{return nil};return unsafe.Add(c.p,int64(index)*c.size)};for i:=int32(0);;i++ {p:=c.Next();if p==nil||i==index{return p}}}
func (c *tableContainerCursor) Next() unsafe.Pointer {if c.order!=nil {if int(c.index)>=len(c.order){return nil};p:=c.arena.At(c.order[c.index]);c.index++;return p};if c.arena==nil {if c.p==nil{return nil};p:=c.p;c.p=unsafe.Add(p,c.size);return p};for c.chunk!=nil {if c.index<c.chunk.Count {p:=unsafe.Add(unsafe.Pointer(c.chunk),32+int64(c.index)*c.size);dead:=c.chunk.Dead&(uint64(1)<<uint(c.index))!=0;c.index++;if dead{continue};return p};c.index=0;c.chunk=(*tableContainerChunk)(c.arena.At(c.chunk.Next))};return nil}
func tableContainerAppend(w *TableWorker,h *tableContainer,size int64) unsafe.Pointer {if w==nil||w.Arena==nil||w.Arena.Locked||h.Count<0||h.Count==math.MaxInt32{return nil};if h.Ref==0 {p,ref:=w.Alloc(32);if p==nil{return nil};h.Ref=ref};head:=(*tableContainerHead)(w.Arena.At(h.Ref));chunk:=(*tableContainerChunk)(w.Arena.At(head.Last));if chunk==nil||chunk.Count==chunk.Capacity {p,ref:=w.Alloc(32+64*size);if p==nil{return nil};if chunk!=nil {chunk.Next=ref}else{head.First=ref};head.Last=ref;chunk=(*tableContainerChunk)(p);chunk.Capacity=64};p:=unsafe.Add(unsafe.Pointer(chunk),32+int64(chunk.Count)*size);chunk.Count++;h.Count++;head.Live++;clear(unsafe.Slice((*byte)(p),int(size)));return p}
func (r *TableReader) Take(sub TableReader) { r.Nodes.Carve=sub.Nodes.Carve }
func tableContainerErase(a *TableArena,h *tableContainer,p unsafe.Pointer,size int64) bool {if a==nil||a.Locked||h.Ref==0||p==nil{return false};head:=(*tableContainerHead)(a.At(h.Ref));if head==nil||head.Live!=h.Count{return false};for chunk:=(*tableContainerChunk)(a.At(head.First));chunk!=nil;chunk=(*tableContainerChunk)(a.At(chunk.Next)) {start:=uintptr(unsafe.Add(unsafe.Pointer(chunk),32));where:=uintptr(p);if where<start||where>=start+uintptr(chunk.Count)*uintptr(size)||(where-start)%uintptr(size)!=0{continue};index:=(where-start)/uintptr(size);bit:=uint64(1)<<uint(index);if chunk.Dead&bit!=0{return false};chunk.Dead|=bit;head.Live--;h.Count--;return true};return false}
type TableExtentCarve struct { Base unsafe.Pointer; At,Limit int64; Worker *TableWorker; Refused bool }
func (c *TableExtentCarve) Alloc(n,align int64) unsafe.Pointer {if c==nil||n<0{return nil};at:=(c.At+align-1)& -align;if at>c.Limit||n>c.Limit-at{return nil};c.At=at+n;return unsafe.Add(c.Base,at)}
func tableContainerFill(c *TableExtentCarve,h *tableContainer,count uint64,size,align int64) bool {if c==nil{return false};*h=tableContainer{};if count>math.MaxInt32 {if c.Worker!=nil {c.Refused=true;c.Worker.refused=true};return false};if count==0{return true};if c.Worker!=nil {return true};p:=c.Alloc(int64(count)*size,align);if p==nil{return false};h.Ref=int64(uintptr(p)-uintptr(unsafe.Pointer(h)));h.Count=int32(count);return true}
func tableContainerFillAt(c *TableExtentCarve,h *tableContainer,index int32,size int64) unsafe.Pointer {if c.Worker!=nil {return tableContainerAppend(c.Worker,h,size)};cursor:=tableContainerCursorBegin(h,size,nil);return cursor.At(index)}
func tableExtentFieldEmpty(p,base unsafe.Pointer,f *TableFieldInfo,a *TableArena) bool {if f.List||f.Map {return (*tableContainer)(p).Count==0};count:=int32(1);if f.IsArray{count=f.ArrayBound};for j:=int32(0);j<count;j++{at:=int64(0);if !tableExtentValue(unsafe.Add(p,uintptr(j)*uintptr(f.ElemSize)),nil,f,a,nil,&at)||at!=0{return false}};return true}
func tableExtentBytes(base unsafe.Pointer,info *TableTypeInfo,a *TableArena) int64 {at:=int64(0);if !tableExtentWalk(base,nil,info,a,nil,&at){return -1};return at}
func tableExtentWalk(base,dest unsafe.Pointer,info *TableTypeInfo,a *TableArena,region unsafe.Pointer,at *int64) bool {
 for i:=range info.Fields {f:=&info.Fields[i];p:=unsafe.Add(base,f.Offset);var q unsafe.Pointer;if dest!=nil {q=unsafe.Add(dest,f.Offset)}
 if !tableJsonGuardHolds(base,info,f.Guard)||f.Optional&&!*(*bool)(unsafe.Add(base,f.PresentOffset)) {if !tableExtentFieldEmpty(p,base,f,a){return false};continue};if f.List||f.Map {if !tableExtentContainer(p,q,f,a,region,at){return false};continue}
 count:=int32(1);if f.IsArray {count=tableRegionCount(base,f);if count<0||count>f.ArrayBound{return false};for j:=count;j<f.ArrayBound;j++ {empty:=int64(0);if !tableExtentValue(unsafe.Add(p,uintptr(j)*uintptr(f.ElemSize)),nil,f,a,nil,&empty)||empty!=0{return false}}};for j:=int32(0);j<count;j++ {var placed unsafe.Pointer;if q!=nil {placed=unsafe.Add(q,uintptr(j)*uintptr(f.ElemSize))};if !tableExtentValue(unsafe.Add(p,uintptr(j)*uintptr(f.ElemSize)),placed,f,a,region,at){return false}} };return true
}
func tableExtentContainer(p,q unsafe.Pointer,f *TableFieldInfo,a *TableArena,region unsafe.Pointer,at *int64)bool {h:=(*tableContainer)(p);if h.Count<0{return false};if h.Count==0 {if q!=nil {*(*tableContainer)(q)=tableContainer{}};return true};*at=(*at+int64(f.ElemAlign)-1)& -int64(f.ElemAlign);start:=*at;*at+=int64(h.Count)*int64(f.ElemSize);if *at<start{return false};cursor:=tableMapCursor(h,f,a);defer cursor.Release();if q!=nil {out:=(*tableContainer)(q);out.Count=h.Count;out.Ref=int64(uintptr(unsafe.Add(region,start))-uintptr(q))};for j:=int32(0);j<h.Count;j++ {elem:=cursor.Next();if elem==nil{return false};var placed unsafe.Pointer;if q!=nil {placed=unsafe.Add(region,start+int64(j)*int64(f.ElemSize));copy(unsafe.Slice((*byte)(placed),f.ElemSize),unsafe.Slice((*byte)(elem),f.ElemSize))};if !tableExtentValue(elem,placed,f,a,region,at){return false}};return true}

func tableExtentValue(p,q unsafe.Pointer,f *TableFieldInfo,a *TableArena,region unsafe.Pointer,at *int64) bool {if f.Pointer{return true};if f.Kind==13&&f.Table!=nil{return tableExtentWalk(p,q,f.Table(),a,region,at)};if f.Kind==15&&f.Arms!=nil {u:=f.Arms();tag:=tableJsonGetRaw(unsafe.Add(p,u.TagOffset),u.TagSize);if tag>=uint64(len(u.Arms)){return false};if tag==0||u.Arms[tag].Void{return true};arm:=&u.Arms[tag].Field;var placed unsafe.Pointer;if q!=nil{placed=unsafe.Add(q,arm.Offset)};count:=int32(1);if arm.IsArray {count=tableRegionCount(p,arm);if count<0||count>arm.ArrayBound{return false}};for j:=int32(0);j<count;j++ {var out unsafe.Pointer;if placed!=nil{out=unsafe.Add(placed,uintptr(j)*uintptr(arm.ElemSize))};if !tableExtentValue(unsafe.Add(p,uintptr(arm.Offset)+uintptr(j)*uintptr(arm.ElemSize)),out,arm,a,region,at){return false}};return true};return true}
func tableWireNodeBytes(r TableReader,t *TableTypeInfo) int64 {at:=int64(0);if !tableWireExtent(r,t,&at){return -2};return tableRegionRound(tableRegionRound(int64(t.Size))+at)}
func tableWireExtent(r TableReader,t *TableTypeInfo,at *int64) bool {for {ref,ok:=r.Leb();if !ok||ref==0{return true};id,ok:=r.Resolve(ref);if !ok||!r.Has(1){return true};kind:=r.Get8();var field *TableFieldInfo;for i:=range t.Fields {if t.Fields[i].Id==id {field=&t.Fields[i];break}};if field==nil {if !r.Skip(kind){return true};continue};f:=field
 if kind==15&&f.Kind==15&&!f.IsArray {if !tableWireUnionExtent(&r,f,at){return false};continue}
 if kind==13&&f.Kind==13&&!f.IsArray&&!f.Pointer {body,ok:=r.Body();if !ok{return true};if !tableWireExtent(body,f.Table(),at){return false};continue}
 if (kind==14&&f.KeyName==nil||kind==16&&f.KeyName!=nil)&&f.IsArray {body,ok:=r.Body();if !ok{return true};if !tableWireArrayExtent(body,f,kind==16,at){return false};continue};if !r.Skip(kind){return true}
 }}
func tableWireArrayExtent(r TableReader,f *TableFieldInfo,keyed bool,at *int64) bool {if len(r.Buffer)<2{return true};kind:=r.Get8();count,ok:=r.Leb();if !ok{return true};if kind!=f.Kind&&!tableKindWidens(kind,f.Kind){return true};if f.List||f.Map {if count>math.MaxInt32{return false};floor:=tableKindBytes(kind);if kind==13{floor=2};if floor<1{floor=1};if count>uint64((int64(len(r.Buffer))-r.Offset)/floor){return false};*at=(*at+int64(f.ElemAlign)-1)& -int64(f.ElemAlign);*at+=int64(count)*int64(f.ElemSize)}
 if kind!=13&&kind!=15{return true};for i:=uint64(0);i<count&&r.Has(1);i++ {if keyed {if _,ok:=r.Leb();!ok{return true}};if kind==13 {body,ok:=r.Body();if !ok{return true};if !tableWireExtent(body,f.Table(),at){return false}}else if !tableWireUnionExtent(&r,f,at){return false}};return true}
func tableWireUnionExtent(r *TableReader,f *TableFieldInfo,at *int64) bool {ref,ok:=r.Leb();if !ok {r.Offset=int64(len(r.Buffer));return true};if ref==0{return true};id,ok:=r.Resolve(ref);if !ok||!r.Has(1){r.Offset=int64(len(r.Buffer));return true};r.Get8();body,ok:=r.Body();if !ok{r.Offset=int64(len(r.Buffer));return true};u:=f.Arms();for i:=1;i<len(u.Arms);i++ {if f.VariantId(uint64(i))!=id{continue};arm:=&u.Arms[i];if arm.Void||arm.Field.Pointer{return true};af:=&arm.Field;if af.IsArray{return tableWireArrayExtent(body,af,af.KeyName!=nil,at)};if af.Kind==13&&af.Table!=nil{return tableWireExtent(body,af.Table(),at)};if af.Kind==15 {return tableWireUnionExtent(&body,af,at)}};return true}
`

// Keep the extent machinery out of units which have no containers.
func containerRegionSource(s string) string {
	s = strings.Replace(s, "nodes:=TableNodeMap{Entries:directory,Arena:true}", "nodes:=TableNodeMap{Entries:directory,Arena:true,Carve:TableExtentCarve{Worker:worker}}", 1)
	s = strings.Replace(s, "graph:=tableJsonGraph{alloc:worker.Alloc,", "graph:=tableJsonGraph{mapPlace:func(p unsafe.Pointer,f *TableFieldInfo,key tableMapKey)(unsafe.Pointer,bool){return tableMapPlace(worker,(*tableContainer)(p),f,key)},listAdd:func(p unsafe.Pointer,size int64)unsafe.Pointer{return tableContainerAppend(worker,(*tableContainer)(p),size)},alloc:worker.Alloc,", 1)
	s = strings.Replace(s, "type TableNodeMap struct {", "type TableNodeMap struct { Carve TableExtentCarve;", 1)
	s = strings.Replace(s, "return tableRegionRound(int64(t.Size))", "return tableWireNodeBytes(body,t)", 1)
	s = strings.Replace(s, "data:=tableRegionRound(int64(root.Size));s:=", "data:=tableWireNodeBytes(r,root);if data<0{return -1,0,TableOpenRefused};s:=", 1)
	s = strings.Replace(s, "used:=tableRegionRound(int64(root.Size));k:=", "used:=tableWireNodeBytes(r,root);k:=", 1)
	s = strings.Replace(s, "body.Nodes=nodes;t.LoadBody(body,p)", "carve:=TableExtentCarve{Base:p,At:tableRegionRound(int64(t.Size)),Limit:tableWireNodeBytes(body,t)};body.Nodes=nodes;body.Nodes.Carve=carve;t.LoadBody(body,p)", 1)
	s = strings.Replace(s, "r.Nodes=nodes;r.Nested=false;root.LoadBody(r,base)", "carve:=TableExtentCarve{Base:base,At:tableRegionRound(int64(root.Size)),Limit:tableWireNodeBytes(r,root)};r.Nodes=nodes;r.Nodes.Carve=carve;r.Nested=false;root.LoadBody(r,base)", 1)
	s = strings.ReplaceAll(s, "tableRegionEdges(p,t,func", "tableRegionEdges(p,t,a,func")
	s = strings.ReplaceAll(s, "tableRegionEdges(base unsafe.Pointer,info *TableTypeInfo,visit", "tableRegionEdges(base unsafe.Pointer,info *TableTypeInfo,a *TableArena,visit")
	s = strings.ReplaceAll(s, "tableRegionValueEdges(p unsafe.Pointer,f *TableFieldInfo,visit", "tableRegionValueEdges(p unsafe.Pointer,f *TableFieldInfo,a *TableArena,visit")
	s = strings.ReplaceAll(s, "tableRegionEdges(p,f.Table(),visit)", "tableRegionEdges(p,f.Table(),a,visit)")
	s = strings.ReplaceAll(s, "tableRegionValueEdges(at,f,visit)", "tableRegionValueEdges(at,f,a,visit)")
	s = strings.ReplaceAll(s, "af,visit)", "af,a,visit)")
	s = strings.ReplaceAll(s, "tableRegionEdges(e.node,e.info,func", "tableRegionEdges(e.node,e.info,nil,func")
	s = strings.Replace(s, "p:=unsafe.Add(base,f.Offset);count:=int32(1);", `p:=unsafe.Add(base,f.Offset);if f.List||f.Map {if !tableRegionContainerEdges((*tableContainer)(p),f,a,visit){return false};continue};count:=int32(1);`, 1)
	s = strings.Replace(s, "if e.info!=nil {return tableRegionRound(int64(e.info.Size))}", "if e.info!=nil {extent:=tableExtentBytes(e.node,e.info,a);if extent<0{return -1};return tableRegionRound(tableRegionRound(int64(e.info.Size))+extent)}", 1)
	s += `
func tableRegionContainerEdges(h *tableContainer,f *TableFieldInfo,a *TableArena,visit func(*TableRef,*TableFieldInfo)bool)bool {
 if h.Count<0{return false};cursor:=tableMapCursor(h,f,a);defer cursor.Release();for j:=int32(0);j<h.Count;j++{elem:=cursor.Next();if elem==nil||!tableRegionValueEdges(elem,f,a,visit){return false}};return true
}
`
	start := strings.Index(s, "func tableRegionPack(")
	end := strings.Index(s[start:], "\n}\n") + start + 3
	s = s[:start] + tableContainerPackSource + s[end:]
	return s
}

const tableContainerPackSource = `
func tableRegionPack(root unsafe.Pointer,info *TableTypeInfo,a *TableArena) ([]byte,[]byte,bool) {
 total:=tableRegionPackMeasure(root,info,a);if total<0{return nil,nil,false};n,ok:=tableNumber(root,info,a);if !ok{return nil,nil,false};defer n.Release();size:=tablePackOffsets(&n);if size<0||size!=total{return nil,nil,false}
 allocation:=a.Allocator.alloc(size);if allocation==nil{return nil,nil,false};region:=allocation[:size];clear(region);base:=unsafe.Pointer(&region[0])
 for i:=range n.entries {e:=&n.entries[i];p:=unsafe.Add(base,e.offset);if e.info==nil {length:=8+int(uint64(*(*uint32)(e.node)));copy(unsafe.Slice((*byte)(p),length),unsafe.Slice((*byte)(e.node),length));continue};copy(unsafe.Slice((*byte)(p),e.info.Size),unsafe.Slice((*byte)(e.node),e.info.Size));at:=tableRegionRound(int64(e.info.Size));if !tableExtentWalk(e.node,p,e.info,a,p,&at){a.Allocator.free(allocation);return nil,nil,false};if !tableRegionEdges(p,e.info,nil,func(slot *TableRef,f *TableFieldInfo) bool {if *slot==0{return true};target:=a.At(*slot);idx,ok:=n.find(target);if !ok{return false};*slot=int64(uintptr(unsafe.Add(base,n.entries[idx].offset))-uintptr(unsafe.Pointer(slot)));return true}){a.Allocator.free(allocation);return nil,nil,false}}
 return region,allocation,true
}
`

// Container JSON stays in the unit-independent descriptor walk. Only the
// authoring allocation callback knows about the arena's chunk representation.
const tableJsonListSource = `
func tableJsonWriteList(out *tableJsonOut,holder unsafe.Pointer,f *TableFieldInfo,depth int32) bool {
 count:=*(*int32)(unsafe.Add(holder,8));if count<0{return false};if count==0{out.text("[]");return true};ref:=*(*int64)(holder);if ref==0{return false};p:=unsafe.Add(holder,ref);out.put('[');for i:=int32(0);i<count;i++ {if i>0{out.put(',')};out.line(depth+1);if !tableJsonWriteScalar(out,unsafe.Add(p,uintptr(i)*uintptr(f.ElemSize)),f,depth+1){return false}};out.line(depth);out.put(']');return true
}
func tableJsonReadList(in *tableJsonIn,holder unsafe.Pointer,f *TableFieldInfo,depth int32) bool {
 if in.peek()!='['||in.graph==nil||in.graph.listAdd==nil {in.bad=true;return false};in.pos++;clear(unsafe.Slice((*byte)(holder),16));shape:=tableJsonElementShape(f)
 for {c:=in.peek();if c==']'{in.pos++;return true};if c==0{in.bad=true;return false};p:=in.graph.listAdd(holder,int64(f.ElemSize));if p==nil{in.bad=true;return false};if f.Kind==13&&!f.Pointer{f.Table().Reset(p)};if in.valueShape()!=shape&&!(f.Pointer&&in.valueShape()=='z'){in.report.KindMismatch++;if !in.skipValue(depth+1){return false}}else if !tableJsonReadScalar(in,p,f,depth+1){return false};c=in.peek();if c==','{in.pos++;continue};if c==']'{in.pos++;return true};in.bad=true;return false}
}
`
