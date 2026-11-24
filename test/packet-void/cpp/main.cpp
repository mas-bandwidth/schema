// The C++ leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. "read" or "write" as the second argument runs one direction
// only: the make/checks/packet-void.mk negative controls sabotage one
// direction and require the other to stay green. A refuse-write case runs
// the write in a forked child, so the debug assert's abort is the refusal
// and the driver never dies with it; a -DNDEBUG build has no write check by
// design and answers it ok without writing.
#include "VoidWire.h"
#include <csignal>
#include <sys/wait.h>
#include <unistd.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <sstream>
#include <string>
#include <vector>

using namespace packetvoid;

enum class Kind { Mixed, Late, Tags, Single, Empty, Frame, Batch, Unknown };

struct Value
{
    Kind kind = Kind::Unknown;
    packetvoid::Mixed mixed;
    packetvoid::Late late;
    packetvoid::Tags tags;
    packetvoid::Single single;
    packetvoid::Empty empty;
    packetvoid::Frame frame;
    packetvoid::Batch batch;

    // A union's constructor sets the tag only; every arm storage here is
    // constructed once, so no read of a value ever sees indeterminate bytes.
    Value()
    {
        fresh( mixed );
        fresh( late );
        fresh( frame.choice );
        for ( packetvoid::Mixed & m : batch.values ) fresh( m );
    }

  private:
    static void fresh( packetvoid::Mixed & m ) { m.SelectPayload(); m.type = MixedType::None; }
    static void fresh( packetvoid::Late & l ) { l.SelectPayload(); l.type = LateType::None; }
};

static bool g_read = true, g_write = true;
static std::string g_why;

static bool fail( const std::string & why )
{
    g_why = why;
    return false;
}

static Kind kind_of( const std::string & type )
{
    if ( type == "Mixed" ) return Kind::Mixed;
    if ( type == "Late" ) return Kind::Late;
    if ( type == "Tags" ) return Kind::Tags;
    if ( type == "Single" ) return Kind::Single;
    if ( type == "Empty" ) return Kind::Empty;
    if ( type == "Frame" ) return Kind::Frame;
    if ( type == "Batch" ) return Kind::Batch;
    return Kind::Unknown;
}

static void set_mixed( packetvoid::Mixed & m, MixedType type )
{
    if ( type == MixedType::Payload )
        m.SelectPayload();
    else
        m.type = type;
}

// The value of each accept case, by name; false for a name this driver does
// not know, which is FAILED and never written with another case's value.
static bool build( const std::string & name, Value & v )
{
    if ( name == "mixed-none" ) { v.kind = Kind::Mixed; set_mixed( v.mixed, MixedType::None ); return true; }
    if ( name == "mixed-ping" ) { v.kind = Kind::Mixed; set_mixed( v.mixed, MixedType::Ping ); return true; }
    if ( name == "mixed-payload" ) { v.kind = Kind::Mixed; set_mixed( v.mixed, MixedType::Payload ); return true; }
    if ( name == "late-payload" ) { v.kind = Kind::Late; v.late.SelectPayload(); return true; }
    if ( name == "late-pong" ) { v.kind = Kind::Late; v.late.SelectPong(); return true; }
    if ( name == "tags-none" ) { v.kind = Kind::Tags; v.tags.type = TagsType::None; return true; }
    if ( name == "tags-ping" ) { v.kind = Kind::Tags; v.tags.type = TagsType::Ping; return true; }
    if ( name == "tags-pong" ) { v.kind = Kind::Tags; v.tags.type = TagsType::Pong; return true; }
    if ( name == "single-none" ) { v.kind = Kind::Single; v.single.type = SingleType::None; return true; }
    if ( name == "single-ack" ) { v.kind = Kind::Single; v.single.type = SingleType::Ack; return true; }
    if ( name == "empty-none" ) { v.kind = Kind::Empty; v.empty.type = EmptyType::None; return true; }
    if ( name == "frame-ping" )
    {
        v.kind = Kind::Frame; v.frame.lead = 5; set_mixed( v.frame.choice, MixedType::Ping ); v.frame.tail = 6;
        return true;
    }
    if ( name == "batch-empty" ) { v.kind = Kind::Batch; v.batch.values_count = 0; return true; }
    if ( name == "batch-ping" )
    {
        v.kind = Kind::Batch; v.batch.values_count = 1; set_mixed( v.batch.values[0], MixedType::Ping );
        return true;
    }
    if ( name == "batch-ping-payload" )
    {
        v.kind = Kind::Batch; v.batch.values_count = 2;
        set_mixed( v.batch.values[0], MixedType::Ping );
        set_mixed( v.batch.values[1], MixedType::Payload );
        return true;
    }
    return false;
}

