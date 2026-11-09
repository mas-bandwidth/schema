package cpp

// The C++ row of the packet-defaults gate (SPEC §4.2, §4.7, §4.8, §5;
// test/packet-defaults/README.md): test/packet-defaults/Defaults.schema is
// generated, compiled and run against the eight shared C++ pins under
// testdata/wire/packet-defaults. Constructors carry their string, bytes and
// flags defaults; every pin writes the same bytes and bit count; each pin
// reads back into reused storage and consumes exactly the pinned bits; and a
// buffer one byte short is refused, so the read is the whole value or a
// refusal.
//
// This is a functional test: it shells out to a C++ compiler, so it skips by
// name without SCHEMA_SLOW. It carries its own strict bit-stream header
// instead of a sibling serialize checkout, so the generated unit (the real
// artifact) is always what is compiled; the test pins that this stub's bytes
// are the checked-in C++ pins before it trusts any read.
//
// The negative control removes only the generated constructor's flags default
// from the real Defaults.h and requires the compiled probe to fail the
// constructor-bytes marker.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const packetDefaultsProbe = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>
#include <new>

#include "DefaultsWire.h"
#include "PlainWire.h"

static int failures = 0;

static void ck( bool ok, const char * what )
{
    if ( !ok )
    {
        std::fprintf( stderr, "FAILED: %s\n", what );
        ++failures;
    }
}

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

static void zero_sample( packetdefaults::Sample & value )
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

static packetdefaults::Sample empty_sample()
{
    packetdefaults::Sample value;
    zero_sample( value );
    return value;
}

static packetdefaults::Sample short_sample()
{
    packetdefaults::Sample value = empty_sample();
    value.name[0] = 'A';
    value.name_length = 1;
    value.token[0] = 0;
    value.token[1] = 0xff;
    value.token_length = 2;
    value.caps = 2;
    return value;
}

static packetdefaults::Sample dirty_sample()
{
    packetdefaults::Sample value;
    std::memcpy( value.name, "dirty!", 6 );
    value.name_length = 6;
    std::memset( value.token, 0xa1, sizeof( value.token ) );
    value.token_length = 4;
    value.caps = 7;
    std::memcpy( value.empty_name, "old", 3 );
    value.empty_name_length = 3;
    std::memset( value.empty_token, 0xb1, sizeof( value.empty_token ) );
    value.empty_token_length = 2;
    value.empty_caps = 7;
    return value;
}

static bool same_sample( const packetdefaults::Sample & a, const packetdefaults::Sample & b )
{
    return a.name_length == b.name_length && std::memcmp( a.name, b.name, sizeof( a.name ) ) == 0 &&
           a.token_length == b.token_length && std::memcmp( a.token, b.token, sizeof( a.token ) ) == 0 &&
           a.caps == b.caps && a.empty_caps == b.empty_caps &&
           a.empty_name_length == b.empty_name_length &&
           std::memcmp( a.empty_name, b.empty_name, sizeof( a.empty_name ) ) == 0 &&
           a.empty_token_length == b.empty_token_length &&
           std::memcmp( a.empty_token, b.empty_token, sizeof( a.empty_token ) ) == 0;
}

static bool same_conditional( const packetdefaults::Conditional & a, const packetdefaults::Conditional & b )
{
    return a.enabled == b.enabled && same_sample( a.value, b.value );
}

static bool same_choice( const packetdefaults::Choice & a, const packetdefaults::Choice & b )
{
    if ( a.type != b.type )
        return false;
    if ( a.type == packetdefaults::ChoiceType::Sample )
        return same_sample( a.sample, b.sample );
    if ( a.type == packetdefaults::ChoiceType::Conditional )
        return same_conditional( a.conditional, b.conditional );
    return true;
}

static bool read_file( const std::string & path, std::vector<uint8_t> & data )
{
    std::ifstream file( path, std::ios::binary );
    if ( !file.good() )
        return false;
    data.assign( std::istreambuf_iterator<char>( file ), {} );
    return true;
}

static bool read_bit_count( const std::string & path, int64_t & bits )
{
    std::ifstream file( path );
    return ( file >> bits ).good() || !file.fail();
}

