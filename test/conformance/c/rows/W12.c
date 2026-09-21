/* W12 — the c leg's fixed-form LAYOUT HASH INCLUDES THE 4-BYTE COUNT
 * (docs/FIXED-FORM-ALGORITHM.md:54-59, schema matrix cell c/W12, audit
 * schema#898).
 *
 * THE LAW (docs/FIXED-FORM-ALGORITHM.md:55-59): "The hash is
 * h := 0xcbf29ce484222325; for each byte v: h ^= v; h *= 0x100000001b3 over
 * the layout's bytes as written, **the 4-byte count included**, and then over
 * the DEFINITIONS DIGEST (§5.2) ... An empty digest leaves the hash the
 * layout's alone."
 *
 * THE VECTOR, DERIVED FROM THE LAW, NOT COPIED FROM A FIXTURE: the layout
 * rides as a u32 ENTRY COUNT, little-endian, followed by 17-byte entries
 * (u64 field id, u8 kind, u32 size, u32 children — ir/fixedform.go:578-592,
 * TableFixedEntryBytes = 17). The unit under test is `fixed table Open
 * { path string(16) }` from test/tables/M1.schema: its closure carries no
 * range, no flags, no bits(N) and no fixed(I,F), so its definitions digest is
 * EMPTY and its hash is fnv1a64 over the layout bytes ALONE — the shape the
 * law's last sentence names, and the one that isolates the count from the
 * digest.
 *
 * THE PRODUCTION PATH UNDER TEST: the emitter laid the constant
 * open_fixed_hash into the lineage open_fixed_known[] (a FixedLineageEntry's
 * Wire, internal/codegen/ctable/fixedlineage.go:53 — ir.TableFixedLayoutHash
 * over ir.TableFixedLayoutBytes), and the reader open_fixed_load — the one
 * path a fixed-form FILE is matched on (docs/FIXED-FORM-ALGORITHM.md §5.3) —
 * selects a lineage entry by the file header's eight hash bytes and then holds
 * the file's layout bytes to that entry's by memcmp. This test exercises that
 * reader on the smallest file the framing allows, twice: with the hash the
 * law names (with the count), which must load; and with the hash a regression
 * would produce (count dropped), which the reader must refuse as layout_newer.
 *
 * Standalone: depends on no other rows/ file, edits no shared file. Build and
 * run exactly as the conformance driver builds its units (make/c.mk:199-206,
 * which links each unit's generated .c beside its unit file):
 *
 *   cc -std=c11 -Wall -Ibuild/tables-generated-c/m1 test/conformance/c/rows/W12.c \
 *       build/tables-generated-c/m1/M1Table.c -o build/rows-c-W12 -lm \
 *       && ./build/rows-c-W12
 */

#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "M1Table.h"

static int failures;

static void check( const char * what, int ok )
{
    printf( "%s: %s\n", what, ok ? "ok" : "FAIL" );
    if ( ! ok ) { failures++; }
}

/* THE HASH ITSELF, straight from the law's words. */
static uint64_t fnv1a64( const uint8_t * bytes, int64_t n )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < n; ++i )
    {
        h ^= (uint64_t) bytes[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

static uint32_t le32( const uint8_t * b )
{
    return (uint32_t) b[0] | ((uint32_t) b[1] << 8) | ((uint32_t) b[2] << 16) | ((uint32_t) b[3] << 24);
}

int main( void )
{
    /* The smallest fixed-form FILE the framing allows (§3.4): the 16-byte
       header (form byte, seven reserved zeros, the hash at 8), the layout
       behind its u32 length, then ONE record — the hash again, then the
       20-byte body (string(16): a u32 byte length and its 16 bytes). */
    uint8_t file[16 + 4 + sizeof( open_fixed_layout ) + 8 + 20];
    TableReport report;
    Open row;
    uint32_t count = le32( open_fixed_layout );
    uint64_t hash_with_count = fnv1a64( open_fixed_layout, open_fixed_layout_bytes );
    uint64_t hash_without_count = fnv1a64( open_fixed_layout + 4, open_fixed_layout_bytes - 4 );
    int64_t got;

    /* 1. The vector's first four bytes ARE the entry count (little-endian)
          and the rest is count 17-byte entries — so the count the law names
          is really there, and the differential below means something. */
    check( "layout opens with the 4-byte entry count, little-endian, then 17-byte entries",
           count == 2 && open_fixed_layout_bytes == 4 + 17 * (int64_t) count );

    /* 2. THE LAW: fnv1a64 over the layout AS WRITTEN — the 4-byte count
          INCLUDED — is the hash the build laid into its lineage. Open's
          digest is empty, so the layout's hash is the whole hash. */
    check( "fnv1a64 over the layout WITH the 4-byte count == the build's open_fixed_hash",
           hash_with_count == open_fixed_hash );

    /* 3. The differential that gives assertion 2 its teeth: the same bytes
          without the count hash to a DIFFERENT number — a hash that skipped
          the count is not a near miss but a stranger. */
    check( "fnv1a64 over the layout WITHOUT the 4-byte count != open_fixed_hash",
           hash_without_count != open_fixed_hash );

    /* 4. THE PRODUCTION PATH, accept: a file whose header hash and record
          hash carry the build's own (with-count) hash loads one row. */
    memset( file, 0, sizeof( file ) );
    file[0] = kTableFixedForm;
    {
        uint8_t * at = file + kTableFixedHeaderBytes;
        table_fixed_put64( file + kTableFixedHashAt, hash_with_count );
        table_fixed_put32( at, (uint32_t) sizeof( open_fixed_layout ) );
        at += 4;
        memcpy( at, open_fixed_layout, sizeof( open_fixed_layout ) );
        at += sizeof( open_fixed_layout );
        table_fixed_put64( at, hash_with_count );
        /* the 20-byte body stays zero: an empty string(16) */
    }
    memset( &row, 0xAA, sizeof( row ) );
    memset( &report, 0, sizeof( report ) );
    got = open_fixed_load( &row, 1, file, (int64_t) sizeof( file ), NULL, 0, NULL, &report );
    check( "open_fixed_load accepts a file whose hash is fnv1a64 over the layout WITH the count",
           got == 1 && report.refused == 0 && report.malformed == 0 && row.path_length == 0 );

    /* 5. THE PRODUCTION PATH, control (compiles, and bites): the same file
          with the hash a count-dropping regression would produce — fnv1a64
          over the layout WITHOUT the count — in the header AND the record.
          No lineage entry holds it, so the reader must refuse layout_newer
          and report THE FILE'S hash, writing not one destination byte. */
    table_fixed_put64( file + kTableFixedHashAt, hash_without_count );
    memcpy( file + kTableFixedHeaderBytes + 4 + sizeof( open_fixed_layout ), &hash_without_count, 8 );
    memset( &row, 0xAA, sizeof( row ) );
    memset( &report, 0, sizeof( report ) );
    got = open_fixed_load( &row, 1, file, (int64_t) sizeof( file ), NULL, 0, NULL, &report );
    check( "open_fixed_load refuses the count-less hash as layout_newer, carrying the file's hash",
           got == -1 && report.refused == 1 && report.reason == SCHEMA_TABLE_LAYOUT_NEWER
           && report.layout_hash == hash_without_count );

    printf( "W12 c leg: %s (%d failure%s)\n", failures == 0 ? "PASS" : "FAIL",
            failures, failures == 1 ? "" : "s" );
    return failures == 0 ? 0 : 1;
}
