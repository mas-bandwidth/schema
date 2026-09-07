// The C++ packet oracle for string, bytes and flags defaults. Every instance
// must match an explicitly initialized twin whose schema declares no defaults.
#include <cstdio>
#include <cstring>
#include <fstream>
#include <iterator>
#include <new>
#include <string>
#include <vector>

#include "DefaultsWire.h"
#include "PlainWire.h"

#define CHECK( condition, name )                                      \
    do                                                               \
    {                                                                \
        if ( !( condition ) )                                        \
        {                                                            \
            std::fprintf( stderr, "FAILED: %s (%s:%d)\n",             \
                          name, __FILE__, __LINE__ );                 \
            return false;                                            \
        }                                                            \
    } while ( 0 )

static const uint8_t name_bytes[] = { 0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80 };
static const uint8_t token_bytes[] = { 0x5c, 0x6e, 0x5c, 0x74 };

template <typename T, size_t N>
static bool zero_buffer( const T ( & value )[N] )
{
    for ( const auto byte : value )
        if ( byte != 0 )
            return false;
    return true;
}

static bool sample_defaults( const packetdefaults::Sample & value )
{
    CHECK( value.name_length == 6, "sample constructor: UTF-8 byte length" );
    CHECK( std::memcmp( value.name, name_bytes, sizeof( name_bytes ) ) == 0,
           "sample constructor: exact-capacity UTF-8 bytes" );
    CHECK( value.name[6] == 0, "sample constructor: C string terminator" );
    CHECK( value.token_length == 4, "sample constructor: bytes length" );
    CHECK( std::memcmp( value.token, token_bytes, sizeof( token_bytes ) ) == 0,
           "sample constructor: literal backslashes are bytes" );
    CHECK( value.caps == 5, "sample constructor: non-adjacent flags mask" );
    CHECK( value.empty_name_length == 0 && zero_buffer( value.empty_name ),
           "sample constructor: explicit empty string" );
    CHECK( value.empty_token_length == 0 && zero_buffer( value.empty_token ),
           "sample constructor: explicit empty bytes" );
    CHECK( value.empty_caps == 0, "sample constructor: empty flags mask" );
    return true;
}

static packetplain::Sample plain_sample()
{
    packetplain::Sample value;
    std::memcpy( value.name, name_bytes, sizeof( name_bytes ) );
    value.name_length = 6;
    std::memcpy( value.token, token_bytes, sizeof( token_bytes ) );
    value.token_length = 4;
    value.caps = 5;
    return value;
}

static packetplain::Batch plain_batch()
{
    packetplain::Batch value;
    value.head = plain_sample();
    for ( auto & sample : value.items )
        sample = plain_sample();
    for ( auto & sample : value.counted )
        sample = plain_sample();
    value.counted_count = 1;
    return value;
}

template <typename Sample>
static void empty_sample( Sample & value )
{
    std::memset( value.name, 0, sizeof( value.name ) );
    value.name_length = 0;
    std::memset( value.token, 0, sizeof( value.token ) );
    value.token_length = 0;
    value.caps = 0;
    std::memset( value.empty_name, 0, sizeof( value.empty_name ) );
    value.empty_name_length = 0;
    std::memset( value.empty_token, 0, sizeof( value.empty_token ) );
    value.empty_token_length = 0;
    value.empty_caps = 0;
}

template <typename Sample>
static void short_sample( Sample & value )
{
    empty_sample( value );
    value.name[0] = 'A';
    value.name_length = 1;
    value.token[0] = 0;
    value.token[1] = 0xff;
    value.token_length = 2;
    value.caps = 2;
}

static bool golden( const char * directory, const char * name,
                    const uint8_t * data, int64_t bytes, int64_t bits, bool update )
{
    const std::string stem = std::string( directory ) + "/" + name;
    const std::string bit_text = std::to_string( bits ) + "\n";
    if ( update )
    {
        std::ofstream wire( stem + ".bin", std::ios::binary );
        wire.write( reinterpret_cast<const char *>( data ), bytes );
        wire.flush();
        CHECK( wire.good(), ( stem + ".bin: write failed" ).c_str() );
        std::ofstream count( stem + ".bits", std::ios::binary );
        count << bit_text;
        count.flush();
        CHECK( count.good(), ( stem + ".bits: write failed" ).c_str() );
        return true;
    }
    std::ifstream wire( stem + ".bin", std::ios::binary );
    CHECK( wire.good(), ( stem + ".bin: missing golden" ).c_str() );
    const std::vector<uint8_t> expected( ( std::istreambuf_iterator<char>( wire ) ), {} );
    CHECK( int64_t( expected.size() ) == bytes &&
           std::memcmp( expected.data(), data, size_t( bytes ) ) == 0,
           ( stem + ".bin: wire mismatch" ).c_str() );
    std::ifstream count( stem + ".bits", std::ios::binary );
    CHECK( count.good(), ( stem + ".bits: missing golden" ).c_str() );
    const std::string expected_bits( ( std::istreambuf_iterator<char>( count ) ), {} );
    CHECK( expected_bits == bit_text, ( stem + ".bits: bit count mismatch" ).c_str() );
    return true;
}