template <typename T>
static void write_pin( const std::string & directory, const char * name, const T & value,
                       bool ( * write )( serialize::WriteStream &, const T & ) )
{
    alignas( 8 ) uint8_t buffer[4096] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    ck( write( stream, value ), ( std::string( name ) + ": write" ).c_str() );
    const int64_t bits = stream.GetBitsProcessed();
    stream.Flush();
    const int64_t bytes = stream.GetBytesProcessed();
    std::vector<uint8_t> expected;
    int64_t expected_bits = -1;
    ck( read_file( directory + "/" + name + ".bin", expected ), ( std::string( name ) + ": read pin bytes" ).c_str() );
    ck( read_bit_count( directory + "/" + name + ".bits", expected_bits ), ( std::string( name ) + ": read pin bits" ).c_str() );
    ck( bits == expected_bits, ( std::string( name ) + ": C++ pin bits" ).c_str() );
    ck( bytes == int64_t( expected.size() ) && std::memcmp( buffer, expected.data(), expected.size() ) == 0,
        ( std::string( name ) + ": C++ pin bytes" ).c_str() );
}

template <typename T>
static T read_pin( const std::string & directory, const char * name, const T & initial,
                   bool ( * read )( serialize::ReadStream &, T & ) )
{
    std::vector<uint8_t> wire;
    int64_t bits = -1;
    ck( read_file( directory + "/" + name + ".bin", wire ), ( std::string( name ) + ": read pin bytes" ).c_str() );
    ck( read_bit_count( directory + "/" + name + ".bits", bits ), ( std::string( name ) + ": read pin bits" ).c_str() );
    std::vector<uint8_t> padded( wire.size() + 8, 0 );
    std::memcpy( padded.data(), wire.data(), wire.size() );

    T value = initial;
    serialize::ReadStream stream( padded.data(), int64_t( wire.size() ) );
    ck( read( stream, value ), ( std::string( name ) + ": read" ).c_str() );
    ck( stream.GetBitsProcessed() == bits, ( std::string( name ) + ": read consumed bits" ).c_str() );

    if ( !wire.empty() )
    {
        T truncated = initial;
        serialize::ReadStream short_stream( padded.data(), int64_t( wire.size() - 1 ) );
        ck( !read( short_stream, truncated ), ( std::string( name ) + ": short read refuses" ).c_str() );
    }
    return value;
}

static packetdefaults::Sample short_overlay()
{
    packetdefaults::Sample value = dirty_sample();
    value.name[0] = 'A';
    value.name[1] = 0;
    value.name_length = 1;
    value.token[0] = 0;
    value.token[1] = 0xff;
    value.token_length = 2;
    value.caps = 2;
    value.empty_name[0] = 0;
    value.empty_name_length = 0;
    value.empty_token_length = 0;
    value.empty_caps = 0;
    return value;
}

static packetdefaults::Sample empty_overlay()
{
    packetdefaults::Sample value = dirty_sample();
    value.name[0] = 0;
    value.name_length = 0;
    value.token_length = 0;
    value.caps = 0;
    value.empty_name[0] = 0;
    value.empty_name_length = 0;
    value.empty_token_length = 0;
    value.empty_caps = 0;
    return value;
}

static packetplain::Sample plain_sample()
{
    packetplain::Sample value;
    std::memcpy( value.name, name_bytes, sizeof( name_bytes ) );
    value.name_length = 6;
    std::memcpy( value.token, token_bytes, sizeof( token_bytes ) );
    value.token_length = 4;
    value.caps = 5;
    std::memset( value.empty_name, 0, sizeof( value.empty_name ) );
    value.empty_name_length = 0;
    std::memset( value.empty_token, 0, sizeof( value.empty_token ) );
    value.empty_token_length = 0;
    value.empty_caps = 0;
    return value;
}

