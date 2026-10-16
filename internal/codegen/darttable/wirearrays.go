// FIXED POSITIONAL AND ENUM-KEYED ARRAYS ON THE VARIABLE FORM
// (docs/SPEC-TABLES.md §2.4, §3, §3.2), form byte 1, in Dart: `[N]T` rides as
// kind 14 with every declared element (position is identity), and `[E]T`
// rides as kind 16, one `(key reference, L, element)` triple per slot that is
// not at its default, ascending by variant ordinal. Both are written as
// internal/tablewire's encodeArray and encodeKeyed write them.
package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const wireKindKeyed = 16

func isKeyed(f *ir.Field) bool { return f.KeyEnum != "" && f.Array == ir.ArrayFixed }

func isFixedArray(f *ir.Field) bool { return f.KeyEnum == "" && f.Array == ir.ArrayFixed }

// singleOf is one element of an array field as a field of its own: the same
// type, defaults and bounds, no array shape (§2.4, §3).
func singleOf(f *ir.Field) *ir.Field {
	s := *f
	s.Array = ir.ArrayNone
	s.ArrayBound = 0
	s.KeyEnum = ""
	s.KeyEnumRef = nil
	return &s
}

// fixedElemCarried reports whether the element of a `[N]T` or `[E]T` is one
// this form carries: a scalar, a string, an enum, a table or a pointer to one.
func fixedElemCarried(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Blob() {
		return false
	}
	// a fixed array of unions (§2.6, §3): element kind 15, every slot
	if un := unionRef(f); un != nil {
		return f.KeyEnum == "" && cardWireUnionSeen(un, seen)
	}
	return cardScalarOrTable(f, seen)
}

// arrayElemKind is the element kind byte an array body opens with (§3): 17 for
// a pointer element, the field's element kind otherwise.
func arrayElemKind(f *ir.Field) int {
	if f.Type.Pointer {
		return wireKindPtr
	}
	return ir.TableWireElemKind(f)
}

// elemDefault is the Dart expression one element of an array holds at its
// declared default (§5).
func (g *wireGen) elemDefault(f *ir.Field) string {
	s := singleOf(f)
	switch {
	case isPointerTable(s):
		return "null"
	case isByValueTable(s), unionRef(s) != nil:
		return s.Type.Name + "()"
	case isWide(s):
		return g.wideDefault(s)
	case s.Type.Kind == ir.TBool:
		return fmt.Sprintf("%v", s.HasDefault && s.DefBool)
	case s.Type.Kind == ir.TInt || s.Type.Kind == ir.TBits || isFlags(s):
		return g.intDefault(s)
	case s.Type.Kind == ir.TFloat32 || s.Type.Kind == ir.TFloat64:
		return g.floatDefault(s)
	case s.Type.Kind == ir.TString || s.Type.Kind == ir.TBytes:
		return g.stringDefault(s)
	case enumRef(s) != nil:
		return g.enumDefault(s)
	}
	return "0"
}

// fixedStorageDecl is the member of a `[N]T` or `[E]T`: a list of exactly the
// declared slot count, every slot at the element's default (§2.4, §5).
func (g *wireGen) fixedStorageDecl(f *ir.Field, name string) string {
	et := dartListElemType(f)
	return fmt.Sprintf("List<%s> %s = List<%s>.generate(%d, (_) => %s);", et, name, et, f.ArrayBound, g.elemDefault(f))
}

func arrayHelper(st *ir.Struct, f *ir.Field, what string) string {
	return "_" + dartName(st.Name) + capitalize(fieldMember(f)) + what
}

// emitSaveFixed writes a `[N]T` (§3, internal/tablewire encodeField): a fixed
// array of tables ALWAYS rides, every other one elides when every element is
// at its default, and a riding one writes all N elements.
func (g *wireGen) emitSaveFixed(f *ir.Field, expr, ind string, id uint64) {
	ek := arrayElemKind(f)
	open := "{"
	switch {
	case ek == wireKindUnion:
		// a fixed array of unions elides when every slot is None (§3)
		open = fmt.Sprintf("if (%s.any((e) => e.armTag != 0)) {", expr)
	case ek != wireKindTable:
		open = fmt.Sprintf("if (%s.any((e) => e != %s)) {", expr, g.elemDefault(f))
	}
	g.pf("%s%s\n", ind, open)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, ek)
	g.pf("%s\ts.putLeb(%s.length);\n", ind, expr)
	g.pf("%s\tfor (final e in %s) {\n", ind, expr)
	g.emitPutElem(f, ek, "e", "s", ind+"\t\t")
	g.pf("%s\t}\n", ind)
	g.pf("%s\tw.putLeb(ref);\n", ind)
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
	g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
	g.pf("%s\tw.putBytes(s.buf);\n", ind)
	g.pf("%s}\n", ind)
}

