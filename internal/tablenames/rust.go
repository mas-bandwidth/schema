package tablenames

// Rust is the Rust table backend (internal/codegen/rusttable).
const Rust Backend = 1 << 2

func init() {
	define(Rust,
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every field row
		// and every declaration row with no `///` block names this ONE
		// definition, so absence costs a unit no string data and a printer
		// concatenates doc columns with no null test. Claimed wherever a unit
		// declares a table.
		Name{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names", RustConst: true},
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		// ---- the RUST backend's own spellings (internal/codegen/rusttable) ----
		//
		// Rust has no overloading, so the enum identity pair C++ and C# spell as an
		// overload set is a TRAIT the generated code implements once per enum: one
		// unit-level name rather than one per declaration.
		Name{Name: "TableEnum", What: "an enum's TABLE-wire identity, as a trait (§5)"},
		// The text form's three bounds, at unit level in Rust because a Rust
		// constant has no class to hide in.
		Name{Name: "TableJsonMaxDepth", What: "the text walk's nesting cap (§16)", RustConst: true},
		Name{Name: "TableJsonMaxKey", What: "the longest key that can name a field", RustConst: true},
		Name{Name: "TableJsonMaxNumber", What: "the longest numeric token the walk converts", RustConst: true},
		// SCOPED, Rust: private to the shared runtime module, which does not
		// import the crate root — so a declaration taking one of these names
		// cannot collide with it, and the name stays the schema author's.
		Name{Name: "TableJsonIn", What: "the text walk's read cursor", Scoped: true},
		Name{Name: "TableJsonOut", What: "the text walk's measure-or-write sink", Scoped: true},
		Name{Name: "TableJsonKey", What: "a scanned object key, kept on the stack", Scoped: true},
		Name{Name: "TableJsonNumber", What: "a scanned numeric token", Scoped: true},
		Name{Name: "TableJsonSink", What: "where a scanned string goes, or nowhere", Scoped: true},
		Name{Name: "TableJsonBase64", What: "the base64 alphabet a bytes(N) rides under", Scoped: true},
		Name{Name: "TableJsonDigits", What: "the float writer's stack sink, so the text form allocates nothing", Scoped: true},
		// SCOPED, Rust: the runtime's snake_case CRATE ITEMS. A schema declaration
		// produces a type (its own spelling) or a SCREAMING_SNAKE constant, and
		// never a bare snake_case crate item — so none of these can be collided
		// with, and claiming forty-odd of them would take forty-odd names from
		// every schema for nothing. They are registered because the scan that
		// holds this list honest now SEES them: its first version was C#'s regex
		// verbatim and blind to lowercase, which is the defect class this block
		// closes. A helper somebody adds is accounted for here rather than
		// slipping in under a regex that could not see it.
		Name{Name: "TableId", What: "the TableEnum trait's value -> wire id half", Scoped: true},
		Name{Name: "TableValue", What: "the TableEnum trait's wire id -> value half", Scoped: true},
		// NOT scoped, because of ELIXIR: a Rust module named table_runtime is
		// private to the crate and no declaration can reach it, but the Elixir
		// emitter's <Package>.TableRuntime is a MODULE at unit level and a
		// declaration named TableRuntime lowers to exactly that spelling. The
		// claim is the UNION over the backends, never the intersection, so the
		// name is claimed for all of them.
		Name{Name: "TableRuntime", What: "the unit's shared table runtime — a Rust crate module, an Elixir unit-level module"},
		Name{Name: "TableRelocatable", What: "the relocatability assert's const fn", Scoped: true},
		Name{Name: "TableBlockByteswap64", What: "the block magic's byte-order swap", Scoped: true},
		Name{Name: "TableJsonRead", What: "the text form's read entry point", Scoped: true},
		Name{Name: "TableJsonWrite", What: "the text form's write entry point", Scoped: true},
		Name{Name: "TableJsonCount", What: "a counted field's companion, read", Scoped: true},
		Name{Name: "TableJsonSetCount", What: "a counted field's companion, written", Scoped: true},
		Name{Name: "TableJsonGetRaw", What: "a slot's raw bits at a width", Scoped: true},
		Name{Name: "TableJsonSetRaw", What: "a slot's raw bits, written", Scoped: true},
		Name{Name: "TableJsonGetSigned", What: "a slot's sign-extended value", Scoped: true},
		Name{Name: "TableJsonFinite", What: "not a NaN, not an infinity", Scoped: true},
		Name{Name: "TableJsonNamed", What: "a vocabulary entry the descriptor could spell", Scoped: true},
		Name{Name: "TableJsonFormatG", What: "C's percent-star-g, digit for digit", Scoped: true},
		Name{Name: "TableJsonShape", What: "what a field's kind expects in the text", Scoped: true},
		Name{Name: "TableJsonElementShape", What: "the same classifier one level down", Scoped: true},
		Name{Name: "TableJsonIsBytes", What: "a bytes(N), which rides as base64", Scoped: true},
		Name{Name: "TableJsonIsEnum", What: "an enum field", Scoped: true},
		Name{Name: "TableJsonIsFlags", What: "a flags field", Scoped: true},
		Name{Name: "TableJsonIsKeyed", What: "an enum-keyed array", Scoped: true},
		Name{Name: "TableJsonKeyedSlotKey", What: "the key a storage slot holds", Scoped: true},
		Name{Name: "TableJsonKeyedSlotValid", What: "a slot whose key names a variant", Scoped: true},
		Name{Name: "TableJsonGuardHolds", What: "a branch guard, evaluated over the descriptors", Scoped: true},
		Name{Name: "TableJsonEncodeUtf8", What: "one code point, encoded", Scoped: true},
		Name{Name: "TableJsonUtf8", What: "one UTF-8 sequence, validated", Scoped: true},
		Name{Name: "TableJsonWalkNumber", What: "JSON's own number production, consumed", Scoped: true},
		Name{Name: "TableJsonScanNumber", What: "the same production, kept", Scoped: true},
		Name{Name: "TableJsonScanString", What: "one JSON string, into a caller slot", Scoped: true},
		Name{Name: "TableJsonScanKey", What: "one object key, onto the stack", Scoped: true},
		Name{Name: "TableJsonSkipValue", What: "one value, consumed and dropped", Scoped: true},
		Name{Name: "TableJsonSkipContainer", What: "one object or array, consumed and dropped", Scoped: true},
		Name{Name: "TableJsonReadTable", What: "one table object", Scoped: true},
		Name{Name: "TableJsonReadField", What: "one field", Scoped: true},
		Name{Name: "TableJsonReadScalar", What: "one scalar", Scoped: true},
		Name{Name: "TableJsonWriteValue", What: "one instance, every field", Scoped: true},
		Name{Name: "TableJsonWriteField", What: "one field", Scoped: true},
		Name{Name: "TableJsonWriteScalar", What: "one scalar", Scoped: true},
		Name{Name: "TableJsonWriteString", What: "one string, escaped", Scoped: true},
		Name{Name: "TableJsonWriteStringBytes", What: "one byte run, escaped", Scoped: true},
		Name{Name: "TableJsonWriteBase64", What: "one bytes(N), base64", Scoped: true},
		Name{Name: "TableJsonWriteFloat", What: "one float, at its shortest round-tripping precision", Scoped: true},
		Name{Name: "TableJsonWriteSigned", What: "one signed integer", Scoped: true},
		Name{Name: "TableJsonWriteUnsigned", What: "one unsigned integer", Scoped: true},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
