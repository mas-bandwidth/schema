// THE FINITE RULE'S PIN (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables
// go finite-only too, same rule as packets"): a NaN or an infinity, whatever
// its sign or payload, is REFUSED by every writer and by every read, the
// tolerant read included, and the read refuses it ON THE RAW BITS, before
// they become a float and before the field is written, as non_finite_float.
// Nothing lands. -0 and the subnormals are finite and ride bit-exact, and a
// -0 under a +0 default is not elided: elision compares bits.
//
// Over testdata/wire/tables/floats_nan.bin, the shared MUST-REFUSE vector, and
// over vectors this program forges from finite ones:
//
//   THE PIN         floats_nan.bin read by F1 (identity) and by F2, where three
//                   float32 fields are spelled float64 — the float rung — is
//                   refused, strict and tolerant, and lands nothing.
//   THE WRITERS     every non-finite pattern, in every float position, is
//                   refused by the variable form's measure and save, the
//                   message form's, the fixed form's save and the cook.
//   FIXED, MESSAGE  a finite file with ONE float forged non-finite is refused
//                   by the fixed read and by the message read (count 0).
//   FINITE RIDES    -0 and subnormals round trip bit-exact in the variable,
//                   message and fixed forms, and -0 is not elided.
//
// Its own binary rather than a case in main.cpp, because the negative control
// regenerates ONLY this pair from a sabotaged emitter and needs a link that
// carries nothing else. `make tables-float-nan-negative-control` makes the
// read's mask test answer finite for every pattern and requires this to go
// RED on the pin.

#include <cstdio>
#include <cstring>

#include <new>
#include <vector>

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
        printf( "FAIL missing testdata/wire/tables/floats_nan.bin (the frozen must-refuse vector)\n" );
        failures++;
        return -1;
    }
    const size_t n = fread( pinned_bytes, 1, sizeof( pinned_bytes ), f );
    fclose( f );
    return (int64_t) n;
}

// A REFUSAL BY NAME: refused, non_finite_float on both reason channels
template <typename Report>
static bool refused_non_finite( const Report & r )
{
    return r.refused && r.reason == tblf1::non_finite_float && r.refuse_reason == tblf1::non_finite_float && r.non_finite_floats > 0;
}
template <typename Report>
static bool refused_non_finite_f2( const Report & r )
{
    return r.refused && r.reason == tblf2::non_finite_float && r.refuse_reason == tblf2::non_finite_float && r.non_finite_floats > 0;
}

// the finite instance every forge starts from: one SENTINEL per form to find
// the float by, -0 and a subnormal beside it
static const uint32_t kSentinel = 0x3FA00000u; // 1.25f, nowhere else in the record
static void build_finite( tblf1::Floats & f )
{
    tblf1::FloatsReset( f );
    f.signalling = float_from_bits( kSentinel );
    f.payload = float_from_bits( 0x80000000u ); // -0 under a +0 default
    f.negative = float_from_bits( 0x80000001u ); // the smallest negative subnormal
    f.quiet = double_from_bits( 0x8000000000000000ull ); // -0 at sixty-four bits
    f.wide = double_from_bits( 0x000FFFFFFFFFFFFFull ); // the largest subnormal double
    f.samples[0] = float_from_bits( 0x00000001u );
    f.samples[1] = 1.5f;
    f.samples[2] = float_from_bits( 0x80000000u );
    f.past = 7;
}

static void check_finite_landed( const tblf1::Floats & f, const char * form )
{
    if ( bits_of_float( f.signalling ) != kSentinel || bits_of_float( f.payload ) != 0x80000000u ||
         bits_of_float( f.negative ) != 0x80000001u || bits_of_double( f.quiet ) != 0x8000000000000000ull ||
         bits_of_double( f.wide ) != 0x000FFFFFFFFFFFFFull || bits_of_float( f.samples[0] ) != 0x00000001u ||
         f.samples[1] != 1.5f || bits_of_float( f.samples[2] ) != 0x80000000u || f.past != 7 )
    {
        printf( "FAIL %s: -0 and the subnormals did not round trip bit-exact\n", form );
        failures++;
    }
}