static int run( const char * directory )
{
    const std::string dir = directory;
    packetdefaults::Sample sample;
    ck( sample.name_length == 6 && std::memcmp( sample.name, name_bytes, sizeof( name_bytes ) ) == 0 &&
        sample.name[6] == 0 && sample.token_length == 4 &&
        std::memcmp( sample.token, token_bytes, sizeof( token_bytes ) ) == 0 && sample.caps == 5 &&
        sample.empty_name_length == 0 && zero_buffer( sample.empty_name ) &&
        sample.empty_token_length == 0 && zero_buffer( sample.empty_token ) && sample.empty_caps == 0,
        "packet-default constructor bytes" );

    packetdefaults::EmptyOnly empty;
    ck( empty.name_length == 0 && zero_buffer( empty.name ) && empty.token_length == 0 &&
        zero_buffer( empty.token ) && empty.caps == 0, "explicit empty constructor" );

    packetdefaults::Prefix prefix;
    const uint8_t prefix_name[6] = { 0xc3, 0xa9, 0, 0, 0, 0 };
    const uint8_t prefix_token[5] = { 0x5c, 0x6e, 0, 0, 0 };
    ck( prefix.name_length == 2 && prefix.token_length == 2 &&
        std::memcmp( prefix.name, prefix_name, sizeof( prefix_name ) ) == 0 &&
        std::memcmp( prefix.token, prefix_token, sizeof( prefix_token ) ) == 0,
        "short literal constructor tails" );

    packetdefaults::WideMask wide;
    ck( wide.high == ( uint64_t( 1 ) << 63 ) && wide.all == ~uint64_t( 0 ),
        "unsigned 64-bit flags defaults" );

    packetdefaults::Batch batch;
    ck( batch.counted_count == 1 && same_sample( batch.head, sample ), "nested constructor defaults" );
    for ( const auto & item : batch.items )
        ck( same_sample( item, sample ), "fixed-array constructor defaults" );
    for ( const auto & item : batch.counted )
        ck( same_sample( item, sample ), "counted backing constructor defaults" );

    packetdefaults::ZeroCount zero_count;
    ck( zero_count.items_count == 0 && same_sample( zero_count.items[0], sample ) &&
        same_sample( zero_count.items[1], sample ), "zero-count constructor defaults" );

    packetdefaults::Conditional conditional;
    ck( conditional.enabled && same_sample( conditional.value, sample ), "conditional constructor defaults" );
    packetdefaults::Conditional conditional_off;
    conditional_off.enabled = false;

    packetdefaults::Choice choice;
    ck( choice.type == packetdefaults::ChoiceType::None, "union construction is None" );
    packetdefaults::Choice choice_sample;
    choice_sample.SelectSample();

    const packetdefaults::Sample short_value = short_sample();
    const packetdefaults::Sample empty_value = empty_sample();

    write_pin( dir, "sample-defaults", sample, packetdefaults::WriteSample );
    write_pin( dir, "batch-defaults", batch, packetdefaults::WriteBatch );
    write_pin( dir, "zero-count", zero_count, packetdefaults::WriteZeroCount );
    write_pin( dir, "conditional-on", conditional, packetdefaults::WriteConditional );
    write_pin( dir, "conditional-off", conditional_off, packetdefaults::WriteConditional );
    write_pin( dir, "choice-sample", choice_sample, packetdefaults::WriteChoice );
    write_pin( dir, "sample-short", short_value, packetdefaults::WriteSample );
    write_pin( dir, "sample-empty", empty_value, packetdefaults::WriteSample );

    {
        alignas( 8 ) uint8_t defaults[4096] = {};
        alignas( 8 ) uint8_t plain[4096] = {};
        serialize::WriteStream defaults_stream( defaults, sizeof( defaults ) );
        serialize::WriteStream plain_stream( plain, sizeof( plain ) );
        const packetplain::Sample plain_value = plain_sample();
        ck( packetdefaults::WriteSample( defaults_stream, sample ) &&
            packetplain::WriteSample( plain_stream, plain_value ),
            "defaultless twin writes" );
        const int64_t bits = defaults_stream.GetBitsProcessed();
        defaults_stream.Flush();
        plain_stream.Flush();
        ck( bits == plain_stream.GetBitsProcessed() &&
            defaults_stream.GetBytesProcessed() == plain_stream.GetBytesProcessed() &&
            std::memcmp( defaults, plain, size_t( defaults_stream.GetBytesProcessed() ) ) == 0,
            "defaults do not change the packet wire" );
    }

    const packetdefaults::Sample sample_read =
        read_pin( dir, "sample-defaults", empty_value, packetdefaults::ReadSample );
    ck( same_sample( sample_read, sample ), "sample-defaults read values" );

    const packetdefaults::Batch batch_read =
        read_pin( dir, "batch-defaults", packetdefaults::Batch{}, packetdefaults::ReadBatch );
    bool batch_ok = batch_read.counted_count == 1 && same_sample( batch_read.head, sample );
    for ( const auto & item : batch_read.items )
        batch_ok = batch_ok && same_sample( item, sample );
    for ( const auto & item : batch_read.counted )
        batch_ok = batch_ok && same_sample( item, sample );
    ck( batch_ok, "batch-defaults read values" );

    packetdefaults::ZeroCount zero_count_read;
    zero_count_read.items[0] = dirty_sample();
    zero_count_read.items[1] = short_sample();
    zero_count_read.items_count = 2;
    zero_count_read = read_pin( dir, "zero-count", zero_count_read, packetdefaults::ReadZeroCount );
    ck( zero_count_read.items_count == 0 && same_sample( zero_count_read.items[0], dirty_sample() ) &&
        same_sample( zero_count_read.items[1], short_sample() ),
        "zero-count read keeps backing storage" );

    packetdefaults::Conditional conditional_read;
    conditional_read.enabled = false;
    conditional_read.value = empty_value;
    conditional_read = read_pin( dir, "conditional-on", conditional_read, packetdefaults::ReadConditional );
    ck( same_conditional( conditional_read, conditional ), "conditional-on read values" );

    packetdefaults::Conditional conditional_off_read;
    conditional_off_read.enabled = true;
    conditional_off_read.value = dirty_sample();
    conditional_off_read = read_pin( dir, "conditional-off", conditional_off_read, packetdefaults::ReadConditional );
    ck( !conditional_off_read.enabled && same_sample( conditional_off_read.value, empty_value ),
        "conditional-off read clears the untaken side" );

    packetdefaults::Choice choice_read;
    choice_read.SelectConditional();
    choice_read.conditional.enabled = true;
    choice_read.conditional.value = dirty_sample();
    choice_read = read_pin( dir, "choice-sample", choice_read, packetdefaults::ReadChoice );
    ck( same_choice( choice_read, choice_sample ), "choice-sample read values" );

    const packetdefaults::Sample short_read =
        read_pin( dir, "sample-short", dirty_sample(), packetdefaults::ReadSample );
    ck( same_sample( short_read, short_overlay() ), "sample-short read overlays the prefix" );

    const packetdefaults::Sample empty_read =
        read_pin( dir, "sample-empty", dirty_sample(), packetdefaults::ReadSample );
    ck( same_sample( empty_read, empty_overlay() ), "sample-empty read overlays the prefix" );

    return failures == 0 ? 0 : 1;
}

