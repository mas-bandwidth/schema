package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

// Table-only unions share the packet emitter's tag-plus-arms storage. Field
// payloads retain the same companion names as ordinary table fields.
func (g *tableGen) emitTableUnion(un *ir.Union) {
	width := 32
	if g.regional {
		width = ir.StorageBitsFor(un.Max)
	}
	g.tf("type %sType uint%d\nconst (\n%sTypeNone %sType = iota\n", un.Name, width, un.Name, un.Name)
	for _, v := range un.Variants {
		g.tf("%sType%s\n", un.Name, ir.GoExportName(v.Name))
	}
	g.tf("%sTypeCount\n%sTypeMax = %sTypeCount - 1\n)\n", un.Name, un.Name, un.Name)
	if g.regional {
		g.tf("type %s = %sRow\n", un.Name, un.Name)
	} else {
		g.tf("type %s struct {\nType %sType\n", un.Name, un.Name)
		for _, v := range un.Variants {
			if !v.Void() {
				f := *v.F
				f.Name = v.Name
				g.emitTableStorageField(&f)
			}
		}
		g.tf("}\n")
	}
	g.pf("func EnumName%sType(value uint64) string { switch value { case 0:return \"None\"\n", un.Name)
	for i, v := range un.Variants {
		g.pf("case %d:return %q\n", i+1, ir.GoExportName(v.Name))
	}
	g.pf("};return \"???\" }\n")
}

// An overlay has one canonical payload extent. Accessors expose typed views
// without storing Go pointers in the record or duplicating inactive arms.
func (g *tableGen) emitUnionRow(un *ir.Union) {
	size, align, tag, offset := ir.UnionLayout(g.unit, un)
	g.tf("type %sRow struct { _ [0]uint%d; Type %sType;", un.Name, min(align, 8)*8, un.Name)
	if offset > tag {
		g.tf("_ [%d]byte;", offset-tag)
	}
	if size > offset {
		g.tf("Payload [%d]byte", size-offset)
	}
	g.tf("}\n")
	g.pf("func init(){ if unsafe.Sizeof(%sRow{})!=%d || unsafe.Offsetof(%sRow{}.Type)!=0 { panic(\"schema union row layout\") } }\n", un.Name, size, un.Name)
	for _, v := range un.Variants {
		if v.Void() {
			continue
		}
		g.pf("func (value *%sRow) %s() *%s { return (*%s)(unsafe.Add(unsafe.Pointer(value),%d)) }\n", un.Name, ir.GoExportName(v.Name), g.unionArmStorage(v), g.unionArmStorage(v), offset)
	}
}
func (g *tableGen) unionArmStorage(v ir.UnionVariant) string {
	cg := &cookGen{unit: g.unit}
	f := *v.F
	f.Name = "value"
	w := &cookWriter{g: cg, pieces: ir.FieldPieces(g.unit, &f, 0)}
	cg.emitBlittableField(&f, w)
	size, _ := ir.ArmLayout(g.unit, v)
	w.pad(size)
	return "struct {" + cg.structs.String() + "}"
}
func (g *tableGen) unionArmExpr(un *ir.Union, v ir.UnionVariant, expr string) string {
	if !g.regional {
		return expr + "." + ir.GoExportName(v.Name)
	}
	return expr + "." + ir.GoExportName(v.Name) + "().Value"
}
func (g *tableGen) emitRegionUnionDescriptor(un *ir.Union) {
	_, _, tag, offset := ir.UnionLayout(g.unit, un)
	g.pf("tableUnionArms[%d]=TableUnionInfo{TagOffset:0,TagSize:%d,Arms:[]TableUnionArmInfo{{Void:true},\n", g.unionArmSlot[un.Name], tag)
	for _, v := range un.Variants {
		if v.Void() {
			g.pf("{Void:true},\n")
			continue
		}
		f := *v.F
		f.Name = "value"
		g.pf("{Offset:%d,Reset:func(storage unsafe.Pointer){value:=(*%s)(unsafe.Add(storage,%d));", offset, g.unionArmStorage(v), offset)
		g.emitTableResetField(&f)
		g.pf("},Field:TableFieldInfo")
		f.Name = v.Name
		g.armOffset = &offset
		g.emitTableFieldDescriptor(&ir.Struct{Name: un.Name}, &f, "")
		g.armOffset = nil
		g.pf("},\n")
	}
	g.pf("}}\n")
}
