package rusttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitLoad(st *ir.Struct) {
	g.pf("#[inline(always)]\npub fn %s(r: &mut TableReader, report: &mut TableReport, value: &mut %s) -> bool {\n", fn(st.Name, "load_body"), st.Name)
	g.pf("    %s(value);\n    loop {\n        if r.builder_refused(){return false;}\n", fn(st.Name, "reset"))
	g.pf("        let field_id = match r.getid() {\n            Some(None) => return true,\n            Some(Some(id)) => id,\n            None => { report.malformed = true; return false; }\n        };\n")
	g.pf("        if r.reserved(field_id) || !r.has(1) { report.malformed = true; return false; }\n        let kind = r.get8();\n        match field_id {\n")
	for _, f := range st.Fields {
		g.pf("            0x%016x => { // %s\n", ir.TableFieldWireId(f), f.Name)
		g.pf("                if kind != %d {\n", ir.TableWireFieldKind(f))
		if f.Array == ir.ArrayNone && f.KeyEnum == "" && widenable(f) {
			g.pf("if TableReader::widens(kind, %d) {\n", ir.TableWireFieldKind(f))
			g.emitWidenedScalar(f, "r", "value."+f.Name, "kind", readBad)
			g.pf("report.widened += 1;\n")
			if f.Type.Optional {
				g.pf("value.%s_present = true;\n", f.Name)
			}
			g.pf("} else {\n")
		}
		g.pf("report.kind_mismatch += 1;\nif !r.skip(kind) { report.malformed = true; return false; }\n")
		if f.Array == ir.ArrayNone && f.KeyEnum == "" && widenable(f) {
			g.pf("}\n")
		}
		g.pf("} else {\n")
		g.emitLoadPayload(f)
		if f.Type.Optional {
			g.pf("                    value.%s_present = true;\n", f.Name)
		}
		g.pf("                }\n            }\n")
	}
	if g.variable[st.Name] {
		g.pf("            u64::MAX if r.is_root() => { if !r.skip(kind) { report.malformed=true; return false; } }\n")
	}
	g.pf("            _ => {\n                report.unknown += 1;\n                if !r.skip(kind) { report.malformed = true; return false; }\n            }\n        }\n    }\n}\n\n")
	if g.variable[st.Name] {
		return
	}
	g.pf("pub fn %s(value: &mut %s, bytes: &[u8], report: &mut TableReport) -> TableOpenVerdict {\n", fn(st.Name, "load_verdict"), st.Name)
	g.pf("    let mut r = match TableReader::open(bytes) {\n        Ok(r) => r,\n        Err(verdict) => {\n            %s(value);\n            report.verdict = verdict;\n            if verdict == TableOpenVerdict::Damaged { report.malformed = true; }\n            return verdict;\n        }\n    };\n", fn(st.Name, "reset"))
	g.pf("    report.verdict = if %s(&mut r, report, value) { TableOpenVerdict::Ok } else { TableOpenVerdict::BodyStopped };\n    report.verdict\n}\n\n", fn(st.Name, "load_body"))
	g.pf("pub fn %s(value: &mut %s, bytes: &[u8], report: &mut TableReport) -> bool {\n    %s(value, bytes, report) == TableOpenVerdict::Ok\n}\n\n", fn(st.Name, "load"), st.Name, fn(st.Name, "load_verdict"))
}

const readBad = "report.malformed = true; return false;"

func (g *gen) takeBody(reader, name, onBad string) {
	g.pf("let mut %s = match %s.take() { Some(r) => r, None => { %s } };\n", name, reader, onBad)
}

func (g *gen) emitLoadPayload(f *ir.Field) {
	switch {
	case f.IsList() || f.IsMap():
		g.pf("if !unsafe{table_sequence_load(r,report,&mut value.%s as *mut _ as *mut u8,%s())}{return false;}\n", f.Name, sequenceName(g.owner, f))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitLoadElement(f, "r", "value."+f.Name, true, readBad)
	case f.Type.Kind == ir.TWString:
		g.pf("let text = match r.take() { Some(text) => text, None => { %s } };\n", readBad)
		g.pf("match text.utf16(&mut value.%s) { Some(keep) => { value.%s_length = keep as i32; if text.buffer.len()/2 > %d { report.clamped += 1; } }, None => { report.malformed = true; value.%s_length = 0; } }\n", f.Name, f.Name, f.Type.Size, f.Name)
	case f.Type.Kind == ir.TString:
		g.pf("let text = match r.take() { Some(r) => r, None => { %s } };\n", readBad)
		g.pf("if text.buffer.contains(&0) || core::str::from_utf8(text.buffer).is_err() {\n    report.malformed = true;\n")
		g.emitResetField(f)
		g.pf("} else {\n")
		g.pf("    let mut keep = text.buffer.len();\n    if keep > %d {\n        keep = %d;\n        while keep > 0 && text.buffer[keep] & 0xc0 == 0x80 { keep -= 1; }\n        report.clamped += 1;\n    }\n", f.Type.Size, f.Type.Size)
		g.pf("    value.%s[..keep].copy_from_slice(&text.buffer[..keep]);\n    value.%s_length = keep as i32;\n}\n", f.Name, f.Name)
	case f.KeyEnum != "":
		g.emitLoadKeyed(f)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitLoadArray(f)
	default:
		g.emitLoadElement(f, "r", "value."+f.Name, true, readBad)
	}
}

