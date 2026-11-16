package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageRead(st *ir.Struct) {
	g.pf("func %sLoadMessageBody(r *TableMessageReader,value *%s)bool {\n%sReset(value);\n", st.Name, g.storageName(st.Name), st.Name)
	// A FIELD ID TWICE IN ONE BODY IS DAMAGE (docs/SPEC-TABLES.md §3.3), known
	// or unknown: the ids this body has carried are held in a stack buffer.
	g.pf("var seenIDs [64]uint64;seen:=seenIDs[:0]\n")
	// A BODY HAS EXACTLY ONE ENCODING FOR ITS VALUE (docs/SPEC-TABLES.md §3.3):
	// its fields in the writer's declaration order, no union field at None, and
	// no field at the default its writer elides where this reader knows that
	// default exactly, under its own build version (exact).
	if len(st.Fields) > 0 {
		g.pf("order:=-1;exact:=r.Vocabulary.BuildVersion==BuildVersion;_ = exact\n")
	}
	g.pf("for {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok {r.Report.Refuse(TableRefuseWireDamaged,\"\");return false};if ref==0{return true};entry,ok:=r.Vocabulary.Entry(ref);if !ok||entry.ID>=0xfffffffffffffffd {r.Report.Refuse(TableRefuseWireDamaged,\"\");return false};if slices.Contains(seen,entry.ID){r.Report.Refuse(TableRefuseWireDamaged,\"\");return false};seen=append(seen,entry.ID);switch entry.ID {\n")
	for ordinal, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("case 0x%016x:{\n", mine.Id)
		g.pf("if order>=%d {r.Report.Refuse(TableRefuseWireDamaged,\"\");return false};order=%d\n", ordinal, ordinal)
		g.emitMessageCompatibility(f, mine, "entry", "", "\t")
		elides := !f.Type.Optional && !f.Required
		g.elides = elides
		g.emitMessageReadValue(f, "value."+member(f), "entry", "\t", false)
		g.elides = false
		if elides {
			g.emitElidedRefusalWhen(f, "value."+member(f), "\t", "exact && ")
		}
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
	g.pf("default:r.Report.Unknown++;if !tableMessageSkip(&r.Bits,r.Vocabulary,r.IndexBits,entry,0){r.Report.Refuse(TableRefuseWireDamaged,\"\");return false}\n}}}\n")
	// THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3): the
	// batch is a fixed root's, and a variable table has no message form.
	if !st.IsTable || !st.FixedDeclared || st.IsMapEntry() {
		return
	}
	// THE PLAIN NAME IS THE READ, AND IT IS THE ONLY READ (docs/SPEC-TABLES.md
	// §6.5): the batch is accepted whole or refused with a count of 0 and every
	// value it had written reset (§3.3). The batch trailer of a declaring root
	// is checked before any body is decoded (§2.14).
	open := "tableMessageBatchOpen(vocabulary,data,read)"
	if len(ir.TableIntegrityTables(g.unit)) > 0 {
		sealed := "false"
		if st.Integrity != "" {
			sealed = "true"
		}
		open = "tableMessageBatchOpenSealed(vocabulary,data,read," + sealed + ")"
	}
	g.pf("// %sLoadMessages is the message read (docs/SPEC-TABLES.md §3.3, §6.5): the whole batch, or ok false with a\n// count of 0 and every value it had written reset. A supplied report receives this read's report;\n// report.Err() is the reason.\n", st.Name)
	g.pf("func %sLoadMessages(values []%s,vocabulary *TableVocabulary,data []byte,report *TableReport)(int64,bool) {read:=report;if read==nil{read=&TableReport{}}else{*read=TableReport{}};r,count:=%s;defer func(){*read=r.Report;if read.Refused(){tableRefused(read)}}();if count<0{return 0,false};if count>int64(len(values)){tableMessageRefuse(&r.Report,\"batch_too_large\");return 0,false};for i := range count {if !%sLoadMessageBody(&r,&values[i])||r.Report.Refused(){for j := range i+1 {%sReset(&values[j])};return 0,false}};if !tableMessageBatchClose(&r){for j := range count {%sReset(&values[j])};return 0,false};return count,true}\n", st.Name, g.storageName(st.Name), open, st.Name, st.Name, st.Name)
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
	// an announced kind that is not the declared one and not a widening
	// refuses the batch (docs/SPEC-TABLES.md §3.3, §4)
	g.pf("%sif %s {widened=true}else{%s;r.Report.Refuse(TableRefuseKindMismatch,\"\");return false}\n%s}\n", ind, compat, reset, ind)
}
func (g *tableGen) emitMessageReadValue(f *ir.Field, expr, entry, ind string, element bool) {
	bad := "r.Report.Refuse(TableRefuseWireDamaged,\"\");return false"
	// A FIELD's elided spellings that its stored value cannot show are read off
	// the wire here (docs/SPEC-TABLES.md §3.3): a union's None reference, an
	// enum's variant reference, a nested body that is its terminator alone, a
	// keyed array of no slot. g.elides is the field read's, taken and cleared
	// so no element or arm read below it is held to the field's rule.
	elide, sink := g.takeElides(), g.takeSink()
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};text:=r.Bits.Buffer[start:start+int64(n)];if !tableUTF8Valid(text){%s};if n>%d{%s};clear(%s[:]);copy(%s[:],text);%sLength=int32(n)}\n", ind, entry, bad, bad, bad, f.Type.Size, refuse("TableRefuseLengthOverBound", g.floatOwner(f), "return false"), expr, expr, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%s{n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok||!r.Bits.Align(){%s};start:=r.Bits.Offset/8;if !r.Bits.Skip(int64(n)*8){%s};if n>%d{%s};clear(%s[:]);copy(%s[:],r.Bits.Buffer[start:start+int64(n)]);%sLength=int32(n)}\n", ind, entry, bad, bad, f.Type.Size, refuse("TableRefuseLengthOverBound", g.floatOwner(f), "return false"), expr, expr, expr)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.elides = elide
		g.emitMessageReadArray(f, expr, entry, ind)
	case isStructRef(f.Type):
		if elide {
			g.pf("%s{start:=r.Bits.Offset;if !%sLoadMessageBody(r,&%s){return false};if exact&&r.Bits.Offset-start==r.Vocabulary.RefBits{%s}}\n", ind, f.Type.Name, expr, bad)
		} else if sink != "" {
			g.pf("%s{start:=r.Bits.Offset;if !%sLoadMessageBody(r,&%s){return false};if r.Bits.Offset-start!=r.Vocabulary.RefBits{%s}}\n", ind, f.Type.Name, expr, sinkOff(sink))
		} else {
			g.pf("%sif !%sLoadMessageBody(r,&%s){return false}\n", ind, f.Type.Name, expr)
		}
	case isUnionRef(f.Type):
		target := g.nextWireWriter()
		g.pf("{ %s:=&%s\n", target, expr)
		expr = "(*" + target + ")"
		un := f.Type.Ref.(*ir.Union)
		arm := g.nextWireWriter()
		none := fmt.Sprintf("%s.Type=%sTypeNone", expr, un.Name)
		if elide {
			none = bad
		}
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s}else{%s%s,ok:=r.Vocabulary.Entry(ref);if !ok||%s.ID>=0xfffffffffffffffd||%s.Shape.Kind==0{%s};switch %s.ID{\n", ind, bad, none, sinkOff(sink), arm, arm, arm, bad, arm)
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
		def := fieldDefaultExpr(f)
		none := ""
		if elide && def == f.Type.Name+"None" {
			none = "if exact{" + bad + "};"
		} else if def != f.Type.Name+"None" {
			none = sinkOff(sink)
		}
		g.pf("%s{ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};if ref==0{%s%s=%sNone}else{id,ok:=r.Vocabulary.Name(ref);if !ok{%s};if !%s.TableEnumValue(id){r.Report.Unknown++;%s}", ind, bad, none, expr, f.Type.Name, bad, expr, sinkOff(sink))
		if enumHasDeprecated(enumRef(f)) {
			land := def
			if element {
				land = f.Type.Name + "None"
			}
			// A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and
			// as the ELEMENT default for an element (§2.15), never unknown.
			g.pf("else if %s.TableEnumDeprecated() { %s = %s;%s }", expr, expr, land, sinkOff(sink))
		}
		if elide && def != f.Type.Name+"None" {
			// the default is read off the variant the wire names (§3.3)
			g.pf("else if exact&&%s==%s {%s}", expr, def, bad)
		}
		if sink != "" {
			g.pf("else if %s!=%s {%s}", expr, def, sinkOff(sink))
		}
		g.pf("}}\n")
	default:
		scalar := g.nextWireWriter()
		g.pf("%s{var raw [16]byte;ok,over:=tableMessageReadScalar(&r.Bits,%s.Shape,&raw);if !ok{%s};if over{r.Report.Refuse(TableRefuseOutOfRange,\"\");return false};\n", ind, entry, bad)
		g.emitMessageIntegerHeld(f, entry, ind+"\t", "raw", bad)
		g.pf("%s%s:=TableReader{Buffer:raw[:],Report:&r.Report}\n", ind, scalar)
		g.emitReadScalar(f, expr, scalar, entry+".Shape.Kind", ind+"\t", bad)
		if sink != "" {
			g.pf("%sif %s {%s}\n", ind+"\t", g.elideDiffers(f, expr, fieldDefaultExpr(f)), sinkOff(sink))
		}
		g.pf("%s}\n", ind)
	}
}

