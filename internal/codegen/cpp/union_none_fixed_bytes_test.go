package cpp

// A union at None costs only its tag bits (SPEC §4.8), so a fixed [N]uint8
// field that follows one is packed from the next bit, not from the next byte.
// This test generates C++ for such a type, writes it with the union at None,
// and pins both the packed bytes and a round trip.
//
// This is a functional test: it shells out to a C++ compiler, so it skips by
// name without SCHEMA_SLOW.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const unionNoneFixedBytesSchema = `package t

type Small {
    v uint8 | min = 0, max = 127
}

union U {
    a Small
}

type T {
    align
    u    U
    data [4]uint8
}
`

const unionNoneFixedBytesProbe = `#include <cstdint>
#include <cstdio>
#include <cstring>

#include "serialize.h"

#define write_align( stream ) \
    do \
    { \
        const int64_t pad_ = ( -int64_t( ( stream ).GetBitsProcessed() ) ) & 7; \
        write_bits( ( stream ), 0u, int32_t( pad_ ) ); \
    } while ( 0 )

#define read_align( stream ) \
    do \
    { \
        const int64_t pad_ = ( -int64_t( ( stream ).GetBitsProcessed() ) ) & 7; \
        uint64_t pad_bits_ = 0; \
        read_bits( ( stream ), pad_bits_, int32_t( pad_ ) ); \
        if ( pad_bits_ != 0 ) \
        { \
            return false; \
        } \
    } while ( 0 )

#include "AlignWire.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok )
    {
        std::fprintf( stderr, "FAILED: %s\n", what );
        ++failures;
    }
}

int main()
{
    t::T value;
    value.u.type = t::UType::None;
    value.data[0] = 1;
    value.data[1] = 2;
    value.data[2] = 3;
    value.data[3] = 4;

    alignas( 8 ) uint8_t buffer[64] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    check( t::WriteT( stream, value ), "write T" );

    // The tag is one bit; the four data bytes follow immediately, for
    // 1 + 32 = 33 bits. Each byte is written least-significant-bit first,
    // so the packed stream is { 0x02, 0x04, 0x06, 0x08, 0x00 }.
    const uint8_t expected[] = { 0x02, 0x04, 0x06, 0x08, 0x00 };
    const int64_t bits = stream.GetBitsProcessed();
    stream.Flush();
    check( bits == 33, "T writes 33 bits" );
    check( stream.GetBytesProcessed() == 5, "T writes 5 bytes" );
    check( std::memcmp( buffer, expected, sizeof( expected ) ) == 0, "T writes the per-byte layout" );

    t::T round_trip;
    round_trip.data[0] = 9;
    round_trip.data[1] = 9;
    round_trip.data[2] = 9;
    round_trip.data[3] = 9;
    serialize::ReadStream in( buffer, stream.GetBytesProcessed() );
    check( t::ReadT( in, round_trip ), "read T" );
    check( in.GetBitsProcessed() == 33, "T reads 33 bits" );
    check( round_trip.u.type == t::UType::None, "T reads u at None" );
    check( round_trip.data[0] == 1 && round_trip.data[1] == 2 && round_trip.data[2] == 3 && round_trip.data[3] == 4,
        "T reads the data bytes" );

    if ( failures != 0 )
        return 1;
    std::puts( "union None fixed bytes C++: per-byte layout and round trip OK" );
    return 0;
}
`

func runCppUnionNoneFixedBytes(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
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
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(packetDefaultsSerialize), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probe, []byte(unionNoneFixedBytesProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-I", dir, probe, "-o", exe}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the C++ probe did not compile: %w", err)
	}
	return exec.Command(exe).CombinedOutput()
}

func TestCppUnionNoneFixedBytesLayout(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated union probe)")
	u := unitFromSources(t, map[string]string{"Align.schema": unionNoneFixedBytesSchema})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCppUnionNoneFixedBytes(t, files)
	if err != nil {
		t.Fatalf("C++ union None fixed bytes: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("union None fixed bytes C++: per-byte layout and round trip OK")) {
		t.Fatalf("C++ union None fixed bytes did not print its success marker:\n%s", out)
	}
}
