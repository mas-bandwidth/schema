/* W13 — layout+hash == C++ reference (cell c/W13).
 *
 * The C emitter and the C++ reference emitter both call ir.TableFixedLayoutHash
 * and ir.TableFixedLayoutBytes for the same schema, so the emitted constants
 * must be identical. This test asserts that fact for the FX1 schema (the old
 * side of the versioning conformance pair):
 *   - the hash constant matches the C++ reference value
 *   - the layout bytes are byte-identical to the C++ reference
 *   - the body and record byte counts match
 *
 * The C++ reference values are taken from build/tables-generated-cpp/fx1/FX1Table.h,
 * emitted by internal/codegen/cpptable/fixedform.go from test/tables/FX1.schema.
 * Both backends call ir.TableFixedLayoutHash and ir.TableFixedLayoutBytes.
 *
 * Compile: cc -std=c11 -Wall -Ibuild/tables-generated-c-fixed
 *          test/conformance/c/rows/W13.c -o build/rows-c-W13
 * Run:     ./build/rows-c-W13
 */

#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stdlib.h>

#include "FX1Table.h"

static int failures = 0;

static void check( int ok, const char * what )
{
    if ( !ok ) { printf( "FAIL: %s\n", what ); failures++; }
}

/* --- C++ reference constants from build/tables-generated-cpp/fx1/FX1Table.h --- */
/* Emitted by internal/codegen/cpptable/fixedform.go from the same schema. */
static const uint64_t cpp_fx_root_hash   = 0x4000ff732f26789aull;
static const int64_t  cpp_fx_root_body   = 64;
static const uint64_t cpp_fx_nested_hash = 0x0e1d2c15925551c4ull;
static const int64_t  cpp_fx_nested_body = 8;

/* The C++ reference layout bytes for FxRoot, from
   build/tables-generated-cpp/fx1/FX1Table.h FxRootFixedLayout[]. */
static const uint8_t cpp_fx_root_layout[] = {
    0x0d, 0x00, 0x00, 0x00, 0x6f, 0xc7, 0x08, 0x1b, 0xfa, 0x70, 0x85, 0xf9, 0x0d, 0x40, 0x00, 0x00,
    0x00, 0x08, 0x00, 0x00, 0x00, 0x48, 0xf8, 0xf6, 0x5c, 0xd7, 0x7c, 0x5d, 0x58, 0x08, 0x04, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x9a, 0xc2, 0x23, 0xf2, 0x06, 0x9b, 0x56, 0x96, 0x07, 0x02,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x75, 0xe4, 0x74, 0xde, 0x28, 0xe6, 0x1d, 0xd1, 0x04,
    0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xce, 0xb8, 0xa0, 0x0e, 0x72, 0xfb, 0xe8, 0x9c,
    0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a, 0x0a, 0x53, 0x07, 0x8b, 0xf0, 0xc5,
    0xef, 0x0d, 0x08, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x8c, 0xec, 0x01, 0x86, 0x4c, 0xdc,
    0x63, 0xaf, 0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xa5, 0xf1, 0x01, 0x86, 0x4c,
    0xdf, 0x63, 0xaf, 0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x3d, 0x62, 0xcb, 0x8f,
    0xec, 0xfc, 0xf7, 0x39, 0x0c, 0x0c, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x29, 0x6e, 0x72,
    0xe1, 0xa4, 0xd4, 0x77, 0x90, 0x0e, 0x14, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xca,
    0x48, 0x91, 0xc2, 0x9b, 0xb3, 0x73, 0xc5, 0x0e, 0x0a, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x06, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00,
};

static void test_fx_hash( void )
{
    check( fx_root_fixed_hash == cpp_fx_root_hash,
           "FX root hash == C++ reference" );
    check( fx_nested_fixed_hash == cpp_fx_nested_hash,
           "FX nested hash == C++ reference" );
}

static void test_fx_body_bytes( void )
{
    check( fx_root_fixed_body_bytes == cpp_fx_root_body,
           "FX root body bytes == C++ reference" );
    check( fx_nested_fixed_body_bytes == cpp_fx_nested_body,
           "FX nested body bytes == C++ reference" );
}

static void test_fx_record_bytes( void )
{
    check( fx_root_fixed_record_bytes == 8 + cpp_fx_root_body,
           "FX root record bytes == 8 + body" );
    check( fx_nested_fixed_record_bytes == 8 + cpp_fx_nested_body,
           "FX nested record bytes == 8 + body" );
}

static void test_fx_layout_length( void )
{
    check( fx_root_fixed_layout_bytes == (int64_t) sizeof( cpp_fx_root_layout ),
           "FX root layout bytes == C++ reference layout length" );
}

static void test_fx_layout_bytes( void )
{
    check( memcmp( fx_root_fixed_layout, cpp_fx_root_layout,
                   sizeof( cpp_fx_root_layout ) ) == 0,
           "FX root layout bytes == C++ reference (byte-identical)" );
}

/* NEGATIVE CONTROL: break one byte of the layout and verify the hash changes.
   We XOR a byte in a copy and recompute the hash via table_fixed_hash_of. */
static void test_negative_control( void )
{
    uint8_t altered[256];
    uint64_t wrong_hash;
    size_t i;
    int changed = 0;

    check( sizeof( altered ) >= (size_t) fx_root_fixed_layout_bytes,
           "NEGATIVE CONTROL: the alteration buffer is large enough" );

    memcpy( altered, fx_root_fixed_layout, (size_t) fx_root_fixed_layout_bytes );
    /* flip the first non-zero byte past the 4-byte header */
    for ( i = 4; i < (size_t) fx_root_fixed_layout_bytes; i++ )
    {
        if ( altered[i] != 0 ) { altered[i] ^= 0xFF; changed = 1; break; }
    }
    check( changed, "NEGATIVE CONTROL: found a non-zero byte to flip" );

    wrong_hash = table_fixed_hash_of( altered, fx_root_fixed_layout_bytes );
    check( wrong_hash != fx_root_fixed_hash,
           "NEGATIVE CONTROL: a flipped layout byte produces a different hash" );
}

int main( void )
{
    test_fx_hash();
    test_fx_body_bytes();
    test_fx_record_bytes();
    test_fx_layout_length();
    test_fx_layout_bytes();
    test_negative_control();

    if ( failures == 0 )
    {
        printf( "PASS: c/W13 layout+hash == C++ reference\n" );
        return 0;
    }
    printf( "%d failures\n", failures );
    return 1;
}
