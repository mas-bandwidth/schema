package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageWrite(st *ir.Struct) {
	g.pf("func %sSaveMessageBody(w *TableMessageWriter,value *%s)bool {\n", st.Name, g.storageName(st.Name))
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		cond := guards[f.Name]
		if f.Type.Optional {
			if cond != "" {
				cond = "(" + cond + ")&&"
			}
			cond += "value." + member(f) + "Present"
		}
		g.pf("{\n")
		if cond != "" {
			g.pf("if %s {\n", cond)
		}
		expr := "value." + member(f)
		g.emitStorageCheck(f, expr, "\t", messageInvalid)
		rides := "true"
		if !f.Type.Optional {
			rides = g.emitMessageRides(f, expr, "\t")
		}
		g.pf("if %s { w.Put(%d,TableMessageRefBitsHere)\n", rides, g.messageSlot(ir.TableFieldEntry(f)))
		g.emitMessageValue(f, ir.TableFieldEntry(f), expr, "\t")
		g.pf("}\n")
		if cond != "" {
			g.pf("}\n")
		}
		g.pf("}\n")
	}
	g.pf("w.Put(0,TableMessageRefBitsHere);return !w.Overflow\n}\n")
	// THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3): the
	// batch is a fixed root's, and a variable table has no message form.
	if !st.IsTable || !st.FixedDeclared || st.IsMapEntry() {
		return
	}
	n := st.Name
	typ := g.storageName(n)
	// THE MESSAGE FORM CARRIES THE TRAILER PER BATCH (docs/SPEC-TABLES.md
	// §2.14, §3.3): the measure counts the four trailer bytes and the save
	// appends the CRC-32C of the form byte, the count byte and the padded bit
	// stream, using the variable form's seal. A table without the key emits
	// the bytes it emitted before.
	//
	// A REFUSED BATCH LEAVES THE CALLER'S BUFFER UNTOUCHED (docs/SPEC.md §5.1):
	// the save measures the whole batch first, which checks every value and
	// sizes the text, and answers -1 before its first byte when the measure
	// refuses or the buffer is short.
	if st.Integrity != "" {
		g.pf("func %sMeasureMessages(values []*%s,report *TableReport)int64 {if len(values)<1||len(values)>TableMessageBatchMax{if report!=nil{tableMessageRefuse(report,\"batch_too_large\")};return -1};w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};for _,value:=range values {if value==nil||!%sSaveMessageBody(&w,value){if report!=nil&&w.Reason!=\"\"{tableMessageRefuse(report,w.Reason)};return -1}};w.Align();if w.Overflow{return -1};if w.Over!=0{if report!=nil{report.Refuse(TableRefuseInvalidValue,\"\")};return -1};return 6+w.Bits/8}\n", n, typ, n)
		g.pf("func %sSaveMessages(values []*%s,buffer []byte,report *TableReport)int64 {if need:=%sMeasureMessages(values,report);need<0||int64(len(buffer))<need{return -1};buffer[0]=2;buffer[1]=byte(len(values)-1);w:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:buffer[2:len(buffer)-4]}};for _,value:=range values {if value==nil||!%sSaveMessageBody(&w,value){if report!=nil&&w.Reason!=\"\"{tableMessageRefuse(report,w.Reason)};return -1}};w.Align();if w.Overflow{return -1};return tableIntegritySeal(buffer,2+w.Bits/8)}\n", n, typ, n, n)
		return
	}
	g.pf("func %sMeasureMessages(values []*%s,report *TableReport)int64 {if len(values)<1||len(values)>TableMessageBatchMax{if report!=nil{tableMessageRefuse(report,\"batch_too_large\")};return -1};w:=TableMessageWriter{TableBitWriter:TableBitWriter{Measuring:true}};for _,value:=range values {if value==nil||!%sSaveMessageBody(&w,value){if report!=nil&&w.Reason!=\"\"{tableMessageRefuse(report,w.Reason)};return -1}};w.Align();if w.Overflow{return -1};if w.Over!=0{if report!=nil{report.Refuse(TableRefuseInvalidValue,\"\")};return -1};return 2+w.Bits/8}\n", n, typ, n)
	g.pf("func %sSaveMessages(values []*%s,buffer []byte,report *TableReport)int64 {if need:=%sMeasureMessages(values,report);need<0||int64(len(buffer))<need{return -1};buffer[0]=2;buffer[1]=byte(len(values)-1);w:=TableMessageWriter{TableBitWriter:TableBitWriter{Buffer:buffer[2:]}};for _,value:=range values {if value==nil||!%sSaveMessageBody(&w,value){if report!=nil&&w.Reason!=\"\"{tableMessageRefuse(report,w.Reason)};return -1}};w.Align();if w.Overflow{return -1};return 2+w.Bits/8}\n", n, typ, n, n)
}

