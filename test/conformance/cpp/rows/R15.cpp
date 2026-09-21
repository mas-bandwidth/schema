// ONE CELL OF THE SCHEMA MATRIX AT THE TIP: cpp/R15 —
// "the four forward-read clamps are retired — count clamp across bounds,
// range clamp across versions, remap of an unknown variant to None,
// drop-and-count of an unknown field: each is layout_newer now".
//
// THE LAW is docs/FIXED-FORM-ALGORITHM.md §5.6 (docs/FIXED-FORM-ALGORITHM.md:967):
//
//   "The count clamp across bounds, the range clamp across versions, the remap
//    of an unknown variant to `None`, the drop-and-count of an unknown field —
//    all forward reads, now `layout_newer`. Every hostile check on a KNOWN
//    layout's records stays (§4.5, §4.6)."
//
// The four behaviours belonged to the RETIRED contract: a reader handed a
// layout it had never locked walked it FORWARD — clamped a count past its own
// bound, clamped a value past its own range, remapped a variant its set lacks
// to `None`, dropped-and-counted a field id it could not name. Under §5.3's
// step 5 none of them is reachable: A HASH IN NO LINEAGE ENTRY IS REFUSED
// `layout_newer` BEFORE ANY RECORD — refused, with the file's hash in the
// report and nothing else, no counter moved, nothing marked malformed, and not
// one destination byte written, the prefill included.
//
// THE PATH UNDER TEST is the production one, not a helper: each case calls the
// generated fixed-form reader `T##FixedLoad` (algorithm §5.3) that the
// reference suite drives (test/tables/versioning_lists.cpp,
// test/tables/versioning_numbers.cpp) and that the conformance leg's driver
// compiles (test/conformance/cpp/main.cpp) — the same generated units under
// build/tables-generated/.
//
// THE VECTORS are built in memory: the tree has no conformance data for this
// cell (testdata/conformance/tables/MANIFEST.txt carries no lineage row), so
// each case saves the NEW writer's file holding the value that PROVOKES the
// forward read the case is named for — under the retired contract, that
// reader's walk would have moved the very counter the case asserts stays zero.
// One case (the fifth) is crafted byte by byte, and its derivation is the
// comment beside it: the binding constraint of §5.3's step 5 is that THE HASH
// DECIDES BEFORE THE BYTES, so even a file whose layout bytes are this
// reader's OWN is refused when its hash is a stranger's.
//
// RED FIRST: this file is held from both ends. The negative control recorded
// beside it breaks the lineage refusal in ONE generated unit (one constant in
// build/tables-generated/) and this file goes red on the same lines it is
// green on here.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_range_widenTable.h"
#include "VNEW_range_widenTable.h"
#include "VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"
#include "VOLD_union_appendTable.h"
#include "VNEW_union_appendTable.h"
#include "VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"

static int failures = 0;
static int assertions = 0;

