// Package tablenames is THE registry of unit-level names the generated
// TABLE-wire runtimes define (docs/SPEC-TABLES.md §11).
//
// It exists because the same list is needed in two places that must never
// disagree: the CHECKER claims these names when a unit declares a table, so no
// legal schema can reach a generated source that does not compile; and the
// EMITTERS define them. A name defined by one and unclaimed by the other is
// precisely the defect §11 promises cannot happen — a legal schema whose
// generated code a compiler rejects — so the two read one list instead of
// keeping two.
//
// The list is FRONT-END LAW, not one target's inventory: every backend's
// spelling is claimed for ALL of them. A unit legal under one target and
// illegal under another is the trap the claim exists to prevent, so C++'s
// snake_case float helpers and C#'s CamelCase ones are claimed together, and
// so are the variable-length runtime's names — claimed whenever a unit
// declares a table, not only when one carries pointers, so adding a pointer to
// an existing table never turns a legal declaration elsewhere into a collision.
//
// ADDING A RUNTIME NAME. Register it here, with the backends that define it.
// The emitters are not generated from this list — a runtime is prose-laden
// source that reads better written out — so the registry is held honest from
// the other side instead: compiler's TestTableRuntimeNamesAreClaimed scans the
// emitted output for every Table* identifier and requires the set to equal
// this registry's, in both directions. An emitted name nobody registered fails
// there; so does a registered name nothing emits any more.
package tablenames

import "sort"

// Backend is a bit per table backend, so one entry can say which of them
// define the name.
type Backend uint16

const (
	// Cpp is the C++ table backend (internal/codegen/cpptable).
	Cpp Backend = 1 << iota
	// Cs is the C# table backend (internal/codegen/cstable).
	Cs
	// Rust is the Rust table backend (internal/codegen/rusttable).
	Rust
	// Go is the Go table backend (internal/codegen/gotable).
	Go
	// Java is the Java table backend (internal/codegen/javatable).
	Java
	// C is the C table backend (internal/codegen/ctable). It carries the
	// widest surface of the six, and for one reason: C has no namespace to
	// put a runtime in and no nested scope to hide one in, so every spelling
	// the others can scope away is a unit-level name here. Everything a
	// consumer never types is spelled schema_<package>_..._ instead and claims
	// nothing (internal/codegen/ctable's `sym`); what is left is what a
	// consumer reads and writes.
	C
	// Elixir is the Elixir table backend (internal/codegen/elixirtable).
	Elixir
	// Js is the JavaScript table backend (internal/codegen/jstable). It is the
	// READING TIER: no struct layout, so it reads both accelerators and
	// produces neither.
	Js
	// Dart is the Dart table backend (internal/codegen/darttable).
	//
	// DART PUTS THE TABLE VERBS ON THE VALUE — methods on a generated table
	// class, extension methods on a `type`'s packet-emitted class — so the
	// per-member surface claims nothing at library scope and this registry
	// holds only the RUNTIME's own names. Where a runtime name is a free
	// function Dart spells it lowerCamelCase and the registry holds the
	// PascalCase form: the two are a bijection (the packet emitter's
	// dartName), so one registration covers both.
	Dart
)

// Name is one spelling the generated table runtimes carry, and the backends
// whose emitted text carries it.
type Name struct {
	Name string
	By   Backend
	What string // what it is, for a reader of this file

	// Scoped marks a spelling NO DECLARATION CAN COLLIDE WITH, so it claims
	// nothing: a member reached through its owner, or — in Rust — an item
	// private to the shared runtime module, which the crate root never sees.
	// Registered anyway, so the scan that holds this list honest has an answer
	// for every name it finds rather than a filter nobody can see.
	Scoped bool

	// RustConst marks a name the Rust backend spells as a CRATE-SCOPE
	// CONSTANT, whose emitted identifier is therefore ir.RustConstName of the
	// name rather than the name itself.
	//
	// It exists because Rust's constant spelling is MANY-TO-ONE: a schema
	// const named TableCookMagic, TABLE_COOK_MAGIC or table_cook_magic all
	// lower to one crate-scope TABLE_COOK_MAGIC, and claiming the registered
	// spelling alone leaves the other two legal. What they generate is a pair
	// of ambiguous glob re-exports — the generated crate builds with a
	// warning, the user's own constant is silently shadowed at the crate root,
	// and any CONSUMER that names the symbol fails to compile under
	// ambiguous_glob_imports, which is deny-by-default and
	// future-incompatible. internal/check claims these in the MAPPED space,
	// beside the same claim it already makes between two user declarations
	// ("both generate the symbol MAX_HEALTH").
	RustConst bool
}

