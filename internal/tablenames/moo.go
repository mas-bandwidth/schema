package tablenames

// Moo is the moo table backend (internal/codegen/mootable). Its runtimes are
// separate modules required beside the unit's generated <Base>Table.moo
// modules (table_load.moo, table_save.moo, schema_tables.moo, ...), so every
// name they define lives on a runtime module's own returned table, or is a
// type exported from it, and no schema declaration can collide with one. The
// generated table modules hang every declaration's surface from their own
// module table (M.New<Name>, M.<Name>Descriptor), never at unit scope. The
// names are registered anyway, as scoped, so the scan that holds this list
// honest (compiler/tablesmoo_test.go) has an answer for every Table* spelling
// the emitted text carries. The bit is 1 << 11: 1 << 9 and 1 << 10 are the
// Lua and Luau streams' on the dev branch.
const Moo Backend = 1 << 11

func init() {
	define(Moo,
		Name{Name: "TableDescriptor", What: "schema_tables.moo's exported descriptor type of a table (docs/SPEC-TABLES.md §8.1)", Scoped: true},
	)
}
