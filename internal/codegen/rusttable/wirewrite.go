package rusttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Measure runs the same walk into a counting sink. Every framed payload is
// measured with the current vocabulary, then replayed from the same mark:
// speculative references never leak into first-use order or an elided field.
func (g *gen) emitMeasure(st *ir.Struct) {
	g.pf("pub fn %s(value: &%s) -> i64 {\n", fn(st.Name, "measure"), st.Name)
	g.pf("    let mut ids = [0u64; %d];\n", ir.TableWireIdCapacity(g.unit))
	g.pf("    let mut w = TableWriter::new(None, &mut ids);\n")
	g.pf("    w.put8(1);\n    if !%s(&mut w, value) { return -1; }\n", fn(st.Name, "save_body"))
	g.pf("    w.finish()\n}\n\n")
}

func (g *gen) emitSave(st *ir.Struct) {
	g.pf("#[inline(always)]\npub fn %s(w: &mut TableWriter, value: &%s) -> bool {\n", fn(st.Name, "save_body"), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    let _ = value;\n")
	}
	guards := guardExprs(st)
	for _, f := range st.Fields {
		if cond, ok := guards[f.Name]; ok {
			g.pf("    if %s {\n", cond)
			g.indent = "    "
			g.emitSaveField(f)
			g.indent = ""
			g.pf("    }\n")
		} else {
			g.emitSaveField(f)
		}
	}
	g.pf("    w.putleb(0);\n    !w.overflow\n}\n\n")
	g.pf("pub fn %s(value: &%s, buffer: &mut [u8]) -> i64 {\n", fn(st.Name, "save"), st.Name)
	g.pf("    let mut ids = [0u64; %d];\n", ir.TableWireIdCapacity(g.unit))
	g.pf("    let mut w = TableWriter::new(Some(buffer), &mut ids);\n")
	g.pf("    w.put8(1);\n    if !%s(&mut w, value) { return -1; }\n", fn(st.Name, "save_body"))
	g.pf("    w.finish()\n}\n\n")
}

func (g *gen) emitSaveField(f *ir.Field) {
	// Validate live counts before slicing, in both the measure and save walks.
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		g.pf("    if value.%s_length < 0 || value.%s_length > %d { return false; }\n", f.Name, f.Name, f.Type.Size)
	case f.Array == ir.ArrayCounted:
		g.pf("    if value.%s_count < 0 || value.%s_count > %d { return false; }\n", f.Name, f.Name, f.ArrayBound)
	}
	cond := g.nonDefaultTest(f)
	switch {
	case f.Type.Optional:
		cond = "value." + f.Name + "_present"
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		cond = "value." + f.Name + "_length > 0"
		if len(f.DefBytes) > 0 {
			cond = fmt.Sprintf("value.%s_length != %d || value.%s[..value.%s_length as usize] != %s", f.Name, len(f.DefBytes), f.Name, f.Name, rustBytes(f.DefBytes))
		}
	case f.KeyEnum != "":
		cond = "pairs > 0"
		g.pf("    {\n    let mut pairs = 0u64;\n    for i in 0..%s {\n", arrayLen(f))
		g.emitKeyedSlotSkip(f)
		g.pf("        pairs += 1;\n    }\n")
	case f.Array == ir.ArrayCounted:
		cond = "value." + f.Name + "_count > 0"
	case f.Array == ir.ArrayFixed:
		if isStruct(f) {
			cond = "true"
		} else {
			cond = fmt.Sprintf("value.%s.iter().any(|v| *v != %s)", f.Name, g.arrayElementDefault(f))
		}
	case isUnion(f):
		cond = "value." + f.Name + " != " + f.Type.Name + "::None"
	case isStruct(f):
		g.pf("    let size = %s(&value.%s);\n    if size < 0 { return false; }\n", fn(f.Type.Name, "measure"), f.Name)
		cond = "size > 10" // empty form: form + terminator + zero entry count
	}
	g.pf("    if %s {\n", cond)
	g.pf("        w.putid(0x%016x);\n        w.put8(%d); // %s\n", ir.TableFieldWireId(f), ir.TableWireFieldKind(f), f.Name)
	switch {
	case f.Type.Kind == ir.TWString:
		g.pf("w.putleb(value.%s_length as u64 * 2);\nfor &unit in &value.%s[..value.%s_length as usize] { w.put16(unit); }\n", f.Name, f.Name, f.Name)
	case f.Type.Kind == ir.TString:
		g.pf("        w.putleb(value.%s_length as u64);\n        w.raw(&value.%s[..value.%s_length as usize]);\n", f.Name, f.Name, f.Name)
	case f.KeyEnum != "":
		g.pf("        if !w.framed(|w| {\n            w.put8(%d);\n            w.putleb(pairs);\n            for i in 0..%s {\n", ir.TableWireScalarKind(f), arrayLen(f))
		g.emitKeyedSlotSkip(f)
		g.pf("                match %s.table_id() { Some(id) => w.putid(id), None => return false }\n", keyOfSlot(f, "i"))
		g.pf("                if !w.framed(|w| {\n")
		g.emitSaveElement(f, fmt.Sprintf("%s[i]", g.keyedSlots(f)), false)
		g.pf("                    !w.overflow\n                }) { return false; }\n            }\n            !w.overflow\n        }) { return false; }\n")
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		count := fmt.Sprint(f.ArrayBound)
		if f.Type.Kind == ir.TBytes {
			count = "value." + f.Name + "_length as usize"
		} else if f.Array == ir.ArrayCounted {
			count = "value." + f.Name + "_count as usize"
		}
		g.pf("        if !w.framed(|w| {\n            w.put8(%d);\n            w.putleb((%s) as u64);\n            for i in 0..%s {\n", ir.TableWireElemKind(f), count, count)
		g.emitSaveElement(f, "value."+f.Name+"[i]", true)
		g.pf("            }\n            !w.overflow\n        }) { return false; }\n")
	default:
		g.emitSaveElement(f, "value."+f.Name, true)
	}
	g.pf("    }\n")
	if f.KeyEnum != "" {
		g.pf("    }\n")
	}
}

func (g *gen) emitSaveElement(f *ir.Field, expr string, framed bool) {
	if expr == "*arm" && isEnum(f) {
		expr = "(*arm)"
	}
	switch {
	case isStruct(f):
		if framed {
			g.pf("        if !w.framed(|w| %s(w, &%s)) { return false; }\n", fn(f.Type.Name, "save_body"), expr)
		} else {
			g.pf("        if !%s(w, &%s) { return false; }\n", fn(f.Type.Name, "save_body"), expr)
		}
	case isEnum(f):
		g.pf("        match %s.table_id() { Some(id) => {\n            if %s.0 == 0 { w.putleb(0); } else { w.putid(id); }\n        }, None => return false }\n", expr, expr)
	case isUnion(f):
		g.pf("if !%s(w, &(%s)) { return false; }\n", fn(f.Type.Name, "save_union"), expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("        w.put8(%s);\n", expr)
	default:
		g.pf("        w.%s(%s);\n", putFn(tableKindWidth(ir.TableScalarKind(f))), g.scalarToWire(f, expr))
	}
}

func (g *gen) emitKeyedSlotSkip(f *ir.Field) {
	if isStruct(f) {
		g.pf("        let size = %s(&%s[i]);\n        if size < 0 { return false; }\n        if size == 10 { continue; }\n", fn(f.Type.Name, "measure"), g.keyedSlots(f))
	} else {
		g.pf("        if %s[i] == %s { continue; }\n", g.keyedSlots(f), zeroScalar(f))
	}
}
