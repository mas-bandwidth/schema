package tablenames

// C is the C table backend (internal/codegen/ctable). It carries the
// widest surface of the six, and for one reason: C has no namespace to
// put a runtime in and no nested scope to hide one in, so every spelling
// the others can scope away is a unit-level name here. Everything a
// consumer never types is spelled schema_<package>_..._ instead and claims
// nothing (internal/codegen/ctable's `sym`); what is left is what a
// consumer reads and writes.
const C Backend = 1 << 5

func init() {
	define(C,
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
		Name{Name: "table_bits_to_float", What: "u32 bits -> float"},
		Name{Name: "table_float_to_bits", What: "float -> u32 bits"},
		Name{Name: "table_bits_to_double", What: "u64 bits -> double"},
		Name{Name: "table_double_to_bits", What: "double -> u64 bits"},
		Name{Name: "TableRef", What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
		Name{Name: "TableArena", What: "the builder's segmented slab arena"},
		Name{Name: "TableWorker", What: "a builder worker's allocation front"},
		Name{Name: "TableBlockAllocator", What: "the caller's alloc/free pair, used once at build time"},
		Name{Name: "table_block_default_alloc", What: "the default allocator's alloc half"},
		Name{Name: "table_block_default_free", What: "the default allocator's free half"},
		Name{Name: "TableBlockTriple", What: "one array's (offset_of, count, stride)"},
		Name{Name: "TableBlockRefusal", What: "why Begin refused: the array, its count and its maximum"},
		Name{Name: "TableBlockRows", What: "one array's rows, iterated at the pitch the instance gives"},
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "table_block_byteswap64", What: "the byte-order check's swap"},
		Name{Name: "table_block_read64", What: "the prologue read BYTEWISE"},
		Name{Name: "table_block_align", What: "round an offset up to an alignment"},
		Name{Name: "table_cook_read64", What: "the cooked header read BYTEWISE"},
		// ---- THE C BACKEND's own spellings (internal/codegen/ctable) ----
		//
		// C has no namespace and no nested scope, so a runtime name is a unit-level
		// name whatever it is for. The list is long for that reason and for no
		// other: everything a consumer never types carries the schema_<package>_
		// prefix instead and is absent from here (ctable's `sym`), so what follows
		// is the surface a C consumer actually reads and writes, plus the text
		// form's walk — which lives in <Base>Table.c and would collide there with a
		// declaration of the same name in the unit's own header.

		// the wire reader and writer's operations. C++ spells these as member
		// functions of TableWriter and TableReader, which claim nothing; C has no
		// members, so each is a name.
		Name{Name: "table_writer_make", What: "a writer over a caller's buffer"},
		Name{Name: "table_writer_raw", What: "the writer's byte move"},
		Name{Name: "table_writer_put8", What: "the writer's u8"},
		Name{Name: "table_writer_put16", What: "the writer's little-endian u16"},
		Name{Name: "table_writer_put32", What: "the writer's little-endian u32"},
		Name{Name: "table_writer_put64", What: "the writer's little-endian u64"},
		Name{Name: "table_writer_patch32", What: "the writer's back-patch of a length prefix"},
		Name{Name: "table_reader_make", What: "a reader over a caller's buffer"},
		Name{Name: "table_reader_has", What: "the reader's remaining-bytes question"},
		Name{Name: "table_reader_get8", What: "the reader's u8"},
		Name{Name: "table_reader_get16", What: "the reader's little-endian u16"},
		Name{Name: "table_reader_get32", What: "the reader's little-endian u32"},
		Name{Name: "table_reader_get64", What: "the reader's little-endian u64"},
		Name{Name: "table_reader_skip", What: "skip one payload by kind — the tolerant read's unknown-field path"},
		// the descriptors' VOCABULARY, as data. C++ spells an enum's names and wire
		// ids as captureless lambdas in the field descriptor; C has none, and a
		// named function per enum would claim a name per enum, so the same facts
		// ride as a table indexed the way enum_max bounds (§8.1).
		Name{Name: "TableVariantInfo", What: "one vocabulary entry: a variant's name and its table-wire id"},
		// the ENUM-KEYED array (docs/SPEC-TABLES.md §2.4). C's storage IS the array,
		// so there is no TableKeyed to emit; what C++ puts in operator[] — the
		// left shift and the None refusal — lives in these two.
		Name{Name: "table_keyed_slot", What: "the storage index a key names, refusing None in every build"},
		// the VARIABLE-LENGTH runtime's C spellings (docs/SPEC-TABLES.md §6). The
		// arena and the worker are C++'s too; everything a member function or a
		// template did there is a name here.
		Name{Name: "table_ref_null", What: "is this reference slot null (§6.3)"},
		Name{Name: "table_align_up", What: "round a u32 arena offset up to the node alignment"},
		Name{Name: "table_align_up64", What: "round an i64 region offset up to the node alignment"},
		Name{Name: "table_arena_init", What: "start one arena"},
		Name{Name: "table_arena_shutdown", What: "release one arena's segments"},
		Name{Name: "table_arena_at", What: "resolve an arena offset — one L1 load plus an add"},
		Name{Name: "table_arena_grab_slab", What: "hand one worker its next private slab"},
		Name{Name: "table_worker_make", What: "one thread's allocation front"},
		Name{Name: "table_worker_bump", What: "reserve one node's bytes in a worker's slab, untyped"},
		Name{Name: "TableCtx", What: "which encoding a walk is reading: an arena's offsets, or a region's self-relative deltas"},
		Name{Name: "TableRegionSink", What: "bump-allocation into the caller's exact region"},
		Name{Name: "TableSink", What: "where a node comes from — a region sink or a worker; C's form of the reference's Sink template parameter"},
		// the one ATOMIC the arena needs, and its spelling is FEATURE TESTED
		// because C99 has none (§6.4). The names are claimed whatever the
		// compiler picks, so a schema cannot become illegal by changing compilers.
		Name{Name: "TableAtomicU32", What: "the arena cursor's atomic u32"},
		Name{Name: "TableAtomicPtr", What: "an arena segment's atomic pointer"},
		Name{Name: "table_atomic_load32", What: "an acquire load of the cursor"},
		Name{Name: "table_atomic_store32", What: "a relaxed store of the cursor"},
		Name{Name: "table_atomic_load_ptr", What: "an acquire load of a segment"},
		Name{Name: "table_atomic_store_ptr", What: "a relaxed store of a segment"},
		Name{Name: "table_arena_cas32", What: "the slab handout's compare-exchange"},
		Name{Name: "table_arena_cas_ptr", What: "the segment publication's compare-exchange"},
		// the BLOCK form's C spellings (§19)
		Name{Name: "table_block_row_at", What: "one row of an array, at the pitch the instance gives"},
		// THE TUNING CONSTANTS, which C spells as #define and the other two as
		// typed constants inside their own scope. A macro is not scoped by
		// anything, so each is a unit-level name here (§6, §7.1, §16).
		Name{Name: "kTableAlign", What: "every arena node starts eight-aligned"},
		Name{Name: "kTableAllocFailed", What: "the arena's refusal, never a silent smaller slab"},
		Name{Name: "kTableMaxDepth", What: "the pointer-chain depth cap (§3.1)"},
		Name{Name: "kTableMaxSegments", What: "the arena's segment table"},
		Name{Name: "kTableSegmentBits", What: "the arena's segment size, as a shift"},
		Name{Name: "kTableSegmentMask", What: "the offset inside a segment"},
		Name{Name: "kTableSegmentSize", What: "the arena's segment size"},
		Name{Name: "kTableSlabBytes", What: "one worker's slab: one atomic per slab, none per node"},
		Name{Name: "table_cook_header_bytes", What: "the cooked header's 64 bytes (§7.1)"},
		Name{Name: "kTableJsonMaxDepth", What: "the text form's nesting cap"},
		Name{Name: "kTableJsonMaxKey", What: "the longest key that can name a field"},
		Name{Name: "kTableJsonMaxNumber", What: "the longest numeric token the walk converts"},
		// THE TEXT FORM'S WALK (docs/SPEC-TABLES.md §16), which C# scopes inside a
		// nested class and C++ hides in a namespace. C has neither, and the walk
		// lives in <Base>Table.c beside the unit's own header, so every one of its
		// spellings could collide with a declaration in that unit. They are one
		// family with one job, and the registry lists them rather than filtering
		// them: a scan that has to recognise a prefix is a scan that goes blind the
		// day a name leaves the family.
		Name{Name: "table_json_base64_alphabet", What: "the text form's walk"},
		Name{Name: "table_json_count", What: "the text form's walk"},
		Name{Name: "table_json_decimal_point", What: "the text form's walk"},
		Name{Name: "table_json_element_shape", What: "the text form's walk"},
		Name{Name: "table_json_encode_utf8", What: "the text form's walk"},
		Name{Name: "table_json_finite", What: "the text form's walk"},
		Name{Name: "table_json_get_raw", What: "the text form's walk"},
		Name{Name: "table_json_get_signed", What: "the text form's walk"},
		Name{Name: "table_json_guard_holds", What: "the text form's walk"},
		Name{Name: "table_json_hex4", What: "the text form's walk"},
		Name{Name: "TableJsonIn", What: "the text form's walk"},
		Name{Name: "table_json_is_bytes", What: "the text form's walk"},
		Name{Name: "table_json_is_enum", What: "the text form's walk"},
		Name{Name: "table_json_is_flags", What: "the text form's walk"},
		Name{Name: "table_json_is_keyed", What: "the text form's walk"},
		Name{Name: "table_json_key_id", What: "the text form's walk"},
		Name{Name: "table_json_key_name", What: "the text form's walk"},
		Name{Name: "table_json_keyed_slot_key", What: "the text form's walk"},
		Name{Name: "table_json_keyed_slot_valid", What: "the text form's walk"},
		Name{Name: "table_json_line", What: "the text form's walk"},
		Name{Name: "table_json_literal", What: "the text form's walk"},
		Name{Name: "TableJsonOut", What: "the text form's walk"},
		Name{Name: "table_json_peek", What: "the text form's walk"},
		Name{Name: "table_json_put", What: "the text form's walk"},
		Name{Name: "table_json_raw", What: "the text form's walk"},
		Name{Name: "table_json_read", What: "the text form's walk"},
		Name{Name: "table_json_read_field", What: "the text form's walk"},
		Name{Name: "table_json_read_scalar", What: "the text form's walk"},
		Name{Name: "table_json_read_table", What: "the text form's walk"},
		Name{Name: "table_json_scan_number", What: "the text form's walk"},
		Name{Name: "table_json_scan_string", What: "the text form's walk"},
		Name{Name: "table_json_set_count", What: "the text form's walk"},
		Name{Name: "table_json_set_raw", What: "the text form's walk"},
		Name{Name: "table_json_shape", What: "the text form's walk"},
		Name{Name: "table_json_skip_container", What: "the text form's walk"},
		Name{Name: "table_json_skip_value", What: "the text form's walk"},
		Name{Name: "table_json_space", What: "the text form's walk"},
		Name{Name: "table_json_text", What: "the text form's walk"},
		Name{Name: "table_json_token_double", What: "the text form's walk"},
		Name{Name: "table_json_token_integer", What: "the text form's walk"},
		Name{Name: "table_json_utf8", What: "the text form's walk"},
		Name{Name: "table_json_value_shape", What: "the text form's walk"},
		Name{Name: "table_json_variant_id", What: "the text form's walk"},
		Name{Name: "table_json_variant_name", What: "the text form's walk"},
		Name{Name: "table_json_walk_number", What: "the text form's walk"},
		Name{Name: "table_json_write", What: "the text form's walk"},
		Name{Name: "table_json_write_base64", What: "the text form's walk"},
		Name{Name: "table_json_write_field", What: "the text form's walk"},
		Name{Name: "table_json_write_float", What: "the text form's walk"},
		Name{Name: "table_json_write_scalar", What: "the text form's walk"},
		Name{Name: "table_json_write_signed", What: "the text form's walk"},
		Name{Name: "table_json_write_string", What: "the text form's walk"},
		Name{Name: "table_json_write_unsigned", What: "the text form's walk"},
		Name{Name: "table_json_write_value", What: "the text form's walk"},
		// The C spellings of names the other backends carry in PascalCase. They
		// are separate entries because they are separate SPELLINGS: C spells a
		// function and a file-scope constant snake_case, which is the convention
		// its packet half already uses, so the same runtime value is one identifier
		// in Rust and C++ and a different identifier here. Both are claimed for
		// every target, because the claim is front-end law and a unit legal under
		// one backend must be legal under all of them.
		Name{Name: "table_block_default_allocator", What: "the malloc/free pair, for a caller with none of its own"},
		Name{Name: "table_block_magic", What: "the block prologue's magic, and the byte-order check with it"},
		Name{Name: "table_block_byte_order", What: "this build's byte order, as the prologue carries it"},
		Name{Name: "table_cook_open", What: "the cooked header's WHOLE check, shared by every <Name>Open"},
		Name{Name: "table_cook_magic", What: "the cooked header's magic, and the byte-order check with it"},
		Name{Name: "table_cook_byte_order", What: "this build's byte order, as a cooked header records it"},
		Name{Name: "table_cook_max_align", What: "the greatest region alignment a cooked header may name"},
	)
}
