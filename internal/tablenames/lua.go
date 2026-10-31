package tablenames

import "sort"

// Lua is the Lua table backend (internal/codegen/luatable). Its runtime is a
// Lua MODULE per file (runtime/*.lua), and every spelling it defines is a KEY
// OF THE MODULE TABLE its file returns, reached as `require("load").Load` or
// `T.Tables`. A `local` at module scope is private to its file, and a schema
// name reaches a module table only as `_M.New<Declaration>` (no runtime key
// begins with New) or as a string key into `_M.Tables` and `_M.Unions`, so no
// declaration can collide with one: every name here is Scoped and claims
// nothing. They are registered anyway, as JavaScript registers its
// module-scope bindings (internal/tablenames/js.go), so the scan that holds
// this list honest has an answer for every key the emitter spells
// (compiler's TestLuaTableRuntimeNamesAreClaimed reads the emitted text, both
// ways, and TestLuaRuntimeNameScanGoesRed is its control). The one claimable
// Lua spelling is a runtime module's FILE name, because `require` has a single
// flat namespace: the Lua emitter refuses a schema file of that name beside a
// table (docs/SPEC-TABLES.md §11, docs/PORT-LUA.md).
const Lua Backend = 1 << 9

// luaModuleKeys is every key of every module table the Lua table backend emits,
// by the file that returns the table.
var luaModuleKeys = map[string][]string{
	"the unit's table module (<Package>Tables.lua)": {
		"AnnounceRead", "Announcement", "BuildVersion", "BytesBlobTypeId", "DeprecatedVariants", "Fixed", "FixedEnums",
		"FixedFields", "FixedUnions", "Keyed", "LoadMessages", "LoadMessagesTolerant", "MeasureMessages", "MessageBatchMax",
		"MessageEntries", "MessageNodeId", "MessageRefBits", "MessageRoots", "MessageVersionId", "MessageVocabulary",
		"MessageVocabularyId", "Package", "ReadFixed", "ReadFixedTolerant", "RegionAlign", "Registry", "SaveMessages",
		"StringBlobTypeId", "Tables", "TypedBodies", "Unions", "WireIds", "WriteFixed",
	},
	"the unit's type view (<Package>View.lua)": {"Package", "ProtocolId", "Registry", "Tables", "Types"},
	"block.lua":      {"BlockOpen", "ByteOrder", "Dump", "Layout", "Open"},
	"blockbuild.lua": {"Begin", "BlockBytes", "BlockMaxBytes"},
	"cook.lua":       {"Check", "Open"},
	"cookwrite.lua":  {"Cook", "CookMeasure"},
	"descriptor.lua": {
		"F32_EXP", "F64_EXP", "ID_TABLE_BOUND", "by_id", "bytes_less", "check_defaults", "count_of", "deprecated_variant", "field",
		"fingerprint", "finite", "fresh", "guard_holds", "keyed", "list_layout", "map_entries", "number_nodes",
		"pointer_reachable", "prepare", "record_size", "refuse_deprecated", "refuse_non_finite", "region_align",
		"reset", "reset_elem", "seal",
	},
	"fixed.lua":         {"read", "write"},
	"json.lua":          {"FromJson", "FromJsonTolerant", "ToJson"},
	"load.lua":          {"Load", "LoadMeasure", "LoadTolerant", "NewState", "crc32c"},
	"message.lua":       {"announceRead", "loadMessages", "measureMessages", "saveMessages"},
	"refusal.lua":       {"ReadStrict"},
	"retain.lua":        {"LoadRetain", "MeasureRetain", "SaveRetain"},
	"save.lua":          {"Measure", "Save", "SaveTyped", "Typed"},
	"text.lua":          {"LoadText", "SaveText"},
	"textform.lua":      {"Load", "LoadTextForm"},
	"textformwrite.lua": {"Edit", "EditTextForm", "Load", "LoadDocument", "LoadTextForm", "Save", "SaveTextForm", "Span"},
	"uncook.lua":        {"Read"},
}

// LuaModuleKeys is every key of every module table the Lua table backend
// emits, sorted and without repeats (a key two modules both spell is one name).
//
// It is NOT in the shared registry. The registry is global (a name a backend
// registers is looked for in every other backend's view files, and a
// lowercase word like field or read would be found in all of them), and a Lua
// module key claims nothing: this list is the scan's ground truth and nothing
// else. compiler's TestLuaTableRuntimeNamesAreClaimed, holds it to the emitted text both ways.
func LuaModuleKeys() []string {
	seen := map[string]bool{}
	for _, keys := range luaModuleKeys {
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

func init() {
	// ReadStrict predates the list and stays a registered, Scoped name (the
	// refusal runtime's module table, docs/SPEC-TABLES.md §6.5).
	define(Lua, Name{Name: "ReadStrict", What: "the strict read over the tolerant load, on the refusal runtime's module table (docs/SPEC-TABLES.md §6.5)", Scoped: true})
}
