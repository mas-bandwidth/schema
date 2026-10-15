package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitPutLeb(ind, writer, value string) {
	g.pf("%sif !%s.putLebPair(%s) { %s.putLebWide(%s) }\n", ind, writer, value, writer, value)
}

func (g *tableGen) emitHeader(ind, writer, ref, kind string) {
	g.pf("%sif !%s.headerPair(%s, %s) { %s.headerRest(%s, %s) }\n", ind, writer, ref, kind, writer, ref, kind)
}

// arithMeasureEligible is a table whose body is only fixed scalar leaves:
// TInt/TBits/TFloat/TBool/enum/flags, including optional and guarded forms.
// Arrays, strings, bytes, nested tables, unions, keyed fields, pointers,
// retain tails and variable tables keep the dry-run SaveBody walk (and the
// 793 child-table / array-length cache on that path).
func (g *tableGen) arithMeasureEligible(st *ir.Struct) bool {
	if g.retain || ir.VariableTables(g.unit)[st.Name] {
		return false
	}
	for _, f := range st.Fields {
		if !arithMeasureFieldEligible(f) {
			return false
		}
	}
	return true
}

func arithMeasureFieldEligible(f *ir.Field) bool {
	if f.IsMap() || f.IsList() || f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Pointer || f.Type.Blob() {
		return false
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits, ir.TBool, ir.TFloat32, ir.TFloat64:
		return true
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return true
		}
	}
	return false
}

func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	n := st.Name
	if g.arithMeasureEligible(st) {
		g.emitArithMeasureBody(st)
	} else {
		g.pf("func %sMeasureBody(value *%s, ids *TableIds) int64 {\n w := TableWriter{Measuring:true, Ids:ids}\n if !%sSaveBody(&w,value) { return -1 }; return w.Offset\n}\n\n", n, g.storageName(n), n)
	}
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	if g.arithMeasureEligible(st) {
		g.pf("func %sMeasure(value *%s) int64 {\n var ids TableIds\n body := %sMeasureBody(value, &ids)\n if body < 0 || ids.Overflow { return -1 }\n return 1 + body + int64(ids.Count)*8 + 8\n}\n\n", n, g.storageName(n), n)
	} else {
		g.pf("func %sMeasure(value *%s) int64 {\n var ids TableIds\n w := TableWriter{Measuring:true, Ids:&ids}\n w.Put8(1)\n if !%sSaveBody(&w,value) { return -1 }; w.Trailer()\n if w.Overflow || ids.Overflow { return -1 }; return w.Offset\n}\n\n", n, g.storageName(n), n)
	}
	g.pf("func %sMeasureReason(value *%s)(int64,error){size:=%sMeasure(value);if size<0{return size,TableRefuseInvalidValue};return size,nil}\n", n, g.storageName(n), n)
}

// emitArithMeasureBody is C++ MixedStatMeasureBody: terminator 1, then
// tableLebBytes(ids.refAtHit(...))+1+width per riding leaf. Enums intern the
// field then the variant and add leb(variant) instead of a fixed width.
func (g *tableGen) emitArithMeasureBody(st *ir.Struct) {
	n := st.Name
	g.pf("func %sMeasureBody(value *%s, ids *TableIds) int64 {\n", n, g.storageName(n))
	if len(st.Fields) == 0 {
		g.pf("\t_ = value\n\tif ids.Overflow { return -1 }\n\treturn 1\n}\n\n")
		return
	}
	g.pf("\tbytes := int64(1)\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		cond := guards[f.Name]
		if f.Type.Optional {
			present := "value." + member(f) + "Present"
			if cond != "" {
				cond = "(" + cond + ") && " + present
			} else {
				cond = present
			}
		}
		g.emitArithMeasureField(f, cond, "\t")
	}
	g.pf("\tif ids.Overflow { return -1 }\n\treturn bytes\n}\n\n")
}

