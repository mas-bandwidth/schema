package rusttable

// Maps and union arms on the variable wire (docs/SPEC-TABLES.md §2.6, §2.8, §3).
// A map is an array of one generated { key, value } entry, written in ascending
// key order and never elided as an element. A union is an arm header: the arm
// id reference, the arm's kind byte, L, then L bytes of the arm's field type.
// None elides as a field and rides as the zero reference as an array element.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func unionOf(f *ir.Field) *ir.Union {
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

// collectVariableUnions returns each union an emitted table reaches, after the
// unions its arms reach and before the unions that hold it (docs/SPEC-TABLES.md §2.6).
func (g *gen) collectVariableUnions(names []string) []*ir.Union {
	seen := map[*ir.Union]bool{}
	var order []*ir.Union
	var walk func(un *ir.Union)
	walk = func(un *ir.Union) {
		if un == nil || seen[un] {
			return
		}
		seen[un] = true
		for _, v := range un.Variants {
			if v.F != nil {
				g.walkFieldUnions(v.F, walk)
			}
		}
		order = append(order, un)
	}
	for _, n := range names {
		for _, f := range vStruct(g.unit, n).Fields {
			g.walkFieldUnions(f, walk)
		}
	}
	return order
}

// emitUnionTypes emits each union an emitted table reaches, after the structs
// its arms name by value and before the structs that hold the union
// (docs/SPEC-TABLES.md §2.6).
func (g *gen) emitUnionTypes(b *strings.Builder, names []string) {
	for _, un := range g.collectVariableUnions(names) {
		g.emitOneUnion(b, un)
	}
}

func (g *gen) walkFieldUnions(f *ir.Field, walk func(*ir.Union)) {
	if un := unionOf(f); un != nil {
		walk(un)
	}
	if f.IsMap() && f.MapEntry != nil {
		for _, ef := range f.MapEntry.Fields {
			g.walkFieldUnions(ef, walk)
		}
	}
}

func (g *gen) emitOneUnion(b *strings.Builder, un *ir.Union) {
	fmt.Fprintf(b, "/// %s — a union on the variable wire (docs/SPEC-TABLES.md §2.6, §3).\n", un.Name)
	fmt.Fprintf(b, "#[derive(Clone, Debug, Default)]\n")
	fmt.Fprintf(b, "pub enum %s {\n", un.Name)
	fmt.Fprintf(b, "    #[default]\n    None,\n")
	for _, v := range un.Variants {
		if v.Void() {
			fmt.Fprintf(b, "    %s,\n", rustVariant(v.Name))
			continue
		}
		fmt.Fprintf(b, "    %s(%s),\n", rustVariant(v.Name), variableFieldType(v.F))
	}
	b.WriteString("}\n\n")
}

// structHoldsUnion reports a table whose storage names a union, so the union
// type has to be emitted first.
func structHoldsUnion(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if unionOf(f) != nil {
			return true
		}
	}
	return false
}

func (g *gen) emitMapNumber(b *strings.Builder, f *ir.Field, x string) {
	fmt.Fprintf(b, "    for e in %s.iter() { %s(e, g); }\n", x, fn(f.MapEntry.Name, "number"))
}

func (g *gen) emitUnionNumber(b *strings.Builder, f *ir.Field, x string) {
	g.emitUnionNumberInd(b, f, x, "    ")
}

