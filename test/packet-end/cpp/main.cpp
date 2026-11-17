// The C++ driver for the shared packet-end cases: each vector is read with the
// generated public Read<T>, which must accept the exact packet and refuse a
// whole trailing byte and nonzero unused bits in the final byte.
#include <cstdio>
#include <cstring>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

#include "EndWire.h"

static bool load( const std::string & path, std::vector<uint8_t> & out )
{
    std::ifstream file( path, std::ios::binary );
    if ( !file.good() )
    {
        return false;
    }
    out.assign( std::istreambuf_iterator<char>( file ), {} );
    return true;
}

static bool read_packet( const std::vector<uint8_t> & packet, packetend::End & value, int64_t & bits )
{
    std::vector<uint8_t> buffer( packet.size() + 8, 0 );
    if ( !packet.empty() )
    {
        std::memcpy( buffer.data(), packet.data(), packet.size() );
    }
    serialize::ReadStream stream( buffer.data(), int64_t( packet.size() ) );
    if ( !packetend::ReadEnd( stream, value ) )
    {
        return false;
    }
    bits = stream.GetBitsProcessed();
    return true;
}

int main( int argc, char ** argv )
{
    if ( argc != 2 )
    {
        std::fprintf( stderr, "usage: %s <packet-end-directory>\n", argv[0] );
        return 2;
    }
    const std::string directory = argv[1];

    std::vector<uint8_t> exact;
    if ( !load( directory + "/exact.bin", exact ) )
    {
        std::fprintf( stderr, "FAILED: packet end exact: cannot read the case\n" );
        return 1;
    }
    packetend::End exact_value;
    int64_t exact_bits = 0;
    if ( !read_packet( exact, exact_value, exact_bits ) )
    {
        std::fprintf( stderr, "FAILED: packet end exact: the exact packet was refused\n" );
        return 1;
    }
    if ( exact_value.value != 3 )
    {
        std::fprintf( stderr, "FAILED: packet end exact: value %u, want 3\n", unsigned( exact_value.value ) );
        return 1;
    }
    if ( exact_bits != 3 )
    {
        std::fprintf( stderr, "FAILED: packet end exact: %lld bits consumed, want 3\n", (long long) exact_bits );
        return 1;
    }

    std::vector<uint8_t> trailing;
    if ( !load( directory + "/trailing.bin", trailing ) )
    {
        std::fprintf( stderr, "FAILED: packet end trailing: cannot read the case\n" );
        return 1;
    }
    packetend::End trailing_value;
    int64_t trailing_bits = 0;
    if ( read_packet( trailing, trailing_value, trailing_bits ) )
    {
        std::fprintf( stderr, "FAILED: packet end trailing: a packet with one whole trailing byte was accepted\n" );
        return 1;
    }

    std::vector<uint8_t> padding;
    if ( !load( directory + "/padding.bin", padding ) )
    {
        std::fprintf( stderr, "FAILED: packet end padding: cannot read the case\n" );
        return 1;
    }
    packetend::End padding_value;
    int64_t padding_bits = 0;
    if ( read_packet( padding, padding_value, padding_bits ) )
    {
        std::fprintf( stderr, "FAILED: packet end padding: a packet with nonzero unused bits was accepted\n" );
        return 1;
    }

    std::printf( "packet end cpp: exact accepted as 3; trailing refused; padding refused\n" );
    return 0;
}