// NOTHING LANDS: the value a refused read hands back is the declaration's
static bool at_defaults( const tblf1::Floats & f )
{
    return bits_of_float( f.signalling ) == 0 && bits_of_float( f.payload ) == 0 && bits_of_float( f.negative ) == 0 &&
           bits_of_double( f.quiet ) == 0 && bits_of_double( f.wide ) == 0 && bits_of_float( f.samples[0] ) == 0 &&
           bits_of_float( f.samples[1] ) == 0 && bits_of_float( f.samples[2] ) == 0 && f.past == 0;
}

// ---- THE PIN ---------------------------------------------------------------

static void test_pin_refused()
{
    const int64_t pinned = read_pin();
    if ( pinned < 0 ) return;

    {
        static tblf1::Floats value;
        new ( &value ) tblf1::Floats();
        tblf1::TableReport report;
        const bool loaded = tblf1::FloatsLoadTolerant( value, pinned_bytes, pinned, &report );
        if ( loaded || !refused_non_finite( report ) )
        {
            printf( "FAIL floats_nan was not refused as non_finite_float by the TOLERANT read\n" );
            failures++;
        }
        CHECK( at_defaults( value ) ); // nothing lands, the field after the floats included
        CHECK( !report.malformed && report.clamped == 0 && report.kind_mismatch == 0 );
    }
    {
        static tblf1::Floats value;
        new ( &value ) tblf1::Floats();
        tblf1::TableReport report;
        if ( tblf1::FloatsLoad( value, pinned_bytes, pinned, &report ) || !refused_non_finite( report ) )
        {
            printf( "FAIL floats_nan was not refused as non_finite_float by the STRICT read\n" );
            failures++;
        }
        CHECK( at_defaults( value ) );
    }
    {
        // THE FLOAT RUNG: three float32 fields read into float64 ones, where
        // the test runs on the float32's bits before the conversion
        static tblf2::Floats wider;
        new ( &wider ) tblf2::Floats();
        tblf2::TableReport report;
        if ( tblf2::FloatsLoadTolerant( wider, pinned_bytes, pinned, &report ) || !refused_non_finite_f2( report ) )
        {
            printf( "FAIL floats_nan was not refused as non_finite_float through the widened rung\n" );
            failures++;
        }
        CHECK( bits_of_double( wider.signalling ) == 0 && bits_of_double( wider.wide ) == 0 && wider.past == 0 );
    }
}

// ---- THE WRITERS -----------------------------------------------------------

static const uint32_t kNarrow[5] = { kFloatSignalling, kFloatPayload, kFloatNegative, kFloatInfinity, 0xFF800000u };
static const uint64_t kWide[4] = { kDoubleQuiet, kDoubleWide, kDoubleNegInf, 0x7FF0000000000000ull };

static void check_writers_refuse( const tblf1::Floats & f, const char * where )
{
    static uint8_t buffer[1u << 16];
    bool refused = true;
    refused = refused && tblf1::FloatsMeasure( f ) == -1;
    refused = refused && tblf1::FloatsSave( f, buffer, sizeof( buffer ) ) == -1;
    refused = refused && tblf1::FloatsFixedSave( &f, 1, buffer, sizeof( buffer ) ) == -1;
    tblf1::TableReport report;
    refused = refused && tblf1::FloatsMeasureMessages( &f, 1, &report ) == -1;
    refused = refused && tblf1::FloatsSaveMessages( &f, 1, buffer, sizeof( buffer ), &report ) == -1;
    refused = refused && !tblf1::FloatsCook( f, buffer, sizeof( buffer ), tblf1::TableByteOrder::Little );
    if ( !refused )
    {
        printf( "FAIL a writer wrote a non-finite %s\n", where );
        failures++;
    }
}

