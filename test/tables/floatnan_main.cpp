// THE PIN'S OWN CLAIM (docs/SPEC-TABLES.md §3, §4, SPEC.md §4.3, schema#480).
//
// A float rides as its IEEE-754 bit pattern and NOTHING canonicalizes it. Two
// halves over testdata/wire/tables/floats_nan.bin, and the second is the one a
// port fails:
//
//   IDENTITY — the pinned bytes load into a float32 field with every bit where
//   the writer put it, the signalling NaN still signalling and the payload
//   still there.
//
//   WIDENED — the same bytes read by the next generation's declaration, where
//   each float32 field is spelled float64. `f32` into `f64` is the float rung
//   of §4's ladder and the only one, and it is exactly where a backend that
//   models a float32 in a wider cell quiets a signalling NaN through the
//   hardware conversion. The reference widens on the BITS: the 23 payload bits
//   ride in the top of the double's 52, sign and all.
//
// Its own binary rather than a case in main.cpp, because the negative control
// regenerates ONLY this pair from a sabotaged emitter and needs a link that
// carries nothing else. `make tables-float-nan-negative-control` replaces the
// bit surgery in TableWidenF32 with a plain conversion and requires this to go
// RED.

#include <cstdio>
#include <cstring>

#include <new>

#include "F1Table.h"
#include "F2Table.h"
#include "floatnan.h"

static int failures = 0;

#define CHECK( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition );     \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

static uint8_t pinned_bytes[1u << 16];

static int64_t read_pin()
{
    FILE * f = fopen( "testdata/wire/tables/floats_nan.bin", "rb" );
    if ( f == NULL )
    {
        printf( "FAIL missing testdata/wire/tables/floats_nan.bin (run: make update-goldens)\n" );
        failures++;
        return -1;
    }
    const size_t n = fread( pinned_bytes, 1, sizeof( pinned_bytes ), f );
    fclose( f );
    return (int64_t) n;
}

// THE WRITER PUTS THE BITS ON THE WIRE. The instance is built from patterns
// and saved, and the bytes must equal the pin: a writer that narrowed through
// a conversion would move them.
static void test_writer()
{
    const int64_t pinned = read_pin();
    if ( pinned < 0 ) return;

    static tblf1::Floats value;
    build_golden_floats_nan( value );
    static uint8_t buffer[1u << 16];
    const int64_t wrote = tblf1::FloatsSave( value, buffer, sizeof( buffer ) );
    if ( wrote != pinned || memcmp( buffer, pinned_bytes, (size_t) pinned ) != 0 )
    {
        printf( "FAIL floats_nan writes differently: %lld bytes out, %lld pinned\n",
                (long long) wrote, (long long) pinned );
        failures++;
    }
}

static void test_identity()
{
    const int64_t pinned = read_pin();
    if ( pinned < 0 ) return;

    static tblf1::Floats value;
    new ( &value ) tblf1::Floats();
    tblf1::TableReport report;
    if ( !tblf1::FloatsLoad( value, pinned_bytes, pinned, &report ) || report.malformed )
    {
        printf( "FAIL floats_nan does not load under its own declaration\n" );
        failures++;
        return;
    }
    CHECK( bits_of_float( value.signalling ) == kFloatSignalling );
    CHECK( bits_of_float( value.payload ) == kFloatPayload );
    CHECK( bits_of_float( value.negative ) == kFloatNegative );
    CHECK( bits_of_double( value.quiet ) == kDoubleQuiet );
    CHECK( bits_of_double( value.wide ) == kDoubleWide );
    CHECK( bits_of_float( value.samples[0] ) == kFloatSample0 );
    CHECK( bits_of_float( value.samples[2] ) == kFloatSample2 );
    CHECK( value.after == 7 );
    // THE QUIET BIT IS BIT 22, and it is CLEAR on the two signalling ones: a
    // conversion anywhere on the path would have set it, which is the whole
    // failure this pin exists to catch
    CHECK( ( bits_of_float( value.signalling ) & 0x00400000u ) == 0 );
    CHECK( ( bits_of_float( value.negative ) & 0x00400000u ) == 0 );
}

static void test_widened()
{
    const int64_t pinned = read_pin();
    if ( pinned < 0 ) return;

    static tblf2::Floats wider;
    new ( &wider ) tblf2::Floats();
    tblf2::TableReport report;
    if ( !tblf2::FloatsLoad( wider, pinned_bytes, pinned, &report ) || report.malformed )
    {
        printf( "FAIL floats_nan does not load under the widened declaration\n" );
        failures++;
        return;
    }
    // three float32 fields read into float64 ones: three widened, nothing
    // unknown and nothing mismatched
    CHECK( report.widened == 3 && report.unknown == 0 && report.kind_mismatch == 0 );

    const uint32_t narrow[3] = { kFloatSignalling, kFloatPayload, kFloatNegative };
    const double got[3] = { wider.signalling, wider.payload, wider.negative };
    for ( int i = 0; i < 3; i++ )
    {
        const uint64_t want = ( (uint64_t) ( narrow[i] >> 31 ) << 63 )
                            | 0x7FF0000000000000ull
                            | ( (uint64_t) ( narrow[i] & 0x007FFFFFu ) << 29 );
        if ( bits_of_double( got[i] ) != want )
        {
            printf( "FAIL floats_nan widened field %d is 0x%016llx and the pattern is 0x%016llx: "
                    "the payload crossed the two widths through a conversion\n",
                    i, (unsigned long long) bits_of_double( got[i] ), (unsigned long long) want );
            failures++;
        }
    }
    // and the two double fields, which cross no width at all
    CHECK( bits_of_double( wider.quiet ) == kDoubleQuiet );
    CHECK( bits_of_double( wider.wide ) == kDoubleWide );
}

int main()
{
    test_writer();
    test_identity();
    test_widened();
    if ( failures > 0 )
    {
        printf( "float bit-pattern test: %d failure(s)\n", failures );
        return 1;
    }
    printf( "float bit-pattern: a float rides as its bit pattern, identity and widened (docs/SPEC-TABLES.md §3, §4)\n" );
    return 0;
}
