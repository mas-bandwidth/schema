// THE NUMBERS COLUMN OF THE FIXED FORM'S VERSIONING LAW
// (docs/FIXED-FORM-VERSIONING-TESTS.md, docs/FIXED-FORM-BILL-READS-BACKWARD.md,
// docs/FIXED-FORM-ALGORITHM.md §5).
//
// Glenn: "Let the tests guide us in C++ to the correct implementation." So the
// tests are here BEFORE the implementation, and most of them are RED on purpose.
//
// The rows this file carries are the NUMBERS — every definition whose widening
// is a number growing — plus the floor and the hash table:
//
//   array_bounded_grow  array_fixed_grow  array_elem_widen  constant_grow
//   string_grow  wstring_grow  bytes_grow  int_widen  uint_widen  float_widen
//   range_widen  bits_grow  fixed_I_grow  optional_add
//   floor_at  floor_below  floor_raise_live
//   hash_unknown  hash_known_bytes_differ  hash_identity  lineage_merge
//
// Each row has its own pair of schemas, `test/tables/VOLD_<row>.schema` and
// `test/tables/VNEW_<row>.schema`, differing by EXACTLY that row, same table
// name on both sides in DIFFERENT PACKAGES so both generations compile into one
// binary. Every table brackets the row's field with `lead` and `trail`, so a
// mislaid size moves a neighbour and the equality check sees it.
//
// TWO COLUMNS PER ROW (the other two, LOCK-REFUSES and LOCK-ALLOWS, are Go):
//
//   NEW-READS-OLD   the widened reader reads the older writer's bytes: every
//                   old value lands EXACTLY, the reader's tail is the DEFAULT,
//                   and the counters are what §5.2 says. Mostly GREEN already,
//                   because the runtime compiles a plan from the writer's
//                   layout today. Kept, because it is the half that must never
//                   break.
//   OLD-REFUSES-NEW the older reader given the widened writer's bytes refuses
//                   `layout_newer` BEFORE ANY RECORD, no counter moves, nothing
//                   is decoded. RED, all of it: `layout_newer` does not exist.
//
// HOW RED IS SPELLED HERE. `layout_newer`, `layout_unsupported`, the floor and
// the lineage are not in the emitter, so naming them in C++ would not COMPILE,
// and a target that does not build is a target nobody runs. So every case that
// waits on them goes through `red()`: it prints the case name and the fix it
// waits on, it is COUNTED and printed in a block at the end, and it does not
// fail the target. The discipline is held from both ends — a `red()` that
// starts PASSING is a real FAILURE, because a case that has gone green belongs
// in `check()` as part of landing the fix it names.
//
// THE FLOOR API THIS FILE ASSUMES is written out in full at `floor_cases()`.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

// A REFERENCE DEFECT THIS FILE WORKS AROUND, reported in the PR: a unit whose
// only wide arithmetic is a SIGNED `fixed(I,F)` with a NEGATIVE min emits
// `serialize::uint128_t` in its packet codec and does NOT include serialize.h,
// so VOLD_fixed_I_grow's header does not compile on its own. One include here,
// and the emitter keeps the fix.
#include "serialize.h"

#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_array_fixed_growTable.h"
#include "VNEW_array_fixed_growTable.h"
#include "VOLD_array_elem_widenTable.h"
#include "VNEW_array_elem_widenTable.h"
#include "VOLD_constant_growTable.h"
#include "VNEW_constant_growTable.h"
#include "VOLD_string_growTable.h"
#include "VNEW_string_growTable.h"
#include "VOLD_wstring_growTable.h"
#include "VNEW_wstring_growTable.h"
#include "VOLD_bytes_growTable.h"
#include "VNEW_bytes_growTable.h"
#include "VOLD_int_widenTable.h"
#include "VNEW_int_widenTable.h"
#include "VOLD_uint_widenTable.h"
#include "VNEW_uint_widenTable.h"
#include "VOLD_float_widenTable.h"
#include "VNEW_float_widenTable.h"
#include "VOLD_range_widenTable.h"
#include "VNEW_range_widenTable.h"
#include "VOLD_bits_growTable.h"
#include "VNEW_bits_growTable.h"
#include "VOLD_fixed_I_growTable.h"
#include "VNEW_fixed_I_growTable.h"
#include "VOLD_optional_addTable.h"
#include "VNEW_optional_addTable.h"
#include "VOLD_floorTable.h"
#include "VMID_floorTable.h"
#include "VNEW_floorTable.h"
#include "VOLD_lineage_mergeTable.h"
#include "VBRA_lineage_mergeTable.h"
#include "VBRB_lineage_mergeTable.h"
#include "VNEW_lineage_mergeTable.h"

namespace {

int failures = 0;
int reds = 0;
int reds_gone_green = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// A case that waits on a named fix. RED is the expected answer and costs the
// target nothing; GREEN is a FAILURE, because the fix has landed and the case
// must be promoted to check().
[[maybe_unused]] void red( bool ok, const char * what, const char * waits_on )
{
    if ( !ok ) { std::printf( "RED [%s]: %s\n", waits_on, what ); reds++; return; }
    std::printf( "FAIL: RED case PASSES now, promote it to check(): %s (was waiting on %s)\n", what, waits_on );
    failures++;
    reds_gone_green++;
}

// one record, saved
template <typename T, typename Measure, typename Save>
std::vector<uint8_t> one( const T & v, Measure measure, Save save, const char * what )
{
    std::vector<uint8_t> out( (size_t) measure( 1 ) );
    check( save( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(), what );
    return out;
}

// THE REASON NAMES THE BILL ADDS AND THE EMITTER DOES NOT CARRY (#895, bill §4,
// §6b). One -D each the day they land, and every red below becomes the real
// comparison it already reads as.
#ifdef SCHEMA_HAS_LAYOUT_NEWER
#define IS_LAYOUT_NEWER( ns, r ) ( (r) == ns::layout_newer )
#else
#define IS_LAYOUT_NEWER( ns, r ) ( (void) (r), false )
#endif
#ifdef SCHEMA_HAS_LAYOUT_UNSUPPORTED
#define IS_LAYOUT_UNSUPPORTED( ns, r ) ( (r) == ns::layout_unsupported )
#else
#define IS_LAYOUT_UNSUPPORTED( ns, r ) ( (void) (r), false )
#endif

// OLD-REFUSES-NEW, one shape for every row: the OLD build's reader, the NEW
// build's bytes. `layout_newer` before any record, no counter, nothing decoded.
//
// WHAT THIS SHAPE DOES NOT YET CHECK, and what it will. The refusal above says
// only "newer"; it does not say WHICH layout the file carries. Bill §12.4 puts
// the file's layout hash in the report, so when the floor and the report land
// the green phase adds one more conjunct here: the reported hash equals the NEW
// build's own `TBL##FixedHash`, which is what proves the reader refused because
// it read the writer's hash rather than because it refused everything. The
// assertion below is unchanged until §12.4's report field exists.
#define OLD_REFUSES_NEW( OLDNS, TBL, ROW, newbytes )                                                    \
    do {                                                                                                \
        OLDNS::TBL back;  std::memset( &back, 0, sizeof( back ) );  OLDNS::TBL##Reset( back );           \
        OLDNS::TBL fresh; std::memset( &fresh, 0, sizeof( fresh ) ); OLDNS::TBL##Reset( fresh );         \
        OLDNS::TableReport r;                                                                            \
        std::vector<OLDNS::TableFixedEntry> plan( 4096 );                                                 \
        const int64_t n = OLDNS::TBL##FixedLoad( &back, 1, (newbytes).data(), (int64_t) (newbytes).size(),\
                                                 plan.data(), 4096, NULL, &r );                          \
        const uint64_t file_hash = OLDNS::TableFixedGet64( (newbytes).data() + OLDNS::kTableFixedHashAt ); \
        check( n < 0 && r.refused && IS_LAYOUT_NEWER( OLDNS, r.reason ) &&                               \
             r.layout_hash == file_hash &&                                                               \
             r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed &&\
             std::memcmp( &back, &fresh, sizeof( back ) ) == 0,                                          \
             ROW "/OLD-REFUSES-NEW: layout_newer before any record, no counter, nothing decoded" );      \
    } while ( 0 )

// The row must CHANGE THE LAYOUT HASH, or "older or equal" has nothing to
// decide on and no refusal is reachable at all (bill §6a: "any change to a
// definition that inputs into it changes the hash by itself").
#define ROW_MOVES_THE_HASH( OLDH, NEWH, ROW )                                                            \
    check( (OLDH) != (NEWH), ROW ": the row changes the layout hash" )

// ---------------------------------------------------------------------------
// 1. array_bounded_grow — [..4]int32 -> [..8]int32

void array_bounded_grow_case()
{
    ROW_MOVES_THE_HASH( vold_array_bounded_grow::ArrayBoundedGrowFixedHash,
                        vnew_array_bounded_grow::ArrayBoundedGrowFixedHash, "array_bounded_grow" );

    vold_array_bounded_grow::ArrayBoundedGrow old;
    vold_array_bounded_grow::ArrayBoundedGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.vals_count = 4; // the OLD bound, FULL, which is the only count that reaches the new slack
    for ( int i = 0; i < 4; ++i ) { old.vals[i] = 1000 + i; }
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_array_bounded_grow::ArrayBoundedGrowFixedMeasure,
                                   vold_array_bounded_grow::ArrayBoundedGrowFixedSave, "array_bounded_grow: OLD save" );