func (g *gen) emitLoadElement(f *ir.Field, r, target string, framed bool, onBad string) {
	switch {
	case f.Type.Pointer:
		g.pf("let index = match %s.getleb() { Some(index)=>index,None=>{ %s } };\n%s.reference(&mut %s,index,report);\n", r, onBad, r, target)
	case isStruct(f):
		if framed {
			g.takeBody(r, "element", onBad)
			r = "element"
		}
		g.pf("%s(&mut %s, report, &mut %s);\nif %s.builder_refused(){return false;}\n", fn(f.Type.Name, "load_body"), r, target, r)
		g.pf("if %s.offset != %s.buffer.len() { report.malformed = true; %s(&mut %s); }\n", r, r, fn(f.Type.Name, "reset"), target)
	case isEnum(f):
		g.pf("%s = match %s.getid() {\n    Some(None) => %s::NONE,\n    Some(Some(id)) => match %s::table_value(id) { Some(v) => v, None => { report.unknown += 1; %s::NONE } },\n    None => { %s }\n};\n", target, r, f.Type.Name, f.Type.Name, f.Type.Name, onBad)
	case isUnion(f):
		g.emitLoadUnion(f, r, target, onBad)
	default:
		kind := ir.TableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = ir.TableKindU8
		}
		g.pf("if !%s.has(%d) { %s }\n", r, tableKindWidth(kind), onBad)
		g.emitScalarDecode(f, kind, r, target)
	}
}

func (g *gen) emitLoadArray(f *ir.Field) {
	bound := f.ArrayBound
	countField := ""
	if f.Array == ir.ArrayCounted {
		countField = f.Name + "_count"
	}
	if f.Type.Kind == ir.TBytes {
		bound = f.Type.Size
		countField = f.Name + "_length"
	}
	g.emitArrayHeader()
	g.emitElementKindCheck(f, ir.TableWireElemKind(f))
	g.pf("let keep = count.min(%d) as usize;\nif count > %d { report.clamped += 1; }\nlet mut decoded_count = 0;\nfor i in 0..keep {\n", bound, bound)
	if widenable(f) {
		g.pf("if element_kind != %d {\n", ir.TableWireElemKind(f))
		g.emitWidenedScalar(f, "array", "value."+f.Name+"[i]", "element_kind", "report.malformed = true; break;")
		g.pf("} else {\n")
	}
	g.emitLoadElement(f, "array", "value."+f.Name+"[i]", true, "report.malformed = true; break;")
	if widenable(f) {
		g.pf("}\n")
	}
	g.pf("decoded_count = i + 1;\n}\n")
	if countField != "" {
		g.pf("value.%s = decoded_count as i32;\n", countField)
	} else {
		g.pf("let _ = decoded_count;\n")
	}
	g.pf("}\n} else { report.malformed = true; }\n}\n")
}

func (g *gen) emitLoadKeyed(f *ir.Field) {
	g.emitArrayHeader()
	g.emitElementKindCheck(f, ir.TableWireScalarKind(f))
	g.pf("for _ in 0..count {\nlet key = match array.getid() { Some(Some(id)) => id, _ => { report.malformed = true; break; } };\n")
	g.takeBody("array", "element", "report.malformed = true; break;")
	g.pf("let slot = match %s::table_value(key) { Some(k) => k.0 as usize - 1, None => { report.unknown += 1; continue; } };\n", f.KeyEnum)
	if widenable(f) {
		g.pf("if element_kind != %d {\n", ir.TableWireScalarKind(f))
		g.emitWidenedScalar(f, "element", fmt.Sprintf("%s[slot]", g.keyedSlots(f)), "element_kind", "report.malformed = true; continue;")
		g.pf("} else {\n")
	}
	g.emitLoadElement(f, "element", fmt.Sprintf("%s[slot]", g.keyedSlots(f)), false, "report.malformed = true; continue;")
	if widenable(f) {
		g.pf("}\n")
	}
	g.pf("}\n}\n} else { report.malformed = true; }\n}\n")
}

func (g *gen) emitLoadUnion(f *ir.Field, r, target, onBad string) {
	if r == "r" {
		r = "*r"
	}
	g.pf("if !%s(&mut %s, report, &mut %s, %v) { if (%s).builder_refused(){return false;} %s }\n", fn(f.Type.Name, "load_union"), r, target, f.Array != ir.ArrayNone, r, onBad)
}

func (g *gen) emitElementKindCheck(f *ir.Field, kind int) {
	mismatch := "report.kind_mismatch += 1;"
	if f.Type.Optional {
		mismatch += " continue;"
	}
	if widenable(f) {
		g.pf("if element_kind != %d && !TableReader::widens(element_kind, %d) { %s } else {\n", kind, kind, mismatch)
		g.pf("if element_kind != %d { report.widened += 1; }\n", kind)
	} else {
		g.pf("if element_kind != %d { %s } else {\n", kind, mismatch)
	}
}

// The reference parses the header against the enclosing body, then bounds
// every element by L. In particular, a foreign element kind still counts if
// a canonical count ends beyond L; no element may consume those extra bytes.
func (g *gen) emitArrayHeader() {
	g.takeBody("r", "array", readBad)
	g.pf("if array.has(2) {\nlet mut header = *r;\nheader.offset -= array.buffer.len();\nlet start = header.offset;\nlet element_kind = header.get8();\nlet count = header.getleb();\narray.offset = (header.offset - start).min(array.buffer.len());\nif let Some(count) = count {\n")
}
