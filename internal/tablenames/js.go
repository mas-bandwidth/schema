package tablenames

// Js is the JavaScript table backend (internal/codegen/jstable). It is the
// READING TIER: no struct layout, so it reads both accelerators and
// produces neither.
const Js Backend = 1 << 7

func init() {
	define(Js,
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		Name{Name: "TableJson", What: "the text form's generic walk. C# spells it a nested class of Schema, which claims nothing; Java puts it at package scope, Dart at library scope and JavaScript at module scope — an ES module is one scope, with no nested class to hide a walk in — so the claim is the UNION and the name is claimed"},
		// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every descriptor row
		// and registry record with no `///` block names this ONE definition, so
		// absence costs a unit no string data and a printer concatenates doc
		// columns with no null test. Claimed wherever a unit declares a table.
		Name{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names"},
		Name{Name: "TableBitsToFloat", What: "u32 bits -> float"},
		Name{Name: "TableFloatToBits", What: "float -> u32 bits"},
		Name{Name: "TableBitsToDouble", What: "u64 bits -> double"},
		Name{Name: "TableDoubleToBits", What: "double -> u64 bits"},
		// JavaScript's four go through one shared eight-byte DataView, which is a
		// module-level binding like any other and takes a name in this family for
		// that reason: a SCREAMING_SNAKE spelling would have been a module-scope
		// name outside every claim the front end makes.
		Name{Name: "TableBitsScratch", What: "the JavaScript bit helpers' shared eight-byte DataView"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockLayout", What: "the layout contract's check, run once. C# spells it a nested class of Schema; Java puts it at package scope and JavaScript at module scope, so the claim is the UNION"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