// Every read lands on storage dirtied first: a selected payload must be
// constructed with its declared defaults, whatever the storage held.
static void dirty( Value & v, Kind kind )
{
    v.kind = kind;
    DefaultArm & arm = v.mixed.SelectPayload();
    arm.entries_count = 2; arm.entries[0].retries = 99; arm.entries[1].retries = 99; arm.marker = 1;
    DefaultArm & late = v.late.SelectPayload();
    late.entries_count = 2; late.entries[0].retries = 99; late.entries[1].retries = 99; late.marker = 1;
    for ( packetvoid::Mixed & m : v.batch.values )
    {
        DefaultArm & a = m.SelectPayload();
        a.entries[0].retries = 99; a.entries[1].retries = 99;
    }
    v.frame.lead = 1; v.frame.tail = 1;
    v.tags.type = TagsType::Max; v.single.type = SingleType::Max;
}

static bool write_value( serialize::WriteStream & w, const Value & v )
{
    switch ( v.kind )
    {
    case Kind::Mixed: return WriteMixed( w, v.mixed );
    case Kind::Late: return WriteLate( w, v.late );
    case Kind::Tags: return WriteTags( w, v.tags );
    case Kind::Single: return WriteSingle( w, v.single );
    case Kind::Empty: return WriteEmpty( w, v.empty );
    case Kind::Frame: return WriteFrame( w, v.frame );
    case Kind::Batch: return WriteBatch( w, v.batch );
    default: return false;
    }
}

static bool read_value( serialize::ReadStream & r, Value & v )
{
    switch ( v.kind )
    {
    case Kind::Mixed: return ReadMixed( r, v.mixed );
    case Kind::Late: return ReadLate( r, v.late );
    case Kind::Tags: return ReadTags( r, v.tags );
    case Kind::Single: return ReadSingle( r, v.single );
    case Kind::Empty: return ReadEmpty( r, v.empty );
    case Kind::Frame: return ReadFrame( r, v.frame );
    case Kind::Batch: return ReadBatch( r, v.batch );
    default: return false;
    }
}

static bool mixed_equal( const packetvoid::Mixed & got, const packetvoid::Mixed & want )
{
    if ( got.type != want.type ) return false;
    if ( want.type != MixedType::Payload ) return true; // unselected arm bytes are indeterminate
    // every backing entry, used or not, holds what the arm's construction
    // gave it: a read constructs the arm before decoding
    return got.payload.entries_count == want.payload.entries_count && got.payload.marker == want.payload.marker &&
           got.payload.entries[0].retries == want.payload.entries[0].retries &&
           got.payload.entries[1].retries == want.payload.entries[1].retries;
}

static bool late_equal( const packetvoid::Late & got, const packetvoid::Late & want )
{
    if ( got.type != want.type ) return false;
    if ( want.type != LateType::Payload ) return true;
    return got.payload.entries_count == want.payload.entries_count && got.payload.marker == want.payload.marker &&
           got.payload.entries[0].retries == want.payload.entries[0].retries &&
           got.payload.entries[1].retries == want.payload.entries[1].retries;
}

static bool value_equal( const Value & got, const Value & want )
{
    switch ( want.kind )
    {
    case Kind::Mixed: return mixed_equal( got.mixed, want.mixed );
    case Kind::Late: return late_equal( got.late, want.late );
    case Kind::Tags: return got.tags.type == want.tags.type;
    case Kind::Single: return got.single.type == want.single.type;
    case Kind::Empty: return got.empty.type == want.empty.type;
    case Kind::Frame:
        return got.frame.lead == want.frame.lead && got.frame.tail == want.frame.tail &&
               mixed_equal( got.frame.choice, want.frame.choice );
    case Kind::Batch:
        if ( got.batch.values_count != want.batch.values_count ) return false;
        for ( int32_t i = 0; i < want.batch.values_count; ++i )
            if ( !mixed_equal( got.batch.values[i], want.batch.values[i] ) ) return false;
        return true;
    default: return false;
    }
}

// A pin in a padded, aligned allocation; its logical length stays exact.
static bool load( const std::string & dir, const std::string & name, std::vector<uint64_t> & words, int64_t & bytes )
{
    std::ifstream file( dir + "/" + name + ".bin", std::ios::binary );
    if ( !file.good() ) return fail( "cannot open " + dir + "/" + name + ".bin" );
    std::vector<char> data( ( std::istreambuf_iterator<char>( file ) ), {} );
    words.assign( data.size() / 8 + 2, 0 );
    if ( !data.empty() ) std::memcpy( words.data(), data.data(), data.size() );
    bytes = int64_t( data.size() );
    return true;
}

