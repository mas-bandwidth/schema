package tablenames

// Unity is the Unity C# table backend (internal/codegen/unitytable). Its
// runtime types live in the unit's namespace and `public static partial class Schema`,
// matching the C# backend's surface over C# 9 and netstandard2.1; every name
// here is already registered by the C# and C++ backends, so the Unity leg adds
// no global claim of its own.
//
// The allocation takes bit 14, the next one after Python's 13.
const Unity Backend = 1 << 14

func init() {
	define(Unity,
		Name{Name: "Table", What: "TableFieldInfo's nested-table column", Scoped: true},
		Name{Name: "Tables", What: "the registry table set", Scoped: true},
		Name{Name: "TableBitsToDouble", What: "u64 bits -> double"},
		Name{Name: "TableBitsToFloat", What: "u32 bits -> float"},
		Name{Name: "TableBlob", What: "identity-bearing byte storage for a variable table graph"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it"},
		Name{Name: "TableBlockRead64", What: "the prologue read bytewise"},
		Name{Name: "TableBlockRefusal", What: "block count refusal"},
		Name{Name: "TableByteOrder", What: "runtime cook byte order"},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it"},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name"},
		Name{Name: "TableCookRead64", What: "the cooked header read bytewise"},
		Name{Name: "TableCookStorage", What: "what a cooked slot holds (§7.2)"},
		Name{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names"},
		Name{Name: "TableDoubleToBits", What: "double -> u64 bits"},
		Name{Name: "TableEnumId", What: "an enum value -> its table-wire variant id"},
		Name{Name: "TableEnumValue", What: "a table-wire variant id -> its enum value"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		Name{Name: "TableFloatToBits", What: "float -> u32 bits"},
		Name{Name: "TableIntegerKind", What: "TableJson's test for a pure integer kind (§16.2)", Scoped: true},
		Name{Name: "TableIntegerToken", What: "TableJson's integer-token rule: digits only, no fraction or exponent (§16.2)", Scoped: true},
		Name{Name: "TableJson", What: "the text form's generic walk, nested in Schema"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		Name{Name: "TableMessageEntry", What: "announced entry"},
		Name{Name: "TableMessageShape", What: "announced payload shape"},
		Name{Name: "TableRef", What: "a field descriptor's nested-table factory"},
		Name{Name: "TableRefuseReason", What: "the first failing clause of a file refusal"},
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableReset", What: "restore a value's declared defaults in place"},
		Name{Name: "TableRetain", What: "the retain-unknown storage and resolving walk (§6.6)"},
		Name{Name: "TableShapeMatches", What: "TableJson's shape test: exact, or a string or number for a wide integer (§16.2)", Scoped: true},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor"},
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableVocabulary", What: "connection vocabulary"},
		Name{Name: "TableWideJson", What: "TableJson's test for a kind wider than 32 bits, written as a string of digits (§16.2)", Scoped: true},
		Name{Name: "TableWire", What: "the form-1 wire walk, nested in Schema"},
	)
}
