// MAPS AND UNION ARMS ON THE VARIABLE FORM (docs/SPEC-TABLES.md §2.6, §2.8,
// §3, §3.1), form byte 1, in Dart: a union field (kind 15, an arm header that
// IS a field header), an array of unions (kind 14 over element kind 15, a None
// element the single zero byte), union arms of the field types this form
// carries (a table body, a scalar, an enum, a string, bytes(N), a pointer, a
// blob, a counted or unbounded array, another union, and the payload-free arm
// at kind 32), and maps (kind 14 over element kind 13, the generated
// `{ key, value }` entry bodies in ascending key order). Every construct here
// is written byte for byte as internal/tablewire's encoder writes it and read
// with the tolerant rules of its decoder (§4); each function cites the rule it
// implements.
package darttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	wireKindUnion     = 15
	wireKindNoPayload = 32
)

// unionRef is the union a field's values come from, or nil (§2.6).
func unionRef(f *ir.Field) *ir.Union {
	if f == nil || f.Type.Kind != ir.TNamed {
		return nil
	}
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

func isUnionField(f *ir.Field) bool { return f.Array == ir.ArrayNone && unionRef(f) != nil }

// cardWireUnionSeen reports whether every arm of a union is a construct this
// form carries (§2.6): an arm is a field line, so it is judged by the field
// rules; the payload-free arm always is.
func cardWireUnionSeen(un *ir.Union, seen map[string]bool) bool {
	for _, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		// a `type` arm and a table arm are one framing: kind 13 over an
		// id-framed body (§2.6, §3)
		if arm.Body() && arm.Ref == nil {
			return false
		}
		if !cardWireFieldSeen(arm.F, seen) {
			return false
		}
	}
	return true
}

// armMember is the Dart storage member holding one arm's value inside the
// generated union class (§2.6): the arm's lowerCamelCase name, an underscore
// appended on a reserved word, exactly as a field's member is spelled.
func armMember(arm ir.UnionVariant) string {
	name := dartName(arm.Name)
	if dartReserved[name] || name == "armTag" {
		return name + "_"
	}
	return name
}

// armWireKind is the kind byte an arm header carries (docs/SPEC-TABLES.md §2.6,
// §3): 32 for the payload-free arm, 13 for a table body, 17 for a pointer or a
// blob, 14 for bytes(N), and otherwise the kind a FIELD of the arm's type
// takes. It is internal/tablewire's armWireKind, spelled again here.
func armWireKind(arm ir.UnionVariant) int {
	if arm.Void() {
		return wireKindNoPayload
	}
	if arm.Body() {
		return wireKindTable
	}
	f := arm.F
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return wireKindPtr
	}
	if f.Type.Kind == ir.TBytes {
		return wireKindArray
	}
	return ir.TableWireFieldKind(f)
}

// fieldReaches is every table a field reaches through a map or a union (§2.6,
// §2.8): the map's generated entry, and every table an arm names by value or
// by pointer, one level of union at a time. The plain table edges are the
// caller's.
func fieldReaches(f *ir.Field) []*ir.Struct {
	var out []*ir.Struct
	if f.IsMap() {
		out = append(out, f.MapEntry)
		return out
	}
	if un := unionRef(f); un != nil {
		for _, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			if ref := tableRef(arm.F); ref != nil {
				out = append(out, ref)
			}
			out = append(out, fieldReaches(arm.F)...)
		}
	}
	return out
}

// byValueReaches is the part of fieldReaches a file root cannot be: a map's
// entry and a table carried by value in an arm (§2.8, §3.1). A pointer target
// stays a root, as it does for a plain field.
func byValueReaches(f *ir.Field) []*ir.Struct {
	var out []*ir.Struct
	if f.IsMap() {
		return append(out, f.MapEntry)
	}
	if un := unionRef(f); un != nil {
		for _, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			if ref := tableRef(arm.F); ref != nil && !arm.F.Type.Pointer {
				out = append(out, ref)
			}
			out = append(out, byValueReaches(arm.F)...)
		}
	}
	return out
}

// closureUnions is every union a closure table's fields name, directly or
// through an arm, in name order: each owes one storage class and one set of
// arm helpers in the library (§2.6).
func (g *wireGen) closureUnions() []*ir.Union {
	seen := map[string]*ir.Union{}
	var visit func(un *ir.Union)
	visit = func(un *ir.Union) {
		if un == nil || seen[un.Name] != nil {
			return
		}
		seen[un.Name] = un
		for _, arm := range un.Variants {
			if !arm.Void() {
				visit(unionRef(arm.F))
			}
		}
	}
	for _, st := range g.closure {
		for _, f := range st.Fields {
			visit(unionRef(f))
		}
	}
	var out []*ir.Union
	for _, un := range seen {
		out = append(out, un)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// storageDecl is one member declaration for a field or an arm line: the Dart
// type, the member name and its declared default (§5). It is the member list
// emitStorage writes for a table and emitUnionStorage writes for an arm.
func (g *wireGen) storageDecl(f *ir.Field, name string) string {
	if f.Type.Optional {
		// the value in the ordinary member, PRESENCE beside it (§2.3)
		return g.storageDecl(nonOptional(f), name) + "\n  bool " + hasMember(f) + " = false;"
	}
	switch {
	case f.IsMap():
		et := f.MapEntry.Name
		return fmt.Sprintf("List<%s> %s = <%s>[];", et, name, et)
	case f.Array == ir.ArrayFixed:
		return g.fixedStorageDecl(f, name)
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			return fmt.Sprintf("String? %s;", name)
		}
		return fmt.Sprintf("Uint8List? %s;", name)
	case f.CountedOnWire():
		et := g.listElemType(f)
		return fmt.Sprintf("List<%s> %s = <%s>[];", et, name, et)
	case isUnionField(f):
		return fmt.Sprintf("%s %s = %s();", f.Type.Name, name, f.Type.Name)
	case isPointerTable(f):
		return fmt.Sprintf("%s? %s;", f.Type.Name, name)
	case isByValueTable(f):
		return fmt.Sprintf("%s %s = %s();", f.Type.Name, name, f.Type.Name)
	case isWide(f):
		return fmt.Sprintf("BigInt %s = %s;", name, g.wideDefault(f))
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("bool %s = %v;", name, f.HasDefault && f.DefBool)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || isFlags(f):
		return fmt.Sprintf("int %s = %s;", name, g.intDefault(f))
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return fmt.Sprintf("double %s = %s;", name, g.floatDefault(f))
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return fmt.Sprintf("String %s = %s;", name, g.stringDefault(f))
	case f.Type.Kind == ir.TWString:
		return fmt.Sprintf("String %s = '';", name)
	case f.Type.Kind == ir.TNamed && enumRef(f) != nil:
		return fmt.Sprintf("%s %s = %s;", g.enumType(enumRef(f)), name, g.enumDefault(f))
	}
	return fmt.Sprintf("int %s = 0;", name)
}

// emitUnionStorage emits a union's Dart storage class (§2.6): armTag is the
// selected arm's 1-based ordinal and 0 is None, and every arm with a payload
// keeps its value in a member of its own at the arm's declared default.
func (g *wireGen) emitUnionStorage(un *ir.Union) {
	g.pf("final class %s {\n", un.Name)
	g.pf("  int armTag = 0;\n")
	for _, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		g.pf("  %s\n", g.storageDecl(arm.F, armMember(arm)))
	}
	g.pf("}\n\n")
}

// emitUnion emits one union's three helpers: the arm header writer, the
// tolerant arm reader and the node numbering of the set arm (§2.6, §3, §3.1).
func (g *wireGen) emitUnion(un *ir.Union) {
	g.emitUnionSave(un)
	g.emitUnionLoad(un)
	g.emitUnionNumber(un)
}

