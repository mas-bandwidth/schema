package rusttable

// Maps and union arms on the variable wire (docs/SPEC-TABLES.md §2.6, §2.8, §3).
// A map is an array of one generated { key, value } entry, written in ascending
// key order and never elided as an element. A union is an arm header: the arm
// id reference, the arm's kind byte, L, then L bytes of the arm's field type.
// None elides as a field and rides as the zero reference as an array element.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func unionOf(f *ir.Field) *ir.Union {
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

func (g *gen) usesUnion(f *ir.Field) bool {
	return classOf(f) == vUnion
}

// emitUnionTypes emits each union an emitted table reaches, after the structs
// its arms name by value and before the structs that hold the union
// (docs/SPEC-TABLES.md §2.6).
func (g *gen) emitUnionTypes(b *strings.Builder, names []string) {
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
				if inner := unionOf(v.F); inner != nil {
					walk(inner)
				}
			}
		}
		order = append(order, un)
	}
	for _, n := range names {
		for _, f := range g.unit.Tables[n].Fields {
			g.walkFieldUnions(f, walk)
		}
	}
	for _, un := range order {
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
	fmt.Fprintf(b, "#[derive(Clone, Debug)]\n")
	fmt.Fprintf(b, "pub enum %s {\n", un.Name)
	fmt.Fprintf(b, "    None,\n")
	for _, v := range un.Variants {
		if v.Void() {
			fmt.Fprintf(b, "    %s,\n", rustVariant(v.Name))
			continue
		}
		fmt.Fprintf(b, "    %s(%s),\n", rustVariant(v.Name), elemRust(v.F))
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(b, "impl Default for %s {\n    fn default() -> Self { %s::None }\n}\n\n", un.Name, un.Name)
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
	un := unionOf(f)
	fmt.Fprintf(b, "    match &%s {\n", x)
	for _, v := range un.Variants {
		if v.Void() || v.F == nil {
			continue
		}
		arm := rustVariant(v.Name)
		switch classOf(v.F) {
		case vBlob:
			bytes := "b.as_bytes()"
			if v.F.Type.Kind == ir.TBytes {
				bytes = "&b[..]"
			}
			fmt.Fprintf(b, "        %s::%s(b) => { if let Some(b) = b { g.visit_blob(%s, Rc::as_ptr(b) as *const u8 as usize, %s); } }\n", un.Name, arm, blobTypeConst(v.F), bytes)
		case vPointer:
			fmt.Fprintf(b, "        %s::%s(n) => { if let Some(n) = n { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } } }\n", un.Name, arm, ir.TableWireId(v.F.Type.Ref.(*ir.Struct).WireName()), fn(v.F.Type.Name, "number"))
		case vTable:
			fmt.Fprintf(b, "        %s::%s(e) => { %s(e, g); }\n", un.Name, arm, fn(v.F.Type.Name, "number"))
		case vMap:
			fmt.Fprintf(b, "        %s::%s(e) => { for ent in e.iter() { %s(ent, g); } }\n", un.Name, arm, fn(v.F.MapEntry.Name, "number"))
		case vUnion:
			fmt.Fprintf(b, "        %s::%s(e) => { %s }\n", un.Name, arm, unionNumberInline(v.F, "e"))
		default:
			if v.F.Array != ir.ArrayNone && classOf(v.F) == vPointer {
				fmt.Fprintf(b, "        %s::%s(es) => { for e in es.iter() { if let Some(n) = e { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } } } }\n", un.Name, arm, ir.TableWireId(v.F.Type.Ref.(*ir.Struct).WireName()), fn(v.F.Type.Name, "number"))
			} else if v.F.Array != ir.ArrayNone && classOf(v.F) == vTable {
				fmt.Fprintf(b, "        %s::%s(es) => { for e in es.iter() { %s(e, g); } }\n", un.Name, arm, fn(v.F.Type.Name, "number"))
			} else if v.F.Array != ir.ArrayNone && classOf(v.F) == vUnion {
				fmt.Fprintf(b, "        %s::%s(es) => { for e in es.iter() { %s } }\n", un.Name, arm, unionNumberInline(v.F, "e"))
			} else {
				fmt.Fprintf(b, "        %s::%s(_) => {}\n", un.Name, arm)
			}
		}
	}
	fmt.Fprintf(b, "        _ => {}\n")
	fmt.Fprintf(b, "    }\n")
}

func unionNumberInline(f *ir.Field, x string) string {
	var b strings.Builder
	// a nested union's numbering is the same match, spelled at the call
	un := unionOf(f)
	fmt.Fprintf(&b, "match &%s {\n", x)
	for _, v := range un.Variants {
		if v.F == nil || v.Void() {
			continue
		}
		if classOf(v.F) == vPointer {
			fmt.Fprintf(&b, "            %s::%s(n) => { if let Some(n) = n { if g.visit_table(0x%016x, n.clone()) { %s(n, g); } } }\n", un.Name, rustVariant(v.Name), ir.TableWireId(v.F.Type.Ref.(*ir.Struct).WireName()), fn(v.F.Type.Name, "number"))
		} else if classOf(v.F) == vTable {
			fmt.Fprintf(&b, "            %s::%s(e) => { %s(e, g); }\n", un.Name, rustVariant(v.Name), fn(v.F.Type.Name, "number"))
		} else if classOf(v.F) == vBlob {
			bytes := "b.as_bytes()"
			if v.F.Type.Kind == ir.TBytes {
				bytes = "&b[..]"
			}
			fmt.Fprintf(&b, "            %s::%s(b) => { if let Some(b) = b { g.visit_blob(%s, Rc::as_ptr(b) as *const u8 as usize, %s); } }\n", un.Name, rustVariant(v.Name), blobTypeConst(v.F), bytes)
		}
	}
	fmt.Fprintf(&b, "            _ => {}\n        }")
	return b.String()
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
	fmt.Fprintf(b, "%sif let (Some(ret), Some(rep)) = (retain.as_deref_mut(), report.as_deref_mut()) {\n", ein)
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
	fmt.Fprintf(b, "%smatch &%s {\n", ind, x)
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
		fmt.Fprintf(b, "%s%s(%s, &mut payload, ids, g, retain.as_deref_mut(), report.as_deref_mut(), path.step(%d, 0));\n", ind, fn(f.Type.Name, "save_body"), arm, ordinal)
		fmt.Fprintf(b, "%sif let (Some(ret), Some(rep)) = (retain.as_deref_mut(), report.as_deref_mut()) {\n", ind)
		fmt.Fprintf(b, "%s    retain_emit_tail(&mut payload, ids, ret, KNOWN_IDS, path.step(%d, 0), rep);\n", ind, ordinal)
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
	case classOf(f) == vUnion && f.Array == ir.ArrayNone:
		g.emitUnionElement(b, f, arm, "payload", ordinal, ind)
	case f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
		g.emitArrayBodyWrite(b, f, arm, "payload", ordinal, ind)
	case classOf(f) == vEnum:
		fmt.Fprintf(b, "%slet vref = %s;\n", ind, enumRef(f, "*"+arm))
		fmt.Fprintf(b, "%sleb_write(&mut payload, vref);\n", ind)
	default:
		fmt.Fprintf(b, "%s%s\n", ind, scalarWrite(f, "*"+arm, "payload"))
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
	if f.Array == ir.ArrayFixed {
		count = fmt.Sprintf("%d", f.ArrayBound)
	} else if f.Array == ir.ArrayCounted {
		count = fmt.Sprintf("%s.len().min(%d)", x, f.ArrayBound)
	}
	fmt.Fprintf(b, "%s%s.push(%d);\n", ind, dst, ek)
	fmt.Fprintf(b, "%slet count = %s;\n", ind, count)
	fmt.Fprintf(b, "%sleb_write(&mut %s, count as u64);\n", ind, dst)
	fmt.Fprintf(b, "%sfor (j, e) in %s.iter().take(count).enumerate() {\n", ind, x)
	g.emitElementWrite(b, f, "e", dst, ordinal, ind+"    ")
	fmt.Fprintf(b, "%s}\n", ind)
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%sfor j in %s.len()..count {\n", ind, x)
		fmt.Fprintf(b, "%s    let e = %s;\n", ind, fieldDefault(scalarOnly(f)))
		fmt.Fprintf(b, "%s    let _ = j;\n", ind)
		g.emitElementWrite(b, f, "(&e)", dst, ordinal, ind+"    ")
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
		g.emitUnionElement(b, f, x, dst, ordinal, ind)
	case vString:
		fmt.Fprintf(b, "%s%s.extend_from_slice(%s.as_bytes());\n", ind, dst, x)
	default:
		fmt.Fprintf(b, "%s%s\n", ind, scalarWrite(f, "(*"+x+")", dst))
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


// emitMapLoad reads a map as an array of entry tables, keeping an ascending
// prefix and counting a duplicate (docs/SPEC-TABLES.md §2.8, §4).
func (g *gen) emitMapLoad(b *strings.Builder, f *ir.Field, ordinal int, field, in string) {
	key := ir.RustSnake(ir.MapKeyFieldName)
	fmt.Fprintf(b, "%sif kind != %d { %s }\n", in, ir.TableKindArray, kindSkip)
	fmt.Fprintf(b, "%selse {\n", in)
	i1 := in + "    "
	fmt.Fprintf(b, "%slet Some(l) = leb_read(wire, off) else { report.malformed = true; return false; };\n", i1)
	fmt.Fprintf(b, "%sif *off > end || l > (end - *off) as u64 { report.malformed = true; return false; }\n", i1)
	fmt.Fprintf(b, "%slet sub = *off + l as usize;\n", i1)
	fmt.Fprintf(b, "%sif l >= 2 {\n", i1)
	i2 := i1 + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", i2)
	fmt.Fprintf(b, "%smatch leb_read(wire, off) {\n", i2)
	fmt.Fprintf(b, "%s    None => { report.malformed = true; }\n", i2)
	fmt.Fprintf(b, "%s    Some(_) if *off > sub => { report.malformed = true; }\n", i2)
	fmt.Fprintf(b, "%s    Some(count) => {\n", i2)
	i3 := i2 + "        "
	fmt.Fprintf(b, "%sif ek != %d { report.kind_mismatch += 1; }\n", i3, ir.TableKindTable)
	fmt.Fprintf(b, "%selse if count > 0x7fffffff { report.malformed = true; }\n", i3)
	fmt.Fprintf(b, "%selse {\n", i3)
	i4 := i3 + "    "
	fmt.Fprintf(b, "%s%s.clear();\n", i4, field)
	fmt.Fprintf(b, "%slet mut landed = false;\n", i4)
	fmt.Fprintf(b, "%sfor j in 0..count {\n", i4)
	i5 := i4 + "    "
	fmt.Fprintf(b, "%sif *off >= sub { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", i5)
	fmt.Fprintf(b, "%sif *off > sub || el > (sub - *off) as u64 { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", i5)
	fmt.Fprintf(b, "%slet mut e = %s::default();\n", i5, f.MapEntry.Name)
	fmt.Fprintf(b, "%slet mut o = *off;\n", i5)
	fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, j as u32), retain.as_deref_mut(), true);\n", i5, fn(f.MapEntry.Name, "load_body"), ordinal)
	fmt.Fprintf(b, "%sif o != eend { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%s*off = eend;\n", i5)
	fmt.Fprintf(b, "%sif landed {\n", i5)
	fmt.Fprintf(b, "%s    match %s {\n", i5, mapKeyOrd(f, field+"["+field+".len()-1]."+key, "e."+key))
	fmt.Fprintf(b, "%s        std::cmp::Ordering::Greater => { report.malformed = true; break; }\n", i5)
	fmt.Fprintf(b, "%s        std::cmp::Ordering::Equal => { report.duplicate += 1; %s.pop(); }\n", i5, field)
	fmt.Fprintf(b, "%s        std::cmp::Ordering::Less => {}\n", i5)
	fmt.Fprintf(b, "%s    }\n", i5)
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
	fmt.Fprintf(b, "%sif %s != %d { report.kind_mismatch += 1; %s = %s::None; }\n", ind, akind, want, field, un.Name)
	if v.Void() {
		fmt.Fprintf(b, "%selse { %s = %s::%s; }\n", ind, field, un.Name, rustVariant(v.Name))
		return
	}
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	f := v.F
	switch {
	case v.Body():
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", in, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", in)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, %s, ids, &mut e, nodes, report, path.step(%d, 0), retain.as_deref_mut(), true);\n", in, fn(f.Type.Name, "load_body"), aend, ordinal)
		fmt.Fprintf(b, "%sif o != %s { report.malformed = true; %s = %s::None; }\n", in, aend, field, un.Name)
		fmt.Fprintf(b, "%selse { %s = %s::%s(e); }\n", in, field, un.Name, rustVariant(v.Name))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%slet Some(index) = leb_read(wire, off) else { report.malformed = true; %s = %s::None; *off = %s; return false; };\n", in, field, un.Name, aend)
		fmt.Fprintf(b, "%sif *off > %s { report.malformed = true; %s = %s::None; }\n", in, aend, field, un.Name)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let slot;\n", in)
		emitPointerResolve(b, f, "slot", in+"    ")
		fmt.Fprintf(b, "%s    %s = %s::%s(slot);\n", in, field, un.Name, rustVariant(v.Name))
		fmt.Fprintf(b, "%s}\n", in)
	case classOf(f) == vUnion && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%slet mut inner = %s::None;\n", in, f.Type.Name)
		g.emitUnionDecode(b, f, "inner", aend, ordinal, in, true)
		fmt.Fprintf(b, "%s%s = %s::%s(inner);\n", in, field, un.Name, rustVariant(v.Name))
	default:
		fmt.Fprintf(b, "%slet mut slot = %s;\n", in, fieldDefault(scalarOnly(f)))
		if f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) {
			g.emitArrayBodyRead(b, f, "slot", aend, ordinal, in)
		} else if classOf(f) == vEnum {
			emitEnumDecode(b, f, "slot", aend, "return false;", in)
		} else if classOf(f) == vString {
			fmt.Fprintf(b, "%slet bytes = &wire[*off..%s];\n", in, aend)
			fmt.Fprintf(b, "%sif !text_valid(bytes) { report.malformed = true; }\n", in)
			fmt.Fprintf(b, "%selse if bytes.len() > %d { let mut cut = %d; while cut > 0 && bytes[cut] & 0xC0 == 0x80 { cut -= 1; } slot = std::str::from_utf8(&bytes[..cut]).unwrap().to_string(); report.clamped += 1; }\n", in, f.Type.Size, f.Type.Size)
			fmt.Fprintf(b, "%selse { slot = std::str::from_utf8(bytes).unwrap().to_string(); }\n", in)
		} else {
			fmt.Fprintf(b, "%slet Some(raw) = table_scalar_read(wire, off, %s, %s) else { report.malformed = true; %s = %s::None; *off = %s; return false; };\n", in, aend, akind, field, un.Name, aend)
			emitScalarConvert(b, f, "slot", akind, in)
		}
		fmt.Fprintf(b, "%s%s = %s::%s(slot);\n", in, field, un.Name, rustVariant(v.Name))
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitArrayBodyRead reads an array body already framed by `aend` (an arm's L,
// docs/SPEC-TABLES.md §3): element kind, count, elements.
func (g *gen) emitArrayBodyRead(b *strings.Builder, f *ir.Field, field, aend string, ordinal int, ind string) {
	ek := ir.TableKindU8
	if f.Type.Kind != ir.TBytes || f.Type.Pointer {
		ek = elemWireKind(f)
	}
	fmt.Fprintf(b, "%sif *off >= %s { report.malformed = true; }\n", ind, aend)
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%slet ek = wire[*off]; *off += 1;\n", in)
	fmt.Fprintf(b, "%slet Some(count) = leb_read(wire, off) else { report.malformed = true; *off = %s; return false; };\n", in, aend)
	fmt.Fprintf(b, "%sif ek != %d { report.kind_mismatch += 1; }\n", in, ek)
	fmt.Fprintf(b, "%selse {\n", in)
	i2 := in + "    "
	bound := f.ArrayBound
	if f.Type.Kind == ir.TBytes && f.Array == ir.ArrayNone {
		bound = int64(f.Type.Size)
	}
	if f.Array == ir.ArrayList {
		fmt.Fprintf(b, "%slet keep = if count > 0x7fffffff { report.malformed = true; 0 } else { count };\n", i2)
	} else if bound > 0 {
		fmt.Fprintf(b, "%slet keep = if count > %d { report.clamped += 1; %d } else { count };\n", i2, bound, bound)
	} else {
		fmt.Fprintf(b, "%slet keep = count;\n", i2)
	}
	fmt.Fprintf(b, "%s%s.clear();\n", i2, field)
	fmt.Fprintf(b, "%sfor j in 0..keep {\n", i2)
	g.emitElementRead(b, f, field, aend, ordinal, i2+"    ")
	fmt.Fprintf(b, "%s}\n", i2)
	fmt.Fprintf(b, "%s}\n", in)
	fmt.Fprintf(b, "%s}\n", ind)
}

func (g *gen) emitElementRead(b *strings.Builder, f *ir.Field, field, bound string, ordinal int, ind string) {
	fmt.Fprintf(b, "%sif *off >= %s { report.malformed = true; break; }\n", ind, bound)
	switch classOf(f) {
	case vTable:
		fmt.Fprintf(b, "%slet Some(el) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
		fmt.Fprintf(b, "%sif *off > %s || el > (%s - *off) as u64 { report.malformed = true; break; }\n", ind, bound, bound)
		fmt.Fprintf(b, "%slet eend = *off + el as usize;\n", ind)
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", ind, f.Type.Name)
		fmt.Fprintf(b, "%slet mut o = *off;\n", ind)
		fmt.Fprintf(b, "%slet _ = %s(wire, &mut o, eend, ids, &mut e, nodes, report, path.step(%d, j as u32), retain.as_deref_mut(), true);\n", ind, fn(f.Type.Name, "load_body"), ordinal)
		fmt.Fprintf(b, "%sif o != eend { report.malformed = true; e = %s::default(); }\n", ind, f.Type.Name)
		fmt.Fprintf(b, "%s*off = eend;\n", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vPointer:
		fmt.Fprintf(b, "%slet Some(index) = leb_read(wire, off) else { report.malformed = true; break; };\n", ind)
		fmt.Fprintf(b, "%slet e: Option<Rc<%s>>;\n", ind, f.Type.Name)
		emitPointerResolve(b, f, "e", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vUnion:
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", ind, f.Type.Name)
		g.emitUnionDecode(b, f, "e", bound, ordinal, ind, true)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vEnum:
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", ind, f.Type.Name)
		emitEnumDecode(b, f, "e", bound, "break;", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	default:
		fmt.Fprintf(b, "%slet Some(raw) = table_scalar_read(wire, off, %s, %d) else { report.malformed = true; break; };\n", ind, bound, elemWireKind(f))
		fmt.Fprintf(b, "%slet mut e = %s;\n", ind, scalarDefault(f))
		fmt.Fprintf(b, "%slet ek = %d;\n", ind, elemWireKind(f))
		emitScalarConvert(b, f, "e", "ek", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
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
	fmt.Fprintf(b, "%s    let mut order: Vec<usize> = (0..%s.len()).collect();\n", ind, x)
	fmt.Fprintf(b, "%s    order.sort_by(|&a, &b| %s);\n", ind, mapKeyOrd(f, x+"[a]."+key, x+"[b]."+key))
	fmt.Fprintf(b, "%s    w.out.push('{');\n", ind)
	fmt.Fprintf(b, "%s    for (i, &at) in order.iter().enumerate() {\n", ind)
	fmt.Fprintf(b, "%s        if i > 0 { w.out.push(','); }\n", ind)
	fmt.Fprintf(b, "%s        json_newline(&mut w.out, %s + 1);\n", ind, d)
	if ir.MapKeyField(f).Type.Kind == ir.TString {
		fmt.Fprintf(b, "%s        json_escape(&%s[at].%s, &mut w.out);\n", ind, x, key)
	} else {
		fmt.Fprintf(b, "%s        json_escape(&%s[at].%s.to_string(), &mut w.out);\n", ind, x, key)
	}
	fmt.Fprintf(b, "%s        w.out.push_str(\": \");\n", ind)
	g.emitTextWriteValue(b, val, x+"[at]."+ir.RustSnake(ir.MapValueFieldName), d+" + 1", ind+"        ")
	fmt.Fprintf(b, "%s    }\n", ind)
	fmt.Fprintf(b, "%s    json_newline(&mut w.out, %s);\n", ind, d)
	fmt.Fprintf(b, "%s    w.out.push('}');\n", ind)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitMapTextRead reads a JSON object into sorted entries (docs/SPEC-TABLES.md §16.2, §2.8).
func (g *gen) emitMapTextRead(b *strings.Builder, f *ir.Field, field, ind string) {
	key := ir.MapKeyField(f)
	val := ir.MapValueField(f)
	fmt.Fprintf(b, "%sif jr.peek() != b'{' { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind)
	fmt.Fprintf(b, "%selse {\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%sjr.p += 1;\n", in)
	fmt.Fprintf(b, "%s%s.clear();\n", in, field)
	fmt.Fprintf(b, "%sloop {\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() == b'}' { jr.p += 1; break; }\n", in)
	fmt.Fprintf(b, "%s    let Some(ks) = jr.read_string() else { jr.malformed = true; break; };\n", in)
	fmt.Fprintf(b, "%s    jr.space();\n", in)
	fmt.Fprintf(b, "%s    if jr.peek() != b':' { jr.malformed = true; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.p += 1;\n", in)
	fmt.Fprintf(b, "%s    let mut e = %s::default();\n", in, f.MapEntry.Name)
	if key.Type.Kind == ir.TString {
		fmt.Fprintf(b, "%s    e.%s = json_clamp(ks, %d, &mut jr.clamped);\n", in, ir.RustSnake(ir.MapKeyFieldName), key.Type.Size)
	} else {
		signed := key.Type.Signed
		parse := "parse"
		if !signed && key.Type.Kind == ir.TInt {
			parse = "parse"
		}
		fmt.Fprintf(b, "%s    match ks.%s::<%s>() {\n", in, parse, scalarRust(key))
		fmt.Fprintf(b, "%s        Ok(n) => e.%s = n,\n", in, ir.RustSnake(ir.MapKeyFieldName))
		fmt.Fprintf(b, "%s        Err(_) => { jr.kind_mismatch += 1; jr.skip_value(); let c = jr.peek(); if c == b',' { jr.p += 1; continue; } if c == b'}' { jr.p += 1; break; } jr.malformed = true; break; }\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
	}
	g.emitTextReadInto(b, val, "e."+ir.RustSnake(ir.MapValueFieldName), in+"    ")
	fmt.Fprintf(b, "%s    %s.push(e);\n", in, field)
	fmt.Fprintf(b, "%s    let c = jr.peek();\n", in)
	fmt.Fprintf(b, "%s    if c == b',' { jr.p += 1; continue; }\n", in)
	fmt.Fprintf(b, "%s    if c == b'}' { jr.p += 1; break; }\n", in)
	fmt.Fprintf(b, "%s    jr.malformed = true; break;\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	fmt.Fprintf(b, "%s%s.sort_by(|a, b| %s);\n", in, field, mapKeyOrd(f, "a."+ir.RustSnake(ir.MapKeyFieldName), "b."+ir.RustSnake(ir.MapKeyFieldName)))
	fmt.Fprintf(b, "%s}\n", ind)
}

func (g *gen) emitTextReadInto(b *strings.Builder, f *ir.Field, dst, ind string) {
	switch classOf(f) {
	case vPointer:
		fmt.Fprintf(b, "%s%s = %s(jr);\n", ind, dst, fn(f.Type.Name, "json_ptr"))
		return
	case vTable:
		fmt.Fprintf(b, "%sif jr.peek() == b'{' { %s = %s::default(); %s(jr, &mut %s); } else { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind, dst, f.Type.Name, fn(f.Type.Name, "json_read"), dst)
		return
	case vMap:
		g.emitMapTextRead(b, f, dst, ind)
		return
	case vUnion:
		fmt.Fprintf(b, "%sjr.skip_value();\n", ind)
		return
	}
	if f.Array != ir.ArrayNone {
		fmt.Fprintf(b, "%sif jr.peek() == b'[' {\n", ind)
		fmt.Fprintf(b, "%s    jr.p += 1;\n", ind)
		fmt.Fprintf(b, "%s    loop {\n", ind)
		fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", ind)
		fmt.Fprintf(b, "%s        let mut one = %s;\n", ind, fieldDefault(scalarOnly(f)))
		g.emitTextReadScalar(b, scalarOnly(f), "one", ind+"        ")
		fmt.Fprintf(b, "%s        %s.push(one);\n", ind, dst)
		fmt.Fprintf(b, "%s        let c = jr.peek();\n", ind)
		fmt.Fprintf(b, "%s        if c == b',' { jr.p += 1; continue; }\n", ind)
		fmt.Fprintf(b, "%s        if c == b']' { jr.p += 1; break; }\n", ind)
		fmt.Fprintf(b, "%s        jr.malformed = true; break;\n", ind)
		fmt.Fprintf(b, "%s    }\n", ind)
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind)
		return
	}
	g.emitTextReadScalar(b, f, dst, ind)
}