// messageInvalid is the message writer's refusal of a value its declaration
// does not admit (docs/SPEC.md §5.1, docs/SPEC-TABLES.md §22.6): the body names
// invalid_value and returns false, so MeasureMessages and SaveMessages answer
// -1 with that reason and write nothing. A value is never masked, wrapped or
// clamped into its width, a flags bit past its declared width among them (§3.3).
const messageInvalid = `return w.Refuse()`

func (g *tableGen) emitMessageRides(f *ir.Field, expr, ind string) string {
	if f.KeyEnum != "" {
		g.emitMessageKeyedCount(f, expr, ind)
		return "pairs>0"
	}
	if isStructRef(f.Type) && f.Array == ir.ArrayNone {
		g.pf("%sprobe:=*w;probe.Measuring=true;probe.Bits=0;if !%sSaveMessageBody(&probe,&%s){w.Reason=probe.Reason;return false}\n", ind, f.Type.Name, expr)
		return "probe.Bits>TableMessageRefBitsHere"
	}
	return g.emitFieldRides(f, expr, ind)
}
func messageElement(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Type.Optional = false
	e.Required = false
	if f.Type.Kind == ir.TBytes {
		e.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
	}
	return &e
}
func (g *tableGen) emitMessageKeyedCount(f *ir.Field, expr, ind string) {
	g.pf("%spairs:=uint64(0);for i:=range %s {\n", ind, expr)
	e := messageElement(f)
	cond := g.emitMessageRides(e, expr+"[i]", ind+"\t")
	g.pf("%sif %s {pairs++}\n%s}\n", ind, cond, ind)
}
func (g *tableGen) emitMessageValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	g.emitStorageCheck(f, expr, ind, messageInvalid)
	switch {
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = expr + "Count"
		}
		if f.Type.Kind == ir.TBytes {
			count = expr + "Length"
		}
		if f.KeyEnum != "" {
			g.emitMessageKeyedCount(f, expr, ind)
			count = "pairs"
		}
		if f.Array == ir.ArrayCounted && entry.Shape.Min > 0 {
			// a count below the announced minimum is outside the ranged
			// integer the count rides as
			g.pf("%sif %s < %d { %s }\n", ind, count, entry.Shape.Min, messageInvalid)
		}
		g.pf("%sw.Put(uint64(%s)-%d,%d)\n", ind, count, entry.Shape.Min, ir.TableMessageCountBits(entry.Shape))
		if ir.TableMessageAligns(entry.Kind, entry.Shape) {
			g.pf("%sw.Align()\n", ind)
		}
		loopcount := count
		if f.KeyEnum != "" {
			loopcount = fmt.Sprint(f.ArrayBound)
		}
		g.pf("%sfor i := range int(%s) {\n", ind, loopcount)
		e := messageElement(f)
		if f.KeyEnum != "" {
			cond := g.emitMessageRides(e, expr+"[i]", ind+"\t")
			g.pf("%sif !(%s){continue};id,named:=%s(i+1).TableEnumID();if !named{%s};slot:=tableMessageNameSlot(id);if slot==0{return false};w.Put(slot,TableMessageRefBitsHere)\n", ind, cond, f.KeyEnum, messageInvalid)
		}
		g.emitMessageValue(e, ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}, expr+"[i]", ind+"\t")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		// ILL-FORMED TEXT IS REFUSED (docs/SPEC.md §5.1 row 8): the used bytes
		// are well-formed UTF-8
		g.pf("%sif !utf8.Valid(%s[:%sLength]) { %s }\n", ind, expr, expr, messageInvalid)
		g.pf("%sw.Put(uint64(%sLength),%d);w.Raw(%s[:%sLength])\n", ind, expr, ir.TableMessageCountBits(entry.Shape), expr, expr)
	case f.Type.Kind == ir.TWString:
		// and the used units hold no unpaired surrogate
		g.pf("%sfor i := int32(0); i < %sLength; i++ {if u := %s[i]; u >= 0xdc00 && u <= 0xdfff { %s } else if u >= 0xd800 && u <= 0xdbff {i++;if i >= %sLength || %s[i] < 0xdc00 || %s[i] > 0xdfff { %s }}}\n",
			ind, expr, expr, messageInvalid, expr, expr, expr, messageInvalid)
		g.pf("%sw.Put(uint64(%sLength),%d);for i := range int32(%sLength) {w.Put(uint64(%s[i]),16)}\n", ind, expr, ir.TableMessageCountBits(entry.Shape), expr, expr)
	case isStructRef(f.Type):
		g.pf("%sif !%sSaveMessageBody(w,&%s){return false}\n", ind, f.Type.Name, expr)
	case isUnionRef(f.Type):
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sswitch %s.Type {case %sTypeNone:w.Put(0,TableMessageRefBitsHere)\n", ind, expr, un.Name)
		for _, v := range un.Variants {
			g.pf("%scase %sType%s:{w.Put(%d,TableMessageRefBitsHere)\n", ind, un.Name, ir.GoExportName(v.Name), g.messageSlot(ir.TableArmEntry(v)))
			if !v.Void() {
				g.emitMessageValue(v.F, ir.TableArmEntry(v), g.unionArmExpr(un, v, expr), ind+"\t")
			}
			g.pf("%s}\n", ind)
		}
		g.pf("%sdefault:%s}\n", ind, messageInvalid)
	case enumRef(f) != nil:
		if enumHasDeprecated(enumRef(f)) {
			// A STRICT WRITE REFUSES A DEPRECATED VARIANT (docs/SPEC-TABLES.md §2.15).
			g.pf("%sif %s.TableEnumDeprecated(){%s}\n", ind, expr, messageInvalid)
		}
		g.pf("%sif %s==%sNone{w.Put(0,TableMessageRefBitsHere)}else{id,named:=%s.TableEnumID();if !named{%s};slot:=tableMessageNameSlot(id);if slot==0{return false};w.Put(slot,TableMessageRefBitsHere)}\n", ind, expr, f.Type.Name, expr, messageInvalid)
	default:
		// A WRITER REFUSES A VALUE OUTSIDE ITS DECLARATION (docs/SPEC-TABLES.md
		// §5, §3.3): the bounds the variable and fixed saves refuse are the
		// message save's too, so a value the read would refuse, or one whose
		// offset the ranged width would truncate, refuses invalid_value
		if cond := writeBoundViolation(f, expr); cond != "" {
			g.pf("%sif %s { %s }\n", ind, cond, messageInvalid)
		}
		lo, hi := "uint64("+expr+")", "uint64(0)"
		switch entry.Kind {
		case 1:
			g.pf("%sif %s {w.Put(1,1)}else{w.Put(0,1)}\n", ind, expr)
			return
		case 10, 11:
			// A NaN OR AN INFINITY IS REFUSED, never quantized or clamped
			// (docs/SPEC-TABLES.md §3.3, both special-values rules): the writer
			// answers false before the quantizer runs, so measure and save
			// both return -1.
			g.needsMath = true
			g.pf("%sif %s-%s != 0 {return false}\n", ind, expr, expr)
			lo = "math.Float64bits(" + expr + ")"
			if entry.Kind == 10 {
				lo = "uint64(math.Float32bits(" + expr + "))"
			}
		default:
			if ir.TableKindWidth(int(entry.Kind)) == 16 {
				lo = expr + ".Lo"
				hi = expr + ".Hi"
			}
		}
		g.needsMath = true
		g.pf("%stableMessageWriteScalar(&w.TableBitWriter,&w.Over,%s,%s,%s)\n", ind, messageShapeLiteral(entry.Kind, entry.Shape), lo, hi)
	}
}
