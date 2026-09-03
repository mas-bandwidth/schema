package tablenames

// Cs is the C# table backend (internal/codegen/cstable).
const Cs Backend = 1 << 1

func init() {
	define(Cs,
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor"},
		Name{Name: "TableEnumId", What: "an enum value -> its table-wire variant id"},
		Name{Name: "TableEnumValue", What: "a table-wire variant id -> its enum value"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		// SCOPED: a field descriptor's nested-table column. C++ spells it `table`
		// and C# `Table`, and either way it is reached through its owner, so a
		// schema is free to declare the name.
		Name{Name: "Table", What: "TableFieldInfo's nested-table column", Scoped: true},
		Name{Name: "TableReset", What: "restore a value's declared defaults in place — C#'s reset itself, and C++'s ADL hook onto <Name>Reset for the arena. JAVA DOES NOT DEFINE IT: it spells the prefill <name>Reset, name-first as §11 already claims and lowerCamel as Java requires, so no family name is minted there. NEITHER DOES JAVASCRIPT, for the same reason: it has no overloading and spells the name-first <Name>Reset §11 already claims"},
		// SCOPED: the TEXT FORM's generic walk (docs/SPEC-TABLES.md §16), a nested
		// static class of Schema. Everything the walk spells — its reader, its
		// writer sink, its scanners — is a member of it, so one registration
		// covers the whole surface and the walk claims not one name at unit
		// scope. C++ spells the same functions as a TableJson* family at
		// namespace scope inside its own package guard.
		Name{Name: "TableJson", What: "the text form's generic walk. C# spells it a nested class of Schema, which claims nothing; Java puts it at package scope, Dart at library scope and JavaScript at module scope — an ES module is one scope, with no nested class to hide a walk in — so the claim is the UNION and the name is claimed"},
		Name{Name: "TableBitsToFloat", What: "u32 bits -> float"},
		Name{Name: "TableFloatToBits", What: "float -> u32 bits"},
		Name{Name: "TableBitsToDouble", What: "u64 bits -> double"},
		Name{Name: "TableDoubleToBits", What: "double -> u64 bits"},
		Name{Name: "TableRef", What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockLayout", What: "the layout contract's check, run once. C# spells it a nested class of Schema; Java puts it at package scope and JavaScript at module scope, so the claim is the UNION"},
		Name{Name: "TableBlockRead64", What: "the prologue read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		// the COOK's C# half (docs/SPEC-TABLES.md §7, §19.2's road). C# has no include
		// guard, so the read runtime is emitted ONCE per unit into the cook home's
		// <Base>Cook.cs — and because one assembly sees every file, a declaration
		// taking one of these names anywhere in the unit collides with it.
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookRead64", What: "the cooked header read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
	)
}
