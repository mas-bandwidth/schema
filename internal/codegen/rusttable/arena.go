package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitRecordRuntime(st *ir.Struct) {
	g.emitSequences(st)
	g.emitWireExtent(st)
	g.pf("unsafe impl TableRecord for %s { fn table_info() -> &'static TableTypeInfo { %s() } }\n", st.Name, fn(st.Name, "table_type"))
	for _, mutable := range []bool{false, true} {
		verb, mut, slot := "visit_refs", "", "*const i64"
		if mutable {
			verb, mut, slot = "rewrite_refs", "mut ", "&mut i64"
		}
		prefix := "context: TableContext, "
		if mutable {
			prefix = "region_base: *mut u8, "
		}
		g.pf("unsafe fn %s(%sstorage: *%su8, visit: &mut dyn FnMut(%s, &'static TableTypeInfo)) { unsafe {\nlet value = &%s*(storage as *%s%s);\nlet _ = (&value, &visit);\n", fn(st.Name, verb), prefix, map[bool]string{false: "const ", true: "mut "}[mutable], slot, mut, map[bool]string{false: "const ", true: "mut "}[mutable], st.Name)
		if mutable {
			g.pf("let _ = region_base;\n")
		} else {
			g.pf("let _ = context;\n")
		}
		for _, f := range st.Fields {
			if !f.IsList() && !f.IsMap() && !f.Type.Pointer && !(isStruct(f) && g.variable[f.Type.Name]) && !(isUnion(f) && unionHasRefs(unionOf(f), g.variable)) {
				continue
			}
			guard := guardExprs(st)[f.Name]
			if f.Type.Optional {
				if guard != "" {
					guard = "(" + guard + ") && "
				}
				guard += "value." + f.Name + "_present"
			}
			if guard != "" && !mutable {
				g.pf("if %s {\n", guard)
			}
			g.emitRefWalk(f, "value."+f.Name, mutable)
			if guard != "" && !mutable {
				g.pf("}\n")
			}
		}
		g.pf("} }\n")
	}
}
func (g *gen) emitRefWalk(f *ir.Field, expr string, mutable bool) {
	if !f.IsList() && !f.IsMap() && !f.Type.Pointer && !(isStruct(f) && g.variable[f.Type.Name]) && !(isUnion(f) && unionHasRefs(unionOf(f), g.variable)) {
		return
	}
	mut, verb := "", "visit_refs"
	if mutable {
		mut, verb = "mut ", "rewrite_refs"
	}
	cast := "const"
	if mutable {
		cast = "mut"
	}
	switch {
	case f.IsList() || f.IsMap():
		typ := sequenceType(f)
		if f.IsMap() && mutable {
			expr += ".storage"
		}
		clone := sequenceElement(f)
		if mutable {
			g.pf("if %s.value!=0 { let elements=region_base.add(%s.value as usize) as *mut %s; for i in 0..%s.count as usize {let _=i;\n", expr, expr, typ, expr)
			g.emitRefWalk(&clone, "(*elements.add(i))", true)
			g.pf("} %s.value=elements as i64 - (&%s.value as *const i64) as i64; }\n", expr, expr)
		} else {
			if f.IsMap() {
				g.pf("if let Some(elements)=table_map_order(context,&%s as *const _ as *const u8,%s()) {for p in elements {let element=&*(p as *const %s);let _=element;\n", expr, sequenceName(g.owner, f), typ)
			} else {
				g.pf("if let Some(elements)=context.list(&%s) {for element in elements {let _=element;\n", expr)
			}
			g.emitRefWalk(&clone, "(*element)", false)
			g.pf("}}\n")
		}
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("visit(&%s%s.value,%s());\n", mut, expr, pointeeInfo(f.Type))
	case f.Array != ir.ArrayNone:
		count := fmt.Sprint(f.ArrayBound)
		if f.KeyEnum != "" {
			count = arrayLen(f)
			expr += ".slots"
		} else if f.Array == ir.ArrayCounted {
			count = fmt.Sprintf("(%s_count.max(0) as usize).min(%d)", expr, f.ArrayBound)
		}
		g.pf("for i in 0..%s {\n", count)
		clone := *f
		clone.Array = ir.ArrayNone
		clone.KeyEnum = ""
		g.emitRefWalk(&clone, expr+"[i]", mutable)
		g.pf("}\n")
	case isStruct(f):
		ctx := "context"
		if mutable {
			ctx = "region_base"
		}
		g.pf("(%s().%s)(%s,&%s%s as *%s %s as *%s u8,visit);\n", fn(f.Type.Name, "table_type"), verb, ctx, mut, expr, cast, f.Type.Name, cast)
	case isUnion(f):
		g.pf("match &%s%s {\n", mut, expr)
		for _, v := range unionOf(f).Variants {
			if v.Void() || (v.F != nil && !v.F.Type.Pointer && !g.variable[v.F.Type.Name] && !(isUnion(v.F) && unionHasRefs(unionOf(v.F), g.variable))) {
				continue
			}
			g.pf("%s::%s(arm) => {\n", f.Type.Name, ir.GoExportName(v.Name))
			arm := v.F
			if armCounted(arm) && arm.Array != ir.ArrayNone {
				g.pf("for i in 0..(arm.length.max(0) as usize).min(%d) {\n", arm.ArrayBound)
				clone := *arm
				clone.Array = ir.ArrayNone
				g.emitRefWalk(&clone, "arm.value[i]", mutable)
				g.pf("}\n")
			} else {
				g.emitRefWalk(arm, "(*arm)", mutable)
			}
			g.pf("}\n")
		}
		g.pf("_=>{},\n}\n")
	}
}
func (g *gen) emitVariableSurface(st *ir.Struct) {
	name := st.Name
	g.pf("pub fn %s(id:u64) -> Option<&'static TableTypeInfo> {match id {\n", fn(name, "node_type"))
	for _, target := range ir.PointerReachable(st) {
		g.pf("0x%016x => Some(%s()),\n", ir.TableWireId(target.WireName()), fn(target.Name, "table_type"))
	}
	bytes, text := ir.PointerReachableBlobs(st)
	if bytes {
		g.pf("0x%016x => Some(table_bytes_table_type()),\n", ir.BytesWireTypeId)
	}
	if text {
		g.pf("0x%016x => Some(table_string_table_type()),\n", ir.StringWireTypeId)
	}
	g.pf("_=>None,\n}}\n")
	g.pf("pub fn %s()->&'static [fn()->&'static TableTypeInfo] { &[ %s,\n", fn(name, "node_layout"), fn(name, "table_type"))
	for _, target := range ir.PointerReachable(st) {
		g.pf("%s,\n", fn(target.Name, "table_type"))
	}
	if bytes {
		g.pf("table_bytes_table_type,\n")
	}
	if text {
		g.pf("table_string_table_type,\n")
	}
	g.pf("] }\n")
	g.pf("pub type %sBuilder = TableBuilder<%s>;\n", name, name)
	g.pf("pub fn %s(value: &%s, context: TableContext<'_>) -> core::result::Result<i64,TableRefuseReason> {\nlet mut ids=[0u64;%d]; table_graph_write(value,context,None,&mut ids)\n}\n", fn(name, "measure"), name, ir.TableWireIdCapacity(g.unit))
	g.pf("pub fn %s(value: &%s, context: TableContext<'_>, buffer: &mut[u8]) -> core::result::Result<i64,TableRefuseReason> {\nlet mut ids=[0u64;%d]; table_graph_write(value,context,Some(buffer),&mut ids)\n}\n", fn(name, "save"), name, ir.TableWireIdCapacity(g.unit))
	g.pf("pub fn %s(builder:&mut TableBuilder<%s>,bytes:&[u8],report:&mut TableReport)->core::result::Result<(),TableLoadError>{table_graph_load_builder(builder,bytes,report,%s)}\n", fn(name, "load_builder"), name, fn(name, "node_type"))
	g.pf("pub fn %s(bytes:&[u8]) -> core::result::Result<TableLoadSize,TableLoadError> { table_graph_load_measure::<%s>(bytes,%s) }\n", fn(name, "load_measure"), name, fn(name, "node_type"))
	g.pf("pub fn %s<'a,'d>(data:&'a mut[TableStorageWord],directory:&'d mut[TableNodeDirEntry],bytes:&[u8],report:&mut TableReport) -> core::result::Result<TableRegion<'a,'d,%s>,TableLoadError> { table_graph_load(data,directory,bytes,report,%s) }\n", fn(name, "load"), name, fn(name, "node_type"))
}
func unionHasRefs(u *ir.Union, variable map[string]bool) bool {
	for _, v := range u.Variants {
		if v.F != nil && (v.F.Type.Pointer || variable[v.F.Type.Name] || (isUnion(v.F) && unionHasRefs(unionOf(v.F), variable))) {
			return true
		}
	}
	return false
}
