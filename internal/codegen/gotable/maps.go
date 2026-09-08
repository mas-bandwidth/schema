package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) mapDescriptor(f *ir.Field) string {
	for i, field := range g.owner.Fields {
		if field == f {
			return fmt.Sprintf("&%sTableFields[%d]", g.owner.Name, i)
		}
	}
	panic("map field not owned")
}
func mapValueHandle(u *ir.Unit, f *ir.Field) (typ, access string) {
	v := ir.MapValueField(f)
	if v.Type.Optional || v.Array == ir.ArrayCounted || !v.Type.Pointer && (v.Type.Kind == ir.TString || v.Type.Kind == ir.TWString || v.Type.Kind == ir.TBytes) {
		return containerElementType(u, f), "entry"
	}
	typ = goCookBlittableType(u, v.Type)
	switch {
	case v.IsMap():
		typ = "TableMap[" + containerElementType(u, v) + "]"
	case v.IsList():
		typ = "TableList[" + containerElementType(u, v) + "]"
	case v.Array != ir.ArrayNone:
		typ = fmt.Sprintf("[%d]%s", v.ArrayBound, typ)
	}
	return typ, "&entry.Value"
}
func (g *tableGen) emitMapSurface(st *ir.Struct, f *ir.Field) {
	name := st.Name + ir.GoExportName(f.Name)
	entry := containerElementType(g.unit, f)
	typ, access := mapValueHandle(g.unit, f)
	key := ir.MapKeyField(f)
	kt := goFieldType(key.Type)
	kv := "tableMapKey{raw:uint64(key)}"
	if key.Type.Kind == ir.TString {
		kt = "string"
		kv = "tableMapKey{text:[]byte(key)}"
	}
	holder := "*TableMap[" + entry + "]"
	desc := g.mapDescriptor(f)
	g.pf("func %sInsert(worker *TableWorker,holder %s,key %s) *%s {p,_:=tableMapPlace(worker,(*tableContainer)(unsafe.Pointer(holder)),%s,%s);if p==nil{return nil};entry:=(*%s)(p);value:=entry;", name, holder, kt, typ, desc, kv, entry)
	g.emitTableResetField(ir.MapValueField(f))
	if ir.MapValueField(f).Type.Optional {
		g.pf("value.ValuePresent=false;")
	}
	g.pf("_ = value;return %s }\n", access)
	g.pf("func %sFind(arena *TableArena,holder %s,key %s) *%s {p:=tableMapFind((*tableContainer)(unsafe.Pointer(holder)),%s,%s,arena);if p==nil{return nil};entry:=(*%s)(p);return %s }\n", name, holder, kt, typ, desc, kv, entry, access)
	g.pf("func %sErase(arena *TableArena,holder %s,key %s) bool {p:=tableMapFind((*tableContainer)(unsafe.Pointer(holder)),%s,%s,arena);return tableContainerErase(arena,(*tableContainer)(unsafe.Pointer(holder)),p,int64(unsafe.Sizeof(%s{}))) }\n", name, holder, kt, desc, kv, entry)
	keyexpr := "entry.Key"
	if key.Type.Kind == ir.TString {
		keyexpr = "string(entry.Key[:entry.KeyLength])"
	}
	g.pf("func %sEach(holder %s,visit func(%s,*%s)bool,arena ...*TableArena) bool {c:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(holder)),int64(unsafe.Sizeof(%s{})),tableOptionalArena(arena));for i:=int32(0);i<holder.Count;i++ {entry:=(*%s)(c.Next());if entry==nil||!visit(%s,%s){return false}};return true }\n", name, holder, kt, typ, entry, entry, keyexpr, access)
	g.pf("func %sIndexMeasure(holder %s) int64 {return tableMapIndexMeasure(holder.Count)}\n", name, holder)
	g.pf("func %sIndex(holder %s,storage []byte) TableMapIndex {return tableMapIndex((*tableContainer)(unsafe.Pointer(holder)),%s,storage)}\n", name, holder, desc)
	g.pf("func %sIndexFind(index TableMapIndex,holder %s,key %s) *%s {p:=tableMapIndexFind(index,(*tableContainer)(unsafe.Pointer(holder)),%s,%s);if p==nil{return nil};entry:=(*%s)(p);return %s }\n", name, holder, kt, typ, desc, kv, entry, access)
}
func (g *tableGen) emitMapWrite(f *ir.Field, expr, writer, ind string, framed bool) {
	g.emitListWrite(f, expr, writer, ind, framed)
}
func (g *tableGen) emitMapRead(f *ir.Field, ind string) {
	entry := containerElementType(g.unit, f)
	layout := ir.RecordLayout(g.unit, f.MapEntry)
	desc := g.mapDescriptor(f)
	g.pf("%ssub,ok:=r.Body();if !ok {r.Report.Malformed=true;return false};if len(sub.Buffer)>=2 {header:=sub;header.Buffer=r.Buffer[r.Offset-int64(len(sub.Buffer)):];kind:=header.Get8();count,ok:=header.Leb();sub.Offset=header.Offset;if !ok {r.Report.Malformed=true;break};if kind!=13 {r.Report.KindMismatch++;break};field:=%s;holder:=(*tableContainer)(unsafe.Pointer(&value.%s));if !tableContainerFill(&sub.Nodes.Carve,holder,count,%d,%d) {if sub.Nodes.Carve.Refused {r.Take(sub);return false};r.Report.Malformed=true;break};holder.Count=0;var last tableMapKey;var prior *%s;widened:=false\n", ind, desc, member(f), layout.Size, layout.Align, entry)
	g.retainDiscard("r")
	g.pf("%sfor i:=uint64(0);i<count;i++ {elem,ok:=sub.Body();if !ok {r.Report.Malformed=true;break};key,bad,over,wide,whole:=tableMapReadKey(elem,&field.Table().Fields[0]);if wide&&!widened {widened=true;r.Report.Widened++};if bad {r.Report.KindMismatch++;*holder=tableContainer{};break};if !whole {r.Report.Malformed=true;break};if over {r.Report.Clamped++;continue};order:= -1;if prior!=nil {order=tableMapKeyOrder(last,key,&field.Table().Fields[0])};if order>0 {r.Report.Malformed=true;break};p:=prior;if order==0 {r.Report.Duplicate++}else {p=(*%s)(tableContainerFillAt(&sub.Nodes.Carve,holder,holder.Count,%d));if p==nil {r.Report.Malformed=true;break};if sub.Nodes.Carve.Worker==nil {holder.Count++}};\n", ind, entry, layout.Size)
	g.retainReadPath("elem", "r", "holder.Count-1")
	g.pf("%sLoadBody(&elem,p);\n", f.MapEntry.Name)
	g.emitCarveReturn("sub", "elem", ind)
	g.pf("%slast=key;prior=p };\n", ind)
	g.emitCarveReturn("r", "sub", ind)
	g.pf("%s}\n", ind)
}

