package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

// Table-only unions share the packet emitter's tag-plus-arms storage. Field
// payloads retain the same companion names as ordinary table fields.
func (g *tableGen) emitTableUnion(un *ir.Union) {
	g.tf("type %sType uint32\nconst (\n%sTypeNone %sType = iota\n", un.Name, un.Name, un.Name)
	for _, v := range un.Variants {
		g.tf("%sType%s\n", un.Name, ir.GoExportName(v.Name))
	}
	g.tf("%sTypeCount\n%sTypeMax = %sTypeCount - 1\n)\n", un.Name, un.Name, un.Name)
	g.tf("type %s struct {\nType %sType\n", un.Name, un.Name)
	for _, v := range un.Variants {
		if !v.Void() {
			f := *v.F
			f.Name = v.Name
			g.emitTableStorageField(&f)
		}
	}
	g.tf("}\n")
	g.pf("func EnumName%sType(value uint64) string { switch value { case 0:return \"None\"\n", un.Name)
	for i, v := range un.Variants {
		g.pf("case %d:return %q\n", i+1, ir.GoExportName(v.Name))
	}
	g.pf("};return \"???\" }\n")
}
