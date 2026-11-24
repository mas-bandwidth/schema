// The C++ packet Write of a compressed float (docs/SPEC.md §5.1 row 10 and
// docs/PACKET-WIRE-ALGORITHM.md §2.5). The shared cases file names each value
// and its verdict; a lawful value must write and read back to its quantized
// value, and a refused one must be refused by the debug assert. An assert
// aborts, so every case runs in a child and the parent reads the status: death
// by SIGABRT is the assert's refusal. The child's stderr goes to /dev/null.
#include <cerrno>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include <fcntl.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "PacketCFloatWire.h"

namespace {
// CFloat.x and RatioEndpoint.ratio (PacketCFloat.schema).
struct Range
{
    float min;
    float max;
    float resolution;
};
constexpr Range kCFloat = { 0.0f, 10.0f, 0.01f };
constexpr Range kRatio = { 0.0f, 1.0f, 0.01f };

enum
{
    kWroteExit = 10,
    kWriteFailedExit = 11,
    kReadFailedExit = 12,
    kReadMismatchExit = 13,
    kBadCaseExit = 14,
    kWireMismatchExit = 15,
};

struct Case
{
    std::string name;
    float value = 0.0f;
    bool refused = false;
    bool has_reads_back = false;
    float reads_back = 0.0f;
    bool ratio = false;
    bool has_wire = false;
    std::vector<uint8_t> wire;
};

bool parse_hex( const std::string & text, std::vector<uint8_t> & out )
{
    if ( text.empty() || text.size() % 2 != 0 ) return false;
    for ( size_t i = 0; i < text.size(); i += 2 )
    {
        char * end = nullptr;
        const std::string pair = text.substr( i, 2 );
        const unsigned long byte = std::strtoul( pair.c_str(), &end, 16 );
        if ( end != pair.c_str() + 2 ) return false;
        out.push_back( static_cast<uint8_t>( byte ) );
    }
    return true;
}

uint32_t float_bits( float value )
{
    uint32_t bits = 0;
    std::memcpy( &bits, &value, sizeof( bits ) );
    return bits;
}

// The read side's quantization of a lawful value: the product rounds to
// float32 before 0.5 is added, and the integer clamp is the one §2.5 keeps.
float quantized( float value, const Range & range )
{
    const float kMin = range.min;
    const float delta = range.max - range.min;
    const float values = delta / range.resolution;
    const uint32_t steps = static_cast<uint32_t>( std::ceil( values ) );
    float normalized = ( value - kMin ) / delta;
    if ( !( normalized >= 0.0f ) ) normalized = 0.0f;
    else if ( !( normalized <= 1.0f ) ) normalized = 1.0f;
    float scaled = normalized * static_cast<float>( steps );
    SERIALIZE_FLOAT_FORCE_ROUND( scaled );
    uint32_t integer = static_cast<uint32_t>( std::floor( scaled + 0.5f ) );
    if ( integer > steps ) integer = steps;
    float normalized_read = static_cast<float>( integer ) / static_cast<float>( steps );
    float scaled_read = normalized_read * delta;
    SERIALIZE_FLOAT_FORCE_ROUND( scaled_read );
    return scaled_read + kMin;
}

bool parse_float( const std::string & text, float & out )
{
    errno = 0;
    char * end = nullptr;
    const float value = std::strtof( text.c_str(), &end );
    if ( end == text.c_str() || *end != '\0' ) return false;
    out = value;
    return true;
}

bool read_cases( const char * path, std::vector<Case> & cases )
{
    std::ifstream file( path );
    if ( !file.good() ) return false;
    std::string line;
    while ( std::getline( file, line ) )
    {
        if ( !line.empty() && line.back() == '\r' ) line.pop_back();
        const size_t first = line.find_first_not_of( " \t" );
        if ( first == std::string::npos || line[first] == '#' ) continue;
        std::istringstream fields( line );
        std::vector<std::string> token;
        std::string item;
        while ( fields >> item ) token.push_back( item );
        if ( token.size() < 3 || token.size() % 2 != 1 ) return false;
        Case c;
        c.name = token[0];
        if ( !parse_float( token[1], c.value ) ) return false;
        if ( token[2] == "refused" ) c.refused = true;
        else if ( token[2] != "written" ) return false;
        for ( size_t i = 3; i + 1 < token.size(); i += 2 )
        {
            if ( token[i] == "reads-back" )
            {
                if ( !parse_float( token[i + 1], c.reads_back ) ) return false;
                c.has_reads_back = true;
            }
            else if ( token[i] == "type" )
            {
                if ( token[i + 1] != "RatioEndpoint" ) return false;
                c.ratio = true;
            }
            else if ( token[i] == "wire" )
            {
                if ( !parse_hex( token[i + 1], c.wire ) ) return false;
                c.has_wire = true;
            }
            else
            {
                return false;
            }
        }
        cases.push_back( c );
    }
    return true;
}

int run_case( const Case & c )
{
    const int quiet = open( "/dev/null", O_WRONLY );
    if ( quiet >= 0 ) dup2( quiet, 2 );

    cfloat::CFloat value;
    value.x = c.value;
    cfloat::RatioEndpoint ratio;
    ratio.ratio = c.value;

    alignas( 8 ) uint8_t buffer[64] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    const bool written = c.ratio ? cfloat::WriteRatioEndpoint( stream, ratio ) : cfloat::WriteCFloat( stream, value );
    if ( !written ) _exit( kWriteFailedExit );
    if ( c.refused ) _exit( kWroteExit );

    stream.Flush();
    const int64_t bytes = stream.GetBytesProcessed();
    if ( bytes <= 0 || bytes > int64_t( sizeof( buffer ) ) ) _exit( kReadFailedExit );
    if ( c.has_wire && ( bytes != int64_t( c.wire.size() ) || std::memcmp( buffer, c.wire.data(), c.wire.size() ) != 0 ) )
        _exit( kWireMismatchExit );

    float got = 0.0f;
    serialize::ReadStream reader( buffer, bytes );
    if ( c.ratio )
    {
        cfloat::RatioEndpoint readback;
        if ( !cfloat::ReadRatioEndpoint( reader, readback ) ) _exit( kReadFailedExit );
        got = readback.ratio;
    }
    else
    {
        cfloat::CFloat readback;
        if ( !cfloat::ReadCFloat( reader, readback ) ) _exit( kReadFailedExit );
        got = readback.x;
    }

    const float expected = quantized( c.value, c.ratio ? kRatio : kCFloat );
    if ( c.has_reads_back && float_bits( expected ) != float_bits( c.reads_back ) ) _exit( kReadMismatchExit );
    if ( float_bits( got ) != float_bits( expected ) ) _exit( kReadMismatchExit );
    _exit( kWroteExit );
}
} // namespace

