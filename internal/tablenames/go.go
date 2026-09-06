package tablenames

// Go is the Go table backend (internal/codegen/gotable).
const Go Backend = 1 << 3

func init() {
	define(Go,
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every descriptor row
		// and registry record with no `///` block names this ONE definition, so
		// absence costs a unit no string data and a printer concatenates doc
		// columns with no null test. Claimed wherever a unit declares a table.
		Name{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names"},
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor"},
		Name{Name: "TableEnumId", What: "an enum value -> its table-wire variant id"},
		Name{Name: "TableEnumValue", What: "a table-wire variant id -> its enum value"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		Name{Name: "Table", What: "TableFieldInfo's nested-table column", Scoped: true},
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		// the eight MEMBERS of that vocabulary, which C# scopes inside its enum and
		// Rust inside its own type, and which GO flattens into the package
		// namespace — exactly as the Go packet emitter flattens a declared enum's
		// variants, which the checker already claims one by one. A port's spelling
		// decides the claim, and this is Go's alone.
		Name{Name: "TableCookStorageRecord", What: "a nested record, or an array of them"},
		Name{Name: "TableCookStorageReference", What: "an eight-byte signed self-relative delta slot (§6.3)"},
		Name{Name: "TableCookStorageBool", What: "a bool slot"},
		Name{Name: "TableCookStorageSigned", What: "a signed integer slot"},
		Name{Name: "TableCookStorageUnsigned", What: "an unsigned integer, an enum ordinal, a bits(N), a flags mask"},
		Name{Name: "TableCookStorageFloat", What: "a float slot"},
		Name{Name: "TableCookStorageString", What: "char[N + 1] with an int32 used length beside it"},
		Name{Name: "TableCookStorageBytes", What: "uint8[N] with an int32 used length beside it"},
		// ---- THE GO PORT'S LOWERCASE FAMILY (docs/SPEC-TABLES.md §11) ----
		//
		// Go is the first backend whose runtime puts UNEXPORTED names at package
		// scope, and unexported is not private: a Go package is one namespace, so
		// `const tableJsonMaxDepth = 5` in a schema generates a redeclaration and
		// the unit does not compile. That is precisely the defect §11 promises
		// cannot happen, so every one of these is claimed on the same terms as the
		// PascalCase surface above.
		//
		// THE THREE SLICES ARE WHY THERE ARE ONLY THREE OF THEM. A descriptor graph
		// could have taken one variable per record — `cookRecordScene`,
		// `blockInfoRenderFrameShipRow` — and each of those is a name DERIVED from
		// a declaration's own spelling, which is a name a declaration can collide
		// with and which this registry has no shape for. One fixed name per graph
		// is one claim, and the emitters index into it.
		Name{Name: "tableBlockLayoutOffset", What: "the block layout contract's offset refusal"},
		Name{Name: "tableBlockLayoutSize", What: "the block layout contract's size refusal"},
		Name{Name: "tableBlockNativeOrder", What: "this machine's byte order, read once at package initialisation"},
		Name{Name: "tableBlockRecords", What: "the unit's whole block descriptor graph, one slice"},
		Name{Name: "tableCookLayoutOffset", What: "the cook layout contract's offset refusal"},
		Name{Name: "tableCookLayoutSize", What: "the cook layout contract's size refusal"},
		Name{Name: "tableCookNativeOrder", What: "this machine's byte order, read once at package initialisation"},
		Name{Name: "tableCookRecords", What: "the unit's whole cooked-record descriptor graph, one slice"},
		Name{Name: "tableJsonBase64Alphabet", What: "the base64 alphabet a `bytes` field rides under"},
		Name{Name: "tableJsonBytes", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonCount", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonElementShape", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonEncodeUtf8", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonFinite", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonGetRaw", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonGetSigned", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonGuardHolds", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonIn", What: "the text reader's cursor"},
		Name{Name: "tableJsonIsBytes", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonIsEnum", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonIsFlags", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonIsKeyed", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonKeyedSlotKey", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonKeyedSlotValid", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonMaxDepth", What: "the walk's nesting cap"},
		Name{Name: "tableJsonMaxKey", What: "the longest key the walk will place"},
		Name{Name: "tableJsonMaxNumber", What: "the longest numeric token the walk will convert"},
		Name{Name: "tableJsonNameIs", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonNamed", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonOut", What: "the text writer's sink"},
		Name{Name: "tableJsonRead", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonReadField", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonReadScalar", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonReadTable", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonSetCount", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonSetRaw", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonShape", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonText", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonTokenDouble", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonTokenInteger", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonUtf8", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWrite", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteBase64", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteField", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteFloat", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteScalar", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteSigned", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteString", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteUnsigned", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableJsonWriteValue", What: "one step of the text form's generic walk (§16)"},
		Name{Name: "tableUnionArms", What: "the unit's union-field shapes, one slice"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
