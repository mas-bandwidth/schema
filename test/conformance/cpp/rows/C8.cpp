// C8: fixed-point F-shift / bits(N) — the C++ conformance guard for schema
// matrix cell cpp/C8.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:351-352):
//
//   A fixed-point field's bounds are in VALUE UNITS and its storage is raw,
//   so both ends are shifted by `F` first; a `bits(N)` is bound by `2^N - 1`.
//
// THE LAW MOVED (docs/rulings.json strict-only-read; docs/plans/strict-only.md
// decision 1): no read clamps. A value past the bound refuses the WHOLE load
// `out_of_range` (the retired `clamped`, renamed in place) and lands nothing.
// The row once asserted "clamps to the bound, clamped == 1"; it now asserts
// the refusal, as test/conformance/go/rows/C8_test.go does. The writers'
// bounds are debug asserts, so the past-the-bound vectors are FORGED on the
// wire after a lawful save.
//
// Also (docs/FIXED-FORM-ALGORITHM.md:81-84): the widening ladder runs inside
// a family and upward only — `20..24` for signed fixed(I,F), `25..29` for
// unsigned ufixed(I,F). When I grows and F stays equal, the raw scaled value
// must land exactly (the scale does not move).
//
// This test asserts two clauses:
//
//  1. fixed(I,F) F-shift: for fixed(4,4) -> fixed(12,4) with F equal, the raw
//     scaled value lands exactly. The bounds pass shifts the declared min/max
//     by F before comparing against raw storage, so a value at the declared
//     boundary (value 7 → raw 112) lands, and raw 113 refuses out_of_range.
//
//  2. bits(N) bound: for bits(8) -> bits(12), the value lands exactly; a
//     value past 2^N - 1 refuses out_of_range.
//
// Run: c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//        -Ibuild/tables-generated/vnum -I../serialize
//        test/conformance/cpp/rows/C8.cpp -o build/rows-cpp-C8
//      && ./build/rows-cpp-C8
// Exit 0 green, exit 1 red; one printed line per assertion.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_fixed_I_growTable.h"
#include "VNEW_fixed_I_growTable.h"
#include "VOLD_bits_growTable.h"
#include "VNEW_bits_growTable.h"

static int g_failures = 0;

static void check( bool ok, const char * what )
{
    if ( ok ) { std::printf( "ok   %s\n", what ); }
    else      { std::printf( "FAIL %s\n", what ); ++g_failures; }
}