static void test_writers_refuse()
{
    static tblf1::Floats f;
    build_golden_floats_nan( f );
    check_writers_refuse( f, "floats_nan instance" );
    for ( int k = 0; k < 5; ++k )
    {
        const float bad = float_from_bits( kNarrow[k] );
        build_finite( f ); f.signalling = bad; check_writers_refuse( f, "float32 field" );
        build_finite( f ); f.samples[1] = bad; check_writers_refuse( f, "float32 array element" );
    }
    for ( int k = 0; k < 4; ++k )
    {
        build_finite( f ); f.wide = double_from_bits( kWide[k] ); check_writers_refuse( f, "float64 field" );
    }
}

// ---- FINITE RIDES ----------------------------------------------------------

static void test_finite_rides()
{
    static uint8_t buffer[1u << 16];
    static tblf1::Floats f, back;
    build_finite( f );

    // THE VARIABLE FORM: -0 under a +0 default rides, so the wire is longer
    // than the same instance with +0 there
    {
        const int64_t wrote = tblf1::FloatsSave( f, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tblf1::FloatsMeasure( f ) );
        static tblf1::Floats plus;
        plus = f;
        plus.payload = 0.0f;
        CHECK( wrote > tblf1::FloatsMeasure( plus ) ); // -0 is NOT elided
        new ( &back ) tblf1::Floats();
        tblf1::TableReport report;
        CHECK( tblf1::FloatsLoad( back, buffer, wrote, &report ) );
        check_finite_landed( back, "the variable form" );
    }
    // THE MESSAGE FORM
    {
        std::vector<uint8_t> announcement( (size_t) tblf1::AnnounceMeasure() );
        CHECK( tblf1::Announce( announcement.data(), (int64_t) announcement.size() ) == (int64_t) announcement.size() );
        std::vector<tblf1::TableMessageEntry> entries( (size_t) tblf1::kTableMessageEntriesHere );
        tblf1::TableVocabulary vocabulary( entries.data(), tblf1::kTableMessageEntriesHere );
        CHECK( tblf1::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), NULL ) );
        tblf1::TableReport report;
        const int64_t wrote = tblf1::FloatsSaveMessages( &f, 1, buffer, sizeof( buffer ), &report );
        CHECK( wrote > 0 );
        int64_t count = 1;
        new ( &back ) tblf1::Floats();
        tblf1::TableReport loaded;
        CHECK( tblf1::FloatsLoadMessages( &back, &count, vocabulary, buffer, wrote, &loaded ) && count == 1 );
        check_finite_landed( back, "the message form" );
    }
    // THE FIXED FORM
    {
        const int64_t wrote = tblf1::FloatsFixedSave( &f, 1, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 );
        std::vector<tblf1::TableFixedEntry> plan( 1024 );
        tblf1::TableReport report;
        new ( &back ) tblf1::Floats();
        CHECK( tblf1::FloatsFixedLoad( &back, 1, buffer, wrote, plan.data(), 1024, NULL, &report ) == 1 );
        check_finite_landed( back, "the fixed form" );
    }
}

// ---- FIXED AND MESSAGE: one float forged non-finite ------------------------

// the sentinel's byte offset in a byte-aligned form, or -1
static int64_t find_sentinel( const uint8_t * at, int64_t bytes )
{
    for ( int64_t i = 0; i + 4 <= bytes; ++i )
    {
        uint32_t w = 0;
        for ( int b = 0; b < 4; ++b ) { w |= (uint32_t) at[i + b] << ( 8 * b ); }
        if ( w == kSentinel ) { return i; }
    }
    return -1;
}

