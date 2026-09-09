package tablenames

// Rust is the Rust table backend (internal/codegen/rusttable).
const Rust Backend = 1 << 2

func init() {
	define(Rust,
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableBlockByteswap64", What: "the block magic's byte-order swap", Scoped: true},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		// THE FIXED FORM's runtime (docs/SPEC-TABLES.md §3.4), form byte 3.
		// Emitted for a unit that carries a fixed root, and claimed beside
		// any table on this list's own rule: a name free today is a
		// collision the day a table in that unit becomes a fixed root.
		Name{Name: "TableFixedForm", What: "the fixed form's form byte", RustConst: true},
		Name{Name: "TableFixedNoGuard", What: "the guard offset a plan entry that belongs to no union arm carries", RustConst: true},
		Name{Name: "TableFixedOp", What: "the fixed form's plan ops — the whole set"},
		Name{Name: "TableFixedEntry", What: "one plan entry: src, dst, size and the op that moves them"},
		Name{Name: "TableFixedReason", What: "the fixed form's refusals, each one BY NAME"},
		Name{Name: "TableFixedReport", What: "the fixed form's read report — §4's ledger and this form's refusals"},
		Name{Name: "TableFixedBlock", What: "a parsed vocabulary block: the bytes and the entry count"},
		Name{Name: "TableFixedBlockEntry", What: "one block entry — an id, a kind, a constant size and a child count"},
		Name{Name: "TableFixedHash", What: "fnv1a64 over a block's bytes, which is the eight bytes every record carries", Scoped: true},
		Name{Name: "TableFixedRun", What: "THE ONE READ LOOP: one plan over one record body", Scoped: true},
		Name{Name: "TableFixedCompile", What: "the plan compiler, run once per peer and never once per record", Scoped: true},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
