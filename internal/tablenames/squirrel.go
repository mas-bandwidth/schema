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
		// The hand-editable text form (docs/SPEC-TABLES.md §16.9): the runtime
		// schema_table_document's exports, reached as _rt_document.<Name>. The
		// unit module's per-table <Name>LoadTextForm, <Name>SaveTextForm and
		// <Name>EditTextForm are claimed by suffix in internal/check.
		Name{Name: "LoadTextForm", What: "the hand-editable text form's strict read: a document and its typed value (§16.9)", Scoped: true},
		Name{Name: "SaveTextForm", What: "the hand-editable text form's rewrite: the retained bytes (§16.9.3)", Scoped: true},
		Name{Name: "EditTextForm", What: "the hand-editable text form's leaf edits by original span (§16.9.3)", Scoped: true},
		Name{Name: "DefaultLimits", What: "the hand-editable text form's contract limits (§16.9.4)", Scoped: true},
		Name{Name: "Names", What: "the hand-editable text form's stable refusal names (§16.9.4)", Scoped: true},
	)
}
