/* THE SPAN, C side (docs/SPEC-TABLES.md §2.5, §6.5; docs/PORTING.md M17).

   A byte buffer larger than one 64 KiB slab cannot be bump-allocated in a
   slab: it takes a SPAN of the arena's address space, whole segment indices
   reserved from the cursor and one contiguous block published under the first
   of them. This driver is that one claim, the C twin of
   test/tables/blob_span_main.cpp: one blob past a slab, written, then more
   nodes allocated AFTER it, and the blob read back intact from the arena, from
   the locked region and across the wire. An allocator that bump-allocated the
   blob in a slab it does not fit hands the nodes after it addresses inside the
   blob, and the pattern does not survive. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "AssetsTable.h"

static int failures = 0;
#define CHECK( c ) do { if ( !( c ) ) { printf( "FAIL %s:%d blob past the slab: %s\n", __FILE__, __LINE__, #c ); failures++; } } while ( 0 )

static uint8_t pattern( int64_t i ) { return (uint8_t) ( ( i * 31 + 7 ) & 0xff ); }

static int intact( blobdemo_table_bytes_view_t view, int64_t big )
{
    int64_t i;
    if ( view.data == NULL || (int64_t) view.length != big ) { return 0; }
    for ( i = 0; i < big; i++ )
    {
        if ( view.data[i] != pattern( i ) ) { return 0; }
    }
    return 1;
}

int main( void )
{
    const int64_t big = 66000; /* past BLOBDEMO_K_TABLE_SLAB_BYTES = 64 KiB */
    blobdemo_catalog_builder_t builder;
    blobdemo_catalog_t * root;
    blobdemo_table_sink_t sink;
    blobdemo_table_ctx_t ctx;
    blobdemo_asset_t * head;
    blobdemo_table_report_t report;
    const blobdemo_catalog_t * locked;
    const blobdemo_catalog_t * loaded;
    uint8_t * data;
    uint8_t * wire;
    uint8_t * region;
    int64_t i, wrote, need;
    int chain;

    CHECK( big > (int64_t) BLOBDEMO_K_TABLE_SLAB_BYTES );
    if ( !blobdemo_catalog_builder_init( &builder ) ) { printf( "FAIL builder init\n" ); return 1; }
    root = blobdemo_catalog_builder_root( &builder );
    if ( root == NULL ) { printf( "FAIL builder root\n" ); return 1; }
    memcpy( root->name, "span", 4 );
    root->name_length = 4;

    data = blobdemo_table_bytes_emplace( &builder.main, &root->thumb, big );
    CHECK( data != NULL );
    if ( data == NULL ) { return 1; }
    for ( i = 0; i < big; i++ ) { data[i] = pattern( i ); }

    /* allocate AFTER the span: a chain of assets, a caption and a note, so the
       cursor moves on over whatever the span left behind */
    sink.region = NULL;
    sink.worker = &builder.main;
    {
        blobdemo_table_ref_t * slot = &root->head;
        for ( chain = 0; chain < 64; chain++ )
        {
            blobdemo_asset_t * asset = blobdemo_asset_emplace( &sink, slot );
            CHECK( asset != NULL );
            if ( asset == NULL ) { return 1; }
            memcpy( asset->name, "after", 5 );
            asset->name_length = 5;
            asset->kind = (uint32_t) chain;
            CHECK( blobdemo_table_string_emplace( &builder.main, &asset->caption, "tail", 4 ) != NULL );
            slot = &asset->next;
        }
    }
    CHECK( blobdemo_table_string_emplace( &builder.main, &root->note, "twenty-four chars long!!", 24 ) != NULL );

    /* the blob is intact in the arena, after everything above */
    ctx.arena = &builder.arena;
    CHECK( intact( blobdemo_table_bytes_at( &ctx, &root->thumb ), big ) );
    head = blobdemo_asset_at( &ctx, &root->head );
    CHECK( head != NULL && head->name_length == 5 && memcmp( head->name, "after", 5 ) == 0 );

    /* across the wire, which reads every one of those bytes */
    need = blobdemo_catalog_measure( &ctx, root );
    CHECK( need > big );
    if ( need <= big ) { return 1; }
    wire = (uint8_t *) malloc( (size_t) need );
    if ( wire == NULL ) { return 1; }
    wrote = blobdemo_catalog_save( &ctx, root, wire, need );
    CHECK( wrote == need );

    /* and through Lock, which copies every node into one region */
    CHECK( blobdemo_catalog_builder_lock( &builder ) );
    locked = (const blobdemo_catalog_t *) (const void *) builder.region;
    CHECK( locked != NULL && intact( blobdemo_table_bytes_at( NULL, &locked->thumb ), big ) );

    /* the wire loads into a region of its own measure, the blob intact */
    need = blobdemo_catalog_load_measure( wire, wrote );
    CHECK( need > big );
    region = need > 0 ? (uint8_t *) malloc( (size_t) need ) : NULL;
    if ( region == NULL ) { return 1; }
    memset( &report, 0, sizeof( report ) );
    loaded = blobdemo_catalog_load_tolerant( region, need, wire, wrote, &report );
    CHECK( loaded != NULL && !report.malformed );
    CHECK( loaded != NULL && intact( blobdemo_table_bytes_at( NULL, &loaded->thumb ), big ) );

    free( region );
    free( wire );
    blobdemo_catalog_builder_shutdown( &builder );
    if ( failures != 0 ) { printf( "blob span: %d check(s) failed\n", failures ); return 1; }
    printf( "blob span: a %lld-byte blob past the 64 KiB slab survives the nodes allocated after it, Lock and the wire\n", (long long) big );
    return 0;
}
