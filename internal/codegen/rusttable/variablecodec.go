package rusttable

// THE VARIABLE FORM'S CODEC (docs/SPEC-TABLES.md §3, §3.1): the Rust save and
// load for exactly the card record's constructs (plain `table` roots, tables
// as list elements, bool, int32, int64 and enum fields, string(N), *string and
// []T) — the wire the carried framing (variable.go) pins the field headers
// for. It is the SECOND STAGE of the port, emitted beside the constants.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// variableCodecModule emits one source file's variable-table storage structs
// and their save/load codecs, for the tables that pass the card boundary. Nil
// for a file that declares no such variable table.
func (g *gen) variableCodecModule() []byte {
	names := cardVariableTableNames(g)
	if len(names) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package, "the variable wire's storage and codecs (docs/SPEC-TABLES.md §3, §3.1, §16)"))
	b.WriteString("\nuse super::variable_runtime::*;\n\n")
	for _, n := range names {
		g.emitVariableStruct(&b, g.unit.Tables[n])
	}
	for _, n := range names {
		g.emitVariableSaveBody(&b, g.unit.Tables[n])
	}
	for _, n := range names {
		g.emitVariableLoadBody(&b, g.unit.Tables[n], g.isNestedTable(g.unit.Tables[n]))
	}
	for _, n := range names {
		g.emitVariableRoot(&b, g.unit.Tables[n])
	}
	g.emitVariableText(&b)
	return []byte(b.String())
}

// rustVariant turns a schema variant's snake_case into the PascalCase Rust
// enum-variant spelling: flash -> Flash, _reserved -> Reserved.
func rustVariant(name string) string {
	out := make([]byte, 0, len(name))
	up := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			up = true
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			out = append(out, c-'a'+'A')
		} else {
			out = append(out, c)
		}
		up = false
	}
	return string(out)
}

// variableFieldType is a field's Rust storage type for the card slice.
func variableFieldType(f *ir.Field) string {
	switch {
	case f.IsList():
		return "Vec<" + f.Type.Name + ">"
	case f.Type.Blob():
		return "Option<String>"
	case f.Type.Kind == ir.TString:
		return "String"
	case f.Type.Kind == ir.TBool:
		return "bool"
	case f.Type.Kind == ir.TInt:
		return rustInt(f.Type.Width)
	case f.Type.Kind == ir.TNamed:
		return f.Type.Name
	}
	return "()"
}

// emitVariableStruct emits one variable table's storage struct and the enums
// its fields reach, with the Rust enum storage for each enum declaration.
func (g *gen) emitVariableStruct(b *strings.Builder, st *ir.Struct) {
	seen := map[string]bool{}
	for _, f := range st.Fields {
		if f.Type.Kind == ir.TNamed {
			if e, ok := f.Type.Ref.(*ir.Enum); ok && !seen[e.Name] {
				seen[e.Name] = true
				fmt.Fprintf(b, "/// %s — an enum on the variable wire (docs/SPEC-TABLES.md §3, §5).\n", e.Name)
				fmt.Fprintf(b, "#[derive(Clone, Copy, PartialEq, Eq, Default, Debug)]\n")
				fmt.Fprintf(b, "pub enum %s {\n", e.Name)
				fmt.Fprintf(b, "    #[default]\n    None,\n")
				for _, v := range e.Variants {
					fmt.Fprintf(b, "    %s,\n", rustVariant(v))
				}
				b.WriteString("}\n\n")
			}
		}
	}
	fmt.Fprintf(b, "/// %s — a variable-table storage struct (docs/SPEC-TABLES.md §3).\n", st.Name)
	fmt.Fprintf(b, "#[derive(Clone, Default, Debug)]\n")
	fmt.Fprintf(b, "pub struct %s {\n", st.Name)
	for _, f := range st.Fields {
		fmt.Fprintf(b, "    pub %s: %s,\n", ir.RustSnake(f.Name), variableFieldType(f))
	}
	b.WriteString("}\n\n")
}

// emitVariableSaveBody emits <name>_save_body: the field walk that appends one
// body (fields, then the zero reference) to `out`, threading the shared id
// table and the node collector.
func (g *gen) emitVariableSaveBody(b *strings.Builder, st *ir.Struct) {
	fnName := fn(st.Name, "save_body")
	fmt.Fprintf(b, "/// %s — one %s body (docs/SPEC-TABLES.md §3): fields in declaration\n", fnName, st.Name)
	fmt.Fprintf(b, "/// order, then the zero reference. `retain` and `report` are Some on the\n")
	fmt.Fprintf(b, "/// retaining path, where each nested body emits its retained tail before\n")
	fmt.Fprintf(b, "/// its zero reference (docs/SPEC-TABLES.md §6.6); None on the plain path.\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, out: &mut Vec<u8>, ids: &mut IdTable, nodes: &mut Vec<(u64, Vec<u8>)>, mut retain: Option<&mut TableRetain>, mut report: Option<&mut TableReport>, path: TableRetainPath) {\n", fnName, st.Name)
	for i, f := range st.Fields {
		g.emitVariableSaveField(b, f, i, "v."+ir.RustSnake(f.Name), "\t")
	}
	fmt.Fprintf(b, "}\n\n")
}

