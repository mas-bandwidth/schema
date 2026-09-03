// The TABLE-wire codecs (docs/SPEC-TABLES.md §3, §4), mirrored from the C++
// reference: reset, measure, save and load, one set per closure member.
//
// Every framing decision, every elision, every clamp and every report event is
// the reference's. What Dart spells differently is written at the site, and it
// is always one of the four the package doc names.
package darttable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// member is a field's Dart storage member name — the packet emitter's
// lowerCamelCase mapping, so one field is spelled one way across a unit.
func member(f *ir.Field) string { return dartName(f.Name) }

func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

func unionRef(f *ir.Field) *ir.Union {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

func isClassRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// vocab is the Dart expression naming one enum's table-wire vocabulary.
func vocab(name string) string { return "TableEnumVocab." + dartName(name) }

// slot renders the element of an ENUM-KEYED field that the KEY `key` owns, 1
// to E.Max (docs/SPEC-TABLES.md §2.4).
//
// A TABLE's keyed field is a TableKeyed<T>, indexed by key and by nothing else
// — its storage is private, so the one model a consumer meets is the key's. A
// closure `type`'s keyed field is its PACKET storage — a plain typed list with
// the key k at position k - 1 — because a type's class is emitted by the
// packet backend and nothing on this wire changes that. Same slots, two
// spellings, and this is the only place the difference is spelled.
func (g *tableGen) slot(f *ir.Field, key string) string {
	name := "this." + member(f)
	if g.keyedByKey(f) {
		return name + "[" + key + "]"
	}
	return name + "[" + key + " - 1]"
}

// keyedByKey says a field's storage is a TableKeyed, indexed by key.
func (g *tableGen) keyedByKey(f *ir.Field) bool {
	return f.KeyEnum != "" && g.owner != nil && g.owner.IsTable
}

// tableStorageRange is the inclusive range an integer storage of the given
// width can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// actually clamp at: a bound sitting ON the decode width's own limit is a
// comparison no decoded value can satisfy, and the emitter drops it. The
// reference emitter's test, applied to the same numbers.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := tableStorageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// fieldDefaultExpr renders the Dart expression a field's default compares
// against on the write side, and the value its reset restores. It is the same
// value the storage initializer holds, so measure, save and the reader's
// prefill agree.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return dartIntLit(f.DefInt)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			g.needDecl(f.Type.Name)
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "." + dartName(f.DefVariant)
			}
			return f.Type.Name + ".none"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

// elisionCompare renders the "differs from its default" test. A float32
// field's storage is a Dart double, so the test NARROWS first: that is what
// gives the decision C's own float semantics and what makes a Dart wire byte
// equal the C++ one for a value the two languages store differently.
func (g *tableGen) elisionCompare(f *ir.Field, expr string) string {
	if f.Type.Kind == ir.TFloat32 {
		want := "0.0"
		if f.HasDefault {
			want = narrowedFloat32(f.DefFloat)
		}
		return fmt.Sprintf("tableNarrowFloat(%s) != %s", expr, want)
	}
	return fmt.Sprintf("%s != %s", expr, g.fieldDefaultExpr(f))
}

// ---- guards ----

func (g *tableGen) tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, true)
}

func tableGuardStrings(st *ir.Struct) map[string]string {
	return guardWalk(st, false)
}

func guardWalk(st *ir.Struct, dart bool) map[string]string {
	name := func(cond string) string {
		if dart {
			return "this." + dartName(cond)
		}
		return cond
	}
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := name(item.Cond), "!"+name(item.Cond)
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- reset: the reader's prefill ----

// emitTableReset restores a value's declared defaults IN PLACE — the Dart twin
// of the C++ reader's placement-new prefill, in place on purpose: reusing the
// caller's buffers is what keeps the read path free of allocation.
//
// The name is NAME-FIRST (<name>Reset), which §11's suffix set already claims;
// Dart has no overloading, so the C# backend's verb-first TableReset has no
// twin here and this backend defines no such spelling.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("/// Restore %s's declared defaults IN PLACE, reusing every buffer this\n", st.Name)
	g.pf("/// value already owns. The reader calls it before overlaying.\n")
	g.sig("void", "reset")
	if len(st.Fields) == 0 {
		g.pf("  // empty type: nothing to restore\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
		if f.Type.Optional {
			g.pf("  this.%sPresent = false;\n", member(f))
		}
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("  this.%s.fillRange(0, this.%s.length, 0);\n", name, name)
		g.pf("  this.%sLength = 0;\n", name)
	case f.KeyEnum != "" && isClassRef(f.Type):
		// by KEY, 1 to E.Max: the loop a consumer would write
		g.pf("  for (var key = 1; key <= this.%s.length; key++) {\n", name)
		if un := unionRef(f); un != nil {
			g.needUnionTag(un)
			g.pf("    %s.type = %sType.none;\n", g.slot(f, "key"), un.Name)
		} else {
			g.pf("    %s.reset();\n", g.slot(f, "key"))
			g.needMember(f.Type.Name)
		}
		g.pf("  }\n")
	case f.KeyEnum != "" && g.keyedByKey(f):
		g.pf("  this.%s.fill(%s);\n", name, g.zeroElement(f))
	case f.Array != ir.ArrayNone && isClassRef(f.Type):
		g.pf("  for (var i = 0; i < this.%s.length; i++) {\n", name)
		if un := unionRef(f); un != nil {
			g.needUnionTag(un)
			g.pf("    this.%s[i].type = %sType.none;\n", name, un.Name)
		} else {
			g.pf("    this.%s[i].reset();\n", name)
			g.needMember(f.Type.Name)
		}
		g.pf("  }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("  this.%sCount = 0;\n", name)
		}
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		g.pf("  this.%s.fillRange(0, this.%s.length, %s);\n", name, name, g.zeroElement(f))
		if f.Array == ir.ArrayCounted {
			g.pf("  this.%sCount = 0;\n", name)
		}
	default:
		if un := unionRef(f); un != nil {
			// the tag is the whole reset: an arm zero-establishes when the
			// reader selects it, exactly as the packet reader does
			g.needUnionTag(un)
			g.pf("  this.%s.type = %sType.none;\n", name, un.Name)
			return
		}
		if isClassRef(f.Type) {
			g.pf("  this.%s.reset();\n", name)
			g.needMember(f.Type.Name)
			return
		}
		g.pf("  this.%s = %s;\n", name, g.fieldDefaultExpr(f))
	}
}

// zeroElement is the value an ARRAY slot resets to. An array declares no
// per-element default, so it is the type's zero — spelled for the LIST's own
// element type, which is what fillRange takes: a positional array of bools is
// a Uint8List and takes 0, while a keyed array's slots are a List<bool> and
// take false.
func (g *tableGen) zeroElement(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TBool:
		if f.KeyEnum != "" && g.owner != nil && g.owner.IsTable {
			return "false"
		}
		return "0"
	}
	return "0"
}

