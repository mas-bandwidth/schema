// test/conformance/cpp/rows/R15.cpp — the cpp leg's assertion of the cpp/R15
// cell: §5.6's retirement of the four forward-read clamps
// (docs/FIXED-FORM-ALGORITHM.md:971-973).
//
// The law, quoted:
//
//   "The count clamp across bounds, the range clamp across versions, the remap
//    of an unknown variant to `None`, the drop-and-count of an unknown field —
//    all forward reads, now `layout_newer`."          (docs/FIXED-FORM-ALGORITHM.md:971-972)
//
// and the sentence that bounds it:
//
//   "Every hostile check on a KNOWN layout's records stays (§4.5, §4.6)."
//                                                     (docs/FIXED-FORM-ALGORITHM.md:972-973)
//
// THE FOUR RETIRED CLAUSES, and the row of the versioning law each is about:
//
//   1. the count clamp across bounds        → array_bounded_grow, [..4] → [..8]
//   2. the range clamp across versions      → range_widen, int32 0..100 → 0..200
//   3. the remap of an unknown variant      → enum_append, Tier +Platinum
//                                             (an ordinal the OLD reader does not
//                                              name would have landed `None`)
//   4. the drop-and-count of an unknown field → field_append, +w
//
// Each is "now `layout_newer`": the OLD reader, handed a file the NEW writer
// wrote, must REFUSE with `layout_newer` — the file's hash is in no lineage the
// OLD build ever locked — BEFORE the plan is entered: no count is clamped, no
// range is clamped, no variant is remapped to `None`, no field is dropped and
// counted, and the four §4 counters stay exactly zero. A reader that still
// walked the newer file would move `clamped` (1, 2) or `unknown` (4) and land a
// partial value; this file is red on exactly that.
//
// THE VECTOR IS THE NEW GENERATION'S OWN WRITER. `<New>::...FixedSave` lays
// down a lawful NEW form-3 file (a full `[..8]` count, a 150 in the newer
// range, `Platinum`, and the appended `w`); `<Old>::...FixedLoad` reads it. The
// header's hash is the NEW layout's, so the OLD reader's one-entry lineage
// cannot hold it and the answer is `layout_newer` (docs/FIXED-FORM-ALGORITHM.md
// §5.3, §5.7's OLD-REFUSES-NEW column). Nothing here reads
// build/fixedform-corpus: the file is self-contained.
//
// RUN (from ./repo), the standalone shape rows/R16.cpp and rows/R20.cpp use —
// the generated units are header-only, so the -I paths are the whole recipe:
//
//   c++ -std=c++17 -Wall \
//       -Ibuild/tables-generated/vnum \
//       -Ibuild/tables-generated/vold_enum_append -Ibuild/tables-generated/vnew_enum_append \
//       -Ibuild/tables-generated/vold_field_append -Ibuild/tables-generated/vnew_field_append \
//       test/conformance/cpp/rows/R15.cpp -o build/rows-cpp-R15 && ./build/rows-cpp-R15
//
// One printed line per assertion; exit 0 when every line passes, exit 1 when
// one does not.

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
#include "VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"

static int g_failures = 0;

static void check( bool ok, const char * what )
{
    if ( ok ) { std::printf( "ok   %s\n", what ); }
    else      { std::printf( "FAIL %s\n", what ); ++g_failures; }
}

// the hash the writer stamped into the header, read as bytes and not as an
// enum constant, so the assertion is that the report carries THE FILE's hash.
static uint64_t le64( const uint8_t * p )
{
    uint64_t v = 0;
    for ( int i = 0; i < 8; ++i ) { v |= (uint64_t) p[i] << ( 8 * i ); }
    return v;
}

// Every clause below owes the same four facts about the refusal. The caller
// supplies the verdict compare because each generated unit spells its own
// TableMessageReason in its own namespace.
template <typename Report>
static void expect_layout_newer( const Report & r, bool reason_is_newer,
                                 uint64_t file_hash, const char * label )
{
    char buf[256];
    std::snprintf( buf, sizeof( buf ), "%s: the refusal is layout_newer BY NAME", label );
    check( reason_is_newer, buf );
    std::snprintf( buf, sizeof( buf ), "%s: the report carries THE FILE's hash 0x%016llx",
                   label, (unsigned long long) file_hash );
    check( r.layout_hash == file_hash, buf );
    std::snprintf( buf, sizeof( buf ), "%s: refused is set and malformed is not", label );
    check( r.refused && !r.malformed, buf );
    std::snprintf( buf, sizeof( buf ),
                   "%s: the four §4 counters and duplicate all stay at zero — a forward read that "
                   "clamped, remapped or dropped would move one", label );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
           buf );
    std::snprintf( buf, sizeof( buf ), "%s: retention moved nothing either", label );
    check( r.retained == 0 && r.retain_lost == 0, buf );
}