// emitSaveKeyed writes a `[E]T` as kind 16 (docs/SPEC-TABLES.md §3.2,
// internal/tablewire encodeKeyed): the field id is interned first, then per
// slot the key's variant id, then the element; a slot at its default (a table
// body that is its terminator alone) rolls both back and elides, and an array
// with no present slot is not written at all.
func (g *wireGen) emitSaveKeyed(f *ir.Field, expr, ind string, id uint64) {
	e := f.KeyEnumRef
	single := singleOf(f)
	ek := ir.TableWireElemKind(f)
	g.pf("%s{\n", ind)
	g.pf("%s\tfinal mark = w.state.mark();\n", ind)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal body = w.sub();\n", ind)
	g.pf("%s\tvar pairs = 0;\n", ind)
	g.pf("%s\tfor (var slot = 0; slot < %d; slot++) {\n", ind, f.ArrayBound)
	g.pf("%s\t\tfinal e = %s[slot];\n", ind, expr)
	g.pf("%s\t\tfinal slotMark = w.state.mark();\n", ind)
	g.pf("%s\t\tfinal keyRef = w.refFor(%s);\n", ind, enumVidExpr(e, "slot + 1"))
	g.pf("%s\t\tfinal eb = w.sub();\n", ind)
	if ek == wireKindTable {
		g.emitPutPayload(single, "e", "eb", ind+"\t\t")
		g.pf("%s\t\tif (eb.buf.length <= 1) { w.state.rollback(slotMark); continue; }\n", ind)
	} else {
		g.pf("%s\t\tif (e == %s) { w.state.rollback(slotMark); continue; }\n", ind, g.elemDefault(f))
		g.emitPutPayload(single, "e", "eb", ind+"\t\t")
	}
	g.pf("%s\t\tbody.putLeb(keyRef);\n", ind)
	g.pf("%s\t\tbody.putLeb(eb.buf.length);\n", ind)
	g.pf("%s\t\tbody.putBytes(eb.buf);\n", ind)
	g.pf("%s\t\tpairs++;\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tif (pairs == 0) {\n", ind)
	g.pf("%s\t\tw.state.rollback(mark);\n", ind)
	g.pf("%s\t} else {\n", ind)
	g.pf("%s\t\tfinal inner = w.sub();\n", ind)
	g.pf("%s\t\tinner.putByte(%d);\n", ind, ek)
	g.pf("%s\t\tinner.putLeb(pairs);\n", ind)
	g.pf("%s\t\tinner.putBytes(body.buf);\n", ind)
	g.pf("%s\t\tw.putLeb(ref);\n", ind)
	g.pf("%s\t\tw.putByte(%d);\n", ind, wireKindKeyed)
	g.pf("%s\t\tw.putLeb(inner.buf.length);\n", ind)
	g.pf("%s\t\tw.putBytes(inner.buf);\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s}\n", ind)
}

// emitArrayHelpers emits, ahead of a table's load body, the tolerant readers
// of its `[N]T` and `[E]T` fields (§3, §3.2, §4): each reads the array body
// out of a reader bounded to the field's L into a fresh list at the element
// defaults, so a slot the wire does not carry reads its default. An element
// kind the field does not declare is a kind mismatch and the field keeps its
// defaults; framing damage inside L counts malformed and the parent reads on.
func (g *wireGen) emitArrayHelpers(st *ir.Struct) {
	for _, f := range st.Fields {
		switch {
		case isFixedArray(f):
			g.emitFixedHelper(st, f)
		case isKeyed(f):
			g.emitKeyedHelper(st, f)
		}
	}
}

func (g *wireGen) emitFixedHelper(st *ir.Struct, f *ir.Field) {
	ek := arrayElemKind(f)
	et := dartListElemType(f)
	g.pf("bool %s(TableWireReader p, %s v) {\n", arrayHelper(st, f, "Fixed"), st.Name)
	g.pf("\tif (!p.has(1)) { p.report.malformed = true; return false; }\n")
	g.pf("\tfinal elemKind = p.readByte();\n")
	g.pf("\tfinal count = p.readLeb();\n")
	g.pf("\tif (count == null) { p.report.malformed = true; return false; }\n")
	g.pf("\tif (elemKind != %d) { p.report.kindMismatch++; return true; }\n", ek)
	g.pf("\tfinal out = <%s>[];\n", et)
	// a count past the declared slots keeps them and counts one clamp; the
	// excess skips via the length (§4, internal/tablewire arrayBody)
	g.pf("\tvar keep = count;\n")
	g.pf("\tif (keep > %d) { keep = %d; p.report.clamped++; }\n", f.ArrayBound, f.ArrayBound)
	g.pf("\tfor (var i = 0; i < keep; i++) {\n")
	g.emitGetElem(f, ek, "out", "p", "\t\t")
	g.pf("\t}\n")
	g.pf("\tif (keep == count && p.pos != p.limit) { p.report.malformed = true; return false; }\n")
	g.pf("\tfinal fresh = List<%s>.generate(%d, (_) => %s);\n", et, f.ArrayBound, g.elemDefault(f))
	g.pf("\tfor (var i = 0; i < out.length && i < %d; i++) {\n", f.ArrayBound)
	g.pf("\t\tfresh[i] = out[i];\n")
	g.pf("\t}\n")
	g.pf("\tv.%s = fresh;\n", fieldMember(f))
	g.pf("\treturn true;\n")
	g.pf("}\n\n")
}

