// The COOKED FORM's read side in C++ (SPEC-TABLES.md §7): <Name>Open, which
// matches a header and POINTS.
//
// Cooking is fundamentally an optimization, and this file is the half that
// makes it one. A cook is the structure a build at one BUILD VERSION (§20)
// laid out, in that build's byte order, behind a 64-byte header that
// build-locks it. Opening it is a header match and a cast: no walk, no
// per-node work, no fix-up pass, nothing touched but the header — which is
// what makes open O(1) IN THE FILE'S SIZE and what lets a mapped file's pages
// be touched only as they are used.
//
// THE SCALE THAT ASKS FOR IT is stated on the page as the requirement it is —
// *"Assume we have say, 100mbs or many gigabytes of data in Assets.bin at some
// point."* / *"We would want this to be fast :)"*. A walk of any shape
// forfeits it, so there is none here.
//
// WHAT VALIDATES AN UNTRUSTED FILE IS A TOOL, not this code: `schema
// cook-check` reads the DATA against the ATTRIBUTION over the same descriptors
// (§7.4). The runtime keeps ONE entry point, and it either matched this
// build's header or it returns NULL and the caller falls back to a wire load —
// the path that carries every version.
//
// Nothing here is emitted for a unit of value-only tables: a cook's reader is
// the variable-length surface's, so a pointer-free unit's <Base>Table.h is
// byte-identical with or without this file, which is what the zero-cost gate
// (§2.2) asks.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// buildVersionConstant is the unit's BUILD VERSION behind its OWN per-package
// guard.
//
// It has a guard of its own because two generated headers define it — the cook
// runtime below, in <Base>Table.h, and the block runtime, in <Base>Block.h —
// and a Block header includes the Table header beside it. One guard around one
// text is what makes "both forms carry the same id" (§20.6) a fact of the
// build rather than a redefinition in any translation unit that uses both.
func buildVersionConstant(pkg string, buildVersion uint64) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_BUILD_VERSION"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// THE BUILD VERSION (SPEC-TABLES.md §20): one digest over every fact the bytes
// this build produces depend on — the type wire's protocol id, every record's
// layout as the compiler's own C ABI model computes it, and the facts that
// decide what a load PUTS in those slots. It is the number a cook's header
// carries and the number Open compares, and the number a block's prologue
// carries and BlockOpen compares: a build version answers "which build?" and
// not "which form?", and what separates the two forms is their MAGIC.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
// what everything cooked or blocked is keyed by. A table edit moves this and
// never the protocol id; a type edit moves both.
inline constexpr uint64_t BuildVersion = ` + fmt.Sprintf("0x%016xull", buildVersion) + `;

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// tableCookRuntime is the cooked form's shared read runtime, guarded per
// package like the rest of the table runtime so one definition survives any
// include order and a lone Table.h works standalone.
func tableCookRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_COOK"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- the cooked form (SPEC-TABLES.md §7) ----
//
// A cooked file is a HEADER, a DATA part and an ATTRIBUTION part, in that
// order. Every word of the header is a u64 written in the byte order the cook
// was produced in, and the header is 64 bytes:
//
//     0  magic               0x4b4f4f434d484353, read BYTEWISE before anything else
//     8  build_version       the unit's id (SPEC-TABLES.md §20)
//    16  byte_order          1 little, 2 big — the order that WROTE the file
//    24  data_length         the region's bytes, rounded up to alignment
//    32  attribution_length  the directory's bytes, or 0
//    40  alignment           the region's alignment, never below eight
//    48  reserved            zero
//    56  reserved            zero
//
// The DATA part is Lock's region written verbatim (§7.2) — the root at its
// base — and it is what a runtime points at. The ATTRIBUTION part is the node
// directory (§6.3), and NOTHING THAT READS THE STRUCTURE TOUCHES IT: it is
// written beside the data for schema cook-check, so a build that ships no
// tooling need not carry it at all.
static const int64_t kTableCookHeaderBytes = 64;

