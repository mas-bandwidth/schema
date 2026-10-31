// Unbounded text in the Go table backend (docs/SPEC-TABLES.md §2.13.1): a text
// list whose element is a bare `string` and a map whose key is one.
//
// The element record's `value` and the entry's `key` are ONE TableRef slot
// naming the blob record §2.5 defines (u32 length, u32 zero, the bytes, one
// zero byte): an arena reference in a builder, a self-relative delta into the
// holder's node extent in a region and a cook. The wire is kind 12 exactly as
// string(N)'s. What this file adds is the generated read of one element or key,
// the accessors, and the runtime's slot functions — the last as replacements on
// the runtime sources, made only in a unit that uses the construct, so every
// other unit's output is byte-identical to what it was.
package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// unboundedHolder names the text list or map (`Table.field`) an unbounded
// value of the current record belongs to, for the report's Reason.
func (g *tableGen) unboundedHolder(f *ir.Field) string {
	if holder := g.textValueHolder(f); holder != "" {
		return holder
	}
	if g.owner != nil && g.owner.MapEntryOf != "" {
		return g.owner.MapEntryOf
	}
	if g.owner != nil {
		return g.owner.Name + "." + f.Name
	}
	return f.Name
}

// emitReadUnboundedText reads one unbounded value inside its record's body
// (§2.13.1): its L, then well-formed UTF-8 with no zero byte, placed as a blob
// record carved from the node's extent (or the builder's arena). Never clamped.
func (g *tableGen) emitReadUnboundedText(f *ir.Field, expr, ind string) {
	holder := g.unboundedHolder(f)
	g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; tableTextMalformed(r.Report, %q); return false }\n", ind, holder)
	g.pf("%sif !tableUTF8Valid(sub.Buffer) { r.Report.Malformed = true; tableTextMalformed(r.Report, %q); %s = 0; break }\n", ind, holder, expr)
	g.pf("%sif !tableTextPlace(&r.Nodes.Carve, &%s, sub.Buffer) { if r.Nodes.Carve.Refused { return false }; r.Report.Malformed = true; tableTextMalformed(r.Report, %q); break }\n", ind, expr, holder)
}

// emitReadUnboundedTextElement reads one element of a plain unbounded text
// list (§2.13.1, §3): its own L and bytes under element kind 12.
func (g *tableGen) emitReadUnboundedTextElement(f *ir.Field, expr, ind string) {
	v := ir.TextListValue(f)
	value := expr + "." + member(v)
	holder := f.TextListElem().MapEntryOf
	g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; tableTextMalformed(r.Report, %q); break }\n", ind, holder)
	g.pf("%sif !tableUTF8Valid(elem.Buffer) { r.Report.Malformed = true; tableTextMalformed(r.Report, %q); %sReset(&%s); break }\n", ind, holder, f.Type.Name, expr)
	g.pf("%sif !tableTextPlace(&sub.Nodes.Carve, &%s, elem.Buffer) { if sub.Nodes.Carve.Refused { r.Take(sub); return false }; r.Report.Malformed = true; tableTextMalformed(r.Report, %q); break }\n", ind, value, holder)
}

