package tablenames

// Squirrel is the Squirrel table backend (internal/codegen/squirreltable).
// Its runtimes are separate modules, schema_table_<name>.nut, loaded beside
// the unit's generated files (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT), so
// the names they export live on the runtime's own exports table and no schema
// declaration can collide with them. They are registered anyway, the way a
// scoped runtime name is: the registry answers for every spelling the emitted
// text carries.
const Squirrel Backend = 1 << 11 // 1<<9 is Lua's and 1<<10 is Luau's, on their own streams

func init() {
	define(Squirrel,
		Name{Name: "ReadStrict", What: "the strict read over the tolerant load, naming the refusal and the first field that moved its counter (docs/SPEC-TABLES.md §6.5, §11)", Scoped: true},
		Name{Name: "ReadStrictNames", What: "the strict read's refusal vocabulary, by name (§11, §6.5)", Scoped: true},
		Name{Name: "Cook", What: "the cooked form's Open (docs/SPEC-TABLES.md §7)", Scoped: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it (§7.1)"},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it (§7.1)"},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name (§7)"},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header", RustConst: true},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "LoadRetain", What: "the retain-unknown load (§6.6)", Scoped: true},
		Name{Name: "MeasureRetain", What: "the retain-unknown measure (§6.6)", Scoped: true},
		Name{Name: "SaveRetain", What: "the retain-unknown save (§6.6)", Scoped: true},
		Name{Name: "TableRetain", What: "the caller's retention buffer and retained-id list (§6.6)", Scoped: true},
	)
}
