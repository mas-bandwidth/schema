package tablenames

// Dart is the Dart table backend (internal/codegen/darttable).
//
// The Dart backend emits the BLOCK and COOK read halves, and, since schema#514,
// the ID-TABLE WIRE's first slice (internal/codegen/darttable/wire.go): the
// form byte, the sixty-four-bit identity, canonical LEB128 and the per-table
// wire descriptors. Where a runtime name is a free function Dart spells it
// lowerCamelCase and the registry holds the PascalCase form: the two are a
// bijection (the packet emitter's dartName), so one registration covers both.
const Dart Backend = 1 << 8

func init() {
	define(Dart,
		Name{Name: "TableWireFieldInfo", What: "a table-wire field's descriptor: its fnv1a64 id and kind"},
		Name{Name: "TableWireInfo", What: "a table's wire descriptor: its own name id and its fields"},
		Name{Name: "TableReport", What: "a wire read's verdict beside its counters"},
		Name{Name: "TableIds", What: "the writer's id table, first-use order and 1-based references"},
		Name{Name: "TableForm", What: "the variable form's form byte, read first", RustConst: true},
		Name{Name: "TableReservedId", What: "the reserved node-table id", RustConst: true},
		Name{Name: "TableFnv1a64", What: "the sixty-four-bit identity every wire vocabulary shares"},
		Name{Name: "TableLebBytes", What: "the byte width of a canonical LEB128 spelling"},
		Name{Name: "TablePutLeb", What: "write a canonical LEB128 spelling"},
		Name{Name: "TableGetLeb", What: "read a canonical LEB128 spelling"},
		Name{Name: "TableEmptyMeasure", What: "the exact size of an empty body on the wire"},
		Name{Name: "TableEmptySave", What: "frame an empty body with the form byte and id table"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockRead64", What: "the prologue read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableCookRead64", What: "the cooked header read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		Name{Name: "TableCookRef", What: "what a Dart deref answers when it is not an offset: a null (a delta of zero) and a delta that leaves the region"},
		Name{Name: "TableBuildVersion", What: "Dart's spelling of the unit's build version, at library scope in the block runtime home (the cook's, in a unit with no block form)"},
	)
}
