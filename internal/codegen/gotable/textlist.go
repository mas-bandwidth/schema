// Text lists and the array root in the Go table backend (docs/SPEC-TABLES.md
// §2.13, §16.10).
//
// A text list's storage is an array of its generated element record, so the
// storage, the builder, the cook, the block form, the view and retain walk it
// as the array of records it is. What this file adds is the plain list's wire
// element (kind 12, its own L and bytes), the read-side clamp that names the
// field, the generated accessors, and the text form's plain array of strings
// and top-level array — the last two as replacements on the runtime sources,
// made only in a unit that uses the construct, so every other unit's output is
// byte-identical to what it was.
package gotable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// textListCode is the descriptor's TextList column: 1 for string(M) elements,
// 2 for ?string(M).
func textListCode(f *ir.Field) int {
	if ir.TextListOptional(f) {
		return 2
	}
	return 1
}

// jsonArrayColumn is the descriptor column of a `json = "array"` table (§16.10).
func jsonArrayColumn(st *ir.Struct) string {
	if st.JsonArray {
		return "JsonArray: true, "
	}
	return ""
}

// textValueHolder names the text list (`Table.field`) whose element record's
// value f is, and "" for every other field.
func (g *tableGen) textValueHolder(f *ir.Field) string {
	for _, st := range g.unit.Tables {
		if st.TextElem && len(st.Fields) == 1 && st.Fields[0] == f {
			return st.MapEntryOf
		}
	}
	return ""
}

// emitReadTextElement reads one element of a plain text list (§2.13, §3): its
// own L, then L bytes of well-formed UTF-8 with no zero byte, clamped at M on a
// code point boundary and counted, the field named in the report.
func (g *tableGen) emitReadTextElement(f *ir.Field, expr, ind string) {
	v := ir.TextListValue(f)
	m := v.Type.Size
	value := expr + "." + member(v)
	g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; break }\n", ind)
	g.pf("%sif !tableUtf8Valid(elem.Buffer) { r.Report.Malformed = true; %sReset(&%s); break }\n", ind, f.Type.Name, expr)
	g.pf("%skeep := int64(len(elem.Buffer)); if keep > %d { keep = tableUtf8Clamp(elem.Buffer, %d); r.Report.Clamped++; tableTextClamped(r.Report, %q) }\n", ind, m, m, f.TextListElem().MapEntryOf)
	g.pf("%sclear(%s[:]); copy(%s[:keep], elem.Buffer); %sLength = int32(keep)\n", ind, value, value, value)
}

