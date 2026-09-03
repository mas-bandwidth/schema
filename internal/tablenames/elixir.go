package tablenames

// Elixir is the Elixir table backend (internal/codegen/elixirtable).
const Elixir Backend = 1 << 6

func init() {
	define(Elixir,
		Name{Name: "TableRuntime", What: "the unit's shared table runtime — a Rust crate module, an Elixir unit-level module"},
		// ---- the ELIXIR backend's own unit-level MODULES
		// (internal/codegen/elixirtable) ----
		//
		// An Elixir declaration lowers to a module under the unit's own namespace,
		// so every module the emitter defines at that level is a name a
		// declaration could collide with — which is why these are claimed and why
		// the Elixir scan looks for MODULE SEGMENTS rather than for a Table*
		// prefix. The two accelerators carry their own runtimes because a VARIABLE
		// unit gets no table runtime at all (§11) and still has both of them.
		Name{Name: "BlockRuntime", What: "the BLOCK form's shared runtime module (docs/SPEC-TABLES.md §19)"},
		Name{Name: "CookRuntime", What: "the COOKED form's shared runtime module (docs/SPEC-TABLES.md §7)"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