static bool accept_case( const std::string & dir, const std::string & name, const std::string & type, int64_t bits )
{
    Value want;
    if ( !build( name, want ) ) return fail( "unknown accept case" );
    if ( want.kind != kind_of( type ) ) return fail( "case type " + type + ", the driver builds another" );
    std::vector<uint64_t> words;
    int64_t bytes = 0;
    if ( !load( dir, name, words, bytes ) ) return false;
    const uint8_t * pin = reinterpret_cast<const uint8_t *>( words.data() );
    if ( g_write )
    {
        alignas( 8 ) uint8_t out[64] = {};
        serialize::WriteStream w( out, sizeof( out ) );
        if ( !write_value( w, want ) ) return fail( "write refused" );
        if ( w.GetBitsProcessed() != bits )
            return fail( "write has " + std::to_string( w.GetBitsProcessed() ) + " bits, want " + std::to_string( bits ) );
        w.Flush();
        if ( w.GetBytesProcessed() != bytes || std::memcmp( out, pin, size_t( bytes ) ) != 0 )
            return fail( "write bytes differ from the pin" );
    }
    if ( g_read )
    {
        // The public read checks the packet end on a copy of the stream, so
        // the stream's count is the object's bits (SPEC §5).
        Value got;
        dirty( got, want.kind );
        serialize::ReadStream r( pin, bytes );
        if ( !read_value( r, got ) ) return fail( "read refused" );
        if ( r.GetBitsProcessed() != bits )
            return fail( "read has " + std::to_string( r.GetBitsProcessed() ) + " bits, want " + std::to_string( bits ) );
        if ( !value_equal( got, want ) ) return fail( "read value differs" );
    }
    return true;
}

static bool refuse_case( const std::string & dir, const std::string & name, const std::string & type )
{
    Kind kind = kind_of( type );
    if ( kind == Kind::Unknown ) return fail( "unknown type " + type );
    std::vector<uint64_t> words;
    int64_t bytes = 0;
    if ( !load( dir, name, words, bytes ) ) return false;
    if ( g_read )
    {
        Value got;
        dirty( got, kind );
        serialize::ReadStream r( reinterpret_cast<const uint8_t *>( words.data() ), bytes );
        if ( read_value( r, got ) ) return fail( "read accepted" );
    }
    return true;
}

// select: read mixed-ping's pin into dirtied storage with the public read,
// then select the payload arm on that value: the generated SelectPayload
// constructs it with its declared defaults, and the value writes this
// case's pin.
static bool select_case( const std::string & dir, const std::string & name, const std::string & type, int64_t bits )
{
    if ( name != "mixed-ping-select-payload" ) return fail( "unknown select case" );
    if ( kind_of( type ) != Kind::Mixed ) return fail( "case type " + type + ", the driver selects on a Mixed" );
    std::vector<uint64_t> from_words, words;
    int64_t from_bytes = 0, bytes = 0;
    if ( !load( dir, "mixed-ping", from_words, from_bytes ) || !load( dir, name, words, bytes ) ) return false;
    if ( !g_read || !g_write ) return true; // the case reads, then writes: one direction alone cannot answer it
    Value got;
    dirty( got, Kind::Mixed );
    serialize::ReadStream r( reinterpret_cast<const uint8_t *>( from_words.data() ), from_bytes );
    if ( !ReadMixed( r, got.mixed ) ) return fail( "read mixed-ping refused" );
    if ( got.mixed.type != MixedType::Ping ) return fail( "read mixed-ping: not the ping arm" );
    got.mixed.SelectPayload();
    alignas( 8 ) uint8_t out[64] = {};
    serialize::WriteStream w( out, sizeof( out ) );
    if ( !WriteMixed( w, got.mixed ) ) return fail( "write refused" );
    if ( w.GetBitsProcessed() != bits )
        return fail( "write has " + std::to_string( w.GetBitsProcessed() ) + " bits, want " + std::to_string( bits ) );
    w.Flush();
    if ( w.GetBytesProcessed() != bytes || std::memcmp( out, words.data(), size_t( bytes ) ) != 0 )
        return fail( "write bytes differ from the pin" );
    return true;
}