// emitTextListSurface emits a text list's accessors (§2.13): TextAt, Append and,
// for the ? spelling, AppendAbsent.
func (g *tableGen) emitTextListSurface(st *ir.Struct, f *ir.Field) {
	name := st.Name + ir.GoExportName(f.Name)
	storage := g.storageName(st.Name)
	elem := f.Type.Name
	v := ir.TextListValue(f)
	m := v.Type.Size
	field := member(f)
	val := member(v)
	optional := ir.TextListOptional(f)
	absent := ""
	present := ""
	if optional {
		absent = "||!p." + val + "Present"
		present = "p." + val + "Present=true;"
	}
	g.pf("// %sTextAt is element i of %s.%s (docs/SPEC-TABLES.md §2.13): a view into the\n", name, st.Name, f.Name)
	g.pf("// storage that allocates nothing, and false past the count or for an absent element.\n")
	if f.IsList() {
		g.pf("func %sTextAt(value *%s,i int32,arena ...*TableArena)([]byte,bool){if value==nil{return nil,false};p:=value.%s.At(i,arena...);if p==nil||p.%sLength<0||p.%sLength>%d%s{return nil,false};return p.%s[:p.%sLength],true}\n",
			name, storage, field, val, val, m, absent, val, val)
	} else {
		g.pf("func %sTextAt(value *%s,i int32,arena ...*TableArena)([]byte,bool){if value==nil||i<0||i>=value.%sCount||value.%sCount>%d{return nil,false};p:=&value.%s[i];if p.%sLength<0||p.%sLength>%d%s{return nil,false};return p.%s[:p.%sLength],true}\n",
			name, storage, field, field, f.ArrayBound, field, val, val, m, absent, val, val)
	}
	// the write-side text rule (§2.13): the element past M, a zero byte and
	// ill-formed UTF-8 are refused at the append, as Save refuses them
	valid := "if len(text)>%d||!tableUtf8Valid(unsafe.Slice(unsafe.StringData(text),len(text))){return false};"
	g.needsUnsafe()
	// the total (§2.13): the element that would carry the sum past it is
	// refused, as the element past the count is
	if f.TextTotal > 0 {
		arena := ""
		if f.IsList() {
			valid += "if worker==nil||worker.Arena==nil{return false};"
			arena = "worker.Arena"
		}
		valid += g.textBytesBlock(f, "value."+field, arena, "int64(len(text))", "return false")
	}
	g.pf("// %sAppend appends one element to %s.%s, false past either bound (docs/SPEC-TABLES.md §2.13).\n", name, st.Name, f.Name)
	if f.IsList() {
		g.pf("func %sAppend(worker *TableWorker,value *%s,text string)bool{"+valid+"p:=%sAdd(worker,&value.%s);if p==nil{return false};copy(p.%s[:],text);p.%sLength=int32(len(text));%sreturn true}\n",
			name, storage, m, name, field, val, val, present)
		if optional {
			g.pf("// %sAppendAbsent appends one absent element to %s.%s.\n", name, st.Name, f.Name)
			g.pf("func %sAppendAbsent(worker *TableWorker,value *%s)bool{return %sAdd(worker,&value.%s)!=nil}\n", name, storage, name, field)
		}
	} else {
		g.pf("func %sAppend(value *%s,text string)bool{"+valid+"if value.%sCount<0||value.%sCount>=%d{return false};p:=&value.%s[value.%sCount];%sReset(p);copy(p.%s[:],text);p.%sLength=int32(len(text));%svalue.%sCount++;return true}\n",
			name, storage, m, field, field, f.ArrayBound, field, field, elem, val, val, present, field)
		if optional {
			g.pf("// %sAppendAbsent appends one absent element to %s.%s.\n", name, st.Name, f.Name)
			g.pf("func %sAppendAbsent(value *%s)bool{if value.%sCount<0||value.%sCount>=%d{return false};%sReset(&value.%s[value.%sCount]);value.%sCount++;return true}\n",
				name, storage, field, field, f.ArrayBound, elem, field, field, field)
		}
	}
}

// textBytesBlock is the statement that sums a text list's element bytes from
// start and runs fail past its total (docs/SPEC-TABLES.md §2.13): an absent
// element of the ? spelling counts zero. arena walks an unbounded list.
func (g *tableGen) textBytesBlock(f *ir.Field, expr, arena, start, fail string) string {
	val := member(ir.TextListValue(f))
	add := "if "
	if ir.TextListOptional(f) {
		add += "p." + val + "Present&&"
	}
	add += "p." + val + "Length>0{spent+=int64(p." + val + "Length)}"
	if f.IsList() {
		elem := containerElementType(g.unit, f)
		return fmt.Sprintf("{spent:=%s;c:=tableContainerCursorBegin((*tableContainer)(unsafe.Pointer(&%s)),int64(unsafe.Sizeof(*new(%s))),%s);for i:=int32(0);i<%s.Count;i++{p:=(*%s)(c.Next());if p==nil{%s};%s};if spent>%d{%s}};",
			start, expr, elem, arena, expr, elem, fail, add, f.TextTotal, fail)
	}
	return fmt.Sprintf("{spent:=%s;for i:=int32(0);i<%sCount&&i<%d;i++{p:=&%s[i];%s};if spent>%d{%s}};",
		start, expr, f.ArrayBound, expr, add, f.TextTotal, fail)
}