func unionFn(un *ir.Union, what string) string { return "_" + dartName(un.Name) + what }

// emitUnionSave emits _<u>SaveArm: a SET arm written whole, the arm id
// reference, the arm's kind byte, L, then the payload (docs/SPEC-TABLES.md §3,
// internal/tablewire encodeArmHeader). The arm id is interned BEFORE the
// payload is built, so the id table stays in first-use order (§3). A tag that
// names no arm is refused, never written as another arm (§5).
func (g *wireGen) emitUnionSave(un *ir.Union) {
	g.pf("void %s(TableWireWriter w, %s v) {\n", unionFn(un, "SaveArm"), un.Name)
	g.pf("\tswitch (v.armTag) {\n")
	for i, arm := range un.Variants {
		g.pf("\t\tcase %d:\n", i+1)
		g.pf("\t\t\t{\n")
		g.pf("\t\t\t\tfinal ref = w.refFor(%s);\n", dartU64Literal(ir.TableWireId(arm.WireName())))
		g.pf("\t\t\t\tfinal p = w.sub();\n")
		if !arm.Void() {
			g.emitPutPayload(arm.F, "v."+armMember(arm), "p", "\t\t\t\t")
		}
		g.pf("\t\t\t\tw.putLeb(ref);\n")
		g.pf("\t\t\t\tw.putByte(%d);\n", armWireKind(arm))
		g.pf("\t\t\t\tw.putLeb(p.buf.length);\n")
		g.pf("\t\t\t\tw.putBytes(p.buf);\n")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\tbreak;\n")
	}
	g.pf("\t\tdefault:\n")
	g.pf("\t\t\tthrow StateError('union %s: tag ${v.armTag} names no arm — save refuses it (docs/SPEC-TABLES.md §5)');\n", un.Name)
	g.pf("\t}\n}\n\n")
}

// enumVidExpr is a Dart expression for an enum value's variant-name id (§3):
// a switch over the 1-based ordinals, 0 for None.
func enumVidExpr(e *ir.Enum, expr string) string {
	var s strings.Builder
	s.WriteString("switch (" + expr + ") {")
	for i := range e.Variants {
		fmt.Fprintf(&s, " %d => %s,", i+1, dartU64Literal(ir.TableWireId(e.VariantWireName(i))))
	}
	return s.String() + " _ => 0 }"
}