// The refuse-write cases: the type's default value with the one change the
// case names. The tag is a uint8_t enum class and the payload a typed
// member, so a text tag and a payload of another type cannot be built: the
// compiler refuses them. Tag -1 is the uint8_t tag's -1, 255.
static bool build_refused( const std::string & name, Value & v )
{
    if ( name == "mixed-write-tag-negative" ) { v.kind = Kind::Mixed; v.mixed.type = static_cast<MixedType>( static_cast<uint8_t>( -1 ) ); return true; }
    if ( name == "mixed-write-tag-3" ) { v.kind = Kind::Mixed; v.mixed.type = static_cast<MixedType>( 3 ); return true; }
    if ( name == "mixed-write-marker-8" ) { v.kind = Kind::Mixed; v.mixed.SelectPayload().marker = 8; return true; }
    if ( name == "tags-write-tag-3" ) { v.kind = Kind::Tags; v.tags.type = static_cast<TagsType>( 3 ); return true; }
    if ( name == "empty-write-tag-1" ) { v.kind = Kind::Empty; v.empty.type = static_cast<EmptyType>( 1 ); return true; }
    return false;
}

static bool not_representable( const std::string & name )
{
    return name == "mixed-write-tag-text" || name == "mixed-write-payload-type";
}

static bool refuse_write_case( const std::string & name, const std::string & type )
{
    if ( not_representable( name ) ) return true;
    Value v;
    if ( !build_refused( name, v ) ) return fail( "unknown refuse-write case" );
    if ( v.kind != kind_of( type ) ) return fail( "case type " + type + ", the driver builds another" );
    if ( !g_write ) return true;
#ifdef NDEBUG
    return true; // no write check in a release build, by design: debug asserts only
#else
    std::fflush( stdout ); // the child must not repeat buffered answers
    const pid_t child = fork();
    if ( child < 0 ) return fail( "fork failed" );
    if ( child == 0 )
    {
        alignas( 8 ) uint8_t out[64] = {};
        serialize::WriteStream w( out, sizeof( out ) );
        _exit( write_value( w, v ) ? 0 : 10 );
    }
    int status = 0;
    if ( waitpid( child, &status, 0 ) != child ) return fail( "waitpid failed" );
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) return true; // the debug assert
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == 10 ) return true;     // the writer returned false
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == 0 ) return fail( "write accepted" );
    return fail( "the write ended with status " + std::to_string( status ) + ", not the assert" );
#endif
}

int main( int argc, char ** argv )
{
    if ( argc == 3 && std::strcmp( argv[2], "read" ) == 0 ) g_write = false;
    else if ( argc == 3 && std::strcmp( argv[2], "write" ) == 0 ) g_read = false;
    else if ( argc != 2 )
    {
        std::fprintf( stderr, "usage: %s <testdata/wire/packet-void> [read|write]\n", argv[0] );
        return 2;
    }
    // The generated surface beside the wire: tag extents, debug names and
    // maximum sizes. A miss exits non-zero, which tools/casegate holds red.
    if ( uint8_t( MixedType::Max ) != 2 || uint8_t( LateType::Max ) != 3 || uint8_t( LateType::Pong ) != 3 || uint8_t( TagsType::Max ) != 2 || uint8_t( SingleType::Max ) != 1 ||
         uint8_t( EmptyType::Max ) != 0 || std::strcmp( EnumName( MixedType::Ping ), "Ping" ) != 0 ||
         std::strcmp( EnumName( TagsType::Pong ), "Pong" ) != 0 || MixedMaxBits != 23 || TagsMaxBits != 2 ||
         SingleMaxBits != 1 || EmptyMaxBits != 0 )
    {
        std::printf( "FAILED: packet-void tag extents, debug names or maximum bits\n" );
        return 1;
    }
    const std::string dir = argv[1];
    std::ifstream cases( dir + "/cases.txt" );
    if ( !cases.good() )
    {
        std::fprintf( stderr, "cannot open %s/cases.txt\n", dir.c_str() );
        return 2;
    }
    std::string line;
    while ( std::getline( cases, line ) )
    {
        std::istringstream fields( line );
        std::string name, type, verdict, bits;
        if ( !( fields >> name ) || name[0] == '#' ) continue;
        g_why.clear();
        bool ok;
        if ( !( fields >> type >> verdict >> bits ) ) ok = fail( "a case line needs <name> <type> <verdict> <bits>" );
        else if ( verdict == "accept" ) ok = accept_case( dir, name, type, std::strtoll( bits.c_str(), nullptr, 10 ) );
        else if ( verdict == "refuse" ) ok = refuse_case( dir, name, type );
        else if ( verdict == "select" ) ok = select_case( dir, name, type, std::strtoll( bits.c_str(), nullptr, 10 ) );
        else if ( verdict == "refuse-write" ) ok = refuse_write_case( name, type );
        else ok = fail( "verdict " + verdict );
        if ( ok ) std::printf( "case %s ok\n", name.c_str() );
        else std::printf( "case %s FAILED: %s\n", name.c_str(), g_why.c_str() );
    }
    return 0;
}
