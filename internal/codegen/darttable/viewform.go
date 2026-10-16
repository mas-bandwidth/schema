// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §8.3): one TableFieldInfo per
// carried field and one TableTypeInfo per carried table, with the name, kind,
// text key, annotation and Dart storage accessors a generic walk needs.
// The optional unit-wide vocabulary lives in unitview.go.
package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardFieldKind is a field's table-wire kind (§3): bool 1, the integer/bits
// kinds 2..9, float 10/11, string(N) 12, bytes(N)/[]T 14, *string/*bytes 17,
// enum 30. It is [ir.TableWireFieldKind] — the FIELD's kind byte, an array's
// array kind included — so the descriptor and the writer agree (§8.1).
func cardFieldKind(f *ir.Field) int {
	return ir.TableWireFieldKind(f)
}

// cardFieldShape is a field's storage shape (§8.1, the card's three): "value"
// for a scalar or a bounded string(N), "list" for an unbounded []T, and "blob"
// for *string — the node-index pointer to a string blob (§2.5), the one shape a
// walker must resolve before it steps.
func cardFieldShape(f *ir.Field) string {
	switch {
	case f.Type.Blob():
		return "blob"
	case f.IsList():
		return "list"
	default:
		return "value"
	}
}

// emitDescriptors emits the descriptor classes and the cardDescriptors()
// registry into the wire library (docs/SPEC-TABLES.md §8.1, §8.3).
func (g *wireGen) emitDescriptors() {
	g.pf("// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §8.3): one TableFieldInfo\n")
	g.pf("// per carried field, with its storage accessors, annotation and wire\n")
	g.pf("// identity; one TableTypeInfo per table. The optional unit registry\n")
	g.pf("// adds declarations outside the table-wire closure.\n")
	g.pf("final class TableFieldInfo {\n")
	g.pf("\tfinal String name;\n")
	g.pf("\tfinal String json, typeName, doc;\n")
	g.pf("\tfinal int id;\n")
	g.pf("\tfinal int kind;\n")
	g.pf("\tfinal String shape;\n")
	g.pf("\tfinal int? bound;\n")
	g.pf("\tfinal int? min;\n")
	g.pf("\tfinal int? max;\n")
	g.pf("\tfinal bool optional;\n")
	g.pf("\tfinal int? elemSize;\n")
	g.pf("\tfinal List<String> tags;\n")
	g.pf("\tfinal dynamic Function(dynamic) read;\n")
	g.pf("\tfinal void Function(dynamic, dynamic) write;\n")
	g.pf("\tTableFieldInfo(this.name, this.kind, this.shape, this.bound, this.min, this.max, this.json, this.typeName, this.id, this.optional, this.elemSize, this.read, this.write, this.doc, this.tags);\n")
	g.pf("}\n\n")
	g.pf("final class TableTypeInfo {\n")
	g.pf("\tfinal String name;\n")
	g.pf("\tfinal String doc;\n")
	g.pf("\tfinal List<String> tags;\n")
	g.pf("\tfinal List<TableFieldInfo> fields;\n")
	g.pf("\tTableTypeInfo(this.name, this.fields, this.doc, this.tags);\n")
	g.pf("}\n\n")
	g.pf("Map<String, TableTypeInfo> cardDescriptors() => <String, TableTypeInfo>{\n")
	for _, st := range g.closure {
		g.pf("\t'%s': TableTypeInfo('%s', <TableFieldInfo>[\n", st.Name, st.Name)
		for _, f := range st.Fields {
			g.emitFieldDescriptor(f)
		}
		g.pf("\t], %s, %s),\n", viewDartString(st.Doc), viewDartTags(st.Tags))
	}
	g.pf("};\n\n")
}

// emitFieldDescriptor emits one field's TableFieldInfo row: its name, its
// kind, its shape, and its declared bound — the string(N) length, or the
// declared min/max of a ranged integer, null where the declaration bounds
// nothing (§8.1).
func (g *wireGen) emitFieldDescriptor(f *ir.Field) {
	bound := "null"
	min := "null"
	max := "null"
	switch {
	case f.Type.Blob():
		// *string is unbounded (§2.5): no declared bound.
	case f.Type.Kind == ir.TString:
		bound = fmt.Sprintf("%d", f.Type.Size)
	case f.HasIntRange && !isWide(f):
		// a wide field's range is a raw integer a Dart int may not hold
		// (§3, §15), so its row names no min/max
		min = dartBoundLiteral(f.IntMin)
		max = dartBoundLiteral(f.IntMax)
	}
	member := fieldMember(f)
	g.pf("\t\tTableFieldInfo(%s, %d, %q, %s, %s, %s, %s, %s, %s, %t, %s, (dynamic v) => v.%s, (dynamic v, dynamic x) { v.%s = x; }, %s, %s),\n",
		viewDartString(f.Name), cardFieldKind(f), cardFieldShape(f), bound, min, max,
		viewDartString(ir.TableFieldJsonKey(f)), viewDartString(ir.TableTypeSpelling(f)),
		dartU64Literal(ir.TableFieldWireId(f)), f.Type.Optional, viewDartElementSize(f),
		member, member, viewDartString(f.Doc), viewDartTags(f.Tags))
}