    {
        vnew_array_bounded_grow::ArrayBoundedGrow back;
        vnew_array_bounded_grow::ArrayBoundedGrowReset( back );
        vnew_array_bounded_grow::TableReport r;
        std::vector<vnew_array_bounded_grow::TableFixedEntry> plan( 4096 );
        check( vnew_array_bounded_grow::ArrayBoundedGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                                   plan.data(), 4096, NULL, &r ) == 1,
               "array_bounded_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "array_bounded_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.vals_count == 4, "array_bounded_grow/NEW-READS-OLD: the WRITER's count lands, not the reader's bound" );
        bool exact = true;
        for ( int i = 0; i < 4; ++i ) { exact = exact && back.vals[i] == 1000 + i; }
        check( exact, "array_bounded_grow/NEW-READS-OLD: the four written elements, exact" );
        bool slack = true;
        for ( int i = 4; i < 8; ++i ) { slack = slack && back.vals[i] == 0; }
        check( slack, "array_bounded_grow/NEW-READS-OLD: [..N] slack past the count is zeros (§12.6)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "array_bounded_grow/NEW-READS-OLD: a bound that grew moves no counter (§5.2)" );
    }

    vnew_array_bounded_grow::ArrayBoundedGrow nv;
    vnew_array_bounded_grow::ArrayBoundedGrowReset( nv );
    nv.vals_count = 8; // past the OLD bound: the count clamp's cross-version half, retired
    for ( int i = 0; i < 8; ++i ) { nv.vals[i] = 2000 + i; }
    std::vector<uint8_t> nb = one( nv, vnew_array_bounded_grow::ArrayBoundedGrowFixedMeasure,
                                   vnew_array_bounded_grow::ArrayBoundedGrowFixedSave, "array_bounded_grow: NEW save" );
    OLD_REFUSES_NEW( vold_array_bounded_grow, ArrayBoundedGrow, "array_bounded_grow", nb );
}

// A forged count of 7 in the OLD file (writer declared [..4]), read by the
// NEW reader ([..8]). The plan carries the WRITER's bound (bill §12.5): clamp
// to 4, COUNT clamped, never land a count the old writer could not have written.
void array_bounded_grow_hostile_case()
{
    vold_array_bounded_grow::ArrayBoundedGrow old;
    vold_array_bounded_grow::ArrayBoundedGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.vals_count = 4;
    for ( int i = 0; i < 4; ++i ) { old.vals[i] = 1000 + i; }
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_array_bounded_grow::ArrayBoundedGrowFixedMeasure,
                                   vold_array_bounded_grow::ArrayBoundedGrowFixedSave, "array_bounded_grow_hostile: OLD save" );
    const size_t rec = (size_t) vold_array_bounded_grow::kTableFixedHeaderBytes + 4
        + (size_t) vold_array_bounded_grow::ArrayBoundedGrowFixedLayoutBytes;
    vold_array_bounded_grow::TableFixedPut32( ob.data() + rec + 8 + 4, 7u );
    {
        vnew_array_bounded_grow::ArrayBoundedGrow back;
        vnew_array_bounded_grow::ArrayBoundedGrowReset( back );
        vnew_array_bounded_grow::TableReport r;
        std::vector<vnew_array_bounded_grow::TableFixedEntry> plan( 4096 );
        check( vnew_array_bounded_grow::ArrayBoundedGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                                   plan.data(), 4096, NULL, &r ) == 1,
               "array_bounded_grow_hostile: the forged file reads" );
        check( back.vals_count == 4,
               "array_bounded_grow_hostile: a count of 7 clamps to the WRITER's [..4], not the reader's [..8]" );
        check( r.clamped >= 1, "array_bounded_grow_hostile: COUNT clamped" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "array_bounded_grow_hostile: lead and trail stand" );
    }
}

// ---------------------------------------------------------------------------
// 2. array_fixed_grow — [4]Vec -> [8]Vec, and the element's DEFAULTS ARE NONZERO
//
// `Vec { x int32 = 7; y int32 = 9 }`, because that is the only way this row can
// tell the law from an accident. With an int32 element, "slots 4..7 == 0" is
// satisfied by a plain zero fill of the reader's struct just as well as by the
// ELEMENT-DEFAULT prefill the bill requires (§12.6), so the case would pass
// without the behaviour it names ever existing. With x = 7 and y = 9 the two
// answers differ: a zero fill reads 0, §12.6 reads 7 and 9. No element the
// writer sets is ever 7 or 9, so a stale read cannot fake the default either.

void array_fixed_grow_case()
{
    ROW_MOVES_THE_HASH( vold_array_fixed_grow::ArrayFixedGrowFixedHash,
                        vnew_array_fixed_grow::ArrayFixedGrowFixedHash, "array_fixed_grow" );

    vold_array_fixed_grow::ArrayFixedGrow old;
    vold_array_fixed_grow::ArrayFixedGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    for ( int i = 0; i < 4; ++i ) { old.vals[i].x = -500 - i; old.vals[i].y = -600 - i; }
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_array_fixed_grow::ArrayFixedGrowFixedMeasure,
                                   vold_array_fixed_grow::ArrayFixedGrowFixedSave, "array_fixed_grow: OLD save" );
    {
        vnew_array_fixed_grow::ArrayFixedGrow back;
        // Reset before the load only proves the load did not CLOBBER the
        // slack; a zero-fill of the reader's struct would still look like
        // the element's defaults. Poison, then load, then the slack must be
        // 7 and 9 from the plan's prefill (bill §12.6).
        std::memset( &back, 0x5A, sizeof( back ) );
        vnew_array_fixed_grow::TableReport r;
        std::vector<vnew_array_fixed_grow::TableFixedEntry> plan( 4096 );
        check( vnew_array_fixed_grow::ArrayFixedGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                               plan.data(), 4096, NULL, &r ) == 1,
               "array_fixed_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "array_fixed_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        bool exact = true;
        for ( int i = 0; i < 4; ++i )
        {
            exact = exact && back.vals[i].x == -500 - i && back.vals[i].y == -600 - i;
        }
        check( exact, "array_fixed_grow/NEW-READS-OLD: the writer's four slots, exact" );
        // THE ELEMENT'S DEFAULTS, NOT ZERO. 7 and 9 are the only answer §12.6
        // allows; 0 is the zero fill, and this is the case that separates them.
        // It is GREEN on the C++ reference today, so it is a check(), not a red.
        bool slack = true;
        for ( int i = 4; i < 8; ++i )
        {
            slack = slack && back.vals[i].x == 7 && back.vals[i].y == 9;
        }
        check( slack, "array_fixed_grow/NEW-READS-OLD: slots 4..7 hold the ELEMENT's defaults (x = 7, y = 9), not zero (§12.6)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "array_fixed_grow/NEW-READS-OLD: no counter moves" );
    }
    vnew_array_fixed_grow::ArrayFixedGrow nv;
    vnew_array_fixed_grow::ArrayFixedGrowReset( nv );
    for ( int i = 0; i < 8; ++i ) { nv.vals[i].x = 3000 + i; nv.vals[i].y = 4000 + i; }
    std::vector<uint8_t> nb = one( nv, vnew_array_fixed_grow::ArrayFixedGrowFixedMeasure,
                                   vnew_array_fixed_grow::ArrayFixedGrowFixedSave, "array_fixed_grow: NEW save" );
    OLD_REFUSES_NEW( vold_array_fixed_grow, ArrayFixedGrow, "array_fixed_grow", nb );
}