// THE MAGIC'S VALUE, and a consumer written from the page needs the constant
// rather than a description of one. It is "SCHMCOOK" read as ASCII in the byte
// order a little-endian store produces — the same shape the block form's
// SCHMABLK takes, so a hex dump of a little-endian cook is legible and the two
// accelerators sit in one vocabulary.
//
// IT IS STORED IN THE PRODUCER'S ORDER, which is what makes it the byte-order
// check as well as the form check: a consumer reads back this build's
// constant, or that constant byte-reversed — which identifies a cook of the
// OTHER order — or something that is not a cook. All three answers but the
// first refuse, and a cook and a BLOCK are separated here too, because a
// form's identity belongs in its magic rather than in a second digest.
inline constexpr uint64_t TableCookMagic = 0x4b4f4f434d484353ull;

// THIS BUILD's byte order, as the header's own word carries it. The magic is
// what REFUSES a foreign order; this word is what RECORDS which order wrote
// the file, so a refusal names the order rather than inferring it and a tool
// dumping a cook reads the fact. A file whose magic matched and whose order
// word did not is corrupt, and there is no reading that recovers it.
//
// The BUILD VERSION cannot do either job: §20.1 digests byteorder as a
// GENERATION input, little for every target schema generates for today, so
// two builds of one schema for two orders emit the same id.
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
inline constexpr uint64_t TableCookByteOrder = 2; // big
#else
inline constexpr uint64_t TableCookByteOrder = 1; // little
#endif

// The greatest region alignment a cooked file may name. The DATA part begins
// at align_up( 64, alignment ), which is 64 for every unit this language can
// declare — the largest alignment it has is sixteen — so a word past this cap
// describes a file no build of this schema wrote (SPEC-TABLES.md §7.1).
inline constexpr uint64_t TableCookMaxAlign = 64;

// The header read, BYTEWISE. memcpy is the portable spelling of "these eight
// bytes, in this machine's order"; every compiler this repo builds under folds
// it to one load, and it is the only read in the whole of Open that is not a
// comparison.
inline uint64_t table_cook_read64( const uint8_t * p )
{
    uint64_t v;
    memcpy( &v, p, sizeof( v ) );
    return v;
}

