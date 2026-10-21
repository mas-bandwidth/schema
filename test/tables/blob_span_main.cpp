// THE SPAN, on its own (docs/SPEC-TABLES.md §2.5, §6.5).
//
// A byte buffer larger than one 64 KiB slab cannot be bump-allocated in a
// slab: it takes a SPAN of the arena's address space — whole segment indices
// reserved from the cursor and one contiguous block published under the first
// of them. This driver is that one claim and nothing else, so a red here names
// the span rather than a suite.
//
// It is the negative control's driver (`make tables-blob-span-negative-control`
// builds it twice, once over a sabotaged allocator), and it runs under the
// sanitizers, where a blob written into a slab it does not fit is a heap
// overflow the tool names at the byte.

#include "AssetsTable.h"

#include <stdio.h>
#include <string.h>
#include <vector>

static int failures = 0;

#define CHECK( c )                                                                   \
    do                                                                               \
    {                                                                                \
        if ( !( c ) )                                                                \
        {                                                                            \
            printf( "FAIL %s:%d blob past the slab: %s\n", __FILE__, __LINE__, #c ); \
            failures++;                                                              \
        }                                                                            \
    } while ( 0 )

static uint8_t pattern( int64_t i ) { return (uint8_t) ( ( i * 31 + 7 ) & 0xff ); }

// one blob past a slab, written, read back through the builder, and read back
// again after the wire — with more nodes allocated AFTER it, so an allocator
// that handed out addresses inside the span would be seen doing it
static void test_blob_span()
{
    const int64_t big = 66000; // past kTableSlabBytes = 64 KiB

    blobdemo::CatalogBuilder builder;
    blobdemo::Catalog * root = builder.GetRoot();
    memcpy( root->name, "span", 5 );
    root->name_length = 4;

    blobdemo::TableBytesSlot slot = builder.AllocBytes( big );
    CHECK( !slot.null() && slot.length == big );
    if ( slot.null() ) { return; }
    for ( int64_t i = 0; i < big; i++ ) { slot.data[i] = pattern( i ); }
    root->thumb = slot;

    // allocate AFTER the span: a chain of assets and a second blob, so the
    // cursor moves on over whatever the span left behind
    blobdemo::TableSlot<blobdemo::Asset> head = builder.Alloc<blobdemo::Asset>();
    memcpy( head->name, "after", 6 );
    head->name_length = 5;
    CHECK( blobdemo::TableStringEmplace( builder.main, head->caption, "tail", 4 ) != NULL );
    root->head = head;
    blobdemo::TableStringSlot note = builder.AllocString( 24 );
    CHECK( !note.null() );
    memcpy( note.data, "twenty-four chars long!!", 24 );
    root->note = note;

    // the blob is intact in the arena, after everything above
    blobdemo::TableArenaCtx ctx = { &builder.arena };
    blobdemo::TableBytesView live = blobdemo::TableBytesAt( ctx, root->thumb );
    CHECK( live.data != NULL && live.length == big );
    bool intact = live.data != NULL && live.length == big;
    for ( int64_t i = 0; intact && i < big; i++ )
    {
        if ( live.data[i] != pattern( i ) ) { intact = false; }
    }
    CHECK( intact );

    // and across the wire, which reads every one of those bytes
    std::vector<uint8_t> wire( (size_t) ( big + 4096 ) );
    int64_t wrote = blobdemo::CatalogSave( builder, wire.data(), (int64_t) wire.size() );
    CHECK( wrote > big );
    if ( wrote <= 0 ) { return; }
    int64_t need = blobdemo::CatalogLoadMeasure( wire.data(), wrote );
    std::vector<uint8_t> region( (size_t) need );
    blobdemo::TableReport report;
    const blobdemo::Catalog * loaded =
        blobdemo::CatalogLoad( region.data(), need, wire.data(), wrote, &report );
    CHECK( loaded != NULL && !report.malformed );
    if ( loaded == NULL ) { return; }
    blobdemo::TableBytesView back = blobdemo::TableBytesAt( loaded->thumb );
    CHECK( back.data != NULL && back.length == big );
    bool same = back.data != NULL && back.length == big;
    for ( int64_t i = 0; same && i < big; i++ )
    {
        if ( back.data[i] != pattern( i ) ) { same = false; }
    }
    CHECK( same );
    blobdemo::TableStringView note_back = blobdemo::TableStringAt( loaded->note );
    CHECK( note_back.data != NULL && note_back.length == 24 &&
           strcmp( note_back.data, "twenty-four chars long!!" ) == 0 );
}

int main()
{
    test_blob_span();
    if ( failures > 0 )
    {
        printf( "%d failure(s)\n", failures );
        return 1;
    }
    printf( "blob span ok\n" );
    return 0;
}
