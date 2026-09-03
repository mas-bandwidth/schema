package tablenames

// Dart is the Dart table backend (internal/codegen/darttable).
//
// DART PUTS THE TABLE VERBS ON THE VALUE — methods on a generated table
// class, extension methods on a `type`'s packet-emitted class — so the
// per-member surface claims nothing at library scope and this registry
// holds only the RUNTIME's own names. Where a runtime name is a free
// function Dart spells it lowerCamelCase and the registry holds the
// PascalCase form: the two are a bijection (the packet emitter's
// dartName), so one registration covers both.
const Dart Backend = 1 << 8

func init() {
	define(Dart,
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		Name{Name: "TableJson", What: "the text form's generic walk. C# spells it a nested class of Schema, which claims nothing; Java puts it at package scope, Dart at library scope and JavaScript at module scope — an ES module is one scope, with no nested class to hide a walk in — so the claim is the UNION and the name is claimed"},
		Name{Name: "TableBitsToFloat", What: "u32 bits -> float"},
		Name{Name: "TableFloatToBits", What: "float -> u32 bits"},
		Name{Name: "TableBitsToDouble", What: "u64 bits -> double"},
		Name{Name: "TableDoubleToBits", What: "double -> u64 bits"},
		// THE DART READING TIER's own runtime (internal/codegen/darttable). Dart's
		// privacy is per LIBRARY and a generated file is one, so a runtime shared
		// across a unit's files is PUBLIC and every spelling here is claimed. The
		// backend spells no private library-scope name at all — a schema may
		// declare an identifier beginning with an underscore, so a private
		// top-level name would be an unclaimed collision — which is why the
		// conversion scratch is a class of members rather than four constants.
		//
		// What is NOT here is the per-member surface: Dart puts the table verbs on
		// the VALUE (methods on a generated table class, extension methods on a
		// `type`'s packet-emitted class), so <Name>Measure and its family are
		// claimed by the name-first suffix set like every other backend's and
		// nothing per member reaches library scope.
		Name{Name: "TableEnumVocab", What: "one enum's table-wire vocabulary; the per-enum instances are its static members, so they claim nothing"},
		Name{Name: "TableScratch", What: "the 8-byte float conversion scratch, as overlaid typed-data views"},
		Name{Name: "TableNarrowFloat", What: "narrow a double to its float32 value — what gives a float32 elision C's own float semantics"},
		Name{Name: "TableUnsignedLess", What: "unsigned less-than over the bit-transparent int a u64 rides in"},
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