// TableCookOpen: THE WHOLE CHECK, in one place, because §7 states the
// enumeration once and every generated <Name>Open is that one enumeration plus
// its own root's two layout facts.
//
// THE CHECK, in order: the magic read bytewise, the byte order it establishes,
// the build version against this build's own, both RESERVED words zero, the
// region alignment the header names, the two part lengths against the length
// the caller passed — a truncated file and a file with trailing bytes are the
// same refusal — the root's own storage inside the data part, and the
// alignment of the base.
//
// AND THAT IS ALL OF IT. On a match the bytes ARE what this build wrote, in
// this build's layout and this build's byte order, so there is nothing to
// validate and nothing to fix up: the caller gets the root. Nothing per node
// happens here, which is what makes open O(1) in the file's size; a walk of
// any shape would forfeit that, and validating an untrusted file is schema
// cook-check's job and a person's decision (§7.4).
//
// EVERY NUMBER BELOW COMES OUT OF THE FILE, so the arithmetic is unsigned and
// each term is BOUNDED BEFORE IT IS ADDED: a forged length near 2^64 must
// refuse, and an addition that wrapped would be the defect the comparison
// after it was supposed to catch. Nothing past length is read on any path,
// including every refusing one.
inline const uint8_t * TableCookOpen( const void * bytes, uint64_t length, uint64_t root_size, uint64_t root_align )
{
    if ( bytes == NULL ) { return NULL; }
    if ( length < (uint64_t) kTableCookHeaderBytes ) { return NULL; }
    const uint8_t * raw = (const uint8_t *) bytes;
    // the MAGIC, bytewise and first: it is what establishes the byte order
    // every other header word is read in, so nothing else may be read before
    // it. A byte-reversed constant is a cook of the other order and refuses
    // here, which is why the order never reaches a fix-up pass.
    if ( table_cook_read64( raw ) != TableCookMagic ) { return NULL; }
    if ( table_cook_read64( raw + 16 ) != TableCookByteOrder ) { return NULL; }
    if ( table_cook_read64( raw + 8 ) != BuildVersion ) { return NULL; }
    // the RESERVED words: a non-zero one means a writer used a form this build
    // does not understand, and Open refuses rather than ignoring it.
    if ( table_cook_read64( raw + 48 ) != 0 ) { return NULL; }
    if ( table_cook_read64( raw + 56 ) != 0 ) { return NULL; }
    const uint64_t data_length = table_cook_read64( raw + 24 );
    const uint64_t attribution_length = table_cook_read64( raw + 32 );
    const uint64_t alignment = table_cook_read64( raw + 40 );
    // THE ALIGNMENT WORD IS DATA, and it is the one header field the rest of
    // the check does arithmetic WITH rather than only comparison against. A
    // region's alignment is a power of two, never below eight (the floor that
    // puts the attribution part on an eight-byte boundary without a second
    // padding rule) and never past the cap above; a word that is none of those
    // rounds nothing and aligns nothing, so it is refused before it is used.
    if ( alignment < 8 || alignment > TableCookMaxAlign ) { return NULL; }
    if ( ( alignment & ( alignment - 1 ) ) != 0 ) { return NULL; }
    // and it must be an alignment THE ROOT CAN SIT AT, since the root is at
    // the region's base: both are powers of two, so "at least the root's"
    // is one division.
    if ( ( alignment % root_align ) != 0 ) { return NULL; }
    // The DATA part begins at align_up( 64, alignment ). It is DERIVED and not
    // a header field, because a fact a reader computes is a fact two writers
    // cannot disagree about.
    const uint64_t data_offset = ( (uint64_t) kTableCookHeaderBytes + alignment - 1 ) & ~( alignment - 1 );
    if ( length < data_offset ) { return NULL; }
    // the two part lengths against the length the caller passed. The whole
    // file is data_offset + data_length + attribution_length, and a length
    // that is not EXACTLY that refuses — truncation and trailing bytes are one
    // refusal, and both terms are subtracted rather than added so no sum can
    // carry.
    if ( data_length > length - data_offset ) { return NULL; }
    if ( attribution_length != length - data_offset - data_length ) { return NULL; }
    // the ROOT sits at the region's base, so the region has to hold it: a
    // shorter data part describes a root partly outside the file, which is the
    // one way a match-and-point reader could hand back storage it never
    // received.
    if ( data_length < root_size ) { return NULL; }
    const uint8_t * base = raw + data_offset;
    // the alignment of the BASE. The header pads the data part to the region's
    // alignment, so a base an allocator or mmap gave you is already aligned —
    // mmap gives page alignment for free — and a base that is not is a caller's
    // buffer this form cannot be read out of.
    if ( ( (uintptr_t) base % (uintptr_t) alignment ) != 0 ) { return NULL; }
    return base;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// emitCookSurface emits one table's <Name>Open — the RUNTIME's only entry point
// into a cooked file (SPEC-TABLES.md §7).
//
// EVERY TABLE GETS ONE. A cook's root is a table, any table: the tool's
// `--root` names one and refuses a `type`, which is not a node and has no type
// id, and §11 claims the spelling on every closure member for the same reason
// the mutable-life suffixes are claimed there — a table gains and loses
// pointers as an edit, and a name free yesterday must not become a collision
// tomorrow. A fixed root's cook is one region of one node (§7), which is this
// same header match and then the one record.
func (g *tableGen) emitCookSurface(members []*ir.Struct) {
	first := true
	for _, st := range members {
		if !st.IsTable {
			continue // a `type` is not a node and cannot be a cook's root
		}
		if first {
			g.pf("// ---- the cooked form: point at a cook (SPEC-TABLES.md §7) ----\n\n")
			first = false
		}
		g.emitCookOpen(st)
	}
}

func (g *tableGen) emitCookOpen(st *ir.Struct) {
	n := st.Name
	g.pf("// %sOpen: match the header and POINT. On a match the bytes ARE what this\n", n)
	g.pf("// build wrote, in this build's layout and this build's byte order, so there\n")
	g.pf("// is nothing to validate and nothing to fix up and the root comes back as it\n")
	g.pf("// lies. On ANY refusal it returns NULL and the caller falls back to a wire\n")
	g.pf("// load, which is the path that carries every version.\n")
	g.pf("//\n")
	g.pf("// It is O(1) IN THE FILE'S SIZE — the header and nothing per node — so a one\n")
	g.pf("// megabyte cook and a one gigabyte cook open in the same time, and a mapped\n")
	g.pf("// file's pages are touched only as they are used. That is a property of\n")
	g.pf("// touching nothing at open rather than a separate mechanism.\n")
	g.pf("//\n")
	if g.isVar(st.Name) {
		// the deref sentence belongs to a table that HAS a reference slot; a
		// value-only table has none, and its header should not describe one
		g.pf("// A REFERENCE INSIDE THE REGION IS DEREFERENCED THROUGH %sAt: the slot holds\n", n)
		g.pf("// the signed self-relative byte delta of §6.3, so a deref is one add and\n")
		g.pf("// needs no base pointer, a whole region relocates by plain memcpy, and a\n")
		g.pf("// delta of zero is null.\n")
	} else {
		g.pf("// %s IS FIXED-SIZE, so its cook is ONE REGION OF ONE NODE and not a second\n", n)
		g.pf("// shape (§7): one struct behind the header, at the region's base, which is\n")
		g.pf("// what this returns. There is no graph below it and nothing to resolve.\n")
	}
	g.pf("//\n")
	g.pf("// There is ONE entry point and no tolerant twin: a build either wrote this\n")
	g.pf("// file or it did not, and the build version is what says which. Validating a\n")
	g.pf("// file whose provenance a person doubts is schema cook-check, offline,\n")
	g.pf("// over the ATTRIBUTION part beside the data — a person's decision, never a\n")
	g.pf("// parameter on a load.\n")
	g.pf("inline const %s * %sOpen( const void * bytes, uint64_t length )\n{\n", n, n)
	g.pf("    return (const %s *) TableCookOpen( bytes, length, (uint64_t) sizeof( %s ), (uint64_t) alignof( %s ) );\n}\n\n", n, n, n)
}

// emitCookLayoutAsserts is this backend's half of the LAYOUT CONTRACT
// (SPEC-TABLES.md §20.3) for the COOK closure: the compiler computes each
// record's layout from the declaration and folds it into the build version,
// and this backend emits code asserting that ITS OWN compiler agrees. A
// disagreement is a BUILD ERROR naming the record, the field and both offsets
// — which is what lets the id be settled from the schema alone, before any
// game binary exists, and what turns ABI drift from a silent refusal at open
// into a failure at build.
//
// THE GENERATED STRUCT IS THE COOKED RECORD. A cooked node is this record at
// these offsets: the region's bytes and the build version's record lines come
// from one computation, and these asserts are what make that a property of the
// build rather than a claim on a page.
//
// EVERY COOKABLE RECORD, which §20.3 commits the model to and which is every
// record in a unit's table closure — not only the ones a block form reaches,
// and not only the ones a pointered unit declares. They are emitted by the file
// that DECLARES each record, which is the same rule §7 gives a member's walk:
// per referencing file would assert one record many times, and only where
// pointers are declared would leave a value-only file's records unasserted.
func (g *tableGen) emitCookLayoutAsserts(members []*ir.Struct) {
	var laid []*ir.Struct
	for _, st := range members {
		if ir.RecordLayout(g.unit, st) != nil {
			laid = append(laid, st)
		}
	}
	if len(laid) == 0 {
		return
	}
	g.pf("// ---- the cook's layout contract (SPEC-TABLES.md §20.3) ----\n")
	g.pf("//\n")
	g.pf("// The compiler derived every number below from the declaration and folded it\n")
	g.pf("// into the BUILD VERSION; these asserts are this compiler saying whether it\n")
	g.pf("// agrees. The model is not self-evidently right — on 32-bit System V\n")
	g.pf("// alignof(uint64_t) is 4, not 8 — which is precisely why it is asserted\n")
	g.pf("// rather than assumed.\n")
	for _, st := range laid {
		ml := ir.RecordLayout(g.unit, st)
		g.pf("static_assert( sizeof( %s ) == %d, \"%s's sizeof moved: the build version was taken over %d, so a cook of it would not be this build's file (SPEC-TABLES.md §20.3)\" );\n",
			st.Name, ml.Size, st.Name, ml.Size)
		g.pf("static_assert( alignof( %s ) == %d, \"%s's alignof moved: the build version was taken over %d (SPEC-TABLES.md §20.3)\" );\n",
			st.Name, ml.Align, st.Name, ml.Align)
		for _, fl := range ml.Fields {
			g.pf("static_assert( offsetof( %s, %s ) == %d, \"%s's field %s moved: the build version was taken over offset %d (SPEC-TABLES.md §20.3)\" );\n",
				st.Name, fl.Field.Name, fl.Offset, st.Name, fl.Field.Name, fl.Offset)
		}
	}
	g.pf("\n")
}