// ---------------------------------------------------------------------------
// 3. array_elem_widen — [..4]int16 -> [..4]int32 (the BOUND does not move)

void array_elem_widen_case()
{
    ROW_MOVES_THE_HASH( vold_array_elem_widen::ArrayElemWidenFixedHash,
                        vnew_array_elem_widen::ArrayElemWidenFixedHash, "array_elem_widen" );

    vold_array_elem_widen::ArrayElemWiden old;
    vold_array_elem_widen::ArrayElemWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.vals_count = 4;
    old.vals[0] = -1;      // the one value a zero-extension gets wrong
    old.vals[1] = INT16_MIN;
    old.vals[2] = INT16_MAX;
    old.vals[3] = 0;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_array_elem_widen::ArrayElemWidenFixedMeasure,
                                   vold_array_elem_widen::ArrayElemWidenFixedSave, "array_elem_widen: OLD save" );
    {
        vnew_array_elem_widen::ArrayElemWiden back;
        vnew_array_elem_widen::ArrayElemWidenReset( back );
        vnew_array_elem_widen::TableReport r;
        std::vector<vnew_array_elem_widen::TableFixedEntry> plan( 4096 );
        check( vnew_array_elem_widen::ArrayElemWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                               plan.data(), 4096, NULL, &r ) == 1,
               "array_elem_widen/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "array_elem_widen/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.vals_count == 4, "array_elem_widen/NEW-READS-OLD: the count is unmoved" );
        check( back.vals[0] == -1, "array_elem_widen/NEW-READS-OLD: -1 SIGN-EXTENDS to -1" );
        check( back.vals[1] == INT16_MIN, "array_elem_widen/NEW-READS-OLD: INT16_MIN lands exactly" );
        check( back.vals[2] == INT16_MAX, "array_elem_widen/NEW-READS-OLD: INT16_MAX lands exactly" );
        check( r.widened > 0, "array_elem_widen/NEW-READS-OLD: a widened element counts (§5.2)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "array_elem_widen/NEW-READS-OLD: nothing else fired" );
    }
    vnew_array_elem_widen::ArrayElemWiden nv;
    vnew_array_elem_widen::ArrayElemWidenReset( nv );
    nv.vals_count = 4;
    for ( int i = 0; i < 4; ++i ) { nv.vals[i] = 100000 + i; } // past int16 outright
    std::vector<uint8_t> nb = one( nv, vnew_array_elem_widen::ArrayElemWidenFixedMeasure,
                                   vnew_array_elem_widen::ArrayElemWidenFixedSave, "array_elem_widen: NEW save" );
    OLD_REFUSES_NEW( vold_array_elem_widen, ArrayElemWiden, "array_elem_widen", nb );
}

// ---------------------------------------------------------------------------
// 4. constant_grow — const N = 4 -> 8 behind [..N]. The LOCK records the
//    EVALUATED bound, so on the wire this is array_bounded_grow exactly.

void constant_grow_case()
{
    ROW_MOVES_THE_HASH( vold_constant_grow::ConstantGrowFixedHash,
                        vnew_constant_grow::ConstantGrowFixedHash, "constant_grow" );

    vold_constant_grow::ConstantGrow old;
    vold_constant_grow::ConstantGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.vals_count = 4;
    for ( int i = 0; i < 4; ++i ) { old.vals[i] = 7000 + i; }
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_constant_grow::ConstantGrowFixedMeasure,
                                   vold_constant_grow::ConstantGrowFixedSave, "constant_grow: OLD save" );
    {
        vnew_constant_grow::ConstantGrow back;
        vnew_constant_grow::ConstantGrowReset( back );
        vnew_constant_grow::TableReport r;
        std::vector<vnew_constant_grow::TableFixedEntry> plan( 4096 );
        check( vnew_constant_grow::ConstantGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                          plan.data(), 4096, NULL, &r ) == 1,
               "constant_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "constant_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.vals_count == 4, "constant_grow/NEW-READS-OLD: the writer's count" );
        bool exact = true;
        for ( int i = 0; i < 4; ++i ) { exact = exact && back.vals[i] == 7000 + i; }
        check( exact, "constant_grow/NEW-READS-OLD: the four elements, exact" );
        bool slack = true;
        for ( int i = 4; i < 8; ++i ) { slack = slack && back.vals[i] == 0; }
        check( slack, "constant_grow/NEW-READS-OLD: the slots the constant bought take the default" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "constant_grow/NEW-READS-OLD: no counter moves" );
    }
    vnew_constant_grow::ConstantGrow nv;
    vnew_constant_grow::ConstantGrowReset( nv );
    nv.vals_count = 8;
    for ( int i = 0; i < 8; ++i ) { nv.vals[i] = 8000 + i; }
    std::vector<uint8_t> nb = one( nv, vnew_constant_grow::ConstantGrowFixedMeasure,
                                   vnew_constant_grow::ConstantGrowFixedSave, "constant_grow: NEW save" );
    OLD_REFUSES_NEW( vold_constant_grow, ConstantGrow, "constant_grow", nb );
}

// ---------------------------------------------------------------------------
// 5. string_grow — string(8) -> string(16)

void string_grow_case()
{
    ROW_MOVES_THE_HASH( vold_string_grow::StringGrowFixedHash,
                        vnew_string_grow::StringGrowFixedHash, "string_grow" );

    vold_string_grow::StringGrow old;
    vold_string_grow::StringGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    std::strcpy( old.text, "abcdefgh" ); // the OLD capacity, FULL
    old.text_length = 8;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_string_grow::StringGrowFixedMeasure,
                                   vold_string_grow::StringGrowFixedSave, "string_grow: OLD save" );
    {
        vnew_string_grow::StringGrow back;
        vnew_string_grow::StringGrowReset( back );
        vnew_string_grow::TableReport r;
        std::vector<vnew_string_grow::TableFixedEntry> plan( 4096 );
        check( vnew_string_grow::StringGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                      plan.data(), 4096, NULL, &r ) == 1,
               "string_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "string_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.text_length == 8 && std::strcmp( back.text, "abcdefgh" ) == 0,
               "string_grow/NEW-READS-OLD: the length and the bytes, exact" );
        bool slack = true;
        for ( int i = 8; i < 16; ++i ) { slack = slack && back.text[i] == 0; }
        check( slack, "string_grow/NEW-READS-OLD: the capacity the grow bought is ZERO (the slack rule)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "string_grow/NEW-READS-OLD: no counter moves" );
    }
    vnew_string_grow::StringGrow nv;
    vnew_string_grow::StringGrowReset( nv );
    std::strcpy( nv.text, "0123456789abcdef" ); // past the OLD capacity outright
    nv.text_length = 16;
    std::vector<uint8_t> nb = one( nv, vnew_string_grow::StringGrowFixedMeasure,
                                   vnew_string_grow::StringGrowFixedSave, "string_grow: NEW save" );
    OLD_REFUSES_NEW( vold_string_grow, StringGrow, "string_grow", nb );
}

