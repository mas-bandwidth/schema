package tablenames

// Lua is the Lua table backend (internal/codegen/luatable). Its runtime is a
// Lua MODULE per file (runtime/*.lua), so every spelling it defines is reached
// through its module table and a schema declaration cannot collide with one.
// The names are registered anyway, as JavaScript registers its module-scope
// bindings (internal/tablenames/js.go), so the registry has an answer for
// every runtime spelling the leg emits (docs/SPEC-TABLES.md §11, §6.5).
const Lua Backend = 1 << 9

func init() {
	define(Lua,
		Name{Name: "ReadStrict", What: "the strict read over the tolerant load, on the refusal runtime's module table (docs/SPEC-TABLES.md §6.5)", Scoped: true},
	)
}
