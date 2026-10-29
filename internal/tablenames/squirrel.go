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
		Name{Name: "loadTolerant", What: "the tolerant load, keeping the recovered value and its counters (docs/SPEC-TABLES.md §6.5)", Scoped: true},
		Name{Name: "FromJsonTolerant", What: "the tolerant text read, keeping the recovered value and its counters (docs/SPEC-TABLES.md §6.5)", Scoped: true},
		Name{Name: "FixedLoadTolerant", What: "the tolerant fixed-form read, keeping the recovered records (docs/SPEC-TABLES.md §3.4, §6.5)", Scoped: true},
		// The hand-editable text form (docs/SPEC-TABLES.md §16.9): the runtime
		// schema_table_document's exports, reached as _rt_document.<Name>. The
		// unit module's per-table <Name>LoadTextForm, <Name>SaveTextForm and
		// <Name>EditTextForm are claimed by suffix in internal/check.
		Name{Name: "LoadTextForm", What: "the hand-editable text form's strict read: a document and its typed value (§16.9)", Scoped: true},
		Name{Name: "SaveTextForm", What: "the hand-editable text form's rewrite: the retained bytes (§16.9.3)", Scoped: true},
		Name{Name: "EditTextForm", What: "the hand-editable text form's leaf edits by original span (§16.9.3)", Scoped: true},
		Name{Name: "DefaultLimits", What: "the hand-editable text form's contract limits (§16.9.4)", Scoped: true},
		Name{Name: "Names", What: "the hand-editable text form's stable refusal names (§16.9.4)", Scoped: true},
		Name{Name: "Cook", What: "the cooked form's write entry, bound on the unit module's export table (docs/SPEC-TABLES.md §7.6)"},
		Name{Name: "CookMeasure", What: "the cooked form's write measure, bound on the unit module's export table (docs/SPEC-TABLES.md §7.6)"},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name (§7)"},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header", RustConst: true},
		Name{Name: "Block", What: "the block form's Open (docs/SPEC-TABLES.md §19.2)", Scoped: true},
		Name{Name: "TableDocNone", What: "the one shared empty doc of the reflection view (docs/SPEC-TABLES.md §8.1)"},
		Name{Name: "TableTagsNone", What: "the one shared empty tag list of the reflection view (docs/SPEC-TABLES.md §8.1)"},
		Name{Name: "TableUnitView", What: "the unit registry the reflection view lists (docs/SPEC-TABLES.md §8.3)"},
		Name{Name: "LoadRetain", What: "the retain-unknown load (§6.6)", Scoped: true},
		Name{Name: "MeasureRetain", What: "the retain-unknown measure (§6.6)", Scoped: true},
		Name{Name: "SaveRetain", What: "the retain-unknown save (§6.6)", Scoped: true},
		// The unit module binds every one of these on its export table `_M`,
		// beside the declarations' own classes, so each is claimed: a
		// declaration of the name would replace the binding. Found by
		// compiler's TestSquirrelTableModuleScopeNamesAreClaimed, which scans
		// the emitted module; the other direction (every name here is spelled
		// by the emitter) is TestSquirrelRegistryNamesAreEmitted.
		Name{Name: "Package", What: "the unit's package name, as the reflection view reads it (§8.3)"},
		Name{Name: "Roots", What: "the unit's root list (§8.3)"},
		Name{Name: "Descriptors", What: "the unit's descriptor table (§19.2)"},
		Name{Name: "BuildVersionBytes", What: "the unit's build version, as a cooked header records it (§7.1)"},
		Name{Name: "CookReason", What: "the reason the last cook write refused (§7.6)"},
		Name{Name: "TableBlockAlign", What: "the block prologue's alignment (§19.1)"},
		Name{Name: "TableBlockMagicBytes", What: "the block prologue's magic, as bytes (§19.1)"},
		Name{Name: "TableBlockByteOrderBytes", What: "this build's byte order, as the block prologue carries it (§19.1)"},
		Name{Name: "TableCookMagicBytes", What: "the cooked header's magic, as bytes (§7.1)"},
		Name{Name: "TableCookByteOrderBytes", What: "this build's byte order, as the cooked header records it (§7.1)"},
		Name{Name: "TableMessageIds", What: "the unit's message ids (§3.3)"},
		Name{Name: "TableRetainKnown", What: "the unit's known field ids, the retain-unknown test (§6.6)"},
		Name{Name: "blockBegin", What: "the block builder's prologue and triples (§19.1)"},
		Name{Name: "blockBytes", What: "the block builder's used extent (§19.1)"},
		Name{Name: "blockCreate", What: "the block builder's storage (§19.1)"},
		Name{Name: "blockDestroy", What: "the block builder's storage release (§19.1)"},
		Name{Name: "blockForms", What: "the block forms table (§19.2)"},
		Name{Name: "blockKeyed", What: "the keyed arrays of the block form (§19.2)"},
		Name{Name: "blockOpen", What: "the block form's Open (§19.2)"},
		Name{Name: "blockRecords", What: "the block form's record table (§19.2)"},
		Name{Name: "blockRows", What: "one out-of-line array's row storage (§19.1)"},
		Name{Name: "blockSet", What: "the block builder's field writer (§19.1)"},
		Name{Name: "cookForms", What: "the cook forms table (§7)"},
		Name{Name: "cookOpen", What: "the cooked form's Open (§7)"},
		Name{Name: "cookWriteUnions", What: "the cook writer's union table (§7.6)"},
	)
}
