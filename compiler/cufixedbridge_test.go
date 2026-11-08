package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cUfixedBridgeSchema = `package ufixedprobe

type FullDomainProbe
{
    u1_7     ufixed(1, 7)     | min = 0, max = 1
    u8_8     ufixed(8, 8)     | min = 0, max = 255
    u16_16   ufixed(16, 16)   | min = 0, max = 65535
    u32_0    ufixed(32, 0)    | min = 0, max = 4294967295
    u48_16   ufixed(48, 16)   | min = 0, max = 281474976710655
    u64_0    ufixed(64, 0)    | min = 0, max = 9223372036854775807
    u1_127   ufixed(1, 127)   | min = 0, max = 1
    u48_80   ufixed(48, 80)   | min = 0, max = 281474976710655
    u63_65   ufixed(63, 65)   | min = 0, max = 9223372036854775807
    u64_64   ufixed(64, 64)   | min = 0, max = 9223372036854775807
    u112_16  ufixed(112, 16)  | min = 0, max = 2000000
}
`

func TestCUfixedBridgeEmittedShapes(t *testing.T) {
	u := unitFromSource(t, cUfixedBridgeSchema)
	c := New()
	files, err := c.Generate(u, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["ProbeWire.h"])
	for _, want := range []string{
		// Width <= 32 routes through fixed64 with IntBits + 1
		"serialize_write_fixed64( stream, fixed_value, 2, 7, 0, 1 )",
		"serialize_read_fixed64( stream, &fixed_value, 2, 7, 0, 1 )",
		"serialize_write_fixed64( stream, fixed_value, 9, 8, 0, 255 )",
		"serialize_read_fixed64( stream, &fixed_value, 9, 8, 0, 255 )",
		"serialize_write_fixed64( stream, fixed_value, 17, 16, 0, 65535 )",
		"serialize_read_fixed64( stream, &fixed_value, 17, 16, 0, 65535 )",
		"serialize_write_fixed64( stream, fixed_value, 33, 0, 0, 4294967295 )",
		"serialize_read_fixed64( stream, &fixed_value, 33, 0, 0, 4294967295 )",
		// Width == 64 routes through fixed128 with IntBits + 1
		"serialize_write_fixed128( stream, fixed_value, 49, 16, 0, 281474976710655 )",
		"serialize_read_fixed128( stream, &fixed_value, 49, 16, 0, 281474976710655 )",
		"serialize_write_fixed128( stream, fixed_value, 65, 0, 0, 9223372036854775807 )",
		"serialize_read_fixed128( stream, &fixed_value, 65, 0, 0, 9223372036854775807 )",
		// Width == 128 with IntBits < 64 shifts whole-unit bounds by -2^(I-1) and raw value by -2^127
		"serialize_int128_make( value->u1_127.hi ^ 0x8000000000000000ULL, value->u1_127.lo )",
		"serialize_write_fixed128( stream, fixed_value, 1, 127, -1LL, 0LL )",
		"serialize_read_fixed128( stream, &fixed_value, 1, 127, -1LL, 0LL )",
		"value->u1_127 = serialize_uint128_make( fixed_value.hi ^ 0x8000000000000000ULL, fixed_value.lo );",
		"serialize_write_fixed128( stream, fixed_value, 48, 80, -140737488355328LL, 140737488355327LL )",
		"serialize_read_fixed128( stream, &fixed_value, 48, 80, -140737488355328LL, 140737488355327LL )",
		"serialize_write_fixed128( stream, fixed_value, 63, 65, -4611686018427387904LL, 4611686018427387903LL )",
		"serialize_read_fixed128( stream, &fixed_value, 63, 65, -4611686018427387904LL, 4611686018427387903LL )",
		// Width == 128 with IntBits >= 64 passes IntBits directly
		"serialize_write_fixed128( stream, fixed_value, 64, 64, 0, 9223372036854775807 )",
		"serialize_read_fixed128( stream, &fixed_value, 64, 64, 0, 9223372036854775807 )",
		"serialize_write_fixed128( stream, fixed_value, 112, 16, 0, 2000000 )",
		"serialize_read_fixed128( stream, &fixed_value, 112, 16, 0, 2000000 )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("generated ProbeWire.h missing %q", want)
		}
	}
}