// emitUnboundedTextListSurface emits an unbounded-element text list's
// accessors (§2.13.1): TextAt, a view into the blob that allocates nothing,
// and Append (and AppendAbsent) taking the worker, because the bytes are
// allocated, for the bounded [..N] count too.
func (g *tableGen) emitUnboundedTextListSurface(st *ir.Struct, f *ir.Field) {
	name := st.Name + ir.GoExportName(f.Name)
	storage := g.storageName(st.Name)
	elem := f.Type.Name
	v := ir.TextListValue(f)
	field := member(f)
	val := member(v)
	optional := ir.TextListOptional(f)
	absent := ""
	present := ""
	if optional {
		absent = "||!p." + val + "Present"
		present = "p." + val + "Present=true;"
	}
	g.pf("// %sTextAt is element i of %s.%s (docs/SPEC-TABLES.md §2.13.1): a view into its\n", name, st.Name, f.Name)
	g.pf("// blob record that allocates nothing, and false past the count or for an absent element.\n")
	if f.IsList() {
		g.pf("func %sTextAt(value *%s,i int32,arena ...*TableArena)([]byte,bool){if value==nil{return nil,false};p:=value.%s.At(i,arena...);if p==nil%s{return nil,false};return tableTextBytes(&p.%s,tableOptionalArena(arena))}\n",
			name, storage, field, absent, val)
	} else {
		g.pf("func %sTextAt(value *%s,i int32,arena ...*TableArena)([]byte,bool){if value==nil||i<0||i>=value.%sCount||value.%sCount>%d{return nil,false};p:=&value.%s[i];if false%s{return nil,false};return tableTextBytes(&p.%s,tableOptionalArena(arena))}\n",
			name, storage, field, field, f.ArrayBound, field, absent, val)
	}
	// the write-side text rule (§2.13, §2.13.1): a zero byte and ill-formed
	// UTF-8 are refused at the append, as Save refuses them; no length bound
	valid := "if !tableUTF8Valid(unsafe.Slice(unsafe.StringData(text),len(text))){return false};"
	g.needsUnsafe()
	g.pf("// %sAppend appends one element to %s.%s, false past the count bound or when\n", name, st.Name, f.Name)
	g.pf("// the arena refuses (docs/SPEC-TABLES.md §2.13.1).\n")
	if f.IsList() {
		g.pf("func %sAppend(worker *TableWorker,value *%s,text string)bool{"+valid+"ref,ok:=tableTextStore(worker,text);if !ok{return false};p:=%sAdd(worker,&value.%s);if p==nil{return false};p.%s=ref;%sreturn true}\n",
			name, storage, name, field, val, present)
		if optional {
			g.pf("// %sAppendAbsent appends one absent element to %s.%s.\n", name, st.Name, f.Name)
			g.pf("func %sAppendAbsent(worker *TableWorker,value *%s)bool{return %sAdd(worker,&value.%s)!=nil}\n", name, storage, name, field)
		}
	} else {
		g.pf("func %sAppend(worker *TableWorker,value *%s,text string)bool{"+valid+"if value.%sCount<0||value.%sCount>=%d{return false};ref,ok:=tableTextStore(worker,text);if !ok{return false};p:=&value.%s[value.%sCount];%sReset(p);p.%s=ref;%svalue.%sCount++;return true}\n",
			name, storage, field, field, f.ArrayBound, field, field, elem, val, present, field)
		if optional {
			g.pf("// %sAppendAbsent appends one absent element to %s.%s.\n", name, st.Name, f.Name)
			g.pf("func %sAppendAbsent(worker *TableWorker,value *%s)bool{if value.%sCount<0||value.%sCount>=%d{return false};%sReset(&value.%s[value.%sCount]);value.%sCount++;return true}\n",
				name, storage, field, field, f.ArrayBound, elem, field, field, field)
		}
	}
}