func (g *tableGen) emitArithMeasureField(f *ir.Field, cond, ind string) {
	expr := "value." + member(f)
	id := ir.TableFieldWireId(f)
	ride := cond
	if !f.Type.Optional {
		elide := expr + " != " + fieldDefaultExpr(f)
		if ride != "" {
			ride += " && " + elide
		} else {
			ride = elide
		}
	}
	if e := enumRef(f); e != nil {
		inner := ind
		if ride != "" {
			g.pf("%sif %s {\n", ind, ride)
			inner = ind + "\t"
		}
		g.pf("%sref := ids.refAtHit(%d, 0x%016x)\n", inner, g.knownOrdinal(id), id)
		g.pf("%svar vref uint64\n", inner)
		g.pf("%sswitch %s {\n", inner, expr)
		g.pf("%scase %sNone:\n", inner, e.Name)
		for i, v := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%scase %s%s:\n%s\tvref = ids.refAtHit(%d, 0x%016x)\n", inner, e.Name, v, inner, g.knownOrdinal(vid), vid)
		}
		g.pf("%sdefault:\n%s\treturn -1\n%s}\n", inner, inner, inner)
		g.pf("%sbytes += tableLebBytes(ref) + 1 + tableLebBytes(vref)\n", inner)
		if ride != "" {
			g.pf("%s}\n", ind)
		}
		return
	}
	width := ir.TableKindWidth(ir.TableWireScalarKind(f))
	add := fmt.Sprintf("bytes += tableLebBytes(ids.refAtHit(%d, 0x%016x)) + 1 + %d", g.knownOrdinal(id), id, width)
	if ride != "" {
		g.pf("%sif %s { %s } // %s\n", ind, ride, add, f.Name)
	} else {
		g.pf("%s%s // %s\n", ind, add, f.Name)
	}
}

func (g *tableGen) emitTableSave(st *ir.Struct) {
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	g.pf("func %sSave(value *%s, buffer []byte) int64 {\n var ids TableIds\n w := TableWriter{Buffer:buffer, Ids:&ids}\n w.Put8(1)\n if !%sSaveBody(&w,value) { return -1 }; w.Trailer()\n if w.Overflow || ids.Overflow { return -1 }; return w.Offset\n}\n\n", st.Name, g.storageName(st.Name), st.Name)
}

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("func %sSaveBody(w *TableWriter, value *%s) bool {\n", st.Name, g.storageName(st.Name))
	var scratchSlots int64
	for _, f := range st.Fields {
		scratchSlots = max(scratchSlots, g.arrayLengthScratchSlots(f))
	}
	if scratchSlots > 0 {
		g.pf("var arrayLengths [%d]int64\n", scratchSlots)
	}
	if g.regional {
		g.pf("if !%sSaveBodyFields(w,value) { return false }; w.Put8(0); return !w.Overflow && !w.Ids.Overflow\n}\nfunc %sSaveBodyFields(w *TableWriter,value *%s) bool {\n", st.Name, st.Name, g.storageName(st.Name))
	}
	guards := tableGuardExprs(st)
	for ordinal, f := range st.Fields {
		g.retainOrdinal = ordinal
		cond := guards[f.Name]
		if f.Type.Optional {
			if cond != "" {
				cond = "(" + cond + ") && "
			}
			cond += "value." + member(f) + "Present"
		}
		ind := "\t"
		if cond != "" {
			g.pf("%sif %s {\n", ind, cond)
			ind += "\t"
		}
		g.emitWireField(f, "value."+member(f), ind)
		if cond != "" {
			g.pf("\t}\n")
		}
	}
	if g.retain {
		g.pf("if !tableRetainTail(w){return false}\n")
	}
	if !g.regional {
		g.pf("w.Put8(0)\n")
	}
	g.pf("return !w.Overflow && !w.Ids.Overflow\n}\n\n")
}