// ---------------------------------------------------------------------------
// clause 1 — THE COUNT CLAMP ACROSS BOUNDS: [..4] → [..8].
// A NEW record with a FULL eight-element count. The retired forward read would
// clamp the count to the OLD bound of 4 and count `clamped`; the law says the
// OLD reader refuses layout_newer instead and the count is never touched.

static void count_clamp_is_layout_newer( void )
{
    static uint8_t buf[1 << 16];
    vnew_array_bounded_grow::ArrayBoundedGrow nv;
    vnew_array_bounded_grow::ArrayBoundedGrowReset( nv );
    nv.lead = 0xAABBCCDDu;
    nv.vals_count = 8;                       // the NEW bound, past the OLD 4
    for ( int k = 0; k < 8; ++k ) { nv.vals[k] = 1000 + k; }
    nv.trail = 0x11223344u;

    const int64_t len = vnew_array_bounded_grow::ArrayBoundedGrowFixedMeasure( 1 );
    check( vnew_array_bounded_grow::ArrayBoundedGrowFixedSave( &nv, 1, buf, (int64_t) sizeof( buf ) ) == len,
           "count_clamp: the NEW writer wrote its full-count record" );

    const uint64_t file_hash = le64( buf + vold_array_bounded_grow::kTableFixedHashAt );
    check( file_hash == vnew_array_bounded_grow::ArrayBoundedGrowFixedHash,
           "count_clamp: the file carries the NEW layout's hash, not the OLD's" );

    // PRE-POISON the OLD destination, so a reader that walked the newer count
    // and clamped would leave its mark here.
    vold_array_bounded_grow::ArrayBoundedGrow back;
    back.lead = 0xDEADBEEFu;
    back.vals_count = 77;
    back.trail = 0xFEEDFACEu;
    for ( int k = 0; k < 4; ++k ) { back.vals[k] = 0x7F7F7F7F; }

    vold_array_bounded_grow::TableReport r;
    static vold_array_bounded_grow::TableFixedEntry plan[256];
    const int64_t n = vold_array_bounded_grow::ArrayBoundedGrowFixedLoad(
        &back, 1, buf, len, plan, 256, NULL, &r );

    check( n == -1, "count_clamp: the OLD reader refuses the NEW file (n == -1), it does not read it" );
    expect_layout_newer( r, r.reason == vold_array_bounded_grow::layout_newer, file_hash, "count_clamp" );
    check( back.lead == 0xDEADBEEFu && back.vals_count == 77 && back.trail == 0xFEEDFACEu &&
           back.vals[0] == 0x7F7F7F7F,
           "count_clamp: the count was NOT clamped and no value was landed — the destination is untouched" );
}

// ---------------------------------------------------------------------------
// clause 2 — THE RANGE CLAMP ACROSS VERSIONS: int32 0..100 → 0..200.
// A NEW record whose value sits in the NEW range but past the OLD end. The
// retired forward read would clamp 150 to 100 and count `clamped`.

static void range_clamp_is_layout_newer( void )
{
    static uint8_t buf[1 << 16];
    vnew_range_widen::RangeWiden nv;
    vnew_range_widen::RangeWidenReset( nv );
    nv.lead = 1;
    nv.v = 150;                              // inside 0..200, past the OLD 100
    nv.trail = 2;

    const int64_t len = vnew_range_widen::RangeWidenFixedMeasure( 1 );
    check( vnew_range_widen::RangeWidenFixedSave( &nv, 1, buf, (int64_t) sizeof( buf ) ) == len,
           "range_clamp: the NEW writer wrote its out-of-OLD-range record" );

    const uint64_t file_hash = le64( buf + vold_range_widen::kTableFixedHashAt );
    check( file_hash == vnew_range_widen::RangeWidenFixedHash,
           "range_clamp: the file carries the NEW layout's hash, not the OLD's" );

    vold_range_widen::RangeWiden back;
    back.lead = 0xDEADBEEFu;
    back.v = 0x7F7F7F7F;
    back.trail = 0xFEEDFACEu;

    vold_range_widen::TableReport r;
    static vold_range_widen::TableFixedEntry plan[256];
    const int64_t n = vold_range_widen::RangeWidenFixedLoad( &back, 1, buf, len, plan, 256, NULL, &r );

    check( n == -1, "range_clamp: the OLD reader refuses the NEW file (n == -1), it does not read it" );
    expect_layout_newer( r, r.reason == vold_range_widen::layout_newer, file_hash, "range_clamp" );
    check( back.lead == 0xDEADBEEFu && back.v == 0x7F7F7F7F && back.trail == 0xFEEDFACEu,
           "range_clamp: the value was NOT clamped to the OLD end and nothing landed — the destination is untouched" );
}

