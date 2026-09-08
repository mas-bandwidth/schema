package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *tableGen) emitMessageArmReset(un *ir.Union, v ir.UnionVariant, expr string) {
	if g.regional {
		g.pf("{value:=%s.%s();", expr, ir.GoExportName(v.Name))
		f := *v.F
		f.Name = "value"
		g.emitTableResetField(&f)
		g.pf("}\n")
	} else {
		// The ordinary fixed storage keeps an arm as fields of its union.
		g.pf("{value:=&%s;", expr)
		f := *v.F
		f.Name = v.Name
		g.emitTableResetField(&f)
		g.pf("}\n")
	}
}

func (g *tableGen) emitMessageReadList(f *ir.Field, expr, entry, ind string) {
	oldIndex, oldArm := g.retainIndex, g.retainArm
	g.retainIndex = "i"
	g.retainArm = false
	defer func() { g.retainIndex = oldIndex; g.retainArm = oldArm }()
	typ := containerElementType(g.unit, f)
	size, align := ir.ListElementLayout(g.unit, f)
	g.pf("%s{count,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok {r.Report.Malformed=true;return false};if %s.Element.Kind==6&&!r.Bits.Align(){r.Report.Malformed=true;return false};holder:=(*tableContainer)(unsafe.Pointer(&%s));%sif !tableContainerFill(&r.Nodes.Carve,holder,count,%d,%d){r.Report.Malformed=true;return false};holder.Count=0;element:=TableMessageEntry{Shape:%s.Element};_ = element;for i:=uint64(0);i<count;i++ {p:=(*%s)(tableContainerFillAt(&r.Nodes.Carve,holder,int32(i),%d));if p==nil{r.Report.Malformed=true;return false};holder.Count++;\n", ind, entry, entry, expr, g.retainMessageDiscardCode(), size, align, entry, typ, size)
	g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t")
	g.pf("}}\n")
}
func (g *tableGen) emitMessageReadMap(f *ir.Field, expr, entry, ind string) {
	typ := containerElementType(g.unit, f)
	layout := ir.RecordLayout(g.unit, f.MapEntry)
	g.pf("%s{count,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok {r.Report.Malformed=true;return false};field:=%s;holder:=(*tableContainer)(unsafe.Pointer(&%s));%sif !tableContainerFill(&r.Nodes.Carve,holder,count,%d,%d){r.Report.Malformed=true;return false};holder.Count=0;var last tableMapKey;var prior *%s;mapWidened:=false;for i:=uint64(0);i<count;i++ {key,end,bad,over,wide,whole:=tableMessageMapReadKey(*r,&field.Table().Fields[0]);if !whole{r.Report.Malformed=true;return false};if wide&&!mapWidened{mapWidened=true;r.Report.Widened++};if bad{*holder=tableContainer{};r.Report.KindMismatch++;r.Bits.Offset=end;for j:=i+1;j<count;j++ {if !tableMessageSkipBody(&r.Bits,r.Vocabulary,r.IndexBits,0){r.Report.Malformed=true;return false}};break};if over {r.Report.Clamped++;r.Bits.Offset=end;continue};order:= -1;if prior!=nil{order=tableMapKeyOrder(last,key,&field.Table().Fields[0])};if order>0{r.Report.Malformed=true;return false};p:=prior;if order==0 {r.Report.Duplicate++}else{p=(*%s)(tableContainerFillAt(&r.Nodes.Carve,holder,holder.Count,%d));if p==nil {r.Report.Malformed=true;return false};holder.Count++};\n", ind, entry, g.mapDescriptor(f), expr, g.retainMessageDiscardCode(), layout.Size, layout.Align, typ, typ, layout.Size)
	restore := ""
	if g.retain {
		restore = g.retainMessagePush("holder.Count-1")
	}
	g.pf("if !%sLoadMessageBody(r,p){return false};\n", f.MapEntry.Name)
	if restore != "" {
		g.retainMessagePop(restore)
	}
	g.pf("if r.Bits.Offset!=end{r.Report.Malformed=true;return false};last=key;prior=p}}\n")
}