func (g *wireGen) emitKeyedHelper(st *ir.Struct, f *ir.Field) {
	e := f.KeyEnumRef
	ek := ir.TableWireElemKind(f)
	et := dartListElemType(f)
	elem := arrayHelper(st, f, "Elem")
	g.pf("bool %s(TableWireReader p, List<%s> v, int slot, int kind) {\n", elem, et)
	g.emitGetPayload(singleOf(f), "v[slot]", "p", "\t")
	g.pf("}\n\n")
	g.pf("bool %s(TableWireReader p, %s v) {\n", arrayHelper(st, f, "Keyed"), st.Name)
	g.pf("\tif (!p.has(1)) { p.report.malformed = true; return false; }\n")
	g.pf("\tfinal elemKind = p.readByte();\n")
	g.pf("\tfinal pairs = p.readLeb();\n")
	g.pf("\tif (pairs == null) { p.report.malformed = true; return false; }\n")
	g.pf("\tif (elemKind != %d) { p.report.kindMismatch++; return true; }\n", ek)
	g.pf("\tfinal fresh = List<%s>.generate(%d, (_) => %s);\n", et, f.ArrayBound, g.elemDefault(f))
	g.pf("\tfor (var k = 0; k < pairs; k++) {\n")
	g.pf("\t\tfinal keyRef = p.readLeb();\n")
	g.pf("\t\tif (keyRef == null) { p.report.malformed = true; return false; }\n")
	g.pf("\t\tfinal vid = p.refToId[keyRef];\n")
	g.pf("\t\tfinal n = p.readLeb();\n")
	g.pf("\t\tif (vid == null || n == null || !p.has(n)) { p.report.malformed = true; return false; }\n")
	g.pf("\t\tfinal slot = switch (vid) {")
	for i := range e.Variants {
		g.pf(" %s => %d,", dartU64Literal(ir.TableWireId(e.VariantWireName(i))), i)
	}
	g.pf(" _ => -1 };\n")
	g.pf("\t\tif (slot < 0 || slot >= %d) {\n", f.ArrayBound)
	g.pf("\t\t\tp.report.unknown++;\n")
	g.pf("\t\t\tif (p.retain != null) p.report.retainLost++;\n")
	g.pf("\t\t} else {\n")
	g.pf("\t\t\t%s(TableWireReader.sub(p, p.pos, p.pos + n), fresh, slot, elemKind);\n", elem)
	g.pf("\t\t}\n")
	g.pf("\t\tp.pos += n;\n")
	g.pf("\t}\n")
	g.pf("\tif (p.pos != p.limit) { p.report.malformed = true; return false; }\n")
	g.pf("\tv.%s = fresh;\n", fieldMember(f))
	g.pf("\treturn true;\n")
	g.pf("}\n\n")
}

// emitLoadArray is the field case of a `[N]T` (kind 14) or `[E]T` (kind 16):
// the kind byte, L, then the helper over a reader bounded to L (§3, §3.2).
func (g *wireGen) emitLoadArray(st *ir.Struct, f *ir.Field, ind string) {
	kind, helper := wireKindArray, arrayHelper(st, f, "Fixed")
	if isKeyed(f) {
		kind, helper = wireKindKeyed, arrayHelper(st, f, "Keyed")
	}
	g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, kind)
	g.pf("%s\t{\n", ind)
	g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
	g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
	g.pf("%s\t\t%s(TableWireReader.sub(r, r.pos, r.pos + len), v);\n", ind, helper)
	g.pf("%s\t\tr.pos += len;\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tbreak;\n", ind)
}

// emitNumberArray numbers a `[N]T` or `[E]T` (§3.1, internal/tablewire
// visitEdges): every slot of an array of pointers is an edge in index order,
// a null slot is not, and an array of tables is descended slot by slot.
func (g *wireGen) emitNumberArray(f *ir.Field, expr, ind string) {
	if un := unionRef(f); un != nil {
		g.pf("%sfor (final e in %s) { %s(s, e, open); }\n", ind, expr, unionFn(un, "Number"))
		return
	}
	ref := tableRef(f)
	if ref == nil {
		return
	}
	g.pf("%sfor (final e in %s) {\n", ind, expr)
	if f.Type.Pointer {
		g.pf("%s\tif (e != null) s.noteTable(%s, e, open, (o) { %sNumber(s, e, o); });\n", ind, typeConst(ref), dartName(ref.Name))
	} else {
		g.pf("%s\t%sNumber(s, e, open);\n", ind, dartName(ref.Name))
	}
	g.pf("%s}\n", ind)
}