const tableMapSource = `
type TableMap[T any] struct { Ref int64; Count int32; _ int32 }
func (m *TableMap[T]) Clear(){*m=TableMap[T]{}}
func (m *TableMap[T]) At(index int32,arena ...*TableArena)*T {if m==nil||index<0||index>=m.Count{return nil};c:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(m)),int64(unsafe.Sizeof(*new(T))),tableOptionalArena(arena));return (*T)(c.At(index))}
func (c *tableContainerCursor) Release(){if c.memory!=nil {c.arena.Allocator.free(c.memory);c.memory=nil;c.order=nil}}
func tableMapCursor(h *tableContainer,f *TableFieldInfo,a *TableArena) tableContainerCursor {
 c:=tableContainerCursorBegin(h,int64(f.ElemSize),a);if !f.Map||a==nil||h.Count==0{return c};bad:=tableContainerCursor{arena:a};if h.Count<0||h.Ref==0{return bad};memory:=a.Allocator.alloc(int64(h.Count)*8);if memory==nil{return bad};order:=unsafe.Slice((*int64)(unsafe.Pointer(&memory[0])),int(h.Count));head:=(*tableContainerHead)(a.At(h.Ref));n:=0
 for ref:=head.First;ref!=0; {chunk:=(*tableContainerChunk)(a.At(ref));if chunk==nil {a.Allocator.free(memory);return bad};for i:=int32(0);i<chunk.Count;i++ {if chunk.Dead&(uint64(1)<<uint(i))!=0{continue};if n>=len(order){a.Allocator.free(memory);return bad};order[n]=ref+32+int64(i)*int64(f.ElemSize);n++};ref=chunk.Next};if n!=len(order) {a.Allocator.free(memory);return bad};key:=&f.Table().Fields[0]
 slices.SortFunc(order,func(x,y int64)int{return tableMapKeyOrder(tableMapStoredKey(a.At(x),key),tableMapStoredKey(a.At(y),key),key)})
 for i,ref:=range order {k:=tableMapStoredKey(a.At(ref),key);if !tableMapKeyValid(k,key)||i>0&&tableMapKeyOrder(tableMapStoredKey(a.At(order[i-1]),key),k,key)>=0 {a.Allocator.free(memory);return bad}}
 return tableContainerCursor{arena:a,size:int64(f.ElemSize),order:order,memory:memory}
}
func tableMapFind(h *tableContainer,f *TableFieldInfo,key tableMapKey,a *TableArena) unsafe.Pointer {if h==nil||h.Count<=0||h.Ref==0{return nil};kf:=&f.Table().Fields[0];if !tableMapKeyValid(key,kf){return nil};if a==nil {base:=unsafe.Add(unsafe.Pointer(h),h.Ref);lo,hi:=int32(0),h.Count;for lo<hi {mid:=lo+(hi-lo)/2;p:=unsafe.Add(base,int64(mid)*int64(f.ElemSize));order:=tableMapKeyOrder(tableMapStoredKey(p,kf),key,kf);if order<0{lo=mid+1}else{hi=mid}};if lo<h.Count {p:=unsafe.Add(base,int64(lo)*int64(f.ElemSize));if tableMapKeyOrder(tableMapStoredKey(p,kf),key,kf)==0{return p}};return nil};c:=tableContainerCursorBegin(h,int64(f.ElemSize),a);for i:=int32(0);i<h.Count;i++ {p:=c.Next();if p==nil{return nil};if tableMapKeyOrder(tableMapStoredKey(p,kf),key,kf)==0{return p}};return nil}
func tableMapPlace(w *TableWorker,h *tableContainer,f *TableFieldInfo,key tableMapKey)(unsafe.Pointer,bool) {if w==nil||w.Arena==nil||w.Arena.Locked||!tableMapKeyValid(key,&f.Table().Fields[0]) {return nil,false};if p:=tableMapFind(h,f,key,w.Arena);p!=nil{return p,true};p:=tableContainerAppend(w,h,int64(f.ElemSize));if p==nil{return nil,false};f.Table().Reset(p);tableMapStoreKey(p,&f.Table().Fields[0],key);return p,false}
func tableMapReadKey(r TableReader,f *TableFieldInfo)(key tableMapKey,bad,over,wide,whole bool) {
 for {ref,ok:=r.Leb();if !ok{return};if ref==0 {whole=r.Offset==int64(len(r.Buffer));return};id,ok:=r.Resolve(ref);if !ok||!r.Has(1){return};kind:=r.Get8();if id!=f.Id {if !r.Skip(kind){return};continue};if kind!=f.Kind&&tableKindWidens(kind,f.Kind) {wide=true}else{bad=kind!=f.Kind;if bad {if !r.Skip(kind){return};continue}};if kind==12 {body,ok:=r.Body();if !ok||!tableUtf8Valid(body.Buffer){return};key.text=body.Buffer;over=len(key.text)>int(f.ArrayBound)}else{size:=tableKindBytes(kind);if !r.Has(size){return};if f.Kind>=2&&f.Kind<=5 {key.raw=uint64(r.Signed(kind))}else{key.raw=r.Unsigned(kind)}} }
}
type TableMapIndex struct { slots []int32 }
func tableMapIndexMeasure(count int32)int64 {if count<0||count>1<<29{return -1};capacity:=int64(2);for capacity<int64(count)*2 {capacity*=2};return capacity*4}
func tableMapHash(k tableMapKey,f *TableFieldInfo) uint64 {h:=uint64(14695981039346656037);if f.Kind==12 {for _,b:=range k.text{h=(h^uint64(b))*1099511628211}}else{for i:=0;i<8;i++{h=(h^(k.raw>>uint(i*8)&255))*1099511628211}};return h}
func tableMapIndex(h *tableContainer,f *TableFieldInfo,storage []byte) TableMapIndex {n:=tableMapIndexMeasure(h.Count);if n<0||int64(len(storage))<n||len(storage)==0||uintptr(unsafe.Pointer(&storage[0]))%4!=0 {return TableMapIndex{}};slots:=unsafe.Slice((*int32)(unsafe.Pointer(&storage[0])),int(n/4));clear(slots);if h.Count>0&&h.Ref==0{return TableMapIndex{}};base:=unsafe.Add(unsafe.Pointer(h),h.Ref);key:=&f.Table().Fields[0];for i:=int32(0);i<h.Count;i++ {p:=unsafe.Add(base,int64(i)*int64(f.ElemSize));at:=tableMapHash(tableMapStoredKey(p,key),key)&uint64(len(slots)-1);for slots[at]!=0{at=(at+1)&uint64(len(slots)-1)};slots[at]=i+1};return TableMapIndex{slots:slots}}
func tableMapIndexFind(index TableMapIndex,h *tableContainer,f *TableFieldInfo,key tableMapKey) unsafe.Pointer {if len(index.slots)==0{return tableMapFind(h,f,key,nil)};kf:=&f.Table().Fields[0];if !tableMapKeyValid(key,kf)||h.Ref==0{return nil};base:=unsafe.Add(unsafe.Pointer(h),h.Ref);at:=tableMapHash(key,kf)&uint64(len(index.slots)-1);for i:=0;i<len(index.slots);i++ {slot:=index.slots[at];if slot==0{return nil};if slot<0||slot>h.Count{return nil};p:=unsafe.Add(base,int64(slot-1)*int64(f.ElemSize));if tableMapKeyOrder(tableMapStoredKey(p,kf),key,kf)==0{return p};at=(at+1)&uint64(len(index.slots)-1)};return nil}
`

