package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageRead(st *ir.Struct) {
	g.pf("func %sLoadMessageBody(r *TableMessageReader,value *%s)bool {\n%sReset(value);\n", st.Name, g.storageName(st.Name), st.Name)
	g.pf("for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok {r.Report.Malformed=true;return false};if ref==0{return true};entry,ok:=r.Vocabulary.Entry(ref);if !ok||entry.ID>=0xfffffffffffffffd {r.Report.Malformed=true;return false};switch entry.ID {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("case 0x%016x:{\n", mine.Id)
		g.emitMessageCompatibility(f, mine, "entry", "", "\t")
		g.emitMessageReadValue(f, "value."+member(f), "entry", "\t", false)
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
	g.pf("default:r.Report.Unknown++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,entry,0){r.Report.Malformed=true;return false}\n}}}\n")
	// THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3): the
	// batch is a fixed root's, and a variable table has no message form.
	if !st.IsTable || !st.FixedDeclared || st.IsMapEntry() {
		return
	}
	// THE PLAIN NAME IS THE STRICT READ, THE TOLERANT ONE IS NAMED (docs/SPEC-TABLES.md §6.5).
	g.pf("// %sLoadMessagesTolerant is the TOLERANT message read (docs/SPEC-TABLES.md §6.5, §3.3): it keeps every\n// recovered value, with the counters counted onto the report passed.\n", st.Name)
	g.pf("// %sLoadMessages is the plain message read, and it is STRICT (§6.5): ok is false for whatever\n// %sReadStrict refuses, and a batch refused by the strict rule alone answers a count of 0 with every\n// value it had written reset. A supplied report receives THIS read's report; report.Err() is the reason.\n", st.Name, st.Name)
	g.pf("func %sLoadMessages(values []%s,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {read:=report;if read==nil{read=&TableReport{}}else{*read=TableReport{}};count,ok:=%sLoadMessagesTolerant(values,vocabulary,data,read);if !ok{return count,false};if read.Err()!=nil{for i := range count {%sReset(&values[i])};return 0,false};return count,true}\n", st.Name, g.storageName(st.Name), st.Name, st.Name)
	// THE READER CHECKS THE BATCH TRAILER BEFORE IT DECODES ANY BODY
	// (docs/SPEC-TABLES.md §2.14, §3.3): a declaring root opens through the
	// sealed open; in a unit where no table declares the key the original open
	// is kept, so a keyless unit emits the bytes it emitted before.
	if len(ir.TableIntegrityTables(g.unit)) > 0 {
		sealed := "false"
		if st.Integrity != "" {
			sealed = "true"
		}
		g.pf("func %sLoadMessagesTolerant(values []%s,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {if report==nil {var ignored TableReport;report=&ignored};r,count:=tableMessageBatchOpenSealed(vocabulary,data,report,"+sealed+");defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};for i := range count {if !%sLoadMessageBody(&r,&values[i])||r.Report.NonFiniteFloat!=\"\"{if tableNonFiniteRefused(&r.Report){for j := range i+1 {%sReset(&values[j])};return 0,false};return i,false}};return count,tableMessageBatchClose(&r)}\n", st.Name, g.storageName(st.Name), st.Name, st.Name)
		return
	}
	g.pf("func %sLoadMessagesTolerant(values []%s,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {if report==nil {var ignored TableReport;report=&ignored};r,count:=tableMessageBatchOpen(vocabulary,data,report);defer func(){*report=r.Report}();if count<0{return 0,false};if count>int64(len(values)){return count,tableMessageRefuse(&r.Report,\"batch_too_large\")};for i := range count {if !%sLoadMessageBody(&r,&values[i])||r.Report.NonFiniteFloat!=\"\"{if tableNonFiniteRefused(&r.Report){for j := range i+1 {%sReset(&values[j])};return 0,false};return i,false}};return count,tableMessageBatchClose(&r)}\n", st.Name, g.storageName(st.Name), st.Name, st.Name)
}
func (g *tableGen) emitMessageCompatibility(f *ir.Field, mine ir.TableVocabularyEntry, entry, reset, ind string) {
	g.pf("%swidened:=false;if %s.Shape.Kind!=%d||%s.Element.Kind!=%d {\n", ind, entry, mine.Kind, entry, mine.Shape.Elem)
	compat := "false"
	if f != nil && wireWidenable(int(mine.Kind)) {
		compat = fmt.Sprintf("%s.Element.Kind==%d&&tableKindWidens(%s.Shape.Kind,%d)", entry, mine.Shape.Elem, entry, mine.Kind)
	}
	if f != nil && mine.Kind == 14 && wireWidenable(int(mine.Shape.Elem)) {
		compat = fmt.Sprintf("%s.Shape.Kind==14&&tableKindWidens(%s.Element.Kind,%d)", entry, entry, mine.Shape.Elem)
	}
	g.pf("%sif %s {widened=true}else{%s;r.Report.KindMismatch++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,%s,0){r.Report.Malformed=true;return false};break}\n%s}\n", ind, compat, reset, entry, ind)
}
func (g *tableGen) emitMessageReadValue(f *ir.Field, expr, entry, ind string, element bool) {
	bad := "r.Report.Malformed=true;return false"
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};text:=r.Bits.Buffer[start:start+int64(n)];if !tableUTF8Valid(text){%s};kept:=tableUTF8Clamp(text,min(int64(n),int64(%d)));if kept<int64(n){r.Report.Clamped++};clear(%s[:]);copy(%s[:],text[:kept]);%sLength=int32(kept)}\n", ind, entry, bad, bad, bad, f.Type.Size, expr, expr, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};kept:=min(n,uint64(%d));if kept<n{r.Report.Clamped++};clear(%s[:]);copy(%s[:],r.Bits.Buffer[start:start+int64(kept)]);%sLength=int32(kept)}\n", ind, entry, bad, bad, f.Type.Size, expr, expr, expr)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitMessageReadArray(f, expr, entry, ind)
	case isStructRef(f.Type):
		g.pf("%sif !%sLoadMessageBody(r,&%s){return false}\n", ind, f.Type.Name, expr)
	case isUnionRef(f.Type):
		target := g.nextWireWriter()
		g.pf("{ %s:=&%s\n", target, expr)
		expr = "(*" + target + ")"
		un := f.Type.Ref.(*ir.Union)
		arm := g.nextWireWriter()
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s.Type=%sTypeNone}else{%s,ok:=r.Vocabulary.Entry(ref);if !ok||%s.ID>=0xfffffffffffffffd||%s.Shape.Kind==0{%s};switch %s.ID{\n", ind, bad, expr, un.Name, arm, arm, arm, bad, arm)
		for _, v := range un.Variants {
			mine := ir.TableArmEntry(v)
			g.pf("case 0x%016x:{\n", mine.Id)
			g.emitMessageCompatibility(v.F, mine, arm, expr+".Type="+un.Name+"TypeNone", ind+"\t")
			g.pf("%s.Type=%sType%s\n", expr, un.Name, ir.GoExportName(v.Name))
			if !v.Void() {
				g.emitMessageArmReset(un, v, expr)
				g.emitMessageReadValue(v.F, g.unionArmExpr(un, v, expr), arm, ind+"\t", false)
			}
			g.pf("if widened {r.Report.Widened++}\n}\n")
		}
		g.pf("default:%s.Type=%sTypeNone;r.Report.Unknown++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,%s,0){%s}\n}}}\n", expr, un.Name, arm, bad)
		g.pf("}\n")
	case enumRef(f) != nil:
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s=%sNone}else{id,ok:=r.Vocabulary.Name(ref);if !ok{%s};if !%s.TableEnumValue(id){r.Report.Unknown++}", ind, bad, expr, f.Type.Name, bad, expr)
		if enumHasDeprecated(enumRef(f)) {
			land := fieldDefaultExpr(f)
			if element {
				land = f.Type.Name + "None"
			}
			// A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and
			// as the ELEMENT default for an element (§2.15), never unknown.
			g.pf("else if %s.TableEnumDeprecated() { %s = %s }", expr, expr, land)
		}
		g.pf("}}\n")
	default:
		scalar := g.nextWireWriter()
		g.pf("%s{var raw [16]byte;if !tableMessageReadScalar(&r.Bits,%s.Shape,&raw){%s};%s:=TableReader{Buffer:raw[:],Report:&r.Report}\n", ind, entry, bad, scalar)
		g.emitReadScalar(f, expr, scalar, entry+".Shape.Kind", ind+"\t", bad)
		g.pf("%s}\n", ind)
	}
}
func (g *tableGen) emitMessageReadArray(f *ir.Field, expr, entry, ind string) {
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
		g.pf("for range n {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};id,ok:=r.Vocabulary.Name(ref);if !ok{%s};var key %s;known:=key.TableEnumValue(id);var scratch %s;p:=&scratch;if known {p=&base[int(key)-1]}else{r.Report.Unknown++}\n", bad, bad, f.KeyEnum, g.storageElementName(f))
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t", true)
		g.pf("}\n")
	} else {
		g.pf("keep:=min(n,uint64(%d));if keep<n{r.Report.Clamped++};walk:=n;if element.Shape.Bits>=0{walk=keep};for i:=uint64(0);i<walk;i++ {var scratch %s;p:=&scratch;if i<keep{p=&base[i]}\n", bound, g.storageElementName(f))
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t", true)
		g.pf("};if walk<n&&!r.Bits.Skip(int64(n-walk)*element.Shape.Bits){%s}\n", bad)
		if counted {
			g.pf("%s=int32(keep)\n", companion)
		}
	}
	g.pf("%s}\n", ind)
}
func (g *tableGen) storageElementName(f *ir.Field) string {
	if f.Type.Kind == ir.TBytes {
		return "byte"
	}
	if g.regional && isStructRef(f.Type) {
		return g.storageName(f.Type.Name)
	}
	return goFieldType(f.Type)
}

func (g *tableGen) emitMessageArmReset(un *ir.Union, v ir.UnionVariant, expr string) {
	if g.regional {
		g.pf("{value:=%s.%s();", expr, goname.Member(v.Name))
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