// one record, saved by the WRITER generation
template <typename T, typename Measure, typename Save>
static std::vector<uint8_t> one( const T & v, Measure measure, Save save )
{
    std::vector<uint8_t> out( (size_t) measure( 1 ) );
    check( save( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(),
           "one: the writer saved one record" );
    return out;
}

// ---- clause 1: fixed(I,F) F-shift — raw scaled value lands exactly ---------
//
// fixed(4,4) -> fixed(12,4), F=4 equal. The raw scaled value -1 in int8 must
// sign-extend to -1 in int16 without loss. The value 7 in value units is raw
// 112 (= 7 * 2^4); the bounds pass must shift the declared max by F before
// comparing, so 112 must NOT be clamped.

static void fixed_f_shift_exact( void )
{
    // (a) the widening: v = -1 (raw) lands exactly
    {
        vold_fixed_i_grow::FixedIGrow old;
        vold_fixed_i_grow::FixedIGrowReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = -1; // raw scaled value; the one a zero-extension gets wrong
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_fixed_i_grow::FixedIGrowFixedMeasure,
                                       vold_fixed_i_grow::FixedIGrowFixedSave );

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::TableReport r;
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 4096 );
        check( vnew_fixed_i_grow::FixedIGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == 1,
               "C8/fixed-f-shift/widen: the OLD file reads one record" );
        check( back.v == -1,
               "C8/fixed-f-shift/widen: the raw scaled value -1 lands exactly (F equal, sign-extended)" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "C8/fixed-f-shift/widen: lead and trail bracket the row exactly" );
        // strict-only-read: a kind mismatch, an out-of-range value and a repeat now refuse the
        // whole load (no counter), so the retired zero counts read as not refused, not damaged.
        check( r.unknown == 0 && !r.damaged && !r.refused,
               "C8/fixed-f-shift/widen: no counter moved" );
    }

    // (b) the boundary: value 7 (raw 112) is AT the declared max — must land
    //     without clamping. The F-shift means the bounds pass compares the raw
    //     value against max*2^F = 7*16 = 112, NOT against 7 in value units.
    {
        vold_fixed_i_grow::FixedIGrow old;
        vold_fixed_i_grow::FixedIGrowReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = 112; // raw: value 7.0, at the declared max
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_fixed_i_grow::FixedIGrowFixedMeasure,
                                       vold_fixed_i_grow::FixedIGrowFixedSave );

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::TableReport r;
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 4096 );
        check( vnew_fixed_i_grow::FixedIGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == 1,
               "C8/fixed-f-shift/boundary: the OLD file reads one record" );
        check( back.v == 112,
               "C8/fixed-f-shift/boundary: raw 112 (value 7) at the declared max lands without clamping — F-shift" );
        // strict-only-read: was "clamped == 0"; at the bound the load neither refuses nor damages
        check( !r.refused && !r.damaged,
               "C8/fixed-f-shift/boundary: not refused — the F-shifted bound is max*2^F = 112, not max = 7" );
    }

    // (c) past the bound: raw 113 (value 7.0625) is PAST the declared max —
    //     the whole load refuses out_of_range (it once clamped to 112).
    {
        vold_fixed_i_grow::FixedIGrow old;
        vold_fixed_i_grow::FixedIGrowReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = 112; // the lawful raw max; forged to 113 on the wire below
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_fixed_i_grow::FixedIGrowFixedMeasure,
                                       vold_fixed_i_grow::FixedIGrowFixedSave );
        // v is the int8 at body +4 (VOLD FixedIGrowFixedWriteBody)
        ob[ (size_t) vold_fixed_i_grow::kTableFixedHeaderBytes + 4
            + (size_t) vold_fixed_i_grow::FixedIGrowFixedLayoutBytes + 8 + 4 ] = 113;

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::TableReport r;
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 4096 );
        check( vnew_fixed_i_grow::FixedIGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == -1,
               "C8/fixed-f-shift/past: raw 113 past max*2^F refuses the whole load" );
        // strict-only-read: was "v == 112 (clamped to max*2^F), clamped == 1"
        check( r.refused && !r.damaged && r.refuse_reason == vnew_fixed_i_grow::out_of_range,
               "C8/fixed-f-shift/past: the refusal is out_of_range (the retired clamped)" );
        vnew_fixed_i_grow::FixedIGrow fresh;
        vnew_fixed_i_grow::FixedIGrowReset( fresh );
        check( back.v == fresh.v && back.lead == fresh.lead && back.trail == fresh.trail,
               "C8/fixed-f-shift/past: a refused load lands nothing (declared defaults)" );
    }
}

// ---- clause 2: bits(N) — value lands exactly, clamps to 2^N - 1 -----------
//
// bits(8) -> bits(12): the value 0xFF (every bit of bits(8)) lands in bits(12)
// exactly. The implied range of bits(12) is [0, 2^12 - 1] = [0, 4095].