const tableJsonMapSource = `
type tableMapKey struct { text []byte; raw uint64 }
func tableMapKeyValid(k tableMapKey,f *TableFieldInfo)bool {return f.Kind!=12||k.raw==0&&len(k.text)<=int(f.ArrayBound)}
func tableMapStoredKey(p unsafe.Pointer,f *TableFieldInfo)tableMapKey {key:=tableMapKey{};if f.Kind==12 {n:=*(*int32)(unsafe.Add(p,f.CountOffset));if n<0||n>f.ArrayBound{return tableMapKey{text:nil,raw:1}};key.text=unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),int(n))}else{key.raw=tableJsonGetRaw(unsafe.Add(p,f.Offset),f.ElemSize);if f.Kind>=2&&f.Kind<=5 {key.raw=uint64(tableJsonGetSigned(unsafe.Add(p,f.Offset),f.ElemSize))}};return key}
func tableMapStoreKey(p unsafe.Pointer,f *TableFieldInfo,k tableMapKey) {if f.Kind==12 {clear(unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),int(f.ArrayBound)+1));copy(unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),len(k.text)),k.text);*(*int32)(unsafe.Add(p,f.CountOffset))=int32(len(k.text))}else{tableJsonSetRaw(unsafe.Add(p,f.Offset),f.ElemSize,k.raw)}}
func tableMapKeyOrder(a,b tableMapKey,f *TableFieldInfo)int {if f.Kind==12 {for i:=0;i<len(a.text)&&i<len(b.text);i++ {if a.text[i]<b.text[i]{return -1};if a.text[i]>b.text[i]{return 1}};if len(a.text)<len(b.text){return -1};if len(a.text)>len(b.text){return 1};return 0};if f.Kind>=2&&f.Kind<=5 {if int64(a.raw)<int64(b.raw){return -1};if int64(a.raw)>int64(b.raw){return 1};return 0};if a.raw<b.raw{return -1};if a.raw>b.raw{return 1};return 0}
func tableJsonWriteMap(out *tableJsonOut,p unsafe.Pointer,f *TableFieldInfo,depth int32)bool {if depth>tableJsonMaxDepth{return false};count:=*(*int32)(unsafe.Add(p,8));if count<0{return false};out.put('{');if count>0 {ref:=*(*int64)(p);if ref==0{return false};base:=unsafe.Add(p,ref);entry:=f.Table();key,value:=&entry.Fields[0],&entry.Fields[1];for i:=int32(0);i<count;i++ {if i>0{out.put(',')};out.line(depth+1);at:=unsafe.Add(base,int64(i)*int64(f.ElemSize));k:=tableMapStoredKey(at,key);if key.Kind==12 {tableJsonWriteString(out,k.text)}else{out.put('"');if key.Kind>=2&&key.Kind<=5 {tableJsonWriteSigned(out,int64(k.raw))}else{tableJsonWriteUnsigned(out,k.raw)};out.put('"')};out.text(": ");if value.Optional&&!*(*bool)(unsafe.Add(at,value.PresentOffset)){out.text("null")}else if !tableJsonWriteField(out,at,value,depth+1){return false}};out.line(depth)};out.put('}');return true}
func tableJsonReadMap(in *tableJsonIn,p unsafe.Pointer,f *TableFieldInfo,depth int32)bool {if depth>tableJsonMaxDepth||in.peek()!='{'||in.graph==nil||in.graph.mapPlace==nil {in.bad=true;return false};clear(unsafe.Slice((*byte)(p),16));in.pos++;entry:=f.Table();key,value:=&entry.Fields[0],&entry.Fields[1]
 for {if in.peek()=='}'{in.pos++;return true};probe:=*in;n,ok:=probe.scanString(nil);if !ok {in.bad=true;return false};good:=true;var k tableMapKey
 if key.Kind==12 {if n>key.ArrayBound {in.report.Clamped++;in.pos=probe.pos;good=false}else{memory,_:=in.graph.alloc(int64(n)+1);if memory==nil{in.bad=true;return false};k.text=unsafe.Slice((*byte)(memory),int(n));_,ok=in.scanString(k.text)}}else{var text [tableJsonMaxNumber]byte;if n>tableJsonMaxNumber {in.pos=probe.pos;good=false}else{_,ok=in.scanString(text[:n]);k.raw,good=tableJsonMapInteger(text[:n],key)};if !good{in.report.KindMismatch++}}
 if !ok||in.peek()!=':' {in.bad=true;return false};in.pos++;if !good {tableJsonDropLabel(in);if !in.skipValue(depth+1){return false}}else{slot,duplicate:=in.graph.mapPlace(p,f,k);if slot==nil {in.bad=true;return false};if duplicate{in.report.Duplicate++};entry.Reset(slot);tableMapStoreKey(slot,key,k);shape:=in.valueShape();if value.Optional&&shape=='z' {if !in.literal("null"){return false}}else if shape!=tableJsonShape(value)&&!(value.Pointer&&shape=='z'){in.report.KindMismatch++;tableJsonDropLabel(in);if !in.skipValue(depth+1){return false}}else{if !tableJsonReadField(in,slot,value,depth+1){return false};if value.Optional{*(*bool)(unsafe.Add(slot,value.PresentOffset))=true}}};c:=in.peek();if c==','{in.pos++;continue};if c=='}'{in.pos++;return true};in.bad=true;return false }
}
func tableJsonMapInteger(text []byte,f *TableFieldInfo)(uint64,bool) {var report TableReport;in:=tableJsonIn{text:text,report:&report};integral,ok:=in.walkNumber();if !ok||in.pos!=len(text){return 0,false};_ = integral;v,ok:=tableJsonDecimalKey(text,f.Kind>=2&&f.Kind<=5,int(f.ElemSize)*8);return v,ok}
`

