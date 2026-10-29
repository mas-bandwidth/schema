package jstable

// variabletyped.go emits the TYPED SAVE of the variable form (docs/PORTING.md
// M22): one function per table that writes its body with the field names and
// the ids spelled out, so a save reads `v.health` and not `v[fd.n]`.
//
// WHY IT EXISTS. The descriptor walk (variableruntime.go) reads every field
// through one megamorphic keyed load, and a megamorphic load of a double field
// answers a FRESH HEAP NUMBER: sixteen bytes per float per save, whatever the
// walk does with it after. A named load on a known shape is a raw double read.
// So the walk is spelled once per table, here, for the shapes a game's config
// is made of (scalars, strings, enums, pointers, tables by value, arrays of
// those, enum-keyed arrays of those), and everything else calls back into the
// descriptor walk with the field's own descriptor.
//
// THE CONTRACT. The typed body is the descriptor walk's body for a save with NO
// retention store: the same bytes, the same ids interned in the same order, the
// same elision and the same refusals. A save that carries a store never enters
// it (TableVariableWriteBody). The runtime holds the two to each other: the
// differential gate writes every corpus through both and compares the bytes.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// denseOf numbers an id for the unit's interner (variableform.go
// TableVariableDense), in the order the typed save first names it. The list is
// handed to the runtime before the link, so the number a typed body spells is
// the number the link gives the id.
func (g *variableGen) denseOf(id uint64) int {
	if g.dense == nil {
		g.dense = map[uint64]int{}
	}
	if d, ok := g.dense[id]; ok {
		return d
	}
	d := len(g.denseList)
	g.dense[id] = d
	g.denseList = append(g.denseList, id)
	return d
}

// jsProp renders a property access on a holder expression.
func jsProp(holder, name string) string {
	ident := name != ""
	for i, r := range name {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		ident = false
	}
	if ident {
		return holder + "." + name
	}
	return holder + "[" + jsStr(name) + "]"
}

// typedFnName is the name of a table's typed save.
func typedFnName(st *ir.Struct) string { return "TableVariableWrite_" + st.Name }

// emitTypedSaves returns the typed save of every table in the closure.
func (g *variableGen) emitTypedSaves(closure []*ir.Struct) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// ---------------------------------------------------------------------------\n")
	fmt.Fprintf(&b, "// THE TYPED SAVE (docs/PORTING.md M22): one body writer per table, its field\n")
	fmt.Fprintf(&b, "// names and ids spelled out. The descriptor walk is the fallback for a field\n")
	fmt.Fprintf(&b, "// shape this does not spell, and the oracle it is held to.\n")
	fmt.Fprintf(&b, "// ---------------------------------------------------------------------------\n\n")
	for _, st := range closure {
		g.emitTypedSave(&b, st)
	}
	return b.String()
}

func (g *variableGen) emitTypedSave(b *strings.Builder, st *ir.Struct) {
	guards := structGuards(st)
	fmt.Fprintf(b, "function %s(w, ids, nodes, v) {\n", typedFnName(st))
	fmt.Fprintf(b, "  const F = TableVariableCodecs.%s.fields;\n", st.Name)
	for i, f := range st.Fields {
		fmt.Fprintf(b, "  // %s\n", f.Name)
		if len(guards[f.Name]) > 0 || f.Type.Optional {
			g.typedFallback(b, i)
			continue
		}
		if !g.typedField(b, f, i) {
			g.typedFallback(b, i)
		}
	}
	fmt.Fprintf(b, "  w.u8(0);\n  return true;\n}\n\n")
}

func (g *variableGen) typedFallback(b *strings.Builder, i int) {
	fmt.Fprintf(b, "  if (!TableVariableWriteField(F[%d], w, ids, nodes, v, null, null)) { return false; }\n", i)
}

// typedKind classifies a field the way fieldDescTail does, in the same order.
func typedKind(f *ir.Field) string {
	switch {
	case f.IsMap():
		return "map"
	case f.KeyEnum != "":
		return "keyed"
	case unionOf(f) != nil && f.Array == ir.ArrayNone:
		return "union"
	case f.Array != ir.ArrayNone:
		return "arr"
	case f.Type.Pointer && f.Type.Blob() && f.Type.Kind == ir.TString:
		return "sblob"
	case f.Type.Pointer && f.Type.Blob():
		return "bblob"
	case f.Type.Pointer:
		return "ptr"
	case enumOf(f) != nil:
		return "enum"
	case flagsOf(f) != nil:
		return "flags"
	case tableOf(f) != nil:
		return "tab"
	case f.Type.Kind == ir.TString:
		return "str"
	case f.Type.Kind == ir.TWString:
		return "wstr"
	case f.Type.Kind == ir.TBytes:
		return "bytes"
	case f.Type.Kind == ir.TBool:
		return "bool"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return "float"
	}
	return "int"
}