static void bits_n_exact( void )
{
    // (a) widening: 0xFF lands exactly
    {
        vold_bits_grow::BitsGrow old;
        vold_bits_grow::BitsGrowReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = 0xFFu; // every bit of bits(8)
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_bits_grow::BitsGrowFixedMeasure,
                                       vold_bits_grow::BitsGrowFixedSave );

        vnew_bits_grow::BitsGrow back;
        vnew_bits_grow::BitsGrowReset( back );
        vnew_bits_grow::TableReport r;
        std::vector<vnew_bits_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bits_grow::BitsGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
               "C8/bits-n/widen: the OLD file reads one record" );
        check( back.v == 0xFFu,
               "C8/bits-n/widen: every bit of bits(8) lands in bits(12), exact" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "C8/bits-n/widen: lead and trail bracket the row exactly" );
        // strict-only-read: a kind mismatch, an out-of-range value and a repeat now refuse the
        // whole load (no counter), so the retired zero counts read as not refused, not damaged.
        check( r.unknown == 0 && !r.damaged && !r.refused,
               "C8/bits-n/widen: no counter moved" );
    }

    // (b) boundary: bits(12) value 4095 (2^12 - 1) is the implied max — must
    //     land without clamping on the identity plan.
    {
        vnew_bits_grow::BitsGrow rec;
        vnew_bits_grow::BitsGrowReset( rec );
        rec.lead = 0xAAAAAAAAu;
        rec.v = 4095u; // 2^12 - 1, the implied max of bits(12)
        rec.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> buf( (size_t) vnew_bits_grow::BitsGrowFixedMeasure( 1 ) );
        check( vnew_bits_grow::BitsGrowFixedSave( &rec, 1, buf.data(), (int64_t) buf.size() ) == (int64_t) buf.size(),
               "C8/bits-n/boundary: the NEW writer saved one record" );

        vnew_bits_grow::BitsGrow back;
        vnew_bits_grow::BitsGrowReset( back );
        vnew_bits_grow::TableReport r;
        std::vector<vnew_bits_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bits_grow::BitsGrowFixedLoad( &back, 1, buf.data(), (int64_t) buf.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
               "C8/bits-n/boundary: identity read one record" );
        check( back.v == 4095u,
               "C8/bits-n/boundary: bits(12) value 4095 (= 2^12-1) lands without clamping" );
        // strict-only-read: was "clamped == 0"
        check( !r.refused && !r.damaged,
               "C8/bits-n/boundary: not refused at the implied max" );
    }

    // (c) past the bound: bits(12) value 4096 (2^12) is PAST the implied max —
    //     the whole load refuses out_of_range (it once clamped to 4095).
    {
        vnew_bits_grow::BitsGrow rec;
        vnew_bits_grow::BitsGrowReset( rec );
        rec.lead = 0xAAAAAAAAu;
        rec.v = 4095u; // the lawful max; forged to 4096 on the wire below
        rec.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> buf( (size_t) vnew_bits_grow::BitsGrowFixedMeasure( 1 ) );
        check( vnew_bits_grow::BitsGrowFixedSave( &rec, 1, buf.data(), (int64_t) buf.size() ) == (int64_t) buf.size(),
               "C8/bits-n/past: the NEW writer saved one record" );
        // v is the uint32 at body +4 (VNEW BitsGrowFixedWriteBody)
        vnew_bits_grow::TableFixedPut32( buf.data() + vnew_bits_grow::kTableFixedHeaderBytes + 4
            + vnew_bits_grow::BitsGrowFixedLayoutBytes + 8 + 4, 4096u );

        vnew_bits_grow::BitsGrow back;
        vnew_bits_grow::BitsGrowReset( back );
        vnew_bits_grow::TableReport r;
        std::vector<vnew_bits_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bits_grow::BitsGrowFixedLoad( &back, 1, buf.data(), (int64_t) buf.size(),
                                                   plan.data(), 4096, NULL, &r ) == -1,
               "C8/bits-n/past: bits(12) value 4096 refuses the whole load" );
        // strict-only-read: was "v == 4095 (clamped to 2^12-1), clamped == 1"
        check( r.refused && !r.damaged && r.refuse_reason == vnew_bits_grow::out_of_range,
               "C8/bits-n/past: the refusal is out_of_range (the retired clamped)" );
        vnew_bits_grow::BitsGrow fresh;
        vnew_bits_grow::BitsGrowReset( fresh );
        check( back.v == fresh.v && back.lead == fresh.lead && back.trail == fresh.trail,
               "C8/bits-n/past: a refused load lands nothing (declared defaults)" );
    }
}

int main( void )
{
    fixed_f_shift_exact();
    bits_n_exact();

    if ( g_failures != 0 )
    {
        std::printf( "C8: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "C8: fixed-point F-shift / bits(N) — all assertions passed\n" );
    return 0;
}
