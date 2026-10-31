package tablenames

// Cpp is the C++ table backend (internal/codegen/cpptable).
const Cpp Backend = 1 << 0

func init() {
	names := []Name{
		// the shared surface both backends define per unit
		{Name: "TableReport", What: "the read report — the permissive contract's ledger"},
		// RETAIN-UNKNOWN's caller-owned storage (docs/SPEC-TABLES.md §6.6): the
		// record bytes, the retained-id list, and what has been used of each. The
		// entry type rides INSIDE it and claims no name of its own.
		{Name: "TableRetain", What: "the caller's retention buffer and its retained-id list, which the C++ backend emits"},
		// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), claimed whenever a unit
		// declares a table, on the standing rule every runtime name here
		// follows: a name free today must not become a collision the day a
		// build starts speaking the form. The connection's own table, the
		// vocabulary of refusal reasons, and the three unit-scope entry
		// points, whose announcement is a compile-time constant of the unit.
		{Name: "TableVocabulary", What: "one direction of one connection's announced id table"},
		{Name: "TableMessageReason", What: "why a read was refused, by name — the message path's spelling of the one TableRefuseReason vocabulary (§3.3, §6.5)"},
		{Name: "newer_form", What: "a form byte this reader does not carry (§3)"},
		{Name: "previous_form", What: "a form byte this form is AHEAD of — the variable form handed to a fixed reader (§3, §3.4)"},
		{Name: "no_vocabulary", What: "no table for this connection"},
		{Name: "second_announcement", What: "a second announcement on a connection: it sets nothing and the connection closes"},
		{Name: "vocabulary_too_large", What: "an announcement above the receiver's declared bound"},
		{Name: "message_form_as_file", What: "a form 2 wire where a FILE was expected"},
		// THE FILE REFUSALS (docs/SPEC-TABLES.md §6.5, §7, §11, §19.2): the one
		// vocabulary a cook's Open, a block's BlockOpen and LoadMeasure's -1
		// share, and its values, which sit at namespace scope like the message
		// reasons above.
		// THE TEXT FORM'S BASE64 BODY (docs/SPEC-TABLES.md §16.2): one scan,
		// the JSON layer first, shared by bytes(N) and *bytes.
		{Name: "TableJsonBase64Char", What: "one character of a base64 body after the JSON layer: escapes undone, ill-formed text refused (§16.2)"},
		{Name: "TableJsonScanBase64", What: "a base64 body's first pass: the JSON layer, then the walk's base64 grammar (§16.2)"},
		{Name: "TableJsonDecodeBase64", What: "a base64 body's second pass: six bits a symbol into the field, the clamp counted (§16.2)"},
		{Name: "TableRefuseReason", What: "why a FILE was refused, by name. Open, BlockOpen and LoadMeasure's -1 share the one vocabulary (§6.5, §7, §19.2)"},
		{Name: "ok", What: "no clause failed: the only refusal value beside a non-null root (§7)"},
		{Name: "not_a_cook", What: "the magic is neither this build's nor its byte reversal (§7)"},
		{Name: "foreign_order", What: "a cook of the other byte order (§7.1)"},
		{Name: "wrong_build_version", What: "a build version this build does not match (§20)"},
		{Name: "reserved_not_zero", What: "a reserved header word that is not zero (§7.1)"},
		{Name: "bad_alignment", What: "an alignment word that is not a region's alignment (§7)"},
		{Name: "truncated", What: "the part lengths against the caller's length (§7)"},
		{Name: "unaligned_base", What: "a base not aligned for the region: the caller's defect (§7)"},
		{Name: "bad_layout", What: "a block's pitch, count, offset or extent that disagrees with this build's (§19.2)"},
		{Name: "unknown_form", What: "a form byte this build does not carry, at a measure (§6.5)"},
		{Name: "count_over_length", What: "an array or map count whose elements cannot fit the field's own L (§6.5)"},
		{Name: "count_over_extent_cap", What: "a count above the int32 extent cap (§6.5)"},
		{Name: "blob_over_size_cap", What: "a blob whose length is past the derived-size cap (§6.5)"},
		{Name: "data_cycle", What: "a data cycle reached from a builder: the authoring side's -1 (§6.5)"},
		// THE VALUES GO APPENDED AFTER data_cycle (docs/SPEC-TABLES.md §6.5,
		// §11): the strict read's, the tolerant report's, the carrier's, the
		// integrity trailer's and the blob's refusals, in Go's order. They are
		// appended and never renumbered, and no call writes one yet.
		{Name: "invalid_value", What: "a writer handed a value outside the declaration (§3.1, §11)"},
		{Name: "allocation_failed", What: "an allocation the codec asked for was refused (§7.6)"},
		{Name: "wire_damaged", What: "a read that reached damage rather than a refusal (§4)"},
		{Name: "malformed", What: "a tolerant read that saw a malformed body; the report counts it (§4)"},
		{Name: "clamped", What: "a tolerant read that clamped a value to its declared bound (§4)"},
		{Name: "kind_mismatch", What: "a wire field whose kind does not match the declaration (§4)"},
		{Name: "base64_damaged", What: "base64 text with a byte outside the alphabet or the wrong padding (§16.8)"},
		{Name: "base64_not_canonical", What: "base64 text that decodes but is not the canonical spelling of its bytes (§16.8)"},
		{Name: "integrity_mismatch", What: "the integrity trailer does not match the bytes before it (§2.14, §3, §19)"},
		{Name: "non_finite_float", What: "a float whose bits are a NaN or an infinity; every read and every writer refuses it (§3.3)"},
		{Name: "blob_shared", What: "one blob record named from two slots; every writer refuses to emit it (§2.5)"},
		{Name: "Announce", What: "the unit's announcement, written into the caller's buffer (§3.3)"},
		{Name: "AnnounceMeasure", What: "the announcement's byte count, a constant of the unit"},
		{Name: "AnnounceRead", What: "read an announcement into one direction's table"},
		{Name: "TableWriter", What: "the wire writer over the caller's buffer"},
		{Name: "TableReader", What: "the wire reader over the caller's buffer"},
		// THE DESCRIPTOR SURFACE, and the one part of this registry a VIEW
		// FILE carries itself (docs/SPEC-TABLES.md §8.2): the vocabulary
		// keeps its Table spellings "in a unit that declares no table at
		// all", and a table-free unit carries the descriptor primitives in
		// its view file behind the same include guard the table headers use.
		// These carry View, so they are the names claimed in EVERY unit; the
		// reason docs/SPEC-TABLES.md:560 gives for an every-unit claim is
		// this one, and it reaches exactly this far.
		{Name: "TableTypeInfo", What: "a table's reflection descriptor", View: true},
		{Name: "TableWideRange", What: "a wide scalar reflection range", View: true},
		{Name: "TableFieldInfo", What: "a field's reflection descriptor", View: true},
		// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every field row
		// and every declaration row with no `///` block names this ONE
		// definition, so absence costs a unit no string data and a printer
		// concatenates doc columns with no null test. Every row a view file
		// writes names it, so it rides with the descriptors above.
		{Name: "TableDocNone", What: "the one shared empty doc every unannotated descriptor row names", View: true},
		// a UNION field's shape (docs/SPEC-TABLES.md §8.1): the tag, and each arm's
		// payload by its own descriptor. Every backend defines both, and every one
		// puts them at unit level beside the two descriptors above — a union
		// field's column has to name a type, and a nested one would be reached
		// through a descriptor a walk holds by value. The view file spells
		// both, on a union field's row, so they take the descriptors' scope.
		{Name: "TableUnionInfo", What: "a union field's tag and its arms", View: true},
		{Name: "TableUnionArmInfo", What: "one union arm's payload and descriptor", View: true},
		// the identity pair is NOT view surface, and the measurement says so:
		// a declaration no table closure reaches has ids nothing ever checked,
		// so its rows carry id 0 and its variant_id column answers 0 as a
		// literal (docs/SPEC-TABLES.md §8.2). A view file over out-of-closure
		// declarations, which is the whole of a table-free unit's view,
		// spells neither name.
		{Name: "TableEnumId", What: "an enum value -> its table-wire variant id"},
		{Name: "TableEnumValue", What: "a table-wire variant id -> its enum value"},
		// the ENUM-KEYED array's storage type (docs/SPEC-TABLES.md §2.4). C++ spells it
		// a class template and C# a generic class; both put it at unit level, and
		// both emit it ONLY into a unit that declares a keyed array — but the
		// claim is unconditional on a unit declaring a table, for the same reason
		// the variable-length names are: adding a keyed array to an existing table
		// must not turn a legal declaration elsewhere into a collision.
		{Name: "TableKeyed", What: "an enum-keyed array's slot storage"},
		// the MAP's storage and its side index (docs/SPEC-TABLES.md §2.8),
		// claimed on TableKeyed's exact terms: a backend emits them only
		// into a unit that declares a map, and the claim is unconditional on
		// a unit declaring a table, so adding a map to an existing table
		// never turns a legal declaration elsewhere into a collision. They
		// are claimed with the CONSTRUCT rather than with the codec — a name
		// freed now is a collision the day the codec lands.
		{Name: "TableMap", What: "a map field's sorted entry array and its count"},
		{Name: "TableMapIndex", What: "a map's side index, built once and searched in place"},
		// the UNBOUNDED ARRAY's storage (docs/SPEC-TABLES.md §2.9), claimed on
		// TableMap's exact terms and for its reason: the map's slot with the
		// key taken out, emitted only into a unit that declares a `[]T` and
		// claimed in every unit that declares a table.
		{Name: "TableList", What: "an unbounded array's element reference and its count"},
		// THE HAND-EDITABLE TEXT FORM (§16.9): the strict s-expression reader
		// and its one resolver, emitted into every Table.cpp behind the
		// package guard. Claimed unconditionally with the rest of the C++
		// runtime, on the standing rule above: a name free today must not
		// become a collision the day a unit starts carrying the form.
		{Name: "TableTextFormNode", What: "one s-expression document node — a list, a string, a number, a keyword or a literal (§16.9.1)"},
		{Name: "TableTextFormReader", What: "the strict s-expression lexer and parser (§16.9.1)"},
		{Name: "TableB64Val", What: "one base64 alphabet position (§16.9.2)"},
		{Name: "TableB64Valid", What: "canonical base64: zero pad bits and no stray byte, with its decoded length (§16.9.2)"},
		{Name: "TableUtf16Length", What: "a UTF-8 string's length in UTF-16 code units (§16.9.2)"},
		{Name: "TableTextFormKeyNode", What: "whether a node is a record key: a keyword or a string (§16.9.1)"},
		{Name: "TableTextFormJsonString", What: "escape decoded UTF-8 as a JSON string (§16.9.2)"},
		{Name: "TableTextFormRecord", What: "resolve one record against its descriptor (§16.9.2)"},
		{Name: "TableTextFormField", What: "resolve one field value against its descriptor (§16.9.2)"},
		{Name: "TableTextFormScalar", What: "resolve one scalar, array element, keyed slot or arm value (§16.9.2)"},
		{Name: "TableTextFormToJson", What: "transcode one strict document into the canonical JSON of the same value (§16.9.1, §16.9.2)"},
		// C++'s float <-> IEEE-754 bit pattern helpers
		{Name: "table_bits_to_float", What: "u32 bits -> float"},
		{Name: "table_float_to_bits", What: "float -> u32 bits"},
		{Name: "table_bits_to_double", What: "u64 bits -> double"},
		{Name: "table_double_to_bits", What: "double -> u64 bits"},
		// THE FINITE RULE (docs/SPEC-TABLES.md §3.3): the read's test on the raw
		// bits, the writer's on a value, and the report's refusal
		{Name: "TableBitsFinite32", What: "true when a float32's raw bits are finite: the read's non_finite_float test (§3.3)"},
		{Name: "TableBitsFinite64", What: "true when a float64's raw bits are finite: the read's non_finite_float test (§3.3)"},
		{Name: "TableFloatFinite", What: "true when a float or a double is finite: the writer's refusal of a NaN or an infinity (§3.3)"},
		{Name: "TableNonFiniteFloat", What: "a read meeting a NaN or an infinity: refused as non_finite_float (§3.3)"},
		{Name: "TableNonFiniteRefused", What: "a load's own end: whether its walk met a non-finite float, the refusal stamped (§3.3)"},
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
		{Name: "TableReset", What: "restore a value's declared defaults in place — C#'s reset itself, and C++'s ADL hook onto <Name>Reset for the arena. JAVA DOES NOT DEFINE IT: it spells the prefill <name>Reset, name-first as §11 already claims and lowerCamel as Java requires, so no family name is minted there. NEITHER DOES JAVASCRIPT, for the same reason: it has no overloading and spells the name-first <Name>Reset §11 already claims"},
		// the VARIABLE-LENGTH runtime (docs/SPEC-TABLES.md §6): the arena, the region
		// and the reference slot. C++ only today — the C# backend carries the
		// fixed class and refuses a pointered unit by name (§11).
		// TableRef carries a SECOND, unrelated meaning in C#: a field descriptor's
		// lazy factory for the nested table's descriptor, which is scoped. The
		// C++ meaning is unit-level, so the name is claimed whatever the target —
		// the claim is the union, never the intersection.
		// TableRef and TableWorker reach a view file only through a POINTER
		// field's resolve and emplace lambdas (§8.1), and a pointer is
		// refused outside a table body (§2.1), so no view file over
		// out-of-closure declarations spells either, and neither is claimed
		// in a table-free unit.
		{Name: "TableRef", What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
		{Name: "TableSlot", What: "an arena slot"},
		{Name: "TableArena", What: "the builder's segmented slab arena"},
		{Name: "TableWorker", What: "a builder worker's allocation front"},
		// Lock's identity map (docs/SPEC-TABLES.md §6.2): one entry per reachable
		// node, so a shared node is packed once and a reference to a node whose
		// descent is still open is a cycle. Emitted into a pointered unit's header
		// only, and claimed on the same terms as everything above — a name free
		// today must not become a collision the day a table gains a pointer.
		{Name: "TablePackMap", What: "the pack walk's identity map"},
		{Name: "TablePackEntry", What: "one node's entry in it: where it landed, and whether its descent is open"},
		// the FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1): the numbering a save
		// derives, the directory a load fills, and the record scan that is the
		// whole of load's bound. Emitted into a pointered unit's header only, and
		// claimed whenever a unit declares a table, on the same terms as the
		// variable-length names above.
		{Name: "TableNumbering", What: "one save's numbering: the identity map and the nodes in index order"},
		{Name: "TableNodeEntry", What: "one numbered node, with the thunks that reach its codec"},
		{Name: "TableNodeMeasure", What: "the numbering's bridge to a member's MeasureBody, by argument-dependent lookup"},
		{Name: "TableNodeSave", What: "the same bridge to a member's SaveBody"},
		{Name: "TableNodeMeasureThunk", What: "the instantiation a numbering stores for a node's measure"},
		{Name: "TableNodeSaveThunk", What: "the instantiation a numbering stores for a node's save"},
		{Name: "TableNodeDirEntry", What: "one entry of a region's node directory: an offset and a type id"},
		{Name: "TableNodeMap", What: "the resident numbering a pointer slot resolves through"},
		{Name: "TableNodeResolve", What: "place one node index in a pointer slot, or report why it did not"},
		{Name: "TableNodeScan", What: "the record scan over a root body's node-table fields"},
		// the BLOCK FORM's runtime (docs/SPEC-TABLES.md §19), emitted into
		// <Base>Block.h / <Base>Block.cs and into no Table source at all. Claimed
		// whenever a unit declares a table, on the same terms as everything above:
		// nothing declares the block form, every fixed table has one, and a table
		// gains and loses it as its closure gains and loses a pointer.
		{Name: "TableBlockAllocator", What: "the caller's alloc/free pair, used once at build time"},
		{Name: "TableBlockDefaultAllocator", What: "the malloc/free pair, for a caller with none of its own"},
		{Name: "table_block_default_alloc", What: "the default allocator's alloc half"},
		{Name: "table_block_default_free", What: "the default allocator's free half"},
		{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		{Name: "TableBlockRefusal", What: "why Begin refused: the array, its count and its maximum"},
		{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		{Name: "TableBlockSpan", What: "one array's rows as a contiguous view (C# uses ReadOnlySpan)"},
		{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		{Name: "table_block_byteswap64", What: "the byte-order check's swap"},
		{Name: "table_block_read64", What: "the prologue read BYTEWISE"},
		{Name: "table_block_align", What: "round an offset up to an alignment"},
		// the COOKED FORM's read runtime (docs/SPEC-TABLES.md §7), emitted into
		// <Base>Table.h of a unit that declares a variable-length table — a cook's
		// root is one — and into no value-only unit's header at all. Claimed
		// whenever a unit declares a table, on the same terms as the
		// variable-length names above: a table gains and loses its cook reader as
		// its closure gains and loses a pointer, and a name free today must not
		// become a collision tomorrow.
		{Name: "TableCookOpen", What: "the cooked header's WHOLE check, shared by every <Name>Open"},
		{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		{Name: "table_cook_read64", What: "the cooked header read BYTEWISE"},
		// and the COOKED FORM's WRITE runtime (docs/SPEC-TABLES.md §7.6), emitted
		// beside the read half in the same guard. Three names: the byte order a
		// cook is WRITTEN in, one store and one buffer copy. A store takes its
		// width as an argument rather than minting a name per width — every call
		// site passes a literal, so the loop folds — because a claimed name costs
		// every schema in every unit and saves nothing here.
		{Name: "TableByteOrder", What: "the byte order a cook is WRITTEN in, as Cook's parameter"},
		{Name: "table_cook_put", What: "one scalar into a cook, in the target's byte order"},
		{Name: "table_cook_bytes", What: "a string or bytes buffer's used prefix into a cook"},
		// and its POINTERED half, in a unit that has a variable-length table: the
		// region being laid out and written, and the one reference store
		{Name: "TableCookRegion", What: "a pointered root's region while a cook lays it out and writes it: the numbering, one offset per node, the extent and the alignment"},
		{Name: "table_cook_ref", What: "a reference slot into a cook: the self-relative delta, or a refusal for a node the numbering did not reach"},
		// the unit's BUILD VERSION (docs/SPEC-TABLES.md §20): the one digest a block
		// carries and BlockOpen compares. It is not a Table* spelling, and it is
		// claimed here because it is a unit-level name the generated block sources
		// define.
		{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
		// THE FIXED FORM's plan cache (docs/SPEC-TABLES.md §3.4): keyed by the
		// layout hash so a compile is paid once per endpoint, not once per record.
		// The rest of the fixed form's names are still owed (reference-fix 8).
		{Name: "TableFixedPlanCache", What: "the plan cache BY HASH, so a compile is paid once per endpoint and never once per record"},
		{Name: "TableFixedPlanCacheSlot", What: "one cached compiled plan: hash, plan pointer, count, record_bytes"},
		{Name: "kTableFixedPlanCacheCapacity", What: "the plan cache's named bound: 64, overflow is a miss"},
		{Name: "TableFixedPlanCacheInit", What: "bind caller-owned slot storage to a plan cache"},
	}
	define(Cpp, append(names, cppRuntimeNames()...)...)
}
