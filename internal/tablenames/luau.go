package tablenames

import "sort"

// Luau is the Luau table backend (internal/codegen/luautable). Its runtimes
// are separate modules, schema_<name>.luau, required beside the unit's
// generated <Base>Table.luau modules (docs/PORT-LUAU.md, THE RUNTIME LAYOUT),
// so every name they define is a KEY OF THE MODULE TABLE its file returns, and
// no schema declaration can collide with one: a schema name reaches a module
// table only as the spellings the emitter writes per declaration (the name,
// New<Name>, <Name>Descriptor, ...), and a table module's unit-level keys
// (TableMessage*) begin with a prefix no declaration-derived key carries.
// Every name here is Scoped and claims nothing.
// They are registered anyway, as Lua registers its module keys
// (internal/tablenames/lua.go), so the scan that holds this list honest has an
// answer for every key the emitter spells (compiler's
// TestLuauTableRuntimeNamesAreClaimed reads the emitted text, both ways, and
// TestLuauRuntimeNameScanGoesRed is its control). The bit is 1 << 10: 1 << 9
// is claimed on the Lua and Squirrel streams.
const Luau Backend = 1 << 10

// luauModuleKeys is every key of every module table the Luau table backend
// emits, by the file that returns the table.
var luauModuleKeys = map[string][]string{
	"a schema file's table module (<Base>Table.luau)": {
		"TableMessageAnnouncement", "TableMessageAnnouncementLength", "TableMessageBuildVersion",
		"TableMessageEntriesHere", "TableMessageRefBits",
	},
	"schema_block.luau":     {"BLOCK_ALIGN", "Begin", "alignUp", "elemAlign"},
	"schema_blockread.luau": {"BlockOpen", "NonFiniteFloat", "Read"},
	"schema_cook.luau":      {"BuildVersion", "Cook", "CookMeasure"},
	"schema_cookread.luau":  {"Check", "layout", "open"},
	"schema_fixed.luau":     {"check", "fixed_layout", "load", "loadTolerant", "read", "save"},
	"schema_json.luau":      {"FromJson", "FromJsonTolerant", "SetFlags", "ToJson"},
	"schema_load.luau":      {"ReadStrict", "load", "loadTolerant", "resetReport"},
	"schema_message.luau": {
		"AnnounceRead", "LoadMessages", "LoadMessagesTolerant", "MeasureMessages", "NewVocabulary", "SaveMessages",
		"announceRead",
	},
	"schema_refusal.luau":  {"ReadStrict", "ReadStrictNames"},
	"schema_retain.luau":   {"LoadRetain", "LoadRetainTolerant", "SaveRetain"},
	"schema_save.luau":     {"measure", "save"},
	"schema_tables.luau":   {"keyed", "knownId", "lookup", "table", "union", "wireIds"},
	"schema_text.luau":     {"LoadText", "SaveText"},
	"schema_textform.luau": {"EditTextForm", "LoadTextForm", "SaveTextForm", "SetFlags", "TimestampOK"},
	"schema_view.luau":     {"UnitView"},
}

// LuauModuleKeys is every key of every module table the Luau table backend
// emits, sorted and without repeats (a key two modules both spell is one name).
//
// It is NOT in the shared registry, for the reason LuaModuleKeys gives: the
// registry is global, and a lowercase module key claims nothing. This list is
// the scan's ground truth and nothing else.
func LuauModuleKeys() []string {
	seen := map[string]bool{}
	for _, keys := range luauModuleKeys {
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
	define(Luau,
		Name{Name: "ReadStrict", What: "refusal.luau's strict read over the tolerant load, naming the refusal and the first field that moved its counter (docs/SPEC-TABLES.md §6.5, §11)", Scoped: true},
		Name{Name: "ReadStrictNames", What: "refusal.luau's strict-read refusal vocabulary, by name (§11, §6.5)", Scoped: true},
	)
}
