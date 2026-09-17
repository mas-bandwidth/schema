package rusttable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A union remains a Rust enum. Only payloads with a live length need a
// wrapper; scalar and fixed-array arms keep their natural Rust type.
func armCounted(f *ir.Field) bool {
	return f != nil && (f.Array == ir.ArrayCounted || !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes))
}
func armType(f *ir.Field) string {
	t := rustFieldType(f.Type)
	if !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes) {
		t = fmt.Sprintf("[%s; %d]", t, f.Type.Size)
	} else if f.Array != ir.ArrayNone {
		t = fmt.Sprintf("[%s; %d]", t, f.ArrayBound)
	}
	if armCounted(f) {
		t = "TableArm<" + t + ">"
	}
	return t
}
func armZero(f *ir.Field) string {
	z := zeroScalar(f)
	if !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes) {
		z = fmt.Sprintf("[0; %d]", f.Type.Size)
	} else if f.Array != ir.ArrayNone {
		z = fmt.Sprintf("[%s; %d]", z, f.ArrayBound)
	}
	if armCounted(f) {
		z = "TableArm { value: " + z + ", length: 0 }"
	}
	return z
}
func armKind(v ir.UnionVariant) int {
	if v.Void() {
		return ir.TableKindNoPayload
	}
	return ir.TableWireFieldKind(v.F)
}

