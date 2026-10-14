// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §8.3), the card's slice of the
// reflection surface: one TableFieldInfo per field — name, wire kind, shape
// (value / list / blob) and declared bound — and one TableTypeInfo per table,
// so a walker reads a record by name and kind without a per-version struct.
// The registry cardDescriptors() lists the tables of the wire closure in the
// order they are emitted (members before the table that holds them), which is
// the declaration order the card's tables appear in. Nothing else of §8 is
// claimed: no type view, no enum/flags/union/constant registry rows, no doc or
// tag columns.
package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardFieldKind is a field's table-wire kind (§3): ir.TableWireFieldKind covers
// the whole variable class (scalars, string(N), bytes(N), []T, [..N]T, *T,
// *string, *bytes, enum).
func cardFieldKind(f *ir.Field) int {
	return ir.TableWireFieldKind(f)
}

// cardFieldShape is a field's storage shape (§8.1): "value" for a scalar, a
// bounded string(N)/bytes(N), an enum or a table nested by value; "list" for
// an array ([]T and [..N]T, §2.9); "blob" for *string and *bytes (§2.5); and
// "pointer" for *T (§2.1).
func cardFieldShape(f *ir.Field) string {
	switch {
	case f.Array != ir.ArrayNone:
		return "list"
	case f.Type.Blob():
		return "blob"
	case f.Type.Pointer:
		return "pointer"
	default:
		return "value"
	}
}

// emitDescriptors emits the descriptor classes and the cardDescriptors()
// registry into the wire library (docs/SPEC-TABLES.md §8.1, §8.3).
func (g *wireGen) emitDescriptors() {
	g.pf("// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §8.3): one TableFieldInfo\n")
	g.pf("// per field — name, wire kind, shape (value/list/blob/pointer) and\n")
	g.pf("// declared bound — and one TableTypeInfo per table, so a walker reads a\n")
	g.pf("// record by name and kind with no per-version struct. The registry lists\n")
	g.pf("// the tables in declaration order and allocates nothing (§8.1).\n")
	g.pf("final class TableFieldInfo {\n")
	g.pf("\tfinal String name;\n")
	g.pf("\tfinal int kind;\n")
	g.pf("\tfinal String shape;\n")
	g.pf("\tfinal int? bound;\n")
	g.pf("\tfinal int? min;\n")
	g.pf("\tfinal int? max;\n")
	g.pf("\tTableFieldInfo(this.name, this.kind, this.shape, this.bound, this.min, this.max);\n")
	g.pf("}\n\n")
	g.pf("final class TableTypeInfo {\n")
	g.pf("\tfinal String name;\n")
	g.pf("\tfinal List<TableFieldInfo> fields;\n")
	g.pf("\tTableTypeInfo(this.name, this.fields);\n")
	g.pf("}\n\n")
	g.pf("Map<String, TableTypeInfo> cardDescriptors() => <String, TableTypeInfo>{\n")
	for _, st := range g.closure {
		g.pf("\t'%s': TableTypeInfo('%s', <TableFieldInfo>[\n", st.Name, st.Name)
		for _, f := range st.Fields {
			g.emitFieldDescriptor(f)
		}
		g.pf("\t]),\n")
	}
	g.pf("};\n\n")
}

// emitFieldDescriptor emits one field's TableFieldInfo row: its name, its
// kind, its shape, and its declared bound — the string(N)/bytes(N)/[..N]T
// bound, or the declared min/max of a ranged integer, null where the
// declaration bounds nothing (§8.1).
func (g *wireGen) emitFieldDescriptor(f *ir.Field) {
	bound := "null"
	min := "null"
	max := "null"
	if f.Array == ir.ArrayCounted {
		bound = fmt.Sprintf("%d", f.ArrayBound)
	} else if f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Blob() {
		bound = fmt.Sprintf("%d", f.Type.Size)
	}
	if f.HasIntRange {
		min = f.IntMin.String()
		max = f.IntMax.String()
	}
	g.pf("\t\tTableFieldInfo(%q, %d, %q, %s, %s, %s),\n",
		f.Name, cardFieldKind(f), cardFieldShape(f), bound, min, max)
}