// ---------------------------------------------------------------------------
// 6. wstring_grow — wstring(8) -> wstring(16), the length in CODE UNITS

void wstring_grow_case()
{
    ROW_MOVES_THE_HASH( vold_wstring_grow::WstringGrowFixedHash,
                        vnew_wstring_grow::WstringGrowFixedHash, "wstring_grow" );

    const char16_t src[8] = { u'h', u'e', u'l', u'l', u'o', 0xD83D, 0xDE00, u'￿' };
    vold_wstring_grow::WstringGrow old;
    vold_wstring_grow::WstringGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    std::memcpy( old.text, src, sizeof( src ) );
    old.text_length = 8; // EIGHT CODE UNITS, a surrogate pair counting two
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_wstring_grow::WstringGrowFixedMeasure,
                                   vold_wstring_grow::WstringGrowFixedSave, "wstring_grow: OLD save" );
    {
        vnew_wstring_grow::WstringGrow back;
        vnew_wstring_grow::WstringGrowReset( back );
        vnew_wstring_grow::TableReport r;
        std::vector<vnew_wstring_grow::TableFixedEntry> plan( 4096 );
        check( vnew_wstring_grow::WstringGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == 1,
               "wstring_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "wstring_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.text_length == 8, "wstring_grow/NEW-READS-OLD: the length is in CODE UNITS" );
        check( std::memcmp( back.text, src, sizeof( src ) ) == 0,
               "wstring_grow/NEW-READS-OLD: the code units, exact, the surrogate pair included" );
        bool slack = true;
        for ( int i = 8; i < 16; ++i ) { slack = slack && back.text[i] == 0; }
        check( slack, "wstring_grow/NEW-READS-OLD: the code units the grow bought are ZERO" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "wstring_grow/NEW-READS-OLD: no counter moves" );
    }
    vnew_wstring_grow::WstringGrow nv;
    vnew_wstring_grow::WstringGrowReset( nv );
    for ( int i = 0; i < 16; ++i ) { nv.text[i] = (char16_t) ( u'a' + i ); }
    nv.text_length = 16;
    std::vector<uint8_t> nb = one( nv, vnew_wstring_grow::WstringGrowFixedMeasure,
                                   vnew_wstring_grow::WstringGrowFixedSave, "wstring_grow: NEW save" );
    OLD_REFUSES_NEW( vold_wstring_grow, WstringGrow, "wstring_grow", nb );
}

// ---------------------------------------------------------------------------
// 7. bytes_grow — bytes(8) -> bytes(16)

void bytes_grow_case()
{
    ROW_MOVES_THE_HASH( vold_bytes_grow::BytesGrowFixedHash,
                        vnew_bytes_grow::BytesGrowFixedHash, "bytes_grow" );

    vold_bytes_grow::BytesGrow old;
    vold_bytes_grow::BytesGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    for ( int i = 0; i < 8; ++i ) { old.blob[i] = (uint8_t) ( 0xF0 + i ); } // high bytes: a sign error shows
    old.blob_length = 8;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_bytes_grow::BytesGrowFixedMeasure,
                                   vold_bytes_grow::BytesGrowFixedSave, "bytes_grow: OLD save" );
    {
        vnew_bytes_grow::BytesGrow back;
        vnew_bytes_grow::BytesGrowReset( back );
        vnew_bytes_grow::TableReport r;
        std::vector<vnew_bytes_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bytes_grow::BytesGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                    plan.data(), 4096, NULL, &r ) == 1,
               "bytes_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "bytes_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( back.blob_length == 8, "bytes_grow/NEW-READS-OLD: the live length" );
        bool exact = true;
        for ( int i = 0; i < 8; ++i ) { exact = exact && back.blob[i] == (uint8_t) ( 0xF0 + i ); }
        check( exact, "bytes_grow/NEW-READS-OLD: the eight bytes, exact" );
        bool slack = true;
        for ( int i = 8; i < 16; ++i ) { slack = slack && back.blob[i] == 0; }
        check( slack, "bytes_grow/NEW-READS-OLD: the capacity the grow bought is ZERO" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "bytes_grow/NEW-READS-OLD: no counter moves" );
    }
    vnew_bytes_grow::BytesGrow nv;
    vnew_bytes_grow::BytesGrowReset( nv );
    for ( int i = 0; i < 16; ++i ) { nv.blob[i] = (uint8_t) i; }
    nv.blob_length = 16;
    std::vector<uint8_t> nb = one( nv, vnew_bytes_grow::BytesGrowFixedMeasure,
                                   vnew_bytes_grow::BytesGrowFixedSave, "bytes_grow: NEW save" );
    OLD_REFUSES_NEW( vold_bytes_grow, BytesGrow, "bytes_grow", nb );
}

// ---------------------------------------------------------------------------
// 8. int_widen — int16 -> int32, and the three values a widening gets wrong:
//    -1, INT16_MIN, INT16_MAX (zero-extension, a missing sign, an off-by-one)

void int_widen_case()
{
    ROW_MOVES_THE_HASH( vold_int_widen::IntWidenFixedHash,
                        vnew_int_widen::IntWidenFixedHash, "int_widen" );

    const int16_t values[3] = { -1, INT16_MIN, INT16_MAX };
    const char * names[3] = { "int_widen/NEW-READS-OLD: -1 sign-extends to -1",
                              "int_widen/NEW-READS-OLD: INT16_MIN lands exactly",
                              "int_widen/NEW-READS-OLD: INT16_MAX lands exactly" };
    for ( int k = 0; k < 3; ++k )
    {
        vold_int_widen::IntWiden old;
        vold_int_widen::IntWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = values[k];
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_int_widen::IntWidenFixedMeasure,
                                       vold_int_widen::IntWidenFixedSave, "int_widen: OLD save" );
        vnew_int_widen::IntWiden back;
        vnew_int_widen::IntWidenReset( back );
        vnew_int_widen::TableReport r;
        std::vector<vnew_int_widen::TableFixedEntry> plan( 4096 );
        check( vnew_int_widen::IntWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                  plan.data(), 4096, NULL, &r ) == 1,
               "int_widen/NEW-READS-OLD: one record" );
        check( back.v == (int32_t) values[k], names[k] );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "int_widen/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( r.widened == 1, "int_widen/NEW-READS-OLD: `widened` += 1, per record (§5.2)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "int_widen/NEW-READS-OLD: nothing else fired" );
    }

    vnew_int_widen::IntWiden nv;
    vnew_int_widen::IntWidenReset( nv );
    nv.v = 100000; // past int16 outright: the value an old reader must never land
    std::vector<uint8_t> nb = one( nv, vnew_int_widen::IntWidenFixedMeasure,
                                   vnew_int_widen::IntWidenFixedSave, "int_widen: NEW save" );
    OLD_REFUSES_NEW( vold_int_widen, IntWiden, "int_widen", nb );
}

// ---------------------------------------------------------------------------
// 9. uint_widen — uint16 -> uint32, ZERO-extended

