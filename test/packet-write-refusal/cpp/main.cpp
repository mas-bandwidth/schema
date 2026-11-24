// The write-refusal-packet surface (docs/SPEC.md §5.1) in the C++ leg's own
// mechanism: a debug assert. Each row of the one shared list,
// test/conformance/write-refusal-packet/rows.txt (<name> <verdict> <op> <arg>,
// keyed here by name; op and arg are the probes'), changes ONE
// field of a default PacketWriteRefusal and runs the generated packet Write in
// a forked child, so an abort is the refusal and the parent never dies with
// it. A written twin must be written, read back and have the same bit count.
// A row in the file this driver does not run, or a setter it runs that the
// file does not name, fails.
//
// A refusal must be the GENERATED assert, named by the text of its expression
// on the child's stderr (refusedBy below). The runtime's write_bits asserts
// value_fits_in_bits on its own uint64 argument, so a mask bit above a flags
// width aborts there too: an abort alone stays green with the generated flags
// check deleted, and the expression is what tells them apart.
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include <map>
#include <string>
#include <vector>

#include "RefusalPacketWire.h"

using namespace packetrefusal;

typedef void ( *Setter )( PacketWriteRefusal & value );

static void fillText( PacketWriteRefusal & v )
{
    for ( int i = 0; i < 8; i++ )
    {
        v.text[i] = 'a';
    }
    v.text_length = 8;
}

static void scalarHigh( PacketWriteRefusal & v ) { v.scalar = 11; }
static void scalarLow( PacketWriteRefusal & v ) { v.scalar = -1; }
static void scalarAtMax( PacketWriteRefusal & v ) { v.scalar = 10; }
static void scalarAtMin( PacketWriteRefusal & v ) { v.scalar = 0; }
static void bitsWide( PacketWriteRefusal & v ) { v.narrow = 200; }
static void bitsJustWide( PacketWriteRefusal & v ) { v.narrow = 128; }
static void bitsAtMax( PacketWriteRefusal & v ) { v.narrow = 127; }
static void enumHigh( PacketWriteRefusal & v ) { v.grade = static_cast<Grade>( 4 ); }
static void enumAtMax( PacketWriteRefusal & v ) { v.grade = Grade::Gold; }
static void unionTagHigh( PacketWriteRefusal & v ) { v.shot.type = static_cast<ShotType>( 3 ); }
static void unionTagAtMax( PacketWriteRefusal & v ) { v.shot.SelectMissile(); }
static void countHigh( PacketWriteRefusal & v ) { v.window_count = 9; }
static void countLow( PacketWriteRefusal & v ) { v.window_count = 1; }
static void countAtMax( PacketWriteRefusal & v ) { v.window_count = 8; }
static void countAtMin( PacketWriteRefusal & v ) { v.window_count = 2; }
static void textLong( PacketWriteRefusal & v ) { v.text_length = 9; }
static void textIllformed( PacketWriteRefusal & v ) { v.text[0] = (char) 0xff; v.text_length = 1; }
static void textZero( PacketWriteRefusal & v ) { v.text[0] = 0; v.text_length = 1; }
static void textAtMax( PacketWriteRefusal & v ) { fillText( v ); }
static void constWrong( PacketWriteRefusal & v ) { v.fixed_constant = 6; }
static void constAt( PacketWriteRefusal & v ) { v.fixed_constant = 5; }
static void reservedWrong( PacketWriteRefusal & v ) { v.reserved_zero = 1; }
static void reservedAt( PacketWriteRefusal & v ) { v.reserved_zero = 0; }
static void flagsWide( PacketWriteRefusal & v ) { v.perks = 1ull << 3; }
static void flagsWideHigh( PacketWriteRefusal & v ) { v.perks = 1ull << 40; }
static void flagsAtMax( PacketWriteRefusal & v ) { v.perks = Perk_Shield | Perk_Boost | Perk_Cloak; }

