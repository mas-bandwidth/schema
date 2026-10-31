package tablenames

import "sort"

// Moo is the moo table backend (internal/codegen/mootable). Its runtimes are
// separate modules required beside the unit's generated <Base>Table.moo
// modules (table_load.moo, table_save.moo, schema_tables.moo, ...), so every
// name they define lives on a runtime module's own returned table, or is a
// type exported from it, and no schema declaration can collide with one. The
// generated table modules hang every declaration's surface from their own
// module table (M.New<Name>, M.<Name>Descriptor), never at unit scope. The
// names are registered anyway, as scoped, so the scan that holds this list
// honest (compiler/tablesmoo_test.go) has an answer for every Table* spelling
// the emitted text carries. The bit is 1 << 11: 1 << 9 and 1 << 10 are the
// Lua and Luau streams' on the dev branch.
const Moo Backend = 1 << 11

func init() {
	define(Moo,
		Name{Name: "TableDescriptor", What: "schema_tables.moo's exported descriptor type of a table (docs/SPEC-TABLES.md §8.1)", Scoped: true},
		Name{Name: "TableMessageAnnouncement", What: "the unit's announcement bytes on each table module, a compile-time constant (§3.3)", Scoped: true},
		Name{Name: "TableMessageAnnouncementLength", What: "the announcement's length on each table module (§3.3)", Scoped: true},
		Name{Name: "TableMessageRefBits", What: "the unit's reference width on each table module (§3.3)", Scoped: true},
		Name{Name: "TableMessageEntriesHere", What: "the unit's vocabulary entry count on each table module (§3.3)", Scoped: true},
		Name{Name: "TableMessageBuildVersion", What: "the unit's build version on each table module (§3.3)", Scoped: true},
	)
}

// mooModuleKeys is every key of every module table the moo table backend
// emits, by the file that returns the table. Every one is a key of a module
// table a file returns, reached as `require("./table_load").loadTolerant`,
// so none claims a name a schema could declare; the list is the ground truth
// of the scan that holds it both ways (compiler's
// TestMooTableModuleKeysAreClaimed, with TestMooModuleKeyScanGoesRed as its
// control).
var mooModuleKeys = map[string][]string{
	"a schema file's table module (<Base>Table.moo)": {
		"TableMessageAnnouncement", "TableMessageAnnouncementLength", "TableMessageBuildVersion",
		"TableMessageEntriesHere", "TableMessageRefBits",
	},
	"json.moo": {"extra", "formatFloat32", "formatFloat64", "fromJson", "jsonLayerAccepts", "parseFloat32",
		"parseFloat64", "toJson", "toJsonInto", "toJsonMeasure"},
	"schema_tables.moo":   {"find", "isKnown", "keyed", "known", "lookup", "table", "union"},
	"table_block.moo":     {"Begin", "Triple", "Write"},
	"table_blockread.moo": {"BlockOpen", "Read", "ReadAt", "Triple", "info"},
	"table_cookread.moo":  {"layout", "open", "read", "record", "union", "unionLayout", "unit", "version"},
	"table_cookwrite.moo": {"cook", "measure"},
	"table_fixed.moo":     {"Names", "check", "fixed", "load", "measure", "save"},
	"table_load.moo": {"ReadStrict", "integrityOpen", "load", "loadTolerant", "newReport", "nonFinite",
		"resetReport", "setRetain"},
	"table_message.moo": {"AnnounceRead", "LoadMessages", "MeasureMessages", "NewVocabulary", "SaveMessages",
		"bitio", "resetReport", "slots", "unit"},
	"table_refusal.moo": {"ReadStrict", "ReadStrictNames"},
	"table_retain.moo": {"FixedClassRefusal", "LoadRetain", "LoadRetainMessages", "MeasureRetain", "SaveRetain",
		"ids", "newStore", "used"},
	"table_save.moo": {"measure", "number", "save", "setRetain"},
	"table_values.moo": {"BuildVersionId", "BytesBlobId", "HUGE", "Int128", "Int64", "MapKeyId",
		"MessageVocabularyId", "NodeTableId", "StringBlobId", "TWO32", "Uint128", "Uint64", "bitsCap", "clampUtf8",
		"clampWide", "cmp64", "crc32c", "f32to64", "finite32", "finite64", "idHex", "kindClass", "leb", "lebLen",
		"lebString", "rawCmp", "readRaw", "sameStorage", "signedKind", "toRaw", "toStorage", "utf8", "utf8String",
		"wide", "widen", "widens", "width", "writeRaw"},
	"textcarrier.moo": {"decode", "encode", "loadText", "saveText"},
}

// MooModuleKeys is every key of every module table the moo table backend
// emits, sorted and without repeats (a key two modules both spell is one
// name). It is not in the shared registry: a module key claims nothing.
func MooModuleKeys() []string {
	seen := map[string]bool{}
	for _, keys := range mooModuleKeys {
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
