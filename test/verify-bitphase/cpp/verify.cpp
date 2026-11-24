// verify.cpp — the C++ leg of a verify-bitphase case family (--family): it
// reads a corpus another program wrote and holds the generated C++ codec to
// it. A valid row reads and writes back to exactly its bytes; a refuse row is
// refused by the one-packet read. It names only the ten types every family's
// BitPhase.schema declares, so it compiles against any of them; oracle.cpp
// beside it is the main corpus's generator and builds its values field by
// field.
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include "BitPhaseWire.h"

using namespace bitphase;

static void die( const char * msg )
{
    std::fprintf( stderr, "FAILED: %s\n", msg );
    std::exit( 1 );
}

static std::vector<uint8_t> read_file( const std::string & path )
{
    std::ifstream f( path, std::ios::binary | std::ios::ate );
    if ( !f ) die( ( "open read " + path ).c_str() );
    std::streamsize sz = f.tellg();
    f.seekg( 0, std::ios::beg );
    std::vector<uint8_t> out( static_cast<size_t>( sz ) );
    if ( sz > 0 && !f.read( reinterpret_cast<char *>( out.data() ), sz ) )
    {
        die( ( "read " + path ).c_str() );
    }
    return out;
}
template <typename T>
static std::vector<uint8_t> encode_pkt( const T & val, bool ( *write_fn )( serialize::WriteStream &, const T & ), int & out_bits )
{
    uint8_t buf[512] = {};
    serialize::WriteStream ws( buf, sizeof( buf ) );
    if ( !write_fn( ws, val ) )
    {
        die( "encode_pkt failed" );
    }
    out_bits = ws.GetBitsProcessed();
    ws.Flush();
    int nbytes = ws.GetBytesProcessed();
    return std::vector<uint8_t>( buf, buf + nbytes );
}

template <typename T>
static bool check_roundtrip( const std::vector<uint8_t> & raw, T & reuse_val,
                             bool ( *read_fn )( serialize::ReadStream &, T & ),
                             bool ( *write_fn )( serialize::WriteStream &, const T & ) )
{
    std::vector<uint8_t> padded( raw.size() + 16, 0 );
    if ( !raw.empty() ) std::memcpy( padded.data(), raw.data(), raw.size() );
    serialize::ReadStream rs( padded.data(), static_cast<int>( raw.size() ) );
    if ( !read_fn( rs, reuse_val ) )
    {
        return false;
    }
    int bits = 0;
    std::vector<uint8_t> back = encode_pkt( reuse_val, write_fn, bits );
    return back == raw;
}

template <typename T>
static bool check_refused( const std::vector<uint8_t> & raw,
                           bool ( *read_fn )( serialize::ReadStream &, T & ) )
{
    std::vector<uint8_t> padded( raw.size() + 16, 0 );
    if ( !raw.empty() ) std::memcpy( padded.data(), raw.data(), raw.size() );
    T tmp{};
    serialize::ReadStream rs( padded.data(), static_cast<int>( raw.size() ) );
    return !read_fn( rs, tmp );
}
static void verify_corpus( const std::string & dir )
{
    std::vector<uint8_t> mraw = read_file( dir + "/manifest.tsv" );
    std::istringstream in( std::string( mraw.begin(), mraw.end() ) );
    EmptyZero r_ez{};
    Phase0 r0{}; Phase1 r1{}; Phase7 r7{}; Phase31 r31{};
    Phase32 r32{}; Phase33 r33{}; Phase56 r56{}; Phase63 r63{};
    int count = 0;
    std::string kind, type_name, fname;
    while ( in >> kind >> type_name >> fname )
    {
        std::vector<uint8_t> raw = read_file( dir + "/" + fname );
        bool ok = false;
        if ( kind == "valid" )
        {
            if ( type_name == "EmptyZero" ) ok = check_roundtrip( raw, r_ez, ReadEmptyZero, WriteEmptyZero );
            else if ( type_name == "Phase0" ) ok = check_roundtrip( raw, r0, ReadPhase0, WritePhase0 );
            else if ( type_name == "Phase1" ) ok = check_roundtrip( raw, r1, ReadPhase1, WritePhase1 );
            else if ( type_name == "Phase7" ) ok = check_roundtrip( raw, r7, ReadPhase7, WritePhase7 );
            else if ( type_name == "Phase31" ) ok = check_roundtrip( raw, r31, ReadPhase31, WritePhase31 );
            else if ( type_name == "Phase32" ) ok = check_roundtrip( raw, r32, ReadPhase32, WritePhase32 );
            else if ( type_name == "Phase33" ) ok = check_roundtrip( raw, r33, ReadPhase33, WritePhase33 );
            else if ( type_name == "Phase56" ) ok = check_roundtrip( raw, r56, ReadPhase56, WritePhase56 );
            else if ( type_name == "Phase63" ) ok = check_roundtrip( raw, r63, ReadPhase63, WritePhase63 );
        }
        else
        {
            if ( type_name == "EmptyZero" ) ok = check_refused( raw, ReadEmptyZero );
            else if ( type_name == "Phase0" ) ok = check_refused( raw, ReadPhase0 );
            else if ( type_name == "Phase1" ) ok = check_refused( raw, ReadPhase1 );
            else if ( type_name == "Phase7" ) ok = check_refused( raw, ReadPhase7 );
            else if ( type_name == "Phase31" ) ok = check_refused( raw, ReadPhase31 );
            else if ( type_name == "Phase32" ) ok = check_refused( raw, ReadPhase32 );
            else if ( type_name == "Phase33" ) ok = check_refused( raw, ReadPhase33 );
            else if ( type_name == "Phase56" ) ok = check_refused( raw, ReadPhase56 );
            else if ( type_name == "Phase63" ) ok = check_refused( raw, ReadPhase63 );
        }
        if ( !ok )
        {
            die( ( "cpp failed " + kind + " " + type_name + " " + fname ).c_str() );
        }
        count++;
    }
    std::printf( "BITPHASE_OK cpp %d\n", count );
}

int main( int argc, char ** argv )
{
    if ( argc != 3 ) die( "usage: oracle --verify <corpus_dir>" );
    std::string mode = argv[1];
    if ( mode != "--verify" ) die( "only --verify" );
    verify_corpus( argv[2] );
    return 0;
}