static std::map<std::string, Setter> setters()
{
    std::map<std::string, Setter> m;
    // write-refusal-packet setters: begin
    m["scalar_high"] = scalarHigh;
    m["scalar_low"] = scalarLow;
    m["scalar_at_max"] = scalarAtMax;
    m["scalar_at_min"] = scalarAtMin;
    m["bits_wide"] = bitsWide;
    m["bits_just_wide"] = bitsJustWide;
    m["bits_at_max"] = bitsAtMax;
    m["enum_high"] = enumHigh;
    m["enum_at_max"] = enumAtMax;
    m["union_tag_high"] = unionTagHigh;
    m["union_tag_at_max"] = unionTagAtMax;
    m["count_high"] = countHigh;
    m["count_low"] = countLow;
    m["count_at_max"] = countAtMax;
    m["count_at_min"] = countAtMin;
    m["text_long"] = textLong;
    m["text_illformed"] = textIllformed;
    m["text_zero"] = textZero;
    m["text_at_max"] = textAtMax;
    m["const_wrong"] = constWrong;
    m["const_at"] = constAt;
    m["reserved_wrong"] = reservedWrong;
    m["reserved_at"] = reservedAt;
    m["flags_wide"] = flagsWide;
    m["flags_wide_high"] = flagsWideHigh;
    m["flags_at_max"] = flagsAtMax;
    // write-refusal-packet setters: end
    return m;
}

// refusedBy names, for every refused case, a fragment of the generated
// assert's expression that must appear in the abort message. The flags rows
// carry the whole comparison: the runtime's own assert also names
// value.perks, inside value_fits_in_bits.
static std::map<std::string, std::string> refusedBy()
{
    std::map<std::string, std::string> m;
    m["scalar_high"] = "value.scalar";
    m["scalar_low"] = "value.scalar";
    m["bits_wide"] = "value.narrow";
    m["bits_just_wide"] = "value.narrow";
    m["enum_high"] = "value.grade";
    m["union_tag_high"] = "value.type";
    m["count_high"] = "value.window_count";
    m["count_low"] = "value.window_count";
    m["text_long"] = "value.text";
    m["text_illformed"] = "value.text";
    m["text_zero"] = "value.text";
    m["const_wrong"] = "value.fixed_constant";
    m["reserved_wrong"] = "value.reserved_zero";
    m["flags_wide"] = "value.perks < ( 1ull << 3 )";
    m["flags_wide_high"] = "value.perks < ( 1ull << 3 )";
    return m;
}