// unboundedTextRuntimeSource is the slot runtime (§2.13.1, §2.5): reading,
// placing and storing a blob record behind a TableRef, the extent a wire
// read carves, and the strict read's malformed refusal naming the field.
const unboundedTextRuntimeSource = `
// tableTextBytes is the text an unbounded slot names (docs/SPEC-TABLES.md
// §2.13.1): its blob record's bytes, nil for a null slot, which is the empty
// string. It allocates nothing.
func tableTextBytes(slot *TableRef,a *TableArena)([]byte,bool){if *slot==0{return nil,true};p:=tableRefAt(slot,a);if p==nil{return nil,false};return unsafe.Slice((*byte)(unsafe.Add(p,8)),int((*TableBlob)(p).Length)),true}
func tableTextString(slot *TableRef,a *TableArena)string{t,_:=tableTextBytes(slot,a);return string(t)}
func tableTextRides(slot *TableRef,a *TableArena)bool{t,ok:=tableTextBytes(slot,a);return !ok||len(t)>0}
// tableTextStore allocates text's blob record in the worker's arena.
func tableTextStore(w *TableWorker,text string)(TableRef,bool){return tableTextStoreWith(unsafe.Slice(unsafe.StringData(text),len(text)),w.Alloc)}
func tableTextStoreWith(text []byte,alloc func(int64)(unsafe.Pointer,TableRef))(TableRef,bool){if len(text)==0{return 0,true};if uint64(len(text))>0xffffffff||alloc==nil{return 0,false};p,ref:=alloc(9+int64(len(text)));if p==nil{return 0,false};*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(len(text)))};copy(unsafe.Slice((*byte)(unsafe.Add(p,8)),len(text)),text);*(*byte)(unsafe.Add(p,8+len(text)))=0;return ref,true}
// tableTextPlace places a read's text: in the builder's arena when the carve
// has a worker, else carved from the node's extent, which LoadMeasure counted
// from the framing, so the read path allocates nothing.
func tableTextPlace(c *TableExtentCarve,slot *TableRef,text []byte)bool{*slot=0;if len(text)==0{return true};if uint64(len(text))>0xffffffff{return false};if c.Worker!=nil{ref,ok:=tableTextStoreWith(text,c.Worker.Alloc);if !ok{c.Refused=true;c.Worker.refused=true;return false};*slot=ref;return true};p:=c.Alloc(tableRegionRound(9+int64(len(text))),8);if p==nil{return false};*(*TableBlob)(p)=TableBlob{Length:uint32(uint64(len(text)))};copy(unsafe.Slice((*byte)(unsafe.Add(p,8)),len(text)),text);*(*byte)(unsafe.Add(p,8+len(text)))=0;*slot=int64(uintptr(p)-uintptr(unsafe.Pointer(slot)));return true}
func tableTextExtentAdd(at *int64,n int64)TableRefuseReason{if n==0{return TableRefuseOk};if uint64(n)>0xffffffff{return TableRefuseBlobOverSizeCap};*at=(*at+7)&^7;*at+=tableRegionRound(9+n);return TableRefuseOk}
// tableTextExtentValue packs one unbounded slot into a region's extent.
func tableTextExtentValue(p,q unsafe.Pointer,a *TableArena,region unsafe.Pointer,at *int64)bool{t,ok:=tableTextBytes((*TableRef)(p),a);if !ok{return false};if len(t)==0{if q!=nil{*(*TableRef)(q)=0};return true};*at=(*at+7)&^7;start:=*at;*at+=tableRegionRound(9+int64(len(t)));if q!=nil{dst:=unsafe.Add(region,start);*(*TableBlob)(dst)=TableBlob{Length:uint32(uint64(len(t)))};copy(unsafe.Slice((*byte)(unsafe.Add(dst,8)),len(t)),t);*(*TableRef)(q)=int64(uintptr(dst)-uintptr(q))};return true}
func tableMapStoreKeyed(p unsafe.Pointer,f *TableFieldInfo,k tableMapKey,alloc func(int64)(unsafe.Pointer,TableRef))bool{if !f.Unbounded{tableMapStoreKey(p,f,k);return true};slot:=(*TableRef)(unsafe.Add(p,f.Offset));if k.blob!=0{*slot=k.blob;return true};ref,ok:=tableTextStoreWith(k.text,alloc);*slot=ref;return ok}

// tableTextMalformed names the text list or map whose element or key a read
// refused (docs/SPEC-TABLES.md §2.13.1), so a strict read can say which field.
func tableTextMalformed(r *TableReport,field string){if r!=nil&&r.Reason==""{r.Reason="malformed: "+field}}

// TableMalformedField is a strict read's malformed refusal naming the text
// list or map it refused (docs/SPEC-TABLES.md §2.13.1):
// errors.Is(err, TableRefuseMalformed) holds.
type TableMalformedField struct{ Field string }

func (e TableMalformedField) Error() string { return "malformed: " + e.Field }
func (e TableMalformedField) Is(target error) bool { return target == TableRefuseMalformed }
`

// unboundedTextRuntime is the core runtime with the descriptor's Unbounded
// column, the malformed error and the map key's slot, in a unit that uses
// unbounded text and nowhere else.
func unboundedTextRuntime(u *ir.Unit, s string) string {
	if !ir.UnitHasUnboundedText(u) {
		return s
	}
	s = tableSourceReplace(s, "type TableFieldInfo struct {\n", "type TableFieldInfo struct {\nUnbounded bool // unbounded text (docs/SPEC-TABLES.md §2.13.1): the slot names a blob record\n", 1)
	s = tableSourceReplace(s, "if r.Malformed {return TableRefuseMalformed}", "if r.Malformed {const p=\"malformed: \";if len(r.Reason)>len(p)&&r.Reason[:len(p)]==p{return TableMalformedField{Field:r.Reason[len(p):]}};return TableRefuseMalformed}", 1)
	s = tableSourceReplace(s, "type tableMapKey struct { text []byte; raw uint64 }", "type tableMapKey struct { text []byte; raw uint64; blob TableRef }", 1)
	s = tableSourceReplace(s, "return f.Kind!=12||k.raw==0&&len(k.text)<=int(f.ArrayBound)}", "return f.Kind!=12||k.raw==0&&(f.Unbounded&&uint64(len(k.text))<=0xffffffff&&tableUTF8Valid(k.text)||!f.Unbounded&&len(k.text)<=int(f.ArrayBound))}", 1)
	s = tableSourceReplace(s, "func tableMapStoredKey(p unsafe.Pointer,f *TableFieldInfo)tableMapKey {key:=tableMapKey{};if f.Kind==12 {", "func tableMapStoredKey(p unsafe.Pointer,f *TableFieldInfo,arena ...*TableArena)tableMapKey {key:=tableMapKey{};if f.Unbounded {t,ok:=tableTextBytes((*TableRef)(unsafe.Add(p,f.Offset)),tableOptionalArena(arena));if !ok{return tableMapKey{raw:1}};key.text=t;return key};if f.Kind==12 {", 1)
	return s + unboundedTextRuntimeSource
}

