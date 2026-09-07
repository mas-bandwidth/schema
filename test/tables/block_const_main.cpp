/*
    THE BLOCK FORM's CONST READ PATH (docs/SPEC-TABLES.md §19.2, schema#455).

    A block is memory another build wrote, and a consumer of foreign bytes
    reads it. This is the leg that holds the READ side to that: a pinned block
    image is loaded into a 64-byte aligned buffer, held as a `const uint8_t *`
    from that moment on, and opened WITH NO CAST. Every row it reads comes back
    read-only, so the write access a producer needs never reaches a consumer
    that only reads.

    It is one program with two halves, and the Makefile runs both:

      * the DEFAULT build opens the const region, walks its rows through the
        const view and compares them against the same bytes opened writable, so
        the two overloads agree value for value and the producer's path is
        proved unchanged;
      * `-DBLOCK_CONST_WRITE` adds ONE assignment through the const view, and
        that build MUST NOT COMPILE. A read-only view whose writes compile is a
        view in name only, which is what the negative compile control holds.

    Prints OK and exits 0: no test framework, exit code is the verdict.
*/

#include "RenderBlock.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

using namespace blockdemo;

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok )
    {
        printf( "FAILED: %s\n", what );
        failures++;
    }
}

// The image, read off disk into a 64-byte aligned buffer, which is §19.1's
// base alignment and BlockOpen's last clause. The allocation is handed
// back beside the base so the caller frees what it was given rather than the
// pointer it aligned.
static uint8_t * load_image( const char * path, int64_t * bytes, void ** allocation )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    const long length = ftell( f );
    fseek( f, 0, SEEK_SET );
    if ( length <= 0 ) { fclose( f ); return NULL; }
    void * raw = malloc( (size_t) length + 63 );
    if ( raw == NULL ) { fclose( f ); return NULL; }
    uint8_t * base = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 );
    const size_t read = fread( base, 1, (size_t) length, f );
    fclose( f );
    if ( read != (size_t) length ) { free( raw ); return NULL; }
    *bytes = (int64_t) length;
    *allocation = raw;
    return base;
}