// emitVariableSaveField emits the elision guard and the write for one field.
func (g *gen) emitVariableSaveField(b *strings.Builder, f *ir.Field, ordinal int, expr, ind string) {
	id := ir.TableFieldWireId(f)
	kind := ir.TableWireFieldKind(f)
	guard := variableRides(f, expr)
	fmt.Fprintf(b, "%sif %s {\n", ind, guard)
	inner := ind + "    "
	switch {
	case f.IsList():
		elem := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "%slet r = ids.id_ref(0x%016x);\n", inner, id)
		fmt.Fprintf(b, "%slet mut body = Vec::new();\n", inner)
		fmt.Fprintf(b, "%sbody.push(%d); // element kind: table\n", inner, ir.TableWireElemKind(f))
		fmt.Fprintf(b, "%sleb_write(&mut body, %s.len() as u64);\n", inner, expr)
		fmt.Fprintf(b, "%sfor (j, e) in %s.iter().enumerate() {\n", inner, expr)
		fmt.Fprintf(b, "%s    let elem_path = path.step(%d, j as u32);\n", inner, ordinal)
		fmt.Fprintf(b, "%s    let mut eb = Vec::new();\n", inner)
		fmt.Fprintf(b, "%s    %s(e, &mut eb, ids, nodes, retain.as_deref_mut(), report.as_deref_mut(), elem_path);\n", inner, fn(elem.Name, "save_body"))
		fmt.Fprintf(b, "%s    if let (Some(ret), Some(rep)) = (retain.as_deref_mut(), report.as_deref_mut()) {\n", inner)
		fmt.Fprintf(b, "%s        retain_emit_tail(&mut eb, ids, ret, KNOWN_IDS, elem_path, rep);\n", inner)
		fmt.Fprintf(b, "%s    }\n", inner)
		fmt.Fprintf(b, "%s    eb.push(0); // the element body's zero reference\n", inner)
		fmt.Fprintf(b, "%s    leb_write(&mut body, eb.len() as u64);\n", inner)
		fmt.Fprintf(b, "%s    body.extend_from_slice(&eb);\n", inner)
		fmt.Fprintf(b, "%s}\n", inner)
		fmt.Fprintf(b, "%sleb_write(out, r);\n", inner)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, body.len() as u64);\n", inner)
		fmt.Fprintf(b, "%sout.extend_from_slice(&body);\n", inner)
	case f.Type.Blob():
		fmt.Fprintf(b, "%slet idx = node_blob_index(nodes, STRING_TYPE_ID, %s.as_ref().unwrap().as_bytes());\n", inner, expr)
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, idx);\n", inner)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sleb_write(out, %s.len() as u64);\n", inner, expr)
		fmt.Fprintf(b, "%sout.extend_from_slice(%s.as_bytes());\n", inner, expr)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sout.push(if %s { 1 } else { 0 });\n", inner, expr)
	case f.Type.Kind == ir.TInt:
		fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
		fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
		fmt.Fprintf(b, "%sout.extend_from_slice(&(%s as u%d).to_le_bytes());\n", inner, expr, f.Type.Width)
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "%sleb_write(out, ids.id_ref(0x%016x));\n", inner, id)
			fmt.Fprintf(b, "%sout.push(%d);\n", inner, kind)
			fmt.Fprintf(b, "%slet vref = match %s {\n", inner, expr)
			fmt.Fprintf(b, "%s    %s::None => 0,\n", inner, e.Name)
			for i, v := range e.Variants {
				vid := ir.TableWireId(e.VariantWireName(i))
				fmt.Fprintf(b, "%s    %s::%s => ids.id_ref(0x%016x),\n", inner, e.Name, rustVariant(v), vid)
			}
			fmt.Fprintf(b, "%s};\n", inner)
			fmt.Fprintf(b, "%sleb_write(out, vref);\n", inner)
		}
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// variableRides is the elision guard: a field rides when it is not entirely
// default (docs/SPEC-TABLES.md §3's "writers elide what readers default").
func variableRides(f *ir.Field, expr string) string {
	if f.IsList() {
		return "!" + expr + ".is_empty()"
	}
	if f.Type.Blob() {
		return expr + ".is_some()"
	}
	if f.Type.Kind == ir.TString {
		return "!" + expr + ".is_empty()"
	}
	if f.Type.Kind == ir.TBool {
		return expr
	}
	if f.Type.Kind == ir.TInt {
		return expr + " != 0"
	}
	if f.Type.Kind == ir.TNamed {
		if _, ok := f.Type.Ref.(*ir.Enum); ok {
			return expr + " != " + f.Type.Name + "::None"
		}
	}
	return "true"
}

