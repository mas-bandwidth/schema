package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func sequenceSlotType(f *ir.Field) string {
	if f.IsMap() {
		return "TableMap"
	}
	return "TableList"
}
func mapValueType(f *ir.Field) string {
	if f.IsList() || f.IsMap() {
		return sequenceSlotType(f) + "<" + sequenceType(f) + ">"
	}
	if f.KeyEnum != "" {
		return keyedType(f)
	}
	if f.Array != ir.ArrayNone {
		return fmt.Sprintf("[%s; %d]", rustFieldType(f.Type), f.ArrayBound)
	}
	if !f.Type.Pointer {
		switch f.Type.Kind {
		case ir.TString, ir.TBytes:
			return fmt.Sprintf("[u8; %d]", f.Type.Size)
		case ir.TWString:
			return fmt.Sprintf("[u16; %d]", f.Type.Size)
		}
	}
	return rustFieldType(f.Type)
}
func (g *gen) emitMapEntryAPI(f *ir.Field) {
	value := ir.MapValueField(f)
	typ := mapValueType(value)
	borrow := "&'a mut " + typ
	expr := "&mut self.value"
	if armCounted(value) {
		companion := "value_length"
		if value.Array == ir.ArrayCounted {
			companion = "value_count"
		}
		borrow = "(" + borrow + ", &'a mut i32)"
		expr = "(&mut self.value, &mut self." + companion + ")"
	}
	g.pf("unsafe impl TableMapEntry for %s {type ValueMut<'a>=%s;fn sequence_info()->&'static TableSequenceInfo{%s()} fn value_mut(&mut self)->Self::ValueMut<'_>{%s}}\n", f.MapEntry.Name, borrow, sequenceName(g.owner, f), expr)
}
func sequenceElement(f *ir.Field) ir.Field {
	element := *f
	element.Array = ir.ArrayNone
	element.KeyEnum = ""
	element.Type.Optional = false
	if f.IsMap() {
		element.MapEntry = nil
		element.Type = ir.FieldType{Kind: ir.TNamed, Name: f.MapEntry.Name, Ref: f.MapEntry}
	}
	return element
}
func sequenceType(f *ir.Field) string {
	element := sequenceElement(f)
	return rustFieldType(element.Type)
}
func sequenceName(st *ir.Struct, f *ir.Field) string { return fn(st.Name, f.Name+"_sequence") }
func (g *gen) emitSequences(st *ir.Struct) {
	for _, f := range st.Fields {
		if !f.IsList() && !f.IsMap() {
			continue
		}
		name, typ := sequenceName(st, f), sequenceType(f)
		element := sequenceElement(f)
		g.pf("pub fn %s() -> &'static TableSequenceInfo {\n", name)
		g.pf("unsafe fn save(w:&mut TableWriter,p:*const u8)->bool {unsafe{let value=&*(p as *const %s);\n", typ)
		expr := "(*value)"
		if element.Type.Kind == ir.TFloat32 || element.Type.Kind == ir.TFloat64 {
			expr = "*value"
		}
		if _, flags := element.Type.Ref.(*ir.Flags); flags {
			expr = "*value"
		}
		g.emitSaveElement(&element, expr, true)
		g.pf("!w.overflow }}\n")
		g.pf("unsafe fn load(r:&mut TableReader,report:&mut TableReport,p:*mut u8,kind:u8)->bool {unsafe{let value=&mut*(p as *mut %s);let _=kind;\n", typ)
		if widenable(&element) {
			g.pf("if kind!=%d {\n", ir.TableWireElemKind(f))
			g.emitWidenedScalar(&element, "r", "(*value)", "kind", readBad)
			g.pf("} else {\n")
		}
		decodeElement := element
		decodeElement.Array = ir.ArrayList
		g.emitLoadElement(&decodeElement, "r", "(*value)", true, readBad)
		if widenable(&element) {
			g.pf("}\n")
		}
		g.pf("true }}\n")
		for _, mutable := range []bool{false, true} {
			if mutable {
				g.pf("unsafe fn rewrite(region_base:*mut u8,p:*mut u8,visit:&mut dyn FnMut(&mut i64,&'static TableTypeInfo)){unsafe{let value=&mut*(p as *mut %s);let _=(&value,&visit,region_base);\n", typ)
			} else {
				g.pf("unsafe fn visit_refs(context:TableContext,p:*const u8,visit:&mut dyn FnMut(*const i64,&'static TableTypeInfo)){unsafe{let value=&*(p as *const %s);let _=(&value,&visit,context);\n", typ)
			}
			g.emitRefWalk(&element, "(*value)", mutable)
			g.pf("}}\n")
		}
		extent := "None"
		if hasExtentField(&element) {
			g.pf("fn extent(body:TableReader,at:&mut usize)->core::result::Result<(),TableRefuseReason>{\n")
			g.emitInnerExtent(&element, "body")
			g.pf("Ok(())}\n")
			extent = "Some(extent)"
		}
		floor := ir.TableKindWidth(ir.TableWireElemKind(f))
		if element.Type.Pointer {
			floor = 1
		}
		if floor == 0 {
			floor = 1
			if isStruct(&element) && !element.Type.Pointer {
				floor = 2
			}
		}
		mapEntry := "None"
		if f.IsMap() {
			mapEntry = "Some(" + fn(f.MapEntry.Name, "table_type") + ")"
		}
		g.pf("static INFO:TableSequenceInfo=TableSequenceInfo {map_entry:%s,type_id:core::any::TypeId::of::<%s>,size:table_sequence_size::<%s>(),align:core::mem::align_of::<%s>(),kind:%d,floor:%d,initialize:|p|unsafe{(p as *mut %s).write(<%s>::default());},save,load,visit:visit_refs,rewrite,extent:%s}; &INFO }\n", mapEntry, typ, typ, typ, ir.TableWireElemKind(f), floor, typ, typ, extent)
		if f.IsMap() {
			g.emitMapEntryAPI(f)
		}
	}
}
func hasExtentStruct(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if hasExtentField(f) {
			return true
		}
	}
	return false
}
func hasExtentField(f *ir.Field) bool {
	if f.IsList() || f.IsMap() {
		return true
	}
	if f.Type.Pointer {
		return false
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return hasExtentStruct(st)
	}
	if u, ok := f.Type.Ref.(*ir.Union); ok {
		for _, v := range u.Variants {
			if v.F != nil && hasExtentField(v.F) {
				return true
			}
		}
	}
	return false
}
func (g *gen) emitWireExtent(st *ir.Struct) {
	if !hasExtentStruct(st) {
		return
	}
	g.pf("pub fn %s(mut r:TableReader,at:&mut usize)->core::result::Result<(),TableRefuseReason> {while let Some(Some(id))=r.getid(){if !r.has(1){break;}let kind=r.get8();match id {\n", fn(st.Name, "wire_extent"))
	for _, f := range st.Fields {
		if !hasExtentField(f) {
			continue
		}
		take := "r.take()"
		if isUnion(f) && f.Array == ir.ArrayNone {
			take = "r.extent_element(15)"
		}
		g.pf("0x%016x if kind==%d=>{let Some(body)=%s else{break;};\n", ir.TableFieldWireId(f), ir.TableWireFieldKind(f), take)
		if f.IsList() || f.IsMap() {
			g.pf("table_sequence_extent(body,at,%s())?;\n", sequenceName(st, f))
		} else {
			g.emitInnerExtent(f, "body")
		}
		g.pf("}\n")
	}
	g.pf("_=>{if !r.skip(kind){break;}},\n}}Ok(())}\n")
}
func (g *gen) emitInnerExtent(f *ir.Field, body string) {
	if f.Type.Pointer {
		return
	}
	if f.Array != ir.ArrayNone {
		g.pf("let mut array=%s;if array.has(2)&&array.get8()==%d {if let Some(n)=array.getleb(){for _ in 0..n {\n", body, ir.TableWireElemKind(f))
		if f.KeyEnum != "" {
			g.pf("if array.getleb().is_none(){break;}\n")
		}
		if f.KeyEnum != "" {
			g.pf("let Some(element)=array.take() else{break;};\n")
		} else {
			g.pf("let Some(element)=array.extent_element(%d) else{break;};\n", ir.TableWireElemKind(f))
		}
		clone := *f
		clone.Array = ir.ArrayNone
		clone.KeyEnum = ""
		g.emitInnerExtent(&clone, "element")
		g.pf("}}}\n")
		return
	}
	if isStruct(f) {
		g.pf("%s(%s,at)?;\n", fn(f.Type.Name, "wire_extent"), body)
		return
	}
	if u := unionOf(f); u != nil {
		g.pf("let mut arm=%s;if let Some(Some(id))=arm.getid(){if arm.has(1){let kind=arm.get8();if let Some(payload)=arm.take(){match id {\n", body)
		for _, v := range u.Variants {
			if v.F != nil && hasExtentField(v.F) {
				g.pf("0x%016x if kind==%d=>{\n", ir.TableWireId(v.WireName()), armKind(v))
				g.emitInnerExtent(v.F, "payload")
				g.pf("}\n")
			}
		}
		g.pf("_=>{},}}}}\n")
	}
}
