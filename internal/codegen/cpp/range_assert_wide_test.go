package cpp

// The write-side range assert of a 64-bit field whose range fits int32 compares
// in the field's own width: the int32 fold keeps its wire bytes, but the assert
// casts to int64_t so a value outside the range is never narrowed into it, and
// a bound at the int32 extremes is not a tautology over 64-bit storage.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const rangeAssertWideSchema = `package rangewide
type Wide {
    id int64 | min = 0, max = 100
    span int64 | min = -2147483648, max = 2147483647
    count uint64 | min = 0, max = 100
}
`

// TestRangeAssertWideStorageComparesIn64Bits pins the asserts' text.
func TestRangeAssertWideStorageComparesIn64Bits(t *testing.T) {
	t.Parallel()
	_, w := generateOne(t, "Wide", rangeAssertWideSchema)
	for _, want := range []string{
		"serialize_assert( int64_t( value.id ) >= int64_t( 0 ) && int64_t( value.id ) <= int64_t( 100 ) );",
		"serialize_assert( int64_t( value.span ) >= int64_t( ",
		"int64_t( value.span ) <= int64_t( 2147483647 ) );",
		// uint64 storage can wrap negative in the int64_t cast: the low half catches that
		"serialize_assert( int64_t( value.count ) >= int64_t( 0 ) && int64_t( value.count ) <= int64_t( 100 ) );",
		"write_bits( stream, uint32_t( value.id ), 7 );",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("WideWire.h: want %q in\n%s", want, w)
		}
	}
	for _, bad := range []string{" int32_t( value.id )", " int32_t( value.span )", " int32_t( value.count )"} {
		if strings.Contains(w, bad) {
			t.Errorf("WideWire.h narrows a 64-bit field in its range assert (%q)", bad)
		}
	}
}

const rangeAssertWideProbe = `#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>

#include "WideWire.h"

int main( int argc, char ** argv )
{
    if ( argc != 2 )
        return 2;
    rangewide::Wide value;
    value.id = 50;
    value.span = -5;
    value.count = 7;
    if ( std::strcmp( argv[1], "id" ) == 0 )
        value.id = 4294967301ll;
    else if ( std::strcmp( argv[1], "span" ) == 0 )
        value.span = 2147483648ll;
    else if ( std::strcmp( argv[1], "count" ) == 0 )
        value.count = 4294967301ull;
    else if ( std::strcmp( argv[1], "ok" ) != 0 )
        return 2;
    uint8_t buffer[64];
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    if ( !rangewide::WriteWide( stream, value ) )
    {
        std::fprintf( stderr, "write refused\n" );
        return 1;
    }
    std::printf( "wrote %s\n", argv[1] );
    return 0;
}
`

// TestCppRangeAssertWideStorage compiles the generated unit and requires the
// debug assert to fire for each out-of-range 64-bit value, and an in-range
// write to go through.
func TestCppRangeAssertWideStorage(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated range-assert unit)")
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
	files, err := Generate(unitFromSources(t, map[string]string{"Wide.schema": rangeAssertWideSchema}))
	if err != nil {
		t.Fatal(err)
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
	// the defaults stub reads ints into int32_t storage only; these fields are 64-bit
	stub := packetDefaultsSerialize
	for from, to := range map[string]string{
		"bool ReadInt( int32_t & value,":                "template <typename T>\n    bool ReadInt( T & value,",
		"value = int32_t( uint64_t( minimum ) + raw );": "value = T( int32_t( uint64_t( minimum ) + raw ) );",
	} {
		if strings.Count(stub, from) != 1 {
			t.Fatalf("the serialize stub no longer carries %q once", from)
		}
		stub = strings.Replace(stub, from, to, 1)
	}
	if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(stub), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probe, []byte(rangeAssertWideProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-I", dir, probe, "-o", exe}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the C++ probe did not compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(exe, "ok").CombinedOutput(); err != nil || !strings.Contains(string(out), "wrote ok") {
		t.Fatalf("an in-range write must go through: %v\n%s", err, out)
	}
	for _, field := range []string{"id", "span", "count"} {
		out, err := exec.Command(exe, field).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "FAILED: serialize_assert: ") || !strings.Contains(string(out), "value."+field) {
			t.Errorf("an out-of-range %s must fire its range assert: %v\n%s", field, err, out)
		}
	}
}