void uint_widen_case()
{
    ROW_MOVES_THE_HASH( vold_uint_widen::UintWidenFixedHash,
                        vnew_uint_widen::UintWidenFixedHash, "uint_widen" );

    vold_uint_widen::UintWiden old;
    vold_uint_widen::UintWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = 0xFFFFu; // the top of the old width: a SIGN-extension lands 0xFFFFFFFF here
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_uint_widen::UintWidenFixedMeasure,
                                   vold_uint_widen::UintWidenFixedSave, "uint_widen: OLD save" );
    {
        vnew_uint_widen::UintWiden back;
        vnew_uint_widen::UintWidenReset( back );
        vnew_uint_widen::TableReport r;
        std::vector<vnew_uint_widen::TableFixedEntry> plan( 4096 );
        check( vnew_uint_widen::UintWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                    plan.data(), 4096, NULL, &r ) == 1,
               "uint_widen/NEW-READS-OLD: one record" );
        check( back.v == 0xFFFFu, "uint_widen/NEW-READS-OLD: 0xFFFF ZERO-extends, it does not sign-extend" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "uint_widen/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( r.widened == 1, "uint_widen/NEW-READS-OLD: `widened` += 1" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "uint_widen/NEW-READS-OLD: nothing else fired" );
    }
    vnew_uint_widen::UintWiden nv;
    vnew_uint_widen::UintWidenReset( nv );
    nv.v = 0xDEADBEEFu;
    std::vector<uint8_t> nb = one( nv, vnew_uint_widen::UintWidenFixedMeasure,
                                   vnew_uint_widen::UintWidenFixedSave, "uint_widen: NEW save" );
    OLD_REFUSES_NEW( vold_uint_widen, UintWiden, "uint_widen", nb );
}

// ---------------------------------------------------------------------------
// 10. float_widen — float32 -> float64, BY THE BITS.
//
// THE FU RULING (#895, for #876 to settle): the fixed form's float rung is a
// plain `(double) f`, so a SIGNALLING NaN comes back QUIET and the one payload
// bit that said "signalling" is gone. §4's own TableWidenF32 does the bit
// surgery and keeps it. This case pins WHAT THE REFERENCE DOES TODAY and asserts
// the 22 payload bits BELOW THE QUIET BIT ride exactly — the half no ruling can
// change — so the day the quiet bit is ruled on, one line moves.

uint64_t bits64( double d ) { uint64_t b; std::memcpy( &b, &d, 8 ); return b; }
float f_from( uint32_t b ) { float f; std::memcpy( &f, &b, 4 ); return f; }

void float_widen_case()
{
    ROW_MOVES_THE_HASH( vold_float_widen::FloatWidenFixedHash,
                        vnew_float_widen::FloatWidenFixedHash, "float_widen" );

    // signalling (quiet bit CLEAR), with a payload in the 22 bits below it
    const uint32_t kSignalling = 0x7F8ABCDEu;
    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = f_from( kSignalling );
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave, "float_widen: OLD save" );
    {
        vnew_float_widen::FloatWiden back;
        vnew_float_widen::FloatWidenReset( back );
        vnew_float_widen::TableReport r;
        std::vector<vnew_float_widen::TableFixedEntry> plan( 4096 );
        check( vnew_float_widen::FloatWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                      plan.data(), 4096, NULL, &r ) == 1,
               "float_widen/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "float_widen/NEW-READS-OLD: lead and trail bracket the row exactly" );
        const uint64_t got = bits64( back.v );
        // the sign and the exponent: a NaN stays a NaN of the same sign
        check( ( got >> 63 ) == ( kSignalling >> 31 ), "float_widen/NEW-READS-OLD: the SIGN bit rides" );
        check( ( ( got >> 52 ) & 0x7FFull ) == 0x7FFull, "float_widen/NEW-READS-OLD: a NaN stays a NaN" );
        // THE HALF NO RULING CAN MOVE: the 22 payload bits below f32's quiet
        // bit, where they land in f64 (mantissa bits 51..30 of the double).
        // f32's mantissa bits 22..0 land in f64's 51..29: the quiet bit at 51,
        // the 22 payload bits below it at 50..29.
        const uint32_t payload22 = kSignalling & 0x003FFFFFu;
        check( (uint32_t) ( ( got >> 29 ) & 0x003FFFFFull ) == payload22,
               "float_widen/NEW-READS-OLD: the 22 payload bits BELOW the quiet bit ride exactly (the FU ruling)" );
        check( ( got & 0x1FFFFFFFull ) == 0, "float_widen/NEW-READS-OLD: the 29 bits f32 never had are zero" );
        // AND THE QUIET BIT, pinned to what the reference does TODAY and named
        // so the ruling in #876 moves ONE line.
        const uint64_t quiet_today = 1ull << 51; // `(double) f` sets it; TableWidenF32 would not
        check( ( got & ( 1ull << 51 ) ) == quiet_today,
               "float_widen/NEW-READS-OLD: the QUIET BIT is what the reference does today (#876 to rule)" );
        check( r.widened == 1, "float_widen/NEW-READS-OLD: `widened` += 1" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "float_widen/NEW-READS-OLD: nothing else fired" );
    }
    vnew_float_widen::FloatWiden nv;
    vnew_float_widen::FloatWidenReset( nv );
    nv.v = 1.0e300; // a value no float32 holds at all
    std::vector<uint8_t> nb = one( nv, vnew_float_widen::FloatWidenFixedMeasure,
                                   vnew_float_widen::FloatWidenFixedSave, "float_widen: NEW save" );
    OLD_REFUSES_NEW( vold_float_widen, FloatWiden, "float_widen", nb );
}

// ---------------------------------------------------------------------------
// 11. range_widen — int32 | 0..100 -> | 0..200.
//
// AND THE FINDING THIS ROW EXISTS TO SURFACE: a range is NOT IN THE LAYOUT, so
// the two sides hash IDENTICALLY and an old reader cannot tell the widened
// writer from itself. Bill §6a says "any change to a definition that inputs
// into it changes the hash by itself"; for a range that is FALSE today, and
// `layout_newer` is therefore UNREACHABLE for this row by hash. Both halves are
// red by name below.

void range_widen_case()
{
    // §13 (MERGED) rules that the hash covers a DEFINITIONS DIGEST, not only the
    // byte layout, so a widened range IS an input to it and the hash WILL move.
    // The red is the emitter's today, not the law's: promote this to check() the
    // day the digest lands.
    check( vold_range_widen::RangeWidenFixedHash != vnew_range_widen::RangeWidenFixedHash,
           "range_widen: the row changes the layout hash (§13: the hash covers a definitions digest)" );

    vold_range_widen::RangeWiden old;
    vold_range_widen::RangeWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = 100; // the OLD max, which must land unclamped
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_range_widen::RangeWidenFixedMeasure,
                                   vold_range_widen::RangeWidenFixedSave, "range_widen: OLD save" );
    {
        vnew_range_widen::RangeWiden back;
        vnew_range_widen::RangeWidenReset( back );
        vnew_range_widen::TableReport r;
        std::vector<vnew_range_widen::TableFixedEntry> plan( 4096 );
        check( vnew_range_widen::RangeWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                      plan.data(), 4096, NULL, &r ) == 1,
               "range_widen/NEW-READS-OLD: one record" );
        check( back.v == 100, "range_widen/NEW-READS-OLD: 100 lands" );
        check( r.clamped == 0, "range_widen/NEW-READS-OLD: `clamped` == 0" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "range_widen/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "range_widen/NEW-READS-OLD: nothing else fired" );
    }

    // THE NEW WRITER PUTS 150 ON THE WIRE — inside its range, outside the old
    // reader's. The bill says the old reader refuses the FILE; today it reads
    // the record and CLAMPS, silently as far as any version is concerned.
    vnew_range_widen::RangeWiden nv;
    vnew_range_widen::RangeWidenReset( nv );
    nv.v = 150;
    std::vector<uint8_t> nb = one( nv, vnew_range_widen::RangeWidenFixedMeasure,
                                   vnew_range_widen::RangeWidenFixedSave, "range_widen: NEW save" );
    OLD_REFUSES_NEW( vold_range_widen, RangeWiden, "range_widen", nb );
}

