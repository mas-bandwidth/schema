package tablenames

// Luau is the Luau table backend (internal/codegen/luautable). Its runtime is
// a set of required modules emitted beside the generated <Base>Table.luau
// files (docs/PORT-LUAU.md, THE RUNTIME LAYOUT): schema_tables.luau,
// load.luau, refusal.luau and the rest. Every name a Luau runtime carries
// lives INSIDE one of those modules — a require resolves it, never a unit
// scope — so no spelling can collide with a user declaration, and each claim
// is scoped (docs/SPEC-TABLES.md §11).
const Luau Backend = 1 << 9

func init() {
	define(Luau,
		Name{Name: "TableReadStrict", What: "§6.5's strict read: one call, one typed refusal name", Scoped: true},
		Name{Name: "TableFormPrevious", What: "a form-3 file at a variable root, or a form-1 file at a fixed root: previous_form", Scoped: true},
		Name{Name: "TableFormUnknown", What: "an unknown form byte, refused by name", Scoped: true},
		Name{Name: "TableIntegrityMismatch", What: "§2.14's integrity check, where the unit declares integrity", Scoped: true},
	)
}