// ---------------------------------------------------------------------------
// clause 3 — THE REMAP OF AN UNKNOWN VARIANT TO None: Tier +Platinum.
// A NEW record whose ordinal is Platinum, a variant the OLD reader has no name
// for. The retired forward read would land `None` and count `clamped`.

static void unknown_variant_remap_is_layout_newer( void )
{
    static uint8_t buf[1 << 16];
    vnew_enum_append::Lineage nv;
    vnew_enum_append::LineageReset( nv );
    nv.tier = vnew_enum_append::Tier::Platinum; // one past the OLD Gold
    nv.seq = 9;

    const int64_t len = vnew_enum_append::LineageFixedMeasure( 1 );
    check( vnew_enum_append::LineageFixedSave( &nv, 1, buf, (int64_t) sizeof( buf ) ) == len,
           "unknown_variant: the NEW writer wrote its appended-variant record" );

    const uint64_t file_hash = le64( buf + vold_enum_append::kTableFixedHashAt );
    check( file_hash == vnew_enum_append::LineageFixedHash,
           "unknown_variant: the file carries the NEW layout's hash, not the OLD's" );

    vold_enum_append::Lineage back;
    back.tier = vold_enum_append::Tier::Gold;  // a name the OLD reader does have
    back.seq = 0x7F7F7F7F;

    vold_enum_append::TableReport r;
    static vold_enum_append::TableFixedEntry plan[256];
    const int64_t n = vold_enum_append::LineageFixedLoad( &back, 1, buf, len, plan, 256, NULL, &r );

    check( n == -1, "unknown_variant: the OLD reader refuses the NEW file (n == -1), it does not read it" );
    expect_layout_newer( r, r.reason == vold_enum_append::layout_newer, file_hash, "unknown_variant" );
    check( back.tier == vold_enum_append::Tier::Gold && back.seq == 0x7F7F7F7F,
           "unknown_variant: the ordinal was NOT remapped to None and no value landed — the destination is untouched" );
}

// ---------------------------------------------------------------------------
// clause 4 — THE DROP-AND-COUNT OF AN UNKNOWN FIELD: Lineage +w.
// A NEW record carrying the appended `w`. The retired forward read would drop
// `w` and count `unknown` once per peer; the law says the OLD reader refuses
// layout_newer and `unknown` stays at zero.

static void unknown_field_drop_is_layout_newer( void )
{
    static uint8_t buf[1 << 16];
    vnew_field_append::Lineage nv;
    vnew_field_append::LineageReset( nv );
    nv.x = 11; nv.y = 22; nv.z = 33; nv.w = 99;

    const int64_t len = vnew_field_append::LineageFixedMeasure( 1 );
    check( vnew_field_append::LineageFixedSave( &nv, 1, buf, (int64_t) sizeof( buf ) ) == len,
           "unknown_field: the NEW writer wrote its appended-field record" );

    const uint64_t file_hash = le64( buf + vold_field_append::kTableFixedHashAt );
    check( file_hash == vnew_field_append::LineageFixedHash,
           "unknown_field: the file carries the NEW layout's hash, not the OLD's" );

    vold_field_append::Lineage back;
    back.x = 101; back.y = 102; back.z = 103;

    vold_field_append::TableReport r;
    static vold_field_append::TableFixedEntry plan[256];
    const int64_t n = vold_field_append::LineageFixedLoad( &back, 1, buf, len, plan, 256, NULL, &r );

    check( n == -1, "unknown_field: the OLD reader refuses the NEW file (n == -1), it does not read it" );
    expect_layout_newer( r, r.reason == vold_field_append::layout_newer, file_hash, "unknown_field" );
    check( back.x == 101 && back.y == 102 && back.z == 103,
           "unknown_field: w was NOT dropped-and-counted and nothing landed — the destination is untouched" );
}

int main( void )
{
    count_clamp_is_layout_newer();
    range_clamp_is_layout_newer();
    unknown_variant_remap_is_layout_newer();
    unknown_field_drop_is_layout_newer();

    if ( g_failures != 0 )
    {
        std::printf( "R15: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "R15: the four forward-read clamps are retired — each is layout_newer now\n" );
    return 0;
}