// emitTextTotalWrite refuses, in SaveBody and so in Save, Measure and every
// nested or retaining write, a text list whose element bytes sum past its
// total (docs/SPEC-TABLES.md §2.13): -1 with nothing written, as a count past
// [..N] is refused.
func (g *tableGen) emitTextTotalWrite(f *ir.Field, expr, arena, ind string) {
	if !f.IsTextList() || f.TextTotal <= 0 {
		return
	}
	g.needsUnsafe()
	g.pf("%s// the text list's total (docs/SPEC-TABLES.md §2.13)\n%s%s\n", ind, ind, g.textBytesBlock(f, expr, arena, "int64(0)", "return false"))
}

// emitTextTotalRead is the read side of a text list's total (docs/SPEC-TABLES.md
// §2.13, §4), after one element is read into elem: the running sum takes the
// element, and the first element that would carry it past the total is reset
// and dropped with every element after it, clamped counted once and the field
// named. The enclosing element loop declared spent.
func (g *tableGen) emitTextTotalRead(f *ir.Field, elem, ind string) {
	if !f.IsTextList() || f.TextTotal <= 0 {
		return
	}
	val := member(ir.TextListValue(f))
	present := ""
	if ir.TextListOptional(f) {
		present = elem + "." + val + "Present&&"
	}
	g.pf("%sif %s%s.%sLength>0 {spent+=int64(%s.%sLength)}; if spent>%d {r.Report.Clamped++;tableTextClamped(r.Report,%q);%sReset(&%s);break}\n",
		ind, present, elem, val, elem, val, f.TextTotal, f.TextListElem().MapEntryOf, f.Type.Name, elem)
}

func unitUsesTextForms(u *ir.Unit) bool {
	return ir.UnitHasTextLists(u) || len(ir.TableArrayRoots(u)) > 0
}

// textListRuntimeSource is the runtime a text-list unit adds: the clamp that
// names its field (§2.13) and the typed error ReadStrict returns for it.
const textListRuntimeSource = `
// tableTextClamped names the text list whose count or element a read clamped
// (docs/SPEC-TABLES.md §2.13), so a strict read can say which field.
func tableTextClamped(r *TableReport,field string){if r!=nil&&r.Reason==""{r.Reason="clamped: "+field}}

// TableClampedField is a strict read's clamped refusal naming the text list it
// clamped (docs/SPEC-TABLES.md §2.13): errors.Is(err, TableRefuseClamped) holds.
type TableClampedField struct{ Field string }

func (e TableClampedField) Error() string { return "clamped: " + e.Field }
func (e TableClampedField) Is(target error) bool { return target == TableRefuseClamped }
`

// textListRuntime is the core runtime with the two descriptor columns and the
// clamped error, in a unit that uses a text list or an array root and nowhere
// else.
func textListRuntime(u *ir.Unit, s string) string {
	if !unitUsesTextForms(u) {
		return s
	}
	s = tableSourceReplace(s, "type TableFieldInfo struct {\n", "type TableFieldInfo struct {\nTextList uint8 // a text list (docs/SPEC-TABLES.md §2.13): 1 string(M) elements, 2 ?string(M)\n", 1)
	if ir.UnitHasTextTotals(u) {
		s = tableSourceReplace(s, "TextList uint8 // a text list (docs/SPEC-TABLES.md §2.13): 1 string(M) elements, 2 ?string(M)\n", "TextList uint8 // a text list (docs/SPEC-TABLES.md §2.13): 1 string(M) elements, 2 ?string(M)\nTextTotal int64 // its | total = N (§2.13): the most bytes its elements hold in all, 0 for none\nTextTotalOf string // the Table.field a clamp of the total names\n", 1)
	}
	s = tableSourceReplace(s, "type TableTypeInfo struct {\n", "type TableTypeInfo struct {\nJsonArray bool // json = \"array\" (docs/SPEC-TABLES.md §16.10): the text is the one field's array\n", 1)
	s = tableSourceReplace(s, "if r.Clamped>0 {return TableRefuseClamped}", "if r.Clamped>0 {const p=\"clamped: \";if len(r.Reason)>len(p)&&r.Reason[:len(p)]==p{return TableClampedField{Field:r.Reason[len(p):]}};return TableRefuseClamped}", 1)
	return s + textListRuntimeSource
}

