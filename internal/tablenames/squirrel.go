package tablenames

// Squirrel is the Squirrel table backend (internal/codegen/squirreltable).
// Its runtimes are separate modules, schema_table_<name>.nut, loaded beside
// the unit's generated files (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT), so
// the names they export live on the runtime's own exports table and no schema
// declaration can collide with them. They are registered anyway, the way a
// scoped runtime name is: the registry answers for every spelling the emitted
// text carries.
const Squirrel Backend = 1 << 9

func init() {
	define(Squirrel,
		Name{Name: "ReadStrict", What: "the strict read over the tolerant load (docs/SPEC-TABLES.md §6.5)", Scoped: true},
		Name{Name: "ReadStrictNames", What: "the strict read's refusal vocabulary, by name (§11, §6.5)", Scoped: true},
	)
}