// elemKind classifies an array element the way elemDesc does.
func elemKind(f *ir.Field) string {
	switch {
	case unionOf(f) != nil:
		return "union"
	case f.Type.Pointer:
		return "ptr"
	case enumOf(f) != nil:
		return "enum"
	case flagsOf(f) != nil:
		return "flags"
	case tableOf(f) != nil:
		return "tab"
	case f.Type.Kind == ir.TBool:
		return "bool"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return "float"
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		return "other"
	}
	return "int"
}

// writeIntStmt writes one integer of kind k's width.
func writeIntStmt(x string, kind int) string {
	switch ir.TableKindWidth(kind) {
	case 1:
		return fmt.Sprintf("w.u8(%s);", x)
	case 2:
		return fmt.Sprintf("w.u16(%s);", x)
	case 4:
		return fmt.Sprintf("w.u32(%s);", x)
	case 16:
		return fmt.Sprintf("w.u128(%s);", x)
	}
	return fmt.Sprintf("w.u64(%s);", x)
}

func writeFloatStmt(x string, f32 bool) string {
	if f32 {
		return fmt.Sprintf("w.f32(%s);", x)
	}
	return fmt.Sprintf("w.f64(%s);", x)
}

// typedField spells one field's save, answering false for a shape it leaves to
// the descriptor walk.
func (g *variableGen) typedField(b *strings.Builder, f *ir.Field, idx int) bool {
	x := jsProp("v", f.Name)
	id := g.denseOf(ir.TableFieldWireId(f))
	switch typedKind(f) {
	case "bool":
		def := "false"
		if f.HasDefault && f.DefBool {
			def = "true"
		}
		fmt.Fprintf(b, "  if (%s !== %s) { w.head(ids.refd(%d), 1); w.u8(%s ? 1 : 0); }\n", x, def, id, x)
	case "int":
		kind := ir.TableWireScalarKind(f)
		fmt.Fprintf(b, "  if (%s !== %s) { w.head(ids.refd(%d), %d); %s }\n", x, intDefault(f), id, kind, writeIntStmt(x, kind))
	case "float":
		f32 := f.Type.Kind == ir.TFloat32
		cmp := fmt.Sprintf("%s !== %s", x, floatDefault(f))
		if f32 {
			cmp = fmt.Sprintf("Math.fround(%s) !== Math.fround(%s)", x, floatDefault(f))
		}
		fmt.Fprintf(b, "  if (%s) { w.head(ids.refd(%d), %d); %s }\n", cmp, id, ir.TableWireFieldKind(f), writeFloatStmt(x, f32))
	case "str":
		fmt.Fprintf(b, "  if (%s !== %s) { w.head(ids.refd(%d), 12); const m = w.open(); w.utf8(%s); w.close(m); }\n", x, jsStr(string(f.DefBytes)), id, x)
	case "enum":
		fmt.Fprintf(b, "  if (%s !== %s) {\n    const ref = ids.refd(%d);\n    const vref = TableVariableEnumRef(F[%d], ids, %s);\n    if (vref < 0) { return false; }\n    w.leb(ref); w.u8(30); w.leb(vref);\n  }\n", x, enumDefault(f), id, idx, x)
		for _, name := range enumVariantNames(enumOf(f)) {
			g.denseOf(ir.TableWireId(name))
		}
	case "ptr":
		fmt.Fprintf(b, "  if (%s !== null) { w.head(ids.refd(%d), 17); w.leb(nodes.index.get(%s)); }\n", x, id, x)
	case "tab":
		fmt.Fprintf(b, "  {\n    const mark = ids.n;\n    const start = w.n;\n    w.head(ids.refd(%d), 13);\n    const m = w.open();\n    if (!%s(w, ids, nodes, %s)) { return false; }\n    if (w.n - m - 1 <= 1) { ids.rollback(mark); w.n = start; } else { w.close(m); }\n  }\n",
			id, typedFnName(tableOf(f)), x)
	case "arr":
		return g.typedArray(b, f, idx, x, id)
	case "keyed":
		return g.typedKeyed(b, f, idx, x, id)
	default:
		return false
	}
	return true
}

// enumVariantNames lists an enum's variant wire names, so the dense numbering
// holds every id the enum's reference may intern.
func enumVariantNames(e *ir.Enum) []string {
	out := make([]string, len(e.Variants))
	for i := range e.Variants {
		out[i] = e.VariantWireName(i)
	}
	return out
}

// typedElem spells one element's write of an array whose elements are of
// elemKind k, answering false for a shape left to the descriptor.
func (g *variableGen) typedElem(b *strings.Builder, f *ir.Field, desc, x string, indent string) bool {
	switch elemKind(f) {
	case "bool":
		fmt.Fprintf(b, "%sw.u8(%s ? 1 : 0);\n", indent, x)
	case "int":
		fmt.Fprintf(b, "%s%s\n", indent, writeIntStmt(x, ir.TableWireScalarKind(f)))
	case "float":
		fmt.Fprintf(b, "%s%s\n", indent, writeFloatStmt(x, f.Type.Kind == ir.TFloat32))
	case "enum":
		fmt.Fprintf(b, "%s{ const vref = TableVariableEnumRef(%s, ids, %s); if (vref < 0) { return false; } w.leb(vref); }\n", indent, desc, x)
		for _, name := range enumVariantNames(enumOf(f)) {
			g.denseOf(ir.TableWireId(name))
		}
	case "ptr":
		fmt.Fprintf(b, "%sw.leb(%s === null ? 0 : nodes.index.get(%s));\n", indent, x, x)
	case "tab":
		fmt.Fprintf(b, "%s{ const m = w.open(); if (!%s(w, ids, nodes, %s)) { return false; } w.close(m); }\n", indent, typedFnName(tableOf(f)), x)
	default:
		return false
	}
	return true
}

