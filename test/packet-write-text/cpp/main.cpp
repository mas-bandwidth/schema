// The packet Write text contract (SPEC §5.1 row 8) in the C++ leg's own
// mechanism: a debug assert. The shared cases are each run in a child, so an
// abort is the refusal and the parent never dies with it. A written case is
// written, read back and written again, byte exact.
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include <string>
#include <vector>

#include "TextPacketWire.h"

using namespace packetwritetext;

struct Case
{
    std::string name;
    std::vector<unsigned char> bytes;
    bool refused;
};

static std::string trim( const std::string & text )
{
    const char * space = " \t\r\n";
    const std::string::size_type first = text.find_first_not_of( space );
    if ( first == std::string::npos )
    {
        return std::string();
    }
    const std::string::size_type last = text.find_last_not_of( space );
    return text.substr( first, last - first + 1 );
}

static bool parseHex( const std::string & field, std::vector<unsigned char> & bytes )
{
    bytes.clear();
    if ( trim( field ) == "-" )
    {
        return true;
    }
    const char * at = field.c_str();
    while ( *at != 0 )
    {
        while ( *at == ' ' || *at == '\t' )
        {
            at++;
        }
        if ( *at == 0 )
        {
            break;
        }
        char * end = NULL;
        const unsigned long value = strtoul( at, &end, 16 );
        if ( end == at || value > 0xFF )
        {
            return false;
        }
        bytes.push_back( (unsigned char) value );
        at = end;
    }
    return true;
}

static bool parseCases( const char * path, std::vector<Case> & cases )
{
    FILE * file = fopen( path, "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "packet-write-text-cpp: cannot open %s\n", path );
        return false;
    }
    char line[512];
    while ( fgets( line, sizeof( line ), file ) != NULL )
    {
        std::string text = trim( line );
        if ( text.empty() || text[0] == '#' )
        {
            continue;
        }
        const std::string::size_type first = text.find( '|' );
        const std::string::size_type second = first == std::string::npos ? std::string::npos : text.find( '|', first + 1 );
        if ( first == std::string::npos || second == std::string::npos )
        {
            fprintf( stderr, "packet-write-text-cpp: malformed case line: %s\n", text.c_str() );
            fclose( file );
            return false;
        }
        Case c;
        c.name = trim( text.substr( 0, first ) );
        const std::string verdict = trim( text.substr( second + 1 ) );
        if ( c.name.empty() || !parseHex( text.substr( first + 1, second - first - 1 ), c.bytes ) )
        {
            fprintf( stderr, "packet-write-text-cpp: malformed case line: %s\n", text.c_str() );
            fclose( file );
            return false;
        }
        if ( verdict == "refused" )
        {
            c.refused = true;
        }
        else if ( verdict == "written" )
        {
            c.refused = false;
        }
        else
        {
            fprintf( stderr, "packet-write-text-cpp: unknown verdict %s\n", verdict.c_str() );
            fclose( file );
            return false;
        }
        cases.push_back( c );
    }
    fclose( file );
    return true;
}

static int runChild( const Case & c )
{
    TextPacket value;
    memset( value.text, 0, sizeof( value.text ) );
    if ( !c.bytes.empty() )
    {
        memcpy( value.text, c.bytes.data(), c.bytes.size() );
    }
    value.text_length = (int32_t) c.bytes.size();

    unsigned char storage[256] = {0};
    serialize::WriteStream write_stream( storage, sizeof( storage ) );
    if ( !WriteTextPacket( write_stream, value ) )
    {
        return 10;
    }
    write_stream.Flush();
    if ( c.refused )
    {
        return 0;
    }

    TextPacket back;
    memset( back.text, 0x7f, sizeof( back.text ) );
    serialize::ReadStream read_stream( storage, (int32_t) write_stream.GetBytesProcessed() );
    if ( !ReadTextPacket( read_stream, back ) )
    {
        return 11;
    }
    if ( back.text_length != value.text_length )
    {
        return 12;
    }
    if ( value.text_length != 0 && memcmp( back.text, value.text, (size_t) value.text_length ) != 0 )
    {
        return 13;
    }
    if ( back.text[back.text_length] != 0 )
    {
        return 14;
    }
    if ( read_stream.GetBitsProcessed() != write_stream.GetBitsProcessed() )
    {
        return 15;
    }

    unsigned char again[256] = {0};
    serialize::WriteStream rewrite_stream( again, sizeof( again ) );
    if ( !WriteTextPacket( rewrite_stream, back ) )
    {
        return 16;
    }
    rewrite_stream.Flush();
    if ( rewrite_stream.GetBytesProcessed() != write_stream.GetBytesProcessed() ||
         memcmp( again, storage, write_stream.GetBytesProcessed() ) != 0 )
    {
        return 17;
    }
    return 0;
}

static std::string describe( int status )
{
    char text[64];
    if ( WIFSIGNALED( status ) )
    {
        snprintf( text, sizeof( text ), "aborted by signal %d", WTERMSIG( status ) );
        return text;
    }
    if ( WIFEXITED( status ) )
    {
        snprintf( text, sizeof( text ), "exited %d", WEXITSTATUS( status ) );
        return text;
    }
    return "did not terminate";
}

int main( int argc, char ** argv )
{
    if ( argc != 2 )
    {
        fprintf( stderr, "usage: %s <cases.txt>\n", argv[0] );
        return 2;
    }
    std::vector<Case> cases;
    if ( !parseCases( argv[1], cases ) )
    {
        return 2;
    }

    int failures = 0;
    int refused = 0;
    int written = 0;
    for ( size_t i = 0; i < cases.size(); i++ )
    {
        const Case & c = cases[i];
        if ( c.refused )
        {
            refused++;
        }
        else
        {
            written++;
        }
        fflush( stdout );
        fflush( stderr );
        const pid_t pid = fork();
        if ( pid < 0 )
        {
            perror( "packet-write-text-cpp: fork" );
            return 2;
        }
        if ( pid == 0 )
        {
            const int code = runChild( c );
            fflush( stdout );
            fflush( stderr );
            _exit( code );
        }
        int status = 0;
        if ( waitpid( pid, &status, 0 ) != pid )
        {
            perror( "packet-write-text-cpp: waitpid" );
            return 2;
        }
        const bool ok = c.refused ? ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT )
                                  : ( WIFEXITED( status ) && WEXITSTATUS( status ) == 0 );
        if ( !ok )
        {
            printf( "FAILED: packet-write-text-cpp: %s: expected %s, child %s\n", c.name.c_str(),
                    c.refused ? "a debug assert" : "a written value", describe( status ).c_str() );
            failures++;
        }
    }

    printf( "packet-write-text-cpp: %zu cases, %d refused, %d written\n", cases.size(), refused, written );
    return failures == 0 ? 0 : 1;
}