// emitVariableLoadBody emits <name>_load_body: the tolerant scan of one body
// (docs/SPEC-TABLES.md §4, §5) into the storage struct. A pointer field resolves
// its node index against the node blobs (the table is scanned whole before the
// body walk, docs/SPEC-TABLES.md §3.1), so an absent pointer leaves its slot
// null and never shifts a neighbour's — exactly as the reference resolves a
// pointer into its own cell (internal/tablewire/decodenodes.go).
func (g *gen) emitVariableLoadBody(b *strings.Builder, st *ir.Struct, nested bool) {
	fnName := fn(st.Name, "load_body")
	fmt.Fprintf(b, "/// %s — the tolerant read of one %s body (docs/SPEC-TABLES.md §4, §5).\n", fnName, st.Name)
	fmt.Fprintf(b, "/// `retain` is Some on the retaining path, and every unknown field it names\n")
	fmt.Fprintf(b, "/// is captured into the store under `path` (docs/SPEC-TABLES.md §6.6); None on\n")
	fmt.Fprintf(b, "/// the plain path.\n")
	fmt.Fprintf(b, "pub fn %s(wire: &[u8], off: &mut usize, end: usize, ids: &[u64], v: &mut %s, blobs: &[Option<Vec<u8>>], report: &mut TableReport, path: TableRetainPath, mut retain: Option<&mut TableRetain>) -> bool {\n", fnName, st.Name)
	fmt.Fprintf(b, "    while *off < end {\n")
	fmt.Fprintf(b, "        let Some(r) = leb_read(wire, off) else { report.malformed = true; return false; };\n")
	fmt.Fprintf(b, "        if r == 0 { return true; }\n")
	fmt.Fprintf(b, "        let Some(id) = ref_id(ids, r) else { report.malformed = true; return false; };\n")
	fmt.Fprintf(b, "        if *off >= end { report.malformed = true; return false; }\n")
	fmt.Fprintf(b, "        let kind = wire[*off]; *off += 1;\n")
	fmt.Fprintf(b, "        if id == BUILD_VERSION_ID || id == VOCABULARY_ID { report.malformed = true; return false; } // the reserved announcement ids ride in the announcement and nowhere else (§3.3)\n")
	fmt.Fprintf(b, "        if id == NODE_TABLE_ID {\n")
	if nested {
		fmt.Fprintf(b, "            // the reserved id inside a nested body is malformed (§3.1)\n")
		fmt.Fprintf(b, "            report.malformed = true; return false;\n")
	} else {
		fmt.Fprintf(b, "            // a VARIABLE-class root steps over its own node table and never\n")
		fmt.Fprintf(b, "            // counts it unknown (§3.1)\n")
		fmt.Fprintf(b, "            if !leb_skip(wire, off, end, kind) { report.malformed = true; return false; }\n")
		fmt.Fprintf(b, "            continue;\n")
	}
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        match id {\n")
	for i, f := range st.Fields {
		g.emitVariableLoadField(b, f, i, "\t", "off", "*off", "retain.as_deref_mut()")
	}
	// THE UNKNOWN ARM (docs/SPEC-TABLES.md §4, §6.6): the plain read skips the
	// field and counts one unknown; the retaining read also captures it, or
	// counts one retain_lost when the walk or the capacity cannot keep it whole.
	fmt.Fprintf(b, "            _ => {\n")
	fmt.Fprintf(b, "                report.unknown += 1;\n")
	fmt.Fprintf(b, "                let start = *off;\n")
	fmt.Fprintf(b, "                if !leb_skip(wire, off, end, kind) { report.malformed = true; return false; }\n")
	fmt.Fprintf(b, "                if let Some(ret) = retain.as_deref_mut() {\n")
	fmt.Fprintf(b, "                    match retain_capture(ids, kind, &wire[start..*off]) {\n")
	fmt.Fprintf(b, "                        Some(resolved) => {\n")
	fmt.Fprintf(b, "                            let need = 17 + 8 * path.depth as usize + resolved.len();\n")
	fmt.Fprintf(b, "                            if ret.bytes.len() + need <= ret.bytes.capacity() {\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&(need as u32).to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&path.depth.to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&id.to_le_bytes());\n")
	fmt.Fprintf(b, "                                ret.bytes.push(kind);\n")
	fmt.Fprintf(b, "                                for i in 0..path.depth as usize {\n")
	fmt.Fprintf(b, "                                    let (o, x) = path.steps[i];\n")
	fmt.Fprintf(b, "                                    ret.bytes.extend_from_slice(&o.to_le_bytes());\n")
	fmt.Fprintf(b, "                                    ret.bytes.extend_from_slice(&x.to_le_bytes());\n")
	fmt.Fprintf(b, "                                }\n")
	fmt.Fprintf(b, "                                ret.bytes.extend_from_slice(&resolved);\n")
	fmt.Fprintf(b, "                                report.retained += 1;\n")
	fmt.Fprintf(b, "                            } else { report.retain_lost += 1; }\n")
	fmt.Fprintf(b, "                        }\n")
	fmt.Fprintf(b, "                        None => { report.retain_lost += 1; }\n")
	fmt.Fprintf(b, "                    }\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    report.malformed = true; false\n")
	fmt.Fprintf(b, "}\n\n")
}

// isNestedTable reports whether a table is reached only as an []T list element
// (docs/SPEC-TABLES.md §2.9): its body is a NESTED body, so the reserved node id
// inside it is malformed (§3.1) rather than its own numbering.
func (g *gen) isNestedTable(st *ir.Struct) bool {
	for _, t := range g.unit.Tables {
		for _, f := range t.Fields {
			if f.IsList() && f.Type.Name == st.Name {
				return true
			}
		}
	}
	return false
}

// emitVariableLoadField emits one field's decode arm inside the load match.
// ordinal is the field's position in its body's declaration order, the step the
// retention path descends through (docs/SPEC-TABLES.md §6.6). offArg is the
// expression handed to the leb readers (a &mut usize: `off` in a load_body,
// `&mut off` in a root), and offDeref the cursor's value (`*off` in a
// load_body, `off` in a root).
func (g *gen) emitVariableLoadField(b *strings.Builder, f *ir.Field, ordinal int, ind, offArg, offDeref, retainArg string) {
	id := ir.TableFieldWireId(f)
	field := "v." + ir.RustSnake(f.Name)
	fmt.Fprintf(b, "%s0x%016x => {\n", ind, id)
	inner := ind + "    "
	switch {
	case f.IsList():
		elem := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "%sif kind != %d { report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; } }\n", inner, ir.TableWireFieldKind(f), offArg)
		fmt.Fprintf(b, "%selse {\n", inner)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, %s) else { report.malformed = true; return false; };\n", inner, offArg)
		fmt.Fprintf(b, "%s    if l > (end - %s) as u64 { report.malformed = true; return false; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s    let sub = %s + l as usize;\n", inner, offDeref)
		fmt.Fprintf(b, "%s    if %s >= sub { report.malformed = true; return false; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s    let ek = wire[%s]; %s += 1;\n", inner, offDeref, offDeref)
		fmt.Fprintf(b, "%s    if ek != %d { report.kind_mismatch += 1; %s = sub; }\n", inner, ir.TableWireElemKind(f), offDeref)
		fmt.Fprintf(b, "%s    else {\n", inner)
		fmt.Fprintf(b, "%s        if let Some(n) = leb_read(wire, %s) {\n", inner, offArg)
		fmt.Fprintf(b, "%s            if n > 0x7fffffff { report.malformed = true; if let Some(ret) = %s { retain_discard(ret, path, %d); } %s.clear(); %s = sub; }\n", inner, retainArg, ordinal, field, offDeref)
		fmt.Fprintf(b, "%s            else {\n", inner)
		fmt.Fprintf(b, "%s                if let Some(ret) = %s { retain_discard(ret, path, %d); }\n", inner, retainArg, ordinal)
		fmt.Fprintf(b, "%s                %s.clear();\n", inner, field)
		fmt.Fprintf(b, "%s                for j in 0..n {\n", inner)
		fmt.Fprintf(b, "%s                    if %s >= sub { report.malformed = true; break; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s                    let Some(el) = leb_read(wire, %s) else { report.malformed = true; break; };\n", inner, offArg)
		fmt.Fprintf(b, "%s                    if %s > sub || el > (sub - %s) as u64 { report.malformed = true; break; }\n", inner, offDeref, offDeref)
		fmt.Fprintf(b, "%s                    let eend = %s + el as usize;\n", inner, offDeref)
		fmt.Fprintf(b, "%s                    let mut e = %s::default();\n", inner, elem.Name)
		fmt.Fprintf(b, "%s                    let _ = %s(wire, %s, eend, ids, &mut e, blobs, report, path.step(%d, j as u32), %s);\n", inner, fn(elem.Name, "load_body"), offArg, ordinal, retainArg)
		fmt.Fprintf(b, "%s                    if %s != eend { report.malformed = true; e = %s::default(); }\n", inner, offDeref, elem.Name)
		fmt.Fprintf(b, "%s                    %s = eend;\n", inner, offDeref)
		fmt.Fprintf(b, "%s                    %s.push(e);\n", inner, field)
		fmt.Fprintf(b, "%s                }\n", inner)
		fmt.Fprintf(b, "%s                %s = sub;\n", inner, offDeref)
		fmt.Fprintf(b, "%s            }\n", inner)
		fmt.Fprintf(b, "%s        } else { report.malformed = true; %s = sub; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s    }\n", inner)
		fmt.Fprintf(b, "%s}\n", inner)
	case f.Type.Blob():
		fmt.Fprintf(b, "%sif kind != %d { report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; } }\n", inner, ir.TableWireFieldKind(f), offArg)
		fmt.Fprintf(b, "%selse {\n", inner)
		fmt.Fprintf(b, "%s    let Some(idx) = leb_read(wire, %s) else { report.malformed = true; return false; };\n", inner, offArg)
		// a node index resolves into THIS slot (docs/SPEC-TABLES.md §3.1):
		// 0 is null, 1 is the root (a kind mismatch on a blob slot), and 2+
		// name the node table's records in index order.
		fmt.Fprintf(b, "%s    if idx == 0 { %s = None; } else if idx == 1 { report.kind_mismatch += 1; %s = None; } else if idx as usize - 2 >= blobs.len() { report.malformed = true; %s = None; } else { %s = blobs[idx as usize - 2].as_ref().map(|b| String::from_utf8_lossy(b).into_owned()); }\n", inner, field, field, field, field)
		fmt.Fprintf(b, "%s}\n", inner)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sif kind != %d { report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; } }\n", inner, ir.TableWireFieldKind(f), offArg)
		fmt.Fprintf(b, "%selse {\n", inner)
		fmt.Fprintf(b, "%s    let Some(l) = leb_read(wire, %s) else { report.malformed = true; return false; };\n", inner, offArg)
		fmt.Fprintf(b, "%s    if l > (end - %s) as u64 { report.malformed = true; return false; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s    let s = %s + l as usize;\n", inner, offDeref)
		fmt.Fprintf(b, "%s    let b = &wire[%s..s];\n", inner, offDeref)
		fmt.Fprintf(b, "%s    if !text_valid(b) { report.malformed = true; %s = String::new(); }\n", inner, field)
		fmt.Fprintf(b, "%s    else if b.len() > %d {\n", inner, f.Type.Size)
		fmt.Fprintf(b, "%s        let mut cut = %d;\n", inner, f.Type.Size)
		fmt.Fprintf(b, "%s        while cut > 0 && b[cut] & 0xC0 == 0x80 { cut -= 1; }\n", inner)
		fmt.Fprintf(b, "%s        %s = std::str::from_utf8(&b[..cut]).unwrap().to_string();\n", inner, field)
		fmt.Fprintf(b, "%s        report.clamped += 1;\n", inner)
		fmt.Fprintf(b, "%s    }\n", inner)
		fmt.Fprintf(b, "%s    else { %s = std::str::from_utf8(b).unwrap().to_string(); }\n", inner, field)
		fmt.Fprintf(b, "%s    %s = s;\n", inner, offDeref)
		fmt.Fprintf(b, "%s}\n", inner)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "%sif kind != %d { report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; } }\n", inner, ir.TableWireFieldKind(f), offArg)
		fmt.Fprintf(b, "%selse {\n", inner)
		fmt.Fprintf(b, "%s    if %s >= end { report.malformed = true; return false; }\n", inner, offDeref)
		fmt.Fprintf(b, "%s    %s = wire[%s] != 0; %s += 1;\n", inner, field, offDeref, offDeref)
		fmt.Fprintf(b, "%s}\n", inner)
	case f.Type.Kind == ir.TInt:
		g.emitIntLoadField(b, f, field, inner, offArg, offDeref)
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "%sif kind != %d { report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; } }\n", inner, ir.TableWireFieldKind(f), offArg)
			fmt.Fprintf(b, "%selse {\n", inner)
			fmt.Fprintf(b, "%s    let Some(vref) = leb_read(wire, %s) else { report.malformed = true; return false; };\n", inner, offArg)
			fmt.Fprintf(b, "%s    if vref == 0 { %s = %s::None; }\n", inner, field, e.Name)
			fmt.Fprintf(b, "%s    else if vref as usize > ids.len() { report.malformed = true; return false; }\n", inner)
			fmt.Fprintf(b, "%s    else {\n", inner)
			fmt.Fprintf(b, "%s        %s = match ids[vref as usize - 1] {\n", inner, field)
			for i, v := range e.Variants {
				vid := ir.TableWireId(e.VariantWireName(i))
				fmt.Fprintf(b, "%s            0x%016x => %s::%s,\n", inner, vid, e.Name, rustVariant(v))
			}
			fmt.Fprintf(b, "%s            _ => { report.unknown += 1; if %s.is_some() { report.retain_lost += 1; } %s::None },\n", inner, retainArg, e.Name)
			fmt.Fprintf(b, "%s        };\n", inner)
			fmt.Fprintf(b, "%s    }\n", inner)
			fmt.Fprintf(b, "%s}\n", inner)
		}
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitIntLoadField emits one signed integer field's decode arm: the declared
// kind, each narrower kind that widens into it (§4, decoded exactly at its own
// width and counted `widened`), and the declared min/max clamp (§4) — the same
// ladder the oracle's scalarAt walks (internal/tablewire/decode.go).
func (g *gen) emitIntLoadField(b *strings.Builder, f *ir.Field, field, ind, offArg, offDeref string) {
	declared := ir.TableWireFieldKind(f)
	width := f.Type.Width / 8
	clamp := func(inner string) {
		if !f.HasIntRange {
			return
		}
		fmt.Fprintf(b, "%s    if x < %s { x = %s; report.clamped += 1; }\n", inner, f.IntMin.String(), f.IntMin.String())
		fmt.Fprintf(b, "%s    if x > %s { x = %s; report.clamped += 1; }\n", inner, f.IntMax.String(), f.IntMax.String())
	}
	emitRead := func(inner string, kw int, rustWidth int) {
		fmt.Fprintf(b, "%s    if %s + %d > end { report.malformed = true; return false; }\n", inner, offDeref, kw)
		fmt.Fprintf(b, "%s    let mut x = i%d::from_le_bytes(wire[%s..%s + %d].try_into().unwrap()) as i64;\n", inner, rustWidth, offDeref, offDeref, kw)
		clamp(inner)
		fmt.Fprintf(b, "%s    %s = x as %s;\n", inner, field, rustInt(f.Type.Width))
		fmt.Fprintf(b, "%s    %s += %d;\n", inner, offDeref, kw)
	}
	fmt.Fprintf(b, "%sif kind == %d {\n", ind, declared)
	emitRead(ind+"    ", width, f.Type.Width)
	for k := 1; k < declared; k++ {
		if !ir.TableKindWidens(k, declared) {
			continue
		}
		kw := ir.TableKindWidth(k)
		fmt.Fprintf(b, "%s} else if kind == %d {\n", ind, k)
		emitRead(ind+"    ", kw, ir.TableKindWidth(k)*8)
		fmt.Fprintf(b, "%s    report.widened += 1;\n", ind)
	}
	fmt.Fprintf(b, "%s} else {\n", ind)
	fmt.Fprintf(b, "%s    report.kind_mismatch += 1; if !leb_skip(wire, %s, end, kind) { report.malformed = true; return false; }\n", ind, offArg)
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitVariableRoot emits <name>_save and <name>_load: the whole file — form
// byte, the root body, the node table (for the pointered constructs §3.1) and
// the id-table trailer — and the tolerant read of the same.
func (g *gen) emitVariableRoot(b *strings.Builder, st *ir.Struct) {
	fnName := ir.RustSnake(st.Name)
	// SAVE
	fmt.Fprintf(b, "/// %s_save — a whole form-1 FILE (docs/SPEC-TABLES.md §3, §3.1).\n", fnName)
	fmt.Fprintf(b, "pub fn %s_save(v: &%s) -> Vec<u8> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    let mut out = Vec::new();\n")
	fmt.Fprintf(b, "    out.push(TABLE_WIRE_FORM);\n")
	fmt.Fprintf(b, "    let mut ids = IdTable::new();\n")
	fmt.Fprintf(b, "    let mut nodes: Vec<(u64, Vec<u8>)> = Vec::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut out, &mut ids, &mut nodes, None, None, TableRetainPath::default());\n", fn(st.Name, "save_body"))
	fmt.Fprintf(b, "    if !nodes.is_empty() {\n")
	fmt.Fprintf(b, "        leb_write(&mut out, ids.id_ref(NODE_TABLE_ID));\n")
	fmt.Fprintf(b, "        out.push(12); // kind 12: the opaque byte payload (§3.1)\n")
	fmt.Fprintf(b, "        let mut payload = Vec::new();\n")
	fmt.Fprintf(b, "        leb_write(&mut payload, nodes.len() as u64);\n")
	fmt.Fprintf(b, "        for (type_id, blob) in &nodes {\n")
	fmt.Fprintf(b, "            leb_write(&mut payload, ids.id_ref(*type_id));\n")
	fmt.Fprintf(b, "            leb_write(&mut payload, blob.len() as u64);\n")
	fmt.Fprintf(b, "            payload.extend_from_slice(blob);\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        leb_write(&mut out, payload.len() as u64);\n")
	fmt.Fprintf(b, "        out.extend_from_slice(&payload);\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    out.push(0); // the root body's zero reference\n")
	fmt.Fprintf(b, "    for e in &ids.entries { out.extend_from_slice(&e.to_le_bytes()); }\n")
	fmt.Fprintf(b, "    out.extend_from_slice(&(ids.entries.len() as u64).to_le_bytes());\n")
	fmt.Fprintf(b, "    out\n")
	fmt.Fprintf(b, "}\n\n")

	// SAVE-RETAIN (docs/SPEC-TABLES.md §6.6): the same file, with the retained
	// tail pinned BEFORE the node-table field and the retained ids numbered into
	// the one trailer in merged first-use order.
	fmt.Fprintf(b, "/// %s_save_retain — the retaining save (docs/SPEC-TABLES.md §6.6): the\n", fnName)
	fmt.Fprintf(b, "/// retained tail rides before the node-table field, and every retained id\n")
	fmt.Fprintf(b, "/// enters the one id table in first-use order.\n")
	fmt.Fprintf(b, "pub fn %s_save_retain(v: &%s, retain: &mut TableRetain, report: &mut TableReport) -> Vec<u8> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    retain.ids.clear();\n")
	fmt.Fprintf(b, "    let mut out = Vec::new();\n")
	fmt.Fprintf(b, "    out.push(TABLE_WIRE_FORM);\n")
	fmt.Fprintf(b, "    let mut ids = IdTable::new();\n")
	fmt.Fprintf(b, "    let mut nodes: Vec<(u64, Vec<u8>)> = Vec::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut out, &mut ids, &mut nodes, Some(&mut *retain), Some(&mut *report), TableRetainPath::default());\n", fn(st.Name, "save_body"))
	fmt.Fprintf(b, "    retain_emit_tail(&mut out, &mut ids, retain, KNOWN_IDS, TableRetainPath::default(), report);\n")
	fmt.Fprintf(b, "    if !nodes.is_empty() {\n")
	fmt.Fprintf(b, "        leb_write(&mut out, ids.id_ref(NODE_TABLE_ID));\n")
	fmt.Fprintf(b, "        out.push(12); // kind 12: the opaque byte payload (§3.1)\n")
	fmt.Fprintf(b, "        let mut payload = Vec::new();\n")
	fmt.Fprintf(b, "        leb_write(&mut payload, nodes.len() as u64);\n")
	fmt.Fprintf(b, "        for (type_id, blob) in &nodes {\n")
	fmt.Fprintf(b, "            leb_write(&mut payload, ids.id_ref(*type_id));\n")
	fmt.Fprintf(b, "            leb_write(&mut payload, blob.len() as u64);\n")
	fmt.Fprintf(b, "            payload.extend_from_slice(blob);\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        leb_write(&mut out, payload.len() as u64);\n")
	fmt.Fprintf(b, "        out.extend_from_slice(&payload);\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    out.push(0); // the root body's zero reference\n")
	fmt.Fprintf(b, "    for e in &ids.entries { out.extend_from_slice(&e.to_le_bytes()); }\n")
	fmt.Fprintf(b, "    out.extend_from_slice(&(ids.entries.len() as u64).to_le_bytes());\n")
	fmt.Fprintf(b, "    out\n")
	fmt.Fprintf(b, "}\n\n")

	// LOAD
	fmt.Fprintf(b, "/// %s_load — the tolerant read of a whole file (docs/SPEC-TABLES.md §4, §5).\n", fnName)
	fmt.Fprintf(b, "pub fn %s_load(wire: &[u8], report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    %s_load_inner(wire, None, report)\n", fnName)
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s_load_retain — the retaining read of the same file (docs/SPEC-TABLES.md\n", fnName)
	fmt.Fprintf(b, "/// §6.6): the next load into the store resets both of its stores, and every\n")
	fmt.Fprintf(b, "/// unknown field it can keep is captured whole.\n")
	fmt.Fprintf(b, "pub fn %s_load_retain(wire: &[u8], retain: &mut TableRetain, report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    retain.bytes.clear();\n")
	fmt.Fprintf(b, "    retain.ids.clear();\n")
	fmt.Fprintf(b, "    %s_load_inner(wire, Some(retain), report)\n", fnName)
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "pub fn %s_load_inner(wire: &[u8], mut retain: Option<&mut TableRetain>, report: &mut TableReport) -> Option<%s> {\n", fnName, st.Name)
	fmt.Fprintf(b, "    if wire.is_empty() { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    if wire[0] != TABLE_WIRE_FORM { report.refused = true; return None; }\n")
	fmt.Fprintf(b, "    if %s_load_measure(wire) < 0 { return None; } // a framing the container cannot open (§3, §6.5)\n", fnName)
	fmt.Fprintf(b, "    if wire.len() < 9 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    let count = u64::from_le_bytes(wire[wire.len() - 8..].try_into().ok()?) as usize;\n")
	fmt.Fprintf(b, "    if count > wire.len() / 8 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    let span = 8 + 8 * count;\n")
	fmt.Fprintf(b, "    if span + 1 > wire.len() { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "    let estart = wire.len() - span;\n")
	fmt.Fprintf(b, "    let end = estart;\n")
	fmt.Fprintf(b, "    let mut ids = Vec::with_capacity(count);\n")
	fmt.Fprintf(b, "    for i in 0..count {\n")
	fmt.Fprintf(b, "        let s = estart + 8 * i;\n")
	fmt.Fprintf(b, "        let id = u64::from_le_bytes(wire[s..s + 8].try_into().ok()?);\n")
	fmt.Fprintf(b, "        if ids.contains(&id) { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "        ids.push(id);\n")
	fmt.Fprintf(b, "    }\n")
	// body-extent pre-scan (docs/SPEC-TABLES.md §3): a body whose zero reference
	// lands before the id table is malformed, and nothing is decoded.
	fmt.Fprintf(b, "    {\n")
	fmt.Fprintf(b, "        let mut p = 1usize;\n")
	fmt.Fprintf(b, "        let mut term: Option<usize> = None;\n")
	fmt.Fprintf(b, "        loop {\n")
	fmt.Fprintf(b, "            if p >= end { break; }\n")
	fmt.Fprintf(b, "            let Some(r) = leb_read(wire, &mut p) else { break; };\n")
	fmt.Fprintf(b, "            if r == 0 { term = Some(p); break; }\n")
	fmt.Fprintf(b, "            if r as usize > ids.len() { break; }\n")
	fmt.Fprintf(b, "            if p >= end { break; }\n")
	fmt.Fprintf(b, "            let kind = wire[p]; p += 1;\n")
	fmt.Fprintf(b, "            if !leb_skip(wire, &mut p, end, kind) { break; }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        if let Some(t) = term { if t != end { report.malformed = true; return Some(%s::default()); } }\n", st.Name)
	fmt.Fprintf(b, "    }\n")
	// The node table (docs/SPEC-TABLES.md §3.1) is written LAST, so a scan that
	// resolves pointers as it meets them cannot know the numbering yet. One pass
	// gathers the node blobs, then the body read decodes and resolves the indices.
	fmt.Fprintf(b, "    let mut node_blobs: Vec<Option<Vec<u8>>> = Vec::new();\n")
	fmt.Fprintf(b, "    {\n")
	fmt.Fprintf(b, "        let mut off = 1usize;\n")
	fmt.Fprintf(b, "        while off < end {\n")
	fmt.Fprintf(b, "            let Some(r) = leb_read(wire, &mut off) else { report.malformed = true; return None; };\n")
	fmt.Fprintf(b, "            if r == 0 { break; }\n")
	fmt.Fprintf(b, "            if off >= end { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "            let kind = wire[off]; off += 1;\n")
	fmt.Fprintf(b, "            let Some(id) = ref_id(&ids, r) else { report.malformed = true; return None; };\n")
	fmt.Fprintf(b, "            if id == NODE_TABLE_ID {\n")
	fmt.Fprintf(b, "                if kind != 12 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "                let Some(l) = leb_read(wire, &mut off) else { report.malformed = true; return None; };\n")
	fmt.Fprintf(b, "                if l > (end - off) as u64 { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "                let sub = off + l as usize;\n")
	fmt.Fprintf(b, "                let Some(declared) = leb_read(wire, &mut off) else { report.malformed = true; return None; };\n")
	fmt.Fprintf(b, "                let mut recs = 0usize;\n")
	fmt.Fprintf(b, "                let mut scan_ok = true;\n")
	fmt.Fprintf(b, "                while off < sub {\n")
	fmt.Fprintf(b, "                    let Some(tref) = leb_read(wire, &mut off) else { scan_ok = false; break; };\n")
	fmt.Fprintf(b, "                    if tref == 0 || tref as usize > ids.len() { scan_ok = false; break; }\n")
	fmt.Fprintf(b, "                    let Some(bl) = leb_read(wire, &mut off) else { scan_ok = false; break; };\n")
	fmt.Fprintf(b, "                    if bl as usize > sub - off { scan_ok = false; break; }\n")
	fmt.Fprintf(b, "                    let tid = ids[tref as usize - 1];\n")
	fmt.Fprintf(b, "                    if tid == STRING_TYPE_ID {\n")
	fmt.Fprintf(b, "                        let bytes = &wire[off..off + bl as usize];\n")
	fmt.Fprintf(b, "                        if text_valid(bytes) { node_blobs.push(Some(bytes.to_vec())); }\n")
	fmt.Fprintf(b, "                        else { report.malformed = true; node_blobs.push(None); }\n")
	fmt.Fprintf(b, "                    } else { report.unknown += 1; if retain.is_some() { report.retain_lost += 1; } node_blobs.push(None); }\n")
	fmt.Fprintf(b, "                    off += bl as usize;\n")
	fmt.Fprintf(b, "                    recs += 1;\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "                if !scan_ok || off != sub || declared != recs as u64 { report.malformed = true; node_blobs.clear(); }\n")
	fmt.Fprintf(b, "                off = sub;\n")
	fmt.Fprintf(b, "                continue;\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if !leb_skip(wire, &mut off, end, kind) { report.malformed = true; return None; }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "    let mut off = 1usize;\n")
	fmt.Fprintf(b, "    let _ = %s(wire, &mut off, end, &ids, &mut v, &node_blobs, report, TableRetainPath::default(), retain.as_deref_mut());\n", fn(st.Name, "load_body"))
	fmt.Fprintf(b, "    Some(v)\n")
	fmt.Fprintf(b, "}\n\n")

	// MEASURE
	g.emitVariableMeasure(b, st)
}

// emitVariableMeasure emits <name>_load_measure: the region storage the framing
// commands (docs/SPEC-TABLES.md §3.1, §6.5), the card's slice of the oracle's
// FileRegionMeasure (internal/tablewire/extent.go) — the root record, every
// []T list's elements, the *string node blobs and the attribution.
func (g *gen) emitVariableMeasure(b *strings.Builder, st *ir.Struct) {
	fnName := ir.RustSnake(st.Name)
	rootSize := ir.RecordLayout(g.unit, st).Size
	align := ir.TableRegionAlign(g.unit)
	var lists []*ir.Field
	for _, f := range st.Fields {
		if f.IsList() {
			lists = append(lists, f)
		}
	}
	fmt.Fprintf(b, "/// %s_load_measure — the region bytes the framing commands\n", fnName)
	fmt.Fprintf(b, "/// (docs/SPEC-TABLES.md §3.1, §6.5): -1 when the file cannot be opened.\n")
	fmt.Fprintf(b, "pub fn %s_load_measure(wire: &[u8]) -> i64 {\n", fnName)
	fmt.Fprintf(b, "    if wire.is_empty() || wire[0] != TABLE_WIRE_FORM { return -1; }\n")
	fmt.Fprintf(b, "    if wire.len() < 9 { return -1; }\n")
	fmt.Fprintf(b, "    let count = u64::from_le_bytes(wire[wire.len() - 8..].try_into().unwrap()) as usize;\n")
	fmt.Fprintf(b, "    if count > wire.len() / 8 { return -1; }\n")
	fmt.Fprintf(b, "    let span = 8 + 8 * count;\n")
	fmt.Fprintf(b, "    if span + 1 > wire.len() { return -1; }\n")
	fmt.Fprintf(b, "    let estart = wire.len() - span;\n")
	fmt.Fprintf(b, "    let end = estart;\n")
	fmt.Fprintf(b, "    let mut ids = Vec::with_capacity(count);\n")
	fmt.Fprintf(b, "    for i in 0..count {\n")
	fmt.Fprintf(b, "        let s = estart + 8 * i;\n")
	fmt.Fprintf(b, "        let id = u64::from_le_bytes(wire[s..s + 8].try_into().unwrap());\n")
	fmt.Fprintf(b, "        if ids.contains(&id) { return -1; }\n")
	fmt.Fprintf(b, "        ids.push(id);\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    let round = |n: i64| (n + %d - 1) & -%d;\n", align, align)
	fmt.Fprintf(b, "    let mut at: i64 = 0;\n")
	fmt.Fprintf(b, "    let mut blob_total: i64 = 0;\n")
	fmt.Fprintf(b, "    let mut records: i64 = 0;\n")
	fmt.Fprintf(b, "    let mut off = 1usize;\n")
	fmt.Fprintf(b, "    while off < end {\n")
	fmt.Fprintf(b, "        let Some(r) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "        if r == 0 { break; }\n")
	fmt.Fprintf(b, "        if off >= end { return -1; }\n")
	fmt.Fprintf(b, "        let kind = wire[off]; off += 1;\n")
	fmt.Fprintf(b, "        let Some(id) = ref_id(&ids, r) else { return -1; };\n")
	fmt.Fprintf(b, "        if id == NODE_TABLE_ID {\n")
	fmt.Fprintf(b, "            if kind != 12 { return -1; }\n")
	fmt.Fprintf(b, "            let Some(l) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "            if l > (end - off) as u64 { return -1; }\n")
	fmt.Fprintf(b, "            let sub = off + l as usize;\n")
	fmt.Fprintf(b, "            let Some(declared) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "            let mut recs = 0i64;\n")
	fmt.Fprintf(b, "            let mut cur_blob = 0i64;\n")
	fmt.Fprintf(b, "            while off < sub {\n")
	fmt.Fprintf(b, "                let Some(tref) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "                if tref == 0 || tref as usize > ids.len() { return -1; }\n")
	fmt.Fprintf(b, "                let Some(bl) = leb_read(wire, &mut off) else { return -1; };\n")
	fmt.Fprintf(b, "                if bl as usize > sub - off { return -1; }\n")
	fmt.Fprintf(b, "                if ids[tref as usize - 1] == STRING_TYPE_ID {\n")
	fmt.Fprintf(b, "                    if bl > 0xffffffff { return -1; }\n")
	fmt.Fprintf(b, "                    cur_blob += round(8 + bl as i64 + 1);\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "                off += bl as usize;\n")
	fmt.Fprintf(b, "                recs += 1;\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            if off != sub || declared != recs as u64 { return -1; }\n")
	fmt.Fprintf(b, "            records = recs;\n")
	fmt.Fprintf(b, "            blob_total = cur_blob;\n")
	fmt.Fprintf(b, "            off = sub;\n")
	fmt.Fprintf(b, "            continue;\n")
	fmt.Fprintf(b, "        }\n")
	if len(lists) > 0 {
		fmt.Fprintf(b, "        if ")
		for i, f := range lists {
			if i > 0 {
				fmt.Fprintf(b, " || ")
			}
			fmt.Fprintf(b, "id == 0x%016x", ir.TableFieldWireId(f))
		}
		fmt.Fprintf(b, " {\n")
		fmt.Fprintf(b, "            if kind != %d { if !leb_skip(wire, &mut off, end, kind) { return -1; } }\n", ir.TableKindArray)
		fmt.Fprintf(b, "            else {\n")
		fmt.Fprintf(b, "                let Some(l) = leb_read(wire, &mut off) else { return -1; };\n")
		fmt.Fprintf(b, "                if l > (end - off) as u64 { return -1; }\n")
		fmt.Fprintf(b, "                let sub = off + l as usize;\n")
		fmt.Fprintf(b, "                if sub - off >= 2 {\n")
		fmt.Fprintf(b, "                    let ek = wire[off]; off += 1;\n")
		fmt.Fprintf(b, "                    let Some(c) = leb_read(wire, &mut off) else { return -1; };\n")
		fmt.Fprintf(b, "                    if ek == %d {\n", ir.TableKindTable)
		fmt.Fprintf(b, "                        if c > 0x7fffffff { return -1; }\n")
		fmt.Fprintf(b, "                        if c as usize > (sub - off) / 2 { return -1; }\n")
		fmt.Fprintf(b, "                        match id {\n")
		for _, f := range lists {
			size, la := ir.ListElementLayout(g.unit, f)
			fmt.Fprintf(b, "                            0x%016x => { at = (at + %d - 1) & -%d; at += c as i64 * %d; }\n", ir.TableFieldWireId(f), la, la, size)
		}
		fmt.Fprintf(b, "                            _ => {}\n")
		fmt.Fprintf(b, "                        }\n")
		fmt.Fprintf(b, "                    }\n")
		fmt.Fprintf(b, "                }\n")
		fmt.Fprintf(b, "                off = sub;\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "            continue;\n")
		fmt.Fprintf(b, "        }\n")
	}
	fmt.Fprintf(b, "        if !leb_skip(wire, &mut off, end, kind) { return -1; }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    round(round(%d) + at) + blob_total + (records + 1) * 16\n", rootSize)
	fmt.Fprintf(b, "}\n\n")
}
