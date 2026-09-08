package tablenames

// Java is the Java table backend (internal/codegen/javatable).
const Java Backend = 1 << 4

func init() {
	define(Java,
		// JAVA's byte-access primitive. C++ reads a record through its type, C#
		// through a pointer cast and Rust through a transmute; Java has none of
		// those, so every multi-byte read of a block or a cook goes through one
		// package-level class of explicit little-endian readers — which is also
		// what settles the byte order of both accelerators without asking the host.
		Name{Name: "TableBytes", What: "explicit little-endian reads out of a byte[] (Java's block and cook read through it)"},
		Name{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockLayout", What: "the layout contract's check, run once. C# spells it a nested class of Schema; Java puts it at package scope and JavaScript at module scope, so the claim is the UNION"},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
