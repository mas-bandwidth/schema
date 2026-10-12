package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE CARD DESCRIPTORS (docs/SPEC-TABLES.md §8, §8.1, §8.3): one table type
// descriptor per card table — the field's name, its table-wire kind, its shape
// (value, list or blob) and its declared bound — and the unit registry that
// lists every table in declaration order. The claim is scoped to the card
// constructs and nothing else of section 8 is claimed: there is no doc/tags
// column, no offset/element-size column and no enum/union vocabulary here,
// because the card wire does not carry those constructs (§2.5, §2.9).
//
// The descriptors are CONSTANT DATA, emitted beside the variable wire's
// runtime, so a tool walks a record by name and kind with no per-version
// struct and no schema files on hand (§8.1).

// VariableDescriptorModule is the module the card descriptors ride in.
const VariableDescriptorModule = "card_descriptors"

// cardDescriptorTables returns the card-boundary variable tables in sorted
// declaration order — the same set the codec emits (cardVariableTableNames),
// across every source file of the unit.
func cardDescriptorTables(u *ir.Unit) []*ir.Struct {
	variable := ir.VariableTables(u)
	seen := map[string]bool{}
	names := make([]string, 0)
	for _, st := range u.Tables {
		if !variable[st.Name] || seen[st.Name] {
			continue
		}
		if refuseCardUnsupportedFields(st) != nil {
			continue
		}
		seen[st.Name] = true
		names = append(names, st.Name)
	}
	sort.Strings(names)
	out := make([]*ir.Struct, 0, len(names))
	for _, n := range names {
		out = append(out, u.Tables[n])
	}
	return out
}

// cardFieldShape is a field's SHAPE for the descriptor (docs/SPEC-TABLES.md
// §8.1): value (a scalar, enum, bool or bounded string), list (an unbounded
// []T), or blob (a *string byte buffer). The declared BOUND is a bounded
// string's N, -1 for the two unbounded shapes, and 0 for a value with no
// declared length.
func cardFieldShape(f *ir.Field) (shape string, bound int64) {
	switch {
	case f.IsList():
		return "SHAPE_LIST", -1
	case f.Type.Blob():
		return "SHAPE_BLOB", -1
	case f.Type.Kind == ir.TString:
		return "SHAPE_VALUE", f.Type.Size
	default:
		return "SHAPE_VALUE", 0
	}
}

// variableDescriptorModule emits the card descriptor module: the three shape
// constants, the field and table descriptor structs, one static field array per
// table, and the unit registry (TABLES) listing them in declaration order.
func variableDescriptorModule(u *ir.Unit) []byte {
	tables := cardDescriptorTables(u)
	if len(tables) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package, "the card descriptors (docs/SPEC-TABLES.md §8, §8.1, §8.3)"))
	b.WriteString(`
/// The three SHAPES a card field takes (docs/SPEC-TABLES.md §8.1, §2.5, §2.9):
/// a value, an unbounded list of table elements, or a *string byte buffer.
pub const SHAPE_VALUE: u8 = 0;
pub const SHAPE_LIST: u8 = 1;
pub const SHAPE_BLOB: u8 = 2;

/// One field's descriptor: its name, its table-wire kind (§3), its shape and
/// its declared bound. Constant data, never allocated (§8.1).
#[derive(Clone, Copy)]
pub struct TableFieldDesc {
    pub name: &'static str,
    pub kind: u8,
    pub shape: u8,
    pub bound: i64,
}

/// One table's descriptor: its name and its fields in declaration order.
#[derive(Clone, Copy)]
pub struct TableDesc {
    pub name: &'static str,
    pub num_fields: i32,
    pub fields: &'static [TableFieldDesc],
}
`)
	for _, st := range tables {
		constName := strings.ToUpper(ir.RustSnake(st.Name)) + "_FIELDS"
		fmt.Fprintf(&b, "\nstatic %s: [TableFieldDesc; %d] = [\n", constName, len(st.Fields))
		for _, f := range st.Fields {
			shape, bound := cardFieldShape(f)
			fmt.Fprintf(&b, "    TableFieldDesc { name: %q, kind: %d, shape: %s, bound: %d },\n",
				f.Name, ir.TableWireFieldKind(f), shape, bound)
		}
		b.WriteString("];\n")
	}
	b.WriteString("\n/// The unit registry (docs/SPEC-TABLES.md §8.3): every card table in\n")
	b.WriteString("/// declaration order, so a tool that knows the unit and nothing else can\n")
	b.WriteString("/// enumerate the tables and walk each one's fields by name and kind.\n")
	fmt.Fprintf(&b, "pub static TABLES: [TableDesc; %d] = [\n", len(tables))
	for _, st := range tables {
		constName := strings.ToUpper(ir.RustSnake(st.Name)) + "_FIELDS"
		fmt.Fprintf(&b, "    TableDesc { name: %q, num_fields: %d, fields: &%s },\n",
			st.Name, len(st.Fields), constName)
	}
	b.WriteString("];\n\n")
	b.WriteString("/// Look a table up by name in the registry (docs/SPEC-TABLES.md §8.3).\n")
	b.WriteString("pub fn table_desc(name: &str) -> Option<&'static TableDesc> {\n")
	b.WriteString("    TABLES.iter().find(|t| t.name == name)\n")
	b.WriteString("}\n")
	return []byte(b.String())
}