static void test_fixed_refused()
{
    static uint8_t buffer[1u << 16];
    static tblf1::Floats f;
    build_finite( f );
    const int64_t wrote = tblf1::FloatsFixedSave( &f, 1, buffer, sizeof( buffer ) );
    const int64_t at = find_sentinel( buffer, wrote );
    CHECK( wrote > 0 && at >= 0 );
    if ( wrote <= 0 || at < 0 ) return;
    for ( int k = 0; k < 5; ++k )
    {
        for ( int b = 0; b < 4; ++b ) { buffer[at + b] = (uint8_t) ( kNarrow[k] >> ( 8 * b ) ); }
        static tblf1::Floats back[1];
        build_finite( back[0] );
        std::vector<tblf1::TableFixedEntry> plan( 1024 );
        tblf1::TableReport report;
        const int64_t n = tblf1::FloatsFixedLoadTolerant( back, 1, buffer, wrote, plan.data(), 1024, NULL, &report );
        if ( n != -1 || !refused_non_finite( report ) )
        {
            printf( "FAIL a fixed record carrying 0x%08x was not refused as non_finite_float\n", kNarrow[k] );
            failures++;
        }
        CHECK( at_defaults( back[0] ) ); // the record this call wrote goes back to its defaults
    }
}

// the sentinel's BIT offset in the bitpacked message form, low bit first as
// the bit writer puts it, or -1
static int64_t find_sentinel_bits( const uint8_t * at, int64_t bytes )
{
    for ( int64_t bit = 0; bit + 32 <= bytes * 8; ++bit )
    {
        uint32_t w = 0;
        for ( int b = 0; b < 32; ++b ) { w |= (uint32_t) ( ( at[( bit + b ) >> 3] >> ( ( bit + b ) & 7 ) ) & 1u ) << b; }
        if ( w == kSentinel ) { return bit; }
    }
    return -1;
}

static void test_message_refused()
{
    std::vector<uint8_t> announcement( (size_t) tblf1::AnnounceMeasure() );
    tblf1::Announce( announcement.data(), (int64_t) announcement.size() );
    std::vector<tblf1::TableMessageEntry> entries( (size_t) tblf1::kTableMessageEntriesHere );
    tblf1::TableVocabulary vocabulary( entries.data(), tblf1::kTableMessageEntriesHere );
    CHECK( tblf1::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), NULL ) );

    static uint8_t buffer[1u << 16];
    static tblf1::Floats f;
    build_finite( f );
    tblf1::TableReport saved;
    const int64_t wrote = tblf1::FloatsSaveMessages( &f, 1, buffer, sizeof( buffer ), &saved );
    const int64_t bit = find_sentinel_bits( buffer, wrote );
    CHECK( wrote > 0 && bit >= 0 );
    if ( wrote <= 0 || bit < 0 ) return;
    for ( int k = 0; k < 5; ++k )
    {
        for ( int b = 0; b < 32; ++b )
        {
            const int64_t at = bit + b;
            const uint8_t mask = (uint8_t) ( 1u << ( at & 7 ) );
            if ( ( kNarrow[k] >> b ) & 1u ) { buffer[at >> 3] |= mask; } else { buffer[at >> 3] &= (uint8_t) ~mask; }
        }
        static tblf1::Floats back[1];
        build_finite( back[0] );
        int64_t count = 1;
        tblf1::TableReport report;
        const bool loaded = tblf1::FloatsLoadMessages( back, &count, vocabulary, buffer, wrote, &report );
        if ( loaded || count != 0 || !refused_non_finite( report ) )
        {
            printf( "FAIL a message carrying 0x%08x was not refused as non_finite_float\n", kNarrow[k] );
            failures++;
        }
    }
}

int main()
{
    test_pin_refused();
    test_writers_refuse();
    test_finite_rides();
    test_fixed_refused();
    test_message_refused();
    if ( failures > 0 )
    {
        printf( "float finite test: %d failure(s)\n", failures );
        return 1;
    }
    printf( "float finite: every read refuses a NaN or an infinity as non_finite_float, every writer refuses one, and -0 and subnormals ride bit-exact (docs/SPEC-TABLES.md §3.3)\n" );
    return 0;
}