struct Case
{
    std::string name;
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

static bool parseCases( const char * path, std::vector<Case> & cases )
{
    FILE * file = fopen( path, "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "packet-write-refusal-cpp: cannot open %s\n", path );
        return false;
    }
    char line[512];
    while ( fgets( line, sizeof( line ), file ) != NULL )
    {
        const std::string text = trim( line );
        if ( text.empty() || text[0] == '#' )
        {
            continue;
        }
        char name[128];
        char verdictText[32];
        char op[64];
        char arg[128];
        char extra[2];
        if ( sscanf( text.c_str(), "%127s %31s %63s %127s %1s", name, verdictText, op, arg, extra ) != 4 )
        {
            fprintf( stderr, "packet-write-refusal-cpp: a row is not <name> <verdict> <op> <arg>: %s\n", text.c_str() );
            fclose( file );
            return false;
        }
        Case c;
        c.name = name;
        const std::string verdict = verdictText;
        if ( verdict != "refused" && verdict != "wrote" )
        {
            fprintf( stderr, "packet-write-refusal-cpp: unknown verdict %s\n", verdict.c_str() );
            fclose( file );
            return false;
        }
        c.refused = verdict == "refused";
        cases.push_back( c );
    }
    fclose( file );
    return true;
}

static int runChild( Setter set )
{
    PacketWriteRefusal value;
    set( value );
    unsigned char storage[256] = {0};
    serialize::WriteStream write_stream( storage, sizeof( storage ) );
    if ( !WritePacketWriteRefusal( write_stream, value ) )
    {
        return 10;
    }
    write_stream.Flush();
    PacketWriteRefusal back;
    serialize::ReadStream read_stream( storage, (int32_t) write_stream.GetBytesProcessed() );
    if ( !ReadPacketWriteRefusal( read_stream, back ) )
    {
        return 11;
    }
    if ( back.scalar != value.scalar || back.narrow != value.narrow || back.window_count != value.window_count ||
         back.text_length != value.text_length || back.grade != value.grade || back.shot.type != value.shot.type ||
         back.perks != value.perks )
    {
        return 12;
    }
    if ( read_stream.GetBitsProcessed() != write_stream.GetBitsProcessed() )
    {
        return 13;
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
        fprintf( stderr, "usage: %s <rows.txt>\n", argv[0] );
        return 2;
    }
    std::vector<Case> cases;
    if ( !parseCases( argv[1], cases ) )
    {
        return 2;
    }
    const std::map<std::string, Setter> table = setters();
    const std::map<std::string, std::string> expected = refusedBy();
    int failures = 0;
    int refused = 0;
    int written = 0;
    std::map<std::string, bool> named;
    for ( size_t i = 0; i < cases.size(); i++ )
    {
        const Case & c = cases[i];
        if ( named.find( c.name ) != named.end() )
        {
            printf( "FAILED: packet-write-refusal-cpp: %s: named twice in rows.txt\n", c.name.c_str() );
            failures++;
            continue;
        }
        named[c.name] = true;
        const std::map<std::string, Setter>::const_iterator it = table.find( c.name );
        if ( it == table.end() )
        {
            printf( "FAILED: packet-write-refusal-cpp: %s: a row in rows.txt this driver has no setter for\n", c.name.c_str() );
            failures++;
            continue;
        }
        if ( c.refused )
        {
            refused++;
        }
        else
        {
            written++;
        }
        const std::map<std::string, std::string>::const_iterator want = expected.find( c.name );
        if ( c.refused && want == expected.end() )
        {
            printf( "FAILED: packet-write-refusal-cpp: %s: refused, and refusedBy names no generated assert for it\n", c.name.c_str() );
            failures++;
            continue;
        }
        int pipe_fds[2];
        if ( pipe( pipe_fds ) != 0 )
        {
            perror( "packet-write-refusal-cpp: pipe" );
            return 2;
        }
        fflush( stdout );
        fflush( stderr );
        const pid_t pid = fork();
        if ( pid < 0 )
        {
            perror( "packet-write-refusal-cpp: fork" );
            return 2;
        }
        if ( pid == 0 )
        {
            close( pipe_fds[0] );
            if ( dup2( pipe_fds[1], 2 ) < 0 )
            {
                _exit( 20 );
            }
            const int code = runChild( it->second );
            fflush( stdout );
            fflush( stderr );
            _exit( code );
        }
        close( pipe_fds[1] );
        std::string message;
        char chunk[512];
        ssize_t got;
        while ( ( got = read( pipe_fds[0], chunk, sizeof( chunk ) ) ) > 0 )
        {
            message.append( chunk, (size_t) got );
        }
        close( pipe_fds[0] );
        fputs( message.c_str(), stderr ); // the child's assert line, as before
        int status = 0;
        if ( waitpid( pid, &status, 0 ) != pid )
        {
            perror( "packet-write-refusal-cpp: waitpid" );
            return 2;
        }
        const bool aborted = WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
        const bool ok = c.refused ? ( aborted && message.find( want->second ) != std::string::npos )
                                  : ( WIFEXITED( status ) && WEXITSTATUS( status ) == 0 );
        if ( !ok )
        {
            if ( c.refused && aborted )
            {
                printf( "FAILED: packet-write-refusal-cpp: %s: aborted, but not by the generated assert on %s\n", c.name.c_str(),
                        want->second.c_str() );
            }
            else
            {
                printf( "FAILED: packet-write-refusal-cpp: %s: expected %s, child %s\n", c.name.c_str(),
                        c.refused ? "a debug assert" : "a written value", describe( status ).c_str() );
            }
            failures++;
        }
    }
    for ( std::map<std::string, Setter>::const_iterator it = table.begin(); it != table.end(); ++it )
    {
        if ( named.find( it->first ) == named.end() )
        {
            printf( "FAILED: packet-write-refusal-cpp: %s: a setter this driver runs that rows.txt does not name\n", it->first.c_str() );
            failures++;
        }
    }
    printf( "packet-write-refusal-cpp: %zu rows, %d refused, %d written\n", cases.size(), refused, written );
    return failures == 0 ? 0 : 1;
}