int main( int argc, char ** argv )
{
    const char * path = argc > 1 ? argv[1] : "testdata/wire/tables/block_render.bin";

    int64_t bytes = 0;
    void * allocation = NULL;
    uint8_t * loaded = load_image( path, &bytes, &allocation );
    if ( loaded == NULL )
    {
        printf( "FAILED: could not read the block image %s\n", path );
        return 1;
    }

    // FROM HERE THE BYTES ARE CONST, and nothing below casts that away. This
    // is the consumer an mmap of a read-only file hands its bytes to (#455).
    const uint8_t * region = loaded;

    RenderFrameBlock::Const block;
    TableRefuseReason reason = ok;
    check( RenderFrameBlockOpen( block, region, bytes, &reason ),
           "a const region opens with no cast (docs/SPEC-TABLES.md §19.2)" );
    check( reason == ok, "a matched open writes no reason" );
    check( block.base == region, "the const handle points at the bytes it was given" );
    check( block.projection != NULL, "the const handle carries the projection" );
    check( block.bytes > 0 && block.bytes <= bytes, "the used extent lies inside the caller's bytes" );

    // THE ROWS, READ-ONLY. The iteration is §19.2's, at the pitch the instance
    // gives and never spelled at the call site, and what it yields is a
    // reference a consumer cannot write through.
    int32_t iterated = 0;
    uint32_t checksum = 0;
    for ( const RenderShip & ship : RenderFrameShips( block ) )
    {
        iterated++;
        checksum += ship.object_id;
    }
    TableBlockConstRows<RenderShip> rows = RenderFrameShips( block );
    check( iterated == rows.size(), "the const iteration visits every row of the array" );
    check( rows.size() == (int32_t) block.projection->ships.count,
           "and the count it walks is the INSTANCE's, never a constant of this build (§19.2)" );

    TableBlockConstSpan<RenderShip> span = RenderFrameShipsSpan( block );
    check( span.size() == rows.size(), "the contiguous const view carries the same count" );
    uint32_t span_checksum = 0;
    for ( int32_t i = 0; i < span.size(); i++ ) { span_checksum += span[i].object_id; }
    check( span_checksum == checksum, "and the same rows: the pitch IS sizeof (§2.7)" );

    const RenderShip * base_pointer = span.begin();
    check( base_pointer == (const RenderShip *) ( region + block.projection->ships.offset_of ),
           "the const span begins at the array's own offset_of" );

    // THE PRODUCER'S PATH IS UNCHANGED, and that is half the claim: the same
    // bytes, opened through the mutable overload, are the same rows. A copy is
    // taken because a mutable open is a mutable base, and the const region
    // above must not be reachable from it.
    void * mutable_allocation = NULL;
    int64_t mutable_bytes = 0;
    uint8_t * writable = load_image( path, &mutable_bytes, &mutable_allocation );
    if ( writable == NULL )
    {
        printf( "FAILED: could not read the block image a second time\n" );
        free( allocation );
        return 1;
    }
    RenderFrameBlock mutable_block;
    check( RenderFrameBlockOpen( mutable_block, writable, mutable_bytes ),
           "the mutable overload still opens the same bytes" );
    TableBlockRows<RenderShip> mutable_rows = RenderFrameShips( mutable_block );
    check( mutable_rows.size() == rows.size(), "both overloads read the same count" );
    int mismatches = 0;
    for ( int32_t i = 0; i < rows.size(); i++ )
    {
        if ( memcmp( &rows[i], &mutable_rows[i], sizeof( RenderShip ) ) != 0 ) { mismatches++; }
    }
    check( mismatches == 0, "and the same row bytes, value for value" );

    // A NULL base and a short buffer answer on the const overload exactly as
    // they do on the mutable one: one check, two entry points (§19.2).
    RenderFrameBlock::Const refused;
    reason = ok;
    check( !RenderFrameBlockOpen( refused, (const void *) NULL, bytes, &reason ), "a null const base refuses" );
    check( reason == unaligned_base, "and names the caller's own defect" );
    reason = ok;
    check( !RenderFrameBlockOpen( refused, region, 8, &reason ), "a const buffer shorter than the projection refuses" );
    check( reason == truncated, "and names it truncated" );
    // the base's alignment is the LAST clause (§19.2), so the image is moved
    // whole to a base eight bytes off the sixty-four: every clause before it
    // reads exactly what it read above, and only the alignment is wrong.
    void * offset_allocation = malloc( (size_t) bytes + 128 );
    if ( offset_allocation == NULL )
    {
        printf( "FAILED: could not allocate the unaligned image\n" );
        free( allocation );
        return 1;
    }
    uint8_t * offset_base = (uint8_t *) ( ( (uintptr_t) offset_allocation + 63 ) & ~(uintptr_t) 63 ) + 8;
    memcpy( offset_base, region, (size_t) bytes );
    reason = ok;
    check( !RenderFrameBlockOpen( refused, (const uint8_t *) offset_base, bytes, &reason ),
           "an unaligned const base refuses" );
    check( reason == unaligned_base, "and names the caller's own defect, last (§19.2)" );
    free( offset_allocation );

#if defined( BLOCK_CONST_WRITE )
    // THE NEGATIVE COMPILE CONTROL. One assignment through the const view, and
    // this build must not compile: a read-only view whose writes compile is a
    // view in name only.
    RenderFrameShipsSpan( block )[0].object_id = 1;
    // and the rows view's two other doors, the typed base and the iterator,
    // each held read-only by the same build
    RenderFrameShips( block )[0].object_id = 1;
    for ( RenderShip & ship : RenderFrameShips( block ) ) { ship.object_id = 2; }
#endif

    free( mutable_allocation );
    free( allocation );

    if ( failures > 0 )
    {
        printf( "%d failed\n", failures );
        return 1;
    }
    printf( "OK: a const block region opens with no cast, and its rows read back read-only (docs/SPEC-TABLES.md §19.2)\n" );
    return 0;
}
