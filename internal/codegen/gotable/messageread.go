package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageRead(st *ir.Struct) {
	g.pf("func %sLoadMessageBody(r *TableMessageReader,value *%s)bool {\n%sReset(value);\n", st.Name, g.storageName(st.Name), st.Name)
	if g.retain {
		g.pf("r.Retain.discard(r.Path,false,0);\n")
	}
	g.pf("for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok {r.Report.Malformed=true;return false};if ref==0{return true};entry,ok:=r.Vocabulary.Entry(ref);if !ok||entry.Id>=0xfffffffffffffffd {r.Report.Malformed=true;return false};switch entry.Id {\n")
	for ordinal, f := range st.Fields {
		g.retainOrdinal = ordinal
		g.retainIndex = ""
		mine := ir.TableFieldEntry(f)
		g.pf("case 0x%016x:{\n", mine.Id)
		g.emitMessageCompatibility(f, mine, "entry", "", "\t")
		g.emitMessageReadValue(f, "value."+member(f), "entry", "\t")
		if f.Type.Optional {
			g.pf("value.%sPresent=true\n", member(f))
		}
		if !st.IsMapEntry() || f.Name != "key" {
			g.pf("if widened {r.Report.Widened++}\n")
		} else {
			g.pf("_ = widened\n")
		}
		g.pf("}\n")
	}
	if g.retain {
		g.pf("default:r.Report.Unknown++;if !r.capture(entry){r.Report.Malformed=true;return false}\n}}}\n")
	} else {
		g.pf("default:r.Report.Unknown++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,entry,0){r.Report.Malformed=true;return false}\n}}}\n")
	}
	if g.retain || st.IsMapEntry() || ir.VariableTables(g.unit)[st.Name] {
		return
	}
	g.pf("func %sLoadMessages(values []%s,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {if report==nil {var ignored TableReport;report=&ignored};r,count:=tableMessageBatchOpen(vocabulary,data,report);defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};for i:=int64(0);i<count;i++ {if !%sLoadMessageBody(&r,&values[i]){return i,false}};return count,tableMessageBatchClose(&r)}\n", st.Name, g.storageName(st.Name), st.Name)
}
func (g *tableGen) emitMessageCompatibility(f *ir.Field, mine ir.TableVocabularyEntry, entry, reset, ind string) {
	g.pf("%swidened:=false;if %s.Shape.Kind!=%d||%s.Element.Kind!=%d {\n", ind, entry, mine.Kind, entry, mine.Shape.Elem)
	compat := "false"
	if f != nil && wireWidenable(int(mine.Kind)) {
		compat = fmt.Sprintf("%s.Element.Kind==%d&&tableKindWidens(%s.Shape.Kind,%d)", entry, mine.Shape.Elem, entry, mine.Kind)
	}
	if f != nil && mine.Kind == 14 && !f.IsMap() && wireWidenable(int(mine.Shape.Elem)) {
		compat = fmt.Sprintf("%s.Shape.Kind==14&&tableKindWidens(%s.Element.Kind,%d)", entry, entry, mine.Shape.Elem)
	}
	g.pf("%sif %s {widened=true}else{%s;r.Report.KindMismatch++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,%s,0){r.Report.Malformed=true;return false};break}\n%s}\n", ind, compat, reset, entry, ind)
}
func (g *tableGen) emitMessageReadValue(f *ir.Field, expr, entry, ind string) {
	bad := "r.Report.Malformed=true;return false"
	switch {
	case f.IsMap():
		g.emitMessageReadMap(f, expr, entry, ind)
	case f.IsList():
		g.emitMessageReadList(f, expr, entry, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%s{index,ok:=r.Bits.Get(r.IndexBits);if !ok||r.IndexBits<=0{%s};r.Nodes.Resolve(&%s,index,0x%016x,&r.Report)}\n", ind, bad, expr, pointerTargetId(f))
	case f.Type.Kind == ir.TString:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};text:=r.Bits.Buffer[start:start+int64(n)];if !tableUtf8Valid(text){%s};kept:=tableUtf8Clamp(text,min(int64(n),int64(%d)));if kept<int64(n){r.Report.Clamped++};clear(%s[:]);copy(%s[:],text[:kept]);%sLength=int32(kept)}\n", ind, entry, bad, bad, bad, f.Type.Size, expr, expr, expr)
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};kept:=min(n,uint64(%d));if kept<n{r.Report.Clamped++};clear(%s[:]);copy(%s[:],r.Bits.Buffer[start:start+int64(kept)]);%sLength=int32(kept)}\n", ind, entry, bad, bad, f.Type.Size, expr, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||int64(n)*16>int64(len(r.Bits.Buffer))*8-r.Bits.Offset{%s};kept:=min(n,uint64(%d));if kept<n{r.Report.Clamped++};clear(%s[:]);var high bool;for i:=uint64(0);i<n;i++ {v,_:=r.Bits.Get(16);if v==0||v>=0xdc00&&v<=0xdfff&&!high||high&&(v<0xdc00||v>0xdfff){%s};high=v>=0xd800&&v<=0xdbff;if i<kept {%s[i]=uint16(v)}};if high{%s};if kept>0&&%s[kept-1]>=0xd800&&%s[kept-1]<=0xdbff {kept--};%sLength=int32(kept)}\n", ind, entry, bad, f.Type.Size, expr, bad, expr, bad, expr, expr, expr)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitMessageReadArray(f, expr, entry, ind)
	case isStructRef(f.Type):
		restore := ""
		if g.retain && !g.retainArm {
			restore = g.retainMessagePush(g.retainIndex)
		}
		g.pf("%sif !%sLoadMessageBody(r,&%s){return false}\n", ind, f.Type.Name, expr)
		if restore != "" {
			g.retainMessagePop(restore)
		}
	case isUnionRef(f.Type):
		target := g.nextWireWriter()
		g.pf("{ %s:=&%s\n", target, expr)
		expr = "(*" + target + ")"
		outer := ""
		if g.retain {
			if g.retainIndex != "" {
				outer = g.retainMessagePush(g.retainIndex)
			}
			rdr := "r"
			g.retainDiscard(rdr)
		}
		un := f.Type.Ref.(*ir.Union)
		arm := g.nextWireWriter()
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s.Type=%sTypeNone}else{%s,ok:=r.Vocabulary.Entry(ref);if !ok||%s.Id>=0xfffffffffffffffd||%s.Shape.Kind==0{%s};switch %s.Id{\n", ind, bad, expr, un.Name, arm, arm, arm, bad, arm)
		for ordinal, v := range un.Variants {
			mine := ir.TableArmEntry(v)
			g.pf("case 0x%016x:{\n", mine.Id)
			g.emitMessageCompatibility(v.F, mine, arm, expr+".Type="+un.Name+"TypeNone", ind+"\t")
			g.pf("%s.Type=%sType%s\n", expr, un.Name, ir.GoExportName(v.Name))
			if !v.Void() {
				savedIndex, savedArm := g.retainIndex, g.retainArm
				g.retainIndex, g.retainArm = "", true
				restore := ""
				if g.retain {
					restore = g.retainMessagePush(fmt.Sprint(ordinal + 1))
				}
				g.emitMessageArmReset(un, v, expr)
				g.emitMessageReadValue(v.F, g.unionArmExpr(un, v, expr), arm, ind+"\t")
				if restore != "" {
					g.retainMessagePop(restore)
				}
				g.retainIndex, g.retainArm = savedIndex, savedArm
			}
			g.pf("if widened {r.Report.Widened++}\n}\n")
		}
		extra := ""
		if g.retain {
			extra = "if r.Retain!=nil{r.Report.RetainLost++};"
		}
		g.pf("default:%s.Type=%sTypeNone;r.Report.Unknown++;%sif !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,%s,0){%s}\n}}}\n", expr, un.Name, extra, arm, bad)
		if outer != "" {
			g.retainMessagePop(outer)
		}
		g.pf("}\n")
	case enumRef(f) != nil:
		extra := ""
		if g.retain {
			extra = "if r.Retain!=nil{r.Report.RetainLost++}"
		}
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s=%sNone}else{id,ok:=r.Vocabulary.Name(ref);if !ok{%s};if !%s.TableEnumValue(id){r.Report.Unknown++;%s}}}\n", ind, bad, expr, f.Type.Name, bad, expr, extra)
	default:
		scalar := g.nextWireWriter()
		readerType := "TableReader"
		if g.retain {
			readerType = "tableRetainPlainReader"
		}
		g.pf("%s{var raw [16]byte;if !tableMessageReadScalar(&r.Bits,%s.Shape,&raw){%s};%s:=%s{Buffer:raw[:],Report:&r.Report}\n", ind, entry, bad, scalar, readerType)
		g.emitReadScalar(f, expr, scalar, entry+".Shape.Kind", ind+"\t", bad)
		g.pf("%s}\n", ind)
	}
}
func (g *tableGen) emitMessageReadArray(f *ir.Field, expr, entry, ind string) {
	oldIndex, oldArm := g.retainIndex, g.retainArm
	g.retainIndex = "i"
	g.retainArm = false
	defer func() { g.retainIndex = oldIndex; g.retainArm = oldArm }()
	bad := "r.Report.Malformed=true;return false"
	bound := f.ArrayBound
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
	companion := expr + "Count"
	if f.Type.Kind == ir.TBytes {
		bound = f.Type.Size
		companion = expr + "Length"
	}
	g.pf("%s{base:=&%s;n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok{%s};", ind, expr, entry, bad)
	if f.KeyEnum == "" {
		g.pf("if %s.Element.Kind==6&&!r.Bits.Align(){%s};", entry, bad)
	}
	g.pf("element:=TableMessageEntry{Shape:%s.Element};_ = element;\n", entry)
	if f.KeyEnum != "" {
		g.pf("for i:=uint64(0);i<n;i++ {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};id,ok:=r.Vocabulary.Name(ref);if !ok{%s};var key %s;known:=key.TableEnumValue(id);var scratch %s;p:=&scratch;if known {p=&base[int(key)-1]}else{r.Report.Unknown++}\n", bad, bad, f.KeyEnum, g.storageElementName(f))
		if g.retain {
			g.pf("store:=r.Retain;if !known&&store!=nil{r.Report.RetainLost++;r.Retain=nil}\n")
			g.retainIndex = "int(key)-1"
		}
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t")
		if g.retain {
			g.pf("r.Retain=store\n")
		}
		g.pf("}\n")
	} else {
		g.retainDiscard("r")
		g.pf("keep:=min(n,uint64(%d));if keep<n{r.Report.Clamped++};walk:=n;if element.Shape.Bits>=0{walk=keep};for i:=uint64(0);i<walk;i++ {var scratch %s;p:=&scratch;if i<keep{p=&base[i]}\n", bound, g.storageElementName(f))
		if g.retain {
			g.pf("store:=r.Retain;if i>=keep{r.Retain=nil}\n")
		}
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t")
		if g.retain {
			g.pf("r.Retain=store\n")
		}
		g.pf("};if walk<n&&!r.Bits.Skip(int64(n-walk)*element.Shape.Bits){%s}\n", bad)
		if counted {
			g.pf("%s=int32(keep)\n", companion)
		}
	}
	g.pf("%s}\n", ind)
}
func (g *tableGen) storageElementName(f *ir.Field) string {
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		return "byte"
	}
	if f.Type.Pointer {
		return "TableRef"
	}
	if g.regional && isStructRef(f.Type) {
		return g.storageName(f.Type.Name)
	}
	return goFieldType(f.Type)
}