// ---------------------------------------------------------------------------
// 12. bits_grow — bits(8) -> bits(12)

void bits_grow_case()
{
    ROW_MOVES_THE_HASH( vold_bits_grow::BitsGrowFixedHash,
                        vnew_bits_grow::BitsGrowFixedHash, "bits_grow" );

    vold_bits_grow::BitsGrow old;
    vold_bits_grow::BitsGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = 0xFFu; // every bit of the OLD width
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_bits_grow::BitsGrowFixedMeasure,
                                   vold_bits_grow::BitsGrowFixedSave, "bits_grow: OLD save" );
    {
        vnew_bits_grow::BitsGrow back;
        vnew_bits_grow::BitsGrowReset( back );
        vnew_bits_grow::TableReport r;
        std::vector<vnew_bits_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bits_grow::BitsGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                  plan.data(), 4096, NULL, &r ) == 1,
               "bits_grow/NEW-READS-OLD: one record" );
        check( back.v == 0xFFu, "bits_grow/NEW-READS-OLD: every bit of bits(8) lands in bits(12), exact" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "bits_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "bits_grow/NEW-READS-OLD: nothing else fired" );
    }
    vnew_bits_grow::BitsGrow nv;
    vnew_bits_grow::BitsGrowReset( nv );
    nv.v = 0xFFFu; // a value bits(8) cannot hold
    std::vector<uint8_t> nb = one( nv, vnew_bits_grow::BitsGrowFixedMeasure,
                                   vnew_bits_grow::BitsGrowFixedSave, "bits_grow: NEW save" );
    OLD_REFUSES_NEW( vold_bits_grow, BitsGrow, "bits_grow", nb );
}

// ---------------------------------------------------------------------------
// 13. fixed_I_grow — fixed(4,4) -> fixed(12,4). I GROWS, F IS EQUAL: the scale
//     may not move, so the RAW SCALED VALUE is what must land exactly.

void fixed_I_grow_case()
{
    ROW_MOVES_THE_HASH( vold_fixed_i_grow::FixedIGrowFixedHash,
                        vnew_fixed_i_grow::FixedIGrowFixedHash, "fixed_I_grow" );

    vold_fixed_i_grow::FixedIGrow old;
    vold_fixed_i_grow::FixedIGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = -1; // the raw scaled value, and the one a zero-extension gets wrong
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_fixed_i_grow::FixedIGrowFixedMeasure,
                                   vold_fixed_i_grow::FixedIGrowFixedSave, "fixed_I_grow: OLD save" );
    {
        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::TableReport r;
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 4096 );
        check( vnew_fixed_i_grow::FixedIGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                       plan.data(), 4096, NULL, &r ) == 1,
               "fixed_I_grow/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "fixed_I_grow/NEW-READS-OLD: lead and trail bracket the row exactly" );
        // A FINDING: `fixed(I,F)` with I grown is NOT A WIDENING RUNG today. The
        // read comes back kind_mismatch == 1 and the raw scaled value is LOST to
        // the declared default, which is the one outcome bill §2 forbids for a
        // widening. The rung is int8 -> int16 on the raw value and nothing else.
        check( back.v == -1 && r.kind_mismatch == 0,
               "fixed_I_grow/NEW-READS-OLD: the RAW SCALED VALUE lands exactly (F equal)" );
        check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "fixed_I_grow/NEW-READS-OLD: nothing else fired" );
    }
    vnew_fixed_i_grow::FixedIGrow nv;
    vnew_fixed_i_grow::FixedIGrowReset( nv );
    nv.v = 1000; // a raw value fixed(4,4) has no room for
    std::vector<uint8_t> nb = one( nv, vnew_fixed_i_grow::FixedIGrowFixedMeasure,
                                   vnew_fixed_i_grow::FixedIGrowFixedSave, "fixed_I_grow: NEW save" );
    OLD_REFUSES_NEW( vold_fixed_i_grow, FixedIGrow, "fixed_I_grow", nb );
}

// ---------------------------------------------------------------------------
// 14. optional_add — T -> ?T: every old value lands PRESENT

void optional_add_case()
{
    ROW_MOVES_THE_HASH( vold_optional_add::OptionalAddFixedHash,
                        vnew_optional_add::OptionalAddFixedHash, "optional_add" );

    vold_optional_add::OptionalAdd old;
    vold_optional_add::OptionalAddReset( old );
    old.lead = 0xAAAAAAAAu;
    old.link.value = 777;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_optional_add::OptionalAddFixedMeasure,
                                   vold_optional_add::OptionalAddFixedSave, "optional_add: OLD save" );
    {
        vnew_optional_add::OptionalAdd back;
        vnew_optional_add::OptionalAddReset( back );
        vnew_optional_add::TableReport r;
        std::vector<vnew_optional_add::TableFixedEntry> plan( 4096 );
        check( vnew_optional_add::OptionalAddFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == 1,
               "optional_add/NEW-READS-OLD: one record" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "optional_add/NEW-READS-OLD: lead and trail bracket the row exactly" );
        // A FINDING: `T` read into `?T` comes back kind_mismatch == 1, ABSENT, and
        // with the payload at its default. Bill §2 says "an optional: `T` where
        // the reader has `?T` (landed present)"; §5.2's plan row says
        // "?T where x has T: present := 1, then the value". Neither happens.
        check( back.link_present && back.link.value == 777 && r.kind_mismatch == 0,
               "optional_add/NEW-READS-OLD: `present` == 1 and the payload exact (§12.8)" );
        check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "optional_add/NEW-READS-OLD: nothing else fired" );
    }
    // THE OTHER DIRECTION IS THE REFUSAL: `?T` where the reader has `T` — the
    // present byte is a byte the old reader has no room for.
    vnew_optional_add::OptionalAdd nv;
    vnew_optional_add::OptionalAddReset( nv );
    nv.link_present = false; // and ABSENT, which a T cannot represent at all
    nv.link.value = 0;
    std::vector<uint8_t> nb = one( nv, vnew_optional_add::OptionalAddFixedMeasure,
                                   vnew_optional_add::OptionalAddFixedSave, "optional_add: NEW save" );
    OLD_REFUSES_NEW( vold_optional_add, OptionalAdd, "optional_add", nb );
}

// ---------------------------------------------------------------------------
// THE FLOOR (bill §6b, algorithm §5.3).
//
// THE API THIS TEST ASSUMES, and it does not exist — the bill leaves the syntax
// open (§9: on the table, in the lock, or a compiler flag). What the DESIGN
// implies is a floor PER TABLE, fixed AT BUILD TIME, with the lineage shipped as
// static data beside it. In a generated C++ unit, for a fixed table T:
//
//     constexpr int32_t  T##FixedLineageCount;   -- how many layouts the lock knows
//     extern const uint64_t T##FixedLineage[];   -- their hashes, OLDEST FIRST, the
//                                                   reader's own LAST
//     constexpr int32_t  T##FixedFloor;          -- the lowest lineage INDEX this
//                                                   build accepts; 0 = the whole lineage
//     enum reason        layout_unsupported;     -- a hash IN the lineage, BELOW the floor
//
// and, for `floor_raise_live` only, a TEST-ONLY setter, because a floor that can
// only be chosen at build time cannot be raised inside one process:
//
//     void T##FixedSetFloorForTest( int32_t index );   -- under SCHEMA_FIXED_FLOOR_TEST_HOOKS
//
// THE SETTER IS BEHIND ITS OWN DEFINE, not the floor's. `SCHEMA_HAS_FLOOR` says
// this build HAS a floor; `SCHEMA_FIXED_FLOOR_TEST_HOOKS` says this build is a
// TEST and may move it. Only the fixedform test rules in the Makefile pass
// -DSCHEMA_FIXED_FLOOR_TEST_HOOKS, so a shipped unit that has a floor never
// compiles a way to lower it, and the emitter must emit the setter ONLY under
// that define. The call site below is guarded by both.
//
// Whether that setter is the right shape is the question the PR asks: the
// alternative is two BUILDS of the same unit at different floors, which the
// Makefile can do and which costs this test its ability to say "the file that
// read yesterday refuses today" in one place.
//
// Until any of it exists, the three cases below are red by name.

