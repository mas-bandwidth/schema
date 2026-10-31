// R21 — widenf widens a FINITE float exactly, and a NaN never reaches it: the
// fixed read refuses one on its bits as non_finite_float (docs/SPEC-TABLES.md
// §3.3; docs/FIXED-FORM-ALGORITHM.md, the widenf row).
//
// Law (Glenn, 2026-10-27, "tables go finite-only too, same rule as packets"):
//
//   A NaN (any sign, any payload, quiet or signalling) and both infinities are
//   not values of the type: every writer refuses one, and every read refuses
//   one as non_finite_float, the tolerant read included. Every finite float32
//   widens to float64 exactly, so widenf is the ordinary conversion.
//
// This row used to pin the NaN's quiet bit across the rung. Under the finite
// rule there is no quiet bit to carry: the OLD writer refuses the NaN, and a
// file FORGED to carry one — a finite record with its float32's four bytes
// replaced — is refused by the NEW reader's compiled plan and by the OLD
// reader's identity plan alike, with nothing landed. A finite value, a
// negative subnormal with a rich mantissa among them, widens to exactly its
// own value.
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

// A SIGNALLING NaN: exponent all-1s, quiet bit (mantissa bit 22) = 0.
static constexpr uint32_t kSignalling = 0x7F8ABCDEu;
// A QUIET NaN: same exponent, quiet bit (mantissa bit 22) = 1.
static constexpr uint32_t kQuiet = kSignalling | 0x00400000u;
// The finite value the forge starts from, found by its bits in the record.
static constexpr uint32_t kSentinel = 0x3FA00000u; // 1.25f

// THE OLD FILE, FORGED: a finite record whose float32 is then replaced by
// `bits`, which the writer itself refuses to write
static std::vector<uint8_t> forged( uint32_t bits )
{
    float sf;
    std::memcpy( &sf, &kSentinel, 4 );
    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = sf;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave );
    bool found = false;
    for ( size_t i = 0; i + 4 <= ob.size() && !found; ++i )
    {
        const uint32_t w = (uint32_t) ob[i] | (uint32_t) ob[i + 1] << 8 | (uint32_t) ob[i + 2] << 16 | (uint32_t) ob[i + 3] << 24;
        if ( w == kSentinel )
        {
            for ( int b = 0; b < 4; ++b ) { ob[i + (size_t) b] = (uint8_t) ( bits >> ( 8 * b ) ); }
            found = true;
        }
    }
    check( found, "forge: the float32 is where the record holds it" );
    return ob;
}

// ---- the writer refuses the NaN -------------------------------------------

static void writer_refuses( void )
{
    const uint32_t patterns[3] = { kSignalling, kQuiet, 0xFF800000u /* -inf */ };
    for ( int k = 0; k < 3; ++k )
    {
        float f;
        std::memcpy( &f, &patterns[k], 4 );
        vold_float_widen::FloatWiden old;
        vold_float_widen::FloatWidenReset( old );
        old.v = f;
        std::vector<uint8_t> out( (size_t) vold_float_widen::FloatWidenFixedMeasure( 1 ) );
        check( vold_float_widen::FloatWidenFixedSave( &old, 1, out.data(), (int64_t) out.size() ) == -1,
               "R21/writer: a NaN or an infinity is refused, never written" );
    }
}

// ---- the read refuses the NaN on both plans --------------------------------

static void nan_refused( uint32_t bits, const char * compiled, const char * identity )
{
    const std::vector<uint8_t> ob = forged( bits );

    // THE COMPILED PLAN: the NEW reader's widenf meets the float32
    vnew_float_widen::FloatWiden back;
    vnew_float_widen::FloatWidenReset( back );
    back.lead = 7u;
    vnew_float_widen::TableReport r;
    std::vector<vnew_float_widen::TableFixedEntry> plan( 4096 );
    const int64_t n = vnew_float_widen::FloatWidenFixedLoadTolerant( &back, 1, ob.data(), (int64_t) ob.size(),
                                                                       plan.data(), 4096, NULL, &r );
    check( n == -1 && r.refused && r.reason == vnew_float_widen::non_finite_float &&
           r.refuse_reason == vnew_float_widen::non_finite_float, compiled );
    check( back.lead == 1u && back.trail == 2u,
           "R21: nothing lands — the record goes back to its declared defaults" );

    // THE IDENTITY PLAN: the OLD reader over its own generation's bytes
    vold_float_widen::FloatWiden same;
    vold_float_widen::TableReport r2;
    std::vector<vold_float_widen::TableFixedEntry> plan2( 4096 );
    check( vold_float_widen::FloatWidenFixedLoad( &same, 1, ob.data(), (int64_t) ob.size(),
                                                   plan2.data(), 4096, NULL, &r2 ) == -1 &&
           r2.refuse_reason == vold_float_widen::non_finite_float, identity );
}

// ---- widenf: a finite value widens exactly ---------------------------------

static void finite_value( float of, const char * what )
{
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
           "R21/finite: the OLD file reads one record" );
    const double want = (double) of;
    uint64_t got = 0, want_bits = 0;
    std::memcpy( &got, &back.v, 8 );
    std::memcpy( &want_bits, &want, 8 );
    check( got == want_bits, what );
    check( r.widened == 1, "R21/finite: widened counts once" );
    check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R21/finite: nothing else fired" );
}

int main( void )
{
    writer_refuses();
    nan_refused( kSignalling, "R21/signalling: the compiled plan refuses a signalling NaN as non_finite_float",
                 "R21/signalling: the identity plan refuses it too" );
    nan_refused( kQuiet, "R21/quiet: the compiled plan refuses a quiet NaN as non_finite_float",
                 "R21/quiet: the identity plan refuses it too" );
    nan_refused( 0x7F800000u, "R21/infinity: the compiled plan refuses +inf as non_finite_float",
                 "R21/infinity: the identity plan refuses it too" );
    finite_value( 1.5f, "R21/ordinary: 1.5f widens to exactly 1.5" );
    float sub;
    const uint32_t kSubnormal = 0x807ABCDEu;
    std::memcpy( &sub, &kSubnormal, 4 );
    finite_value( sub, "R21/subnormal: a negative subnormal widens to exactly its own value" );
    float nz;
    const uint32_t kMinusZero = 0x80000000u;
    std::memcpy( &nz, &kMinusZero, 4 );
    finite_value( nz, "R21/minus-zero: -0 widens to -0, sign and all" );

    if ( g_failures != 0 )
    {
        std::printf( "R21: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "R21: widenf is exact over finite values and a NaN is refused — all assertions passed\n" );
    return 0;
}