// A field's vocabulary is speculative until its value is known to ride. The
// payload walk is the same for fields, elements and union arms; only their
// framing and the field's elision differ.
func (g *tableGen) emitWireField(f *ir.Field, expr, ind string) {
	kind := ir.TableWireFieldKind(f)
	if f.KeyEnum == "" && kind != tkTable {
		g.pf("%s{\n", ind)
		i := ind + "\t"
		g.emitStorageCheck(f, expr, i)
		cond := "true"
		if !f.Type.Optional {
			cond = g.emitFieldRides(f, expr, i)
		}
		g.pf("%sif %s {\n%s if ref := w.Ids.refAtHit(%d, 0x%016x); !w.headerPair(ref, %d) { w.headerRest(ref, %d) }\n", i, cond, i, g.knownOrdinal(ir.TableFieldWireId(f)), ir.TableFieldWireId(f), kind, kind)
		g.emitWireValue(f, expr, "w", i+"\t", true)
		g.pf("%s}\n%s}\n", i, ind)
		return
	}
	if !g.retain && f.KeyEnum == "" && kind == tkTable {
		g.pf("%s{\n", ind)
		i := ind + "\t"
		g.pf("%smark := w.Ids.Count; ref := w.Ids.refAtHit(%d, 0x%016x); start := w.Ids.Count\n", i, g.knownOrdinal(ir.TableFieldWireId(f)), ir.TableFieldWireId(f))
		g.pf("%sn := %sMeasureBody(&%s, w.Ids)\n", i, f.Type.Name, expr)
		g.pf("%sif n < 0 || w.Ids.Overflow { return false }\n", i)
		cond := "n > 1"
		if f.Type.Optional {
			cond = "true"
		}
		g.pf("%sif %s {\n%s if w.Measuring { w.Advance(tableLebBytes(ref)+1+tableLebBytes(uint64(n))+n) } else {\n%s  w.Ids.Truncate(start)\n", i, cond, i, i)
		g.emitHeader(i+"  ", "w", "ref", fmt.Sprintf("%d", kind))
		g.emitPutLeb(i+"  ", "w", "uint64(n)")
		g.pf("%s  if !%sSaveBody(w, &%s) { return false }\n%s }\n%s} else { w.Ids.Truncate(mark) }\n%s}\n", i, f.Type.Name, expr, i, i, ind)
		return
	}
	g.pf("%s{\n", ind)
	i := ind + "\t"
	g.pf("%smark := w.Ids.Count; ref := w.Ids.refAtHit(%d, 0x%016x); start := w.Ids.Count\n", i, g.knownOrdinal(ir.TableFieldWireId(f)), ir.TableFieldWireId(f))
	g.pf("%spayload := TableWriter{Measuring:true, Ids:w.Ids}\n", i)
	g.emitWireValue(f, expr, "payload", i, true)
	g.pf("%sif payload.Overflow || w.Ids.Overflow { return false }\n", i)
	empty := 2
	if f.KeyEnum != "" {
		empty = 3
	}
	cond := fmt.Sprintf("payload.Offset > %d", empty)
	if f.Type.Optional {
		cond = "true"
	}
	g.pf("%sif %s {\n%s if w.Measuring { w.Advance(tableLebBytes(ref)+1+payload.Offset) } else {\n%s  w.Ids.Truncate(start); if !w.headerPair(ref, %d) { w.headerRest(ref, %d) }\n", i, cond, i, i, kind, kind)
	g.emitWireValue(f, expr, "w", i+"\t\t", true)
	g.pf("%s }\n%s} else { w.Ids.Truncate(mark) }\n%s}\n", i, i, ind)
}

func (g *tableGen) emitStorageCheck(f *ir.Field, expr, ind string) {
	if f.IsList() || f.IsMap() {
		g.pf("%sif %s.Count<0 {return false}\n", ind, expr)
		return
	}
	if f.Type.Pointer {
		if f.Array == ir.ArrayCounted {
			g.pf("%sif %sCount<0 || %sCount>%d { return false }\n", ind, expr, expr, f.ArrayBound)
		}
		return
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
		g.pf("%sif %sLength < 0 || %sLength > %d { return false }\n", ind, expr, expr, f.Type.Size)
	} else if f.Array == ir.ArrayCounted {
		g.pf("%sif %sCount < 0 || %sCount > %d { return false }\n", ind, expr, expr, f.ArrayBound)
	}
}

