package tablenames

// Cpp is the C++ table backend (internal/codegen/cpptable).
const Cpp Backend = 1 << 0

func init() {
	define(Cpp,
		// the shared surface both backends define per unit
		Name{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		// RETAIN-UNKNOWN's caller-owned storage (docs/SPEC-TABLES.md §6.6): the
		// record bytes, the retained-id list, and what has been used of each. The
		// entry type rides INSIDE it and claims no name of its own.
		Name{Name: "TableRetain", What: "the caller's retention buffer and its retained-id list, which the C++ backend emits"},
		// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), claimed whenever a unit
		// declares a table, on the standing rule every runtime name here
		// follows: a name free today must not become a collision the day a
		// build starts speaking the form. The connection's own table, the
		// vocabulary of refusal reasons, and the three unit-scope entry
		// points, whose announcement is a compile-time constant of the unit.
		Name{Name: "TableVocabulary", What: "one direction of one connection's announced id table"},
		Name{Name: "TableMessageReason", What: "why a read was refused, by name — the message path's vocabulary, not the cooked form's (§7.4)"},
		Name{Name: "newer_form", What: "a form byte this reader does not carry (§3)"},
		Name{Name: "no_vocabulary", What: "no table for this connection"},
		Name{Name: "second_announcement", What: "a second announcement on a connection: it sets nothing and the connection closes"},
		Name{Name: "vocabulary_too_large", What: "an announcement above the receiver's declared bound"},
		Name{Name: "message_form_as_file", What: "a form 2 wire where a FILE was expected"},
		// THE FILE REFUSALS (docs/SPEC-TABLES.md §6.5, §7, §11, §19.2): the one
		// vocabulary a cook's Open, a block's BlockOpen and LoadMeasure's -1
		// share, and its values, which sit at namespace scope like the message
		// reasons above.
		Name{Name: "TableRefuseReason", What: "why a FILE was refused, by name. Open, BlockOpen and LoadMeasure's -1 share the one vocabulary (§6.5, §7, §19.2)"},
		Name{Name: "ok", What: "no clause failed: the only refusal value beside a non-null root (§7)"},
		Name{Name: "not_a_cook", What: "the magic is neither this build's nor its byte reversal (§7)"},
		Name{Name: "foreign_order", What: "a cook of the other byte order (§7.1)"},
		Name{Name: "wrong_build_version", What: "a build version this build does not match (§20)"},
		Name{Name: "reserved_not_zero", What: "a reserved header word that is not zero (§7.1)"},
		Name{Name: "bad_alignment", What: "an alignment word that is not a region's alignment (§7)"},
		Name{Name: "truncated", What: "the part lengths against the caller's length (§7)"},
		Name{Name: "unaligned_base", What: "a base not aligned for the region: the caller's defect (§7)"},
		Name{Name: "bad_layout", What: "a block's pitch, count, offset or extent that disagrees with this build's (§19.2)"},
		Name{Name: "unknown_form", What: "a form byte this build does not carry, at a measure (§6.5)"},
		Name{Name: "count_over_length", What: "an array or map count whose elements cannot fit the field's own L (§6.5)"},
		Name{Name: "count_over_extent_cap", What: "a count above the int32 extent cap (§6.5)"},
		Name{Name: "blob_over_size_cap", What: "a blob whose length is past the derived-size cap (§6.5)"},
		Name{Name: "data_cycle", What: "a data cycle reached from a builder: the authoring side's -1 (§6.5)"},
		Name{Name: "Announce", What: "the unit's announcement, written into the caller's buffer (§3.3)"},
		Name{Name: "AnnounceMeasure", What: "the announcement's byte count, a constant of the unit"},
		Name{Name: "AnnounceRead", What: "read an announcement into one direction's table"},
		Name{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		Name{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		Name{Name: "TableTypeInfo", What: "a table's reflection descriptor"},
		Name{Name: "TableFieldInfo", What: "a field's reflection descriptor"},
		// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every field row
		// and every declaration row with no `///` block names this ONE
		// definition, so absence costs a unit no string data and a printer
		// concatenates doc columns with no null test. Claimed wherever a unit
		// declares a table.
		Name{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names"},
		// a UNION field's shape (docs/SPEC-TABLES.md §8.1): the tag, and each arm's
		// payload by its own descriptor. Every backend defines both, and every one
		// puts them at unit level beside the two descriptors above — a union
		// field's column has to name a type, and a nested one would be reached
		// through a descriptor a walk holds by value.
		Name{Name: "TableUnionInfo", What: "a union field's tag and its arms"},
		Name{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor"},
		Name{Name: "TableEnumId", What: "an enum value -> its table-wire variant id"},
		Name{Name: "TableEnumValue", What: "a table-wire variant id -> its enum value"},
		// the ENUM-KEYED array's storage type (docs/SPEC-TABLES.md §2.4). C++ spells it
		// a class template and C# a generic class; both put it at unit level, and
		// both emit it ONLY into a unit that declares a keyed array — but the
		// claim is unconditional on a unit declaring a table, for the same reason
		// the variable-length names are: adding a keyed array to an existing table
		// must not turn a legal declaration elsewhere into a collision.
		Name{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		// the MAP's storage and its side index (docs/SPEC-TABLES.md §2.8),
		// claimed on TableKeyed's exact terms: a backend emits them only
		// into a unit that declares a map, and the claim is unconditional on
		// a unit declaring a table, so adding a map to an existing table
		// never turns a legal declaration elsewhere into a collision. They
		// are claimed with the CONSTRUCT rather than with the codec — a name
		// freed now is a collision the day the codec lands.
		Name{Name: "TableMap", What: "a map field's sorted entry array and its count"},
		Name{Name: "TableMapIndex", What: "a map's side index, built once and searched in place"},
		// the UNBOUNDED ARRAY's storage (docs/SPEC-TABLES.md §2.9), claimed on
		// TableMap's exact terms and for its reason: the map's slot with the
		// key taken out, emitted only into a unit that declares a `[]T` and
		// claimed in every unit that declares a table.
		Name{Name: "TableList", What: "an unbounded array's element reference and its count"},
		// C++'s float <-> IEEE-754 bit pattern helpers
		Name{Name: "table_bits_to_float", What: "u32 bits -> float"},
		Name{Name: "table_float_to_bits", What: "float -> u32 bits"},
		Name{Name: "table_bits_to_double", What: "u64 bits -> double"},
		Name{Name: "table_double_to_bits", What: "double -> u64 bits"},
		// C#'s twins, plus the reader's in-place prefill. These are VERB-FIRST on
		// purpose: §11 freezes the name-first suffixes a closure member claims
		// (internal/check's tableGeneratedVerbs), and a port does not mint another —
		// so the reset joins the runtime family here instead, where it is claimed at
		// unit level.
		// BOTH backends define TableReset, for the same reason and in two shapes:
		// C# has no `<Name>Reset` and this IS its reset, while C++ has one per
		// member and adds a verb-first overload set on top so the ARENA's generic
		// Alloc — a template, which cannot spell `<Name>Reset` — reaches a node's
		// declared defaults by argument-dependent lookup (§6). The C++ overloads
		// are emitted only into a unit with pointers, which is the only unit that
		// has an arena; the CLAIM does not vary with that, because a name free
		// today must not become a collision the day a table gains a pointer.
		Name{Name: "TableReset", What: "restore a value's declared defaults in place — C#'s reset itself, and C++'s ADL hook onto <Name>Reset for the arena. JAVA DOES NOT DEFINE IT: it spells the prefill <name>Reset, name-first as §11 already claims and lowerCamel as Java requires, so no family name is minted there. NEITHER DOES JAVASCRIPT, for the same reason: it has no overloading and spells the name-first <Name>Reset §11 already claims"},
		// the VARIABLE-LENGTH runtime (docs/SPEC-TABLES.md §6): the arena, the region
		// and the reference slot. C++ only today — the C# backend carries the
		// fixed class and refuses a pointered unit by name (§11).
		// TableRef carries a SECOND, unrelated meaning in C#: a field descriptor's
		// lazy factory for the nested table's descriptor, which is scoped. The
		// C++ meaning is unit-level, so the name is claimed whatever the target —
		// the claim is the union, never the intersection.
		Name{Name: "TableRef", What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
		Name{Name: "TableSlot", What: "an arena slot"},
		Name{Name: "TableArena", What: "the builder's segmented slab arena"},
		Name{Name: "TableSlab", What: "one worker's privately-owned slab of it"},
		Name{Name: "TableWorker", What: "a builder worker's allocation front"},
		Name{Name: "TableBuilder", What: "the mutable life's base"},
		Name{Name: "TableRegion", What: "the locked, packed region"},
		// Lock's identity map (docs/SPEC-TABLES.md §6.2): one entry per reachable
		// node, so a shared node is packed once and a reference to a node whose
		// descent is still open is a cycle. Emitted into a pointered unit's header
		// only, and claimed on the same terms as everything above — a name free
		// today must not become a collision the day a table gains a pointer.
		Name{Name: "TablePackMap", What: "the pack walk's identity map"},
		Name{Name: "TablePackEntry", What: "one node's entry in it: where it landed, and whether its descent is open"},
		// the FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1): the numbering a save
		// derives, the directory a load fills, and the record scan that is the
		// whole of load's bound. Emitted into a pointered unit's header only, and
		// claimed whenever a unit declares a table, on the same terms as the
		// variable-length names above.
		Name{Name: "TableNumbering", What: "one save's numbering: the identity map and the nodes in index order"},
		Name{Name: "TableNodeEntry", What: "one numbered node, with the thunks that reach its codec"},
		Name{Name: "TableNodeMeasure", What: "the numbering's bridge to a member's MeasureBody, by argument-dependent lookup"},
		Name{Name: "TableNodeSave", What: "the same bridge to a member's SaveBody"},
		Name{Name: "TableNodeMeasureThunk", What: "the instantiation a numbering stores for a node's measure"},
		Name{Name: "TableNodeSaveThunk", What: "the instantiation a numbering stores for a node's save"},
		Name{Name: "TableNodeDirEntry", What: "one entry of a region's node directory: an offset and a type id"},
		Name{Name: "TableNodeMap", What: "the resident numbering a pointer slot resolves through"},
		Name{Name: "TableNodeResolve", What: "place one node index in a pointer slot, or report why it did not"},
		Name{Name: "TableNodeScan", What: "the record scan over a root body's node-table fields"},
		// the BLOCK FORM's runtime (docs/SPEC-TABLES.md §19), emitted into
		// <Base>Block.h / <Base>Block.cs and into no Table source at all. Claimed
		// whenever a unit declares a table, on the same terms as everything above:
		// nothing declares the block form, every fixed table has one, and a table
		// gains and loses it as its closure gains and loses a pointer.
		Name{Name: "TableBlockAllocator", What: "the caller's alloc/free pair, used once at build time"},
		Name{Name: "TableBlockDefaultAllocator", What: "the malloc/free pair, for a caller with none of its own"},
		Name{Name: "table_block_default_alloc", What: "the default allocator's alloc half"},
		Name{Name: "table_block_default_free", What: "the default allocator's free half"},
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockRefusal", What: "why Begin refused: the array, its count and its maximum"},
		Name{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		Name{Name: "TableBlockSpan", What: "one array's rows as a contiguous view (C# uses ReadOnlySpan)"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "table_block_byteswap64", What: "the byte-order check's swap"},
		Name{Name: "table_block_read64", What: "the prologue read BYTEWISE"},
		Name{Name: "table_block_align", What: "round an offset up to an alignment"},
		// the COOKED FORM's read runtime (docs/SPEC-TABLES.md §7), emitted into
		// <Base>Table.h of a unit that declares a variable-length table — a cook's
		// root is one — and into no value-only unit's header at all. Claimed
		// whenever a unit declares a table, on the same terms as the
		// variable-length names above: a table gains and loses its cook reader as
		// its closure gains and loses a pointer, and a name free today must not
		// become a collision tomorrow.
		Name{Name: "TableCookOpen", What: "the cooked header's WHOLE check, shared by every <Name>Open"},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "table_cook_read64", What: "the cooked header read BYTEWISE"},
		// and the COOKED FORM's WRITE runtime (docs/SPEC-TABLES.md §7.6), emitted
		// beside the read half in the same guard. Three names: the byte order a
		// cook is WRITTEN in, one store and one buffer copy. A store takes its
		// width as an argument rather than minting a name per width — every call
		// site passes a literal, so the loop folds — because a claimed name costs
		// every schema in every unit and saves nothing here.
		Name{Name: "TableByteOrder", What: "the byte order a cook is WRITTEN in, as Cook's parameter"},
		Name{Name: "table_cook_put", What: "one scalar into a cook, in the target's byte order"},
		Name{Name: "table_cook_bytes", What: "a string or bytes buffer's used prefix into a cook"},
		// and its POINTERED half, in a unit that has a variable-length table: the
		// region being laid out and written, and the one reference store
		Name{Name: "TableCookRegion", What: "a pointered root's region while a cook lays it out and writes it: the numbering, one offset per node, the extent and the alignment"},
		Name{Name: "table_cook_ref", What: "a reference slot into a cook: the self-relative delta, or a refusal for a node the numbering did not reach"},
		// the unit's BUILD VERSION (docs/SPEC-TABLES.md §20): the one digest a block
		// carries and BlockOpen compares. It is not a Table* spelling, and it is
		// claimed here because it is a unit-level name the generated block sources
		// define.
		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
