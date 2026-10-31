package tablenames

// Luau is the Luau table backend (internal/codegen/luautable). Its runtimes
// are separate modules, <name>.luau, required beside the unit's generated
// <Base>Table.luau modules (docs/PORT-LUAU.md, THE RUNTIME LAYOUT), so the
// names they export live on the runtime module's own returned table and no
// schema declaration can collide with them. They are registered anyway, the
// way a scoped runtime name is: the registry answers for every spelling the
// emitted text carries. The bit is 1 << 10: 1 << 9 is claimed on the Lua and
// Squirrel streams.
const Luau Backend = 1 << 10

func init() {
	define(Luau,
		Name{Name: "ReadStrict", What: "refusal.luau's strict read over the tolerant load, naming the refusal and the first field that moved its counter (docs/SPEC-TABLES.md §6.5, §11)", Scoped: true},
		Name{Name: "ReadStrictNames", What: "refusal.luau's strict-read refusal vocabulary, by name (§11, §6.5)", Scoped: true},
	)
}