func (g *gen) emitUnionNumberInd(b *strings.Builder, f *ir.Field, x, ind string) {
	un := unionOf(f)
	fmt.Fprintf(b, "%smatch %s {\n", ind, borrowOf(x))
	in := ind + "    "
	for _, v := range un.Variants {
		if v.Void() || v.F == nil {
			continue
		}
		arm := rustVariant(v.Name)
		if v.F.KeyEnum != "" || v.F.Array != ir.ArrayNone {
			switch classOf(v.F) {
			case vBlob:
				bytes := "b.as_bytes()"
				if v.F.Type.Kind == ir.TBytes {
					bytes = "&b[..]"
				}
				fmt.Fprintf(b, "%s%s::%s(es) => { for b in es.iter().flatten() { g.visit_blob(%s, Rc::as_ptr(b) as *const u8 as usize, %s); } }\n", in, un.Name, arm, blobTypeConst(v.F), bytes)
			case vPointer:
				fmt.Fprintf(b, "%s%s::%s(es) => { for n in es.iter().flatten() { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } } }\n", in, un.Name, arm, ir.TableWireId(v.F.Type.Ref.(*ir.Struct).WireName()), fn(v.F.Type.Name, "number"))
			case vTable:
				fmt.Fprintf(b, "%s%s::%s(es) => { for e in es.iter() { %s(e, g); } }\n", in, un.Name, arm, fn(v.F.Type.Name, "number"))
			case vUnion:
				fmt.Fprintf(b, "%s%s::%s(es) => { for e in es.iter() {\n", in, un.Name, arm)
				g.emitUnionNumberInd(b, v.F, "(*e)", in+"    ")
				fmt.Fprintf(b, "%s} }\n", in)
			default:
				fmt.Fprintf(b, "%s%s::%s(_) => {}\n", in, un.Name, arm)
			}
			continue
		}
		switch classOf(v.F) {
		case vBlob:
			bytes := "b.as_bytes()"
			if v.F.Type.Kind == ir.TBytes {
				bytes = "&b[..]"
			}
			fmt.Fprintf(b, "%s%s::%s(b) => { if let Some(b) = b { g.visit_blob(%s, Rc::as_ptr(b) as *const u8 as usize, %s); } }\n", in, un.Name, arm, blobTypeConst(v.F), bytes)
		case vPointer:
			fmt.Fprintf(b, "%s%s::%s(n) => { if let Some(n) = n && g.visit_table(0x%016x, n.clone()) { %s(n, g); } }\n", in, un.Name, arm, ir.TableWireId(v.F.Type.Ref.(*ir.Struct).WireName()), fn(v.F.Type.Name, "number"))
		case vTable:
			fmt.Fprintf(b, "%s%s::%s(e) => { %s(e, g); }\n", in, un.Name, arm, fn(v.F.Type.Name, "number"))
		case vMap:
			fmt.Fprintf(b, "%s%s::%s(e) => { for ent in e.iter() { %s(ent, g); } }\n", in, un.Name, arm, fn(v.F.MapEntry.Name, "number"))
		case vUnion:
			fmt.Fprintf(b, "%s%s::%s(e) => {\n", in, un.Name, arm)
			g.emitUnionNumberInd(b, v.F, "(*e)", in+"    ")
			fmt.Fprintf(b, "%s}\n", in)
		default:
			fmt.Fprintf(b, "%s%s::%s(_) => {}\n", in, un.Name, arm)
		}
	}
	fmt.Fprintf(b, "%s_ => {}\n", in)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitOptionalSaveField writes a present optional field unconditionally and
// elides None (docs/SPEC-TABLES.md §2.3, §3).
func (g *gen) emitOptionalSaveField(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	id := ir.TableFieldWireId(f)
	kind := fieldWireKind(f)
	inner := ind + "    "
	fmt.Fprintf(b, "%sif let Some(opt) = &%s {\n", ind, x)
	if f.Array != ir.ArrayNone {
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		g.emitArrayBodyWrite(b, f, "opt", "body", ordinal, inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindArray)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	switch classOf(f) {
	case vTable:
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		emitTableBodyInto(b, f, "opt", "body", fmt.Sprintf("path.step(%d, 0)", ordinal), inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindTable)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
	case vUnion:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindUnion)
		g.emitUnionElement(b, f, "opt", "(*out)", ordinal, inner)
	case vString:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, opt.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(opt.as_bytes());\n", inner)
	case vBytes:
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		fmt.Fprintf(b, "%sbody.push(%d);\n", inner, ir.TableKindU8)
		fmt.Fprintf(b, "%sleb_write(&mut body, opt.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sbody.extend_from_slice(opt);\n", inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
	case vEnum:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%slet vref = %s;\n", inner, enumRef(f, "*opt"))
		fmt.Fprintf(b, "%sleb_write(out, vref);\n", inner)
	default:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%s%s\n", inner, g.scalarWrite(f, "(*opt)", "out"))
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitFixedArraySave writes a fixed array [N]T: a fixed array of tables always
// rides; any other fixed array elides when all N slots hold their default
// (docs/SPEC-TABLES.md §2.2, §3).
func (g *gen) emitFixedArraySave(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	id := ir.TableFieldWireId(f)
	inner := ind + "    "
	if classOf(f) == vTable {
		fmt.Fprintf(b, "%s{\n", ind)
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		g.emitArrayBodyWrite(b, f, x, "body", ordinal, inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindArray)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	fmt.Fprintf(b, "%sif %s.iter().take(%d).any(|e| %s) {\n", ind, x, f.ArrayBound, elemNonDefaultExpr(f, "e"))
	fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
	fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
	g.emitArrayBodyWrite(b, f, x, "body", ordinal, inner)
	fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
	fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindArray)
	fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
	fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
	fmt.Fprintf(b, "%s}\n", ind)
}

func elemNonDefaultExpr(f *ir.Field, e string) string {
	elem := scalarOnly(f)
	switch classOf(elem) {
	case vBlob, vPointer:
		return e + ".is_some()"
	case vString:
		if len(f.DefBytes) > 0 {
			return e + ".as_bytes() != " + rustByteArray(f.DefBytes)
		}
		return "!" + e + ".is_empty()"
	case vBytes:
		if len(f.DefBytes) > 0 {
			return e + ".as_slice() != " + rustByteArray(f.DefBytes)
		}
		return "!" + e + ".is_empty()"
	case vEnum:
		return "*" + e + " != " + enumDefault(f)
	case vUnion:
		return fmt.Sprintf("!matches!(%s, %s::None)", e, unionOf(f).Name)
	default:
		if f.Type.Kind == ir.TBool {
			if f.HasDefault && f.DefBool {
				return "!*" + e
			}
			return "*" + e
		}
		return scalarDiffers(f, "(*"+e+")", scalarDefault(f))
	}
}

// emitKeyedSave writes an enum-keyed array as kind 16: element kind, pair
// count, and (key_ref, L, elem_bytes) triples for non-default slots in variant
// declaration order (docs/SPEC-TABLES.md §2.4, §3.2).
func (g *gen) emitKeyedSave(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	if f.KeyEnumRef == nil || len(f.KeyEnumRef.Variants) == 0 {
		return
	}
	id := ir.TableFieldWireId(f)
	inner := ind + "    "
	fmt.Fprintf(b, "%s{\n", ind)
	fmt.Fprintf(b, "%sconst KEYS: [u64; %d] = [", inner, len(f.KeyEnumRef.Variants))
	for i := range f.KeyEnumRef.Variants {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "0x%016x", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
	}
	b.WriteString("];\n")
	fmt.Fprintf(b, "%slet mark = ids.mark();\n", inner)
	fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
	fmt.Fprintf(b, "%slet mut pairs = 0u64;\n", inner)
	fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
	fmt.Fprintf(b, "%sfor (j, &kid) in KEYS.iter().enumerate() {\n", inner)
	ein := inner + "    "
	fmt.Fprintf(b, "%sif j >= %s.len() { break; }\n", ein, x)
	fmt.Fprintf(b, "%slet e = &%s[j];\n", ein, x)
	fmt.Fprintf(b, "%slet slot_mark = ids.mark();\n", ein)
	fmt.Fprintf(b, "%slet kref = ids.id_ref(kid);\n", ein)
	if classOf(f) == vTable {
		fmt.Fprintf(b, "%slet mut eb = Vec::new();\n", ein)
		emitTableBodyInto(b, f, "e", "eb", fmt.Sprintf("path.step(%d, j as u32)", ordinal), ein)
		fmt.Fprintf(b, "%sif eb.len() <= 1 { ids.rollback(slot_mark); continue; }\n", ein)
	} else {
		fmt.Fprintf(b, "%sif !(%s) { ids.rollback(slot_mark); continue; }\n", ein, elemNonDefaultExpr(f, "e"))
		fmt.Fprintf(b, "%slet mut eb = Vec::new();\n", ein)
		switch classOf(f) {
		case vPointer:
			fmt.Fprintf(b, "%sleb_write(&mut eb, match e { Some(n) => g.node_index(n), None => 0 });\n", ein)
		case vBlob:
			fmt.Fprintf(b, "%sleb_write(&mut eb, match e { Some(n) => g.blob_index(Rc::as_ptr(n) as *const u8 as usize), None => 0 });\n", ein)
		case vEnum:
			fmt.Fprintf(b, "%slet vref = %s;\n", ein, enumRef(f, "*e"))
			fmt.Fprintf(b, "%sleb_write(&mut eb, vref);\n", ein)
		case vUnion:
			g.withArmIndex("j as u32", func() { g.emitUnionElement(b, f, "e", "eb", ordinal, ein) })
		case vString:
			fmt.Fprintf(b, "%seb.extend_from_slice(e.as_bytes());\n", ein)
		default:
			fmt.Fprintf(b, "%s%s\n", ein, g.scalarWrite(f, "(*e)", "eb"))
		}
	}
	fmt.Fprintf(b, "%sleb_write(&mut body, kref);\n", ein)
	fmt.Fprintf(b, "%sleb_write(&mut body, eb.len() as u64);\n", ein)
	fmt.Fprintf(b, "%sbody.extend_from_slice(&eb);\n", ein)
	fmt.Fprintf(b, "%spairs += 1;\n", ein)
	fmt.Fprintf(b, "%s}\n", inner)
	fmt.Fprintf(b, "%sif pairs == 0 {\n", inner)
	fmt.Fprintf(b, "%s    ids.rollback(mark);\n", inner)
	fmt.Fprintf(b, "%s} else {\n", inner)
	fmt.Fprintf(b, "%s    let mut inner_body = Vec::new();\n", inner)
	fmt.Fprintf(b, "%s    inner_body.push(%d);\n", inner, elemWireKind(f))
	fmt.Fprintf(b, "%s    leb_write(&mut inner_body, pairs);\n", inner)
	fmt.Fprintf(b, "%s    inner_body.extend_from_slice(&body);\n", inner)
	fmt.Fprintf(b, "%s    leb_write(out, r);\n", inner)
	fmt.Fprintf(b, "%s    out.push(%d);\n", inner, ir.TableKindKeyed)
	fmt.Fprintf(b, "%s    leb_write(out, inner_body.len() as u64);\n", inner)
	fmt.Fprintf(b, "%s    out.extend_from_slice(&inner_body);\n", inner)
	fmt.Fprintf(b, "%s}\n", inner)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitMapSave writes one map as kind 14 over element kind 13, entries in
// ascending key order, each entry always riding (docs/SPEC-TABLES.md §2.8, §3).
func (g *gen) emitMapSave(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	id := ir.TableFieldWireId(f)
	inner := ind + "    "
	fmt.Fprintf(b, "%sif !%s.is_empty() {\n", ind, x)
	fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
	fmt.Fprintf(b, "%slet mut order: Vec<usize> = (0..%s.len()).collect();\n", inner, x)
	fmt.Fprintf(b, "%sorder.sort_by(|&a, &b| %s);\n", inner, mapKeyOrd(f, x+"[a]."+ir.RustSnake(ir.MapKeyFieldName), x+"[b]."+ir.RustSnake(ir.MapKeyFieldName)))
	fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
	fmt.Fprintf(b, "%sbody.push(%d); // element kind 13: the entry is a table (§2.8)\n", inner, ir.TableKindTable)
	fmt.Fprintf(b, "%sleb_write(&mut body, %s.len() as u64);\n", inner, x)
	fmt.Fprintf(b, "%sfor (j, &at) in order.iter().enumerate() {\n", inner)
	ein := inner + "    "
	fmt.Fprintf(b, "%slet mut eb = Vec::new();\n", ein)
	fmt.Fprintf(b, "%slet bp = path.step(%d, j as u32);\n", ein, ordinal)
	fmt.Fprintf(b, "%s%s(&%s[at], &mut eb, ids, g, retain.as_deref_mut(), report.as_deref_mut(), bp);\n", ein, fn(f.MapEntry.Name, "save_body"), x)
	fmt.Fprintf(b, "%sif let (Some(ret), Some(rep)) = (retain.as_mut(), report.as_mut()) {\n", ein)
	fmt.Fprintf(b, "%s    retain_emit_tail(&mut eb, ids, ret, KNOWN_IDS, bp, rep);\n", ein)
	fmt.Fprintf(b, "%s}\n", ein)
	fmt.Fprintf(b, "%seb.push(0); // the entry always rides, an all-default value included (§2.8)\n", ein)
	fmt.Fprintf(b, "%sleb_write(&mut body, eb.len() as u64);\n", ein)
	fmt.Fprintf(b, "%sbody.extend_from_slice(&eb);\n", ein)
	fmt.Fprintf(b, "%s}\n", inner)
	fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
	fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindArray)
	fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
	fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
	fmt.Fprintf(b, "%s}\n", ind)
}

func mapKeyOrd(f *ir.Field, a, b string) string {
	key := ir.MapKeyField(f)
	if key.Type.Kind == ir.TString {
		return fmt.Sprintf("%s.as_bytes().cmp(%s.as_bytes())", a, b)
	}
	return fmt.Sprintf("%s.cmp(&%s)", a, b)
}

// emitUnionSave writes a set arm and elides None (docs/SPEC-TABLES.md §2.6, §3).
func (g *gen) emitUnionSave(b *strings.Builder, f *ir.Field, ordinal int, x, ind string) {
	un := unionOf(f)
	id := ir.TableFieldWireId(f)
	inner := ind + "    "
	fmt.Fprintf(b, "%smatch %s {\n", ind, borrowOf(x))
	fmt.Fprintf(b, "%s    %s::None => {}\n", ind, un.Name)
	for _, v := range un.Variants {
		fmt.Fprintf(b, "%s    %s::%s", ind, un.Name, rustVariant(v.Name))
		if v.Void() {
			b.WriteString(" => {\n")
		} else {
			fmt.Fprintf(b, "(arm) => {\n")
		}
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, ir.TableKindUnion)
		g.emitArmHeader(b, v, "arm", ordinal, inner)
		fmt.Fprintf(b, "%s}\n", ind)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

func armWireKind(v ir.UnionVariant) int {
	if v.Void() {
		return ir.TableKindNoPayload
	}
	if v.Body() {
		return ir.TableKindTable
	}
	f := v.F
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return ir.TableKindPointer
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		return ir.TableKindArray
	}
	return ir.TableWireFieldKind(f)
}

// emitArmHeader writes the arm id, kind byte, L and payload (docs/SPEC-TABLES.md §3).
func (g *gen) emitArmHeader(b *strings.Builder, v ir.UnionVariant, arm string, ordinal int, ind string) {
	fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", ind, ir.TableWireId(v.WireName()))
	fmt.Fprintf(b, "%sout.push(%d);\n", ind, armWireKind(v))
	if v.Void() {
		fmt.Fprintf(b, "%sleb_write(out, 0);\n", ind)
		return
	}
	fmt.Fprintf(b, "%slet mut payload = Vec::new();\n", ind)
	g.emitArmPayload(b, v, arm, ordinal, ind)
	fmt.Fprintf(b, "%sleb_write(out, payload.len() as u64);\n", ind)
	fmt.Fprintf(b, "%sout.extend_from_slice(&payload);\n", ind)
}

func (g *gen) emitArmPayload(b *strings.Builder, v ir.UnionVariant, arm string, ordinal int, ind string) {
	f := v.F
	switch {
	case v.Body():
		fmt.Fprintf(b, "%s%s(%s, &mut payload, ids, g, retain.as_deref_mut(), report.as_deref_mut(), %s);\n", ind, fn(f.Type.Name, "save_body"), arm, g.armPath(ordinal))
		fmt.Fprintf(b, "%sif let (Some(ret), Some(rep)) = (retain.as_mut(), report.as_mut()) {\n", ind)
		fmt.Fprintf(b, "%s    retain_emit_tail(&mut payload, ids, ret, KNOWN_IDS, %s, rep);\n", ind, g.armPath(ordinal))
		fmt.Fprintf(b, "%s}\n", ind)
		fmt.Fprintf(b, "%spayload.push(0);\n", ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		if f.Type.Blob() {
			fmt.Fprintf(b, "%slet index = match %s { Some(n) => g.blob_index(Rc::as_ptr(n) as *const u8 as usize), None => 0 };\n", ind, arm)
		} else {
			fmt.Fprintf(b, "%slet index = match %s { Some(n) => g.node_index(n), None => 0 };\n", ind, arm)
		}
		fmt.Fprintf(b, "%sleb_write(&mut payload, index);\n", ind)
	case f.Type.Kind == ir.TString && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%spayload.extend_from_slice(%s.as_bytes());\n", ind, arm)
	case f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone:
		// AN ARM'S L IS THE WSTRING'S BYTE LENGTH (docs/SPEC-TABLES.md §3's arm
		// payload table), so the units ride under it with no length of their
		// own; over N is the save's refusal by name (§3, SPEC.md §4.12)
		fmt.Fprintf(b, "%slet n = table_wide_fit(ids, %s, %d, %q);\n", ind, arm, f.Type.Size, wideOwner(g.unit, f)+"."+f.Name) // arm is the match's reference
		fmt.Fprintf(b, "%stable_wide_put(&mut payload, &%s[..n]);\n", ind, arm)
	case classOf(f) == vUnion && f.Array == ir.ArrayNone:
		g.emitUnionElement(b, f, arm, "payload", ordinal, ind)
	case f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
		g.emitArrayBodyWrite(b, f, arm, "payload", ordinal, ind)
	case classOf(f) == vEnum:
		fmt.Fprintf(b, "%slet vref = %s;\n", ind, enumRef(f, "*"+arm))
		fmt.Fprintf(b, "%sleb_write(&mut payload, vref);\n", ind)
	default:
		fmt.Fprintf(b, "%s%s\n", ind, g.scalarWrite(f, "(*"+arm+")", "payload"))
	}
}

// emitArrayBodyWrite writes an array body with no outer L: element kind, count,
// elements (docs/SPEC-TABLES.md §3). An arm's L already frames it.
func (g *gen) emitArrayBodyWrite(b *strings.Builder, f *ir.Field, x, dst string, ordinal int, ind string) {
	ek := ir.TableKindU8
	if f.Type.Kind != ir.TBytes || f.Type.Pointer {
		ek = elemWireKind(f)
	}
	count := x + ".len()"
	switch {
	case f.Array == ir.ArrayFixed:
		count = fmt.Sprintf("%d", f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		count = fmt.Sprintf("%s.len().min(%d)", x, f.ArrayBound)
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		count = fmt.Sprintf("%s.len().min(%d)", x, f.Type.Size)
	}
	fmt.Fprintf(b, "%s%s.push(%d);\n", ind, dst, ek)
	fmt.Fprintf(b, "%slet count = %s;\n", ind, count)
	fmt.Fprintf(b, "%sleb_write(&mut %s, count as u64);\n", ind, dst)
	fmt.Fprintf(b, "%sfor (j, e) in %s.iter().take(count).enumerate() {\n", ind, x)
	g.emitElementWrite(b, f, "e", dst, ordinal, ind+"    ")
	fmt.Fprintf(b, "%s}\n", ind)
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%sfor j in %s.len()..count {\n", ind, x)
		fmt.Fprintf(b, "%s    let e: &%s = &%s;\n", ind, elemRust(scalarOnly(f)), fieldDefault(scalarOnly(f)))
		fmt.Fprintf(b, "%s    let _ = j;\n", ind)
		g.emitElementWrite(b, f, "e", dst, ordinal, ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
	}
}

func (g *gen) emitElementWrite(b *strings.Builder, f *ir.Field, x, dst string, ordinal int, ind string) {
	fmt.Fprintf(b, "%slet _ = j;\n", ind)
	switch classOf(f) {
	case vTable:
		fmt.Fprintf(b, "%slet mut eb = Vec::new();\n", ind)
		emitTableBodyInto(b, f, x, "eb", fmt.Sprintf("path.step(%d, j as u32)", ordinal), ind)
		fmt.Fprintf(b, "%sleb_write(&mut %s, eb.len() as u64);\n", ind, dst)
		fmt.Fprintf(b, "%s%s.extend_from_slice(&eb);\n", ind, dst)
	case vPointer:
		fmt.Fprintf(b, "%sleb_write(&mut %s, match %s { Some(n) => g.node_index(n), None => 0 });\n", ind, dst, x)
	case vBlob:
		fmt.Fprintf(b, "%sleb_write(&mut %s, match %s { Some(n) => g.blob_index(Rc::as_ptr(n) as *const u8 as usize), None => 0 });\n", ind, dst, x)
	case vEnum:
		fmt.Fprintf(b, "%sleb_write(&mut %s, %s);\n", ind, dst, enumRef(f, "*"+x))
	case vUnion:
		g.withArmIndex("j as u32", func() { g.emitUnionElement(b, f, x, dst, ordinal, ind) })
	case vString:
		fmt.Fprintf(b, "%s%s.extend_from_slice(%s.as_bytes());\n", ind, dst, x)
	case vBytes:
		fmt.Fprintf(b, "%s%s.push(*%s);\n", ind, dst, x)
	default:
		fmt.Fprintf(b, "%s%s\n", ind, g.scalarWrite(f, "(*"+x+")", dst))
	}
}

// emitUnionElement writes one array element's union payload: the zero reference
// for None, otherwise the arm header (docs/SPEC-TABLES.md §3).
func (g *gen) emitUnionElement(b *strings.Builder, f *ir.Field, x, dst string, ordinal int, ind string) {
	un := unionOf(f)
	fmt.Fprintf(b, "%smatch %s {\n", ind, x)
	fmt.Fprintf(b, "%s    %s::None => { leb_write(&mut %s, 0); }\n", ind, un.Name, dst)
	for _, v := range un.Variants {
		if v.Void() {
			fmt.Fprintf(b, "%s    %s::%s => {\n", ind, un.Name, rustVariant(v.Name))
		} else {
			fmt.Fprintf(b, "%s    %s::%s(arm) => {\n", ind, un.Name, rustVariant(v.Name))
		}
		// the element writes into dst, not out: redirect by a local alias
		fmt.Fprintf(b, "%s        let mut out = &mut %s;\n", ind, dst)
		g.emitArmHeader(b, v, "arm", ordinal, ind+"        ")
		fmt.Fprintf(b, "%s    }\n", ind)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitOptionalLoadField decodes an optional field into Some(...) (docs/SPEC-TABLES.md §2.3, §4).
func (g *gen) emitOptionalLoadField(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	inner := *f
	inner.Type.Optional = false
	defExpr := fieldDefault(&inner)
	kind := fieldWireKind(f)
	if f.Array != ir.ArrayNone {
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindArray, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		i1 := in + "    "
		fmt.Fprintf(b, "%slet Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", i1)
		fmt.Fprintf(b, "%sif *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", i1)
		fmt.Fprintf(b, "%slet sub = *off + l as usize;\n", i1)
		fmt.Fprintf(b, "%sif l < 2 {\n", i1)
		fmt.Fprintf(b, "%s    if %s.is_none() { %s = Some(%s); }\n", i1, field, field, defExpr)
		fmt.Fprintf(b, "%s} else {\n", i1)
		i2 := i1 + "    "
		fmt.Fprintf(b, "%slet mut arr = %s.clone()%s;\n", i2, field, orElse(defExpr))
		fmt.Fprintf(b, "%slet mut ok = true;\n", i2)
		g.emitArrayBodyReadChecked(b, &inner, "arr", "sub", "end", "ok", ordinal, i2, true)
		fmt.Fprintf(b, "%sif ok { %s = Some(arr); }\n", i2, field)
		fmt.Fprintf(b, "%s}\n", i1)
		fmt.Fprintf(b, "%s*off = sub;\n", i1)
		fmt.Fprintf(b, "%s}\n", in)
		return
	}
	switch classOf(&inner) {
	case vTable:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", in)
		fmt.Fprintf(b, "%s    let sub = *off + l as usize;\n", in)
		fmt.Fprintf(b, "%s    if let Some(ret) = retain.as_deref_mut() { retain_discard(ret, path, %d); }\n", in, ordinal)
		fmt.Fprintf(b, "%s    let mut tmp = %s::default();\n", in, f.Type.Name)
		fmt.Fprintf(b, "%s    let mut o = *off;\n", in)
		fmt.Fprintf(b, "%s    let _ = %s(wire, &mut o, sub, ids, &mut tmp, nodes, report, path.step(%d, 0), retain.as_deref_mut(), true);\n", in, fn(f.Type.Name, "load_body"), ordinal)
		fmt.Fprintf(b, "%s    if o != sub { report.malformed = true; tmp = %s::default(); }\n", in, f.Type.Name)
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s    *off = sub;\n", in)
		fmt.Fprintf(b, "%s}\n", in)
	case vUnion:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindUnion, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let mut tmp = %s::None;\n", in, unionOf(f).Name)
		g.emitUnionDecode(b, &inner, "tmp", "end", ordinal, in+"    ", true)
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s}\n", in)
	case vString:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", in)
		fmt.Fprintf(b, "%s    let s = *off + l as usize;\n", in)
		fmt.Fprintf(b, "%s    let b = &wire[*off..s];\n", in)
		fmt.Fprintf(b, "%s    let mut tmp = %s;\n", in, defExpr)
		fmt.Fprintf(b, "%s    if !text_valid(b) { report.malformed = true; }\n", in)
		fmt.Fprintf(b, "%s    else if b.len() > %d {\n", in, f.Type.Size)
		fmt.Fprintf(b, "%s        let mut cut = %d;\n", in, f.Type.Size)
		fmt.Fprintf(b, "%s        while cut > 0 && b[cut] & 0xC0 == 0x80 { cut -= 1; }\n", in)
		fmt.Fprintf(b, "%s        tmp = std::str::from_utf8(&b[..cut]).unwrap().to_string();\n", in)
		fmt.Fprintf(b, "%s        report.clamped += 1;\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s    else { tmp = std::str::from_utf8(b).unwrap().to_string(); }\n", in)
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s    *off = s;\n", in)
		fmt.Fprintf(b, "%s}\n", in)
	case vBytes:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
		fmt.Fprintf(b, "%s    if *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", in)
		fmt.Fprintf(b, "%s    let sub = *off + l as usize;\n", in)
		fmt.Fprintf(b, "%s    let mut tmp = %s.take()%s;\n", in, field, orElse(defExpr))
		fmt.Fprintf(b, "%s    if l >= 2 {\n", in)
		fmt.Fprintf(b, "%s        let ek = wire[*off]; *off += 1;\n", in)
		// THE HEADER IS READ AGAINST THE PARENT (docs/SPEC-TABLES.md §3, the
		// oracle's arrayBody): a count spelled past this body's L still reads,
		// and the elements are bounded by the L.
		fmt.Fprintf(b, "%s        match leb_read(wire, off) {\n", in)
		fmt.Fprintf(b, "%s            None => { report.malformed = true; }\n", in)
		fmt.Fprintf(b, "%s            Some(count) => {\n", in)
		fmt.Fprintf(b, "%s                if ek != %d { report.kind_mismatch += 1; }\n", in, ir.TableKindU8)
		fmt.Fprintf(b, "%s                else {\n", in)
		fmt.Fprintf(b, "%s                    let keep = if count > %d { report.clamped += 1; %d } else { count as usize };\n", in, f.Type.Size, f.Type.Size)
		fmt.Fprintf(b, "%s                    let avail = sub.saturating_sub(*off);\n", in)
		fmt.Fprintf(b, "%s                    let n = if keep > avail { report.malformed = true; avail } else { keep };\n", in)
		fmt.Fprintf(b, "%s                    tmp = wire[*off..*off + n].to_vec();\n", in)
		fmt.Fprintf(b, "%s                }\n", in)
		fmt.Fprintf(b, "%s            }\n", in)
		fmt.Fprintf(b, "%s        }\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s    *off = sub;\n", in)
		fmt.Fprintf(b, "%s}\n", in)
	case vEnum:
		fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		emitEnumDecode(b, &inner, "let tmp", "end", "return false;", in+"    ")
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s}\n", in)
	default:
		fmt.Fprintf(b, "%sif kind != %d && !table_kind_widens(kind, %d) { %s }\n", in, kind, kind, kindSkip)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    if kind != %d { report.widened += 1; }\n", in, kind)
		fmt.Fprintf(b, "%s    let Some(raw) = %s(wire, off, end, kind) else { report.malformed = true; return false; };\n", in, scalarReadFn(f))
		emitScalarConvert(b, &inner, g.floatOwner(f), "let tmp", "kind", in+"    ")
		fmt.Fprintf(b, "%s    %s = Some(tmp);\n", in, field)
		fmt.Fprintf(b, "%s}\n", in)
	}
}

// emitKeyedLoad reads an enum-keyed array (docs/SPEC-TABLES.md §2.4, §3.2, §4).
func (g *gen) emitKeyedLoad(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	ek := elemWireKind(f)
	fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindKeyed, kindSkip)
	fmt.Fprintf(b, "%selse {\n", in)
	i1 := in + "    "
	fmt.Fprintf(b, "%slet Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", i1)
	fmt.Fprintf(b, "%sif *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", i1)
	fmt.Fprintf(b, "%slet sub = *off + l as usize;\n", i1)
	fmt.Fprintf(b, "%sif l >= 2 {\n", i1)
	i2 := i1 + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", i2)
	// THE HEADER IS READ AGAINST THE PARENT (docs/SPEC-TABLES.md §3; the
	// oracle's keyed): a count spelled past this body's L still reads, so the
	// element kind is judged, and the elements are bounded by the L.
	fmt.Fprintf(b, "%smatch leb_read(wire, off) {\n", i2)
	fmt.Fprintf(b, "%s    None => { report.malformed = true; }\n", i2)
	fmt.Fprintf(b, "%s    Some(count) => {\n", i2)
	i3 := i2 + "        "
	fmt.Fprintf(b, "%sif ek != %d && !table_kind_widens(ek, %d) { report.kind_mismatch += 1; }\n", i3, ek, ek)
	fmt.Fprintf(b, "%selse {\n", i3)
	i4 := i3 + "    "
	fmt.Fprintf(b, "%sif ek != %d { report.widened += 1; }\n", i4, ek)
	fmt.Fprintf(b, "%sfor _ in 0..count {\n", i4)
	i5 := i4 + "    "
	fmt.Fprintf(b, "%slet Some(kref) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
	fmt.Fprintf(b, "%sif kref == 0 || *off > sub || kref as usize > ids.len() { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet kid = ids[kref as usize - 1];\n", i5)
	fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
	fmt.Fprintf(b, "%sif *off > sub || el > (sub - *off) as u64 { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", i5)
	fmt.Fprintf(b, "%slet slot: Option<usize> = match kid {\n", i5)
	for i := range f.KeyEnumRef.Variants {
		fmt.Fprintf(b, "%s    0x%016x => Some(%d),\n", i5, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	fmt.Fprintf(b, "%s    _ => None,\n", i5)
	fmt.Fprintf(b, "%s};\n", i5)
	fmt.Fprintf(b, "%slet Some(slot) = slot else {\n", i5)
	fmt.Fprintf(b, "%s    report.unknown += 1;\n", i5)
	fmt.Fprintf(b, "%s    if retain.is_some() { report.retain_lost += 1; }\n", i5)
	fmt.Fprintf(b, "%s    *off = eend;\n", i5)
	fmt.Fprintf(b, "%s    continue;\n", i5)
	fmt.Fprintf(b, "%s};\n", i5)
	switch classOf(f) {
	case vTable:
		// A SLOT WRITTEN AGAIN replaces its body, and the records retained
		// under the body it loses go with it (docs/SPEC-TABLES.md §6.6; the
		// oracle's keyed forgets the slot's instance before refilling it).
		fmt.Fprintf(b, "%sif let Some(ret) = retain.as_deref_mut() { retain_discard_body(ret, path.step(%d, slot as u32)); }\n", i5, ordinal)
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, slot as u32), retain.as_deref_mut(), true);\n", i5, fn(f.Type.Name, "load_body"), ordinal)
		fmt.Fprintf(b, "%sif o != eend { report.malformed = true; e = %s::default(); }\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%s%s[slot] = e;\n", i5, field)
	case vPointer:
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%sif let Some(index) = leb_read(wire, &mut o) {\n", i5)
		fmt.Fprintf(b, "%s    if o <= eend { let e; ", i5)
		emitPointerResolve(b, f, "e", "")
		fmt.Fprintf(b, "%s[slot] = e; } else { report.malformed = true; }\n", field)
		fmt.Fprintf(b, "%s} else { report.malformed = true; }\n", i5)
	case vUnion:
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", i5, f.Type.Name)
		fmt.Fprintf(b, "%s{ let off = &mut o;\n", i5)
		g.withArmIndex("slot as u32", func() { g.emitUnionDecode(b, f, "e", "eend", ordinal, i5+"    ", true) })
		fmt.Fprintf(b, "%s}\n", i5)
		fmt.Fprintf(b, "%s%s[slot] = e;\n", i5, field)
	case vEnum:
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%slet mut e = %s[slot];\n", i5, field) // a table enum is Copy
		fmt.Fprintf(b, "%s{ let off = &mut o; 'enum_decode: {\n", i5)
		emitEnumDecode(b, f, "e", "eend", "break 'enum_decode;", i5+"    ")
		fmt.Fprintf(b, "%s} }\n", i5)
		fmt.Fprintf(b, "%s%s[slot] = e;\n", i5, field)
	default:
		fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
		fmt.Fprintf(b, "%sif let Some(raw) = %s(wire, &mut o, eend, ek) {\n", i5, scalarReadFn(f))
		emitScalarConvert(b, f, g.floatOwner(f), "let e", "ek", i5+"    ")
		fmt.Fprintf(b, "%s    %s[slot] = e;\n", i5, field)
		fmt.Fprintf(b, "%s} else { report.malformed = true; }\n", i5)
	}
	fmt.Fprintf(b, "%s*off = eend;\n", i5)
	fmt.Fprintf(b, "%s}\n", i4) // for
	fmt.Fprintf(b, "%s}\n", i3) // else ek
	fmt.Fprintf(b, "%s}\n", i2) // Some(count)
	fmt.Fprintf(b, "%s}\n", i2) // match
	fmt.Fprintf(b, "%s}\n", i1) // if l >= 2
	fmt.Fprintf(b, "%s*off = sub;\n", i1)
	fmt.Fprintf(b, "%s}\n", in)
}

// emitMapLoad reads a map as an array of entry tables, scanning each entry's
// key first, keeping an ascending prefix and counting a duplicate
// (docs/SPEC-TABLES.md §2.8, §4).
func (g *gen) emitMapLoad(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	key := ir.RustSnake(ir.MapKeyFieldName)
	keyField := ir.MapKeyField(f)
	keyKind := fieldWireKind(keyField)
	fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindArray, kindSkip)
	fmt.Fprintf(b, "%selse {\n", in)
	i1 := in + "    "
	fmt.Fprintf(b, "%slet Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", i1)
	fmt.Fprintf(b, "%sif *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", i1)
	fmt.Fprintf(b, "%slet sub = *off + l as usize;\n", i1)
	fmt.Fprintf(b, "%sif l >= 2 {\n", i1)
	i2 := i1 + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", i2)
	// THE HEADER IS READ AGAINST THE PARENT (docs/SPEC-TABLES.md §3; the
	// oracle's mapField): a count spelled past this body's L still reads, so the
	// element kind is judged, and the elements are bounded by the L.
	fmt.Fprintf(b, "%smatch leb_read(wire, off) {\n", i2)
	fmt.Fprintf(b, "%s    None => { report.malformed = true; }\n", i2)
	fmt.Fprintf(b, "%s    Some(count) => {\n", i2)
	i3 := i2 + "        "
	fmt.Fprintf(b, "%sif ek != %d { report.kind_mismatch += 1; }\n", i3, ir.TableKindTable)
	fmt.Fprintf(b, "%selse if count > 0x7fffffff { report.malformed = true; }\n", i3)
	fmt.Fprintf(b, "%selse {\n", i3)
	i4 := i3 + "    "
	fmt.Fprintf(b, "%sif let Some(ret) = retain.as_deref_mut() { retain_discard(ret, path, %d); }\n", i4, ordinal)
	fmt.Fprintf(b, "%s%s.clear();\n", i4, field)
	fmt.Fprintf(b, "%slet mut landed = false;\n", i4)
	fmt.Fprintf(b, "%slet mut widened = false;\n", i4)
	fmt.Fprintf(b, "%sfor _ in 0..count {\n", i4)
	i5 := i4 + "    "
	fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
	fmt.Fprintf(b, "%sif *off > sub || el > (sub - *off) as u64 { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", i5)
	fmt.Fprintf(b, "%slet mut key_val = %s;\n", i5, fieldDefault(keyField))
	fmt.Fprintf(b, "%slet mut kind_bad = false;\n", i5)
	fmt.Fprintf(b, "%slet mut entry_widened = false;\n", i5)
	fmt.Fprintf(b, "%slet mut key_over = false;\n", i5)
	fmt.Fprintf(b, "%slet mut scan_ok = false;\n", i5)
	fmt.Fprintf(b, "%slet mut scan_off = *off;\n", i5)
	fmt.Fprintf(b, "%swhile let Some(kref) = leb_read(wire, &mut scan_off) {\n", i5)
	i6 := i5 + "    "
	fmt.Fprintf(b, "%sif kref == 0 { scan_ok = scan_off == eend; break; }\n", i6)
	fmt.Fprintf(b, "%sif kref as usize > ids.len() || scan_off >= eend { break; }\n", i6)
	fmt.Fprintf(b, "%slet kid = ids[kref as usize - 1];\n", i6)
	fmt.Fprintf(b, "%slet kkind = wire[scan_off]; scan_off += 1;\n", i6)
	fmt.Fprintf(b, "%sif kid == 0x%016x {\n", i6, ir.MapKeyWireId)
	i7 := i6 + "    "
	if keyField.Type.Kind == ir.TString {
		fmt.Fprintf(b, "%skind_bad = kkind != 12;\n", i7)
		fmt.Fprintf(b, "%sif kind_bad {\n", i7)
		fmt.Fprintf(b, "%s    if !leb_skip(wire, &mut scan_off, eend, kkind) { break; }\n", i7)
		fmt.Fprintf(b, "%s    continue;\n", i7)
		fmt.Fprintf(b, "%s}\n", i7)
		fmt.Fprintf(b, "%slet Some(kl) = leb_read(wire, &mut scan_off) else { break; };\n", i7)
		fmt.Fprintf(b, "%sif scan_off > eend || kl > (eend - scan_off) as u64 { break; }\n", i7)
		fmt.Fprintf(b, "%slet ks = scan_off + kl as usize;\n", i7)
		fmt.Fprintf(b, "%slet kb = &wire[scan_off..ks];\n", i7)
		fmt.Fprintf(b, "%sscan_off = ks;\n", i7)
		fmt.Fprintf(b, "%sif !text_valid(kb) { break; }\n", i7)
		fmt.Fprintf(b, "%skey_over = kb.len() > %d;\n", i7, keyField.Type.Size)
		fmt.Fprintf(b, "%skey_val = std::str::from_utf8(kb).unwrap().to_string();\n", i7)
		fmt.Fprintf(b, "%scontinue;\n", i7)
	} else {
		castExpr := rustCastStmt("raw", "u64", scalarRust(keyField))
		if keyField.Type.Signed {
			if keyField.Type.Width == 128 {
				castExpr = "raw as i128"
			} else {
				castExpr = fmt.Sprintf("(raw as i64) as %s", scalarRust(keyField))
			}
		}
		fmt.Fprintf(b, "%sif kkind != %d && table_kind_widens(kkind, %d) {\n", i7, keyKind, keyKind)
		fmt.Fprintf(b, "%s    entry_widened = true;\n", i7)
		fmt.Fprintf(b, "%s    kind_bad = false;\n", i7)
		fmt.Fprintf(b, "%s    let Some(raw) = %s(wire, &mut scan_off, eend, kkind) else { break; };\n", i7, scalarReadFn(keyField))
		fmt.Fprintf(b, "%s    key_val = %s;\n", i7, castExpr)
		fmt.Fprintf(b, "%s    continue;\n", i7)
		fmt.Fprintf(b, "%s}\n", i7)
		fmt.Fprintf(b, "%skind_bad = kkind != %d;\n", i7, keyKind)
		fmt.Fprintf(b, "%sif kind_bad {\n", i7)
		fmt.Fprintf(b, "%s    if !leb_skip(wire, &mut scan_off, eend, kkind) { break; }\n", i7)
		fmt.Fprintf(b, "%s    continue;\n", i7)
		fmt.Fprintf(b, "%s}\n", i7)
		fmt.Fprintf(b, "%slet Some(raw) = %s(wire, &mut scan_off, eend, kkind) else { break; };\n", i7, scalarReadFn(keyField))
		fmt.Fprintf(b, "%skey_val = %s;\n", i7, castExpr)
		fmt.Fprintf(b, "%scontinue;\n", i7)
	}
	fmt.Fprintf(b, "%s}\n", i6)
	fmt.Fprintf(b, "%sif !leb_skip(wire, &mut scan_off, eend, kkind) { break; }\n", i6)
	fmt.Fprintf(b, "%s}\n", i5) // loop
	fmt.Fprintf(b, "%sif entry_widened && !widened { widened = true; report.widened += 1; }\n", i5)
	fmt.Fprintf(b, "%sif kind_bad { report.kind_mismatch += 1; %s.clear(); break; }\n", i5, field)
	fmt.Fprintf(b, "%sif !scan_ok { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%sif key_over { report.clamped += 1; *off = eend; continue; }\n", i5)
	fmt.Fprintf(b, "%slet mut order = std::cmp::Ordering::Less;\n", i5)
	fmt.Fprintf(b, "%sif landed {\n", i5)
	fmt.Fprintf(b, "%s    order = %s;\n", i5, mapKeyOrd(f, field+"["+field+".len()-1]."+key, "key_val"))
	fmt.Fprintf(b, "%s}\n", i5)
	fmt.Fprintf(b, "%sif order == std::cmp::Ordering::Greater { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet mut e = %s::default();\n", i5, f.MapEntry.Name)
	fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
	fmt.Fprintf(b, "%slet slot_idx = if order == std::cmp::Ordering::Equal { %s.len() - 1 } else { %s.len() };\n", i5, field, field)
	fmt.Fprintf(b, "%sif order == std::cmp::Ordering::Equal && let Some(ret) = retain.as_deref_mut() { retain_discard_body(ret, path.step(%d, slot_idx as u32)); }\n", i5, ordinal)
	fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, slot_idx as u32), retain.as_deref_mut(), true);\n", i5, fn(f.MapEntry.Name, "load_body"), ordinal)
	fmt.Fprintf(b, "%s*off = eend;\n", i5)
	fmt.Fprintf(b, "%se.%s = key_val;\n", i5, key)
	fmt.Fprintf(b, "%sif order == std::cmp::Ordering::Equal {\n", i5)
	fmt.Fprintf(b, "%s    report.duplicate += 1;\n", i5)
	fmt.Fprintf(b, "%s    %s[slot_idx] = e;\n", i5, field)
	fmt.Fprintf(b, "%s    continue;\n", i5)
	fmt.Fprintf(b, "%s}\n", i5)
	fmt.Fprintf(b, "%s%s.push(e);\n", i5, field)
	fmt.Fprintf(b, "%slanded = true;\n", i5)
	fmt.Fprintf(b, "%s}\n", i4) // for
	fmt.Fprintf(b, "%s}\n", i3) // else ek
	fmt.Fprintf(b, "%s}\n", i2) // Some(count)
	fmt.Fprintf(b, "%s}\n", i2) // match
	fmt.Fprintf(b, "%s}\n", i1) // if l >= 2
	fmt.Fprintf(b, "%s*off = sub;\n", i1)
	fmt.Fprintf(b, "%s}\n", in) // else kind
}

// emitUnionLoad reads an arm header into the union enum (docs/SPEC-TABLES.md §2.6, §3).
func (g *gen) emitUnionLoad(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindUnion, kindSkip)
	fmt.Fprintf(b, "%selse {\n", in)
	fmt.Fprintf(b, "%s    if let Some(ret) = retain.as_deref_mut() { retain_discard(ret, path, %d); }\n", in, ordinal)
	g.emitUnionDecode(b, f, field, "end", ordinal, in+"    ", true)
	fmt.Fprintf(b, "%s}\n", in)
}

// emitUnionDecode reads one union payload. fieldFramed is false for an array
// element, whose end is the array body; a field's end is the enclosing body.
func (g *gen) emitUnionDecode(b *strings.Builder, f *ir.Field, field, bound string, ordinal int, ind string, assign bool) {
	un := unionOf(f)
	fmt.Fprintf(b, "%slet Some(aref) = leb_read(wire, off) else { report.malformed = true; return false; };\n", ind)
	fmt.Fprintf(b, "%sif *off > %s { report.malformed = true; return false; }\n", ind, bound)
	fmt.Fprintf(b, "%sif aref == 0 { %s = %s::None; }\n", ind, field, un.Name)
	fmt.Fprintf(b, "%selse if aref as usize > ids.len() { report.malformed = true; return false; }\n", ind)
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%slet aid = ids[aref as usize - 1];\n", in)
	fmt.Fprintf(b, "%sif *off >= %s { report.malformed = true; return false; }\n", in, bound)
	fmt.Fprintf(b, "%slet akind = wire[*off]; *off += 1;\n", in)
	fmt.Fprintf(b, "%slet Some(al) = leb_read(wire, off) else { report.malformed = true; return false; };\n", in)
	fmt.Fprintf(b, "%sif *off > %s || al > (%s - *off) as u64 { report.malformed = true; return false; }\n", in, bound, bound)
	fmt.Fprintf(b, "%slet aend = *off + al as usize;\n", in)
	fmt.Fprintf(b, "%smatch aid {\n", in)
	for _, v := range un.Variants {
		fmt.Fprintf(b, "%s    0x%016x => {\n", in, ir.TableWireId(v.WireName()))
		g.emitArmDecode(b, un, v, field, "aend", "akind", ordinal, in+"        ")
		fmt.Fprintf(b, "%s    }\n", in)
	}
	fmt.Fprintf(b, "%s    _ => { report.unknown += 1; if retain.is_some() { report.retain_lost += 1; } %s = %s::None; }\n", in, field, un.Name)
	fmt.Fprintf(b, "%s}\n", in)
	fmt.Fprintf(b, "%s*off = aend;\n", in)
	fmt.Fprintf(b, "%s}\n", ind)
	_ = assign
}

func (g *gen) emitArmDecode(b *strings.Builder, un *ir.Union, v ir.UnionVariant, field, aend, akind string, ordinal int, ind string) {
	want := armWireKind(v)
	if !v.Void() && !v.Body() && v.F.Array == ir.ArrayNone && classOf(v.F) == vScalar {
		f := v.F
		fmt.Fprintf(b, "%sif %s != %d && !table_kind_widens(%s, %d) { report.kind_mismatch += 1; %s = %s::None; }\n", ind, akind, want, akind, want, field, un.Name)
		fmt.Fprintf(b, "%selse if (%s - *off) != table_kind_width(%s) { report.malformed = true; %s = %s::None; }\n", ind, aend, akind, field, un.Name)
		fmt.Fprintf(b, "%selse {\n", ind)
		in := ind + "    "
		fmt.Fprintf(b, "%sif %s != %d { report.widened += 1; }\n", in, akind, want)
		fmt.Fprintf(b, "%slet Some(raw) = %s(wire, off, %s, %s) else { report.malformed = true; %s = %s::None; *off = %s; return false; };\n", in, scalarReadFn(f), aend, akind, field, un.Name, aend)
		emitScalarConvert(b, f, g.floatOwner(f), "let slot", akind, in)
		fmt.Fprintf(b, "%s%s = %s::%s(slot);\n", in, field, un.Name, rustVariant(v.Name))
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	fmt.Fprintf(b, "%sif %s != %d { report.kind_mismatch += 1; %s = %s::None; }\n", ind, akind, want, field, un.Name)
	if v.Void() {
		fmt.Fprintf(b, "%selse if *off != %s { report.malformed = true; %s = %s::None; }\n", ind, aend, field, un.Name)
		fmt.Fprintf(b, "%selse { %s = %s::%s; }\n", ind, field, un.Name, rustVariant(v.Name))
		return
	}
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	f := v.F
	switch {
	case v.Body():
		fmt.Fprintf(b, "%slet mut body_val = %s::default();\n", in, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", in)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, %s, ids, &mut body_val, nodes, report, %s, retain.as_deref_mut(), true);\n", in, fn(f.Type.Name, "load_body"), aend, g.armPath(ordinal))
		fmt.Fprintf(b, "%sif o != %s { report.malformed = true; %s = %s::None; }\n", in, aend, field, un.Name)
		fmt.Fprintf(b, "%selse { %s = %s::%s(body_val); }\n", in, field, un.Name, rustVariant(v.Name))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%sif let Some(index) = leb_read(wire, off) {\n", in)
		fmt.Fprintf(b, "%s    if *off != %s { report.malformed = true; %s = %s::None; }\n", in, aend, field, un.Name)
		fmt.Fprintf(b, "%s    else {\n", in)
		emitPointerResolve(b, f, "let slot", in+"    ")
		fmt.Fprintf(b, "%s    %s = %s::%s(slot);\n", in, field, un.Name, rustVariant(v.Name))
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s} else { report.malformed = true; %s = %s::None; }\n", in, field, un.Name)
	case classOf(f) == vUnion && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%slet mut inner = %s::None;\n", in, f.Type.Name)
		fmt.Fprintf(b, "%slet inner_ok = (|| -> bool {\n", in)
		g.emitUnionDecode(b, f, "inner", aend, ordinal, in+"    ", true)
		fmt.Fprintf(b, "%s    true\n%s})();\n", in, in)
		fmt.Fprintf(b, "%sif inner_ok { %s = %s::%s(inner); } else { %s = %s::None; }\n", in, field, un.Name, rustVariant(v.Name), field, un.Name)
	case f.Array == ir.ArrayNone && classOf(f) == vEnum:
		emitEnumDecode(b, f, "let slot", aend, "return false;", in)
		fmt.Fprintf(b, "%sif *off != %s { report.malformed = true; %s = %s::None; } else { %s = %s::%s(slot); }\n", in, aend, field, un.Name, field, un.Name, rustVariant(v.Name))
	case f.Array == ir.ArrayNone && classOf(f) == vScalar:
		// a scalar arm: the read either refuses (returning) or yields the value
		fmt.Fprintf(b, "%slet Some(raw) = %s(wire, off, %s, %s) else { report.malformed = true; %s = %s::None; *off = %s; return false; };\n", in, scalarReadFn(f), aend, akind, field, un.Name, aend)
		emitScalarConvert(b, f, g.floatOwner(f), "let slot", akind, in)
		fmt.Fprintf(b, "%s%s = %s::%s(slot);\n", in, field, un.Name, rustVariant(v.Name))
	default:
		fmt.Fprintf(b, "%slet mut slot = %s;\n", in, fieldDefault(f))
		fmt.Fprintf(b, "%slet mut ok = true;\n", in)
		switch {
		case f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
			g.emitArrayBodyReadChecked(b, f, "slot", aend, aend, "ok", ordinal, in, false)
		case classOf(f) == vEnum:
			emitEnumDecode(b, f, "slot", aend, "return false;", in)
			fmt.Fprintf(b, "%sif *off != %s { report.malformed = true; ok = false; }\n", in, aend)
		case classOf(f) == vWString:
			// an arm's wide text (docs/SPEC-TABLES.md §3, §4; the oracle's
			// decode.go arm case): an odd L or ill-formed units read the union
			// None and count malformed; over N keeps the longest prefix that
			// does not split a pair and counts one clamp
			fmt.Fprintf(b, "%smatch table_wide_units(&wire[*off..%s]) {\n", in, aend)
			fmt.Fprintf(b, "%s    None => { report.malformed = true; ok = false; }\n", in)
			fmt.Fprintf(b, "%s    Some(u) => { let keep = table_wide_boundary(&u, %d); if keep < u.len() { report.clamped += 1; } slot = u[..keep].to_vec(); }\n", in, f.Type.Size)
			fmt.Fprintf(b, "%s}\n", in)
		case classOf(f) == vString:
			fmt.Fprintf(b, "%slet bytes = &wire[*off..%s];\n", in, aend)
			fmt.Fprintf(b, "%sif !text_valid(bytes) { report.malformed = true; ok = false; }\n", in)
			fmt.Fprintf(b, "%selse if bytes.len() > %d { let mut cut = %d; while cut > 0 && bytes[cut] & 0xC0 == 0x80 { cut -= 1; } slot = std::str::from_utf8(&bytes[..cut]).unwrap().to_string(); report.clamped += 1; }\n", in, f.Type.Size, f.Type.Size)
			fmt.Fprintf(b, "%selse { slot = std::str::from_utf8(bytes).unwrap().to_string(); }\n", in)
		default:
			fmt.Fprintf(b, "%slet Some(raw) = %s(wire, off, %s, %s) else { report.malformed = true; %s = %s::None; *off = %s; return false; };\n", in, scalarReadFn(f), aend, akind, field, un.Name, aend)
			emitScalarConvert(b, f, g.floatOwner(f), "slot", akind, in)
		}
		fmt.Fprintf(b, "%sif ok { %s = %s::%s(slot); } else { %s = %s::None; }\n", in, field, un.Name, rustVariant(v.Name), field, un.Name)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitArrayBodyReadChecked reads an array body already framed by `aend`
// (docs/SPEC-TABLES.md §3): element kind, count, elements. Sets `okVar` to
// false when the element kind mismatches or the count is damaged.
//
// The header is read against `hdr`, the bound of the body that holds it: an
// arm's own `L`, because the oracle reads an arm out of a reader framed by that
// L, so a count running past it is the arm's own framing damage and nothing
// is counted before it; or `end` for an optional FIELD, whose header, like
// every field array's, reads against the parent (docs/SPEC-TABLES.md §3). The
// body's own leb_read is already bounded by `end`.
func (g *gen) emitArrayBodyReadChecked(b *strings.Builder, f *ir.Field, field, aend, hdr, okVar string, ordinal int, ind string, countDamagedPresent bool) {
	ek := ir.TableKindU8
	if f.Type.Kind != ir.TBytes || f.Type.Pointer {
		ek = elemWireKind(f)
	}
	fmt.Fprintf(b, "%sif (%s - *off) >= 2 {\n", ind, aend)
	in := ind + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", in)
	if hdr == "end" {
		fmt.Fprintf(b, "%smatch leb_read(wire, off) {\n", in)
	} else {
		fmt.Fprintf(b, "%smatch leb_read(wire, off).filter(|_| *off <= %s) {\n", in, hdr)
	}
	if countDamagedPresent {
		// A field's optional array has ridden even if its count is damaged:
		// §4 keeps the current value (or declared default) and marks it present.
		fmt.Fprintf(b, "%s    None => { report.malformed = true; }\n", in)
	} else {
		// An arm with a damaged count is not selected (§3).
		fmt.Fprintf(b, "%s    None => { report.malformed = true; %s = false; }\n", in, okVar)
	}
	fmt.Fprintf(b, "%s    Some(count) => {\n", in)
	i2 := in + "        "
	fmt.Fprintf(b, "%sif ek != %d && !table_kind_widens(ek, %d) { report.kind_mismatch += 1; %s = false; }\n", i2, ek, ek, okVar)
	fmt.Fprintf(b, "%selse {\n", i2)
	i3 := i2 + "    "
	fmt.Fprintf(b, "%sif ek != %d { report.widened += 1; }\n", i3, ek)
	bound := f.ArrayBound
	if f.Type.Kind == ir.TBytes && f.Array == ir.ArrayNone {
		bound = f.Type.Size
	}
	switch {
	case f.Array == ir.ArrayList:
		fmt.Fprintf(b, "%slet keep = if count > 0x7fffffff { report.malformed = true; 0 } else { count };\n", i3)
	case bound > 0:
		fmt.Fprintf(b, "%slet keep = if count > %d { report.clamped += 1; %d } else { count };\n", i3, bound, bound)
	default:
		fmt.Fprintf(b, "%slet keep = count;\n", i3)
	}
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%slet prior = %s.clone();\n", i3, field)
	}
	fmt.Fprintf(b, "%s%s.clear();\n", i3, field)
	fmt.Fprintf(b, "%sfor j in 0..keep {\n", i3)
	g.emitElementReadWithKind(b, f, field, aend, "ek", ordinal, i3+"    ")
	fmt.Fprintf(b, "%s}\n", i3)
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%swhile %s.len() < %d { %s.push(prior.get(%s.len()).cloned()%s); }\n", i3, field, f.ArrayBound, field, field, orElse(fieldDefault(scalarOnly(f))))
	}
	fmt.Fprintf(b, "%s}\n", i2)
	fmt.Fprintf(b, "%s    }\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	fmt.Fprintf(b, "%s}\n", ind)
}

func (g *gen) emitElementReadWithKind(b *strings.Builder, f *ir.Field, field, bound, ekVar string, ordinal int, ind string) {
	fmt.Fprintf(b, "%sif *off >= %s { report.malformed = true; break; }\n", ind, bound)
	switch classOf(f) {
	case vTable:
		fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
		fmt.Fprintf(b, "%sif *off > %s || el > (%s - *off) as u64 { report.malformed = true; break; };\n", ind, bound, bound)
		fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", ind)
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", ind, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", ind)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, j as u32), retain.as_deref_mut(), true);\n", ind, fn(f.Type.Name, "load_body"), ordinal)
		fmt.Fprintf(b, "%sif o != eend { report.malformed = true; e = %s::default(); }\n", ind, f.Type.Name)
		fmt.Fprintf(b, "%s*off = eend;\n", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vPointer:
		fmt.Fprintf(b, "%slet Some(index) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
		emitPointerResolve(b, f, "let e", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vBlob:
		fmt.Fprintf(b, "%slet Some(index) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
		emitPointerResolve(b, f, "let e", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vUnion:
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", ind, f.Type.Name)
		g.withArmIndex("j as u32", func() { g.emitUnionDecode(b, f, "e", bound, ordinal, ind, true) })
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vEnum:
		emitEnumDecode(b, f, "let e", bound, "break;", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vBytes:
		fmt.Fprintf(b, "%slet Some(raw) = table_scalar_read(wire, off, %s, %s) else { report.malformed = true; break; };\n", ind, bound, ekVar)
		fmt.Fprintf(b, "%s%s.push(raw as u8);\n", ind, field)
	default:
		fmt.Fprintf(b, "%slet Some(raw) = %s(wire, off, %s, %s) else { report.malformed = true; break; };\n", ind, scalarReadFn(f), bound, ekVar)
		emitScalarConvert(b, f, g.floatOwner(f), "let e", ekVar, ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	}
}

// emitKeyedTextWrite writes an enum-keyed array as a JSON object keyed by
// variant name (docs/SPEC-TABLES.md §2.4, §16.2).
func (g *gen) emitKeyedTextWrite(b *strings.Builder, f *ir.Field, x, d, ind string) {
	if f.KeyEnumRef == nil || len(f.KeyEnumRef.Variants) == 0 {
		fmt.Fprintf(b, "%sw.out.push_str(\"{}\");\n", ind)
		return
	}
	elemField := scalarOnly(f)
	defExpr := fieldDefault(elemField)
	fmt.Fprintf(b, "%sw.out.push('{');\n", ind)
	for i, v := range f.KeyEnumRef.Variants {
		if i > 0 {
			fmt.Fprintf(b, "%sw.out.push(',');\n", ind)
		}
		fmt.Fprintf(b, "%sjson_newline(&mut w.out, %s + 1);\n", ind, d)
		fmt.Fprintf(b, "%sjson_escape(%q, &mut w.out);\n", ind, v)
		fmt.Fprintf(b, "%sw.out.push_str(\": \");\n", ind)
		fmt.Fprintf(b, "%s{\n", ind)
		clone := ".clone()"
		if c := classOf(elemField); c == vScalar || c == vEnum {
			clone = "" // Copy: indexing copies (clippy's clone_on_copy)
		}
		has := fmt.Sprintf("%s.len() > %d", x, i)
		if i == 0 {
			has = "!" + x + ".is_empty()"
		}
		fmt.Fprintf(b, "%s    let slot_val = if %s { %s[%d]%s } else { %s };\n", ind, has, x, i, clone, defExpr)
		g.emitTextWriteOne(b, elemField, "slot_val", d+" + 1", ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
	}
	fmt.Fprintf(b, "%sjson_newline(&mut w.out, %s);\n", ind, d)
	fmt.Fprintf(b, "%sw.out.push('}');\n", ind)
}

// emitKeyedTextRead reads a JSON object keyed by variant name into an
// enum-keyed array (docs/SPEC-TABLES.md §2.4, §16.2).
func (g *gen) emitKeyedTextRead(b *strings.Builder, f *ir.Field, field, ind string) {
	elemField := scalarOnly(f)
	fmt.Fprintf(b, "%sif jr.peek() != b'{' { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind)
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%sjr.p += 1;\n", in)
	fmt.Fprintf(b, "%s%s = %s;\n", in, field, fieldDefault(f))
	fmt.Fprintf(b, "%slet mut seen = [false; %d];\n", in, len(f.KeyEnumRef.Variants))
	fmt.Fprintf(b, "%sloop {\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() == b'}' { jr.p += 1; break; }\n", in)
	fmt.Fprintf(b, "%s    let Some(ks) = jr.read_string() else { jr.malformed = true; break; };\n", in)
	fmt.Fprintf(b, "%s    jr.space();\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() != b':' { jr.malformed = true; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.p += 1;\n", in)
	fmt.Fprintf(b, "%s    match ks.as_str() {\n", in)
	for i, v := range f.KeyEnumRef.Variants {
		fmt.Fprintf(b, "%s        %q => {\n", in, v)
		fmt.Fprintf(b, "%s            if seen[%d] { jr.duplicate += 1; }\n", in, i)
		fmt.Fprintf(b, "%s            seen[%d] = true;\n", in, i)
		g.emitTextReadValue(b, elemField, fmt.Sprintf("%s[%d]", field, i), in+"            ")
		fmt.Fprintf(b, "%s        }\n", in)
	}
	fmt.Fprintf(b, "%s        _ => {\n", in)
	fmt.Fprintf(b, "%s            if ks.starts_with('&') { jr.malformed = true; break; }\n", in)
	fmt.Fprintf(b, "%s            jr.unknown += 1;\n", in)
	fmt.Fprintf(b, "%s            jr.skip_value();\n", in)
	fmt.Fprintf(b, "%s        }\n", in)
	fmt.Fprintf(b, "%s    }\n", in)
	fmt.Fprintf(b, "%s    if jr.malformed { break; }\n", in)
	fmt.Fprintf(b, "%s    let c = jr.peek();\n", in)
	fmt.Fprintf(b, "%s    if c == b',' { jr.p += 1; continue; }\n", in)
	fmt.Fprintf(b, "%s    if c == b'}' { jr.p += 1; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.malformed = true; break;\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitUnionText emits <union>_json_count, <union>_json_write and <union>_json_read
// for each union an emitted table reaches (docs/SPEC-TABLES.md §2.6, §16.2).
func (g *gen) emitUnionText(b *strings.Builder, names []string) {
	for _, un := range g.collectVariableUnions(names) {
		fmt.Fprintf(b, "#[allow(unused_variables, dead_code)]\n")
		fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut Jw) {\n", fn(un.Name, "json_count"), un.Name)
		fmt.Fprintf(b, "    match v {\n")
		fmt.Fprintf(b, "        %s::None => {}\n", un.Name)
		for _, v := range un.Variants {
			if v.Void() || v.F == nil {
				fmt.Fprintf(b, "        %s::%s => {}\n", un.Name, rustVariant(v.Name))
				continue
			}
			fmt.Fprintf(b, "        %s::%s(arm) => {\n", un.Name, rustVariant(v.Name))
			g.emitFieldTextCount(b, v.F, "(*arm)", "            ")
			fmt.Fprintf(b, "        }\n")
		}
		fmt.Fprintf(b, "    }\n}\n\n")

		fmt.Fprintf(b, "#[allow(unused_variables, dead_code)]\n")
		fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut Jw, depth: usize) {\n", fn(un.Name, "json_write"), un.Name)
		fmt.Fprintf(b, "    if depth > JSON_DEPTH_CAP { w.refused = true; }\n")
		fmt.Fprintf(b, "    match v {\n")
		fmt.Fprintf(b, "        %s::None => w.out.push_str(\"{}\"),\n", un.Name)
		for _, v := range un.Variants {
			if v.Void() {
				fmt.Fprintf(b, "        %s::%s => {\n", un.Name, rustVariant(v.Name))
			} else {
				fmt.Fprintf(b, "        %s::%s(arm) => {\n", un.Name, rustVariant(v.Name))
			}
			fmt.Fprintf(b, "            w.out.push('{');\n")
			fmt.Fprintf(b, "            json_newline(&mut w.out, depth + 1);\n")
			fmt.Fprintf(b, "            json_escape(%q, &mut w.out);\n", v.Name)
			fmt.Fprintf(b, "            w.out.push_str(\": \");\n")
			if v.Void() {
				fmt.Fprintf(b, "            w.out.push_str(\"null\");\n")
			} else {
				g.emitTextWriteValue(b, v.F, "(*arm)", "depth + 1", "            ")
			}
			fmt.Fprintf(b, "            json_newline(&mut w.out, depth);\n")
			fmt.Fprintf(b, "            w.out.push('}');\n")
			fmt.Fprintf(b, "        }\n")
		}
		fmt.Fprintf(b, "    }\n}\n\n")

		fmt.Fprintf(b, "#[allow(unused_variables, unused_mut, unused_assignments, dead_code)]\n")
		fmt.Fprintf(b, "pub fn %s(jr: &mut Jr, v: &mut %s) {\n", fn(un.Name, "json_read"), un.Name)
		fmt.Fprintf(b, "    if jr.peek() != b'{' { jr.kind_mismatch += 1; jr.skip_value(); return; }\n")
		fmt.Fprintf(b, "    jr.p += 1;\n")
		fmt.Fprintf(b, "    *v = %s::None;\n", un.Name)
		fmt.Fprintf(b, "    if jr.peek() == b'}' { jr.p += 1; return; }\n")
		fmt.Fprintf(b, "    let Some(key) = jr.read_string() else { jr.malformed = true; return; };\n")
		fmt.Fprintf(b, "    if jr.peek() != b':' { jr.malformed = true; return; }\n")
		fmt.Fprintf(b, "    jr.p += 1;\n")
		fmt.Fprintf(b, "    match key.as_str() {\n")
		for _, v := range un.Variants {
			fmt.Fprintf(b, "        %q => {\n", v.Name)
			switch {
			case v.Void():
				fmt.Fprintf(b, "            if jr.peek() == b'n' {\n")
				fmt.Fprintf(b, "                if jr.read_null() { *v = %s::%s; }\n", un.Name, rustVariant(v.Name))
				fmt.Fprintf(b, "            } else { jr.kind_mismatch += 1; jr.skip_value(); }\n")
			case v.F.Array != ir.ArrayNone:
				fmt.Fprintf(b, "            if jr.peek() != b'[' { jr.kind_mismatch += 1; jr.skip_value(); }\n")
				fmt.Fprintf(b, "            else {\n")
				fmt.Fprintf(b, "                let mut arm = %s;\n", fieldDefault(v.F))
				g.emitTextReadValue(b, v.F, "arm", "                ")
				fmt.Fprintf(b, "                if !jr.malformed { *v = %s::%s(arm); }\n", un.Name, rustVariant(v.Name))
				fmt.Fprintf(b, "            }\n")
			case classOf(v.F) == vTable || classOf(v.F) == vMap || classOf(v.F) == vUnion:
				fmt.Fprintf(b, "            if jr.peek() != b'{' { jr.kind_mismatch += 1; jr.skip_value(); }\n")
				fmt.Fprintf(b, "            else {\n")
				fmt.Fprintf(b, "                let mut arm = %s;\n", fieldDefault(v.F))
				g.emitTextReadValue(b, v.F, "arm", "                ")
				fmt.Fprintf(b, "                if !jr.malformed { *v = %s::%s(arm); }\n", un.Name, rustVariant(v.Name))
				fmt.Fprintf(b, "            }\n")
			default:
				fmt.Fprintf(b, "            let before_km = jr.kind_mismatch;\n")
				fmt.Fprintf(b, "            let mut arm = %s;\n", fieldDefault(v.F))
				g.emitTextReadValue(b, v.F, "arm", "            ")
				fmt.Fprintf(b, "            if !jr.malformed && jr.kind_mismatch == before_km {\n")
				fmt.Fprintf(b, "                *v = %s::%s(arm);\n", un.Name, rustVariant(v.Name))
				fmt.Fprintf(b, "            }\n")
			}
			fmt.Fprintf(b, "        }\n")
		}
		fmt.Fprintf(b, "        _ => {\n")
		fmt.Fprintf(b, "            if key.starts_with('&') { jr.malformed = true; return; }\n")
		fmt.Fprintf(b, "            jr.unknown += 1;\n")
		fmt.Fprintf(b, "            jr.skip_value();\n")
		fmt.Fprintf(b, "        }\n")
		fmt.Fprintf(b, "    }\n")
		fmt.Fprintf(b, "    if jr.malformed { return; }\n")
		fmt.Fprintf(b, "    let c = jr.peek();\n")
		fmt.Fprintf(b, "    if c == b',' {\n")
		fmt.Fprintf(b, "        jr.p += 1;\n")
		fmt.Fprintf(b, "        if jr.peek() == b'}' { jr.p += 1; return; }\n")
		fmt.Fprintf(b, "        jr.malformed = true; *v = %s::None; return;\n", un.Name)
		fmt.Fprintf(b, "    }\n")
		fmt.Fprintf(b, "    if c == b'}' { jr.p += 1; return; }\n")
		fmt.Fprintf(b, "    jr.malformed = true; *v = %s::None;\n", un.Name)
		fmt.Fprintf(b, "}\n\n")
	}
}

// emitMapTextWrite writes a map as a JSON object keyed by the entry key, in
// ascending key order (docs/SPEC-TABLES.md §2.8, §16.2).
func (g *gen) emitMapTextWrite(b *strings.Builder, f *ir.Field, x, d, ind string) {
	key := ir.RustSnake(ir.MapKeyFieldName)
	val := ir.MapValueField(f)
	fmt.Fprintf(b, "%sif %s.is_empty() {\n", ind, x)
	fmt.Fprintf(b, "%s    w.out.push_str(\"{}\");\n", ind)
	fmt.Fprintf(b, "%s} else {\n", ind)
	fmt.Fprintf(b, "%s    let map_val = &%s;\n", ind, x)
	fmt.Fprintf(b, "%s    let mut order: Vec<usize> = (0..map_val.len()).collect();\n", ind)
	fmt.Fprintf(b, "%s    order.sort_by(|&a, &b| %s);\n", ind, mapKeyOrd(f, "map_val[a]."+key, "map_val[b]."+key))
	fmt.Fprintf(b, "%s    w.out.push('{');\n", ind)
	fmt.Fprintf(b, "%s    for (i, &at) in order.iter().enumerate() {\n", ind)
	fmt.Fprintf(b, "%s        let ent = &map_val[at];\n", ind)
	fmt.Fprintf(b, "%s        if i > 0 { w.out.push(','); }\n", ind)
	fmt.Fprintf(b, "%s        json_newline(&mut w.out, %s + 1);\n", ind, d)
	if ir.MapKeyField(f).Type.Kind == ir.TString {
		fmt.Fprintf(b, "%s        json_escape(&ent.%s, &mut w.out);\n", ind, key)
	} else {
		fmt.Fprintf(b, "%s        json_escape(&ent.%s.to_string(), &mut w.out);\n", ind, key)
	}
	fmt.Fprintf(b, "%s        w.out.push_str(\": \");\n", ind)
	g.emitTextWriteValue(b, val, "ent."+ir.RustSnake(ir.MapValueFieldName), d+" + 1", ind+"        ")
	fmt.Fprintf(b, "%s    }\n", ind)
	fmt.Fprintf(b, "%s    json_newline(&mut w.out, %s);\n", ind, d)
	fmt.Fprintf(b, "%s    w.out.push('}');\n", ind)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitMapTextRead reads a JSON object into sorted entries (docs/SPEC-TABLES.md §16.2, §2.8).
func (g *gen) emitMapTextRead(b *strings.Builder, f *ir.Field, field, ind string) {
	key := ir.MapKeyField(f)
	val := ir.MapValueField(f)
	keyName := ir.RustSnake(ir.MapKeyFieldName)
	valName := ir.RustSnake(ir.MapValueFieldName)
	ent := "ent_" + ir.RustSnake(f.MapEntry.Name)
	fmt.Fprintf(b, "%sif jr.peek() != b'{' { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind)
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%sjr.p += 1;\n", in)
	fmt.Fprintf(b, "%s%s.clear();\n", in, field)
	fmt.Fprintf(b, "%sloop {\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() == b'}' { jr.p += 1; break; }\n", in)
	// a map key is bound for storage whatever its kind, so a U+0000 in it
	// refuses the read even when the key is then dropped (§16.3)
	fmt.Fprintf(b, "%s    let Some(ks) = jr.read_text() else { jr.malformed = true; break; };\n", in)
	fmt.Fprintf(b, "%s    jr.space();\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() != b':' { jr.malformed = true; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.p += 1;\n", in)
	fmt.Fprintf(b, "%s    let mut %s = %s::default();\n", in, ent, f.MapEntry.Name)
	if key.Type.Kind == ir.TString {
		fmt.Fprintf(b, "%s    if ks.len() > %d {\n", in, key.Type.Size)
		fmt.Fprintf(b, "%s        jr.clamped += 1; jr.skip_value();\n", in)
		fmt.Fprintf(b, "%s        let c = jr.peek(); if c == b',' { jr.p += 1; continue; } else if c == b'}' { jr.p += 1; break; } jr.malformed = true; break;\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s    %s.%s = ks;\n", in, ent, keyName)
	} else {
		fmt.Fprintf(b, "%s    match ks.parse::<%s>() {\n", in, scalarRust(key))
		fmt.Fprintf(b, "%s        Ok(n) => %s.%s = n,\n", in, ent, keyName)
		fmt.Fprintf(b, "%s        Err(_) => { jr.kind_mismatch += 1; jr.skip_value(); let c = jr.peek(); if c == b',' { jr.p += 1; continue; } else if c == b'}' { jr.p += 1; break; } jr.malformed = true; break; }\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
	}
	g.emitTextReadValue(b, val, ent+"."+valName, in+"    ")
	fmt.Fprintf(b, "%s    if jr.malformed { break; }\n", in)
	fmt.Fprintf(b, "%s    if let Some(pos) = %s.iter().position(|x| x.%s == %s.%s) { jr.duplicate += 1; %s[pos] = %s; } else { %s.push(%s); }\n", in, field, keyName, ent, keyName, field, ent, field, ent)
	fmt.Fprintf(b, "%s    let c = jr.peek();\n", in)
	fmt.Fprintf(b, "%s    if c == b',' { jr.p += 1; continue; }\n", in)
	fmt.Fprintf(b, "%s    if c == b'}' { jr.p += 1; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.malformed = true; break;\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	if c := classOf(ir.MapKeyField(f)); c == vScalar || c == vEnum {
		// a Copy key sorts by the key itself (clippy's unnecessary_sort_by)
		fmt.Fprintf(b, "%s%s.sort_by_key(|e| e.%s);\n", in, field, keyName)
	} else {
		fmt.Fprintf(b, "%s%s.sort_by(|a, b| %s);\n", in, field, mapKeyOrd(f, "a."+keyName, "b."+keyName))
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// cheapDefault is a default expression with nothing to compute: a literal or
// a path, which an Option fills eagerly (clippy's unnecessary_lazy_evaluations).
var cheapDefault = regexp.MustCompile(`^(-?[0-9][0-9a-z_.]*|true|false|None|[A-Z][A-Za-z0-9_]*(::[A-Za-z0-9_]+)*)$`)

// typeDefault is a bare `T::default()`, the same value unwrap_or_default gives.
var typeDefault = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*::default\(\)$`)

// orElse renders the Option fallback for a declared default: the type's own
// Default, the value eagerly when it is cheap, a closure otherwise.
func orElse(def string) string {
	switch {
	case def == "Vec::new()" || def == "String::new()" || typeDefault.MatchString(def):
		return ".unwrap_or_default()"
	case cheapDefault.MatchString(def):
		return ".unwrap_or(" + def + ")"
	}
	return ".unwrap_or_else(|| " + def + ")"
}
