package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE REFLECTION DESCRIPTORS (docs/SPEC-TABLES.md §8), the card's slice only:
// one table type descriptor per table in the card closure, carrying each
// field's name, wire kind, shape (value/list/blob) and declared bound, plus a
// unit registry that lists every descriptor. Enough to walk a record by name
// and kind without a per-version struct. Nothing else of §8 is claimed: no
// doc/tags columns, no type view for a `type` no table reaches, no union or
// flags rows.
//
// A field's SHAPE (docs/SPEC-TABLES.md §8.1) is the three-way the card's
// constructs split into: `value` for a scalar (bool, int32/int64, enum) and an
// inline string(N); `blob` for the *string byte buffer (§2.5); `list` for an
// unbounded []T (§2.9). A field's DECLARED BOUND is the string's declared
// length, and 0 for a blob and an unbounded list, which have none.
func generateViewFiles(u *ir.Unit) (map[string][]byte, error) {
	tables := cardDescriptorTables(u)
	if len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	out := map[string][]byte{
		"TableFieldInfo.java": javaFile(u, "a table field's descriptor (docs/SPEC-TABLES.md §8)", tableFieldInfoRuntime),
		"TableTypeInfo.java":  javaFile(u, "a table type's descriptor (docs/SPEC-TABLES.md §8)", tableTypeInfoRuntime),
	}
	for _, st := range tables {
		out[st.Name+"Descriptor.java"] = javaFile(u,
			fmt.Sprintf("the descriptor for table %s (docs/SPEC-TABLES.md §8)", st.Name),
			descriptorFor(u, st))
	}
	out["TableRegistry.java"] = javaFile(u, "the unit's registry of table descriptors (docs/SPEC-TABLES.md §8.3)", registryFor(tables))
	return out, nil
}

// cardDescriptorTables is the set of tables the descriptors name: every card
// variable root plus the table elements its unbounded lists reach (§2.9), in
// declaration order, deduplicated.
func cardDescriptorTables(u *ir.Unit) []*ir.Struct {
	seen := map[string]bool{}
	var tables []*ir.Struct
	var add func(*ir.Struct)
	add = func(st *ir.Struct) {
		if st == nil || st.IsMapEntry() || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed {
				if elem, ok := f.Type.Ref.(*ir.Struct); ok {
					add(elem)
				}
			}
		}
		tables = append(tables, st)
	}
	for _, st := range cardVariableTables(u) {
		add(st)
	}
	return tables
}

// fieldShape is a field's descriptor shape (docs/SPEC-TABLES.md §8.1).
func fieldShape(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		return "blob"
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		return "blob"
	case f.Array != ir.ArrayNone:
		return "list"
	}
	return "value"
}

// fieldBound is a field's declared bound (docs/SPEC-TABLES.md §8.1): the
// string's declared length; 0 for a blob and an unbounded list, which have no
// bound.
func fieldBound(f *ir.Field) int64 {
	if f.Type.Kind == ir.TString && !f.Type.Blob() {
		return f.Type.Size
	}
	return 0
}

func descriptorFor(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	fmt.Fprintf(&b, "public final class %sDescriptor {\n", st.Name)
	fmt.Fprintf(&b, "    private %sDescriptor() {}\n\n", st.Name)
	b.WriteString("    // Published by CLASS INITIALIZATION, not a plain cached write, because\n")
	b.WriteString("    // the field array is not final and a racing reader could otherwise see a\n")
	b.WriteString("    // non-null descriptor with a null field array (JLS §17.4).\n")
	b.WriteString("    private static final class Holder {\n")
	b.WriteString("        static final TableTypeInfo TABLE = build();\n\n")
	b.WriteString("        private static TableTypeInfo build() {\n")
	fmt.Fprintf(&b, "            TableTypeInfo t = new TableTypeInfo();\n")
	fmt.Fprintf(&b, "            t.name = %q;\n", st.Name)
	fmt.Fprintf(&b, "            t.fields =\n                    new TableFieldInfo[] {\n")
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "                        TableFieldInfo.of(%q, %d, %q, %dL),\n", f.Name, variableWireKind(f), fieldShape(f), fieldBound(f))
	}
	b.WriteString("                    };\n")
	b.WriteString("            return t;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
	fmt.Fprintf(&b, "    public static TableTypeInfo tableType() { return Holder.TABLE; }\n")
	b.WriteString("}\n")
	return b.String()
}

func registryFor(tables []*ir.Struct) string {
	var b strings.Builder
	b.WriteString("public final class TableRegistry {\n")
	b.WriteString("    private TableRegistry() {}\n\n")
	b.WriteString("    // The unit's table descriptors (docs/SPEC-TABLES.md §8.3), in\n")
	b.WriteString("    // declaration order. Immutable after class initialization.\n")
	b.WriteString("    public static TableTypeInfo[] tables() {\n")
	b.WriteString("        return new TableTypeInfo[] {\n")
	for _, st := range tables {
		fmt.Fprintf(&b, "            %sDescriptor.tableType(),\n", st.Name)
	}
	b.WriteString("        };\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

const tableFieldInfoRuntime = `public final class TableFieldInfo {
    public String name;
    public int kind;
    public String shape;
    public long bound;

    public static TableFieldInfo of(String name, int kind, String shape, long bound) {
        TableFieldInfo f = new TableFieldInfo();
        f.name = name;
        f.kind = kind;
        f.shape = shape;
        f.bound = bound;
        return f;
    }
}
`

const tableTypeInfoRuntime = `public final class TableTypeInfo {
    public String name;
    public TableFieldInfo[] fields;
}
`
