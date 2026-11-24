// The C++ leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. "read" or "write" as the second argument runs one direction
// only: the make/checks/packet-void.mk negative controls sabotage one
// direction and require the other to stay green.
#include "VoidWire.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <sstream>
#include <string>
#include <vector>

using namespace packetvoid;

enum class Kind { Mixed, Tags, Single, Empty, Frame, Batch, Unknown };

struct Value
{
    Kind kind = Kind::Unknown;
    packetvoid::Mixed mixed;
    packetvoid::Tags tags;
    packetvoid::Single single;
    packetvoid::Empty empty;
    packetvoid::Frame frame;
    packetvoid::Batch batch;
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

static bool value_equal( const Value & got, const Value & want )
{
    switch ( want.kind )
    {
    case Kind::Mixed: return mixed_equal( got.mixed, want.mixed );
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
    if ( uint8_t( MixedType::Max ) != 2 || uint8_t( TagsType::Max ) != 2 || uint8_t( SingleType::Max ) != 1 ||
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
        else ok = fail( "verdict " + verdict );
        if ( ok ) std::printf( "case %s ok\n", name.c_str() );
        else std::printf( "case %s FAILED: %s\n", name.c_str(), g_why.c_str() );
    }
    return 0;
}
