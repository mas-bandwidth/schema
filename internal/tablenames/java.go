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
		Name{Name: "TableBlockRefusal", What: "the producer count that violates its declared bound (docs/SPEC-TABLES.md §19.1), the Java spelling of Go's TableBlockRefusal"},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		// THE FIXED FORM's runtime (docs/SPEC-TABLES.md §3.4), form byte 3, and
		// it is ONE class: Java has no free functions, so the plan entry, the
		// report, the layout reader and the one read loop all live inside
		// TableFixed as nested types and statics — which is what keeps the
		// claim to a single name where C++ spends a dozen. Emitted for a unit
		// that carries a fixed root, and claimed beside any table on this
		// list's own rule: a name free today is a collision the day a table in
		// that unit becomes a fixed root. The `<Type>Fixed` classes beside it
		// are named for a DECLARATION and are refused by the backend's own
		// file-collision check rather than by a registry of fixed names.
		Name{Name: "TableFixed", What: "the fixed form's runtime: the plan entry, the report, the layout reader with its seven named rules, the ONE read loop and the plan compiler (docs/SPEC-TABLES.md §3.4)"},
		// THE VARIABLE FORM's runtime (docs/SPEC-TABLES.md §3), form byte 1, and
		// the TEXT FORM's (§16): one class each at package scope, beside the
		// per-table <Type>Variable codecs. Emitted for a unit whose variable
		// tables this backend carries, and claimed beside any table on this
		// list's own rule: a name free today is a collision the day a table in
		// that unit becomes a variable root.
		Name{Name: "TableVariable", What: "the variable form's runtime: the LEB128 reader/writer, the id-table trailer, the flat node table and the report (docs/SPEC-TABLES.md §3, §3.1, §4)"},
		Name{Name: "TableJson", What: "the text form's runtime: JSON in and out of a table (docs/SPEC-TABLES.md §16)"},
		// THE RETAIN-UNKNOWN runtime (§6.6) and the reflection descriptors the
		// variable form's fields describe (§8, §8.3), emitted beside the
		// variable form.
		Name{Name: "TableRetain", What: "the retain-unknown storage and resolving walk (docs/SPEC-TABLES.md §6.6)"},
		Name{Name: "TableFieldInfo", What: "a table field's reflection descriptor (docs/SPEC-TABLES.md §8)"},
		Name{Name: "TableTypeInfo", What: "a table type's reflection descriptor (docs/SPEC-TABLES.md §8)"},
		Name{Name: "TableRegistry", What: "the unit's registry of table descriptors (docs/SPEC-TABLES.md §8.3)"},
		// THE MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md §3.3),
		// form byte 2: one public type holding the announcement, its reader and
		// the resolved vocabulary.
		Name{Name: "TableVocabulary", What: "the message form's announcement surface (docs/SPEC-TABLES.md §3.3)"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
