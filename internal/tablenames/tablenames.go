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
type Backend uint8

const (
	// Cpp is the C++ table backend (internal/codegen/cpptable).
	Cpp Backend = 1 << iota
	// Cs is the C# table backend (internal/codegen/cstable).
	Cs
	// Rust is the Rust table backend (internal/codegen/rusttable).
	Rust
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
}

// registry is the whole list. Sorted by nothing in particular — callers that
// need an order sort for themselves, because map and slice order must never
// decide what a diagnostic says.
var registry = []Name{
	// the shared surface both backends define per unit
	{Name: "TableReport", By: Cpp | Cs | Rust, What: "the read report — the permissive contract's ledger"},
	{Name: "TableWriter", By: Cpp | Cs | Rust, What: "the wire writer over the caller's buffer"},
	{Name: "TableReader", By: Cpp | Cs | Rust, What: "the wire reader over the caller's buffer"},
	{Name: "TableTypeInfo", By: Cpp | Cs | Rust, What: "a table's reflection descriptor"},
	{Name: "TableFieldInfo", By: Cpp | Cs | Rust, What: "a field's reflection descriptor"},
	// a UNION field's shape (docs/SPEC-TABLES.md §8.1): the tag, and each arm's
	// payload by its own descriptor. Every backend defines both, and every one
	// puts them at unit level beside the two descriptors above — a union
	// field's column has to name a type, and a nested one would be reached
	// through a descriptor a walk holds by value.
	{Name: "TableUnionInfo", By: Cpp | Cs | Rust, What: "a union field's tag and its arms"},
	{Name: "TableUnionArmInfo", By: Cpp | Cs | Rust, What: "one union arm's payload and descriptor"},
	{Name: "TableEnumId", By: Cpp | Cs, What: "an enum value -> its table-wire variant id"},
	{Name: "TableEnumValue", By: Cpp | Cs, What: "a table-wire variant id -> its enum value"},

	// the ENUM-KEYED array's storage type (docs/SPEC-TABLES.md §2.4). C++ spells it
	// a class template and C# a generic class; both put it at unit level, and
	// both emit it ONLY into a unit that declares a keyed array — but the
	// claim is unconditional on a unit declaring a table, for the same reason
	// the variable-length names are: adding a keyed array to an existing table
	// must not turn a legal declaration elsewhere into a collision.
	{Name: "TableKeyed", By: Cpp | Cs | Rust, What: "an enum-keyed array's slot storage"},

	// SCOPED: a field descriptor's nested-table column. C++ spells it `table`
	// and C# `Table`, and either way it is reached through its owner, so a
	// schema is free to declare the name.
	{Name: "Table", By: Cs, What: "TableFieldInfo's nested-table column", Scoped: true},

	// C++'s float <-> IEEE-754 bit pattern helpers
	{Name: "table_bits_to_float", By: Cpp, What: "u32 bits -> float"},
	{Name: "table_float_to_bits", By: Cpp, What: "float -> u32 bits"},
	{Name: "table_bits_to_double", By: Cpp, What: "u64 bits -> double"},
	{Name: "table_double_to_bits", By: Cpp, What: "double -> u64 bits"},

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
	{Name: "TableReset", By: Cpp | Cs, What: "restore a value's declared defaults in place — C#'s reset itself, and C++'s ADL hook onto <Name>Reset for the arena"},
	// SCOPED: the TEXT FORM's generic walk (docs/SPEC-TABLES.md §16), a nested
	// static class of Schema. Everything the walk spells — its reader, its
	// writer sink, its scanners — is a member of it, so one registration
	// covers the whole surface and the walk claims not one name at unit
	// scope. C++ spells the same functions as a TableJson* family at
	// namespace scope inside its own package guard.
	{Name: "TableJson", By: Cs, What: "the text form's generic walk, a nested class of Schema", Scoped: true},
	{Name: "TableBitsToFloat", By: Cs, What: "u32 bits -> float"},
	{Name: "TableFloatToBits", By: Cs, What: "float -> u32 bits"},
	{Name: "TableBitsToDouble", By: Cs, What: "u64 bits -> double"},
	{Name: "TableDoubleToBits", By: Cs, What: "double -> u64 bits"},

	// the VARIABLE-LENGTH runtime (docs/SPEC-TABLES.md §6): the arena, the region
	// and the reference slot. C++ only today — the C# backend carries the
	// fixed class and refuses a pointered unit by name (§11).
	// TableRef carries a SECOND, unrelated meaning in C#: a field descriptor's
	// lazy factory for the nested table's descriptor, which is scoped. The
	// C++ meaning is unit-level, so the name is claimed whatever the target —
	// the claim is the union, never the intersection.
	{Name: "TableRef", By: Cpp | Cs, What: "C++: a pointer's eight-byte reference slot; C#: a field descriptor's nested-table factory"},
	{Name: "TableSlot", By: Cpp, What: "an arena slot"},
	{Name: "TableArena", By: Cpp, What: "the builder's segmented slab arena"},
	{Name: "TableSlab", By: Cpp, What: "one worker's privately-owned slab of it"},
	{Name: "TableWorker", By: Cpp, What: "a builder worker's allocation front"},
	{Name: "TableBuilder", By: Cpp, What: "the mutable life's base"},
	{Name: "TableRegion", By: Cpp, What: "the locked, packed region"},

	// the BLOCK FORM's runtime (docs/SPEC-TABLES.md §19), emitted into
	// <Base>Block.h / <Base>Block.cs and into no Table source at all. Claimed
	// whenever a unit declares a table, on the same terms as everything above:
	// nothing declares the block form, every fixed table has one, and a table
	// gains and loses it as its closure gains and loses a pointer.
	{Name: "TableBlockAllocator", By: Cpp, What: "the caller's alloc/free pair, used once at build time"},
	{Name: "TableBlockDefaultAllocator", By: Cpp, What: "the malloc/free pair, for a caller with none of its own"},
	{Name: "table_block_default_alloc", By: Cpp, What: "the default allocator's alloc half"},
	{Name: "table_block_default_free", By: Cpp, What: "the default allocator's free half"},
	{Name: "TableBlockTriple", By: Cpp | Cs | Rust, What: "one array's (offset_of, count, stride)"},
	{Name: "TableBlockRefusal", By: Cpp, What: "why Begin refused: the array, its count and its maximum"},
	{Name: "TableBlockRows", By: Cpp | Cs, What: "one array's rows, iterated at the pitch the instance gives"},
	{Name: "TableBlockSpan", By: Cpp, What: "one array's rows as a contiguous view (C# uses ReadOnlySpan)"},
	{Name: "TableBlockFieldInfo", By: Cpp | Cs | Rust, What: "a block field's reflection descriptor"},
	{Name: "TableBlockInfo", By: Cpp | Cs | Rust, What: "a block's reflection descriptor"},
	{Name: "TableBlockMagic", By: Cpp | Cs | Rust, What: "the block prologue's magic, and the byte-order check with it"},
	{Name: "TableBlockLayout", By: Cs, What: "the C# layout contract's check, run once", Scoped: true},
	{Name: "TableBlockRead64", By: Cs, What: "the C# prologue read (a Schema member, so it claims nothing)", Scoped: true},
	{Name: "TableBlockByteOrder", By: Cpp | Cs | Rust, What: "this build's byte order, as the prologue carries it"},
	{Name: "table_block_byteswap64", By: Cpp, What: "the byte-order check's swap"},
	{Name: "table_block_read64", By: Cpp, What: "the prologue read BYTEWISE"},
	{Name: "table_block_align", By: Cpp, What: "round an offset up to an alignment"},

	// the COOKED FORM's read runtime (docs/SPEC-TABLES.md §7), emitted into
	// <Base>Table.h of a unit that declares a variable-length table — a cook's
	// root is one — and into no value-only unit's header at all. Claimed
	// whenever a unit declares a table, on the same terms as the
	// variable-length names above: a table gains and loses its cook reader as
	// its closure gains and loses a pointer, and a name free today must not
	// become a collision tomorrow.
	{Name: "TableCookOpen", By: Cpp, What: "the cooked header's WHOLE check, shared by every <Name>Open"},
	{Name: "TableCookMagic", By: Cpp | Cs | Rust, What: "the cooked header's magic, and the byte-order check with it"},
	{Name: "TableCookByteOrder", By: Cpp | Cs | Rust, What: "this build's byte order, as a cooked header records it"},
	{Name: "TableCookMaxAlign", By: Cpp | Cs | Rust, What: "the greatest region alignment a cooked header may name"},
	{Name: "table_cook_read64", By: Cpp, What: "the cooked header read BYTEWISE"},

	// ---- the RUST backend's own spellings (internal/codegen/rusttable) ----
	//
	// Rust has no overloading, so the enum identity pair C++ and C# spell as an
	// overload set is a TRAIT the generated code implements once per enum: one
	// unit-level name rather than one per declaration.
	{Name: "TableEnum", By: Rust, What: "an enum's TABLE-wire identity, as a trait (§5)"},

	// The text form's three bounds, at unit level in Rust because a Rust
	// constant has no class to hide in.
	{Name: "TableJsonMaxDepth", By: Rust, What: "the text walk's nesting cap (§16)"},
	{Name: "TableJsonMaxKey", By: Rust, What: "the longest key that can name a field"},
	{Name: "TableJsonMaxNumber", By: Rust, What: "the longest numeric token the walk converts"},

	// SCOPED, Rust: private to the shared runtime module, which does not
	// import the crate root — so a declaration taking one of these names
	// cannot collide with it, and the name stays the schema author's.
	{Name: "TableJsonIn", By: Rust, What: "the text walk's read cursor", Scoped: true},
	{Name: "TableJsonOut", By: Rust, What: "the text walk's measure-or-write sink", Scoped: true},
	{Name: "TableJsonKey", By: Rust, What: "a scanned object key, kept on the stack", Scoped: true},
	{Name: "TableJsonNumber", By: Rust, What: "a scanned numeric token", Scoped: true},
	{Name: "TableJsonSink", By: Rust, What: "where a scanned string goes, or nowhere", Scoped: true},
	{Name: "TableJsonBase64", By: Rust, What: "the base64 alphabet a bytes(N) rides under", Scoped: true},

	// the COOK's C# half (docs/SPEC-TABLES.md §7, §19.2's road). C# has no include
	// guard, so the read runtime is emitted ONCE per unit into the cook home's
	// <Base>Cook.cs — and because one assembly sees every file, a declaration
	// taking one of these names anywhere in the unit collides with it.
	{Name: "TableCookHeaderBytes", By: Cs | Rust, What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, so the claim is the UNION and the name is claimed"},
	{Name: "TableCookRead64", By: Cs, What: "the C# header read (a Schema member, so it claims nothing)", Scoped: true},
	{Name: "TableCookLayout", By: Cs, What: "the C# cook closure's layout contract, run once (§20.3)"},
	{Name: "TableCookInfo", By: Cs | Rust, What: "a cooked record's reflection descriptor"},
	{Name: "TableCookFieldInfo", By: Cs | Rust, What: "a cooked field's reflection descriptor"},
	{Name: "TableCookStorage", By: Cs | Rust, What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},

	// the unit's BUILD VERSION (docs/SPEC-TABLES.md §20): the one digest a block
	// carries and BlockOpen compares. It is not a Table* spelling, and it is
	// claimed here because it is a unit-level name the generated block sources
	// define.
	{Name: "BuildVersion", By: Cpp | Rust, What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++ puts it at namespace scope, so the claim is the union"},
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
