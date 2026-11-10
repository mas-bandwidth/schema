// The packet carrier's C++ driver (test/conformance/packet/carrier.go states
// the contract): write|read <codec> <in> <out>. The generated headers sit
// beside it; one codec per seed root.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <map>
#include <sstream>
#include <string>
#include <vector>

#include "SeedWire.h"

typedef std::vector<int64_t> Fields;
typedef std::vector<uint8_t> Packet;

static bool WriteSeedT( const Fields & f, Packet & packet )
{
    seed::T v;
    size_t at = 0;
    if ( f.size() < 1 )
        return false;
    if ( f[at++] == 1 )
    {
        if ( at >= f.size() )
            return false;
        v.u.SelectA().v = uint8_t( f[at++] );
    }
    for ( int i = 0; i < 4; i++ )
    {
        if ( at >= f.size() )
            return false;
        v.data[i] = uint8_t( f[at++] );
    }
    if ( at != f.size() )
        return false;
    alignas( 8 ) uint8_t buffer[seed::TMaxBytes + 8] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    if ( !seed::WriteT( stream, v ) )
        return false;
    stream.Flush();
    packet.assign( stream.GetData(), stream.GetData() + stream.GetBytesProcessed() );
    return true;
}

static bool ReadSeedT( const Packet & packet, Fields & f )
{
    std::vector<uint8_t> buffer( packet.size() + 8, 0 );
    if ( !packet.empty() )
        memcpy( buffer.data(), packet.data(), packet.size() );
    serialize::ReadStream stream( buffer.data(), int64_t( packet.size() ) );
    seed::T v;
    v.u.SelectA(); // every arm's storage initialized before the read replaces it
    if ( !seed::ReadT( stream, v ) )
        return false;
    f.push_back( int64_t( v.u.type ) );
    if ( v.u.type == seed::UType::A )
        f.push_back( int64_t( v.u.a.v ) );
    for ( int i = 0; i < 4; i++ )
        f.push_back( int64_t( v.data[i] ) );
    return true;
}

struct Codec
{
    bool ( *write )( const Fields &, Packet & );
    bool ( *read )( const Packet &, Fields & );
};

int main( int argc, char ** argv )
{
    if ( argc != 5 )
    {
        fprintf( stderr, "usage: driver write|read <codec> <in> <out>\n" );
        return 1;
    }
    std::map<std::string, Codec> codecs;
    codecs["union-before-bytes"] = Codec{ WriteSeedT, ReadSeedT };
    auto it = codecs.find( argv[2] );
    if ( it == codecs.end() )
    {
        fprintf( stderr, "driver: no codec %s\n", argv[2] );
        return 1;
    }
    const std::string verb = argv[1];
    if ( verb != "write" && verb != "read" )
    {
        fprintf( stderr, "driver: no verb %s\n", argv[1] );
        return 1;
    }
    std::ifstream in( argv[3] );
    if ( !in )
    {
        fprintf( stderr, "driver: cannot open %s\n", argv[3] );
        return 1;
    }
    std::ostringstream out;
    std::string line;
    while ( std::getline( in, line ) )
    {
        if ( verb == "write" )
        {
            Fields fields;
            std::istringstream tokens( line );
            long long n;
            while ( tokens >> n )
                fields.push_back( int64_t( n ) );
            Packet packet;
            if ( it->second.write( fields, packet ) )
            {
                for ( uint8_t b : packet )
                {
                    char hex[3];
                    snprintf( hex, sizeof( hex ), "%02x", b );
                    out << hex;
                }
                out << "\n";
            }
            else
            {
                out << "refused\n";
            }
        }
        else
        {
            Packet packet;
            for ( size_t i = 0; i + 1 < line.size(); i += 2 )
                packet.push_back( uint8_t( std::stoul( line.substr( i, 2 ), nullptr, 16 ) ) );
            Fields fields;
            if ( it->second.read( packet, fields ) )
            {
                out << "ok";
                for ( int64_t n : fields )
                    out << " " << (long long) n;
                out << "\n";
            }
            else
            {
                out << "refused\n";
            }
        }
    }
    std::ofstream file( argv[4] );
    file << out.str();
    return file.good() ? 0 : 1;
}
