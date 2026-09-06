// The wide-text gate (SPEC §4.12) and the string(N) read-side UTF-8 gate
// (§4.7), both driven from serialize's SHARED CORPUS rather than from bytes
// this repository invented: testdata/conformance/text/wstring.txt and
// string.txt are copies of conformance/wstring.txt and conformance/string.txt,
// hand-authored bytes with a checked-in verdict, and this program replays each
// one through the generated reader.
//
// Every type in examples-wide/ holds ONE text field, so a type's whole wire IS
// that field's wire and a corpus vector is the stream verbatim. That is what
// makes the reproduction exact rather than approximate: an implementation that
// inserted an align before the code units, or accepted an unpaired surrogate,
// or dropped the terminator, disagrees with the corpus on bytes.
//
// A vector whose buffer_size has no schema declaration is SKIPPED and named:
// wstring buffer_size 1 and 2 sit below the N floor of 2 (§4.12), and the
// string vectors at buffer_size 1, 8 and 10 declare bounds this unit does not.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>

#include "WideTextWire.h"

using namespace wide;

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d)\n", #condition, __FILE__, __LINE__ ); \
            return 1;                                                         \
        }                                                                     \
    } while ( 0 )

#define check_vector( condition, vector )                                     \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s on vector %s (%s:%d)\n",                      \
                    #condition, ( vector ).name, __FILE__, __LINE__ );        \
            return 1;                                                         \
        }                                                                     \
    } while ( 0 )

// ---------------------------------------------------------------------------
// the corpus reader. STANDARD.md's vector format: blank-line separated
// records, `#` comments, one `key value` per line.

static const int max_bytes = 64;
static const int max_units = 32;

struct Vector
{
    char name[128] = {};
    int buffer_size = 0;
    uint8_t bytes[max_bytes] = {};
    int num_bytes = 0;
    bool refused = false;
    uint32_t units[max_units] = {}; // wstring: the transmitted code units
    uint8_t payload[max_bytes] = {}; // string: the transmitted bytes
    int num_values = 0;
    int64_t consumed = -1;
    bool canonical = false;
};

// POISON every destination buffer before the read. Generated storage
// default-initializes to zeros, so a terminator check against a fresh
// object passes whether or not the reader wrote anything at index
// length. Filling the buffer with a non-zero unit first makes that zero
// the READ's work and nothing else's (SPEC §4.7, §4.12).
template <typename T, int N> static void poison( T ( & buffer )[N] )
{
    for ( int i = 0; i < N; i++ )
    {
        buffer[i] = T( 0x7F );
    }
}

static bool hex_nibble( char c, int & out )
{
    if ( c >= '0' && c <= '9' ) { out = c - '0'; return true; }
    if ( c >= 'A' && c <= 'F' ) { out = c - 'A' + 10; return true; }
    if ( c >= 'a' && c <= 'f' ) { out = c - 'a' + 10; return true; }
    return false;
}

// parses a whitespace-separated run of hexadecimal groups of `width` nibbles
static int parse_hex_groups( const char * text, int width, uint32_t * out, int max_out )
{
    int count = 0;
    const char * p = text;
    while ( *p )
    {
        while ( *p == ' ' || *p == '\t' || *p == '\r' || *p == '\n' ) { p++; }
        if ( !*p ) { break; }
        uint32_t value = 0;
        int digits = 0;
        while ( digits < width )
        {
            int nibble = 0;
            if ( !hex_nibble( *p, nibble ) ) { return -1; }
            value = ( value << 4 ) | uint32_t( nibble );
            p++;
            digits++;
        }
        if ( count >= max_out ) { return -1; }
        out[count++] = value;
    }
    return count;
}

static void trim( char * s )
{
    size_t n = strlen( s );
    while ( n > 0 && ( s[n - 1] == '\n' || s[n - 1] == '\r' || s[n - 1] == ' ' || s[n - 1] == '\t' ) )
    {
        s[--n] = 0;
    }
}