// textListJsonSource is the text form's plain element of a text list (§16.2).
const textListJsonSource = `
// tableJsonWriteTextElement writes one text-list element (docs/SPEC-TABLES.md
// §2.13, §16.2): its string, or null for an absent ?string(M) element.
func tableJsonWriteTextElement(out *tableJsonOut,p unsafe.Pointer,f *TableFieldInfo)bool{v:=&f.Table().Fields[0];if f.TextList==2&&tableJsonGetRaw(unsafe.Add(p,uintptr(v.PresentOffset)),1)==0{out.text("null");return true};n:=tableJsonCount(p,v);if n<0||n>v.ArrayBound{return false};tableJsonWriteString(out,tableJsonBytes(unsafe.Add(p,uintptr(v.Offset)),n));return true}

// tableJsonReadTextElement reads one: a string, or null where the element may be
// absent; the caller's shape check has already counted every other JSON type.
func tableJsonReadTextElement(in *tableJsonIn,p unsafe.Pointer,f *TableFieldInfo,depth int32)bool{t:=f.Table();t.Reset(p);if in.valueShape()=='z'{return in.literal("null")};v:=&t.Fields[0];if !tableJsonReadField(in,p,v,depth){return false};if f.TextList==2{tableJsonSetRaw(unsafe.Add(p,uintptr(v.PresentOffset)),1,1)};return true}
`

// textListJson is the text form's walk with the text list's plain array and the
// array root's top-level array, in a unit that uses one and nowhere else. walk
// states whether s is the shared JSON walker: the region JSON source reuses the
// walker by calling into it and carries none of these ten anchors, so that
// surface is a parameter, not a probe of the text.
func textListJson(u *ir.Unit, s string, walk bool) string {
	if !unitUsesTextForms(u) {
		return s
	}
	if !walk {
		return s
	}
	s = tableSourceReplace(s, "func tableJsonShape(f *TableFieldInfo) byte {\n", "func tableJsonShape(f *TableFieldInfo) byte {\nif f.TextList!=0{return 'a'};if f.Kind==13&&!f.Pointer&&!f.IsArray&&!f.List&&f.Table!=nil&&f.Table().JsonArray{return 'a'}\n", 1)
	s = tableSourceReplace(s, "func tableJsonElementShape(f *TableFieldInfo) byte {\n", "func tableJsonElementShape(f *TableFieldInfo) byte {\nif f.TextList!=0{return 's'};if f.Kind==13&&!f.Pointer&&f.Table!=nil&&f.Table().JsonArray{return 'a'}\n", 1)
	s = tableSourceReplace(s, "func tableJsonWriteScalar(out *tableJsonOut, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\n", "func tableJsonWriteScalar(out *tableJsonOut, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\nif f.TextList!=0{return tableJsonWriteTextElement(out,storage,f)}\n", 1)
	s = tableSourceReplace(s, "func tableJsonReadScalar(in *tableJsonIn, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\n", "func tableJsonReadScalar(in *tableJsonIn, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\nif f.TextList!=0{return tableJsonReadTextElement(in,storage,f,depth)}\n", 1)
	s = tableSourceReplace(s, "\tif f.Kind == 12 {\n\t\ttableJsonWriteString(out, tableJsonBytes(storage, tableJsonCount(base, f)))", "\tif f.Kind == 12 && f.TextList == 0 {\n\t\ttableJsonWriteString(out, tableJsonBytes(storage, tableJsonCount(base, f)))", 1)
	s = tableSourceReplace(s, "\tif f.Kind == 12 {\n\t\tlength, ok := in.scanString(", "\tif f.Kind == 12 && f.TextList == 0 {\n\t\tlength, ok := in.scanString(", 1)
	s = tableSourceReplace(s, "!(f.Pointer && in.valueShape()=='z')", "!((f.Pointer||f.TextList==2) && in.valueShape()=='z')", 2)
	// The list reader carries the unspaced historical spelling, exactly once.
	s = tableSourceReplace(s, "!(f.Pointer&&in.valueShape()=='z')", "!((f.Pointer||f.TextList==2)&&in.valueShape()=='z')", 1)
	s = tableSourceReplace(s, " if depth>tableJsonMaxDepth{return false}\n any := label!=0\n", " if depth>tableJsonMaxDepth{return false}\n if info.JsonArray&&label==0&&len(info.Fields)==1{return tableJsonWriteField(out,base,&info.Fields[0],depth)}\n any := label!=0\n", 1)
	s = tableSourceReplace(s, "func tableJsonReadTable(in *tableJsonIn, base unsafe.Pointer, info *TableTypeInfo, depth int32) bool {\n\tif depth > tableJsonMaxDepth {\n\t\tin.bad = true\n\t\treturn false\n\t}\n", "func tableJsonReadTable(in *tableJsonIn, base unsafe.Pointer, info *TableTypeInfo, depth int32) bool {\n\tif depth > tableJsonMaxDepth {\n\t\tin.bad = true\n\t\treturn false\n\t}\n"+"\tif info.JsonArray&&len(info.Fields)==1 {if in.peek()!='['{if in.report!=nil&&in.report.Reason==\"\"{in.report.Reason=\"json_root_not_array: \"+info.Name};in.bad=true;return false};return tableJsonReadField(in,base,&info.Fields[0],depth)}\n", 1)
	s = tableSourceReplace(s, "// ---- json walk: end ----", textListJsonSource+"// ---- json walk: end ----", 1)
	return textTotalJson(u, s)
}