int main( int argc, char ** argv )
{
    if ( argc != 2 )
    {
        std::fprintf( stderr, "usage: %s <cases.txt>\n", argv[0] );
        return 2;
    }
    std::vector<Case> cases;
    if ( !read_cases( argv[1], cases ) || cases.empty() )
    {
        std::fprintf( stderr, "FAILED: cannot read the shared cases from %s\n", argv[1] );
        return 1;
    }

    int failures = 0;
    for ( const Case & c : cases )
    {
        std::fflush( stdout );
        std::fflush( stderr );
        const pid_t child = fork();
        if ( child < 0 )
        {
            std::fprintf( stderr, "FAILED: %s: fork failed\n", c.name.c_str() );
            return 1;
        }
        if ( child == 0 ) _exit( run_case( c ) );
        int status = 0;
        if ( waitpid( child, &status, 0 ) != child )
        {
            std::fprintf( stderr, "FAILED: %s: waitpid failed\n", c.name.c_str() );
            return 1;
        }
        const bool aborted = WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
        const bool wrote = WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit;
        if ( c.refused )
        {
            if ( aborted ) continue;
            if ( wrote )
            {
                std::fprintf( stderr, "FAILED: %s: write was not refused (debug assert)\n", c.name.c_str() );
                ++failures;
            }
            else
            {
                std::fprintf( stderr, "FAILED: %s: child ended with status 0x%x\n", c.name.c_str(), status );
                ++failures;
            }
            continue;
        }
        if ( wrote ) continue;
        if ( aborted )
        {
            std::fprintf( stderr, "FAILED: %s: lawful write was refused (debug assert)\n", c.name.c_str() );
            ++failures;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kReadMismatchExit )
        {
            std::fprintf( stderr, "FAILED: %s: read-back is not the quantized value\n", c.name.c_str() );
            ++failures;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWireMismatchExit )
        {
            std::fprintf( stderr, "FAILED: %s: the written bytes are not the case's wire\n", c.name.c_str() );
            ++failures;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWriteFailedExit )
        {
            std::fprintf( stderr, "FAILED: %s: write returned false\n", c.name.c_str() );
            ++failures;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kReadFailedExit )
        {
            std::fprintf( stderr, "FAILED: %s: read-back failed\n", c.name.c_str() );
            ++failures;
        }
        else
        {
            std::fprintf( stderr, "FAILED: %s: child ended with status 0x%x\n", c.name.c_str(), status );
            ++failures;
        }
    }

    if ( failures != 0 )
    {
        std::fprintf( stderr, "FAILED: %d compressed-float packet case(s)\n", failures );
        return 1;
    }
    std::printf( "packet-write-cfloat-cpp: %zu cases passed\n", cases.size() );
    return 0;
}
