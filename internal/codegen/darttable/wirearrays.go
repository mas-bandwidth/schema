// FIXED POSITIONAL AND ENUM-KEYED ARRAYS ON THE VARIABLE FORM
// (docs/SPEC-TABLES.md §2.4, §3, §3.2), form byte 1, in Dart: `[N]T` rides as
// kind 14 with every declared element (position is identity), and `[E]T`
// rides as kind 16, one `(key reference, L, element)` triple per slot that is
// not at its default, ascending by variant ordinal. Both are written as
// internal/tablewire's encodeArray and encodeKeyed write them.
package darttable

import (
	"fmt"
	"strings"

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
	et := g.listElemType(f)
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
	case ek == wireKindF32 || ek == wireKindF64:
		// a float element's elision compares BITS (docs/SPEC-TABLES.md §3.3):
		// one -0 under a +0 default is enough for the array to ride
		open = fmt.Sprintf("if (%s.any((e) => %s)) {", expr, floatRides(singleOf(f), "e", g.elemDefault(f)))
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
	switch ek {
	case wireKindTable:
		g.emitPutPayload(single, "e", "eb", ind+"\t\t")
		g.pf("%s\t\tif (eb.buf.length <= 1) { w.state.rollback(slotMark); continue; }\n", ind)
	case wireKindF32, wireKindF64:
		// a float slot at its default elides by BITS (docs/SPEC-TABLES.md §3.3)
		g.pf("%s\t\tif (!(%s)) { w.state.rollback(slotMark); continue; }\n", ind, floatRides(single, "e", g.elemDefault(f)))
		g.emitPutPayload(single, "e", "eb", ind+"\t\t")
	default:
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

// emitWidenedElems emits the read of `keep` elements at a WIDENED element kind
// (§4): every element decodes at the wire kind's width, ONE `widened` counts
// for the field however many it holds, and framing damage keeps the prefix.
// It needs `elemKind` and `keep` in scope.
func (g *wireGen) emitWidenedElems(f *ir.Field, ek int, out, p, ind string) {
	g.pf("%s%s.report.widened++;\n", ind, p)
	g.pf("%sfinal w = _kindWidth(elemKind);\n", ind)
	g.pf("%sfor (var i = 0; i < keep; i++) {\n", ind)
	g.pf("%s\tif (!%s.has(w)) { %s.report.malformed = true; break; }\n", ind, p, p)
	if ek == wireKindF64 {
		// a widened float32 element is refused on its raw bits as any float
		// read is (docs/SPEC-TABLES.md §3.3)
		g.pf("%s\t%s.add(%s.readF32(%s.pos, '%s')); %s.pos += 4;\n", ind, out, p, p, g.floatOwner(f), p)
	} else {
		g.pf("%s\tvar raw = 0;\n", ind)
		g.pf("%s\tfor (var b = 0; b < w; b++) { raw |= %s.readByte() << (8 * b); }\n", ind, p)
		g.emitElemInt(f, "_signExtend(raw, w)", ek >= wireKindI8 && ek <= wireKindI64, out, p, ind+"\t")
	}
	g.pf("%s}\n", ind)
}

// absentOnMismatch is the statement an array under a foreign element kind runs
// besides counting it: an OPTIONAL array stays ABSENT (§2.3, internal/tablewire
// arrayBody), so the presence the field set on arrival comes back down.
func (g *wireGen) absentOnMismatch(f *ir.Field) string {
	if !f.Type.Optional {
		return ""
	}
	return " v." + hasMember(f) + " = had;"
}

// hadParam is the trailing parameter an OPTIONAL array's helper takes: the
// presence the field held before this occurrence, which a foreign element kind
// restores (an occurrence that turns back leaves the field standing, §4).
func hadParam(f *ir.Field) string {
	if f.Type.Optional {
		return ", bool had"
	}
	return ""
}

// hadArg is the argument for hadParam: the local emitLoadField declared for the
// optional field whose body is being emitted (its bare field is what reaches
// the array reader), or nothing.
func (g *wireGen) hadArg() string {
	if g.optHad == "" {
		return ""
	}
	return ", " + g.optHad
}

// hadName names the local that holds an optional field's presence on entry.
func hadName(f *ir.Field) string {
	has := hasMember(f)
	return "had" + strings.ToUpper(has[:1]) + has[1:]
}

// widenCond is the Dart condition on the wire's element kind under which it
// WIDENS to the declared kind ek (§4, ir.TableKindWidens), or "" when no kind
// does. A signed or unsigned integer ladder widens from its narrower kinds, an
// f64 from an f32.
func widenCond(ek int) string {
	switch {
	case ek > wireKindI8 && ek <= wireKindI64:
		return fmt.Sprintf("elemKind >= %d && elemKind < %d", wireKindI8, ek)
	case ek > wireKindU8 && ek <= wireKindU64:
		return fmt.Sprintf("elemKind >= %d && elemKind < %d", wireKindU8, ek)
	case ek == wireKindF64:
		return fmt.Sprintf("elemKind == %d", wireKindF32)
	}
	return ""
}

func (g *wireGen) emitFixedHelper(st *ir.Struct, f *ir.Field) {
	ek := arrayElemKind(f)
	et := g.listElemType(f)
	// THE HEADER (element kind and count) is read by the FIELD against the
	// PARENT buffer, exactly as internal/tablewire arrayBody reads it; the
	// helper gets the body the header framed and the header it carried, and
	// judges the element kind before the count is looked at (§3, §4).
	g.pf("bool %s(TableWireReader p, %s v, int elemKind, int count%s) {\n", arrayHelper(st, f, "Fixed"), st.Name, hadParam(f))
	wc := widenCond(ek)
	if wc == "" || f.Type.Pointer {
		g.pf("\tif (elemKind != %d) { p.report.kindMismatch++;%s return true; }\n", ek, g.absentOnMismatch(f))
	} else {
		g.pf("\tif (elemKind != %d && !(%s)) { p.report.kindMismatch++;%s return true; }\n", ek, wc, g.absentOnMismatch(f))
	}
	g.pf("\tfinal out = <%s>[];\n", et)
	// a count past the declared slots keeps them and counts one clamp; the
	// excess skips via the length (§4, internal/tablewire arrayBody)
	g.pf("\tvar keep = count;\n")
	g.pf("\tif (keep > %d || keep < 0) { keep = %d; p.report.clamped++; }\n", f.ArrayBound, f.ArrayBound)
	if wc != "" && !f.Type.Pointer {
		// WIDENED at an element kind (§4): every element decodes at the wire
		// kind's width, and ONE `widened` counts for the field
		g.pf("\tif (elemKind != %d) {\n", ek)
		g.emitWidenedElems(f, ek, "out", "p", "\t\t")
		g.pf("\t} else {\n")
		g.pf("\t\tfor (var i = 0; i < keep; i++) {\n")
		g.emitGetElemBad(f, ek, "out", "p", "\t\t\t", "{ p.report.malformed = true; break; }")
		g.pf("\t\t}\n")
		g.pf("\t}\n")
	} else {
		g.pf("\tfor (var i = 0; i < keep; i++) {\n")
		if unionRef(f) != nil {
			// AN ELEMENT OF AN ARRAY OF UNIONS IS RESET the moment its arm id
			// is read, before the rest of its framing is checked, so a repeat
			// under the field id leaves no arm an earlier occurrence decoded
			// standing: the last occurrence wins whole, damaged or not (§3, §4).
			g.pf("\t\tif (p.has(1)) v.%s[i] = %s();\n", fieldMember(f), f.Type.Name)
		}
		g.emitGetElemBad(f, ek, "out", "p", "\t\t", "{ p.report.malformed = true; break; }")
		g.pf("\t}\n")
	}
	// The decoded prefix lands in the field's own slots (internal/tablewire
	// arrayBody decodes in place): a slot the body does not reach keeps what it
	// held, so a repeated id whose later occurrence is damaged leaves the
	// earlier occurrence standing (§4).
	g.pf("\tfinal slots = v.%s;\n", fieldMember(f))
	g.pf("\tfor (var i = 0; i < out.length && i < %d; i++) {\n", f.ArrayBound)
	g.pf("\t\tslots[i] = out[i];\n")
	g.pf("\t}\n")
	g.pf("\treturn true;\n")
	g.pf("}\n\n")
}

func (g *wireGen) emitKeyedHelper(st *ir.Struct, f *ir.Field) {
	e := f.KeyEnumRef
	ek := ir.TableWireElemKind(f)
	et := g.listElemType(f)
	elem := arrayHelper(st, f, "Elem")
	g.pf("bool %s(TableWireReader p, List<%s> v, int slot, int kind) {\n", elem, et)
	g.lenientPayload = true
	g.emitGetPayload(singleOf(f), "v[slot]", "p", "\t")
	g.lenientPayload = false
	g.pf("}\n\n")
	// the header (element kind, pair count) arrives read against the parent
	// by emitLoadArray; p is the body under the field's L
	g.pf("bool %s(TableWireReader p, %s v, int elemKind, int pairs%s) {\n", arrayHelper(st, f, "Keyed"), st.Name, hadParam(f))
	// WIDENED at a keyed body's element kind (§4): one count for the field,
	// every slot read at the wire kind's width
	if wc := widenCond(ek); wc != "" {
		g.pf("\tif (elemKind != %d) {\n", ek)
		g.pf("\t\tif (!(%s)) { p.report.kindMismatch++;%s return true; }\n", wc, g.absentOnMismatch(f))
		g.pf("\t\tp.report.widened++;\n")
		g.pf("\t}\n")
	} else {
		g.pf("\tif (elemKind != %d) { p.report.kindMismatch++;%s return true; }\n", ek, g.absentOnMismatch(f))
	}
	// The triples land in the field's own slots, in place (internal/tablewire
	// keyed): a slot the body never sends keeps what it held, damage inside the
	// body stops it and keeps what it decoded, and unread triples and slack
	// skip via the length (§3.2, §4).
	g.pf("\tfinal slots = v.%s;\n", fieldMember(f))
	g.pf("\tfor (var k = 0; k < pairs || pairs < 0; k++) {\n")
	g.pf("\t\tfinal keyRef = p.readLeb();\n")
	g.pf("\t\tif (keyRef == null || keyRef == 0) { p.report.malformed = true; break; }\n")
	g.pf("\t\tfinal vid = p.refToId[keyRef];\n")
	g.pf("\t\tif (vid == null) { p.report.malformed = true; break; }\n")
	g.pf("\t\tfinal n = p.readLeb();\n")
	g.pf("\t\tif (n == null || n < 0 || !p.has(n)) { p.report.malformed = true; break; }\n")
	g.pf("\t\tfinal slot = switch (vid) {")
	for i := range e.Variants {
		g.pf(" %s => %d,", dartU64Literal(ir.TableWireId(e.VariantWireName(i))), i)
	}
	g.pf(" _ => -1 };\n")
	g.pf("\t\tif (slot < 0 || slot >= %d) {\n", f.ArrayBound)
	g.pf("\t\t\tp.report.unknown++;\n")
	g.pf("\t\t\tif (p.retain != null) p.report.retainLost++;\n")
	g.pf("\t\t} else {\n")
	g.pf("\t\t\t%s(TableWireReader.sub(p, p.pos, p.pos + n), slots, slot, elemKind);\n", elem)
	g.pf("\t\t}\n")
	g.pf("\t\tp.pos += n;\n")
	g.pf("\t}\n")
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
	g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
	g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
	// Both arrays read their header against the parent: a count past the
	// body's own L still reads, a positional count clamps at the bound
	// before the body is consulted, and a keyed body's element kind is
	// judged before its triples (internal/tablewire decode.go arrayBody,
	// keyed, subTo; docs/SPEC-TABLES.md §3, §3.2, §4). A damaged count is
	// framing damage inside the body's own L: counted, and the walk goes on
	// past it.
	g.pf("%s\t\tfinal elemKind = r.readByte();\n", ind)
	g.pf("%s\t\tfinal count = r.readLeb();\n", ind)
	g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
	g.pf("%s\t\t%s(TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end), v, elemKind, count%s);\n", ind, helper, g.hadArg())
	g.pf("%s\t\tr.pos = end;\n", ind)
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