void floor_cases()
{
    // the lineage: three layouts of one table, each appending one field
    vold_floor::Floored v0;
    vold_floor::FlooredReset( v0 );
    v0.a = 11;
    std::vector<uint8_t> b0 = one( v0, vold_floor::FlooredFixedMeasure,
                                   vold_floor::FlooredFixedSave, "floor: lineage entry 0 save" );
    vmid_floor::Floored v1;
    vmid_floor::FlooredReset( v1 );
    v1.a = 11;
    v1.b = 22;
    std::vector<uint8_t> b1 = one( v1, vmid_floor::FlooredFixedMeasure,
                                   vmid_floor::FlooredFixedSave, "floor: lineage entry 1 save" );

    check( vold_floor::FlooredFixedHash != vmid_floor::FlooredFixedHash &&
           vmid_floor::FlooredFixedHash != vnew_floor::FlooredFixedHash,
           "floor: the three lineage entries have three distinct hashes" );

    // floor_at — A FILE AT THE FLOOR READS. With the floor at index 1, entry 1
    // is the oldest supported layout and the reader takes it whole.
    {
        vnew_floor::Floored back;
        vnew_floor::FlooredReset( back );
        vnew_floor::TableReport r;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
        const int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, b1.data(), (int64_t) b1.size(),
                                                        plan.data(), 4096, NULL, &r );
        check( n == 1 && back.a == 11 && back.b == 22 && back.c == 3,
               "floor_at: a file AT the floor reads, and the reader's tail is the default" );
#ifdef SCHEMA_HAS_FLOOR
        check( vnew_floor::FlooredFixedFloor == 1, "floor_at: the floor sits at lineage index 1" );
#else
        check( false, "floor_at: the floor sits at lineage index 1 — there is no floor to sit at" );
#endif
    }

    // floor_below — A FILE ONE BELOW THE FLOOR REFUSES layout_unsupported, no
    // counter, nothing decoded. Today it READS, because there is no floor.
    {
        vnew_floor::Floored back;  std::memset( &back, 0, sizeof( back ) );  vnew_floor::FlooredReset( back );
        vnew_floor::Floored fresh; std::memset( &fresh, 0, sizeof( fresh ) ); vnew_floor::FlooredReset( fresh );
        vnew_floor::TableReport r;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
        const int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, b0.data(), (int64_t) b0.size(),
                                                        plan.data(), 4096, NULL, &r );
        check( n < 0 && r.refused && IS_LAYOUT_UNSUPPORTED( vnew_floor, r.reason ) &&
             r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed &&
             std::memcmp( &back, &fresh, sizeof( back ) ) == 0,
             "floor_below: a file ONE BELOW the floor refuses layout_unsupported, no counter, nothing decoded" );
    }

    // floor_raise_live — THE FLOOR RAISED BY ONE: the file that read yesterday
    // refuses today, BY NAME. Assumes the test-only setter named above.
    {
        vnew_floor::Floored back;
        vnew_floor::FlooredReset( back );
        vnew_floor::TableReport before;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
        const int64_t read_yesterday = vnew_floor::FlooredFixedLoad( &back, 1, b1.data(), (int64_t) b1.size(),
                                                                     plan.data(), 4096, NULL, &before );
        check( read_yesterday == 1, "floor_raise_live: the file read before the floor moved" );
// the floor must exist AND this build must be one that may move it
#if defined( SCHEMA_HAS_FLOOR ) && defined( SCHEMA_FIXED_FLOOR_TEST_HOOKS )
        vnew_floor::FlooredFixedSetFloorForTest( 2 );
        vnew_floor::TableReport after;
        const int64_t today = vnew_floor::FlooredFixedLoad( &back, 1, b1.data(), (int64_t) b1.size(),
                                                            plan.data(), 4096, NULL, &after );
        check( today < 0 && after.refused && IS_LAYOUT_UNSUPPORTED( vnew_floor, after.reason ),
               "floor_raise_live: the same file refuses after the floor is raised by one" );
#else
        check( false, "floor_raise_live: the same file refuses after the floor is raised by one" );
#endif
    }
}

// ---------------------------------------------------------------------------
// THE HASH TABLE (bill §6b, algorithm §5.3): select by hash, never parse a
// stranger.