func (g *tableGen) emitFieldRides(f *ir.Field, expr, ind string) string {
	if f.IsList() || f.IsMap() {
		return expr + ".Count>0"
	}
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return expr + " != 0"
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return byteValueRides(f, expr)
	}
	if f.Type.Kind == ir.TWString {
		return expr + "Length > 0"
	}
	if f.Array == ir.ArrayCounted {
		return expr + "Count > 0"
	}
	if f.Array == ir.ArrayFixed {
		if isStructRef(f.Type) && !f.Type.Pointer {
			return "true"
		}
		lhs, def := expr+"[i]", fieldDefaultExpr(f)
		if isUnionRef(f.Type) {
			lhs += ".Type"
			def = f.Type.Name + "TypeNone"
		}
		g.pf("%sallDefault := true; for i := range %s { if %s != %s { allDefault = false; break } }\n", ind, expr, lhs, def)
		return "!allDefault"
	}
	if isUnionRef(f.Type) {
		return expr + ".Type != " + f.Type.Name + "TypeNone"
	}
	return expr + " != " + fieldDefaultExpr(f)
}

// framed adds the ordinary value's L. A union arm already supplies its own
// L, so its value is emitted bare. Union values themselves have no L.
func (g *tableGen) emitWireValue(f *ir.Field, expr, writer, ind string, framed bool) {
	g.emitStorageCheck(f, expr, ind)
	switch {
	case f.IsMap():
		g.emitMapWrite(f, expr, writer, ind, framed)
	case f.IsList():
		g.emitListWrite(f, expr, writer, ind, framed)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		g.emitWireArray(f, expr, writer, ind, framed)
	case f.Type.Pointer:
		g.pf("%sif %s==0 {\n", ind, expr)
		g.emitPutLeb(ind+"\t", writer, "0")
		g.pf("%s} else { index,ok := %s.Ids.Numbering.Index(&%s);if !ok {return false}\n", ind, writer, expr)
		g.emitPutLeb(ind+"\t", writer, "index")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		if framed {
			g.emitPutLeb(ind, writer, "uint64("+expr+"Length)")
		}
		g.pf("%s%s.Raw(%s[:%sLength])\n", ind, writer, expr, expr)
	case f.Type.Kind == ir.TWString:
		if framed {
			g.emitPutLeb(ind, writer, "uint64("+expr+"Length)*2")
		}
		g.pf("%sfor i := int32(0); i < %sLength; i++ { %s.Put16(%s[i]) }\n", ind, expr, writer, expr)
	case isStructRef(f.Type):
		restore := ""
		if g.retain && !g.retainArm {
			restore = g.retainWritePush(writer, g.retainIndex)
		}
		if !framed {
			g.pf("%sif !%sSaveBody(%s,&%s) { return false }\n", ind, f.Type.Name, writerPointer(writer), expr)
			if restore != "" {
				g.retainWritePop(writer, restore)
			}
			return
		}
		g.pf("%s{\n%s mark := %s.Ids.Count\n%s n := %sMeasureBody(&%s,%s.Ids); if n < 0 { return false }\n", ind, ind, writer, ind, f.Type.Name, expr, writer)
		g.emitPutLeb(ind+" ", writer, "uint64(n)")
		g.pf("%s if %s.Measuring { %s.Advance(n) } else {\n%s  %s.Ids.Truncate(mark)\n%s  if !%sSaveBody(%s,&%s) { return false }\n%s }\n%s}\n", ind, writer, writer, ind, writer, ind, f.Type.Name, writerPointer(writer), expr, ind, ind)
		if restore != "" {
			g.retainWritePop(writer, restore)
		}
	case isUnionRef(f.Type):
		g.emitWireUnion(f.Type.Ref.(*ir.Union), expr, writer, ind)
	default:
		g.emitWireScalar(f, expr, writer, ind)
	}
}