static void check( bool ok, const char * what )
{
    assertions++;
    std::printf( "%s %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
}

// THE POISON (algorithm §5.3's own spelling): fill the destination with 0x5A
// through a byte pointer, load, and the poison must stand — a Reset before the
// load only proves the load did not clobber, and a zero-fill looks exactly
// like a zero default. Padding is poisoned too, so a byte-for-byte comparison
// against the unloaded twin is the whole "not one destination byte written"
// claim, padding included.
static void poison( void * p, size_t bytes )
{
    unsigned char * b = (unsigned char *) p;
    for ( size_t i = 0; i < bytes; ++i ) { b[i] = 0x5A; }
}

// THE SIX LINES EVERY CASE OWES (algorithm §5.3's REFUSE table): refused
// before any record; the reason is `layout_newer`'s own, BY NAME — the name is
// the contract, the integer is each leg pair's (docs line: the C and C++ pair
// number their own set); the report carries the file's hash and nothing else;
// no counter moved — all five of §4's counters, and the fixed root's
// retain-unknown pair is refused by name (§6.6) so those two stay zero as
// well; a refusal by name is never malformed; and not one destination byte
// was written.
template < typename Report, typename Value >
static void refuses_newer( const char * what, bool own_reason, Value & back, const Value & poisoned_twin,
                           int64_t n, const Report & r, uint64_t file_hash )
{
    check( n < 0 && r.refused, what );
    check( r.refused && own_reason, "  the reason is `layout_newer`'s own — not a form byte, not a broken layout" );
    check( r.layout_hash == file_hash, "  the report carries the file's hash and nothing else (bill §12.4)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0 &&
           r.retained == 0 && r.retain_lost == 0,
           "  REFUSE is total — no counter moved" );
    check( !r.malformed, "  a refusal by name is not malformed" );
    check( std::memcmp( &back, &poisoned_twin, sizeof( back ) ) == 0, "  not one destination byte written, prefill included" );
}

// the reason compared BY NAME in the caller's own unit (the name is the
// contract; the integer is each leg pair's)
#define R15_OWN_REASON( NS, r ) ( ( r ).reason == NS::layout_newer )

// ---- 1. THE COUNT CLAMP ACROSS BOUNDS IS layout_newer ----------------------
//
// VOLD's bound is 4 (VOLD_array_bounded_grow.schema), VNEW's is 8. The file
// carries EIGHT slots — the new bound, full — so under the retired contract
// the old reader's count op would clamp 8 to 4 and move `clamped`
// (kTableFixedCount: "a count: clamp it to the reader's own bound").

static void count_clamp_across_bounds()
{
    vnew_array_bounded_grow::ArrayBoundedGrow nv;
    vnew_array_bounded_grow::ArrayBoundedGrowReset( nv );
    nv.vals_count = 8; // the NEW bound, full: past the OLD reader's own by four
    for ( int i = 0; i < 8; ++i ) { nv.vals[i] = 3000 + i; }
    std::vector< uint8_t > nb( (size_t) vnew_array_bounded_grow::ArrayBoundedGrowFixedMeasure( 1 ), 0 );
    if ( vnew_array_bounded_grow::ArrayBoundedGrowFixedSave( &nv, 1, nb.data(), (int64_t) nb.size() ) != (int64_t) nb.size() )
    {
        check( false, "count_clamp_across_bounds: the NEW writer's file saves" );
        return;
    }
    const uint64_t file_hash = vnew_array_bounded_grow::TableFixedGet64( nb.data() + vnew_array_bounded_grow::kTableFixedHashAt );

    vold_array_bounded_grow::ArrayBoundedGrow back;
    vold_array_bounded_grow::ArrayBoundedGrow back_twin;
    poison( &back, sizeof( back ) );
    poison( &back_twin, sizeof( back_twin ) );
    vold_array_bounded_grow::TableReport r;
    std::vector< vold_array_bounded_grow::TableFixedEntry > plan( 1024 );
    const int64_t n = vold_array_bounded_grow::ArrayBoundedGrowFixedLoad(
        &back, 1, nb.data(), (int64_t) nb.size(), plan.data(), (int32_t) plan.size(), NULL, &r );
    refuses_newer( "count clamp across bounds: a count past the reader's own bound is layout_newer before any record",
                   R15_OWN_REASON( vold_array_bounded_grow, r ), back, back_twin, n, r, file_hash );
}

// ---- 2. THE RANGE CLAMP ACROSS VERSIONS IS layout_newer --------------------
//
// VOLD's range is 0..100 (VOLD_range_widen.schema), VNEW's is 0..200. The file
// carries 150 — inside the NEW writer's range, past the OLD reader's max — so
// under the retired contract the reader's known-range pass would clamp 150 to
// 100 and move `clamped` (algorithm §5.3's step 11, TableFixedClampKnownRanges).

static void range_clamp_across_versions()
{
    vnew_range_widen::RangeWiden nv;
    vnew_range_widen::RangeWidenReset( nv );
    nv.lead = 0xAAAAAAAAu;
    nv.v = 150; // inside the NEW range, past the OLD max of 100
    nv.trail = 0xBBBBBBBBu;
    std::vector< uint8_t > nb( (size_t) vnew_range_widen::RangeWidenFixedMeasure( 1 ), 0 );
    if ( vnew_range_widen::RangeWidenFixedSave( &nv, 1, nb.data(), (int64_t) nb.size() ) != (int64_t) nb.size() )
    {
        check( false, "range_clamp_across_versions: the NEW writer's file saves" );
        return;
    }
    const uint64_t file_hash = vnew_range_widen::TableFixedGet64( nb.data() + vnew_range_widen::kTableFixedHashAt );

    vold_range_widen::RangeWiden back;
    vold_range_widen::RangeWiden back_twin;
    poison( &back, sizeof( back ) );
    poison( &back_twin, sizeof( back_twin ) );
    vold_range_widen::TableReport r;
    std::vector< vold_range_widen::TableFixedEntry > plan( 1024 );
    const int64_t n = vold_range_widen::RangeWidenFixedLoad(
        &back, 1, nb.data(), (int64_t) nb.size(), plan.data(), (int32_t) plan.size(), NULL, &r );
    refuses_newer( "range clamp across versions: a value past the reader's own range is layout_newer before any record",
                   R15_OWN_REASON( vold_range_widen, r ), back, back_twin, n, r, file_hash );
}

// ---- 3. THE REMAP OF AN UNKNOWN VARIANT TO None IS layout_newer ------------
//
// VOLD's Tier is { Bronze, Silver, Gold } (VOLD_enum_append.schema); VNEW
// appends Platinum. The file carries tier = Platinum — a variant the OLD
// reader's set lacks — so under the retired contract the plan's ordinal op
// would remap the unknown variant to `None` and land it (kTableFixedOrdinal:
// an ordinal past the writer's set lands None AND counts).
//
// The UNION twin provokes the same retirement through the union's tag: VOLD's
// Pick is { alpha, beta }, VNEW appends gamma, and the file carries pick =
// gamma — an ARM the old reader does not know. The same clause lands None on
// the tag.

static void remap_unknown_variant_to_none()
{
    vnew_enum_append::Lineage nv;
    vnew_enum_append::LineageReset( nv );
    nv.tier = vnew_enum_append::Tier::Platinum;
    nv.seq = 9;
    std::vector< uint8_t > nb( (size_t) vnew_enum_append::LineageFixedMeasure( 1 ), 0 );
    if ( vnew_enum_append::LineageFixedSave( &nv, 1, nb.data(), (int64_t) nb.size() ) != (int64_t) nb.size() )
    {
        check( false, "remap_unknown_variant_to_none: the enum NEW writer's file saves" );
        return;
    }
    const uint64_t file_hash = vnew_enum_append::TableFixedGet64( nb.data() + vnew_enum_append::kTableFixedHashAt );

    vold_enum_append::Lineage back;
    vold_enum_append::Lineage back_twin;
    poison( &back, sizeof( back ) );
    poison( &back_twin, sizeof( back_twin ) );
    vold_enum_append::TableReport r;
    std::vector< vold_enum_append::TableFixedEntry > plan( 1024 );
    const int64_t n = vold_enum_append::LineageFixedLoad(
        &back, 1, nb.data(), (int64_t) nb.size(), plan.data(), (int32_t) plan.size(), NULL, &r );
    refuses_newer( "remap of an unknown variant to None: a variant the reader's set lacks is layout_newer before any record",
                   R15_OWN_REASON( vold_enum_append, r ), back, back_twin, n, r, file_hash );

    vnew_union_append::Lineage uv;
    vnew_union_append::LineageReset( uv );
    uv.pick.type = vnew_union_append::PickType::Gamma;
    uv.pick.gamma.p = 7;
    std::vector< uint8_t > ub( (size_t) vnew_union_append::LineageFixedMeasure( 1 ), 0 );
    if ( vnew_union_append::LineageFixedSave( &uv, 1, ub.data(), (int64_t) ub.size() ) != (int64_t) ub.size() )
    {
        check( false, "remap_unknown_variant_to_none: the union NEW writer's file saves" );
        return;
    }
    const uint64_t union_hash = vnew_union_append::TableFixedGet64( ub.data() + vnew_union_append::kTableFixedHashAt );

    vold_union_append::Lineage uback;
    vold_union_append::Lineage uback_twin;
    poison( &uback, sizeof( uback ) );
    poison( &uback_twin, sizeof( uback_twin ) );
    vold_union_append::TableReport ur;
    std::vector< vold_union_append::TableFixedEntry > uplan( 1024 );
    const int64_t un = vold_union_append::LineageFixedLoad(
        &uback, 1, ub.data(), (int64_t) ub.size(), uplan.data(), (int32_t) uplan.size(), NULL, &ur );
    refuses_newer( "remap of an unknown variant to None: a union arm the reader lacks is layout_newer before any record",
                   R15_OWN_REASON( vold_union_append, ur ), uback, uback_twin, un, ur, union_hash );
}

// ---- 4. THE DROP-AND-COUNT OF AN UNKNOWN FIELD IS layout_newer -------------
//
// VOLD is { x, y, z } (VOLD_field_append.schema); VNEW appends `w` with the
// declared default 77. The file carries w = 777, so under the retired contract
// the old reader's plan — compiled from the stranger's layout, which is what
// the retired walk was — would drop the field it cannot name and move
// `unknown` (TableReport's own words: "unknown field ids skipped").

static void drop_and_count_unknown_field()
{
    vnew_field_append::Lineage nv;
    vnew_field_append::LineageReset( nv );
    nv.x = 11; nv.y = 22; nv.z = 33;
    nv.w = 777; // the field the OLD reader has never locked
    std::vector< uint8_t > nb( (size_t) vnew_field_append::LineageFixedMeasure( 1 ), 0 );
    if ( vnew_field_append::LineageFixedSave( &nv, 1, nb.data(), (int64_t) nb.size() ) != (int64_t) nb.size() )
    {
        check( false, "drop_and_count_unknown_field: the NEW writer's file saves" );
        return;
    }
    const uint64_t file_hash = vnew_field_append::TableFixedGet64( nb.data() + vnew_field_append::kTableFixedHashAt );

    vold_field_append::Lineage back;
    vold_field_append::Lineage back_twin;
    poison( &back, sizeof( back ) );
    poison( &back_twin, sizeof( back_twin ) );
    vold_field_append::TableReport r;
    std::vector< vold_field_append::TableFixedEntry > plan( 1024 );
    const int64_t n = vold_field_append::LineageFixedLoad(
        &back, 1, nb.data(), (int64_t) nb.size(), plan.data(), (int32_t) plan.size(), NULL, &r );
    refuses_newer( "drop-and-count of an unknown field: a field the reader cannot name is layout_newer before any record",
                   R15_OWN_REASON( vold_field_append, r ), back, back_twin, n, r, file_hash );
}

// ---- 5. THE BINDING CONSTRAINT: THE HASH DECIDES BEFORE THE BYTES ----------
//
// The crafted vector, from the law (§5.3's steps 3–5 and §3's one header rule,
// the shapes the reader reads and never re-derives):
//
//   [ 0]      3                          the fixed form byte
//   [ 1.. 7]  0x00                       the seven reserved bytes
//   [ 8..15]  H, little-endian           THE FILE'S HASH, TAKEN AS GIVEN — H is
//                                        vnew_field_append's own hash, and the OLD
//                                        reader's lineage holds 0xb1c0fa32a35181b8
//                                        alone, so H is in no entry it holds
//   [16..19]  72, little-endian          the layout's length, L
//   [20..91]  the OLD reader's OWN layout bytes (LineageFixedLayout, 72 of them)
//   [92..111] the record: H again (the per-record hash §5.3's step 11 checks), then
//             x = 11, y = 22, z = 33 as three little-endian int32 — the reader's own
//             body shape, LineageFixedRecordBytes == 20
//
// The layout bytes are this reader's own, byte for byte, and the file is STILL
// `layout_newer`: step 5 never reaches the bytes, and never reaches the record.
// A reader that compared bytes first — or walked them at all — would decode
// this file; the refusal happening here IS the retirement of the forward read.

static void hash_decides_before_the_bytes()
{
    std::vector< uint8_t > f( (size_t) vold_field_append::kTableFixedHeaderBytes + 4 +
                              (size_t) vold_field_append::LineageFixedLayoutBytes +
                              (size_t) vold_field_append::LineageFixedRecordBytes, 0 );
    const uint64_t h = vnew_field_append::LineageFixedHash; // in no entry of the OLD reader's lineage
    f[0] = 3; // the fixed form byte
    vold_field_append::TableFixedPut64( f.data() + vold_field_append::kTableFixedHashAt, h );
    vold_field_append::TableFixedPut32( f.data() + vold_field_append::kTableFixedHeaderBytes,
                                        (uint32_t) vold_field_append::LineageFixedLayoutBytes );
    std::memcpy( f.data() + vold_field_append::kTableFixedHeaderBytes + 4,
                 vold_field_append::LineageFixedLayout, (size_t) vold_field_append::LineageFixedLayoutBytes );
    uint8_t * rec = f.data() + vold_field_append::kTableFixedHeaderBytes + 4 +
                    (size_t) vold_field_append::LineageFixedLayoutBytes;
    vold_field_append::TableFixedPut64( rec, h ); // the per-record hash, the file's own
    vold_field_append::TableFixedPut32( rec + 8, 11u );
    vold_field_append::TableFixedPut32( rec + 12, 22u );
    vold_field_append::TableFixedPut32( rec + 16, 33u );

    vold_field_append::Lineage back;
    vold_field_append::Lineage back_twin;
    poison( &back, sizeof( back ) );
    poison( &back_twin, sizeof( back_twin ) );
    vold_field_append::TableReport r;
    std::vector< vold_field_append::TableFixedEntry > plan( 1024 );
    const int64_t n = vold_field_append::LineageFixedLoad(
        &back, 1, f.data(), (int64_t) f.size(), plan.data(), (int32_t) plan.size(), NULL, &r );
    refuses_newer( "the hash decides before the bytes: the reader's OWN layout under a stranger's hash is layout_newer before any record",
                   R15_OWN_REASON( vold_field_append, r ), back, back_twin, n, r, h );
}

int main()
{
    count_clamp_across_bounds();
    range_clamp_across_versions();
    remap_unknown_variant_to_none();
    drop_and_count_unknown_field();
    hash_decides_before_the_bytes();

    std::printf( "R15: %d assertions, %d failed\n", assertions, failures );
    return failures == 0 ? 0 : 1;
}
