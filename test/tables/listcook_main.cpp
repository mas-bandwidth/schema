// THE TWO WRITERS OF ONE LIST COOK MEET (schema#380, docs/SPEC-TABLES.md §2.9,
// §7.6).
//
// `schema cook` is the REFERENCE for the cooked form's bytes and the generated
// `<Root>Cook` is held to it, byte for byte, in BOTH byte orders — a cook is
// content-addressed by (asset hash, build version), so two writers of one
// instance produce ONE artifact or the pair means nothing. Until schema#380
// the tool refused a list-bearing unit at its cook surface, so the list class
// had no such pairing at all; this binary is it.
//
// It reads three files the tool wrote, over test/tables/L1.schema:
//
//   l1.bin       the tolerant wire of the instance
//   l1.cook      the tool's little-endian cook of it
//   l1-be.cook   the tool's big-endian cook of it
//
// loads the wire into a builder through the ordinary tolerant path, cooks it
// with the generated writer, and compares. The wire comes back out of the
// tool's own cook in the Go leg beside this one; what THIS leg adds is that
// the two writers agree on the bytes.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "L1Table.h"

using namespace tbll1;

static int failures = 0;

static uint8_t wire_bytes[1u << 20];
static uint8_t pinned[1u << 20];
static uint8_t cooked[1u << 20];

static int64_t slurp( const char * path, uint8_t * into, int64_t capacity )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAIL cannot read %s (it is written by the Go leg: SCHEMA_LIST_TOOL_COOK_DIR)\n", path );
        failures++;
        return -1;
    }
    const size_t n = fread( into, 1, (size_t) capacity, f );
    fclose( f );
    return (int64_t) n;
}

static void compare( const char * dir, const char * name, TableByteOrder order,
                     const SaveBuilder & builder )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s", dir, name );
    const int64_t want = slurp( path, pinned, (int64_t) sizeof( pinned ) );
    if ( want < 0 ) { return; }

    const int64_t need = SaveCookMeasure( builder );
    if ( need != want )
    {
        printf( "FAIL %s: the generated CookMeasure answers %lld and the tool wrote %lld bytes\n",
                name, (long long) need, (long long) want );
        failures++;
        return;
    }
    if ( !SaveCook( builder, cooked, (uint64_t) need, order ) )
    {
        printf( "FAIL %s: the generated Cook refused a graph the tool cooked\n", name );
        failures++;
        return;
    }
    if ( memcmp( cooked, pinned, (size_t) want ) != 0 )
    {
        int64_t at = 0;
        while ( at < want && cooked[at] == pinned[at] ) { at++; }
        printf( "FAIL %s: the two writers disagree at byte %lld (generated 0x%02x, tool 0x%02x)\n",
                name, (long long) at, cooked[at], pinned[at] );
        failures++;
        return;
    }
    printf( "%s: the generated Cook lands on the tool's %lld bytes exactly\n", name, (long long) want );
}

int main( int argc, char ** argv )
{
    if ( argc < 2 )
    {
        printf( "usage: schema_test_listcook <dir holding l1.bin, l1.cook, l1-be.cook>\n" );
        return 2;
    }
    const char * dir = argv[1];

    char path[512];
    snprintf( path, sizeof( path ), "%s/l1.bin", dir );
    const int64_t wire = slurp( path, wire_bytes, (int64_t) sizeof( wire_bytes ) );
    if ( wire < 0 ) { return 1; }

    SaveBuilder builder;
    TableReport report;
    if ( !SaveLoadBuilder( builder, wire_bytes, wire, &report ) )
    {
        printf( "FAIL the reference refused the wire the tool wrote\n" );
        return 1;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 || report.malformed )
    {
        printf( "FAIL the wire did not read clean (unknown %d, kind_mismatch %d, clamped %d, malformed %d)\n",
                report.unknown, report.kind_mismatch, report.clamped, (int) report.malformed );
        return 1;
    }

    compare( dir, "l1.cook", TableByteOrder::Little, builder );
    compare( dir, "l1-be.cook", TableByteOrder::Big, builder );

    if ( failures > 0 )
    {
        printf( "list tool-cook: %d failure(s)\n", failures );
        return 1;
    }
    printf( "list tool-cook: the tool and the reference write ONE cook of a list, both byte orders\n" );
    return 0;
}