// emitMessageIntegerHeld holds a reconstructed message-form integer to THIS
// reader's bound before the read narrows it to the reader's storage: the
// declared range where there is one, or the domain of the declared kind where
// there is none (docs/SPEC-TABLES.md §3.3). The sender's offset plus base can
// land past the reader kind's width, so the test is taken while the value is
// still wide, high lane included; outside the bound the batch refuses and is
// never masked or wrapped. A 128-bit kind's whole domain is its 128 bits, so
// the reconstruction above is what refuses a value past it; its declared range
// is held by the wide read.
func (g *tableGen) emitMessageIntegerHeld(f *ir.Field, entry, ind, raw, onBad string) {
	if f == nil {
		return
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
	default:
		return
	}
	signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
	lo, hi := tableStorageRange(signed, f.Type.Width)
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		if rlo.Cmp(lo) > 0 {
			lo = rlo
		}
		if rhi.Cmp(hi) < 0 {
			hi = rhi
		}
	}
	stop := refuse("TableRefuseOutOfRange", g.floatOwner(f), onBad)
	if f.Type.Width >= 128 {
		return
	}
	if signed {
		g.pf("%swide:=int64(binary.LittleEndian.Uint64(%s[:8]));if int64(binary.LittleEndian.Uint64(%s[8:]))!=wide>>63||wide < %s || wide > %s {%s}\n", ind, raw, raw, goIntLit(lo, true, 8), goIntLit(hi, true, 8), stop)
		return
	}
	g.pf("%swide:=binary.LittleEndian.Uint64(%s[:8]);if binary.LittleEndian.Uint64(%s[8:])!=0||wide < %s || wide > %s {%s}\n", ind, raw, raw, goIntLit(lo, false, 8), goIntLit(hi, false, 8), stop)
}
func (g *tableGen) emitMessageReadArray(f *ir.Field, expr, entry, ind string) {
	bad := "r.Report.Refuse(TableRefuseWireDamaged,\"\");return false"
	bound := f.ArrayBound
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
	companion := expr + "Count"
	if f.Type.Kind == ir.TBytes {
		bound = f.Type.Size
		companion = expr + "Length"
	}
	elide := g.takeElides()
	g.pf("%s{base:=&%s;n,ok:=tableMessageCount(&r.Bits,%s.Shape);if !ok{%s};", ind, expr, entry, bad)
	if elide && f.KeyEnum != "" {
		// a keyed array with no slot riding is elided by its writer (§3.3)
		g.pf("if exact&&n==0{%s};", bad)
	}
	// a keyed slot at its default elides, and so does an [N] array of enums
	// or unions every element of which is (§3.2, §3.3; canonical.go); a
	// quantized float cannot say so (quantizedMessageFloat)
	sink := ""
	if f.KeyEnum != "" && !quantizedMessageFloat(f) {
		sink = "slotDefault"
	} else if f.KeyEnum == "" && (enumRef(f) != nil || isUnionRef(f.Type)) {
		sink = fixedArraySink(f, elide)
	}
	if f.KeyEnum == "" {
		g.pf("if %s.Element.Kind==6&&!r.Bits.Align(){%s};", entry, bad)
	}
	g.pf("element:=TableMessageEntry{Shape:%s.Element};_ = element;\n", entry)
	if f.KeyEnum != "" {
		// A KEYED SLOT TWICE IN ONE ARRAY IS DAMAGE (docs/SPEC-TABLES.md §3.2, §3.3)
		g.pf("var seenKeys [64]uint64;seenKey:=seenKeys[:0];lastKey:=0\n")
		g.pf("for range n {ref,ok:=r.Bits.Get(r.Vocabulary.RefBits);if !ok{%s};id,ok:=r.Vocabulary.Name(ref);if !ok{%s};if slices.Contains(seenKey,id){%s};seenKey=append(seenKey,id);var key %s;known:=key.TableEnumValue(id);var scratch %s;p:=&scratch;if known {p=&base[int(key)-1];if int(key)<=lastKey{%s};lastKey=int(key)}else{r.Report.Unknown++}\n", bad, bad, bad, f.KeyEnum, g.storageElementName(f), bad)
		if sink != "" {
			g.pf("%s:=true\n", sink)
		}
		g.sink = sink
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t", true)
		if sink != "" {
			g.pf("if exact&&known&&%s{%s}\n", sink, bad)
		}
		g.pf("}\n")
	} else {
		over := "TableRefuseCountOverBound"
		if f.Type.Kind == ir.TBytes {
			over = "TableRefuseLengthOverBound"
		}
		if sink != "" {
			g.pf("%s:=true\n", sink)
		}
		g.pf("if n>%d{%s};for i:=range n {p:=&base[i]\n", bound, refuse(over, g.floatOwner(f), "return false"))
		g.sink = sink
		g.emitMessageReadValue(messageElement(f), "(*p)", "element", ind+"\t", true)
		g.pf("}\n")
		if sink != "" {
			g.pf("if exact&&%s{%s}\n", sink, bad)
		}
		if counted {
			g.pf("%s=int32(n)\n", companion)
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