// Walk the whole closure before filtering by declaring file. An enum or
// union may be used only through an arm, or declared in a type-only file.
func (g *gen) unionMembers() []*ir.Union {
	seen := map[string]*ir.Union{}
	var visit func(*ir.Field)
	visit = func(f *ir.Field) {
		if u := unionOf(f); u != nil && seen[u.Name] == nil {
			seen[u.Name] = u
			for _, v := range u.Variants {
				if v.F != nil {
					visit(v.F)
				}
			}
		}
	}
	for _, file := range g.unit.Files {
		for _, st := range file.Tables {
			for _, f := range st.Fields {
				visit(f)
			}
		}
		for _, d := range file.Decls {
			if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
				for _, f := range st.Fields {
					visit(f)
				}
			}
		}
	}
	var out []*ir.Union
	for _, name := range sortedUnionNames(seen) {
		if g.unit.DeclFile[name] == g.file.Base {
			out = append(out, seen[name])
		}
	}
	return out
}
func (g *gen) allTableEnums() []*ir.Enum {
	seen := map[string]*ir.Enum{}
	unions := map[string]bool{}
	var visit func(*ir.Field)
	visit = func(f *ir.Field) {
		if e := enumOf(f); e != nil {
			seen[e.Name] = e
		}
		if f.KeyEnumRef != nil {
			seen[f.KeyEnum] = f.KeyEnumRef
		}
		if u := unionOf(f); u != nil && !unions[u.Name] {
			unions[u.Name] = true
			for _, v := range u.Variants {
				if v.F != nil {
					visit(v.F)
				}
			}
		}
	}
	for _, file := range g.unit.Files {
		for _, st := range file.Tables {
			for _, f := range st.Fields {
				visit(f)
			}
		}
		for _, d := range file.Decls {
			if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
				for _, f := range st.Fields {
					visit(f)
				}
			}
		}
	}
	var out []*ir.Enum
	for name, e := range seen {
		if g.unit.DeclFile[name] == g.file.Base {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (g *gen) emitUnionStorage(u *ir.Union) {
	g.pf("#[derive(Clone, Copy, PartialEq, Debug, Default)]\npub enum %s {\n    #[default]\n    None,\n", u.Name)
	for _, v := range u.Variants {
		if v.Void() {
			g.pf("    %s,\n", ir.GoExportName(v.Name))
		} else {
			g.pf("    %s(%s),\n", ir.GoExportName(v.Name), armType(v.F))
		}
	}
	g.pf("}\n\n")
	g.pf("#[repr(transparent)]\n#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]\npub struct %sType(pub %s);\nimpl %sType {\n    pub const NONE: Self = Self(0);\n", u.Name, rustUint(u.StorageBits), u.Name)
	for i, v := range u.Variants {
		g.pf("    pub const %s: Self = Self(%d);\n", ir.RustConstName(v.Name), i+1)
	}
	g.pf("    pub const MAX: Self = Self(%d);\n    pub const COUNT: Self = Self(%d);\n}\n\n", u.Max, len(u.Variants))
	g.pf("pub fn enum_name_%s(value: %sType) -> &'static str { match value.0 {\n0 => \"None\",\n", ir.RustSnake(u.Name+"Type"), u.Name)
	for i, v := range u.Variants {
		g.pf("%d => %q,\n", i+1, ir.GoExportName(v.Name))
	}
	g.pf("_ => \"???\",\n} }\n")
}
func (g *gen) emitUnionWire(u *ir.Union) {
	g.pf("pub fn %s(w: &mut TableWriter, value: &%s) -> bool {\n    match value {\n        %s::None => w.putleb(0),\n", fn(u.Name, "save_union"), u.Name, u.Name)
	for _, v := range u.Variants {
		pat := "(arm)"
		if v.Void() {
			pat = ""
		}
		g.pf("        %s::%s%s => {\n            w.putid(0x%016x);\n            w.put8(%d);\n            if !w.framed(|w| {\n", u.Name, ir.GoExportName(v.Name), pat, ir.TableWireId(v.WireName()), armKind(v))
		if v.Void() {
			g.pf("let _ = w;\n")
		} else {
			g.emitSaveArm(v.F)
		}
		g.pf("                !w.overflow\n            }) { return false; }\n        }\n")
	}
	g.pf("    }\n    !w.overflow\n}\n\n")
	g.pf("pub fn %s(r: &mut TableReader, report: &mut TableReport, value: &mut %s, element: bool) -> bool {\n", fn(u.Name, "load_union"), u.Name)
	g.pf("    let id = match r.getid() { Some(id) => id, None => { report.malformed = true; return false; } };\n    if element { *value = %s::None; }\n    let id = match id { Some(id) => id, None => { *value = %s::None; return true; } };\n    if !r.has(1) { report.malformed = true; return false; }\n    let kind = r.get8();\n", u.Name, u.Name)
	g.takeBody("r", "body", readBad)
	g.pf("*value = %s::None;\nmatch id {\n", u.Name)
	for _, v := range u.Variants {
		g.pf("0x%016x => {\n", ir.TableWireId(v.WireName()))
		if v.F != nil && v.F.Array == ir.ArrayNone && widenable(v.F) {
			g.pf("if kind != %d && !TableReader::widens(kind, %d) { report.kind_mismatch += 1; return true; }\n", armKind(v), armKind(v))
		} else {
			g.pf("if kind != %d { report.kind_mismatch += 1; return true; }\n", armKind(v))
		}
		if v.Void() {
			g.pf("if !body.buffer.is_empty() { report.malformed = true; } else { *value = %s::%s; }\n", u.Name, ir.GoExportName(v.Name))
		} else {
			if v.F.Array == ir.ArrayNone && (ir.ArmWireFixedWidth(v.F) > 0 || isEnum(v.F)) {
				g.pf("let arm;\n")
			} else {
				g.pf("let mut arm = %s;\n", armZero(v.F))
			}
			g.emitLoadArm(v.F)
			g.pf("*value = %s::%s(arm);\n", u.Name, ir.GoExportName(v.Name))
		}
		g.pf("}\n")
	}
	g.pf("_ => { report.unknown += 1; }\n}\ntrue\n}\n\n")
}
func (g *gen) emitSaveArm(f *ir.Field) {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitSaveElement(f, "*arm", false)
	case f.Type.Kind == ir.TWString:
		g.pf("if arm.length < 0 || arm.length > %d { return false; }\nfor &unit in &arm.value[..arm.length as usize] { w.put16(unit); }\n", f.Type.Size)
	case f.Type.Kind == ir.TString:
		g.pf("if arm.length < 0 || arm.length > %d { return false; }\nw.raw(&arm.value[..arm.length as usize]);\n", f.Type.Size)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		count, expr := fmt.Sprint(bound), "arm[i]"
		if armCounted(f) {
			count, expr = "arm.length as usize", "arm.value[i]"
			g.pf("if arm.length < 0 || arm.length > %d { return false; }\n", bound)
		}
		g.pf("w.put8(%d);\nw.putleb((%s) as u64);\nfor i in 0..%s {\n", ir.TableWireElemKind(f), count, count)
		g.emitSaveElement(f, expr, true)
		g.pf("}\n")
	default:
		g.emitSaveElement(f, "*arm", false)
	}
}
func (g *gen) emitLoadArm(f *ir.Field) {
	bad := "report.malformed = true; return true;"
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitLoadElement(f, "body", "arm", false, bad)
		g.pf("if body.offset != body.buffer.len() { %s }\n", bad)
	case f.Type.Kind == ir.TWString:
		g.pf("arm.length = match body.utf16(&mut arm.value) { Some(keep) => keep as i32, None => { %s } };\nif body.buffer.len()/2 > %d { report.clamped += 1; }\n", bad, f.Type.Size)
	case f.Type.Kind == ir.TString:
		g.pf("if body.buffer.contains(&0) || core::str::from_utf8(body.buffer).is_err() { %s }\nlet mut keep = body.buffer.len();\nif keep > %d { keep = %d; while keep > 0 && body.buffer[keep] & 0xc0 == 0x80 { keep -= 1; } report.clamped += 1; }\narm.value[..keep].copy_from_slice(&body.buffer[..keep]);\narm.length = keep as i32;\n", bad, f.Type.Size, f.Type.Size)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		target := "arm[i]"
		if armCounted(f) {
			target = "arm.value[i]"
		}
		g.pf("if body.has(2) {\nlet element_kind = body.get8();\nlet count = match body.getleb() { Some(n) => n, None => { %s } };\n", bad)
		k := ir.TableWireElemKind(f)
		if widenable(f) {
			g.pf("if element_kind != %d && !TableReader::widens(element_kind, %d) { report.kind_mismatch += 1; return true; }\nif element_kind != %d { report.widened += 1; }\n", k, k, k)
		} else {
			g.pf("if element_kind != %d { report.kind_mismatch += 1; return true; }\n", k)
		}
		g.pf("let keep = count.min(%d) as usize;\nif count > %d { report.clamped += 1; }\nfor i in 0..keep {\n", bound, bound)
		if widenable(f) {
			g.pf("if element_kind != %d {\n", k)
			g.emitWidenedScalar(f, "body", target, "element_kind", "report.malformed = true; break;")
			g.pf("} else {\n")
		}
		g.emitLoadElement(f, "body", target, true, "report.malformed = true; break;")
		if widenable(f) {
			g.pf("}\n")
		}
		if armCounted(f) {
			g.pf("arm.length = (i + 1) as i32;\n")
		}
		g.pf("}\n}\n")
	default:
		if w := ir.ArmWireFixedWidth(f); w > 0 {
			if widenable(f) {
				g.pf("if body.buffer.len() != TableReader::width(kind) { %s }\nif kind != %d {\n", bad, ir.TableWireScalarKind(f))
				g.emitWidenedScalar(f, "body", "arm", "kind", bad)
				g.pf("report.widened += 1;\n} else {\n")
			} else {
				g.pf("if body.buffer.len() != %d { %s }\n", w, bad)
			}
		}
		g.emitLoadElement(f, "body", "arm", false, bad)
		if ir.ArmWireFixedWidth(f) > 0 && widenable(f) {
			g.pf("}\n")
		}
		if isEnum(f) || isStruct(f) {
			g.pf("if body.offset != body.buffer.len() { %s }\n", bad)
		}
	}
}