// load reads every record whose `operation` is `operation_name`. Returns the
// count, or -1 on a malformed file — a corpus this program cannot read is a
// failure, never a silent zero vectors run.
static int load( const char * path, const char * operation_name, Vector * out, int max_out )
{
    FILE * f = fopen( path, "rb" );
    if ( !f )
    {
        printf( "cannot open %s\n", path );
        return -1;
    }
    int count = 0;
    Vector current;
    bool in_record = false;
    char line[4096];
    bool wide = strcmp( operation_name, "wstring" ) == 0;
    auto flush = [&]() -> bool
    {
        if ( !in_record ) { return true; }
        in_record = false;
        if ( count >= max_out ) { return false; }
        out[count++] = current;
        current = Vector();
        return true;
    };
    while ( fgets( line, sizeof( line ), f ) )
    {
        trim( line );
        if ( line[0] == '#' ) { continue; }
        if ( line[0] == 0 )
        {
            if ( !flush() ) { fclose( f ); return -1; }
            continue;
        }
        char * space = strchr( line, ' ' );
        const char * key = line;
        const char * rest = "";
        if ( space ) { *space = 0; rest = space + 1; }
        if ( strcmp( key, "operation" ) == 0 )
        {
            if ( !flush() ) { fclose( f ); return -1; }
            in_record = strcmp( rest, operation_name ) == 0;
            current = Vector();
            continue;
        }
        if ( !in_record ) { continue; }
        if ( strcmp( key, "name" ) == 0 )
        {
            snprintf( current.name, sizeof( current.name ), "%s", rest );
        }
        else if ( strcmp( key, "param" ) == 0 )
        {
            const char * eq = strchr( rest, '=' );
            if ( !eq ) { fclose( f ); return -1; }
            current.buffer_size = atoi( eq + 1 );
        }
        else if ( strcmp( key, "bytes" ) == 0 )
        {
            uint32_t raw[max_bytes];
            int n = parse_hex_groups( rest, 2, raw, max_bytes );
            if ( n < 0 ) { fclose( f ); return -1; }
            for ( int i = 0; i < n; i++ ) { current.bytes[i] = uint8_t( raw[i] ); }
            current.num_bytes = n;
        }
        else if ( strcmp( key, "expect" ) == 0 )
        {
            if ( strcmp( rest, "refused" ) == 0 )
            {
                current.refused = true;
            }
            else
            {
                const char * eq = strchr( rest, '=' );
                if ( !eq ) { fclose( f ); return -1; }
                const char * values = eq + 1;
                if ( wide )
                {
                    int n = parse_hex_groups( values, 4, current.units, max_units );
                    if ( n < 0 ) { fclose( f ); return -1; }
                    current.num_values = n;
                }
                else
                {
                    uint32_t raw[max_bytes];
                    int n = parse_hex_groups( values, 2, raw, max_bytes );
                    if ( n < 0 ) { fclose( f ); return -1; }
                    for ( int i = 0; i < n; i++ ) { current.payload[i] = uint8_t( raw[i] ); }
                    current.num_values = n;
                }
            }
        }
        else if ( strcmp( key, "consumed" ) == 0 )
        {
            current.consumed = atoll( rest );
        }
        else if ( strcmp( key, "writer" ) == 0 )
        {
            current.canonical = strcmp( rest, "canonical" ) == 0;
        }
    }
    if ( !flush() ) { fclose( f ); return -1; }
    fclose( f );
    return count;
}

// ---------------------------------------------------------------------------
// the replay. A read stream's allocation must extend at least 8 bytes past the
// packet data (SPEC §6.3), which is what the padded scratch buffer is for.

static uint8_t scratch[max_bytes + 16];

static void stage( const Vector & v )
{
    memset( scratch, 0, sizeof( scratch ) );
    memcpy( scratch, v.bytes, size_t( v.num_bytes ) );
}

