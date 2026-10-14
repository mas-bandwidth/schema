// R21 — widenf is the bit-exact widening: signalling NaNs kept, the quiet
// bit carried as the writer wrote it (docs/FIXED-FORM-ALGORITHM.md:334-335).
//
// Law:
//
//   "widenf is the f32 at src as an f64 at dst. Both COUNT widened, and both
//    are exact by construction, NaN payloads included"
//                              (docs/FIXED-FORM-ALGORITHM.md:335)
//
// The quiet bit (IEEE-754 bit 22 of the f32 mantissa, bit 51 of f64) is the
// one a hardware f32->f64 conversion flips: it sets the quiet bit on a
// signalling NaN, destroying the distinction the writer encoded. The widenf
// op is bit surgery, not a hardware conversion, so the quiet bit lands as
// the writer wrote it.
//
// This test constructs a signalling NaN (quiet bit 0) and a quiet NaN
// (quiet bit 1), widens each through the generated fixed-form reader, and
// asserts that the quiet bit and the22-bit payload are preserved exactly.
//
// Run: c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//        -Ibuild/tables-generated/vnum
//        test/conformance/cpp/rows/R21.cpp -o build/rows-cpp-R21
//      && ./build/rows-cpp-R21
// Exit 0 green, exit 1 red; one printed line per assertion.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_float_widenTable.h"
#include "VNEW_float_widenTable.h"

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

// ---- the quiet bit (IEEE-754 bit 22 of the f32 mantissa) -------------------

// A SIGNALLING NaN: exponent all-1s, quiet bit (mantissa bit 22) = 0.
// The payload occupies mantissa bits 21-0.
static constexpr uint32_t kSignalling = 0x7F8ABCDEu;

// A QUIET NaN: same exponent, quiet bit (mantissa bit 22) = 1.
static constexpr uint32_t kQuiet = kSignalling | 0x00400000u;

// The 22 payload bits below the quiet bit (mantissa bits 21-0).
static constexpr uint32_t kPayload22 = kSignalling & 0x003FFFFFu;

// Extract bits from a uint64_t.
static uint64_t bits( uint64_t v, int hi, int lo )
{
    return ( v >> lo ) & ( ( 1ull << ( hi - lo + 1 ) ) - 1 );
}

// ---- widenf: signalling NaN keeps quiet bit = 0 ---------------------------

static void signalling_nan_kept( void )
{
    float sf;
    std::memcpy( &sf, &kSignalling, 4 );

    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = sf;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave );

    vnew_float_widen::FloatWiden back;
    vnew_float_widen::FloatWidenReset( back );
    vnew_float_widen::TableReport r;
    std::vector<vnew_float_widen::TableFixedEntry> plan( 4096 );
    check( vnew_float_widen::FloatWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
           "R21/signalling: the OLD file reads one record" );

    uint64_t got = 0;
    std::memcpy( &got, &back.v, 8 );

    // the sign bit must be preserved
    check( bits( got, 63, 63 ) == (uint64_t)( kSignalling >> 31 ),
           "R21/signalling: the sign bit is preserved" );

    // the exponent must be all-1s (NaN)
    check( bits( got, 62, 52 ) == 0x7FFull,
           "R21/signalling: the exponent is all-1s (NaN)" );

    // THE QUIET BIT: bit 51 of the f64 mantissa (bit 22 of f32 mantissa).
    // A signalling NaN has quiet bit = 0. Hardware conversion would SET it.
    // widenf must NOT set it.
    check( bits( got, 51, 51 ) == 0,
           "R21/signalling: the quiet bit stays 0 — signalling NaN is kept signalling" );

    // the 22 payload bits below the quiet bit ride exactly
    check( (uint32_t) bits( got, 50, 29 ) == kPayload22,
           "R21/signalling: the22 payload bits below the quiet bit ride exactly" );

    // the low 29 bits of f64 mantissa are zero-filled
    check( bits( got, 28, 0 ) == 0,
           "R21/signalling: the low 29 bits of f64 mantissa are zero" );

    check( r.widened == 1, "R21/signalling: widened counts once" );
    check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R21/signalling: nothing else fired" );
}

// ---- widenf: quiet NaN keeps quiet bit = 1 --------------------------------

static void quiet_nan_kept( void )
{
    float qf;
    std::memcpy( &qf, &kQuiet, 4 );

    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = qf;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave );

    vnew_float_widen::FloatWiden back;
    vnew_float_widen::FloatWidenReset( back );
    vnew_float_widen::TableReport r;
    std::vector<vnew_float_widen::TableFixedEntry> plan( 4096 );
    check( vnew_float_widen::FloatWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
           "R21/quiet: the OLD file reads one record" );

    uint64_t got = 0;
    std::memcpy( &got, &back.v, 8 );

    // THE QUIET BIT: bit 51. A quiet NaN has quiet bit = 1. widenf must
    // keep it at 1, not clear it.
    check( bits( got, 51, 51 ) == 1,
           "R21/quiet: the quiet bit stays 1 — quiet NaN is kept quiet" );

    // the same22 payload bits ride exactly
    check( (uint32_t) bits( got, 50, 29 ) == kPayload22,
           "R21/quiet: the22 payload bits below the quiet bit ride exactly" );

    check( r.widened == 1, "R21/quiet: widened counts once" );
    check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R21/quiet: nothing else fired" );
}

// ---- widenf: ordinary values ride without distortion -----------------------

static void ordinary_value( void )
{
    float of = 1.5f;
    uint32_t obits = 0;
    std::memcpy( &obits, &of, 4 );

    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = of;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> wb = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave );

    vnew_float_widen::FloatWiden back;
    vnew_float_widen::FloatWidenReset( back );
    vnew_float_widen::TableReport r;
    std::vector<vnew_float_widen::TableFixedEntry> plan( 4096 );
    check( vnew_float_widen::FloatWidenFixedLoad( &back, 1, wb.data(), (int64_t) wb.size(),
                                                   plan.data(), 4096, NULL, &r ) == 1,
           "R21/ordinary: the OLD file reads one record" );

    // the double value must be exactly 1.5
    check( back.v == 1.5,
           "R21/ordinary: 1.5f widens to exactly 1.5 (no distortion)" );

    // the bit pattern: sign 0, exponent biased for 1.5, mantissa 0.5
    uint64_t got = 0;
    std::memcpy( &got, &back.v, 8 );
    check( bits( got, 63, 63 ) == 0 && bits( got, 62, 52 ) == 0x3FFull && bits( got, 51, 0 ) == 0x8000000000000ull,
           "R21/ordinary: the f64 bit pattern is exactly IEEE-754 1.5" );

    check( r.widened == 1, "R21/ordinary: widened counts once" );
}

int main( void )
{
    signalling_nan_kept();
    quiet_nan_kept();
    ordinary_value();

    if ( g_failures != 0 )
    {
        std::printf( "R21: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "R21: widenf bit-exact widening — all assertions passed\n" );
    return 0;
}