template <typename Value, typename Plain>
static bool compare_and_pin( const char * directory, const char * name,
                             const Value & value, const Plain & plain,
                             bool ( * write )( serialize::WriteStream &, const Value & ),
                             bool ( * write_plain )( serialize::WriteStream &, const Plain & ),
                             bool update )
{
    alignas( 8 ) uint8_t buffer[4096] = {};
    alignas( 8 ) uint8_t twin[4096] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    serialize::WriteStream twin_stream( twin, sizeof( twin ) );
    CHECK( write( stream, value ) && write_plain( twin_stream, plain ), name );
    const int64_t bits = stream.GetBitsProcessed();
    CHECK( bits == twin_stream.GetBitsProcessed(), "defaults changed the packet bit count" );
    stream.Flush();
    twin_stream.Flush();
    const int64_t bytes = stream.GetBytesProcessed();
    CHECK( bytes == twin_stream.GetBytesProcessed() &&
           std::memcmp( buffer, twin, size_t( bytes ) ) == 0,
           "defaults changed the packet bytes" );
    return golden( directory, name, buffer, bytes, bits, update );
}

static bool run( const char * directory, bool update )
{
    packetdefaults::EmptyOnly empty;
    CHECK( empty.name_length == 0 && zero_buffer( empty.name ) &&
           empty.token_length == 0 && zero_buffer( empty.token ) && empty.caps == 0,
           "empty-only constructor" );
    packetdefaults::Prefix prefix;
    const uint8_t prefix_name[6] = { 0xc3, 0xa9 }; // C++ also has a zero terminator.
    const uint8_t prefix_token[5] = { 0x5c, 0x6e };
    CHECK( prefix.name_length == 2 && prefix.token_length == 2 &&
           std::memcmp( prefix.name, prefix_name, sizeof( prefix_name ) ) == 0 &&
           std::memcmp( prefix.token, prefix_token, sizeof( prefix_token ) ) == 0,
           "short literal constructor backing tails" );
    packetdefaults::WideMask wide;
    CHECK( wide.high == ( uint64_t( 1 ) << 63 ) && wide.all == ~uint64_t( 0 ),
           "unsigned 64-bit flags defaults" );

    packetdefaults::Sample sample;
    CHECK( sample_defaults( sample ), "sample defaults" );
    packetdefaults::ZeroCount zero_count;
    CHECK( zero_count.items_count == 0, "zero-count constructor count" );
    for ( const auto & item : zero_count.items )
        CHECK( sample_defaults( item ), "zero-count backing defaults" );
    packetplain::ZeroCount plain_zero_count;
    for ( auto & item : plain_zero_count.items )
        item = plain_sample();
    CHECK( compare_and_pin( directory, "zero-count", zero_count, plain_zero_count,
                           packetdefaults::WriteZeroCount, packetplain::WriteZeroCount, update ),
           "zero-count" );
    auto plain = plain_sample();
    CHECK( compare_and_pin( directory, "sample-defaults", sample, plain,
                           packetdefaults::WriteSample, packetplain::WriteSample, update ),
           "sample-defaults" );

    packetdefaults::Batch batch;
    CHECK( sample_defaults( batch.head ), "nested defaults" );
    for ( const auto & item : batch.items )
        CHECK( sample_defaults( item ), "fixed-array defaults" );
    for ( const auto & item : batch.counted )
        CHECK( sample_defaults( item ), "counted backing-array defaults" );
    CHECK( batch.counted_count == 1, "counted-array born count" );
    CHECK( packetplain::Batch{}.counted_count == 1, "born count survives removing defaults" );
    CHECK( compare_and_pin( directory, "batch-defaults", batch, plain_batch(),
                           packetdefaults::WriteBatch, packetplain::WriteBatch, update ),
           "batch-defaults" );

    packetdefaults::Conditional conditional;
    CHECK( conditional.enabled && sample_defaults( conditional.value ), "conditional construction" );
    packetplain::Conditional plain_conditional;
    plain_conditional.enabled = true;
    plain_conditional.value = plain_sample();
    CHECK( compare_and_pin( directory, "conditional-on", conditional, plain_conditional,
                           packetdefaults::WriteConditional, packetplain::WriteConditional, update ),
           "conditional-on" );
    conditional.enabled = false;
    plain_conditional.enabled = false;
    CHECK( compare_and_pin( directory, "conditional-off", conditional, plain_conditional,
                           packetdefaults::WriteConditional, packetplain::WriteConditional, update ),
           "conditional-off" );

    packetdefaults::Choice choice;
    packetplain::Choice plain_choice;
    CHECK( choice.type == packetdefaults::ChoiceType::None &&
           plain_choice.type == packetplain::ChoiceType::None, "union construction is None" );
    choice.type = packetdefaults::ChoiceType::Sample;
    new ( &choice.sample ) packetdefaults::Sample{};
    plain_choice.type = packetplain::ChoiceType::Sample;
    new ( &plain_choice.sample ) packetplain::Sample( plain_sample() );
    CHECK( compare_and_pin( directory, "choice-sample", choice, plain_choice,
                           packetdefaults::WriteChoice, packetplain::WriteChoice, update ),
           "choice-sample" );

    short_sample( sample );
    short_sample( plain );
    CHECK( compare_and_pin( directory, "sample-short", sample, plain,
                           packetdefaults::WriteSample, packetplain::WriteSample, update ),
           "sample-short" );
    empty_sample( sample );
    empty_sample( plain );
    CHECK( compare_and_pin( directory, "sample-empty", sample, plain,
                           packetdefaults::WriteSample, packetplain::WriteSample, update ),
           "sample-empty" );
    return true;
}

int main( int argc, char ** argv )
{
    if ( argc < 2 || argc > 3 || ( argc == 3 && std::strcmp( argv[2], "--write-goldens" ) != 0 ) )
    {
        std::fprintf( stderr, "usage: %s <golden-directory> [--write-goldens]\n", argv[0] );
        return 2;
    }
    if ( !run( argv[1], argc == 3 ) )
        return 1;
    std::puts( "packet defaults C++: construction, defaultless twins and eight wire goldens OK" );
    return 0;
}
