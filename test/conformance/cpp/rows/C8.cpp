// C8: fixed-point F-shift / bits(N) — the C++ conformance guard for schema
// matrix cell cpp/C8.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:351-352):
//
//   "A RANGED SCALAR clamps to its declared min and max, COUNT clamped.
//    A fixed-point field's bounds are in VALUE UNITS and its storage is raw,
//    so both ends are shifted by `F` first; a `bits(N)` clamps to `2^N - 1`."
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
//     boundary (value 7 → raw 112) lands without clamping.
//
//  2. bits(N) clamping: for bits(8) -> bits(12), the value lands exactly.
//     The bounds pass clamps a value to 2^N - 1.
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
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
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
        check( r.clamped == 0,
               "C8/fixed-f-shift/boundary: clamped == 0 — the F-shifted bound is max*2^F = 112, not max = 7" );
    }

    // (c) the clamp: raw 113 (value 7.0625) is PAST the declared max — must
    //     clamp to 112 (the F-shifted max).
    {
        vold_fixed_i_grow::FixedIGrow old;
        vold_fixed_i_grow::FixedIGrowReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = 113; // raw: value 7.0625, past declared max 7
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_fixed_i_grow::FixedIGrowFixedMeasure,
                                       vold_fixed_i_grow::FixedIGrowFixedSave );

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::TableReport r;
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 4096 );
        check( vnew_fixed_i_grow::FixedIGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                        plan.data(), 4096, NULL, &r ) == 1,
               "C8/fixed-f-shift/clamp: the OLD file reads one record" );
        check( back.v == 112,
               "C8/fixed-f-shift/clamp: raw 113 clamps to 112 (max*2^F), not to 7 (max in value units)" );
        check( r.clamped == 1,
               "C8/fixed-f-shift/clamp: clamped == 1" );
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
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && !r.malformed && !r.refused,
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
        check( r.clamped == 0,
               "C8/bits-n/boundary: clamped == 0 at the implied max" );
    }

    // (c) clamp: bits(12) value 4096 (2^12) is PAST the implied max — must
    //     clamp to 4095.
    {
        vnew_bits_grow::BitsGrow rec;
        vnew_bits_grow::BitsGrowReset( rec );
        rec.lead = 0xAAAAAAAAu;
        rec.v = 4096u; // past the implied max of bits(12)
        rec.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> buf( (size_t) vnew_bits_grow::BitsGrowFixedMeasure( 1 ) );
        check( vnew_bits_grow::BitsGrowFixedSave( &rec, 1, buf.data(), (int64_t) buf.size() ) == (int64_t) buf.size(),
               "C8/bits-n/clamp: the NEW writer saved one record" );

        vnew_bits_grow::BitsGrow back;
        vnew_bits_grow::BitsGrowReset( back );
        vnew_bits_grow::TableReport r;
        std::vector<vnew_bits_grow::TableFixedEntry> plan( 4096 );
        check( vnew_bits_grow::BitsGrowFixedLoad( &back, 1, buf.data(), (int64_t) buf.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
               "C8/bits-n/clamp: identity read one record" );
        check( back.v == 4095u,
               "C8/bits-n/clamp: bits(12) value 4096 clamps to 4095 (= 2^12-1)" );
        check( r.clamped == 1,
               "C8/bits-n/clamp: clamped == 1" );
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