// textTotalJsonSource is the text form's half of a text list's total (§2.13).
const textTotalJsonSource = `
// tableJsonTextBytes is the byte total of count text-list elements at p
// (docs/SPEC-TABLES.md §2.13): an absent ?string(M) element counts zero.
func tableJsonTextBytes(p unsafe.Pointer,count int32,f *TableFieldInfo)int64{v:=&f.Table().Fields[0];spent:=int64(0);for i:=int32(0);i<count;i++{e:=unsafe.Add(p,uintptr(i)*uintptr(f.ElemSize));if f.TextList==2&&tableJsonGetRaw(unsafe.Add(e,uintptr(v.PresentOffset)),1)==0{continue};if n:=tableJsonCount(e,v);n>0{spent+=int64(n)}};return spent}

// tableJsonTextTotalKeep adds one read element to the running total and says
// whether it is kept: the first element past the total is reset, counted
// clamped once and the field named (docs/SPEC-TABLES.md §2.13).
func tableJsonTextTotalKeep(in *tableJsonIn,p unsafe.Pointer,f *TableFieldInfo,spent *int64)bool{*spent+=tableJsonTextBytes(p,1,f);if *spent<=f.TextTotal{return true};f.Table().Reset(p);in.report.Clamped++;tableTextClamped(in.report,f.TextTotalOf);return false}
`