func TestCUfixedBridgeRuntimeAgainstSerializeC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("cc unavailable")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	serializeC := os.Getenv("SERIALIZE_C")
	if serializeC == "" {
		serializeC = filepath.Join(filepath.Dir(root), "serialize.c")
	}
	if _, err := os.Stat(filepath.Join(serializeC, "serialize.h")); err != nil {
		t.Skipf("serialize.c not found at %s", serializeC)
	}

	c := New()
	uProbe := unitFromSource(t, cUfixedBridgeSchema)
	probeFiles, err := c.Generate(uProbe, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}

	ludPaths, err := GatherPaths([]string{filepath.Join(root, "examples128")})
	if err != nil {
		t.Fatal(err)
	}
	uLud, err := c.Load(ludPaths)
	if err != nil {
		t.Fatal(err)
	}
	ludFiles, err := c.Generate(uLud, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for name, data := range probeFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range ludFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	goldenPath := filepath.Join(root, "testdata", "wire", "unsigned_probe.bin")
	goldenBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unsigned_probe.bin"), goldenBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	const driver = `#include <stdio.h>
#include <string.h>
#include "ProbeWire.h"
#include "LudicrousWire.h"

#define CHECK(cond, msg) do { \
    if ( !(cond) ) { \
        fprintf( stderr, "FAIL line %d: %s\n", __LINE__, (msg) ); \
        return 1; \
    } \
} while ( 0 )

