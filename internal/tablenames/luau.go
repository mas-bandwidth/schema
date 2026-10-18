package tablenames

// Luau is the Luau table backend (internal/codegen/luautable). Its runtime is
// a set of required modules emitted beside the generated <Base>Table.luau
// files (docs/PORT-LUAU.md, THE RUNTIME LAYOUT): schema_tables.luau,
// load.luau, refusal.luau and the rest. Every name a Luau runtime carries
// lives INSIDE one of those modules — a field of the table the module
// returns, which a require resolves and never a unit scope — so no spelling
// can collide with a user declaration, and each claim is scoped
// (docs/SPEC-TABLES.md §11). These are the Table* spellings refusal.luau
// exports.
const Luau Backend = 1 << 9

func init() {
	define(Luau,
		Name{Name: "TableReadStrict", What: "§6.5's strict read: the root, or nil and one named reason and the first root field that raised it", Scoped: true},
		Name{Name: "TableFormPrevious", What: "previous_form: a form this read is ahead of — form 3 at a variable root, form 1 at a fixed root (§3)", Scoped: true},
		Name{Name: "TableFormMessageAsFile", What: "message_form_as_file: form 2 handed to a file root (§3, §6.5)", Scoped: true},
		Name{Name: "TableFormNewer", What: "newer_form: a form byte past the registry (§3, §6.5)", Scoped: true},
		Name{Name: "TableFormUnknown", What: "unknown_form: a form byte this call does not carry (§6.5)", Scoped: true},
		Name{Name: "TableIntegrityMismatch", What: "integrity_mismatch: §2.14's CRC-32C trailer, where the root declares integrity", Scoped: true},
	)
}