// registry is the whole list. Sorted by nothing in particular — callers that
// need an order sort for themselves, because map and slice order must never
// decide what a diagnostic says.
var registry = []Name{
	// the shared surface both backends define per unit
	{Name: "TableReport", By: Cpp | Cs | Dart | Rust | Go | C | Java | Js, What: "the read report — the permissive contract's ledger"},
	{Name: "TableWriter", By: Cpp | Cs | Dart | Rust | Go | C | Java | Js, What: "the wire writer over the caller's buffer"},
	{Name: "TableReader", By: Cpp | Cs | Dart | Rust | Go | C | Java | Js, What: "the wire reader over the caller's buffer"},
	{Name: "TableTypeInfo", By: Cpp | Cs | Dart | Rust | Go | C | Java, What: "a table's reflection descriptor"},
	{Name: "TableFieldInfo", By: Cpp | Cs | Dart | Rust | Go | C | Java, What: "a field's reflection descriptor"},
	// a UNION field's shape (docs/SPEC-TABLES.md §8.1): the tag, and each arm's
	// payload by its own descriptor. Every backend defines both, and every one
	// puts them at unit level beside the two descriptors above — a union
	// field's column has to name a type, and a nested one would be reached
	// through a descriptor a walk holds by value.
	{Name: "TableUnionInfo", By: Cpp | Cs | Dart | Rust | Go | C | Java, What: "a union field's tag and its arms"},
	{Name: "TableUnionArmInfo", By: Cpp | Cs | Rust | Go | C | Java, What: "one union arm's payload and descriptor"},
	{Name: "TableEnumId", By: Cpp | Cs | Go | Java, What: "an enum value -> its table-wire variant id"},
	{Name: "TableEnumValue", By: Cpp | Cs | Go | Java, What: "a table-wire variant id -> its enum value"},

	// the ENUM-KEYED array's storage type (docs/SPEC-TABLES.md §2.4). C++ spells it
	// a class template and C# a generic class; both put it at unit level, and
	// both emit it ONLY into a unit that declares a keyed array — but the
	// claim is unconditional on a unit declaring a table, for the same reason
	// the variable-length names are: adding a keyed array to an existing table
	// must not turn a legal declaration elsewhere into a collision.
	{Name: "TableKeyed", By: Cpp | Cs | Dart | Go | Rust | Java | Js, What: "an enum-keyed array's slot storage"},

	// SCOPED: a field descriptor's nested-table column. C++ spells it `table`
	// and C# `Table`, and either way it is reached through its owner, so a
	// schema is free to declare the name.
	{Name: "Table", By: Cs | Go, What: "TableFieldInfo's nested-table column", Scoped: true},

	// C++'s float <-> IEEE-754 bit pattern helpers
	{Name: "table_bits_to_float", By: Cpp | C, What: "u32 bits -> float"},
	{Name: "table_float_to_bits", By: Cpp | C, What: "float -> u32 bits"},
	{Name: "table_bits_to_double", By: Cpp | C, What: "u64 bits -> double"},
	{Name: "table_double_to_bits", By: Cpp | C, What: "double -> u64 bits"},

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
	{Name: "TableReset", By: Cpp | Cs, What: "restore a value's declared defaults in place — C#'s reset itself, and C++'s ADL hook onto <Name>Reset for the arena. JAVA DOES NOT DEFINE IT: it spells the prefill <name>Reset, name-first as §11 already claims and lowerCamel as Java requires, so no family name is minted there. NEITHER DOES JAVASCRIPT, for the same reason: it has no overloading and spells the name-first <Name>Reset §11 already claims"},
	// SCOPED: the TEXT FORM's generic walk (docs/SPEC-TABLES.md §16), a nested
	// static class of Schema. Everything the walk spells — its reader, its
	// writer sink, its scanners — is a member of it, so one registration
	// covers the whole surface and the walk claims not one name at unit
	// scope. C++ spells the same functions as a TableJson* family at
	// namespace scope inside its own package guard.
	{Name: "TableJson", By: Cs | Dart | Java | Js, What: "the text form's generic walk. C# spells it a nested class of Schema, which claims nothing; Java puts it at package scope, Dart at library scope and JavaScript at module scope — an ES module is one scope, with no nested class to hide a walk in — so the claim is the UNION and the name is claimed"},
	{Name: "TableBitsToFloat", By: Cs | Dart | Js, What: "u32 bits -> float"},
	{Name: "TableFloatToBits", By: Cs | Dart | Js, What: "float -> u32 bits"},
	{Name: "TableBitsToDouble", By: Cs | Dart | Js, What: "u64 bits -> double"},
	{Name: "TableDoubleToBits", By: Cs | Dart | Js, What: "double -> u64 bits"},
	// JavaScript's four go through one shared eight-byte DataView, which is a
	// module-level binding like any other and takes a name in this family for
	// that reason: a SCREAMING_SNAKE spelling would have been a module-scope
	// name outside every claim the front end makes.
	{Name: "TableBitsScratch", By: Js, What: "the JavaScript bit helpers' shared eight-byte DataView"},

	// THE DART READING TIER's own runtime (internal/codegen/darttable). Dart's
	// privacy is per LIBRARY and a generated file is one, so a runtime shared
	// across a unit's files is PUBLIC and every spelling here is claimed. The
	// backend spells no private library-scope name at all — a schema may
	// declare an identifier beginning with an underscore, so a private
	// top-level name would be an unclaimed collision — which is why the
	// conversion scratch is a class of members rather than four constants.
	//
	// What is NOT here is the per-member surface: Dart puts the table verbs on
	// the VALUE (methods on a generated table class, extension methods on a
	// `type`'s packet-emitted class), so <Name>Measure and its family are
	// claimed by the name-first suffix set like every other backend's and
	// nothing per member reaches library scope.
	{Name: "TableEnumVocab", By: Dart, What: "one enum's table-wire vocabulary; the per-enum instances are its static members, so they claim nothing"},
	{Name: "TableScratch", By: Dart, What: "the 8-byte float conversion scratch, as overlaid typed-data views"},
	{Name: "TableNarrowFloat", By: Dart, What: "narrow a double to its float32 value — what gives a float32 elision C's own float semantics"},
	{Name: "TableUnsignedLess", By: Dart, What: "unsigned less-than over the bit-transparent int a u64 rides in"},

	// the VARIABLE-LENGTH runtime (docs/SPEC-TABLES.md §6): the arena, the region
	// and the reference slot. C++ only today — the C# backend carries the
	// fixed class and refuses a pointered unit by name (§11).
	// TableRef carries a SECOND, unrelated meaning in C#: a field descriptor's
	// lazy factory for the nested table's descriptor, which is scoped. The
	// C++ meaning is unit-level, so the name is claimed whatever the target —
	// the claim is the union, never the intersection.
	{Name: "TableRef", By: Cpp | Cs | C, What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
	{Name: "TableSlot", By: Cpp, What: "an arena slot"},
	{Name: "TableArena", By: Cpp | C, What: "the builder's segmented slab arena"},
	{Name: "TableSlab", By: Cpp, What: "one worker's privately-owned slab of it"},
	{Name: "TableWorker", By: Cpp | C, What: "a builder worker's allocation front"},
	{Name: "TableBuilder", By: Cpp, What: "the mutable life's base"},
	{Name: "TableRegion", By: Cpp, What: "the locked, packed region"},
	// Lock's identity map (docs/SPEC-TABLES.md §6.2): one entry per reachable
	// node, so a shared node is packed once and a reference to a node whose
	// descent is still open is a cycle. Emitted into a pointered unit's header
	// only, and claimed on the same terms as everything above — a name free
	// today must not become a collision the day a table gains a pointer.
	{Name: "TablePackMap", By: Cpp, What: "the pack walk's identity map"},
	{Name: "TablePackEntry", By: Cpp, What: "one node's entry in it: where it landed, and whether its descent is open"},

	// the FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1): the numbering a save
	// derives, the directory a load fills, and the record scan that is the
	// whole of load's bound. Emitted into a pointered unit's header only, and
	// claimed whenever a unit declares a table, on the same terms as the
	// variable-length names above.
	{Name: "TableNumbering", By: Cpp, What: "one save's numbering: the identity map and the nodes in index order"},
	{Name: "TableNodeEntry", By: Cpp, What: "one numbered node, with the thunks that reach its codec"},
	{Name: "TableNodeMeasure", By: Cpp, What: "the numbering's bridge to a member's MeasureBody, by argument-dependent lookup"},
	{Name: "TableNodeSave", By: Cpp, What: "the same bridge to a member's SaveBody"},
	{Name: "TableNodeMeasureThunk", By: Cpp, What: "the instantiation a numbering stores for a node's measure"},
	{Name: "TableNodeSaveThunk", By: Cpp, What: "the instantiation a numbering stores for a node's save"},
	{Name: "TableNodeDirEntry", By: Cpp, What: "one entry of a region's node directory: an offset and a type id"},
	{Name: "TableNodeMap", By: Cpp, What: "the resident numbering a pointer slot resolves through"},
	{Name: "TableNodeResolve", By: Cpp, What: "place one node index in a pointer slot, or report why it did not"},
	{Name: "TableNodeScan", By: Cpp, What: "the record scan over a root body's node-table fields"},

	// the BLOCK FORM's runtime (docs/SPEC-TABLES.md §19), emitted into
	// <Base>Block.h / <Base>Block.cs and into no Table source at all. Claimed
	// whenever a unit declares a table, on the same terms as everything above:
	// nothing declares the block form, every fixed table has one, and a table
	// gains and loses it as its closure gains and loses a pointer.
	{Name: "TableBlockAllocator", By: Cpp | C, What: "the caller's alloc/free pair, used once at build time"},
	{Name: "TableBlockDefaultAllocator", By: Cpp, What: "the malloc/free pair, for a caller with none of its own"},
	{Name: "table_block_default_alloc", By: Cpp | C, What: "the default allocator's alloc half"},
	{Name: "table_block_default_free", By: Cpp | C, What: "the default allocator's free half"},
	{Name: "TableBlockTriple", By: Cpp | Cs | Rust | Go | C, What: "one array's (offset_of, count, stride)"},
	// JAVA's byte-access primitive. C++ reads a record through its type, C#
	// through a pointer cast and Rust through a transmute; Java has none of
	// those, so every multi-byte read of a block or a cook goes through one
	// package-level class of explicit little-endian readers — which is also
	// what settles the byte order of both accelerators without asking the host.
	{Name: "TableBytes", By: Java, What: "explicit little-endian reads out of a byte[] (Java's block and cook read through it)"},
	{Name: "TableBlockRefusal", By: Cpp | C, What: "why Begin refused: the array, its count and its maximum"},
	{Name: "TableBlockRows", By: Cpp | Cs | Go | C | Java, What: "one array's rows, iterated at the pitch the instance gives"},
	{Name: "TableBlockSpan", By: Cpp, What: "one array's rows as a contiguous view (C# uses ReadOnlySpan)"},
	{Name: "TableBlockFieldInfo", By: Cpp | Cs | Dart | Rust | Go | C | Java, What: "a block field's reflection descriptor"},
	{Name: "TableBlockInfo", By: Cpp | Cs | Dart | Rust | Go | C | Java, What: "a block's reflection descriptor"},
	{Name: "TableBlockMagic", By: Cpp | Cs | Dart | Rust | Go | Js, What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
	{Name: "TableBlockLayout", By: Cs | Java | Js, What: "the layout contract's check, run once. C# spells it a nested class of Schema; Java puts it at package scope and JavaScript at module scope, so the claim is the UNION"},
	{Name: "TableBlockRead64", By: Cs | Dart, What: "the prologue read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
	{Name: "TableBlockByteOrder", By: Cpp | Cs | Dart | Go | Rust | Js, What: "this build's byte order, as the prologue carries it", RustConst: true},
	{Name: "table_block_byteswap64", By: Cpp | C, What: "the byte-order check's swap"},
	{Name: "table_block_read64", By: Cpp | C, What: "the prologue read BYTEWISE"},
	{Name: "table_block_align", By: Cpp | C, What: "round an offset up to an alignment"},

	// the COOKED FORM's read runtime (docs/SPEC-TABLES.md §7), emitted into
	// <Base>Table.h of a unit that declares a variable-length table — a cook's
	// root is one — and into no value-only unit's header at all. Claimed
	// whenever a unit declares a table, on the same terms as the
	// variable-length names above: a table gains and loses its cook reader as
	// its closure gains and loses a pointer, and a name free today must not
	// become a collision tomorrow.
	{Name: "TableCookOpen", By: Cpp, What: "the cooked header's WHOLE check, shared by every <Name>Open"},
	{Name: "TableCookMagic", By: Cpp | Cs | Dart | Go | Rust | Js, What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
	{Name: "TableCookByteOrder", By: Cpp | Cs | Dart | Go | Rust | Js, What: "this build's byte order, as a cooked header records it", RustConst: true},
	{Name: "TableCookMaxAlign", By: Cpp | Cs | Dart | Go | Rust | Js, What: "the greatest region alignment a cooked header may name", RustConst: true},
	{Name: "table_cook_read64", By: Cpp | C, What: "the cooked header read BYTEWISE"},

	// and the COOKED FORM's WRITE runtime (docs/SPEC-TABLES.md §7.6), emitted
	// beside the read half in the same guard. Three names: the byte order a
	// cook is WRITTEN in, one store and one buffer copy. A store takes its
	// width as an argument rather than minting a name per width — every call
	// site passes a literal, so the loop folds — because a claimed name costs
	// every schema in every unit and saves nothing here.
	{Name: "TableByteOrder", By: Cpp, What: "the byte order a cook is WRITTEN in, as Cook's parameter"},
	{Name: "table_cook_put", By: Cpp, What: "one scalar into a cook, in the target's byte order"},
	{Name: "table_cook_bytes", By: Cpp, What: "a string or bytes buffer's used prefix into a cook"},
	// and its POINTERED half, in a unit that has a variable-length table: the
	// region being laid out and written, and the one reference store
	{Name: "TableCookRegion", By: Cpp, What: "a pointered root's region while a cook lays it out and writes it: the numbering, one offset per node, the extent and the alignment"},
	{Name: "table_cook_ref", By: Cpp, What: "a reference slot into a cook: the self-relative delta, or a refusal for a node the numbering did not reach"},

	// ---- the RUST backend's own spellings (internal/codegen/rusttable) ----
	//
	// Rust has no overloading, so the enum identity pair C++ and C# spell as an
	// overload set is a TRAIT the generated code implements once per enum: one
	// unit-level name rather than one per declaration.
	{Name: "TableEnum", By: Rust, What: "an enum's TABLE-wire identity, as a trait (§5)"},

	// The text form's three bounds, at unit level in Rust because a Rust
	// constant has no class to hide in.
	{Name: "TableJsonMaxDepth", By: Rust, What: "the text walk's nesting cap (§16)", RustConst: true},
	{Name: "TableJsonMaxKey", By: Rust, What: "the longest key that can name a field", RustConst: true},
	{Name: "TableJsonMaxNumber", By: Rust, What: "the longest numeric token the walk converts", RustConst: true},

	// SCOPED, Rust: private to the shared runtime module, which does not
	// import the crate root — so a declaration taking one of these names
	// cannot collide with it, and the name stays the schema author's.
	{Name: "TableJsonIn", By: Rust, What: "the text walk's read cursor", Scoped: true},
	{Name: "TableJsonOut", By: Rust, What: "the text walk's measure-or-write sink", Scoped: true},
	{Name: "TableJsonKey", By: Rust, What: "a scanned object key, kept on the stack", Scoped: true},
	{Name: "TableJsonNumber", By: Rust, What: "a scanned numeric token", Scoped: true},
	{Name: "TableJsonSink", By: Rust, What: "where a scanned string goes, or nowhere", Scoped: true},
	{Name: "TableJsonBase64", By: Rust, What: "the base64 alphabet a bytes(N) rides under", Scoped: true},
	{Name: "TableJsonDigits", By: Rust, What: "the float writer's stack sink, so the text form allocates nothing", Scoped: true},

	// SCOPED, Rust: the runtime's snake_case CRATE ITEMS. A schema declaration
	// produces a type (its own spelling) or a SCREAMING_SNAKE constant, and
	// never a bare snake_case crate item — so none of these can be collided
	// with, and claiming forty-odd of them would take forty-odd names from
	// every schema for nothing. They are registered because the scan that
	// holds this list honest now SEES them: its first version was C#'s regex
	// verbatim and blind to lowercase, which is the defect class this block
	// closes. A helper somebody adds is accounted for here rather than
	// slipping in under a regex that could not see it.
	{Name: "TableId", By: Rust, What: "the TableEnum trait's value -> wire id half", Scoped: true},
	{Name: "TableValue", By: Rust, What: "the TableEnum trait's wire id -> value half", Scoped: true},
	// NOT scoped, because of ELIXIR: a Rust module named table_runtime is
	// private to the crate and no declaration can reach it, but the Elixir
	// emitter's <Package>.TableRuntime is a MODULE at unit level and a
	// declaration named TableRuntime lowers to exactly that spelling. The
	// claim is the UNION over the backends, never the intersection, so the
	// name is claimed for all of them.
	{Name: "TableRuntime", By: Rust | Elixir, What: "the unit's shared table runtime — a Rust crate module, an Elixir unit-level module"},
	{Name: "TableRelocatable", By: Rust, What: "the relocatability assert's const fn", Scoped: true},
	{Name: "TableBlockByteswap64", By: Rust, What: "the block magic's byte-order swap", Scoped: true},
	{Name: "TableJsonRead", By: Rust, What: "the text form's read entry point", Scoped: true},
	{Name: "TableJsonWrite", By: Rust, What: "the text form's write entry point", Scoped: true},
	{Name: "TableJsonCount", By: Rust, What: "a counted field's companion, read", Scoped: true},
	{Name: "TableJsonSetCount", By: Rust, What: "a counted field's companion, written", Scoped: true},
	{Name: "TableJsonGetRaw", By: Rust, What: "a slot's raw bits at a width", Scoped: true},
	{Name: "TableJsonSetRaw", By: Rust, What: "a slot's raw bits, written", Scoped: true},
	{Name: "TableJsonGetSigned", By: Rust, What: "a slot's sign-extended value", Scoped: true},
	{Name: "TableJsonFinite", By: Rust, What: "not a NaN, not an infinity", Scoped: true},
	{Name: "TableJsonNamed", By: Rust, What: "a vocabulary entry the descriptor could spell", Scoped: true},
	{Name: "TableJsonFormatG", By: Rust, What: "C's percent-star-g, digit for digit", Scoped: true},
	{Name: "TableJsonShape", By: Rust, What: "what a field's kind expects in the text", Scoped: true},
	{Name: "TableJsonElementShape", By: Rust, What: "the same classifier one level down", Scoped: true},
	{Name: "TableJsonIsBytes", By: Rust, What: "a bytes(N), which rides as base64", Scoped: true},
	{Name: "TableJsonIsEnum", By: Rust, What: "an enum field", Scoped: true},
	{Name: "TableJsonIsFlags", By: Rust, What: "a flags field", Scoped: true},
	{Name: "TableJsonIsKeyed", By: Rust, What: "an enum-keyed array", Scoped: true},
	{Name: "TableJsonKeyedSlotKey", By: Rust, What: "the key a storage slot holds", Scoped: true},
	{Name: "TableJsonKeyedSlotValid", By: Rust, What: "a slot whose key names a variant", Scoped: true},
	{Name: "TableJsonGuardHolds", By: Rust, What: "a branch guard, evaluated over the descriptors", Scoped: true},
	{Name: "TableJsonEncodeUtf8", By: Rust, What: "one code point, encoded", Scoped: true},
	{Name: "TableJsonUtf8", By: Rust, What: "one UTF-8 sequence, validated", Scoped: true},
	{Name: "TableJsonWalkNumber", By: Rust, What: "JSON's own number production, consumed", Scoped: true},
	{Name: "TableJsonScanNumber", By: Rust, What: "the same production, kept", Scoped: true},
	{Name: "TableJsonScanString", By: Rust, What: "one JSON string, into a caller slot", Scoped: true},
	{Name: "TableJsonScanKey", By: Rust, What: "one object key, onto the stack", Scoped: true},
	{Name: "TableJsonSkipValue", By: Rust, What: "one value, consumed and dropped", Scoped: true},
	{Name: "TableJsonSkipContainer", By: Rust, What: "one object or array, consumed and dropped", Scoped: true},
	{Name: "TableJsonReadTable", By: Rust, What: "one table object", Scoped: true},
	{Name: "TableJsonReadField", By: Rust, What: "one field", Scoped: true},
	{Name: "TableJsonReadScalar", By: Rust, What: "one scalar", Scoped: true},
	{Name: "TableJsonWriteValue", By: Rust, What: "one instance, every field", Scoped: true},
	{Name: "TableJsonWriteField", By: Rust, What: "one field", Scoped: true},
	{Name: "TableJsonWriteScalar", By: Rust, What: "one scalar", Scoped: true},
	{Name: "TableJsonWriteString", By: Rust, What: "one string, escaped", Scoped: true},
	{Name: "TableJsonWriteStringBytes", By: Rust, What: "one byte run, escaped", Scoped: true},
	{Name: "TableJsonWriteBase64", By: Rust, What: "one bytes(N), base64", Scoped: true},
	{Name: "TableJsonWriteFloat", By: Rust, What: "one float, at its shortest round-tripping precision", Scoped: true},
	{Name: "TableJsonWriteSigned", By: Rust, What: "one signed integer", Scoped: true},
	{Name: "TableJsonWriteUnsigned", By: Rust, What: "one unsigned integer", Scoped: true},

	// the COOK's C# half (docs/SPEC-TABLES.md §7, §19.2's road). C# has no include
	// guard, so the read runtime is emitted ONCE per unit into the cook home's
	// <Base>Cook.cs — and because one assembly sees every file, a declaration
	// taking one of these names anywhere in the unit collides with it.
	{Name: "TableCookHeaderBytes", By: Cs | Go | Js | Rust, What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
	{Name: "TableCookRead64", By: Cs | Dart, What: "the cooked header read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
	{Name: "TableCookLayout", By: Cs | Java | Js, What: "the cook closure's layout contract, run once (§20.3)"},
	{Name: "TableCookInfo", By: Cs | Dart | Go | Rust | Java, What: "a cooked record's reflection descriptor"},
	{Name: "TableCookFieldInfo", By: Cs | Dart | Go | Rust | Java, What: "a cooked field's reflection descriptor"},
	{Name: "TableCookStorage", By: Cs | Dart | Go | Rust | Java, What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
	{Name: "TableCookRef", By: Dart, What: "what a Dart deref answers when it is not an offset: a null (a delta of zero) and a delta that leaves the region"},

	// the eight MEMBERS of that vocabulary, which C# scopes inside its enum and
	// Rust inside its own type, and which GO flattens into the package
	// namespace — exactly as the Go packet emitter flattens a declared enum's
	// variants, which the checker already claims one by one. A port's spelling
	// decides the claim, and this is Go's alone.
	{Name: "TableCookStorageRecord", By: Go, What: "a nested record, or an array of them"},
	{Name: "TableCookStorageReference", By: Go, What: "an eight-byte signed self-relative delta slot (§6.3)"},
	{Name: "TableCookStorageBool", By: Go, What: "a bool slot"},
	{Name: "TableCookStorageSigned", By: Go, What: "a signed integer slot"},
	{Name: "TableCookStorageUnsigned", By: Go, What: "an unsigned integer, an enum ordinal, a bits(N), a flags mask"},
	{Name: "TableCookStorageFloat", By: Go, What: "a float slot"},
	{Name: "TableCookStorageString", By: Go, What: "char[N + 1] with an int32 used length beside it"},
	{Name: "TableCookStorageBytes", By: Go, What: "uint8[N] with an int32 used length beside it"},

	// ---- THE GO PORT'S LOWERCASE FAMILY (docs/SPEC-TABLES.md §11) ----
	//
	// Go is the first backend whose runtime puts UNEXPORTED names at package
	// scope, and unexported is not private: a Go package is one namespace, so
	// `const tableJsonMaxDepth = 5` in a schema generates a redeclaration and
	// the unit does not compile. That is precisely the defect §11 promises
	// cannot happen, so every one of these is claimed on the same terms as the
	// PascalCase surface above.
	//
	// THE THREE SLICES ARE WHY THERE ARE ONLY THREE OF THEM. A descriptor graph
	// could have taken one variable per record — `cookRecordScene`,
	// `blockInfoRenderFrameShipRow` — and each of those is a name DERIVED from
	// a declaration's own spelling, which is a name a declaration can collide
	// with and which this registry has no shape for. One fixed name per graph
	// is one claim, and the emitters index into it.
	{Name: "tableBlockLayoutOffset", By: Go, What: "the block layout contract's offset refusal"},
	{Name: "tableBlockLayoutSize", By: Go, What: "the block layout contract's size refusal"},
	{Name: "tableBlockNativeOrder", By: Go, What: "this machine's byte order, read once at package initialisation"},
	{Name: "tableBlockRecords", By: Go, What: "the unit's whole block descriptor graph, one slice"},
	{Name: "tableCookLayoutOffset", By: Go, What: "the cook layout contract's offset refusal"},
	{Name: "tableCookLayoutSize", By: Go, What: "the cook layout contract's size refusal"},
	{Name: "tableCookNativeOrder", By: Go, What: "this machine's byte order, read once at package initialisation"},
	{Name: "tableCookRecords", By: Go, What: "the unit's whole cooked-record descriptor graph, one slice"},
	{Name: "tableJsonBase64Alphabet", By: Go, What: "the base64 alphabet a `bytes` field rides under"},
	{Name: "tableJsonBytes", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonCount", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonElementShape", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonEncodeUtf8", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonFinite", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonGetRaw", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonGetSigned", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonGuardHolds", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonIn", By: Go, What: "the text reader's cursor"},
	{Name: "tableJsonIsBytes", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonIsEnum", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonIsFlags", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonIsKeyed", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonKeyedSlotKey", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonKeyedSlotValid", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonMaxDepth", By: Go, What: "the walk's nesting cap"},
	{Name: "tableJsonMaxKey", By: Go, What: "the longest key the walk will place"},
	{Name: "tableJsonMaxNumber", By: Go, What: "the longest numeric token the walk will convert"},
	{Name: "tableJsonNameIs", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonNamed", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonOut", By: Go, What: "the text writer's sink"},
	{Name: "tableJsonRead", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonReadField", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonReadScalar", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonReadTable", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonSetCount", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonSetRaw", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonShape", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonText", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonTokenDouble", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonTokenInteger", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonUtf8", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWrite", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteBase64", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteField", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteFloat", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteScalar", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteSigned", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteString", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteUnsigned", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableJsonWriteValue", By: Go, What: "one step of the text form's generic walk (§16)"},
	{Name: "tableUnionArms", By: Go, What: "the unit's union-field shapes, one slice"},

	// ---- the ELIXIR backend's own unit-level MODULES
	// (internal/codegen/elixirtable) ----
	//
	// An Elixir declaration lowers to a module under the unit's own namespace,
	// so every module the emitter defines at that level is a name a
	// declaration could collide with — which is why these are claimed and why
	// the Elixir scan looks for MODULE SEGMENTS rather than for a Table*
	// prefix. The two accelerators carry their own runtimes because a VARIABLE
	// unit gets no table runtime at all (§11) and still has both of them.
	{Name: "BlockRuntime", By: Elixir, What: "the BLOCK form's shared runtime module (docs/SPEC-TABLES.md §19)"},
	{Name: "CookRuntime", By: Elixir, What: "the COOKED form's shared runtime module (docs/SPEC-TABLES.md §7)"},

	// the unit's BUILD VERSION (docs/SPEC-TABLES.md §20): the one digest a block
	// carries and BlockOpen compares. It is not a Table* spelling, and it is
	// claimed here because it is a unit-level name the generated block sources
	// define.
	{Name: "BuildVersion", By: Cpp | Rust | Go | Java | Elixir | Js, What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	{Name: "TableBuildVersion", By: Dart, What: "Dart's spelling of the unit's build version, at library scope in the block runtime home (the cook's, in a unit with no block form)"},

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
	{Name: "table_writer_make", By: C, What: "a writer over a caller's buffer"},
	{Name: "table_writer_raw", By: C, What: "the writer's byte move"},
	{Name: "table_writer_put8", By: C, What: "the writer's u8"},
	{Name: "table_writer_put16", By: C, What: "the writer's little-endian u16"},
	{Name: "table_writer_put32", By: C, What: "the writer's little-endian u32"},
	{Name: "table_writer_put64", By: C, What: "the writer's little-endian u64"},
	{Name: "table_writer_patch32", By: C, What: "the writer's back-patch of a length prefix"},
	{Name: "table_reader_make", By: C, What: "a reader over a caller's buffer"},
	{Name: "table_reader_has", By: C, What: "the reader's remaining-bytes question"},
	{Name: "table_reader_get8", By: C, What: "the reader's u8"},
	{Name: "table_reader_get16", By: C, What: "the reader's little-endian u16"},
	{Name: "table_reader_get32", By: C, What: "the reader's little-endian u32"},
	{Name: "table_reader_get64", By: C, What: "the reader's little-endian u64"},
	{Name: "table_reader_skip", By: C, What: "skip one payload by kind — the tolerant read's unknown-field path"},

	// the descriptors' VOCABULARY, as data. C++ spells an enum's names and wire
	// ids as captureless lambdas in the field descriptor; C has none, and a
	// named function per enum would claim a name per enum, so the same facts
	// ride as a table indexed the way enum_max bounds (§8.1).
	{Name: "TableVariantInfo", By: C, What: "one vocabulary entry: a variant's name and its table-wire id"},

	// the ENUM-KEYED array (docs/SPEC-TABLES.md §2.4). C's storage IS the array,
	// so there is no TableKeyed to emit; what C++ puts in operator[] — the
	// left shift and the None refusal — lives in these two.
	{Name: "table_keyed_slot", By: C, What: "the storage index a key names, refusing None in every build"},

	// the VARIABLE-LENGTH runtime's C spellings (docs/SPEC-TABLES.md §6). The
	// arena and the worker are C++'s too; everything a member function or a
	// template did there is a name here.
	{Name: "table_ref_null", By: C, What: "is this reference slot null (§6.3)"},
	{Name: "table_align_up", By: C, What: "round a u32 arena offset up to the node alignment"},
	{Name: "table_align_up64", By: C, What: "round an i64 region offset up to the node alignment"},
	{Name: "table_arena_init", By: C, What: "start one arena"},
	{Name: "table_arena_shutdown", By: C, What: "release one arena's segments"},
	{Name: "table_arena_at", By: C, What: "resolve an arena offset — one L1 load plus an add"},
	{Name: "table_arena_grab_slab", By: C, What: "hand one worker its next private slab"},
	{Name: "table_worker_make", By: C, What: "one thread's allocation front"},
	{Name: "table_worker_bump", By: C, What: "reserve one node's bytes in a worker's slab, untyped"},
	{Name: "TableCtx", By: C, What: "which encoding a walk is reading: an arena's offsets, or a region's self-relative deltas"},
	{Name: "TableRegionSink", By: C, What: "bump-allocation into the caller's exact region"},
	{Name: "TableSink", By: C, What: "where a node comes from — a region sink or a worker; C's form of the reference's Sink template parameter"},

	// the one ATOMIC the arena needs, and its spelling is FEATURE TESTED
	// because C99 has none (§6.4). The names are claimed whatever the
	// compiler picks, so a schema cannot become illegal by changing compilers.
	{Name: "TableAtomicU32", By: C, What: "the arena cursor's atomic u32"},
	{Name: "TableAtomicPtr", By: C, What: "an arena segment's atomic pointer"},
	{Name: "table_atomic_load32", By: C, What: "an acquire load of the cursor"},
	{Name: "table_atomic_store32", By: C, What: "a relaxed store of the cursor"},
	{Name: "table_atomic_load_ptr", By: C, What: "an acquire load of a segment"},
	{Name: "table_atomic_store_ptr", By: C, What: "a relaxed store of a segment"},
	{Name: "table_arena_cas32", By: C, What: "the slab handout's compare-exchange"},
	{Name: "table_arena_cas_ptr", By: C, What: "the segment publication's compare-exchange"},

	// the BLOCK form's C spellings (§19)
	{Name: "table_block_row_at", By: C, What: "one row of an array, at the pitch the instance gives"},

	// THE TUNING CONSTANTS, which C spells as #define and the other two as
	// typed constants inside their own scope. A macro is not scoped by
	// anything, so each is a unit-level name here (§6, §7.1, §16).
	{Name: "kTableAlign", By: C, What: "every arena node starts eight-aligned"},
	{Name: "kTableAllocFailed", By: C, What: "the arena's refusal, never a silent smaller slab"},
	{Name: "kTableMaxDepth", By: C, What: "the pointer-chain depth cap (§3.1)"},
	{Name: "kTableMaxSegments", By: C, What: "the arena's segment table"},
	{Name: "kTableSegmentBits", By: C, What: "the arena's segment size, as a shift"},
	{Name: "kTableSegmentMask", By: C, What: "the offset inside a segment"},
	{Name: "kTableSegmentSize", By: C, What: "the arena's segment size"},
	{Name: "kTableSlabBytes", By: C, What: "one worker's slab: one atomic per slab, none per node"},
	{Name: "table_cook_header_bytes", By: C, What: "the cooked header's 64 bytes (§7.1)"},
	{Name: "kTableJsonMaxDepth", By: C, What: "the text form's nesting cap"},
	{Name: "kTableJsonMaxKey", By: C, What: "the longest key that can name a field"},
	{Name: "kTableJsonMaxNumber", By: C, What: "the longest numeric token the walk converts"},

	// THE TEXT FORM'S WALK (docs/SPEC-TABLES.md §16), which C# scopes inside a
	// nested class and C++ hides in a namespace. C has neither, and the walk
	// lives in <Base>Table.c beside the unit's own header, so every one of its
	// spellings could collide with a declaration in that unit. They are one
	// family with one job, and the registry lists them rather than filtering
	// them: a scan that has to recognise a prefix is a scan that goes blind the
	// day a name leaves the family.
	{Name: "table_json_base64_alphabet", By: C, What: "the text form's walk"},
	{Name: "table_json_count", By: C, What: "the text form's walk"},
	{Name: "table_json_decimal_point", By: C, What: "the text form's walk"},
	{Name: "table_json_element_shape", By: C, What: "the text form's walk"},
	{Name: "table_json_encode_utf8", By: C, What: "the text form's walk"},
	{Name: "table_json_finite", By: C, What: "the text form's walk"},
	{Name: "table_json_get_raw", By: C, What: "the text form's walk"},
	{Name: "table_json_get_signed", By: C, What: "the text form's walk"},
	{Name: "table_json_guard_holds", By: C, What: "the text form's walk"},
	{Name: "table_json_hex4", By: C, What: "the text form's walk"},
	{Name: "TableJsonIn", By: C, What: "the text form's walk"},
	{Name: "table_json_is_bytes", By: C, What: "the text form's walk"},
	{Name: "table_json_is_enum", By: C, What: "the text form's walk"},
	{Name: "table_json_is_flags", By: C, What: "the text form's walk"},
	{Name: "table_json_is_keyed", By: C, What: "the text form's walk"},
	{Name: "table_json_key_id", By: C, What: "the text form's walk"},
	{Name: "table_json_key_name", By: C, What: "the text form's walk"},
	{Name: "table_json_keyed_slot_key", By: C, What: "the text form's walk"},
	{Name: "table_json_keyed_slot_valid", By: C, What: "the text form's walk"},
	{Name: "table_json_line", By: C, What: "the text form's walk"},
	{Name: "table_json_literal", By: C, What: "the text form's walk"},
	{Name: "TableJsonOut", By: C, What: "the text form's walk"},
	{Name: "table_json_peek", By: C, What: "the text form's walk"},
	{Name: "table_json_put", By: C, What: "the text form's walk"},
	{Name: "table_json_raw", By: C, What: "the text form's walk"},
	{Name: "table_json_read", By: C, What: "the text form's walk"},
	{Name: "table_json_read_field", By: C, What: "the text form's walk"},
	{Name: "table_json_read_scalar", By: C, What: "the text form's walk"},
	{Name: "table_json_read_table", By: C, What: "the text form's walk"},
	{Name: "table_json_scan_number", By: C, What: "the text form's walk"},
	{Name: "table_json_scan_string", By: C, What: "the text form's walk"},
	{Name: "table_json_set_count", By: C, What: "the text form's walk"},
	{Name: "table_json_set_raw", By: C, What: "the text form's walk"},
	{Name: "table_json_shape", By: C, What: "the text form's walk"},
	{Name: "table_json_skip_container", By: C, What: "the text form's walk"},
	{Name: "table_json_skip_value", By: C, What: "the text form's walk"},
	{Name: "table_json_space", By: C, What: "the text form's walk"},
	{Name: "table_json_text", By: C, What: "the text form's walk"},
	{Name: "table_json_token_double", By: C, What: "the text form's walk"},
	{Name: "table_json_token_integer", By: C, What: "the text form's walk"},
	{Name: "table_json_utf8", By: C, What: "the text form's walk"},
	{Name: "table_json_value_shape", By: C, What: "the text form's walk"},
	{Name: "table_json_variant_id", By: C, What: "the text form's walk"},
	{Name: "table_json_variant_name", By: C, What: "the text form's walk"},
	{Name: "table_json_walk_number", By: C, What: "the text form's walk"},
	{Name: "table_json_write", By: C, What: "the text form's walk"},
	{Name: "table_json_write_base64", By: C, What: "the text form's walk"},
	{Name: "table_json_write_field", By: C, What: "the text form's walk"},
	{Name: "table_json_write_float", By: C, What: "the text form's walk"},
	{Name: "table_json_write_scalar", By: C, What: "the text form's walk"},
	{Name: "table_json_write_signed", By: C, What: "the text form's walk"},
	{Name: "table_json_write_string", By: C, What: "the text form's walk"},
	{Name: "table_json_write_unsigned", By: C, What: "the text form's walk"},
	{Name: "table_json_write_value", By: C, What: "the text form's walk"},

	// The C spellings of names the other backends carry in PascalCase. They
	// are separate entries because they are separate SPELLINGS: C spells a
	// function and a file-scope constant snake_case, which is the convention
	// its packet half already uses, so the same runtime value is one identifier
	// in Rust and C++ and a different identifier here. Both are claimed for
	// every target, because the claim is front-end law and a unit legal under
	// one backend must be legal under all of them.
	{Name: "table_block_default_allocator", By: C, What: "the malloc/free pair, for a caller with none of its own"},
	{Name: "table_block_magic", By: C, What: "the block prologue's magic, and the byte-order check with it"},
	{Name: "table_block_byte_order", By: C, What: "this build's byte order, as the prologue carries it"},
	{Name: "table_cook_open", By: C, What: "the cooked header's WHOLE check, shared by every <Name>Open"},
	{Name: "table_cook_magic", By: C, What: "the cooked header's magic, and the byte-order check with it"},
	{Name: "table_cook_byte_order", By: C, What: "this build's byte order, as a cooked header records it"},
	{Name: "table_cook_max_align", By: C, What: "the greatest region alignment a cooked header may name"},
}

// All returns the whole registry, sorted by name.
func All() []Name {
	out := append([]Name(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Claimed is every UNIT-LEVEL registered name, sorted — what the checker
// claims when a unit declares a table, whatever target it is generating for.
// Scoped spellings are not claimed: they cannot collide with a declaration.
func Claimed() []string {
	var names []string
	for _, n := range registry {
		if !n.Scoped {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// DefinedBy is every name one backend's emitted text carries, scoped ones
// included, sorted.
func DefinedBy(b Backend) []string {
	var names []string
	for _, n := range registry {
		if n.By&b != 0 {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// RustConstants is every registered name the Rust backend spells as a
// crate-scope CONSTANT, sorted. The caller maps each through
// ir.RustConstName to get the emitted identifier; this package does not, so
// that it keeps depending on nothing.
func RustConstants() []string {
	var names []string
	for _, n := range registry {
		if n.RustConst && n.By&Rust != 0 {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// Registered reports whether a spelling is in the registry at all — the
// question the emitted-text scan asks of every Table* identifier it finds.
func Registered(name string) bool {
	for _, n := range registry {
		if n.Name == name {
			return true
		}
	}
	return false
}

// By is the set of backends that define name, or 0 when nothing does.
//
// A diagnostic that names a lowercase claim needs this: C and Go both put
// lowercase runtime names at unit scope, for different reasons — a Go package
// is one namespace, and C has no namespace at all — and a message that guessed
// from the spelling would tell half of its readers about the wrong language.
func By(name string) Backend {
	for _, n := range registry {
		if n.Name == name {
			return n.By
		}
	}
	return 0
}
