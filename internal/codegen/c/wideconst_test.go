package c

// const(v, N) and reserved(N) admit N up to 64 (SPEC §4.3). Above 32 the C
// runtime's bits call cannot carry the item in one group, so the item rides
// the same split a bits(N) field above 32 uses: the low 32 bits, then the
// rest. This test pins the emission and the wire against the bytes the
// generated C++ writes for the same type.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const wideConstSource = `package wideconst

type Header {
    const(0x123456789A, 40)
    x uint8
    reserved(40)
    y uint8
}
`

// wideConstCPPBytes is what the generated C++ WriteHeader writes for
// Header { x = 7, y = 9 }: the 40-bit constant, x, 40 zero bits, then y.
var wideConstCPPBytes = []byte{0x9A, 0x78, 0x56, 0x34, 0x12, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09}

func TestWideConstAndReservedC(t *testing.T) {
	u := unitFromSource(t, "WideConst.schema", wideConstSource)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["WideConstWire.h"])
	if strings.Contains(wire, "(serialize_uint32_t) 78187493530ULL") {
		t.Errorf("the 40-bit constant is narrowed to 32 bits:\n%s", wire)
	}
	if strings.Contains(wire, ", 40 )") {
		t.Errorf("a 40-bit item is passed to the runtime's bits call in one group:\n%s", wire)
	}

	for _, mode := range []struct{ name, flag string }{{"release", "-DNDEBUG"}, {"debug", "-UNDEBUG"}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			out, err := runWideConstProbe(t, files, mode.flag)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if !strings.Contains(string(out), "Header: 12 bytes, the C++ bytes") {
				t.Fatalf("the probe did not report its result:\n%s", out)
			}
		})
	}
}

// runWideConstProbe compiles the generated header with a probe that writes
// Header, compares the bytes with wideConstCPPBytes, reads the C++ bytes back,
// and refuses a changed constant and a nonzero reserved field.
func runWideConstProbe(t *testing.T, files map[string][]byte, flag string) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "the c compiler and the serialize.c sibling")
	serialize, ok := setterSerializeDir(t)
	if !ok {
		t.Fatalf("serialize.h is not staged at %s (SERIALIZE_C, else the sibling checkout): the probe cannot compile", serialize)
	}

	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".h") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var expected strings.Builder
	for i, b := range wideConstCPPBytes {
		if i > 0 {
			expected.WriteString(", ")
		}
		fmt.Fprintf(&expected, "0x%02X", b)
	}
	probe := strings.NewReplacer("{{EXPECTED}}", expected.String()).Replace(wideConstProbe)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wtype-limits", "-O2", flag,
		"-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.c"), filepath.Join(serialize, "serialize.c"),
		"-o", filepath.Join(dir, "probe"), "-lm"}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the wide const probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

const wideConstProbe = `
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "WideConstWire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

static int reads( const uint8_t * bytes, int n, Header * value )
{
    serialize_read_stream_t r;
    serialize_read_stream_init( &r, bytes, n );
    return read_header( &r, value );
}

int main( void )
{
    const uint8_t expected[12] = { {{EXPECTED}} };
    uint64_t sent[4];
    uint8_t changed[12];
    Header value, back;
    serialize_write_stream_t w;
    int sent_bytes;

    memset( &value, 0, sizeof( value ) );
    value.x = 7;
    value.y = 9;
    memset( sent, 0, sizeof( sent ) );
    serialize_write_stream_init( &w, (uint8_t *) sent, sizeof( sent ) );
    check( write_header( &w, &value ), "Header writes" );
    serialize_write_flush( &w );
    sent_bytes = serialize_write_bytes_processed( &w );
    check( sent_bytes == 12, "Header is twelve bytes on the wire" );
    check( memcmp( sent, expected, sizeof( expected ) ) == 0, "Header writes the C++ bytes" );

    memset( &back, 0, sizeof( back ) );
    check( reads( expected, 12, &back ), "the C++ bytes read" );
    check( back.x == 7 && back.y == 9, "the C++ bytes read back x and y" );

    memcpy( changed, expected, sizeof( changed ) );
    changed[4] ^= 0x01;
    check( !reads( changed, 12, &back ), "a changed high byte of the constant is refused" );
    memcpy( changed, expected, sizeof( changed ) );
    changed[10] ^= 0x01;
    check( !reads( changed, 12, &back ), "a nonzero high byte of reserved is refused" );

    if ( failed )
    {
        return 1;
    }
    printf( "Header: %d bytes, the C++ bytes\n", sent_bytes );
    return 0;
}
`