func (g *tableGen) nextWireWriter() string {
	g.wireSerial++
	return fmt.Sprintf("payload%d", g.wireSerial)
}

// Every SaveBody shares one length scratch array, bounded to 1 KiB, across its
// eligible fields and union arms. Nested struct bodies are separate calls with
// their own scratch: this is a per-function bound, not a bound on the call stack.
// Larger arrays and variable, keyed, pointer or retain paths keep the old walk.
const arrayLengthCacheBytes = 1024

func (g *tableGen) cacheArrayLengths(f *ir.Field) bool {
	return !g.regional && !g.retain && f.KeyEnum == "" && !f.Type.Pointer && isStructRef(f.Type) &&
		(f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted) && f.ArrayBound > 0 && f.ArrayBound <= arrayLengthCacheBytes/8
}

func (g *tableGen) arrayLengthScratchSlots(f *ir.Field) int64 {
	if g.regional || g.retain || f.Type.Pointer {
		return 0
	}
	if g.cacheArrayLengths(f) {
		return f.ArrayBound
	}
	// Union arms are emitted in the enclosing SaveBody, including arms of
	// array elements. Only struct arrays use the cache, and each struct body's
	// separate call cannot overwrite this function's in-progress lengths.
	var slots int64
	if isUnionRef(f.Type) {
		for _, v := range f.Type.Ref.(*ir.Union).Variants {
			if !v.Void() {
				slots = max(slots, g.arrayLengthScratchSlots(v.F))
			}
		}
	}
	return slots
}

func (g *tableGen) emitWireArray(f *ir.Field, expr, writer, ind string, framed bool) {
	body := g.nextWireWriter()
	kind := ir.TableWireScalarKind(f)
	cacheLengths := g.cacheArrayLengths(f)
	count := fmt.Sprint(f.ArrayBound)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
		count = expr + "Length"
	} else if f.Array == ir.ArrayCounted {
		count = expr + "Count"
	}
	if f.KeyEnum != "" {
		count = keyedLoopBound(f)
	}
	g.pf("%s{\n", ind)
	i := ind + "\t"
	g.pf("%smark := %s.Ids.Count\n%s%s := TableWriter{Measuring:true, Ids:%s.Ids}; pairs := uint64(0)\n", i, writer, i, body, writer)
	g.pf("%sfor i := 0; i < int(%s); i++ {\n", i, count)
	if cacheLengths {
		g.wireSerial++ // Preserve the skipped element emitter's temporary-name slot.
		g.pf("%s n := %sMeasureBody(&%s[i],%s.Ids); if n < 0 { return false }; arrayLengths[i] = n\n", i, f.Type.Name, expr, body)
		g.emitPutLeb(i+" ", body, "uint64(n)")
		g.pf("%s %s.Advance(n)\n", i, body)
	} else {
		g.emitWireArrayElement(f, expr+"[i]", body, i+"\t")
	}
	g.pf("%s pairs++\n%s}\n", i, i)
	g.pf("%sif %s.Overflow { return false }; n := int64(1)+tableLebBytes(pairs)+%s.Offset\n", i, body, body)
	if framed {
		g.emitPutLeb(i, writer, "uint64(n)")
	}
	g.pf("%sif %s.Measuring { %s.Advance(n) } else {\n%s %s.Ids.Truncate(mark); %s.Put8(%d)\n", i, writer, writer, i, writer, writer, kind)
	g.emitPutLeb(i+" ", writer, "pairs")
	g.pf("%s for i := 0; i < int(%s); i++ {\n", i, count)
	if cacheLengths {
		g.wireSerial++
		// The unchanged outer truncation replays the first pass's vocabulary in
		// the same element order. Its saved lengths therefore include the same
		// reference widths, without another MeasureBody before each SaveBody.
		g.emitPutLeb(i+"  ", writer, "uint64(arrayLengths[i])")
		g.pf("%s  if !%sSaveBody(%s,&%s[i]) { return false }\n", i, f.Type.Name, writerPointer(writer), expr)
	} else {
		g.emitWireArrayElement(f, expr+"[i]", writer, i+"\t\t")
	}
	g.pf("%s }\n%s}\n%s}\n", i, i, ind)
}