// ---- measure ----

// emitTableMeasure emits <name>Measure: the EXACT encoded size of a value,
// writing nothing. Mirrors <name>SaveBody's elision decisions branch for
// branch: for any value, Save writes exactly this many bytes into a buffer of
// exactly this size. A value violating its storage invariants measures as -1,
// exactly as the write side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	g.pf("/// The EXACT encoded size of this value, writing nothing — so a caller can\n")
	g.pf("/// size a buffer, or measure subtables in parallel and scatter-write them.\n")
	g.pf("/// -1 when a storage invariant is broken, which is what save refuses on.\n")
	g.sig("int", "measure")
	g.pf("  var bytes = 2; // terminator\n")
	guards := g.tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("  if (%s) {\n", cond)
			g.indent += "  "
			g.emitTableMeasureField(f)
			g.indent = g.indent[:len(g.indent)-2]
			g.pf("  }\n")
			continue
		}
		g.emitTableMeasureField(f)
	}
	g.pf("  return bytes;\n}\n\n")
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	name := member(f)
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does
		g.pf("  if (this.%sPresent) {\n", name)
		g.pf("    // ?%s: presence decides, not content\n", tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.callMeasure(f, "    ", "body", "this."+name)
			g.pf("    if (body < 0) {\n      return -1;\n    }\n")
			g.pf("    bytes += 3 + 4 + body; // %s\n", f.Name)
		case enumRef(f) != nil:
			g.pf("    if (%s.idOf(this.%s) < 0) {\n", vocab(f.Type.Name), name)
			g.pf("      return -1; // no variant names this value\n    }\n")
			g.pf("    bytes += 3 + 2; // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("    bytes += 3 + %d; // %s\n", width, f.Name)
		}
		g.pf("  }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved
		g.pf("  {\n")
		g.pf("    var pairs = 0;\n    var keyedBytes = 0;\n")
		g.pf("    // [%s]: every stored slot is a named variant's\n", f.KeyEnum)
		g.pf("    for (var key = 1; key <= %d; key++) {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "      ", "return -1;")
		if kind == tkTable {
			g.pf("      pairs++;\n      keyedBytes += 2 + 4 + elemBytes; // key, length, body\n")
		} else {
			g.pf("      pairs++;\n      keyedBytes += 2 + 4 + %d; // key, length, element\n", width)
		}
		g.pf("    }\n")
		g.pf("    if (pairs > 0) {\n      bytes += 3 + 4 + 5 + keyedBytes; // %s\n    }\n", f.Name)
		g.pf("  }\n")
	case f.Type.Kind == ir.TString:
		g.pf("  if (this.%sLength < 0 || this.%sLength > %d) {\n", name, name, f.Type.Size)
		g.pf("    return -1; // storage invariant\n  }\n")
		g.pf("  if (this.%sLength > 0) {\n", name)
		g.pf("    bytes += 3 + 4 + this.%sLength; // %s\n  }\n", name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("  if (this.%sLength < 0 || this.%sLength > %d) {\n", name, name, f.Type.Size)
		g.pf("    return -1; // storage invariant\n  }\n")
		g.pf("  if (this.%sLength > 0) {\n", name)
		g.pf("    bytes += 3 + 4 + 5 + this.%sLength; // %s\n  }\n", name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.pf("  if (this.%sCount < 0 || this.%sCount > %d) {\n", name, name, f.ArrayBound)
		g.pf("    return -1; // storage invariant\n  }\n")
		g.pf("  if (this.%sCount > 0) {\n", name)
		g.pf("    bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("    for (var i = 0; i < this.%sCount; i++) {\n", name)
		g.callMeasure(f, "      ", "elem", fmt.Sprintf("this.%s[i]", name))
		g.pf("      if (elem < 0) {\n        return -1;\n      }\n")
		g.pf("      bytes += 4 + elem;\n    }\n  }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("  if (this.%sCount < 0 || this.%sCount > %d) {\n", name, name, f.ArrayBound)
		g.pf("    return -1; // storage invariant\n  }\n")
		g.pf("  if (this.%sCount > 0) {\n", name)
		g.emitEnumElementCheck(f, fmt.Sprintf("this.%s[i]", name), fmt.Sprintf("this.%sCount", name), "    ", "return -1;")
		g.pf("    bytes += 3 + 4 + 5 + this.%sCount * %d; // %s\n", name, width, f.Name)
		g.pf("  }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("  {\n")
		g.pf("    bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("    for (var i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.callMeasure(f, "      ", "elem", fmt.Sprintf("this.%s[i]", name))
		g.pf("      if (elem < 0) {\n        return -1;\n      }\n")
		g.pf("      bytes += 4 + elem;\n    }\n  }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("  {\n")
		g.pf("    var allDefault = true;\n")
		g.pf("    for (var i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.pf("      if (%s) {\n", g.elisionCompare(f, fmt.Sprintf("this.%s[i]", name)))
		g.pf("        allDefault = false;\n        break;\n      }\n    }\n")
		g.pf("    if (!allDefault) {\n")
		g.emitEnumElementCheck(f, fmt.Sprintf("this.%s[i]", name), fmt.Sprintf("%d", f.ArrayBound), "      ", "return -1;")
		g.pf("      bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("    }\n  }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needUnionTag(un)
		g.pf("  switch (this.%s.type) {\n", name)
		g.pf("    // %s — None elides; TLV absence is the None\n", f.Name)
		g.pf("    case %sType.none:\n      break;\n", un.Name)
		for _, v := range un.Variants {
			g.pf("    case %sType.%s:\n      {\n", un.Name, dartName(v.Name))
			g.callMeasureNamed(v.Type, "        ", "arm", fmt.Sprintf("this.%s.%s", name, dartName(v.Name)))
			g.pf("        if (arm < 0) {\n          return -1;\n        }\n")
			g.pf("        // the u16 ARM ID, then the arm length-prefixed\n")
			g.pf("        bytes += 3 + 2 + 4 + arm;\n      }\n      break;\n")
		}
		g.pf("    default:\n      return -1; // invalid tag — the write side refuses it too\n")
		g.pf("  }\n")
	case kind == tkTable:
		g.pf("  {\n")
		g.callMeasure(f, "    ", "body", "this."+name)
		g.pf("    if (body < 0) {\n      return -1;\n    }\n")
		g.pf("    if (body > 2) {\n")
		g.pf("      bytes += 3 + 4 + body; // %s: all-default nested elides\n    }\n  }\n", f.Name)
	case enumRef(f) != nil:
		g.pf("  if (%s) {\n", g.elisionCompare(f, "this."+name))
		g.pf("    if (%s.idOf(this.%s) < 0) {\n", vocab(f.Type.Name), name)
		g.pf("      return -1; // no variant names this value\n    }\n")
		g.pf("    bytes += 3 + 2; // %s: the variant's name hash\n  }\n", f.Name)
	default:
		g.pf("  if (%s) {\n", g.elisionCompare(f, "this."+name))
		g.pf("    bytes += 3 + %d; // %s\n  }\n", width, f.Name)
	}
}

// callMeasure emits `final <local> = <type>Measure(<expr>);`, recording the
// cross-file import the call needs.
func (g *tableGen) callMeasure(f *ir.Field, ind, local, expr string) {
	g.callMeasureNamed(f.Type.Name, ind, local, expr)
}

func (g *tableGen) callMeasureNamed(typeName, ind, local, expr string) {
	g.needMember(typeName)
	g.pf("%sfinal %s = %s.measure();\n", ind, local, expr)
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2): it elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names, and leaves `keyId` holding
// the slot's wire id. For a table element `elemBytes` holds the measured body,
// so measure and save decide elision on the same number.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	expr := g.slot(f, "key")
	switch {
	case kind == tkTable:
		g.needMember(f.Type.Name)
		g.pf("%sfinal elemBytes = %s.measure();\n", ind, expr)
		g.pf("%sif (elemBytes < 0) {\n%s  %s\n%s}\n", ind, ind, onBad, ind)
		g.pf("%sif (elemBytes <= 2) {\n%s  continue; // an all-default slot elides\n%s}\n", ind, ind, ind)
	case enumRef(f) != nil:
		g.pf("%sif (!(%s)) {\n%s  continue; // a default slot elides\n%s}\n", ind, g.elisionCompare(f, expr), ind, ind)
		g.pf("%sif (%s.idOf(%s) < 0) {\n%s  %s // no variant names this value\n%s}\n",
			ind, vocab(f.Type.Name), expr, ind, onBad, ind)
	default:
		g.pf("%sif (!(%s)) {\n%s  continue; // a default slot elides\n%s}\n", ind, g.elisionCompare(f, expr), ind, ind)
	}
	g.assign(ind, "final keyId", vocab(f.KeyEnum)+".idOf", "key")
	g.pf("%sif (keyId < 0) {\n%s  %s\n%s}\n", ind, ind, onBad, ind)
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None.
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	if enumRef(f) == nil {
		return
	}
	g.pf("%s// %s: every element must be nameable\n", ind, f.Name)
	g.pf("%sfor (var i = 0; i < %s; i++) {\n", ind, count)
	g.pf("%s  if (%s.idOf(%s) < 0) {\n%s    %s\n%s  }\n%s}\n",
		ind, vocab(f.Type.Name), expr, ind, onBad, ind, ind)
}

// ---- save ----

func (g *tableGen) emitTableSave(st *ir.Struct) {
	g.pf("/// Write this value's body through a writer the CALLER owns — the entry a\n")
	g.pf("/// hot loop uses, which allocates nothing at all.\n")
	g.sig("bool", "saveBody", "TableWriter w")
	guards := g.tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("  if (%s) {\n", cond)
			g.indent += "  "
			g.emitTableWriteField(f)
			g.indent = g.indent[:len(g.indent)-2]
			g.pf("  }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("  w.put16(0); // terminator\n")
	g.pf("  return !w.overflow;\n}\n\n")

	g.needData = true
	g.pf("/// Write this value into the caller's buffer and answer the bytes written —\n")
	g.pf("/// exactly [measure]'s number — or -1 when the buffer is too small.\n")
	g.pf("///\n")
	g.pf("/// It allocates one [TableWriter] and one view over `buffer`. A caller in a\n")
	g.pf("/// loop owns a writer and calls [saveBody], which allocates nothing.\n")
	g.sig("int", "save", "Uint8List buffer")
	g.pf("  final w = TableWriter(buffer);\n")
	g.pf("  if (!saveBody(w)) {\n    return -1;\n  }\n")
	g.pf("  return w.offset; // == measure()\n}\n\n")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	name := member(f)
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (docs/SPEC-TABLES.md §2.3, §3.1)
		g.pf("  if (this.%sPresent) {\n    // ?%s\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.callMeasure(f, "    ", "body", "this."+name)
			g.pf("    if (body < 0) {\n")
			g.pf("      return false; // storage invariant, refused as measure refuses it\n    }\n")
			g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, tkTable, f.Name)
			g.pf("    w.put32(body);\n")
			g.callSaveBody(f.Type.Name, "    ", "this."+name)
		case enumRef(f) != nil:
			g.assign("    ", "final variantId", vocab(f.Type.Name)+".idOf", "this."+name)
			g.pf("    if (variantId < 0) {\n      return false;\n    }\n")
			g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, kind, f.Name)
			g.pf("    w.put16(variantId);\n")
		default:
			g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "this."+name, "    ")
		}
		g.pf("  }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs.
		// Two passes so the count is known before the header rides, and so
		// measure and save agree byte for byte.
		g.pf("  {\n")
		g.pf("    var pairs = 0;\n")
		g.pf("    // [%s]: every stored slot is a named variant's\n", f.KeyEnum)
		g.pf("    for (var key = 1; key <= %d; key++) {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "      ", "return false;")
		g.pf("      pairs++;\n")
		g.pf("    }\n")
		g.pf("    if (pairs > 0) {\n")
		g.pf("      // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("      // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("      // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("      w.put16(0x%04x);\n      w.put8(%d); // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("      final lenAt = w.offset;\n      w.put32(0);\n")
		g.pf("      w.put8(%d);\n      w.put32(pairs);\n", kind)
		g.pf("      // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("      // writer's choice, and a reader must not rely on it: every\n")
		g.pf("      // slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("      for (var key = 1; key <= %d; key++) {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "        ", "return false;")
		g.pf("        w.put16(keyId); // the slot's VARIANT id, not its position\n")
		g.pf("        final elemLenAt = w.offset;\n        w.put32(0);\n")
		if kind == tkTable {
			g.callSaveBody(f.Type.Name, "        ", g.slot(f, "key"))
		} else {
			g.emitTableWriteElement(f, kind, g.slot(f, "key"), "        ")
		}
		g.pf("        w.patch32(elemLenAt, w.offset - elemLenAt - 4);\n")
		g.pf("      }\n")
		g.pf("      w.patch32(lenAt, w.offset - lenAt - 4);\n")
		g.pf("    }\n  }\n")
	case f.Type.Kind == ir.TString:
		g.pf("  if (this.%sLength < 0 || this.%sLength > %d) {\n", name, name, f.Type.Size)
		g.pf("    return false; // storage invariant\n  }\n")
		g.pf("  if (this.%sLength > 0) {\n", name)
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, tkString, f.Name)
		g.pf("    w.put32(this.%sLength);\n", name)
		g.pf("    w.raw(this.%s, 0, this.%sLength);\n  }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("  if (this.%sLength < 0 || this.%sLength > %d) {\n", name, name, f.Type.Size)
		g.pf("    return false; // storage invariant\n  }\n")
		g.pf("  if (this.%sLength > 0) {\n", name)
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("    w.put32(5 + this.%sLength);\n", name)
		g.pf("    w.put8(%d);\n    w.put32(this.%sLength);\n", tkU8, name)
		g.pf("    w.raw(this.%s, 0, this.%sLength);\n  }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("  if (this.%sCount < 0 || this.%sCount > %d) {\n", name, name, f.ArrayBound)
		g.pf("    return false; // storage invariant\n  }\n")
		g.pf("  if (this.%sCount > 0) {\n", name)
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("    final lenAt = w.offset;\n    w.put32(0);\n")
		g.pf("    w.put8(%d);\n    w.put32(this.%sCount);\n", kind, name)
		g.pf("    for (var i = 0; i < this.%sCount; i++) {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("this.%s[i]", name), "      ")
		g.pf("    }\n")
		g.pf("    w.patch32(lenAt, w.offset - lenAt - 4);\n  }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there
		g.pf("  {\n")
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("    final lenAt = w.offset;\n    w.put32(0);\n")
		g.pf("    w.put8(%d);\n    w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("    for (var i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("this.%s[i]", name), "      ")
		g.pf("    }\n")
		g.pf("    w.patch32(lenAt, w.offset - lenAt - 4);\n  }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		g.pf("  {\n")
		g.pf("    var allDefault = true;\n")
		g.pf("    for (var i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.pf("      if (%s) {\n", g.elisionCompare(f, fmt.Sprintf("this.%s[i]", name)))
		g.pf("        allDefault = false;\n        break;\n      }\n    }\n")
		g.pf("    if (!allDefault) {\n")
		g.pf("      w.put16(0x%04x);\n      w.put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("      final lenAt = w.offset;\n      w.put32(0);\n")
		g.pf("      w.put8(%d);\n      w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("      for (var i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("this.%s[i]", name), "        ")
		g.pf("      }\n")
		g.pf("      w.patch32(lenAt, w.offset - lenAt - 4);\n    }\n  }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needUnionTag(un)
		g.pf("  if (this.%s.type != %sType.none) {\n", name, un.Name)
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, tkUnion, f.Name)
		g.pf("    // the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("    // arms may be added anywhere, removed and reordered\n")
		g.pf("    switch (this.%s.type) {\n", name)
		for _, v := range un.Variants {
			g.pf("      case %sType.%s:\n        w.put16(0x%04x);\n        break;\n",
				un.Name, dartName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("      default:\n        return false; // write validates the tag before it rides\n")
		g.pf("    }\n")
		g.pf("    final lenAt = w.offset;\n    w.put32(0);\n")
		g.pf("    switch (this.%s.type) {\n", name)
		for _, v := range un.Variants {
			g.needMember(v.Type)
			g.pf("      case %sType.%s:\n        if (!this.%s.%s.saveBody(w)) {\n          return false;\n        }\n        break;\n",
				un.Name, dartName(v.Name), name, dartName(v.Name))
		}
		g.pf("      default:\n        return false; // write validates the tag before it rides\n")
		g.pf("    }\n")
		g.pf("    w.patch32(lenAt, w.offset - lenAt - 4);\n  }\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first keeps
		// an all-default nested field from touching the buffer at all
		g.pf("  {\n")
		g.callMeasure(f, "    ", "body", "this."+name)
		g.pf("    if (body < 0) {\n")
		g.pf("      return false; // storage invariant, refused as measure refuses it\n    }\n")
		g.pf("    if (body > 2) {\n      // all-default nested elides\n")
		g.pf("      w.put16(0x%04x);\n      w.put8(%d); // %s\n", id, tkTable, f.Name)
		g.pf("      w.put32(body);\n")
		g.callSaveBody(f.Type.Name, "      ", "this."+name)
		g.pf("    }\n  }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("  if (%s) {\n", g.elisionCompare(f, "this."+name))
		g.assign("    ", "final variantId", vocab(f.Type.Name)+".idOf", "this."+name)
		g.pf("    if (variantId < 0) {\n      return false;\n    }\n")
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, kind, f.Name)
		g.pf("    w.put16(variantId);\n  }\n")
	default:
		g.pf("  if (%s) {\n", g.elisionCompare(f, "this."+name))
		g.pf("    w.put16(0x%04x);\n    w.put8(%d); // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "this."+name, "    ")
		g.pf("  }\n")
	}
}

func (g *tableGen) callSaveBody(typeName, ind, expr string) {
	g.needMember(typeName)
	g.pf("%sif (!%s.saveBody(w)) {\n%s  return false;\n%s}\n", ind, expr, ind, ind)
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if enumRef(f) != nil {
		g.pf("%s{\n", ind)
		g.assign(ind+"  ", "final writeElementId", vocab(f.Type.Name)+".idOf", expr)
		g.pf("%s  if (writeElementId < 0) {\n%s    return false;\n%s  }\n", ind, ind, ind)
		g.pf("%s  w.put16(writeElementId);\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sw.put8(%s ? 1 : 0);\n", ind, expr)
	case tkF32:
		g.pf("%sw.put32(tableFloatToBits(%s));\n", ind, expr)
	case tkF64:
		g.pf("%sw.put64(tableDoubleToBits(%s));\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s  final elemLenAt = w.offset;\n%s  w.put32(0);\n", ind, ind, ind)
		g.callSaveBody(f.Type.Name, ind+"  ", expr)
		g.pf("%s  w.patch32(elemLenAt, w.offset - elemLenAt - 4);\n%s}\n", ind, ind)
	default:
		g.pf("%sw.%s(%s);\n", ind, tablePut(tableKindWidth(kind)), expr)
	}
}

// ---- load ----

func (g *tableGen) emitTableLoad(st *ir.Struct) {
	g.pf("/// Overlay this value from a reader the CALLER owns — the entry a hot loop\n")
	g.pf("/// uses, which allocates nothing at all. False is framing damage: the value\n")
	g.pf("/// holds what was placed before the stop.\n")
	g.sig("bool", "loadBody", "TableReader r")
	g.pf("  // restore declared defaults in place, then overlay\n")
	g.pf("  reset();\n")
	g.pf("  for (;;) {\n")
	g.pf("    if (!r.has(2)) {\n      r.report.malformed = true;\n      return false;\n    }\n")
	g.pf("    final fieldId = r.get16();\n")
	g.pf("    if (fieldId == 0) {\n      return true;\n    }\n")
	g.pf("    if (!r.has(1)) {\n      r.report.malformed = true;\n      return false;\n    }\n")
	g.pf("    final kind = r.get8();\n")
	if len(st.Fields) > 0 {
		g.pf("    switch (fieldId) {\n")
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				wireKind = tkArray
			}
			if f.KeyEnum != "" {
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("      case 0x%04x: // %s\n        {\n", id, f.Name)
			g.pf("          if (kind != %d) {\n", wireKind)
			g.pf("            r.report.kindMismatch++;\n")
			g.pf("            if (!r.skip(kind)) {\n              r.report.malformed = true;\n              return false;\n            }\n")
			g.pf("            break;\n          }\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				g.pf("          this.%sPresent = true;\n", member(f))
			}
			g.pf("        }\n        break;\n")
		}
		g.pf("      default:\n        {\n")
		g.pf("          r.report.unknown++;\n")
		g.pf("          if (!r.skip(kind)) {\n            r.report.malformed = true;\n            return false;\n          }\n")
		g.pf("        }\n        break;\n")
		g.pf("    }\n  }\n}\n\n")
	} else {
		g.pf("    r.report.unknown++;\n")
		g.pf("    if (!r.skip(kind)) {\n      r.report.malformed = true;\n      return false;\n    }\n")
		g.pf("  }\n}\n\n")
	}

	g.needData = true
	g.pf("/// Overlay this value in place from the caller's bytes, reporting every\n")
	g.pf("/// tolerance event into `report`.\n")
	g.pf("///\n")
	g.pf("/// It allocates one [TableReader] and one view over `bytes`. A caller in a\n")
	g.pf("/// loop owns a reader and a report and calls [loadBody], which allocates\n")
	g.pf("/// nothing.\n")
	g.sig("bool", "load", "Uint8List bytes", "TableReport report")
	g.pf("  return loadBody(TableReader(bytes, report));\n}\n\n")
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	name := member(f)
	ind := "          "
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name
		// however the enum moved; an id this reader cannot name is skipped by
		// its length and counted unknown (docs/SPEC-TABLES.md §3.2)
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyEnd = r.offset + bodyLen;\n", ind)
		g.pf("%sfinal outerLimit = r.limit;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s  final elemKind = r.get8();\n", ind)
		g.pf("%s  final count = r.get32();\n", ind)
		g.pf("%s  if (elemKind != %d) {\n", ind, kind)
		g.pf("%s    r.report.kindMismatch++;\n%s    r.offset = bodyEnd;\n%s    break;\n%s  }\n", ind, ind, ind, ind)
		g.pf("%s  // the body BOUNDS its pairs: narrowing the limit is what a C++\n", ind)
		g.pf("%s  // sub-reader is, without the allocation a Dart sub-view costs\n", ind)
		g.pf("%s  r.limit = bodyEnd;\n", ind)
		g.pf("%s  for (var i = 0; i < count; i++) {\n", ind)
		g.pf("%s    if (!r.has(2)) {\n%s      r.report.malformed = true;\n%s      break;\n%s    }\n", ind, ind, ind, ind)
		g.pf("%s    final key = r.get16();\n", ind)
		g.pf("%s    if (!r.has(4)) {\n%s      r.report.malformed = true;\n%s      break;\n%s    }\n", ind, ind, ind, ind)
		g.pf("%s    final elemLen = r.get32();\n", ind)
		g.pf("%s    if (!r.has(elemLen)) {\n%s      r.report.malformed = true;\n%s      break;\n%s    }\n", ind, ind, ind, ind)
		g.pf("%s    if (key == 0) {\n", ind)
		g.pf("%s      // None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s      // name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s      // merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s      // it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s      // (docs/SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s      r.report.malformed = true;\n%s      break;\n%s    }\n", ind, ind, ind)
		g.assign(ind+"    ", "final slot", vocab(f.KeyEnum)+".valueOf", "key")
		g.pf("%s    if (slot < 0) {\n", ind)
		g.pf("%s      r.report.unknown++; // a slot this reader cannot name\n", ind)
		g.pf("%s      r.offset += elemLen;\n%s      continue;\n%s    }\n", ind, ind, ind)
		g.pf("%s    final pairEnd = r.offset + elemLen;\n", ind)
		g.pf("%s    final pairLimit = r.limit;\n%s    r.limit = pairEnd;\n", ind, ind)
		// the slot the KEY owns (docs/SPEC-TABLES.md §2.4)
		target := g.slot(f, "slot")
		if kind == tkTable {
			g.needMember(f.Type.Name)
			g.pf("%s    %s.loadBody(r);\n", ind, target)
		} else {
			g.emitTableReadScalarFrom(f, kind, target, ind+"    ",
				"r.report.malformed = true;\n"+ind+"      r.limit = pairLimit;\n"+ind+"      r.offset = pairEnd;\n"+ind+"      continue;")
		}
		g.pf("%s    r.limit = pairLimit;\n%s    r.offset = pairEnd;\n", ind, ind)
		g.pf("%s  }\n%s  r.limit = outerLimit;\n%s}\n", ind, ind, ind)
		g.pf("%sr.offset = bodyEnd; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal len = r.get32();\n", ind)
		g.pf("%sif (!r.has(len)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%svar keep = len;\n", ind)
		g.pf("%sif (keep > %d) {\n%s  keep = %d;\n%s  r.report.clamped++;\n%s}\n",
			ind, f.Type.Size, ind, f.Type.Size, ind, ind)
		g.pf("%sr.copyInto(this.%s, keep);\n", ind, name)
		g.pf("%sthis.%sLength = keep;\n", ind, name)
		g.pf("%sr.offset += len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyEnd = r.offset + bodyLen;\n", ind)
		g.pf("%sfinal outerLimit = r.limit;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s  final elemKind = r.get8();\n", ind)
		g.pf("%s  final count = r.get32();\n", ind)
		g.pf("%s  if (elemKind != %d) {\n", ind, kind)
		g.pf("%s    r.report.kindMismatch++;\n%s    r.offset = bodyEnd;\n%s    break;\n%s  }\n", ind, ind, ind, ind)
		g.pf("%s  var keep = count;\n", ind)
		g.pf("%s  if (keep > %d) {\n%s    keep = %d;\n%s    r.report.clamped++;\n%s  }\n",
			ind, bound, ind, bound, ind, ind)
		g.pf("%s  // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s  // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s  // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s  // bytes are never fabricated into elements\n", ind)
		g.pf("%s  r.limit = bodyEnd;\n", ind)
		if f.Type.Kind == ir.TBytes {
			// ONE MEMMOVE for a bytes(N): the elements are the bytes, so the
			// decoded prefix is whatever of `keep` the body covers, copied in
			// one setRange — the same prefix, the same malformed flag, and
			// the same resting offset the element loop below would leave, at
			// a memmove's price rather than a bounds check per byte
			g.pf("%s  var decoded = keep;\n", ind)
			g.pf("%s  if (r.offset + decoded > r.limit) {\n", ind)
			g.pf("%s    decoded = r.limit - r.offset;\n", ind)
			g.pf("%s    r.report.malformed = true;\n%s  }\n", ind, ind)
			g.pf("%s  r.copyInto(this.%s, decoded);\n", ind, name)
			g.pf("%s  r.offset += decoded;\n", ind)
		} else {
			if counted {
				g.pf("%s  var decoded = 0;\n", ind)
			}
			g.pf("%s  for (var i = 0; i < keep; i++) {\n", ind)
			g.emitTableReadElement(f, kind, ind+"    ")
			if counted {
				g.pf("%s    decoded = i + 1;\n", ind)
			}
			g.pf("%s  }\n", ind)
		}
		g.pf("%s  r.limit = outerLimit;\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s  this.%sLength = decoded;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s  this.%sCount = decoded;\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = bodyEnd; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needUnionTag(un)
		g.pf("%sif (!r.has(2)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal armId = r.get16();\n", ind)
		g.pf("%sif (armId == 0) {\n", ind)
		g.pf("%s  // empty: the id is the whole payload\n", ind)
		g.pf("%s  this.%s.type = %sType.none;\n%s  break;\n%s}\n", ind, name, un.Name, ind, ind)
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyEnd = r.offset + bodyLen;\n", ind)
		g.pf("%sfinal outerLimit = r.limit;\n%sr.limit = bodyEnd;\n", ind, ind)
		g.pf("%sswitch (armId) {\n%s  // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n", ind, ind)
		for _, v := range un.Variants {
			g.needMember(v.Type)
			g.pf("%s  case 0x%04x: // %s\n", ind, ir.VariantId(v.Name), v.Name)
			g.pf("%s    this.%s.type = %sType.%s;\n", ind, name, un.Name, dartName(v.Name))
			g.pf("%s    this.%s.%s.loadBody(r);\n", ind, name, dartName(v.Name))
			g.pf("%s    break;\n", ind)
		}
		g.pf("%s  default:\n", ind)
		g.pf("%s    // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s    // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s    // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s    // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s    // standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s    this.%s.type = %sType.none;\n", ind, name, un.Name)
		g.pf("%s    r.report.unknown++;\n%s    break;\n%s}\n", ind, ind, ind)
		g.pf("%sr.limit = outerLimit;\n%sr.offset = bodyEnd;\n", ind, ind)
	case kind == tkTable:
		g.needMember(f.Type.Name)
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) {\n%s  r.report.malformed = true;\n%s  return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal bodyEnd = r.offset + bodyLen;\n", ind)
		g.pf("%sfinal outerLimit = r.limit;\n%sr.limit = bodyEnd;\n", ind, ind)
		g.pf("%sthis.%s.loadBody(r);\n", ind, name)
		g.pf("%sr.limit = outerLimit;\n%sr.offset = bodyEnd;\n", ind, ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "this."+name, ind,
			"r.report.malformed = true;\n"+ind+"  return false;")
	}
}

// emitTableReadElement decodes one array element from the field body;
// truncation keeps the decoded prefix and flags malformed without stopping the
// parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := member(f)
	switch kind {
	case tkTable:
		g.needMember(f.Type.Name)
		g.pf("%sif (!r.has(4)) {\n%s  r.report.malformed = true;\n%s  break;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal elemLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(elemLen)) {\n%s  r.report.malformed = true;\n%s  break;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sfinal elemEnd = r.offset + elemLen;\n", ind)
		g.pf("%sfinal elemLimit = r.limit;\n%sr.limit = elemEnd;\n", ind, ind)
		g.pf("%sthis.%s[i].loadBody(r);\n", ind, name)
		g.pf("%sr.limit = elemLimit;\n%sr.offset = elemEnd;\n", ind, ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("this.%s[i]", name), ind,
			"r.report.malformed = true;\n"+ind+"  break;")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar into a storage
// member, with the range clamps the schema declares. onTrunc is the truncation
// action: a scalar FIELD stops the decode (outer framing damage), an array
// ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif (!r.has(%d)) {\n%s  %s\n%s}\n", ind, width, ind, onTrunc, ind)
	if enum := enumRef(f); enum != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5): an id this build
		// cannot name reads as None and counts as unknown, exactly as an
		// unknown FIELD id does — same event, one counter
		g.pf("%s{\n%s  final variant = r.get16();\n", ind, ind)
		g.pf("%s  var decodedEnum = %s.valueOf(variant);\n", ind, vocab(f.Type.Name))
		g.pf("%s  if (decodedEnum < 0) {\n%s    decodedEnum = %s.none;\n%s    r.report.unknown++;\n%s  }\n",
			ind, ind, f.Type.Name, ind, ind)
		g.needDecl(f.Type.Name)
		g.pf("%s  %s = decodedEnum;\n%s}\n", ind, lvalue, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() != 0;\n", ind, lvalue)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%s{\n%s  var decodedF = tableBitsToFloat(r.get32());\n", ind, ind)
			g.pf("%s  if (decodedF < %s) {\n%s    decodedF = %s;\n%s    r.report.clamped++;\n%s  } else if (decodedF > %s) {\n%s    decodedF = %s;\n%s    r.report.clamped++;\n%s  }\n",
				ind, narrowedFloat32(f.FMin), ind, narrowedFloat32(f.FMin), ind, ind,
				narrowedFloat32(f.FMax), ind, narrowedFloat32(f.FMax), ind, ind)
			g.pf("%s  %s = decodedF;\n%s}\n", ind, lvalue, ind)
			return
		}
		g.pf("%s%s = tableBitsToFloat(r.get32());\n", ind, lvalue)
	case tkF64:
		g.pf("%s%s = tableBitsToDouble(r.get64());\n", ind, lvalue)
	default:
		g.pf("%s{\n%s  var decodedV = r.%s();\n", ind, ind, dartGetter(f, kind))
		if f.HasIntRange {
			low, high := tableClampEnds(f, width)
			unsigned := width == 8 && (f.Type.Kind != ir.TInt || !f.Type.Signed)
			if low {
				lo := dartIntLit(f.IntMin)
				g.pf("%s  if (%s) {\n%s    decodedV = %s;\n%s    r.report.clamped++;\n%s  }",
					ind, dartLess("decodedV", lo, unsigned), ind, lo, ind, ind)
				if high {
					hi := dartIntLit(f.IntMax)
					g.pf(" else if (%s) {\n%s    decodedV = %s;\n%s    r.report.clamped++;\n%s  }",
						dartLess(hi, "decodedV", unsigned), ind, hi, ind, ind)
				}
				g.pf("\n")
			} else if high {
				hi := dartIntLit(f.IntMax)
				g.pf("%s  if (%s) {\n%s    decodedV = %s;\n%s    r.report.clamped++;\n%s  }\n",
					ind, dartLess(hi, "decodedV", unsigned), ind, hi, ind, ind)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s  if (decodedV > %d) {\n%s    decodedV = %d;\n%s    r.report.clamped++; // bits(%d) width clamp\n%s  }\n",
				ind, maxv, ind, maxv, ind, f.Type.Width, ind)
		}
		g.pf("%s  %s = decodedV;\n%s}\n", ind, lvalue, ind)
	}
}

// dartGetter is the reader entry a fixed-width integer kind decodes through.
// The SIGNED narrow kinds have their own getter because Dart's int is 64 bits
// wide and a raw byte read would not sign-extend; u64 and i64 share one, the
// bit pattern being the storage in both.
func dartGetter(f *ir.Field, kind int) string {
	switch kind {
	case tkI8:
		return "getI8"
	case tkI16:
		return "getI16"
	case tkI32:
		return "getI32"
	}
	return tableGet(tableKindWidth(kind))
}

// dartLess renders `a < b` over the bit-transparent int a u64 rides in: at 64
// bits unsigned, the signed comparison would order the domain wrongly.
func dartLess(a, b string, unsigned bool) string {
	if unsigned {
		return fmt.Sprintf("tableUnsignedLess(%s, %s)", a, b)
	}
	return fmt.Sprintf("%s < %s", a, b)
}

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor and the comments ("float32", "bits(9)", "Grade").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return fmt.Sprintf("string(%d)", f.Type.Size)
	case ir.TBytes:
		return fmt.Sprintf("bytes(%d)", f.Type.Size)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}