// elemIsDef spells TableVariableElemIsDefault for a non-table element.
func (g *variableGen) elemIsDef(f *ir.Field, x string) string {
	def := g.elemDefaultExpr(f)
	if elemKind(f) == "float" && f.Type.Kind == ir.TFloat32 {
		return fmt.Sprintf("Math.fround(%s) === Math.fround(%s)", x, def)
	}
	return fmt.Sprintf("%s === %s", x, def)
}

// typedArray spells a counted, fixed or unbounded array (kind 14).
func (g *variableGen) typedArray(b *strings.Builder, f *ir.Field, idx int, x string, id int) bool {
	ek := elemKind(f)
	if ek == "union" || ek == "flags" || ek == "other" {
		return false
	}
	var count string
	switch f.Array {
	case ir.ArrayFixed:
		count = fmt.Sprintf("%d", f.ArrayBound)
	case ir.ArrayList:
		count = "a.length"
	default:
		count = fmt.Sprintf("Math.min(a.length, %d)", f.ArrayBound)
	}
	var body strings.Builder
	if !g.typedElem(&body, f, fmt.Sprintf("F[%d].e", idx), "a[i]", "        ") {
		return false
	}
	fmt.Fprintf(b, "  {\n    const a = %s;\n    const n = %s;\n", x, count)
	cond := "n !== 0"
	if f.Array == ir.ArrayFixed && ek != "tab" {
		// a fixed array holding only its defaults elides (encode.go)
		fmt.Fprintf(b, "    let allDef = true;\n    for (let i = 0; i < n; i++) { if (!(%s)) { allDef = false; break; } }\n", g.elemIsDef(f, "a[i]"))
		cond = "n !== 0 && !allDef"
	}
	fmt.Fprintf(b, "    if (%s) {\n      w.head(ids.refd(%d), 14);\n      const m = w.open();\n      w.u8(%d);\n      w.leb(n);\n      for (let i = 0; i < n; i++) {\n", cond, id, ir.TableWireElemKind(f))
	b.WriteString(body.String())
	fmt.Fprintf(b, "      }\n      w.close(m);\n    }\n  }\n")
	return true
}

// typedKeyed spells an enum-keyed array (kind 16): each slot a literal.
func (g *variableGen) typedKeyed(b *strings.Builder, f *ir.Field, idx int, x string, id int) bool {
	ek := elemKind(f)
	if ek == "union" || ek == "flags" || ek == "other" {
		return false
	}
	var out strings.Builder
	fmt.Fprintf(&out, "  {\n    const a = %s;\n    const mark = ids.n;\n    const start = w.n;\n    w.head(ids.refd(%d), 16);\n    const m = w.open();\n    w.u8(%d);\n    const cm = w.open();\n    let pairs = 0;\n", x, id, ir.TableWireElemKind(f))
	for s := 0; s < int(f.ArrayBound); s++ {
		if s >= len(f.KeyEnumRef.Variants) {
			// a slot past the declared variants has no name: the save refuses
			fmt.Fprintf(&out, "    return false;\n")
			break
		}
		sid := g.denseOf(ir.TableWireId(f.KeyEnumRef.VariantWireName(s)))
		fmt.Fprintf(&out, "    {\n      const sm = ids.n;\n      const at = w.n;\n      w.leb(ids.refd(%d));\n      const em = w.open();\n", sid)
		el := fmt.Sprintf("a[%d]", s)
		if ek == "tab" {
			fmt.Fprintf(&out, "      if (!%s(w, ids, nodes, %s)) { return false; }\n      if (w.n - em - 1 <= 1) { ids.rollback(sm); w.n = at; } else { w.close(em); pairs++; }\n",
				typedFnName(tableOf(f)), el)
		} else {
			var body strings.Builder
			if !g.typedElem(&body, f, fmt.Sprintf("F[%d].e", idx), el, "        ") {
				return false
			}
			fmt.Fprintf(&out, "      if (%s) { ids.rollback(sm); w.n = at; } else {\n%s        w.close(em);\n        pairs++;\n      }\n", g.elemIsDef(f, el), body.String())
		}
		fmt.Fprintf(&out, "    }\n")
	}
	fmt.Fprintf(&out, "    if (pairs === 0) { ids.rollback(mark); w.n = start; } else { w.fit(cm, pairs, 1); w.close(m); }\n  }\n")
	b.WriteString(out.String())
	return true
}