const tableJsonMapDecimalSource = `
// Decimal normalization preserves every uint64 bit and accepts integral
// exponent/fraction spellings without a binary floating-point conversion.
func tableJsonDecimalKey(text []byte,signed bool,width int)(uint64,bool) {
 i:=0;negative:=false;if len(text)>0&&text[0]=='-' {negative=true;i++};var digits [tableJsonMaxNumber]byte;n,fraction:=0,0;point:=false
 for i<len(text)&&text[i]!='e'&&text[i]!='E' {c:=text[i];i++;if c=='.'{point=true;continue};if n==len(digits){return 0,false};digits[n]=c-'0';n++;if point{fraction++}}
 exponent:=0;if i<len(text) {i++;minus:=false;if i<len(text)&&(text[i]=='+'||text[i]=='-'){minus=text[i]=='-';i++};for i<len(text){if exponent<100000{exponent=exponent*10+int(text[i]-'0')};i++};if minus{exponent= -exponent}}
 first:=0;for first<n&&digits[first]==0{first++};if first==n{return 0,true};scale:=exponent-fraction
 for scale<0&&n>first&&digits[n-1]==0 {n--;scale++};if scale<0||n-first+scale>20||negative&&!signed{return 0,false};limit:=^uint64(0);if signed {limit=uint64(1)<<uint(width-1);if !negative{limit--}}else if width<64{limit=uint64(1)<<uint(width)-1}
 value:=uint64(0);for j:=first;j<n+scale;j++ {d:=uint64(0);if j<n{d=uint64(digits[j])};if value>(limit-d)/10{return 0,false};value=value*10+d};if negative {value= -value};return value,true
}
`
