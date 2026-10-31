package tablenames

import "sort"

// Squirrel is the Squirrel table backend (internal/codegen/squirreltable).
// Its runtimes are separate modules, schema_table_<name>.nut, loaded beside
// the unit's generated files (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT), so
// the names they export live on the runtime's own exports table and no schema
// declaration can collide with them. They are registered anyway, the way a
// scoped runtime name is: the registry answers for every spelling the emitted
// text carries.
const Squirrel Backend = 1 << 12 // 1<<9 is Lua's and 1<<10 is Luau's, on their own streams; 1<<11 is moo's

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
		Name{Name: "Cook", What: "the cooked form's Open (docs/SPEC-TABLES.md §7)", Scoped: true},
		Name{Name: "CookMeasure", What: "the cooked form's write measure (docs/SPEC-TABLES.md §7.6)", Scoped: true},
		Name{Name: "CookWrite", What: "the cooked form's write (docs/SPEC-TABLES.md §7.6)", Scoped: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it (§7.1)"},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it (§7.1)"},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name (§7)"},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header", RustConst: true},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "Block", What: "the block form's Open (docs/SPEC-TABLES.md §19.2)", Scoped: true},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it (§19.1)", RustConst: true},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it (§19.1)", RustConst: true},
		Name{Name: "TableBlockLayout", What: "the layout contract's check, run once (§19.3)"},
		Name{Name: "BlockCreate", What: "the block builder's storage, asking the caller's allocator exactly once (§19.1)", Scoped: true},
		Name{Name: "BlockDestroy", What: "the block builder's storage release, through the same pair (§19.1)", Scoped: true},
		Name{Name: "BlockBegin", What: "the block builder's prologue and triples, refusing a bad count by name (§19.1)", Scoped: true},
		Name{Name: "BlockBytes", What: "the block builder's used extent (§19.1)", Scoped: true},
		Name{Name: "BlockRows", What: "one out-of-line array's row storage (§19.1)", Scoped: true},
		Name{Name: "BlockSet", What: "the block builder's field writer, driven by the block descriptors (§19.1, §19.2)", Scoped: true},
		Name{Name: "TableDocNone", What: "the one shared empty doc of the reflection view (docs/SPEC-TABLES.md §8.1)"},
		Name{Name: "TableTagsNone", What: "the one shared empty tag list of the reflection view (docs/SPEC-TABLES.md §8.1)"},
		Name{Name: "TableUnitView", What: "the unit registry the reflection view lists (docs/SPEC-TABLES.md §8.3)"},
		Name{Name: "LoadRetain", What: "the retain-unknown load (§6.6)", Scoped: true},
		Name{Name: "MeasureRetain", What: "the retain-unknown measure (§6.6)", Scoped: true},
		Name{Name: "SaveRetain", What: "the retain-unknown save (§6.6)", Scoped: true},
		Name{Name: "TableRetain", What: "the caller's retention buffer and retained-id list (§6.6)", Scoped: true},
	)
}

// squirrelDeclarationSuffixes are the names the Squirrel table emitter writes
// per declaration on the unit's table module: the declaration's own name
// followed by one of these (the `Fixed...` facts of the fixed form are matched
// by their prefix). A declaration named for another's spelling is refused by
// the emitter, which refuses any key its module would assign twice.
var squirrelDeclarationSuffixes = []string{
	"Descriptor", "EditTextForm", "FromJson", "LoadMessages", "LoadRetain", "LoadTextForm", "Load",
	"MeasureMessages", "MeasureRetain", "Measure", "SaveMessages", "SaveRetain", "SaveTextForm", "Save",
	"TableFields", "TableInfo", "ToJsonMeasure", "ToJson",
}

// SquirrelDeclarationSuffixes is squirrelDeclarationSuffixes, a copy.
func SquirrelDeclarationSuffixes() []string { return append([]string(nil), squirrelDeclarationSuffixes...) }

// squirrelModuleKeys is every name the Squirrel table backend defines that a
// declaration does not spell: the unit's own keys on its table module
// (<Package>Tables.nut), and every runtime file's exports. It is NOT the
// shared registry: a runtime export lives on its own module table and claims
// nothing, and a unit key is held by the emitter's refusal of a key assigned
// twice. compiler's TestSquirrelTableRuntimeNamesAreClaimed holds this list
// to the emitted text both ways, and TestSquirrelRuntimeNameScanGoesRed is its
// control.
var squirrelModuleKeys = map[string][]string{
	"the unit's table module (<Package>Tables.nut)": {},
}

// SquirrelModuleKeys is every name squirrelModuleKeys lists, sorted, without
// repeats.
func SquirrelModuleKeys() []string {
	seen := map[string]bool{}
	for _, keys := range squirrelModuleKeys {
		for _, k := range keys {
			seen[k] = true
		}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