int main( int argc, char ** argv )
{
    if ( argc != 2 )
    {
        std::fprintf( stderr, "usage: %s <golden-directory>\n", argv[0] );
        return 2;
    }
    const std::string directory = argv[1];
    run( directory.c_str() );
    if ( failures != 0 )
        return 1;
    std::puts( "packet defaults C++: constructors, eight pins and strict reads OK" );
    return 0;
}
`

// packetDefaultsSerialize is the strict in-test runtime: the generated unit
// includes serialize.h, and this header provides exactly the primitives it
// names. Writes are least-significant-bit first; every read checks the full
// field before it lands, so a short buffer refuses instead of reading a
// partial value.
const packetDefaultsSerialize = `#pragma once
#include <stdint.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>

#define serialize_assert( condition ) \
    do \
    { \
        if ( !( condition ) ) \
        { \
            std::fprintf( stderr, "FAILED: serialize_assert: %s\n", #condition ); \
            std::abort(); \
        } \
    } while ( 0 )

namespace serialize
{

class WriteStream
{
public:
    WriteStream( uint8_t * data, int64_t size ) : data_( data ), size_( size ), bits_( 0 ), failed_( false ) {}

    bool WriteBits( uint64_t value, int32_t count )
    {
        if ( count < 0 || count > 64 )
        {
            failed_ = true;
            return false;
        }
        for ( int32_t i = 0; i < count; ++i )
        {
            if ( !SetBit( uint64_t( ( value >> i ) & 1u ) ) )
                return false;
        }
        return true;
    }

    bool WriteBytes( const void * data, int32_t length )
    {
        if ( length < 0 )
        {
            failed_ = true;
            return false;
        }
        if ( !AlignToByte() )
            return false;
        const uint8_t * bytes = static_cast<const uint8_t *>( data );
        for ( int32_t i = 0; i < length; ++i )
        {
            if ( !WriteBits( bytes[i], 8 ) )
                return false;
        }
        return true;
    }

    bool WriteBool( bool value ) { return WriteBits( value ? 1u : 0u, 1 ); }
    int64_t GetBitsProcessed() const { return bits_; }
    int64_t GetBytesProcessed() const { return ( bits_ + 7 ) / 8; }
    void Flush() {}

private:
    bool AlignToByte()
    {
        const int32_t padding = int32_t( ( 8 - ( bits_ & 7 ) ) & 7 );
        for ( int32_t i = 0; i < padding; ++i )
        {
            if ( !SetBit( 0 ) )
                return false;
        }
        return true;
    }

    bool SetBit( uint64_t bit )
    {
        if ( bits_ >= size_ * 8 )
        {
            failed_ = true;
            return false;
        }
        const int64_t byte = bits_ >> 3;
        const int32_t offset = int32_t( bits_ & 7 );
        if ( offset == 0 )
            data_[byte] = 0;
        if ( bit != 0 )
            data_[byte] = uint8_t( data_[byte] | uint8_t( 1u << offset ) );
        ++bits_;
        return true;
    }

    uint8_t * data_;
    int64_t size_;
    int64_t bits_;
    bool failed_;
};

class ReadStream
{
public:
    ReadStream( const uint8_t * data, int64_t size ) : data_( data ), size_( size ), bits_( 0 ), failed_( false ) {}

    template <typename T>
    bool ReadBits( T & value, int32_t count )
    {
        uint64_t raw = 0;
        if ( !ReadBitsRaw( raw, count ) )
            return false;
        value = T( raw );
        return true;
    }

    bool ReadBytes( void * data, int32_t length )
    {
        if ( length < 0 )
        {
            failed_ = true;
            return false;
        }
        if ( !AlignToByte() )
            return false;
        uint8_t * bytes = static_cast<uint8_t *>( data );
        for ( int32_t i = 0; i < length; ++i )
        {
            uint64_t byte = 0;
            if ( !ReadBitsRaw( byte, 8 ) )
                return false;
            bytes[i] = uint8_t( byte );
        }
        return true;
    }

    bool ReadBool( bool & value )
    {
        uint64_t bit = 0;
        if ( !ReadBitsRaw( bit, 1 ) )
            return false;
        value = bit != 0;
        return true;
    }

    bool ReadInt( int32_t & value, int32_t minimum, int32_t maximum )
    {
        if ( maximum < minimum )
        {
            failed_ = true;
            return false;
        }
        const uint64_t range = uint64_t( maximum ) - uint64_t( minimum );
        int32_t count = 0;
        while ( count < 32 && ( uint64_t( 1 ) << count ) <= range )
            ++count;
        uint64_t raw = 0;
        if ( !ReadBitsRaw( raw, count ) )
            return false;
        if ( raw > range )
            return false;
        value = int32_t( uint64_t( minimum ) + raw );
        return true;
    }

    int64_t GetBitsProcessed() const { return bits_; }

private:
    bool AlignToByte()
    {
        const int64_t target = ( bits_ + 7 ) & ~int64_t( 7 );
        if ( target > size_ * 8 )
        {
            failed_ = true;
            return false;
        }
        bits_ = target;
        return true;
    }

    bool ReadBitsRaw( uint64_t & value, int32_t count )
    {
        if ( count < 0 || count > 64 || bits_ + count > size_ * 8 )
        {
            failed_ = true;
            return false;
        }
        value = 0;
        for ( int32_t i = 0; i < count; ++i )
        {
            const int64_t byte = bits_ >> 3;
            const int32_t offset = int32_t( bits_ & 7 );
            const uint64_t bit = uint64_t( ( data_[byte] >> offset ) & 1u );
            value |= bit << i;
            ++bits_;
        }
        return true;
    }

    const uint8_t * data_;
    int64_t size_;
    int64_t bits_;
    bool failed_;
};

} // namespace serialize

#define write_bits( stream, value, count ) \
    do \
    { \
        if ( !( stream ).WriteBits( ( value ), ( count ) ) ) \
            return false; \
    } while ( 0 )
#define write_bytes( stream, data, length ) \
    do \
    { \
        if ( !( stream ).WriteBytes( ( data ), ( length ) ) ) \
            return false; \
    } while ( 0 )
#define write_bool( stream, value ) \
    do \
    { \
        if ( !( stream ).WriteBool( ( value ) ) ) \
            return false; \
    } while ( 0 )
#define read_bits( stream, value, count ) \
    do \
    { \
        if ( !( stream ).ReadBits( ( value ), ( count ) ) ) \
            return false; \
    } while ( 0 )
#define read_bytes( stream, data, length ) \
    do \
    { \
        if ( !( stream ).ReadBytes( ( data ), ( length ) ) ) \
            return false; \
    } while ( 0 )
#define read_bool( stream, value ) \
    do \
    { \
        if ( !( stream ).ReadBool( ( value ) ) ) \
            return false; \
    } while ( 0 )
#define read_int( stream, value, minimum, maximum ) \
    do \
    { \
        if ( !( stream ).ReadInt( ( value ), ( minimum ), ( maximum ) ) ) \
            return false; \
    } while ( 0 )
`

func packetDefaultsGenerated(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"Defaults", "Plain"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-defaults", name+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		generated, err := Generate(unitFromSources(t, map[string]string{name + ".schema": string(source)}))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		maps.Copy(files, generated)
	}
	return files
}

func runCppPacketDefaults(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated defaults unit)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("%s: %v", cxx, err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(packetDefaultsSerialize), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probe, []byte(packetDefaultsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-I", dir, probe, "-o", exe}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the C++ probe did not compile: %w", err)
	}
	goldens, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "packet-defaults"))
	if err != nil {
		t.Fatal(err)
	}
	return exec.Command(exe, goldens).CombinedOutput()
}

func TestCppPacketDefaults(t *testing.T) {
	out, err := runCppPacketDefaults(t, packetDefaultsGenerated(t))
	if err != nil {
		t.Fatalf("C++ packet defaults: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("packet defaults C++: constructors, eight pins and strict reads OK")) {
		t.Fatalf("C++ packet defaults did not print its success marker:\n%s", out)
	}
}

const packetDefaultsConstructorAnchor = "Caps caps = ( Caps_Jump | Caps_Fly );"

// TestCppPacketDefaultsNegativeControl removes only the generated Sample's
// flags default from the real Defaults.h and requires the compiled probe to
// fail the constructor-bytes marker.
func TestCppPacketDefaultsNegativeControl(t *testing.T) {
	files := packetDefaultsGenerated(t)
	removed := 0
	for name, data := range files {
		count := bytes.Count(data, []byte(packetDefaultsConstructorAnchor))
		if count == 0 {
			continue
		}
		removed += count
		files[name] = bytes.ReplaceAll(data, []byte(packetDefaultsConstructorAnchor), []byte("Caps caps = 0;"))
	}
	if removed != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the constructor anchor appeared %d times, want 1", removed)
	}
	out, err := runCppPacketDefaults(t, files)
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: removing the flags default left the row GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("FAILED: packet-default constructor bytes\n")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the row went red, but not on the constructor-bytes marker: %v\n%s", err, out)
	}
}
