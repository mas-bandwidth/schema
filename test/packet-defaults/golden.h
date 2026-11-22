// Shared by the C++ packet-default oracles: the pin writer and checker.
#pragma once
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

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