// unboundedTextRegion is the region and container runtime with the slot's
// extent on the wire read and the pack, and the map's arena-mode key reads.
func unboundedTextRegion(u *ir.Unit, s string) string {
	if !ir.UnitHasUnboundedText(u) {
		return s
	}
	s = tableSourceReplace(s, "func tableExtentValue(p,q unsafe.Pointer,f *TableFieldInfo,a *TableArena,region unsafe.Pointer,at *int64) bool {if f.Pointer{return true};", "func tableExtentValue(p,q unsafe.Pointer,f *TableFieldInfo,a *TableArena,region unsafe.Pointer,at *int64) bool {if f.Pointer{return true};if f.Unbounded{return tableTextExtentValue(p,q,a,region,at)};if f.Kind==12&&f.Table!=nil{return tableExtentWalk(p,q,f.Table(),a,region,at)};", 1)
	s = tableSourceReplace(s, "\n if kind==15&&f.Kind==15&&!f.IsArray{if reason:=tableWireUnionExtent(", "\n if kind==12&&f.Unbounded{body,ok:=r.Body();if !ok{return TableRefuseOk};if reason:=tableTextExtentAdd(at,int64(len(body.Buffer)));reason!=TableRefuseOk{return reason};continue}\n if kind==15&&f.Kind==15&&!f.IsArray{if reason:=tableWireUnionExtent(", 1)
	s = tableSourceReplace(s, " if kind!=13&&kind!=15{return TableRefuseOk};for i:=uint64(0);i<count&&r.Has(1);i++{", " if kind==12&&f.Kind==12&&f.Table!=nil&&f.Table().Fields[0].Unbounded{for i:=uint64(0);i<count&&r.Has(1);i++{body,ok:=r.Body();if !ok{return TableRefuseOk};if reason:=tableTextExtentAdd(at,int64(len(body.Buffer)));reason!=TableRefuseOk{return reason}};return TableRefuseOk}\n if kind!=13&&kind!=15{return TableRefuseOk};for i:=uint64(0);i<count&&r.Has(1);i++{", 1)
	s = tableSourceReplace(s, "return tableMapKeyOrder(tableMapStoredKey(a.At(x),key),tableMapStoredKey(a.At(y),key),key)", "return tableMapKeyOrder(tableMapStoredKey(a.At(x),key,a),tableMapStoredKey(a.At(y),key,a),key)", 1)
	s = tableSourceReplace(s, "k:=tableMapStoredKey(a.At(ref),key);if !tableMapKeyValid(k,key)||i>0&&tableMapKeyOrder(tableMapStoredKey(a.At(order[i-1]),key),k,key)>=0", "k:=tableMapStoredKey(a.At(ref),key,a);if !tableMapKeyValid(k,key)||i>0&&tableMapKeyOrder(tableMapStoredKey(a.At(order[i-1]),key,a),k,key)>=0", 1)
	s = tableSourceReplace(s, "if p==nil{return nil};if tableMapKeyOrder(tableMapStoredKey(p,kf),key,kf)==0{return p}};return nil}\nfunc tableMapPlace(", "if p==nil{return nil};if tableMapKeyOrder(tableMapStoredKey(p,kf,a),key,kf)==0{return p}};return nil}\nfunc tableMapPlace(", 1)
	s = tableSourceReplace(s, "f.Table().Reset(p);tableMapStoreKey(p,&f.Table().Fields[0],key);return p,false}", "f.Table().Reset(p);if !tableMapStoreKeyed(p,&f.Table().Fields[0],key,w.Alloc){tableContainerErase(w.Arena,h,p,int64(f.ElemSize));return nil,false};return p,false}", 1)
	s = tableSourceReplace(s, "key.text=body.Buffer;over=len(key.text)>int(f.ArrayBound)}", "key.text=body.Buffer;over=!f.Unbounded&&len(key.text)>int(f.ArrayBound)}", 1)
	return s
}

