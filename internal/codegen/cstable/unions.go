package cstable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A table-only union owns the same fields a record would. The tag selects
// which preallocated member is meaningful; scalar arms never need boxing.
func unionOwner(un *ir.Union) *ir.Struct {
	owner := &ir.Struct{Name: un.Name}
	for _, v := range un.Variants {
		if !v.Void() {
			owner.Fields = append(owner.Fields, v.F)
		}
	}
	return owner
}

func (g *tableGen) emitTableUnion(un *ir.Union) {
	g.owner = unionOwner(un)
	g.tf("// %sType follows the packet tag shape; None selects no arm.\n", un.Name)
	g.tf("public enum %sType : %s\n{\n    None = 0,\n", un.Name, csFieldType(ir.FieldType{Kind: ir.TInt, Width: ir.StorageBitsFor(int64(len(un.Variants)))}))
	for i, v := range un.Variants {
		g.tf("%s    %s = %d,\n", ir.DocComment(v.Doc, "    ", "//"), ir.GoExportName(v.Name), i+1)
	}
	g.tf("    Count = %d,\n    Max = %d,\n}\n\n", len(un.Variants), len(un.Variants))
	g.pf("public static string EnumName%sType(ulong value)\n{\n    switch (value)\n    {\n        case 0: return \"None\";\n", un.Name)
	for i, v := range un.Variants {
		g.pf("        case %d: return %q;\n", i+1, ir.GoExportName(v.Name))
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")
	g.tf("%s", ir.DocComment(un.Doc, "", "//"))
	g.tf("// Selected arms ride even when empty. Every buffer exists at construction.\npublic sealed class %s\n{\n    public %sType Type;\n", un.Name, un.Name)
	for _, f := range g.owner.Fields {
		g.emitTableStorageField(f)
	}
	g.emitElementConstructor(g.owner)
	g.tf("}\n\n")
}

// Arm rows address the union object directly. The legacy table/payload columns
// remain available for body arms; Field also describes scalars and collections.
func (g *tableGen) unionArmsValue(un *ir.Union) string {
	var b strings.Builder
	fmt.Fprintf(&b, "new TableUnionInfo { GetTag = delegate(object o) { return (ulong)((%s)o).Type; }", un.Name)
	fmt.Fprintf(&b, ", SetTag = delegate(object o, ulong t) { ((%s)o).Type = unchecked((%sType)t); }", un.Name, un.Name)
	b.WriteString(", Arms = new TableUnionArmInfo[] { new TableUnionArmInfo()")
	for _, v := range un.Variants {
		b.WriteString(", new TableUnionArmInfo { ")
		if !v.Void() {
			row := &tableGen{unit: g.unit, owner: unionOwner(un), arm: true}
			row.emitTableFieldDescriptor(v.F, "")
			field := strings.TrimSuffix(strings.TrimSpace(row.schema.String()), ",")
			fmt.Fprintf(&b, "Field = %s", field)
			if v.Body() {
				fmt.Fprintf(&b, ", TableRef = delegate { return %sTableType(); }, Payload = delegate(object o) { return ((%s)o).%s; }", v.Type, un.Name, ir.GoExportName(v.Name))
			}
		}
		b.WriteString(" }")
	}
	b.WriteString(" } }")
	return b.String()
}

func armTags(tags []string) string {
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = strconv.Quote(tag)
	}
	return "new string[] { " + strings.Join(quoted, ", ") + " }"
}