func (g *tableGen) emitWireArrayElement(f *ir.Field, expr, writer, ind string) {
	oldIndex, oldArm := g.retainIndex, g.retainArm
	g.retainIndex = "i"
	g.retainArm = false
	defer func() { g.retainIndex = oldIndex; g.retainArm = oldArm }()
	element := g.nextWireWriter()
	elem := *f
	elem.Array = ir.ArrayNone
	elem.KeyEnum = ""
	elem.Type.Optional = false
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		elem.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
	}
	if f.KeyEnum == "" {
		g.emitWireValue(&elem, expr, writer, ind, true)
		return
	}
	g.pf("%s{\n", ind)
	i := ind + "\t"
	// Keyed entries each have their own L around the bare value.
	g.pf("%smark := %s.Ids.Count\n%sid, named := %s(i+1).TableEnumId(); if !named { return false }; ref := %s.Ids.Ref(id); start := %s.Ids.Count\n", i, writer, i, f.KeyEnum, writer, writer)
	g.pf("%s%s := TableWriter{Measuring:true, Ids:%s.Ids}\n", i, element, writer)
	g.emitWireValue(&elem, expr, element, i, false)
	cond := expr + " != " + fieldDefaultExpr(&elem)
	if isStructRef(elem.Type) {
		cond = element + ".Offset > 1"
	} else if isUnionRef(elem.Type) {
		cond = expr + ".Type != " + elem.Type.Name + "TypeNone"
	}
	g.pf("%sif !(%s) { %s.Ids.Truncate(mark); continue }\n%sif %s.Overflow { return false }\n", i, cond, writer, i, element)
	g.emitPutLeb(i, writer, "ref")
	g.emitPutLeb(i, writer, "uint64("+element+".Offset)")
	g.pf("%sif %s.Measuring { %s.Advance(%s.Offset) } else {\n%s %s.Ids.Truncate(start)\n", i, writer, writer, element, i, writer)
	g.emitWireValue(&elem, expr, writer, i+"\t", false)
	g.pf("%s}\n%s}\n", i, ind)
}