// serialize's read stream takes a whole number of WORDS; the corpus's byte
// count is what the vector actually carries, and the reader must not see a
// byte past it. Rounding UP would hand the reader bytes the vector does not
// have, so the count is passed exactly and the stream's own past-end refusal
// is what the truncation vectors exercise.
static int stream_bytes( const Vector & v ) { return v.num_bytes; }

int main()
{
    // ---- wstring, at the corpus's buffer_size 8 (wstring(7)) and 5 (wstring(4))
    static Vector wide_vectors[64];
    const int wide_count = load( "testdata/conformance/text/wstring.txt", "wstring", wide_vectors, 64 );
    check( wide_count > 0 );

    int wide_replayed = 0, wide_skipped = 0, wide_refusals = 0;
    for ( int i = 0; i < wide_count; i++ )
    {
        const Vector & v = wide_vectors[i];
        if ( v.buffer_size != 8 && v.buffer_size != 5 )
        {
            // no declaration reproduces it: buffer_size 1 and 2 put N below
            // the floor of 2 (SPEC §4.12)
            printf( "skipped (no declaration at buffer_size %d): %s\n", v.buffer_size, v.name );
            wide_skipped++;
            continue;
        }
        stage( v );
        wide_replayed++;
        if ( v.refused ) { wide_refusals++; }

        if ( v.buffer_size == 8 )
        {
            WideSeven out;
            poison( out.text );
            serialize::ReadStream rs( scratch, stream_bytes( v ) );
            const bool ok = ReadWideSeven( rs, out );
            check_vector( ok == !v.refused, v );
            if ( v.refused ) { continue; }
            check_vector( out.text_length == v.num_values, v );
            for ( int u = 0; u < v.num_values; u++ )
            {
                check_vector( uint32_t( out.text[u] ) == v.units[u], v );
            }
            // the terminating zero UNIT at index length, always (SPEC §4.12)
            check_vector( out.text[out.text_length] == 0, v );
            check_vector( rs.GetBitsProcessed() == v.consumed, v );

            // the accepted vector re-encodes to the corpus bytes exactly: an
            // align inserted before the code units moves every byte after it
            if ( v.canonical )
            {
                uint8_t encoded[max_bytes + 16] = {};
                serialize::WriteStream ws( encoded, sizeof( encoded ) );
                check_vector( WriteWideSeven( ws, out ), v );
                ws.Flush();
                check_vector( ws.GetBitsProcessed() == v.consumed, v );
                check_vector( memcmp( encoded, v.bytes, size_t( v.num_bytes ) ) == 0, v );
            }
        }
        else
        {
            WideFour out;
            poison( out.text );
            serialize::ReadStream rs( scratch, stream_bytes( v ) );
            const bool ok = ReadWideFour( rs, out );
            check_vector( ok == !v.refused, v );
            if ( v.refused ) { continue; }
            check_vector( out.text_length == v.num_values, v );
            for ( int u = 0; u < v.num_values; u++ )
            {
                check_vector( uint32_t( out.text[u] ) == v.units[u], v );
            }
            check_vector( out.text[out.text_length] == 0, v );
            check_vector( rs.GetBitsProcessed() == v.consumed, v );
            if ( v.canonical )
            {
                uint8_t encoded[max_bytes + 16] = {};
                serialize::WriteStream ws( encoded, sizeof( encoded ) );
                check_vector( WriteWideFour( ws, out ), v );
                ws.Flush();
                check_vector( ws.GetBitsProcessed() == v.consumed, v );
                check_vector( memcmp( encoded, v.bytes, size_t( v.num_bytes ) ) == 0, v );
            }
        }
    }

    // ---- string, at the corpus's buffer_size 16 (string(15)) ----
    static Vector narrow_vectors[64];
    const int narrow_count = load( "testdata/conformance/text/string.txt", "string", narrow_vectors, 64 );
    check( narrow_count > 0 );

    int narrow_replayed = 0, narrow_skipped = 0, narrow_refusals = 0;
    for ( int i = 0; i < narrow_count; i++ )
    {
        const Vector & v = narrow_vectors[i];
        if ( v.buffer_size != 16 )
        {
            printf( "skipped (no declaration at buffer_size %d): %s\n", v.buffer_size, v.name );
            narrow_skipped++;
            continue;
        }
        stage( v );
        narrow_replayed++;
        if ( v.refused ) { narrow_refusals++; }

        NarrowFifteen out;
        poison( out.text );
        serialize::ReadStream rs( scratch, stream_bytes( v ) );
        const bool ok = ReadNarrowFifteen( rs, out );
        check_vector( ok == !v.refused, v );
        if ( v.refused ) { continue; }
        check_vector( out.text_length == v.num_values, v );
        check_vector( memcmp( out.text, v.payload, size_t( v.num_values ) ) == 0, v );
        // the terminating zero BYTE at index length, always (SPEC §4.7)
        check_vector( out.text[out.text_length] == 0, v );
        check_vector( rs.GetBitsProcessed() == v.consumed, v );
        if ( v.canonical )
        {
            uint8_t encoded[max_bytes + 16] = {};
            serialize::WriteStream ws( encoded, sizeof( encoded ) );
            check_vector( WriteNarrowFifteen( ws, out ), v );
            ws.Flush();
            check_vector( ws.GetBitsProcessed() == v.consumed, v );
            check_vector( memcmp( encoded, v.bytes, size_t( v.num_bytes ) ) == 0, v );
        }
    }

    // ---- the six interop cases (SPEC §4.12): serialize.js's own wstring set,
    // round-tripped through the field the cross-language matrix is owed ----
    {
        static const uint32_t cases[6][8] = {
            {},                                                       // empty
            { 0x043C, 0x0438, 0x0440 },                               // cyrillic, basic plane
            { 0xE000 },                                               // first unit above the surrogate block
            { 0xFFFF },                                               // the largest code unit there is
            { 0x0041, 0xD83D, 0xDE00, 0x0042 },                       // an astral pair between two basic-plane units
            { 0x0061, 0x0062, 0x0063, 0x0064, 0x0065, 0x0066, 0x0067 } // seven units, the most the bound carries
        };
        static const int lengths[6] = { 0, 3, 1, 1, 4, 7 };
        for ( int c = 0; c < 6; c++ )
        {
            WideInterop in;
            in.caption_length = lengths[c];
            for ( int u = 0; u < lengths[c]; u++ )
            {
                in.caption[u] = char16_t( cases[c][u] );
            }
            uint8_t encoded[WideInteropMaxBytes] = {};
            serialize::WriteStream ws( encoded, sizeof( encoded ) );
            check( WriteWideInterop( ws, in ) );
            ws.Flush();
            // no alignment anywhere: the length field is 3 bits and each unit
            // is one 32-bit group (SPEC §4.12)
            check( ws.GetBitsProcessed() == 3 + 32 * int64_t( lengths[c] ) );

            WideInterop out;
            poison( out.caption );
            serialize::ReadStream rs( encoded, ws.GetBytesProcessed() );
            check( ReadWideInterop( rs, out ) );
            check( out.caption_length == lengths[c] );
            for ( int u = 0; u < lengths[c]; u++ )
            {
                check( uint32_t( out.caption[u] ) == cases[c][u] );
            }
            check( out.caption[out.caption_length] == 0 );
        }
    }

    printf( "wstring: %d vectors replayed (%d refusals), %d skipped\n", wide_replayed, wide_refusals, wide_skipped );
    printf( "string: %d vectors replayed (%d refusals), %d skipped\n", narrow_replayed, narrow_refusals, narrow_skipped );
    printf( "OK\n" );
    return 0;
}