int main( void )
{
    unsigned char buffer[4096 + 8];
    unsigned char oracle[4096 + 8];
    serialize_write_stream_t w, wo;
    serialize_read_stream_t r;

    /* 1. Ludicrous UnsignedProbe (ufixed(48,16) high-half span) against C++ wire golden */
    {
        ludicrous_unsigned_probe_t in, out;
        unsigned char expected[64];
        FILE * f = fopen( "unsigned_probe.bin", "rb" );
        size_t n;
        CHECK( f != NULL, "open unsigned_probe.bin" );
        n = fread( expected, 1, sizeof( expected ), f );
        fclose( f );
        CHECK( n == 25, "unsigned_probe.bin is 25 bytes" );

        memset( &in, 0, sizeof( in ) );
        in.angle = 2981888;
        in.span = 0xFFFFFFFFFFFF0000ULL;
        in.reach = serialize_uint128_make( 0, 131071999999ULL );
        in.ticks = 777777;
        in.samples[0] = 0;
        in.samples[1] = 1048576;
        in.locked = 196608;
        in.tail = 0xA5;

        memset( buffer, 0, sizeof( buffer ) );
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        CHECK( ludicrous_write_unsigned_probe( &w, &in ), "write UnsignedProbe" );
        serialize_write_flush( &w );
        CHECK( serialize_write_bytes_processed( &w ) == 25, "UnsignedProbe bytes == 25" );
        CHECK( memcmp( buffer, expected, 25 ) == 0, "UnsignedProbe matches C++ wire golden byte-for-byte" );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, 25 );
        CHECK( ludicrous_read_unsigned_probe( &r, &out ), "read UnsignedProbe" );
        CHECK( out.span == 0xFFFFFFFFFFFF0000ULL, "UnsignedProbe.span round-trips bit-exact" );
    }

    /* 2. Full-domain ufixed probe including I + F == 128 (ufixed(1,127), ufixed(48,80), ufixed(63,65), ufixed(64,64)) */
    {
        ufixedprobe_full_domain_probe_t in, out;
        int wb, ob;

        /* 2a. Zero floor */
        memset( &in, 0, sizeof( in ) );
        memset( buffer, 0, sizeof( buffer ) );
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        CHECK( ufixedprobe_write_full_domain_probe( &w, &in ), "write zero FullDomainProbe" );
        serialize_write_flush( &w );
        wb = serialize_write_bytes_processed( &w );

        memset( &out, 0xFF, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, wb );
        CHECK( ufixedprobe_read_full_domain_probe( &r, &out ), "read zero FullDomainProbe" );
        CHECK( out.u1_7 == 0 && out.u8_8 == 0 && out.u16_16 == 0 && out.u32_0 == 0, "narrow zero round-trip" );
        CHECK( out.u48_16 == 0 && out.u64_0 == 0, "64-bit zero round-trip" );
        CHECK( serialize_uint128_equal( out.u1_127, serialize_uint128_make( 0, 0 ) ), "u1_127 zero round-trip" );
        CHECK( serialize_uint128_equal( out.u48_80, serialize_uint128_make( 0, 0 ) ), "u48_80 zero round-trip" );
        CHECK( serialize_uint128_equal( out.u63_65, serialize_uint128_make( 0, 0 ) ), "u63_65 zero round-trip" );
        CHECK( serialize_uint128_equal( out.u64_64, serialize_uint128_make( 0, 0 ) ), "u64_64 zero round-trip" );
        CHECK( serialize_uint128_equal( out.u112_16, serialize_uint128_make( 0, 0 ) ), "u112_16 zero round-trip" );

        /* 2b. Maximum unsigned ceiling across all widths (including bit 127 set in u1_127, u48_80, u63_65) */
        in.u1_7    = 1U << 7;                                                         /* 1.0 in UQ1.7 */
        in.u8_8    = 255U << 8;                                                       /* 255.0 in UQ8.8 */
        in.u16_16  = 65535U << 16;                                                    /* 65535.0 in UQ16.16 */
        in.u32_0   = 4294967295U;                                                     /* 2^32 - 1 in UQ32.0 */
        in.u48_16  = 281474976710655ULL << 16;                                        /* (2^48 - 1) << 16 */
        in.u64_0   = 9223372036854775807ULL;                                          /* 2^63 - 1 */
        in.u1_127  = serialize_uint128_make( 0x8000000000000000ULL, 0ULL );           /* 1 << 127 */
        in.u48_80  = serialize_uint128_make( 0xFFFFFFFFFFFF0000ULL, 0ULL );           /* (2^48 - 1) << 80 */
        in.u63_65  = serialize_uint128_make( 0xFFFFFFFFFFFFFFFEULL, 0ULL );           /* (2^63 - 1) << 65 */
        in.u64_64  = serialize_uint128_make( 0x7FFFFFFFFFFFFFFFULL, 0ULL );           /* (2^63 - 1) << 64 */
        in.u112_16 = serialize_uint128_make( 0ULL, 2000000ULL << 16 );                /* 2000000 << 16 */

        memset( buffer, 0, sizeof( buffer ) );
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        CHECK( ufixedprobe_write_full_domain_probe( &w, &in ), "write max FullDomainProbe" );
        serialize_write_flush( &w );
        wb = serialize_write_bytes_processed( &w );

        /* Verify wire bytes match direct serialize_write_fixed_core oracle */
        memset( oracle, 0, sizeof( oracle ) );
        serialize_write_stream_init( &wo, oracle, sizeof( oracle ) );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u1_7 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 1ULL << 7 ) ), "oracle u1_7" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u8_8 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 255ULL << 8 ) ), "oracle u8_8" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u16_16 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 65535ULL << 16 ) ), "oracle u16_16" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u32_0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 4294967295ULL ) ), "oracle u32_0" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u48_16 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 281474976710655ULL << 16 ) ), "oracle u48_16" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, in.u64_0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 9223372036854775807ULL ) ), "oracle u64_0" );
        CHECK( serialize_write_fixed_core( &wo, in.u1_127, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0x8000000000000000ULL, 0 ) ), "oracle u1_127" );
        CHECK( serialize_write_fixed_core( &wo, in.u48_80, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0xFFFFFFFFFFFF0000ULL, 0 ) ), "oracle u48_80" );
        CHECK( serialize_write_fixed_core( &wo, in.u63_65, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0xFFFFFFFFFFFFFFFEULL, 0 ) ), "oracle u63_65" );
        CHECK( serialize_write_fixed_core( &wo, in.u64_64, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0x7FFFFFFFFFFFFFFFULL, 0 ) ), "oracle u64_64" );
        CHECK( serialize_write_fixed_core( &wo, in.u112_16, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 2000000ULL << 16 ) ), "oracle u112_16" );
        serialize_write_flush( &wo );
        ob = serialize_write_bytes_processed( &wo );
        CHECK( wb == ob, "wire byte count matches core oracle" );
        CHECK( memcmp( buffer, oracle, (size_t) wb ) == 0, "wire bytes match core oracle byte-for-byte" );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, wb );
        CHECK( ufixedprobe_read_full_domain_probe( &r, &out ), "read max FullDomainProbe" );
        CHECK( out.u1_7 == in.u1_7 && out.u8_8 == in.u8_8 && out.u16_16 == in.u16_16 && out.u32_0 == in.u32_0, "narrow max round-trip" );
        CHECK( out.u48_16 == in.u48_16 && out.u64_0 == in.u64_0, "64-bit max round-trip" );
        CHECK( serialize_uint128_equal( out.u1_127, in.u1_127 ), "u1_127 max (1 << 127) round-trip" );
        CHECK( serialize_uint128_equal( out.u48_80, in.u48_80 ), "u48_80 max round-trip" );
        CHECK( serialize_uint128_equal( out.u63_65, in.u63_65 ), "u63_65 max round-trip" );
        CHECK( serialize_uint128_equal( out.u64_64, in.u64_64 ), "u64_64 max round-trip" );
        CHECK( serialize_uint128_equal( out.u112_16, in.u112_16 ), "u112_16 max round-trip" );

        /* 2c. Fractional precision in the high half of uint128 (u1_127 = (1 << 127) - 1, u48_80 with low bits) */
        in.u1_127 = serialize_uint128_make( 0x7FFFFFFFFFFFFFFFULL, 0xFFFFFFFFFFFFFFFFULL );
        in.u48_80 = serialize_uint128_make( 0xABCDEF0123456789ULL, 0xFEDCBA9876543210ULL );
        in.u63_65 = serialize_uint128_make( 0xFEDCBA9876543211ULL, 0x123456789ABCDEF0ULL );
        in.u64_64 = serialize_uint128_make( 0x7EDCBA9876543210ULL, 0xFEDCBA9876543210ULL );
        memset( buffer, 0, sizeof( buffer ) );
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        CHECK( ufixedprobe_write_full_domain_probe( &w, &in ), "write fractional 128-bit FullDomainProbe" );
        serialize_write_flush( &w );
        wb = serialize_write_bytes_processed( &w );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, wb );
        CHECK( ufixedprobe_read_full_domain_probe( &r, &out ), "read fractional 128-bit FullDomainProbe" );
        CHECK( serialize_uint128_equal( out.u1_127, in.u1_127 ), "u1_127 (1<<127)-1 round-trip" );
        CHECK( serialize_uint128_equal( out.u48_80, in.u48_80 ), "u48_80 fractional round-trip" );
        CHECK( serialize_uint128_equal( out.u63_65, in.u63_65 ), "u63_65 fractional round-trip" );
        CHECK( serialize_uint128_equal( out.u64_64, in.u64_64 ), "u64_64 fractional round-trip" );

        /* 2d. Hostile all-ones wire must be refused on read as value_out_of_range */
        memset( buffer, 0xFF, sizeof( buffer ) );
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, wb );
        CHECK( !ufixedprobe_read_full_domain_probe( &r, &out ), "all-ones wire refused" );
        CHECK( r.refusal == SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE, "refusal reason is VALUE_OUT_OF_RANGE" );

        /* 2e. Hostile u1_127 offset = (1 << 127) + 1 (while earlier fields are 0) is refused on read */
        memset( oracle, 0, sizeof( oracle ) );
        serialize_write_stream_init( &wo, oracle, sizeof( oracle ) );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 1ULL << 7 ) ), "hostile u1_7" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 255ULL << 8 ) ), "hostile u8_8" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 65535ULL << 16 ) ), "hostile u16_16" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 4294967295ULL ) ), "hostile u32_0" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 281474976710655ULL << 16 ) ), "hostile u48_16" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 9223372036854775807ULL ) ), "hostile u64_0" );
        CHECK( serialize_write_u128_bits( &wo, serialize_uint128_make( 0x8000000000000000ULL, 1ULL ), 128 ), "hostile u1_127 = (1<<127)+1" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0xFFFFFFFFFFFF0000ULL, 0 ) ), "hostile u48_80" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0xFFFFFFFFFFFFFFFEULL, 0 ) ), "hostile u63_65" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0x7FFFFFFFFFFFFFFFULL, 0 ) ), "hostile u64_64" );
        CHECK( serialize_write_fixed_core( &wo, serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 0 ), serialize_uint128_make( 0, 2000000ULL << 16 ) ), "hostile u112_16" );
        serialize_write_flush( &wo );
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, oracle, wb );
        CHECK( !ufixedprobe_read_full_domain_probe( &r, &out ), "u1_127=(1<<127)+1 wire refused" );
        CHECK( r.refusal == SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE, "u1_127 refusal reason is VALUE_OUT_OF_RANGE" );
    }

    return 0;
}
`
	driverPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(driverPath, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "test_ufixed")
	args := []string{
		"-std=c99", "-O1", "-g", "-Wall", "-Wextra", "-Werror", "-Wtype-limits",
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
		"-I", dir, "-I", serializeC,
		driverPath, filepath.Join(serializeC, "serialize.c"),
		"-o", binPath, "-lm",
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	cmd := exec.Command(binPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
}