// textTotalJson is the text form's walk holding a text list's total (§2.13):
// the write refuses a list past it, and the read keeps the prefix within it,
// in a unit that declares one and nowhere else. Each anchor is a stated
// surface: only a unit that builds the reader/writer shape (bounded vs
// unbounded list, container graph) carries it, so the presence check before
// the exact-count replace is a decision the code states, not a silent skip; a
// present anchor is replaced with its exact count and fails closed if it moves.
func textTotalJson(u *ir.Unit, s string) string {
	if !ir.UnitHasTextTotals(u) {
		return s
	}
	rep := func(old, new string) {
		if strings.Contains(s, old) {
			s = tableSourceReplace(s, old, new, -1)
		}
	}
	rep("\tif f.IsArray {\n\t\tcount := tableJsonCount(base, f)\n", "\tif f.IsArray {\n\t\tcount := tableJsonCount(base, f)\n\t\tif f.TextTotal>0&&tableJsonTextBytes(storage,count,f)>f.TextTotal{return false}\n")
	rep("\t\ttableJsonSetCount(base, f, 0)\n\t\tplaced := int32(0)\n\t\tshape := tableJsonElementShape(f)\n", "\t\ttableJsonSetCount(base, f, 0)\n\t\tplaced := int32(0)\n\t\tshape := tableJsonElementShape(f)\n\t\tspent, full := int64(0), false\n")
	rep("\t\t\tcase placed >= f.ArrayBound:\n", "\t\t\tcase full:\n\t\t\t\tif !in.skipValue(depth + 1) {\n\t\t\t\t\treturn false\n\t\t\t\t\t}\n\t\t\tcase placed >= f.ArrayBound:\n")
	rep("\t\t\t\tif !tableJsonReadScalar(in, unsafe.Add(storage, uintptr(placed)*uintptr(f.ElemSize)), f, depth+1) {\n\t\t\t\t\treturn false\n\t\t\t\t\t}\n\t\t\t\tplaced++\n", "\t\t\t\tif !tableJsonReadScalar(in, unsafe.Add(storage, uintptr(placed)*uintptr(f.ElemSize)), f, depth+1) {\n\t\t\t\t\treturn false\n\t\t\t\t\t}\n\t\t\t\tif f.TextTotal>0&&!tableJsonTextTotalKeep(in,unsafe.Add(storage,uintptr(placed)*uintptr(f.ElemSize)),f,&spent){full=true}else{placed++}\n")
	rep("ref:=*(*int64)(holder);if ref==0{return false};p:=unsafe.Add(holder,ref);out.put('[');", "ref:=*(*int64)(holder);if ref==0{return false};p:=unsafe.Add(holder,ref);if f.TextTotal>0&&tableJsonTextBytes(p,count,f)>f.TextTotal{return false};out.put('[');")
	rep("shape:=tableJsonElementShape(f)\n for {c:=in.peek();if c==']'{in.pos++;return true};if c==0{in.bad=true;return false};p:=in.graph.listAdd(", "shape:=tableJsonElementShape(f);spent,full:=int64(0),false\n for {c:=in.peek();if c==']'{in.pos++;return true};if c==0{in.bad=true;return false};if full{if !in.skipValue(depth+1){return false};c=in.peek();if c==','{in.pos++;continue};if c==']'{in.pos++;return true};in.bad=true;return false};p:=in.graph.listAdd(")
	rep("else if !tableJsonReadScalar(in,p,f,depth+1){return false};c=in.peek()", "else if !tableJsonReadScalar(in,p,f,depth+1){return false}else if f.TextTotal>0&&!tableJsonTextTotalKeep(in,p,f,&spent){full=true;if in.graph.listDrop==nil{in.bad=true;return false};in.graph.listDrop(holder,p,int64(f.ElemSize))};c=in.peek()")
	rep(" listAdd func(unsafe.Pointer,int64)unsafe.Pointer\n", " listAdd func(unsafe.Pointer,int64)unsafe.Pointer\n listDrop func(unsafe.Pointer,unsafe.Pointer,int64)\n")
	rep("listAdd:func(p unsafe.Pointer,size int64)unsafe.Pointer{return tableContainerAppend(worker,(*tableContainer)(p),size)},", "listAdd:func(p unsafe.Pointer,size int64)unsafe.Pointer{return tableContainerAppend(worker,(*tableContainer)(p),size)},listDrop:func(p,e unsafe.Pointer,size int64){tableContainerErase(worker.Arena,(*tableContainer)(p),e,size)},")
	if strings.Contains(s, "// ---- json walk: end ----") {
		s = tableSourceReplace(s, "// ---- json walk: end ----", textTotalJsonSource+"// ---- json walk: end ----", 1)
	}
	return s
}
