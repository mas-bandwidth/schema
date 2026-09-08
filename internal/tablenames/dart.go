package tablenames

// Dart is the Dart table backend (internal/codegen/darttable).
//
// The Dart backend emits the BLOCK and COOK read halves only (the table wire
// in its previous form was removed; schema#514 brings the id-table wire), so
// this registry holds the two accelerator runtimes' library-scope names. Where a runtime name is a free
// function Dart spells it lowerCamelCase and the registry holds the
// PascalCase form: the two are a bijection (the packet emitter's
// dartName), so one registration covers both.
const Dart Backend = 1 << 8

func init() {
	define(Dart,
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