void hash_cases()
{
    // hash_identity — THE READER'S OWN HASH SELECTS THE IDENTITY PLAN and NO
    // PLAN COMPILER RUNS. Asserted by shape: the cache's compile counter, which
    // is the one symbol that says a plan was built, never moves.
    {
        vnew_floor::Floored own;
        vnew_floor::FlooredReset( own );
        own.a = 1; own.b = 2; own.c = 3;
        std::vector<uint8_t> ob = one( own, vnew_floor::FlooredFixedMeasure,
                                       vnew_floor::FlooredFixedSave, "hash_identity: save" );
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
        std::vector<vnew_floor::TableFixedEntry> slab( (size_t) vnew_floor::kTableFixedPlanCacheCapacity * 4096 );
        vnew_floor::TableFixedPlanCache cache;
        vnew_floor::TableFixedPlanCacheInit( cache, slab.data(), 4096 );
        vnew_floor::Floored back;
        vnew_floor::FlooredReset( back );
        vnew_floor::TableReport r;
        check( vnew_floor::FlooredFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                             plan.data(), 4096, &cache, &r ) == 1, "hash_identity: the own-hash load" );
        check( back.a == 1 && back.b == 2 && back.c == 3, "hash_identity: the identity plan lands every field" );
        check( cache.compiles == 0, "hash_identity: the reader's own hash compiles NOTHING" );
        check( !r.refused && r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0,
               "hash_identity: the identity read moves no counter" );
    }

    // hash_unknown — A HASH IN NO LINEAGE REFUSES layout_newer. The bytes below
    // are a well-formed form-3 file of a DIFFERENT table, so the hash is one
    // this reader's lineage has never held and the layout is not its own.
    {
        vnew_lineage_merge::Merged stranger;
        vnew_lineage_merge::MergedReset( stranger );
        stranger.anchor = 5;
        std::vector<uint8_t> sb = one( stranger, vnew_lineage_merge::MergedFixedMeasure,
                                       vnew_lineage_merge::MergedFixedSave, "hash_unknown: the stranger saves" );
        vnew_floor::Floored back;  std::memset( &back, 0, sizeof( back ) );  vnew_floor::FlooredReset( back );
        vnew_floor::Floored fresh; std::memset( &fresh, 0, sizeof( fresh ) ); vnew_floor::FlooredReset( fresh );
        vnew_floor::TableReport r;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
        const int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, sb.data(), (int64_t) sb.size(),
                                                        plan.data(), 4096, NULL, &r );
        check( n < 0 && r.refused && IS_LAYOUT_NEWER( vnew_floor, r.reason ) &&
             std::memcmp( &back, &fresh, sizeof( back ) ) == 0,
             "hash_unknown: a hash in NO lineage refuses layout_newer, nothing decoded" );
    }

    // hash_known_bytes_differ — A KNOWN HASH WHOSE LAYOUT BYTES DIFFER FROM THE
    // LOCK'S REFUSES layout_malformed. This is the §6b claim that pays for
    // everything: the reader NEVER PARSES A STRANGER'S LAYOUT, so the seven
    // §1.1 malformations, applied to the bytes under a KNOWN hash, all land in
    // ONE answer — `layout_malformed`, "a lie about a known version" — and not
    // in seven names from a validation walk.
    //
    // The file below is the reader's OWN (so its header hash is a hash the
    // lineage knows); each case breaks the layout BYTES and leaves that hash
    // alone. The seven land layout_malformed, one name for a lie about a
    // known version.
    {
        vnew_floor::Floored own;
        vnew_floor::FlooredReset( own );
        std::vector<uint8_t> good = one( own, vnew_floor::FlooredFixedMeasure,
                                         vnew_floor::FlooredFixedSave, "hash_known_bytes_differ: the known file saves" );
        const size_t layout_at = (size_t) vnew_floor::kTableFixedHeaderBytes + 4;
        const size_t entry0 = layout_at + 4;

        struct Break { const char * what; int entry; int field; uint32_t value; };
        // field: 0 = the layout's entry COUNT, 1 = entry.kind, 2 = entry.size,
        //        3 = entry.children
        const Break breaks[7] = {
            { "1 the entry count no longer fits the layout length", -1, 0, 0xFFFFFFFFu },
            { "2 a kind outside the closed set",                     1, 1, 200u },
            { "3 a size its kind does not admit",                    1, 2, 5u },
            { "4 a kind used as its definition does not allow",      0, 1, 14u },
            { "5 the pre-order walk does not close",                 0, 3, 99u },
            { "6 a size past 65536",                                 0, 2, 65537u },
            { "7 a size that would overflow the sum",                1, 2, 0xFFFFFFFFu },
        };
        for ( int k = 0; k < 7; ++k )
        {
            std::vector<uint8_t> f = good;
            if ( breaks[k].entry < 0 )
            {
                const uint32_t count = vnew_floor::TableFixedGet32( f.data() + layout_at );
                vnew_floor::TableFixedPut32( f.data() + layout_at, count + 1u );
            }
            else
            {
                uint8_t * e = f.data() + entry0 + (size_t) breaks[k].entry * 17;
                if ( breaks[k].field == 1 ) { e[8] = (uint8_t) breaks[k].value; }
                else if ( breaks[k].field == 2 ) { vnew_floor::TableFixedPut32( e + 9, breaks[k].value ); }
                else { vnew_floor::TableFixedPut32( e + 13, breaks[k].value ); }
            }
            vnew_floor::Floored back;  std::memset( &back, 0, sizeof( back ) );  vnew_floor::FlooredReset( back );
            vnew_floor::Floored fresh; std::memset( &fresh, 0, sizeof( fresh ) ); vnew_floor::FlooredReset( fresh );
            vnew_floor::TableReport r;
            std::vector<vnew_floor::TableFixedEntry> plan( 4096 );
            const int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, f.data(), (int64_t) f.size(),
                                                            plan.data(), 4096, NULL, &r );
            char what[256];
            std::snprintf( what, sizeof( what ),
                           "hash_known_bytes_differ: §1.1 case %s, under a KNOWN hash, is layout_malformed",
                           breaks[k].what );
            check( n < 0 && r.refused && r.reason == vnew_floor::layout_malformed, what );
            check( n < 0 && r.refused, "hash_known_bytes_differ: broken layout bytes are refused, whatever the name" );
            check( std::memcmp( &back, &fresh, sizeof( back ) ) == 0,
                   "hash_known_bytes_differ: nothing decoded" );
        }
    }
}

// ---------------------------------------------------------------------------
// lineage_merge (bill §8a.1) — two branches append DIFFERENT fields; after the
// merge BOTH pre-merge builds' files read, by the NAME-SUBSET rule and not by
// position. Branch B's `from_b` sits at position 1 in its own layout and at
// position 2 in the merged one, so a reader that matched by POSITION lands
// `from_b` in `from_a` and this case says so.

void lineage_merge_case()
{
    vbra_lineage_merge::Merged a;
    vbra_lineage_merge::MergedReset( a );
    a.anchor = 100;
    a.from_a = 111;
    std::vector<uint8_t> ab = one( a, vbra_lineage_merge::MergedFixedMeasure,
                                   vbra_lineage_merge::MergedFixedSave, "lineage_merge: branch A save" );

    vbrb_lineage_merge::Merged b;
    vbrb_lineage_merge::MergedReset( b );
    b.anchor = 200;
    b.from_b = 222;
    std::vector<uint8_t> bb = one( b, vbrb_lineage_merge::MergedFixedMeasure,
                                   vbrb_lineage_merge::MergedFixedSave, "lineage_merge: branch B save" );

    check( vbra_lineage_merge::MergedFixedHash != vbrb_lineage_merge::MergedFixedHash,
           "lineage_merge: the two branches have two hashes" );

    {
        vnew_lineage_merge::Merged back;
        vnew_lineage_merge::MergedReset( back );
        vnew_lineage_merge::TableReport r;
        std::vector<vnew_lineage_merge::TableFixedEntry> plan( 4096 );
        check( vnew_lineage_merge::MergedFixedLoad( &back, 1, ab.data(), (int64_t) ab.size(),
                                                    plan.data(), 4096, NULL, &r ) == 1,
               "lineage_merge: the merged build reads branch A's file" );
        check( back.anchor == 100 && back.from_a == 111,
               "lineage_merge: branch A's own fields land exactly" );
        check( back.from_b == 20, "lineage_merge: the field branch A never had takes its declared default" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "lineage_merge: branch A's file moves no counter" );
    }
    {
        vnew_lineage_merge::Merged back;
        vnew_lineage_merge::MergedReset( back );
        vnew_lineage_merge::TableReport r;
        std::vector<vnew_lineage_merge::TableFixedEntry> plan( 4096 );
        const int64_t n = vnew_lineage_merge::MergedFixedLoad( &back, 1, bb.data(), (int64_t) bb.size(),
                                                               plan.data(), 4096, NULL, &r );
        check( n == 1, "lineage_merge: the merged build reads branch B's file" );
        // THE POSITION TRAP: `from_b` is entry 1 on the wire and field 2 here.
        check( back.anchor == 200, "lineage_merge: branch B's anchor" );
        check( back.from_b == 222, "lineage_merge: `from_b` lands in from_b, BY NAME and not by position" );
        check( back.from_a == 10, "lineage_merge: `from_a`, which branch B never had, takes its declared default" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "lineage_merge: branch B's file moves no counter" );
    }
}

} // namespace

// ONE REGISTRATION CALL, from test/tables/fixedform_main.cpp. Returns the number
// of real FAILURES; the reds are printed, counted and do not fail the target.
int versioning_numbers_cases()
{
    std::printf( "\n=== the versioning law, the NUMBERS (docs/FIXED-FORM-VERSIONING-TESTS.md) ===\n" );
    array_bounded_grow_case();
    array_bounded_grow_hostile_case();
    array_fixed_grow_case();
    array_elem_widen_case();
    constant_grow_case();
    string_grow_case();
    wstring_grow_case();
    bytes_grow_case();
    int_widen_case();
    uint_widen_case();
    float_widen_case();
    range_widen_case();
    bits_grow_case();
    fixed_I_grow_case();
    optional_add_case();
    floor_cases();
    hash_cases();
    lineage_merge_case();
    std::printf( "versioning numbers: %d RED, each named above with the fix it waits on; %d failure(s)\n",
                 reds, failures );
    if ( reds_gone_green != 0 )
    {
        std::printf( "versioning numbers: %d red case(s) now PASS and must be promoted to check()\n", reds_gone_green );
    }
    return failures;
}
