package tablenames

// Unity is the Unity C# table backend (internal/codegen/unitytable). Its
// runtime types are nested in the unit's `public static partial class Schema`,
// the same class the packet functions live in, so a declaration of the same
// spelling would be shadowed inside every generated method body: each name is
// claimed. Every one of them is a spelling the C# backend already claims, so
// the Unity leg adds no claim of its own.
//
// The allocation takes bit 14, the next one after Python's 13.
const Unity Backend = 1 << 14

func init() {
	define(Unity,
		Name{Name: "TableWire", What: "the form-1 wire walk, nested in Schema"},
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
	)
}