func (g *tableGen) emitWireUnion(un *ir.Union, expr, writer, ind string) {
	target := g.nextWireWriter()
	g.pf("%s{ %s := &%s\n", ind, target, expr)
	expr = target
	outer := ""
	if g.retain && g.retainIndex != "" && g.retainIndex != "0" {
		outer = g.retainWritePush(writer, g.retainIndex)
	}
	g.pf("%sswitch %s.Type {\n%scase %sTypeNone:\n", ind, expr, ind, un.Name)
	g.emitPutLeb(ind+"\t", writer, "0")
	for ordinal, v := range un.Variants {
		g.pf("%scase %sType%s:\n", ind, un.Name, ir.GoExportName(v.Name))
		i := ind + "\t"
		kind := ir.TableKindNoPayload
		if !v.Void() {
			kind = ir.TableWireFieldKind(v.F)
		}
		g.pf("%sref := %s.Ids.refAtHit(%d, 0x%016x)\n", i, writer, g.knownOrdinal(ir.TableWireId(v.WireName())), ir.TableWireId(v.WireName()))
		if v.Void() {
			g.emitHeader(i, writer, "ref", "32")
			g.emitPutLeb(i, writer, "0")
			continue
		}
		savedIndex, savedArm := g.retainIndex, g.retainArm
		g.retainIndex = ""
		g.retainArm = true
		restore := ""
		if g.retain {
			restore = g.retainWritePush(writer, fmt.Sprint(ordinal+1))
		}
		arm := g.unionArmExpr(un, v, expr)
		// A table-body arm is the framed nested-struct case. Retain keeps the
		// nested measuring writer: its Path push sits on that writer.
		if !g.retain && v.Body() {
			g.pf("%s{\n%s mark := %s.Ids.Count\n%s n := %sMeasureBody(&%s,%s.Ids); if n < 0 { return false }\n", i, i, writer, i, v.F.Type.Name, arm, writer)
			g.emitHeader(i+" ", writer, "ref", fmt.Sprintf("%d", kind))
			g.emitPutLeb(i+" ", writer, "uint64(n)")
			g.pf("%s if %s.Measuring { %s.Advance(n) } else {\n%s  %s.Ids.Truncate(mark)\n%s  if !%sSaveBody(%s,&%s) { return false }\n%s }\n%s}\n", i, writer, writer, i, writer, i, v.F.Type.Name, writerPointer(writer), arm, i, i)
		} else {
			payload := g.nextWireWriter()
			g.pf("%sstart := %s.Ids.Count; %s := TableWriter{Measuring:true, Ids:%s.Ids}\n", i, writer, payload, writer)
			g.emitWireValue(v.F, arm, payload, i, false)
			g.pf("%sif %s.Overflow { return false }\n", i, payload)
			g.emitHeader(i, writer, "ref", fmt.Sprintf("%d", kind))
			g.emitPutLeb(i, writer, "uint64("+payload+".Offset)")
			g.pf("%sif %s.Measuring { %s.Advance(%s.Offset) } else {\n%s %s.Ids.Truncate(start)\n", i, writer, writer, payload, i, writer)
			g.emitWireValue(v.F, arm, writer, i+"\t", false)
			g.pf("%s}\n", i)
		}
		if restore != "" {
			g.retainWritePop(writer, restore)
		}
		g.retainIndex = savedIndex
		g.retainArm = savedArm
	}
	g.pf("%sdefault: return false\n%s}\n", ind, ind)
	if outer != "" {
		g.retainWritePop(writer, outer)
	}
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitWireScalar(f *ir.Field, expr, writer, ind string) {
	if e := enumRef(f); e != nil {
		// Each arm is a compile-time id, so RefAt is one load and one
		// compare. None is still reference zero and is not interned.
		g.pf("%sswitch %s {\n%scase %sNone:\n", ind, expr, ind, e.Name)
		g.emitPutLeb(ind+"\t", writer, "0")
		for i, v := range e.Variants {
			id := ir.TableWireId(e.VariantWireName(i))
			g.pf("%scase %s%s: %s.IdAt(%d, 0x%016x)\n", ind, e.Name, v, writer, g.knownOrdinal(id), id)
		}
		g.pf("%sdefault: return false\n%s}\n", ind, ind)
		return
	}
	kind := ir.TableWireScalarKind(f)
	switch kind {
	case tkBool:
		g.pf("%sif %s { %s.Put8(1) } else { %s.Put8(0) }\n", ind, expr, writer, writer)
	case tkF32, tkF64:
		g.needsMath = true
		bits := 32
		if kind == tkF64 {
			bits = 64
		}
		g.pf("%s%s.Put%d(math.Float%dbits(%s))\n", ind, writer, bits, bits, expr)
	default:
		width := ir.TableKindWidth(kind) * 8
		if width == 128 {
			g.pf("%s%s.Put128(%s.Lo, %s.Hi)\n", ind, writer, expr, expr)
		} else {
			g.pf("%s%s.Put%d(uint%d(%s))\n", ind, writer, width, width, expr)
		}
	}
}

func writerPointer(name string) string {
	if name == "w" {
		return name
	}
	return "&" + name
}

func byteValueRides(f *ir.Field, expr string) string {
	if len(f.DefBytes) == 0 {
		return expr + "Length > 0"
	}
	return fmt.Sprintf("%sLength != %d || string(%s[:%sLength]) != %q", expr, len(f.DefBytes), expr, expr, string(f.DefBytes))
}