// unboundedTextJson is the text form's walk with the slot's plain string, in
// a unit that uses unbounded text and nowhere else (§2.13.1, §16.2). walk
// states whether s is the shared JSON walker: the region JSON source reuses the
// walker's helpers but carries none of these six anchors, so that surface is a
// parameter, not a probe of the text.
func unboundedTextJson(u *ir.Unit, s string, walk bool) string {
	if !ir.UnitHasUnboundedText(u) {
		return s
	}
	if !walk {
		return s
	}
	s = tableSourceReplace(s, "func tableJSONWriteScalar(out *tableJSONOut, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\n", "func tableJSONWriteScalar(out *tableJSONOut, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\nif f.Unbounded{t,ok:=tableTextBytes((*TableRef)(storage),nil);if !ok{return false};tableJSONWriteString(out,t);return true}\n", 1)
	s = tableSourceReplace(s, " if f.List {return tableJSONReadList(in,storage,f,depth)}\n", " if f.List {return tableJSONReadList(in,storage,f,depth)}\n if f.Unbounded {return tableJSONReadText(in,(*TableRef)(storage))}\n", 1)
	// The write-element anchor lives in textListJsonSource, which textListJson
	// inserts only for a unit that uses text forms; a unit whose only unbounded
	// text is a map key carries it zero times, so that count is stated here.
	textListCount := 1
	if !unitUsesTextForms(u) {
		textListCount = 0
	}
	s = tableSourceReplace(s, "n:=tableJSONCount(p,v);if n<0||n>v.ArrayBound{return false};tableJSONWriteString(out,tableJSONBytes(unsafe.Add(p,uintptr(v.Offset)),n));return true}", "if v.Unbounded{t,ok:=tableTextBytes((*TableRef)(unsafe.Add(p,uintptr(v.Offset))),nil);if !ok{return false};tableJSONWriteString(out,t);return true};n:=tableJSONCount(p,v);if n<0||n>v.ArrayBound{return false};tableJSONWriteString(out,tableJSONBytes(unsafe.Add(p,uintptr(v.Offset)),n));return true}", textListCount)
	s = tableSourceReplace(s, "if key.Kind==12 {if n>key.ArrayBound {in.report.Clamped++;in.pos=probe.pos;good=false}else{", "if key.Kind==12 {if key.Unbounded {memory,ref:=in.graph.alloc(9+int64(n));if memory==nil{in.bad=true;return false};*(*TableBlob)(memory)=TableBlob{Length:uint32(n)};k.text=unsafe.Slice((*byte)(unsafe.Add(memory,8)),int(n));k.blob=ref;_,ok=in.scanString(k.text)}else if n>key.ArrayBound {in.report.Clamped++;in.pos=probe.pos;good=false}else{", 1)
	s = tableSourceReplace(s, "entry.Reset(slot);tableMapStoreKey(slot,key,k);", "entry.Reset(slot);if !tableMapStoreKeyed(slot,key,k,nil){in.bad=true;return false};", 1)
	s = tableSourceReplace(s, "// ---- json walk: end ----", unboundedTextJsonSource+"// ---- json walk: end ----", 1)
	return s
}

// unboundedTextJsonSource reads one unbounded string into a blob record the
// graph's arena holds (§16.2): never clamped.
const unboundedTextJsonSource = `
func tableJSONReadText(in *tableJSONIn,slot *TableRef)bool{*slot=0;probe:=*in;n,ok:=probe.scanString(nil);if !ok{in.bad=true;return false};if n==0{_,ok=in.scanString(nil);return ok};if in.graph==nil||in.graph.alloc==nil{in.bad=true;return false};memory,ref:=in.graph.alloc(9+int64(n));if memory==nil{in.bad=true;return false};*(*TableBlob)(memory)=TableBlob{Length:uint32(n)};if _,ok=in.scanString(unsafe.Slice((*byte)(unsafe.Add(memory,8)),int(n)));!ok{return false};*slot=ref;return true}
`

// unboundedTextCook is the cook writer with the slot packed into the extent,
// in a unit that uses unbounded text and nowhere else (§2.13.1, §7).
func unboundedTextCook(u *ir.Unit, s string) string {
	if !ir.UnitHasUnboundedText(u) {
		return s
	}
	s = tableSourceReplace(s, " if !f.Pointer&&(f.TypeName==\"string\"", " if f.Unbounded {return w.text(p,dest,live)}\n if !f.Pointer&&(f.TypeName==\"string\"", 1)
	return s + `
func(w *tableCookWriter) text(slot unsafe.Pointer,at int64,live bool)bool{if w.numbering==nil{return false};t,ok:=tableTextBytes((*TableRef)(slot),w.numbering.arena);if !ok{return false};if !live||len(t)==0{return true};w.at=(w.at+7)&^7;start:=w.at;w.at+=tableRegionRound(9+int64(len(t)));w.put(at,uint64(start-at),8);if w.buffer!=nil{w.put(start,uint64(len(t)),4);copy(w.buffer[start+8:start+8+int64(len(t))],t)};return true}
`
}
