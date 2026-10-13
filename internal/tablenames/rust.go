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
		Name{Name: "TableFixedGuard", What: "one plan guard link: the tag ordinal, its argument and its width in bytes"},
		Name{Name: "TableFixedOp", What: "the fixed form's plan ops — the whole set"},
		Name{Name: "TableFixedEntry", What: "one plan entry: src, dst, size and the op that moves them"},
		Name{Name: "TableFixedKnown", What: "one known layout of a lineage entry: its hash, its bytes and its record size — the Rust leg's spelling of the page's TableFixedKnownLayout"},
		Name{Name: "TableFixedReason", What: "the fixed form's refusals, each one BY NAME"},
		Name{Name: "TableFixedReport", What: "the fixed form's read report — §4's ledger and this form's refusals"},
		Name{Name: "TableFixedBlock", What: "a parsed vocabulary block: the bytes and the entry count"},
		Name{Name: "TableFixedBlockEntry", What: "one block entry — an id, a kind, a constant size and a child count"},
		Name{Name: "TableFixedHash", What: "fnv1a64 over a block's bytes, which is the eight bytes every record carries", Scoped: true},
		Name{Name: "TableFixedHole", What: "one dst range the plan does not land"},
		Name{Name: "TableFixedHoles", What: "the complement of the unguarded dest writes — what the prefill copies into", Scoped: true},
		Name{Name: "TableFixedRun", What: "THE ONE READ LOOP: one plan over one record body", Scoped: true},
		Name{Name: "TableFixedCompile", What: "the plan compiler, run once per peer and never once per record", Scoped: true},
		Name{Name: "TableFixedHeaderBytes", What: "the fixed form's pinned header: sixteen bytes, the body at 16 (§3)", RustConst: true},
		Name{Name: "TableFixedHashAt", What: "the fixed form's pinned header: the layout hash at offset 8 (§3)", RustConst: true},
		Name{Name: "TableFixedTag", What: "a union tag read at its OWN width, which is what keeps a foreign 0x0101 from firing arm 1", Scoped: true},
		Name{Name: "TableUtf8Valid", What: "§3's one content rule for a string(N): well-formed UTF-8 with no zero among the used bytes", Scoped: true},
		Name{Name: "TableUtf16Valid", What: "§3's one content rule for a wstring(N): paired UTF-16 with no zero among the used units", Scoped: true},
		Name{Name: "TableU128", What: "an unsigned 128-bit ROW SLOT, sixteen bytes aligned SIXTEEN on every target — Rust's own u128 takes the target's C alignment, which is eight on s390x"},
		Name{Name: "TableI128", What: "a signed 128-bit ROW SLOT, sixteen bytes aligned SIXTEEN on every target"},
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
		// THE MESSAGE FORM's runtime (docs/SPEC-TABLES.md §3.3): the two form
		// bytes and the announcement's reserved ids and bound, emitted for a
		// unit that carries a fixed root.
		Name{Name: "TableWireForm", What: "the VARIABLE FORM's form byte (§3)", RustConst: true},
		Name{Name: "TableWireMessageForm", What: "the MESSAGE FORM's form byte (§3.3)", RustConst: true},
		Name{Name: "TableNodeTableId", What: "the reserved node-table id (§3.1)", RustConst: true},
		Name{Name: "TableBuildVersionFieldId", What: "the reserved id the announcement's build-version field rides under (§3.3)", RustConst: true},
		Name{Name: "TableMessageVocabularyFieldId", What: "the reserved id the announcement's vocabulary field rides under (§3.3)", RustConst: true},
		Name{Name: "TableMessageMaxBytes", What: "the announcement's conforming byte bound (§3.3)", RustConst: true},
		Name{Name: "TableReason", What: "the message form's refusal reasons, each one by name"},
		// THE VARIABLE WIRE's runtime (docs/SPEC-TABLES.md §3, §3.1), emitted for
		// a unit with a variable table the Rust leg carries: the save's node
		// numbering, the load's node reader and its slots, the scalar ladder,
		// the retention walk's cap, and the card descriptors.
		Name{Name: "TableNodeGraph", What: "the save's node numbering: first-visit order over pointer edges (§3.1)"},
		Name{Name: "TableNodeRecord", What: "one record of the save's numbering: a table node or a blob (§3.1, §2.5)"},
		Name{Name: "TableNodeReader", What: "the node table a load gathers before any body (§3.1)"},
		Name{Name: "TableNodeSlot", What: "one record's state in a load's numbering (§3.1)"},
		Name{Name: "TableKindWidth", What: "a fixed-width kind's payload size (§3)", Scoped: true},
		Name{Name: "TableKindWidens", What: "the widening ladder: a narrower kind decoded exactly into a wider declaration (§4)", Scoped: true},
		Name{Name: "TableScalarRead", What: "one scalar read at its kind's own width (§3, §4)", Scoped: true},
		Name{Name: "TableWidenF32", What: "a float32 widened into a float64 bit-exactly, NaN payload included (§4)", Scoped: true},
		Name{Name: "TableRetainDepthCap", What: "the retain walk's nesting cap (§6.6)", RustConst: true},
		Name{Name: "TableDesc", What: "one table's descriptor in the card descriptors (§8.1)"},
		Name{Name: "TableFieldDesc", What: "one field's descriptor in the card descriptors (§8.1)"},
	)
}