// emitPutPayload writes ONE value at its kind's payload framing, with no
// header (docs/SPEC-TABLES.md §3's arm payload table, internal/tablewire
// encodeArm): a table body for a table, the value at its width for a scalar,
// the UTF-8 bytes for a string, the array body for bytes(N) or an array, a
// node index for a pointer or a blob (null is 0), a variant reference for an
// enum (None is 0), and the union payload in place for a nested union.
func (g *wireGen) emitPutPayload(f *ir.Field, expr, p, ind string) {
	switch {
	case f.Type.Blob():
		blob := "_kBytesBlobId"
		val := expr + "!"
		if f.Type.Kind == ir.TString {
			blob = "_kStringBlobId"
			val = expr + "!"
		}
		g.pf("%s%s.putLeb(%s == null ? 0 : %s.state.blobIndex(%s, %s));\n", ind, p, expr, p, blob, val)
	case isPointerTable(f):
		g.pf("%s%s.putLeb(%s == null ? 0 : %s.state.indexOf(%s!));\n", ind, p, expr, p, expr)
	case isByValueTable(f):
		g.pf("%s%sWireSaveBody(%s, %s);\n", ind, dartName(tableRef(f).Name), p, expr)
	case isUnionField(f):
		g.pf("%sif (%s.armTag == 0) {\n", ind, expr)
		g.pf("%s\t%s.putLeb(0);\n", ind, p)
		g.pf("%s} else {\n", ind)
		g.pf("%s\t%s(%s, %s);\n", ind, unionFn(unionRef(f), "SaveArm"), p, expr)
		g.pf("%s}\n", ind)
	case f.CountedOnWire() || f.Array == ir.ArrayFixed:
		ek := listElementKind(f)
		if f.Type.Pointer {
			ek = wireKindPtr
		}
		g.pf("%s%s.putByte(%d);\n", ind, p, ek)
		g.pf("%s%s.putLeb(%s.length);\n", ind, p, expr)
		g.pf("%sfor (final e in %s) {\n", ind, expr)
		g.emitPutElem(f, ek, "e", p, ind+"\t")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TWString: // kind 33: the code units, L is the byte length (§3)
		g.emitWStringPayload(expr, p, ind)
	case f.Type.Kind == ir.TString:
		g.pf("%s%s.putString(%s);\n", ind, p, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%s{\n", ind)
		g.pf("%s\tfinal b = latin1.encode(%s);\n", ind, expr)
		g.pf("%s\t%s.putByte(%d);\n", ind, p, wireKindU8)
		g.pf("%s\t%s.putLeb(b.length);\n", ind, p)
		g.pf("%s\t%s.putBytes(b);\n", ind, p)
		g.pf("%s}\n", ind)
	case enumRef(f) != nil:
		g.pf("%s%s.putLeb(%s == 0 ? 0 : %s.refFor(%s));\n", ind, p, expr, p, enumVidExpr(enumRef(f), expr))
	default:
		g.emitPutElem(f, ir.TableWireScalarKind(f), expr, p, ind)
	}
}

// emitPutElem writes one ARRAY element at its element kind's framing (§3): a
// node index for a pointer (null is 0), a length-framed body for a table, a
// length-framed string, a variant reference for an enum, the arm header or
// the single zero byte for a union (position is identity in an array), and a
// scalar at its kind's width.
func (g *wireGen) emitPutElem(f *ir.Field, ek int, e, p, ind string) {
	switch ek {
	case wireKindPtr:
		g.pf("%s%s.putLeb(%s == null ? 0 : %s.state.indexOf(%s));\n", ind, p, e, p, e)
	case wireKindTable:
		g.pf("%s{\n", ind)
		g.pf("%s\tfinal b = %s.sub();\n", ind, p)
		g.pf("%s\t%sWireSaveBody(b, %s);\n", ind, dartName(tableRef(f).Name), e)
		g.pf("%s\t%s.putLeb(b.buf.length);\n", ind, p)
		g.pf("%s\t%s.putBytes(b.buf);\n", ind, p)
		g.pf("%s}\n", ind)
	case wireKindString:
		g.pf("%s{\n", ind)
		g.pf("%s\tfinal b = utf8.encode(%s);\n", ind, e)
		g.pf("%s\t%s.putLeb(b.length);\n", ind, p)
		g.pf("%s\t%s.putBytes(b);\n", ind, p)
		g.pf("%s}\n", ind)
	case wireKindEnum:
		g.pf("%s%s.putLeb(%s == 0 ? 0 : %s.refFor(%s));\n", ind, p, e, p, enumVidExpr(enumRef(f), e))
	case wireKindUnion:
		g.pf("%sif (%s.armTag == 0) {\n", ind, e)
		g.pf("%s\t%s.putLeb(0);\n", ind, p)
		g.pf("%s} else {\n", ind)
		g.pf("%s\t%s(%s, %s);\n", ind, unionFn(unionRef(f), "SaveArm"), p, e)
		g.pf("%s}\n", ind)
	case wireKindBool:
		g.pf("%s%s.putByte(%s ? 1 : 0);\n", ind, p, e)
	case wireKindF32:
		g.pf("%s%s.putU32(_f32Write(%s, '%s'));\n", ind, p, e, g.floatOwner(f))
	case wireKindF64:
		g.pf("%s%s.putU64(_f64Write(%s, '%s'));\n", ind, p, e, g.floatOwner(f))
	default:
		if ir.TableKindWide(ek) { // kinds 18-29, raw at the width (§3, §15)
			g.pf("%s_putWide(%s, %s, %d);\n", ind, p, e, ir.TableKindWidth(ek))
			return
		}
		put := "putU64"
		switch ir.TableKindWidth(ek) {
		case 1:
			put = "putByte"
		case 2:
			put = "putU16"
		case 4:
			put = "putU32"
		}
		g.pf("%s%s.%s(%s);\n", ind, p, put, e)
	}
}

// emitUnionLoad emits _<u>LoadArm, the tolerant read of one union payload in
// place (docs/SPEC-TABLES.md §3, §4, internal/tablewire unionCell): the zero
// reference is None; otherwise the arm id, the kind byte and L frame the
// payload. An arm this reader cannot name reads None and counts unknown; an
// arm under a kind it does not declare counts a kind mismatch and reads None
// (a smaller kind on the same ladder WIDENS); damage inside L is the arm's own
// and the parent reads on past L. False is framing damage the enclosing body
// cannot read past.
func (g *wireGen) emitUnionLoad(un *ir.Union) {
	for i, arm := range un.Variants {
		g.emitArmDecode(un, i, arm)
	}
	g.pf("bool %s(TableWireReader r, %s v) {\n", unionFn(un, "LoadArm"), un.Name)
	g.pf("\tfinal ref = r.readLeb();\n")
	g.pf("\tif (ref == null) { r.report.malformed = true; return false; }\n")
	g.pf("\tif (ref == 0) { v.armTag = 0; return true; }\n")
	g.pf("\tfinal armId = r.refToId[ref];\n")
	g.pf("\tif (armId == null || !r.has(1)) { r.report.malformed = true; return false; }\n")
	g.pf("\tfinal kind = r.readByte();\n")
	g.pf("\tfinal n = r.readLeb();\n")
	g.pf("\tif (n == null || !r.has(n)) { r.report.malformed = true; return false; }\n")
	// The value is RESET once the arm's framing has held (internal/tablewire
	// unionCell): framing damage before that leaves an earlier occurrence's
	// arm standing, and every outcome after it replaces it.
	g.pf("\tv.armTag = 0;\n")
	g.pf("\tfinal p = TableWireReader.sub(r, r.pos, r.pos + n);\n")
	g.pf("\tswitch (armId) {\n")
	for i, arm := range un.Variants {
		k := armWireKind(arm)
		g.pf("\t\tcase %s:\n", dartU64Literal(ir.TableWireId(arm.WireName())))
		g.pf("\t\t\tif (kind == %d) {\n", k)
		g.pf("\t\t\t\tif (%s(p, v, kind)) v.armTag = %d;\n", unionFn(un, fmt.Sprintf("Arm%d", i+1)), i+1)
		if widens := armWidens(arm); len(widens) > 0 {
			g.pf("\t\t\t} else if (")
			for j, w := range widens {
				if j > 0 {
					g.pf(" || ")
				}
				g.pf("kind == %d", w)
			}
			g.pf(") {\n")
			g.pf("\t\t\t\tif (n != _kindWidth(kind)) {\n")
			g.pf("\t\t\t\t\tr.report.malformed = true;\n")
			g.pf("\t\t\t\t} else if (%s(p, v, kind)) {\n", unionFn(un, fmt.Sprintf("Arm%d", i+1)))
			g.pf("\t\t\t\t\tv.armTag = %d;\n", i+1)
			g.pf("\t\t\t\t\tr.report.widened++;\n")
			g.pf("\t\t\t\t}\n")
		}
		g.pf("\t\t\t} else {\n")
		g.pf("\t\t\t\tr.report.kindMismatch++;\n")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\tbreak;\n")
	}
	g.pf("\t\tdefault:\n")
	g.pf("\t\t\tr.report.unknown++;\n")
	g.pf("\t\t\tif (r.retain != null) r.report.retainLost++;\n")
	g.pf("\t}\n")
	g.pf("\tr.pos += n;\n")
	g.pf("\treturn true;\n")
	g.pf("}\n\n")
}

// armWidens is the list of wire kinds an arm's declared kind WIDENS from (§4):
// a smaller integer kind on the arm's own ladder, or f32 under an f64 arm.
func armWidens(arm ir.UnionVariant) []int {
	if arm.Void() || arm.F.Array != ir.ArrayNone || arm.F.Type.Pointer {
		return nil
	}
	f := arm.F
	switch {
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || isFlags(f):
		var out []int
		for _, w := range widenKinds(ir.TableWireScalarKind(f), f.Type.Kind == ir.TInt && f.Type.Signed) {
			out = append(out, w.k)
		}
		return out
	case f.Type.Kind == ir.TFloat64:
		return []int{wireKindF32}
	}
	return nil
}

// emitArmDecode emits _<u>Arm<i>, ONE arm's payload read out of a reader
// bounded to the arm's L (docs/SPEC-TABLES.md §3's arm payload table,
// internal/tablewire arm). kind is the wire kind, the declared one or one it
// widens from. False is the arm's own damage: malformed counts where the
// framing is at fault and the union reads None.
func (g *wireGen) emitArmDecode(un *ir.Union, i int, arm ir.UnionVariant) {
	g.pf("bool %s(TableWireReader p, %s v, int kind) {\n", unionFn(un, fmt.Sprintf("Arm%d", i+1)), un.Name)
	if arm.Void() {
		// a payload-free arm carries nothing: an L that is not zero is damage
		g.pf("\tif (p.pos != p.limit) { p.report.malformed = true; return false; }\n")
		g.pf("\treturn true;\n}\n\n")
		return
	}
	f := arm.F
	dest := "v." + armMember(arm)
	g.emitGetPayload(f, dest, "p", "\t")
	g.pf("}\n\n")
}

// emitGetPayload reads ONE value at its kind's payload framing from a reader
// bounded to exactly that payload and returns whether it read (the inverse of
// emitPutPayload; docs/SPEC-TABLES.md §3, §4). Text is checked for UTF-8
// without NUL and clamped at a code-point boundary past its bound; an integer
// takes its declared range and bits mask; a fixed-width payload whose L is not
// its width is damage.
func (g *wireGen) emitGetPayload(f *ir.Field, dest, p, ind string) {
	bad := fmt.Sprintf("{ %s.report.malformed = true; return false; }", p)
	switch {
	case f.Type.Blob():
		g.pf("%sfinal idx = %s.readLeb();\n", ind, p)
		g.pf("%sif (idx == null || %s.pos != %s.limit) %s\n", ind, p, p, bad)
		if f.Type.Kind == ir.TString {
			g.pf("%s%s = %s.resolveString(idx);\n", ind, dest, p)
		} else {
			g.pf("%s%s = %s.resolveBytes(idx);\n", ind, dest, p)
		}
		g.pf("%sreturn true;\n", ind)
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%sfinal idx = %s.readLeb();\n", ind, p)
		g.pf("%sif (idx == null || %s.pos != %s.limit) %s\n", ind, p, p, bad)
		g.pf("%s%s = %s.resolveTable(idx, %s) as %s?;\n", ind, dest, p, typeConst(ref), ref.Name)
		g.pf("%sreturn true;\n", ind)
	case isByValueTable(f) && g.lenientPayload:
		// a keyed slot's body: damage inside it is the slot's own, so the slot
		// falls back on its declared defaults and the array reads on past it
		// (internal/tablewire keyed: the slot is Refilled, read, and Refilled
		// again when its terminator is not the last byte of its L)
		ref := tableRef(f)
		g.pf("%svar b = %s();\n", ind, ref.Name)
		g.pf("%s%sWireLoadBody(%s, b, true);\n", ind, dartName(ref.Name), p)
		g.pf("%sif (%s.pos != %s.limit) { %s.report.malformed = true; b = %s(); }\n", ind, p, p, p, ref.Name)
		g.pf("%s%s = b;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("%sfinal b = %s();\n", ind, ref.Name)
		g.pf("%s%sWireLoadBody(%s, b, true);\n", ind, dartName(ref.Name), p)
		g.pf("%sif (%s.pos != %s.limit) %s\n", ind, p, p, bad)
		g.pf("%s%s = b;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case isUnionField(f):
		g.pf("%sfinal u = %s();\n", ind, f.Type.Name)
		g.pf("%sif (!%s(%s, u)) return false;\n", ind, unionFn(unionRef(f), "LoadArm"), p)
		g.pf("%s%s = u;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case f.CountedOnWire():
		ek := listElementKind(f)
		if f.Type.Pointer {
			ek = wireKindPtr
		}
		// A payload too short for its own header (the element kind and the
		// count, under two bytes) is INERT (§4): the arm is selected holding the
		// value it has, which is the empty array.
		g.pf("%sif (%s.limit - %s.pos < 2) { %s = <%s>[]; return true; }\n", ind, p, p, dest, g.listElemType(f))
		g.pf("%sfinal elemKind = %s.readByte();\n", ind, p)
		g.pf("%sfinal count = %s.readLeb();\n", ind, p)
		g.pf("%sif (count == null) %s\n", ind, bad)
		// the payload declares another array's element kind: the arm is not
		// this one, so the union reads None (§3)
		wc := widenCond(ek)
		if wc == "" || f.Type.Pointer {
			g.pf("%sif (elemKind != %d) { %s.report.kindMismatch++; return false; }\n", ind, ek, p)
		} else {
			g.pf("%sif (elemKind != %d && !(%s)) { %s.report.kindMismatch++; return false; }\n", ind, ek, wc, p)
		}
		et := g.listElemType(f)
		g.pf("%sfinal out = <%s>[];\n", ind, et)
		// a count past a counted array's bound keeps the bound's prefix and
		// counts one clamp; elements stop at framing damage and keep the prefix
		// they decoded; the excess and any slack skip via the arm's L (§4,
		// internal/tablewire arrayBody, which an arm shares with a field)
		g.pf("%svar keep = count;\n", ind)
		if f.Array == ir.ArrayCounted {
			g.pf("%sif (keep > %d || keep < 0) { keep = %d; %s.report.clamped++; }\n", ind, f.ArrayBound, f.ArrayBound, p)
		} else {
			g.pf("%sif (keep < 0 || keep > 0x7FFFFFFF) %s\n", ind, bad)
		}
		g.emitArmElems(f, ek, wc, p, ind)
		g.pf("%s%s = out;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case f.Array == ir.ArrayFixed:
		// a fixed array arm (§2.6, §3): every declared slot, a slot the payload
		// does not carry at its default
		ek := arrayElemKind(f)
		et := g.listElemType(f)
		g.pf("%sif (%s.limit - %s.pos < 2) { %s = List<%s>.generate(%d, (_) => %s); return true; }\n", ind, p, p, dest, et, f.ArrayBound, g.elemDefault(f))
		g.pf("%sfinal elemKind = %s.readByte();\n", ind, p)
		g.pf("%sfinal count = %s.readLeb();\n", ind, p)
		g.pf("%sif (count == null) %s\n", ind, bad)
		wc := widenCond(ek)
		if wc == "" || f.Type.Pointer {
			g.pf("%sif (elemKind != %d) { %s.report.kindMismatch++; return false; }\n", ind, ek, p)
		} else {
			g.pf("%sif (elemKind != %d && !(%s)) { %s.report.kindMismatch++; return false; }\n", ind, ek, wc, p)
		}
		g.pf("%sfinal out = <%s>[];\n", ind, et)
		g.pf("%svar keep = count;\n", ind)
		g.pf("%sif (keep > %d || keep < 0) { keep = %d; %s.report.clamped++; }\n", ind, f.ArrayBound, f.ArrayBound, p)
		g.emitArmElems(f, ek, wc, p, ind)
		g.pf("%sfinal fresh = List<%s>.generate(%d, (_) => %s);\n", ind, et, f.ArrayBound, g.elemDefault(f))
		g.pf("%sfor (var i = 0; i < out.length && i < %d; i++) { fresh[i] = out[i]; }\n", ind, f.ArrayBound)
		g.pf("%s%s = fresh;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case f.Type.Kind == ir.TWString: // kind 33 (§3)
		g.emitWStringGetPayload(f, dest, p, ind)
	case isWide(f): // kinds 18-29: exactly the width, clamped raw (§3, §4, §15)
		w := ir.TableKindWidth(wideKind(f))
		g.pf("%sif (%s.limit - %s.pos %s %d) %s\n", ind, p, p, g.widthCmp(), w, bad)
		g.pf("%svar x = _getWide(%s, %d, %v);\n", ind, p, w, ir.TableKindSigned(wideKind(f)))
		g.emitWideClamp(f, "x", p, ind)
		g.pf("%s%s = x;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sfinal len = %s.limit - %s.pos;\n", ind, p, p)
		g.pf("%sif (!_lebValid(%s.data, %s.pos, len)) %s\n", ind, p, p, bad)
		g.pf("%sfinal keep = _textBoundary(%s.data, %s.pos, len, %d);\n", ind, p, p, f.Type.Size)
		g.pf("%sif (keep != len) %s.report.clamped++;\n", ind, p)
		g.pf("%s%s = utf8.decode(%s.data.sublist(%s.pos, %s.pos + keep));\n", ind, dest, p, p, p)
		g.pf("%s%s.pos = %s.limit;\n", ind, p, p)
		g.pf("%sreturn true;\n", ind)
	case f.Type.Kind == ir.TBytes:
		// a payload under two bytes is INERT (§4); the elements are bounded by
		// the arm's L, the prefix it covers is kept and the slack skips (arrayBody)
		g.pf("%sif (%s.limit - %s.pos < 2) { %s = ''; return true; }\n", ind, p, p, dest)
		g.pf("%sfinal elemKind = %s.readByte();\n", ind, p)
		g.pf("%sfinal count = %s.readLeb();\n", ind, p)
		g.pf("%sif (count == null) %s\n", ind, bad)
		g.pf("%sif (elemKind != %d) { %s.report.kindMismatch++; return false; }\n", ind, wireKindU8, p)
		g.pf("%svar keep = count;\n", ind)
		g.pf("%sif (keep > %d || keep < 0) { keep = %d; %s.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size, p)
		g.pf("%sif (keep > %s.limit - %s.pos) { keep = %s.limit - %s.pos; %s.report.malformed = true; }\n", ind, p, p, p, p, p)
		g.pf("%s%s = latin1.decode(%s.data.sublist(%s.pos, %s.pos + keep));\n", ind, dest, p, p, p)
		g.pf("%sreturn true;\n", ind)
	case enumRef(f) != nil:
		e := enumRef(f)
		g.pf("%sfinal vref = %s.readLeb();\n", ind, p)
		g.pf("%sif (vref == null%s) %s\n", ind, g.tailCheck(p), bad)
		g.pf("%sif (vref == 0) { %s = %s; return true; }\n", ind, dest, g.enumConst(e, 0))
		g.pf("%sfinal vid = %s.refToId[vref];\n", ind, p)
		g.pf("%sif (vid == null) %s\n", ind, bad)
		g.pf("%sswitch (vid) {\n", ind)
		for i := range e.Variants {
			g.pf("%s\tcase %s: %s = %s;\n", ind, dartU64Literal(ir.TableWireId(e.VariantWireName(i))), dest, g.enumConst(e, i+1))
		}
		g.pf("%s\tdefault: %s = %s; %s.report.unknown++; if (%s.retain != null) %s.report.retainLost++;\n", ind, dest, g.enumConst(e, 0), p, p, p)
		g.pf("%s}\n", ind)
		g.pf("%sreturn true;\n", ind)
	case f.Type.Kind == ir.TBool:
		g.pf("%sif (%s.limit - %s.pos %s 1) %s\n", ind, p, p, g.widthCmp(), bad)
		g.pf("%s%s = %s.readBool();\n", ind, dest, p)
		g.pf("%sreturn true;\n", ind)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		g.pf("%sif (kind == %d) {\n", ind, wireKindF32)
		g.pf("%s\tif (%s.limit - %s.pos %s 4) %s\n", ind, p, p, g.widthCmp(), bad)
		g.pf("%s\t%s = %s.readF32(%s.pos, '%s');\n", ind, dest, p, p, g.floatOwner(f))
		g.pf("%s} else {\n", ind)
		g.pf("%s\tif (%s.limit - %s.pos %s 8) %s\n", ind, p, p, g.widthCmp(), bad)
		g.pf("%s\t%s = %s.readF64(%s.pos, '%s');\n", ind, dest, p, p, g.floatOwner(f))
		g.pf("%s}\n", ind)
		if f.HasFloatRange {
			// a -0 lands as +0, uncounted (§3.3, A COMPRESSED FLOAT'S SPECIAL VALUES)
			g.pf("%sif (%s < %s) { %s = %s; %s.report.clamped++; }\n", ind, dest, fmtFloat(f.FMin), dest, fmtFloat(f.FMin), p)
			g.pf("%sif (%s > %s) { %s = %s; %s.report.clamped++; }\n", ind, dest, fmtFloat(f.FMax), dest, fmtFloat(f.FMax), p)
			g.pf("%sif (%s == 0) %s = 0.0;\n", ind, dest, dest)
		}
		g.pf("%s%s.pos = %s.limit;\n", ind, p, p)
		g.pf("%sreturn true;\n", ind)
	default: // an integer or bits arm, at the wire kind's width (§4)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		g.pf("%sfinal width = _kindWidth(kind);\n", ind)
		g.pf("%sif (%s.limit - %s.pos %s width) %s\n", ind, p, p, g.widthCmp(), bad)
		g.pf("%svar raw = 0;\n", ind)
		g.pf("%sfor (var b = 0; b < width; b++) { raw |= %s.readByte() << (8 * b); }\n", ind, p)
		if signed {
			g.pf("%svar value = _signExtend(raw, width);\n", ind)
		} else {
			g.pf("%svar value = raw;\n", ind)
		}
		g.emitIntClamp(f, "value", p, ind)
		g.pf("%s%s = value;\n", ind, dest)
		g.pf("%sreturn true;\n", ind)
	}
}

// widthCmp is the comparison a fixed-width scalar payload's length check uses.
// An ARM's payload is framed by its own L and must be exactly the kind's width
// (§3); a keyed array's element is read at the wire kind's width and the rest
// of its L skips (internal/tablewire keyed, scalarAt), so it is only too short
// when it is short.
func (g *wireGen) widthCmp() string {
	if g.lenientPayload {
		return "<"
	}
	return "!="
}

// tailCheck is the " || p.pos != p.limit" a single-value payload adds to its
// framing check, and nothing for a keyed element, whose slack skips.
func (g *wireGen) tailCheck(p string) string {
	if g.lenientPayload {
		return ""
	}
	return fmt.Sprintf(" || %s.pos != %s.limit", p, p)
}

// emitArmElems reads an array arm's `keep` elements into `out`: at the declared
// element kind, or WIDENED from a narrower one (§4). Framing damage stops the
// loop and keeps the prefix.
func (g *wireGen) emitArmElems(f *ir.Field, ek int, wc, p, ind string) {
	bad := fmt.Sprintf("{ %s.report.malformed = true; break; }", p)
	if wc != "" && !f.Type.Pointer {
		g.pf("%sif (elemKind != %d) {\n", ind, ek)
		g.emitWidenedElems(f, ek, "out", p, ind+"\t")
		g.pf("%s} else {\n", ind)
		g.pf("%s\tfor (var i = 0; i < keep; i++) {\n", ind)
		g.emitGetElemBad(f, ek, "out", p, ind+"\t\t", bad)
		g.pf("%s\t}\n", ind)
		g.pf("%s}\n", ind)
		return
	}
	g.pf("%sfor (var i = 0; i < keep; i++) {\n", ind)
	g.emitGetElemBad(f, ek, "out", p, ind+"\t", bad)
	g.pf("%s}\n", ind)
}

// emitIntClamp clamps an integer to its declared range and its bits mask (§4),
// counting each clamp on the reader's report.
func (g *wireGen) emitIntClamp(f *ir.Field, value, p, ind string) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	if f.HasIntRange {
		lo, hi := dartBoundLiteral(f.IntMin), dartBoundLiteral(f.IntMax)
		if signed {
			g.pf("%sif (%s < %s) { %s = %s; %s.report.clamped++; }\n", ind, value, lo, value, lo, p)
			g.pf("%sif (%s > %s) { %s = %s; %s.report.clamped++; }\n", ind, value, hi, value, hi, p)
		} else {
			g.pf("%sif (_u64Below(%s, %s)) { %s = %s; %s.report.clamped++; }\n", ind, value, lo, value, lo, p)
			g.pf("%sif (_u64Above(%s, %s)) { %s = %s; %s.report.clamped++; }\n", ind, value, hi, value, hi, p)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		lit := dartU64Literal(uint64(1)<<f.Type.Width - 1)
		g.pf("%sif (_u64Above(%s, %s)) { %s = %s; %s.report.clamped++; }\n", ind, value, lit, value, lit, p)
	}
}

// emitGetElemBad reads one array element at the DECLARED element kind ek and
// appends it to out (§3); bad is the statement framing damage runs. An ARM
// reader returns false (the union reads None, §3); a FIELD's array stops the
// loop and keeps the prefix it decoded (§4, internal/tablewire arrayBody).
func (g *wireGen) emitGetElemBad(f *ir.Field, ek int, out, p, ind, bad string) {
	switch ek {
	case wireKindPtr:
		ref := tableRef(f)
		g.pf("%sfinal idx = %s.readLeb();\n", ind, p)
		g.pf("%sif (idx == null) %s\n", ind, bad)
		g.pf("%s%s.add(%s.resolveTable(idx, %s) as %s?);\n", ind, out, p, typeConst(ref), ref.Name)
	case wireKindTable:
		ref := tableRef(f)
		g.pf("%sfinal elen = %s.readLeb();\n", ind, p)
		g.pf("%sif (elen == null || !%s.has(elen)) %s\n", ind, p, bad)
		g.pf("%svar e = %s();\n", ind, ref.Name)
		g.pf("%sfinal sub = TableWireReader.sub(%s, %s.pos, %s.pos + elen);\n", ind, p, p, p)
		g.pf("%s%sWireLoadBody(sub, e, true);\n", ind, dartName(ref.Name))
		g.pf("%sif (sub.pos != sub.limit) { %s.report.malformed = true; e = %s(); }\n", ind, p, ref.Name)
		g.pf("%s%s.pos += elen;\n", ind, p)
		g.pf("%s%s.add(e);\n", ind, out)
	case wireKindUnion:
		g.pf("%sfinal e = %s();\n", ind, f.Type.Name)
		g.pf("%sif (!%s(%s, e)) %s\n", ind, unionFn(unionRef(f), "LoadArm"), p, bad)
		g.pf("%s%s.add(e);\n", ind, out)
	case wireKindString:
		g.pf("%sfinal elen = %s.readLeb();\n", ind, p)
		g.pf("%sif (elen == null || !%s.has(elen) || !_lebValid(%s.data, %s.pos, elen)) %s\n", ind, p, p, p, bad)
		g.pf("%s%s.add(utf8.decode(%s.data.sublist(%s.pos, %s.pos + elen)));\n", ind, out, p, p, p)
		g.pf("%s%s.pos += elen;\n", ind, p)
	case wireKindEnum:
		e := enumRef(f)
		g.pf("%sfinal vref = %s.readLeb();\n", ind, p)
		g.pf("%sif (vref == null) %s\n", ind, bad)
		g.pf("%sfinal vid = vref == 0 ? 0 : %s.refToId[vref];\n", ind, p)
		g.pf("%sif (vid == null) %s\n", ind, bad)
		// an element naming no variant this reader knows reads None and
		// counts unknown, exactly as an enum field does (§4)
		// reference 0 is the enum's None; a nonzero reference whose id is 0 is
		// a named id that matches no variant, and counts unknown like any other
		g.pf("%sif (vref == 0) {\n", ind)
		g.pf("%s\t%s.add(%s);\n", ind, out, g.enumConst(e, 0))
		g.pf("%s} else {\n", ind)
		g.pf("%s\tswitch (vid) {\n", ind)
		for i := range e.Variants {
			g.pf("%s\t\tcase %s: %s.add(%s);\n", ind, dartU64Literal(ir.TableWireId(e.VariantWireName(i))), out, g.enumConst(e, i+1))
		}
		g.pf("%s\t\tdefault: %s.add(%s); %s.report.unknown++; if (%s.retain != null) %s.report.retainLost++;\n", ind, out, g.enumConst(e, 0), p, p, p)
		g.pf("%s\t}\n", ind)
		g.pf("%s}\n", ind)
	case wireKindBool:
		g.pf("%sif (!%s.has(1)) %s\n", ind, p, bad)
		g.pf("%s%s.add(%s.readBool());\n", ind, out, p)
	case wireKindF32:
		g.pf("%sif (!%s.has(4)) %s\n", ind, p, bad)
		g.pf("%s%s.add(%s.readF32(%s.pos, '%s')); %s.pos += 4;\n", ind, out, p, p, g.floatOwner(f), p)
	case wireKindF64:
		g.pf("%sif (!%s.has(8)) %s\n", ind, p, bad)
		g.pf("%s%s.add(%s.readF64(%s.pos, '%s')); %s.pos += 8;\n", ind, out, p, p, g.floatOwner(f), p)
	default:
		w := ir.TableKindWidth(ek)
		if ir.TableKindWide(ek) { // kinds 18-29 (§3, §15)
			g.pf("%sif (!%s.has(%d)) %s\n", ind, p, w, bad)
			g.pf("%s{\n", ind)
			g.pf("%s\tvar x = _getWide(%s, %d, %v);\n", ind, p, w, ir.TableKindSigned(ek))
			g.emitWideClamp(f, "x", p, ind+"\t")
			g.pf("%s\t%s.add(x);\n", ind, out)
			g.pf("%s}\n", ind)
			return
		}
		signed := ek >= wireKindI8 && ek <= wireKindI64
		g.pf("%sif (!%s.has(%d)) %s\n", ind, p, w, bad)
		g.pf("%svar raw = 0;\n", ind)
		g.pf("%sfor (var b = 0; b < %d; b++) { raw |= %s.readByte() << (8 * b); }\n", ind, w, p)
		g.emitElemInt(f, fmt.Sprintf("_signExtend(raw, %d)", w), signed, out, p, ind)
	}
}

// emitElemInt appends one decoded integer element, clamped to the field's
// declared range and bits mask and counted, as a scalar field's value is (§4).
func (g *wireGen) emitElemInt(f *ir.Field, signedExpr string, signed bool, out, p, ind string, unsignedExpr ...string) {
	expr := "raw"
	if len(unsignedExpr) > 0 {
		expr = unsignedExpr[0]
	}
	if signed {
		expr = signedExpr
	}
	if !f.HasIntRange && (f.Type.Kind != ir.TBits || f.Type.Width >= 64) {
		g.pf("%s%s.add(%s);\n", ind, out, expr)
		return
	}
	g.pf("%s{\n", ind)
	g.pf("%s\tvar x = %s;\n", ind, expr)
	g.emitIntClamp(f, "x", p, ind+"\t")
	g.pf("%s\t%s.add(x);\n", ind, out)
	g.pf("%s}\n", ind)
}

// emitUnionNumber emits _<u>Number: the set arm is an edge of its own kind
// (docs/SPEC-TABLES.md §2.6, §3.1, internal/tablewire visitArmEdges): a table
// arm is descended by value, a pointer arm is a pointer edge, a blob arm is
// that node, a pointer array's live slots are edges in index order, and a
// nested union asks the same one level in. An arm that is not set is no edge.
func (g *wireGen) emitUnionNumber(un *ir.Union) {
	g.pf("void %s(WireState s, %s v, Set<Object> open) {\n", unionFn(un, "Number"), un.Name)
	g.pf("\tswitch (v.armTag) {\n")
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		f := arm.F
		expr := "v." + armMember(arm)
		switch {
		case f.Type.Blob():
			g.pf("\t\tcase %d:\n", i+1)
			if f.Type.Kind == ir.TString {
				g.pf("\t\t\tif (%s != null) s.noteBlob(_kStringBlobId, %s!);\n", expr, expr)
			} else {
				g.pf("\t\t\tif (%s != null) s.noteBlob(_kBytesBlobId, %s!);\n", expr, expr)
			}
			g.pf("\t\t\tbreak;\n")
		case isPointerTable(f):
			ref := tableRef(f)
			g.pf("\t\tcase %d:\n", i+1)
			g.pf("\t\t\tfinal node = %s;\n", expr)
			g.pf("\t\t\tif (node != null) s.noteTable(%s, node, open, (o) { %sNumber(s, node, o); });\n", typeConst(ref), dartName(ref.Name))
			g.pf("\t\t\tbreak;\n")
		case isByValueTable(f):
			g.pf("\t\tcase %d:\n", i+1)
			g.pf("\t\t\t%sNumber(s, %s, open);\n", dartName(tableRef(f).Name), expr)
			g.pf("\t\t\tbreak;\n")
		case f.CountedOnWire() && f.Type.Pointer && tableRef(f) != nil:
			ref := tableRef(f)
			g.pf("\t\tcase %d:\n", i+1)
			g.pf("\t\t\tfor (final e in %s) {\n", expr)
			g.pf("\t\t\t\tif (e != null) s.noteTable(%s, e, open, (o) { %sNumber(s, e, o); });\n", typeConst(ref), dartName(ref.Name))
			g.pf("\t\t\t}\n")
			g.pf("\t\t\tbreak;\n")
		case isUnionField(f):
			g.pf("\t\tcase %d:\n", i+1)
			g.pf("\t\t\t%s(s, %s, open);\n", unionFn(unionRef(f), "Number"), expr)
			g.pf("\t\t\tbreak;\n")
		}
	}
	g.pf("\t}\n}\n\n")
}

// mapKeyCompare is a Dart expression ordering two map entries by key (§2.8):
// a string key by its UTF-8 bytes, a signed key by value, an unsigned key as
// a 64-bit pattern.
func mapKeyCompare(f *ir.Field) string {
	key := ir.MapKeyField(f)
	k := fieldMember(key)
	switch {
	case key.Type.Kind == ir.TString:
		return fmt.Sprintf("_bytesCompare(utf8.encode(a.%s), utf8.encode(b.%s))", k, k)
	case key.Type.Kind == ir.TInt && key.Type.Signed:
		return fmt.Sprintf("a.%s.compareTo(b.%s)", k, k)
	}
	return fmt.Sprintf("_u64Below(a.%s, b.%s) ? -1 : (_u64Above(a.%s, b.%s) ? 1 : 0)", k, k, k, k)
}

// mapSorted is a Dart expression for a map field's entries in ASCENDING KEY
// ORDER, the writer's rule the walk and the save share (§2.8,
// internal/tablewire MapEntryOrder): a stable sort of a copy, so the caller's
// list is not reordered.
func mapSorted(f *ir.Field, expr string) string {
	return fmt.Sprintf("_stableSorted(%s, (%s a, %s b) => %s)", expr, f.MapEntry.Name, f.MapEntry.Name, mapKeyCompare(f))
}

// emitMapRuntime emits the two helpers mapSorted uses: a byte-order compare
// and a stable insertion sort (§2.8).
func (g *wireGen) emitMapRuntime() {
	g.pf(`// _bytesCompare orders two keys by their UTF-8 bytes (docs/SPEC-TABLES.md §2.8).
int _bytesCompare(List<int> a, List<int> b) {
  final n = a.length < b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return a.length.compareTo(b.length);
}

// _stableSorted is a copy of a map's entries in ascending key order, ties in
// their stored order (docs/SPEC-TABLES.md §2.8).
List<T> _stableSorted<T>(List<T> l, int Function(T a, T b) cmp) {
  final out = List<T>.from(l);
  for (var i = 1; i < out.length; i++) {
    final x = out[i];
    var j = i - 1;
    while (j >= 0 && cmp(out[j], x) > 0) {
      out[j + 1] = out[j];
      j--;
    }
    out[j + 1] = x;
  }
  return out;
}

// _wideValid reports whether a run of UTF-16 code units is well-formed: every
// high surrogate followed by a low one, no low surrogate alone, no NUL
// (docs/SPEC-TABLES.md §3, kind 33).
bool _wideValid(List<int> u) {
  for (var i = 0; i < u.length; i++) {
    final c = u[i];
    if (c == 0) return false;
    if (c >= 0xD800 && c <= 0xDBFF) {
      if (i + 1 >= u.length || u[i + 1] < 0xDC00 || u[i + 1] > 0xDFFF) return false;
      i++;
    } else if (c >= 0xDC00 && c <= 0xDFFF) {
      return false;
    }
  }
  return true;
}

// _kindWidth is a fixed-width kind's payload size in bytes (docs/SPEC-TABLES.md §3).
int _kindWidth(int kind) {
  switch (kind) {
    case 1:
    case 2:
    case 6:
      return 1;
    case 3:
    case 7:
      return 2;
    case 4:
    case 8:
    case 10:
      return 4;
    case 5:
    case 9:
    case 11:
      return 8;
  }
  return 0;
}

`)
}

// emitSaveMap writes one map field as kind 14 over element kind 13 (§2.8,
// internal/tablewire encodeMap): the entries in ascending key order, each a
// length-framed entry body that ALWAYS rides (identity is the key), and an
// empty map elides under the by-value rule (§3). The field id is interned
// before the entry bodies.
func (g *wireGen) emitSaveMap(f *ir.Field, expr, ind string, id uint64, ordinal int) {
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal sorted = %s;\n", ind, mapSorted(f, expr))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, wireKindTable)
	g.pf("%s\ts.putLeb(sorted.length);\n", ind)
	g.pf("%s\tfor (var i = 0; i < sorted.length; i++) {\n", ind)
	g.pf("%s\t\tfinal b = s.sub();\n", ind)
	g.pf("%s\t\tb.path = [...w.path, %d, i];\n", ind, ordinal)
	g.pf("%s\t\t%sWireSaveBody(b, sorted[i]);\n", ind, dartName(f.MapEntry.Name))
	g.pf("%s\t\ts.putLeb(b.buf.length);\n", ind)
	g.pf("%s\t\ts.putBytes(b.buf);\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tw.putLeb(ref);\n", ind)
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
	g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
	g.pf("%s\tw.putBytes(s.buf);\n", ind)
	g.pf("%s}\n", ind)
}

// emitLoadMap reads one map field (§2.8, §4): kind 14 over element kind 13,
// each entry a length-framed entry body read tolerantly, decided entry by
// entry exactly as internal/tablewire (*wireReader).mapField decides it
// (docs/SPEC-TABLES.md §2.8, §4; the counters of §6.6). Before an entry body
// decodes, the KEY SCAN (TableWireReader.mapEntryScan, the twin of
// internal/tablewire (*wireReader).mapKey) walks its field headers:
//
//   - A body the scan cannot frame whole to its terminator makes the MAP
//     malformed: that entry does not land, nothing inside it is counted, and
//     the rest of the map skips by its L. No duplicate is judged on it.
//   - A key whose LAST occurrence rides a kind the declaration neither is nor
//     widens from is A MAP WITH HALF ITS KEYS: the map resets to empty, ONE
//     kind_mismatch counts, and the rest skips.
//   - A key that rides a kind the declaration WIDENS from counts ONE
//     widened for the whole map, at the first such key, before the entry is
//     judged: the entry body's own decode never counts it again.
//   - Otherwise the body decodes and the entry lands. An entry whose key
//     EQUALS the previous landed key is a DUPLICATE: the earlier slot is
//     replaced whole (last wins), the map's count excludes it, and the
//     report's duplicate counter rises. A key BELOW the previous landed key
//     is malformed; the ascending prefix stands, nothing the descending
//     body held is counted, and the rest skips by L.
func (g *wireGen) emitLoadMap(f *ir.Field, ind string, ordinal int) {
	name := fieldMember(f)
	en := f.MapEntry.Name
	// the key's declared wire kind and the kinds that widen into it, which
	// the key scan reads (ir.TableKindWidens; internal/tablewire mapKey), and
	// the key's declared string bound (ir.MapKeyField(f).Type.Size) for the
	// fit rule: a `string(N)` key past N drops its entry whole (§2.8, KEYS
	// NEVER CLAMP; internal/tabletext MapKeyFits). keySize is -1 for a key
	// that is not a bounded string, which can never fail to fit.
	keyField := ir.MapKeyField(f)
	keyKind := ir.TableWireScalarKind(keyField)
	keySize := -1
	if keyField.Type.Kind == ir.TString {
		keySize = int(keyField.Type.Size)
	}
	keyWidens := ""
	for k := 1; k <= 40; k++ {
		if ir.TableKindWidens(k, keyKind) {
			if keyWidens != "" {
				keyWidens += ", "
			}
			keyWidens += fmt.Sprint(k)
		}
	}
	g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
	g.pf("%s\t{\n", ind)
	g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
	g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
	g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
	g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
	// THE MAP'S HEADER IS READ AGAINST THE PARENT, as every array's is
	// (internal/tablewire mapField, subTo; docs/SPEC-TABLES.md §2.8, §3):
	// a count LEB128 that runs past the map's L still reads, the element
	// kind is judged, and the entries walk an empty body.
	g.pf("%s\t\tfinal elemKind = r.readByte();\n", ind)
	g.pf("%s\t\tfinal count = r.readLeb();\n", ind)
	g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
	g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);\n", ind)
	g.pf("%s\t\tif (elemKind != %d) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, wireKindTable)
	// Commit a replacement only after its array header is readable and
	// compatible (internal/tablewire/decode.go mapField): a skipped repeat
	// preserves the earlier occurrence (§3, §4).
	g.pf("%s\t\tv.%s = <%s>[];\n", ind, name, en)
	g.pf("%s\t\tif (count < 0 || count > 0x7FFFFFFF) { r.report.malformed = true; r.pos = end; break; }\n", ind)
	g.pf("%s\t\tvar lastEntry = %s();\n", ind, en)
	g.pf("%s\t\tvar lastLanded = false;\n", ind)
	g.pf("%s\t\tvar mapWidened = false;\n", ind)
	g.pf("%s\t\tfor (var i = 0; i < count; i++) {\n", ind)
	g.pf("%s\t\t\tfinal elen = arr.readLeb();\n", ind)
	g.pf("%s\t\t\tif (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }\n", ind)
	g.pf("%s\t\t\tvar e = %s();\n", ind, en)
	g.pf("%s\t\t\tfinal sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);\n", ind)
	g.pf("%s\t\t\tarr.pos = arr.pos + elen;\n", ind)
	g.pf("%s\t\t\tsub.path = [...r.path, %d, i];\n", ind, ordinal)
	g.pf("%s\t\t\tfinal scan = sub.mapEntryScan(%s, %d, const <int>[%s], %d);\n", ind, dartU64Literal(ir.MapKeyWireId), keyKind, keyWidens, keySize)
	// the MAP counts ONE widened, at the first key the declaration widens,
	// and before the entry is judged: a widened key ahead of the damage still
	// counts (§2.8, §4; internal/tablewire mapField `if entryWidened &&
	// !widened`)
	g.pf("%s\t\t\tif ((scan & 4) != 0 && !mapWidened) { mapWidened = true; r.report.widened++; }\n", ind)
	g.pf("%s\t\t\tif ((scan & 3) == 2) { r.report.kindMismatch++; v.%s = <%s>[]; break; }\n", ind, name, en)
	g.pf("%s\t\t\tif ((scan & 3) != 0) { r.report.malformed = true; break; }\n", ind)
	// KEYS NEVER CLAMP (§2.8): a key whose LAST occurrence rides a string the
	// declaration bounds under its length drops the entry WHOLE, one `clamped`
	// per entry, and nothing inside the body is decoded or counted, and the
	// entry never becomes the last landed key (internal/tablewire mapField
	// `if !tabletext.MapKeyFits(f, key) { r.report.Clamped++; continue }`).
	g.pf("%s\t\t\tif ((scan & 8) != 0) { r.report.clamped++; continue; }\n", ind)
	// The Go reference judges the key's ORDER before it decodes the body, so a
	// DESCENDING entry's body is never read and nothing inside it counts. The
	// Dart order compares decoded entries, so the counters are kept here and
	// put back when the order stops the map (§2.8, §6.6; internal/tablewire
	// mapField).
	g.pf("%s\t\t\tfinal pre = TableWireReport()\n", ind)
	g.pf("%s\t\t\t\t..unknown = r.report.unknown\n", ind)
	g.pf("%s\t\t\t\t..kindMismatch = r.report.kindMismatch\n", ind)
	g.pf("%s\t\t\t\t..widened = r.report.widened\n", ind)
	g.pf("%s\t\t\t\t..clamped = r.report.clamped\n", ind)
	g.pf("%s\t\t\t\t..duplicate = r.report.duplicate\n", ind)
	g.pf("%s\t\t\t\t..malformed = r.report.malformed\n", ind)
	g.pf("%s\t\t\t\t..refused = r.report.refused\n", ind)
	g.pf("%s\t\t\t\t..retained = r.report.retained\n", ind)
	g.pf("%s\t\t\t\t..retainLost = r.report.retainLost;\n", ind)
	g.pf("%s\t\t\t%sWireLoadBody(sub, e, true);\n", ind, dartName(en))
	g.pf("%s\t\t\tif (lastLanded) {\n", ind)
	g.pf("%s\t\t\t\tfinal a = lastEntry;\n", ind)
	g.pf("%s\t\t\t\tfinal b = e;\n", ind)
	g.pf("%s\t\t\t\tfinal order = %s;\n", ind, mapKeyCompare(f))
	g.pf("%s\t\t\t\tif (order > 0) {\n", ind)
	g.pf("%s\t\t\t\t\t// DESCENDING: the body is not read (§2.8, internal/tablewire mapField)\n", ind)
	g.pf("%s\t\t\t\t\tr.report\n", ind)
	g.pf("%s\t\t\t\t\t\t..unknown = pre.unknown\n", ind)
	g.pf("%s\t\t\t\t\t\t..kindMismatch = pre.kindMismatch\n", ind)
	g.pf("%s\t\t\t\t\t\t..widened = pre.widened\n", ind)
	g.pf("%s\t\t\t\t\t\t..clamped = pre.clamped\n", ind)
	g.pf("%s\t\t\t\t\t\t..duplicate = pre.duplicate\n", ind)
	g.pf("%s\t\t\t\t\t\t..refused = pre.refused\n", ind)
	g.pf("%s\t\t\t\t\t\t..retained = pre.retained\n", ind)
	g.pf("%s\t\t\t\t\t\t..retainLost = pre.retainLost\n", ind)
	g.pf("%s\t\t\t\t\t\t..malformed = true;\n", ind)
	g.pf("%s\t\t\t\t\tbreak;\n", ind)
	g.pf("%s\t\t\t\t}\n", ind)
	g.pf("%s\t\t\t\t// EQUAL: a DUPLICATE, last wins whole (§2.8, §6.6)\n", ind)
	g.pf("%s\t\t\t\tif (order == 0) { r.report.duplicate++; v.%s[v.%s.length - 1] = e; continue; }\n", ind, name, name)
	g.pf("%s\t\t\t}\n", ind)
	g.pf("%s\t\t\tlastEntry = e;\n", ind)
	g.pf("%s\t\t\tlastLanded = true;\n", ind)
	g.pf("%s\t\t\tv.%s.add(e);\n", ind, name)
	g.pf("%s\t\t}\n", ind)
	g.pf("%s\t\tr.pos = end;\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tbreak;\n", ind)
}

// emitSaveUnionField writes a union field (§2.6, §3): None elides — the
// absence of the field is the None — and a set arm rides as the field header
// at kind 15 followed by the arm header.
func (g *wireGen) emitSaveUnionField(f *ir.Field, expr, ind string, id uint64) {
	g.pf("%sif (%s.armTag != 0) {\n", ind, expr)
	g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindUnion)
	g.pf("%s\t%s(w, %s);\n", ind, unionFn(unionRef(f), "SaveArm"), expr)
	g.pf("%s}\n", ind)
}

// emitSaveUnionList writes a counted or unbounded array of unions as kind 14
// over element kind 15 (§3): the live count, then each element's arm header,
// a None element the single zero byte. An empty array elides (§2.9).
func (g *wireGen) emitSaveUnionList(f *ir.Field, expr, ind string, id uint64) {
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, wireKindUnion)
	g.pf("%s\ts.putLeb(%s.length);\n", ind, expr)
	g.pf("%s\tfor (final e in %s) {\n", ind, expr)
	g.emitPutElem(f, wireKindUnion, "e", "s", ind+"\t\t")
	g.pf("%s\t}\n", ind)
	g.pf("%s\tw.putLeb(ref);\n", ind)
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
	g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
	g.pf("%s\tw.putBytes(s.buf);\n", ind)
	g.pf("%s}\n", ind)
}
