// The CROSS-ENDIAN BLOCK driver (SPEC-TABLES.md §19.1).
//
// A block is memory one language wrote and another points at, and it is
// produced in the byte order of the build that wrote it. Every other block
// test in this tree runs producer and consumer in ONE process, so the rule
// that a block does not cross a byte order is the one part of §19 no
// single-process test can reach — it needs two builds and a file between them.
//
// This driver is one of those builds. The Makefile's tables-big-endian leg
// runs it four ways: the host writes a block the big-endian target must
// refuse, the target writes one the host must refuse, and each opens its own.
//
//   block-endian write  <path>   lay a block out, fill it, write BlockBytes bytes
//   block-endian accept <path>   BlockOpen must succeed and the rows must read back
//   block-endian refuse <path>   BlockOpen must return false, and BY BYTE ORDER
//
// The refusal case proves three things and not just the first: that BlockOpen
// returned false rather than pointing at rows it cannot read, that the file's
// magic is this build's magic byte-REVERSED — so byte order is what refused
// it, not some other mismatch — and that the recorded `byte_order` word names
// the other order once read the way that file wrote it. The magic is what
// refuses; the word is what says which order it was.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "RenderBlock.h"

using namespace blockdemo;

static int failures = 0;

#define CHECK( cond )                                               \
    do {                                                            \
        if ( !( cond ) ) {                                          \
            printf( "FAIL %s:%d %s\n", __FILE__, __LINE__, #cond ); \
            failures++;                                             \
        }                                                           \
    } while ( 0 )

// A frame small enough to write in a moment under an emulator, and wide
// enough that every array carries rows.
static RenderFrameCounts endian_counts()
{
    RenderFrameCounts counts = {};
    counts.cameras = 1;
    counts.ships = 4;
    counts.turrets = 3;
    counts.missiles = 2;
    counts.dynamic_props = 2;
    counts.static_props = 3;
    counts.cosmetic_props = 2;
    counts.lasers = 2;
    counts.explosions = 1;
    return counts;
}

static void fill( RenderFrameBlock & block, const RenderFrameCounts & counts )
{
    RenderCamera * cameras = RenderFrameCameras( block );
    for ( int i = 0; i < counts.cameras; i++ )
    {
        cameras[i].camera_id = (uint32_t) ( i + 1 );
        cameras[i].fov = 60.0f + i;
        cameras[i].position.x = 1.5 * i;
    }
    RenderShip * ships = RenderFrameShips( block );
    for ( int i = 0; i < counts.ships; i++ )
    {
        ships[i].object_id = (uint32_t) ( i * 3 + 11 );
        ships[i].thrust = 0.25f * i;
        ships[i].team = (Team) ( i % 5 );
        ships[i].has_target_lock = ( i % 2 ) == 0;
        ships[i].position.y = 2.25 * i;
    }
    RenderTurret * turrets = RenderFrameTurrets( block );
    for ( int i = 0; i < counts.turrets; i++ ) { turrets[i].object_id = (uint32_t) ( i * 2 + 1 ); }
    RenderMissile * missiles = RenderFrameMissiles( block );
    for ( int i = 0; i < counts.missiles; i++ ) { missiles[i].object_id = (uint32_t) ( i * 23 ); }
    RenderDynamicProp * dynamic_props = RenderFrameDynamicProps( block );
    for ( int i = 0; i < counts.dynamic_props; i++ ) { dynamic_props[i].object_id = (uint32_t) ( i * 31 ); }
    RenderStaticProp * static_props = RenderFrameStaticProps( block );
    for ( int i = 0; i < counts.static_props; i++ ) { static_props[i].static_prop_id = (uint32_t) ( i * 41 ); }
    RenderCosmeticProp * cosmetic_props = RenderFrameCosmeticProps( block );
    for ( int i = 0; i < counts.cosmetic_props; i++ ) { cosmetic_props[i].cosmetic_prop_id = (uint32_t) ( i * 47 ); }
    RenderLaser * lasers = RenderFrameLasers( block );
    for ( int i = 0; i < counts.lasers; i++ ) { lasers[i].laser_id = (uint32_t) ( i * 53 ); }
    RenderExplosion * explosions = RenderFrameExplosions( block );
    for ( int i = 0; i < counts.explosions; i++ ) { explosions[i].explosion_id = (uint32_t) ( i * 59 ); }
}

// The caller owns the bytes and a block's base is 64-byte aligned by
// construction, so the reader allocates roomy and aligns inside.
static uint8_t * read_file( const char * path, int64_t & bytes, void *& allocation )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    const long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    allocation = malloc( (size_t) size + 64 );
    if ( allocation == NULL ) { fclose( f ); return NULL; }
    uint8_t * base = (uint8_t *) ( ( (uintptr_t) allocation + 63 ) & ~(uintptr_t) 63 );
    bytes = (int64_t) fread( base, 1, (size_t) size, f );
    fclose( f );
    return base;
}

int main( int argc, char ** argv )
{
    if ( argc != 3 )
    {
        printf( "usage: block-endian write|accept|refuse <path>\n" );
        return 2;
    }
    const char * verb = argv[1];
    const char * path = argv[2];
    const RenderFrameCounts counts = endian_counts();

    if ( strcmp( verb, "write" ) == 0 )
    {
        RenderFrameBlockStorage storage;
        if ( !storage.Create( TableBlockDefaultAllocator() ) )
        {
            printf( "FAIL the block storage did not allocate\n" );
            return 1;
        }
        memset( storage.base, 0, (size_t) RenderFrameBlockMaxBytes );
        RenderFrameBlock block;
        CHECK( RenderFrameBlockBegin( block, storage, counts ) );
        block.projection->version = RenderVersion;
        fill( block, counts );
        const int64_t bytes = RenderFrameBlockBytes( block );
        FILE * f = fopen( path, "wb" );
        if ( f == NULL )
        {
            printf( "FAIL cannot write %s\n", path );
            storage.Destroy();
            return 1;
        }
        fwrite( storage.base, 1, (size_t) bytes, f );
        fclose( f );
        printf( "wrote %s: %lld bytes, build version 0x%016llx, byte order %llu\n",
                path, (long long) bytes, (unsigned long long) BuildVersion,
                (unsigned long long) block.projection->byte_order );
        storage.Destroy();
        return failures == 0 ? 0 : 1;
    }

    int64_t bytes = 0;
    void * allocation = NULL;
    uint8_t * base = read_file( path, bytes, allocation );
    if ( base == NULL )
    {
        printf( "FAIL cannot read %s\n", path );
        return 1;
    }

    if ( strcmp( verb, "accept" ) == 0 )
    {
        RenderFrameBlock block;
        CHECK( RenderFrameBlockOpen( block, base, bytes ) );
        if ( failures == 0 )
        {
            CHECK( block.projection->version == RenderVersion );
            CHECK( block.projection->byte_order == TableBlockByteOrder );
            CHECK( RenderFrameShipsSpan( block ).size() == counts.ships );
            const RenderShip * ships = RenderFrameShips( block );
            for ( int i = 0; i < counts.ships; i++ )
            {
                CHECK( ships[i].object_id == (uint32_t) ( i * 3 + 11 ) );
                CHECK( ships[i].thrust == 0.25f * i );
                CHECK( ships[i].has_target_lock == ( ( i % 2 ) == 0 ) );
                CHECK( ships[i].position.y == 2.25 * i );
            }
            CHECK( RenderFrameCameras( block )[0].fov == 60.0f );
        }
        printf( failures == 0 ? "accept OK\n" : "accept FAILED\n" );
    }
    else if ( strcmp( verb, "refuse" ) == 0 )
    {
        RenderFrameBlock block;
        CHECK( !RenderFrameBlockOpen( block, base, bytes ) );
        CHECK( block.base == NULL ); // it points at nothing, rather than at rows it cannot read

        // and the refusal is BY BYTE ORDER, not by some other mismatch: the
        // file's magic read the way THIS build reads is this build's magic,
        // byte-reversed.
        const uint64_t magic = table_block_read64( base );
        CHECK( magic != TableBlockMagic );
        CHECK( table_block_byteswap64( magic ) == TableBlockMagic );

        // the recorded word names the OTHER order, once read the way the file
        // wrote it — which is what the word is for
        const uint64_t recorded = table_block_byteswap64( table_block_read64( base + 16 ) );
        CHECK( recorded != TableBlockByteOrder );
        CHECK( recorded == 1 || recorded == 2 );

        // a clean refusal leaves the reader able to carry on, which is the
        // whole point of returning false
        RenderFrameBlockStorage storage;
        CHECK( storage.Create( TableBlockDefaultAllocator() ) );
        memset( storage.base, 0, (size_t) RenderFrameBlockMaxBytes );
        RenderFrameBlock own;
        CHECK( RenderFrameBlockBegin( own, storage, counts ) );
        fill( own, counts );
        CHECK( RenderFrameBlockOpen( own, storage.base, RenderFrameBlockBytes( own ) ) );
        storage.Destroy();

        printf( failures == 0 ? "refuse OK (this build's order is %llu, the file's is %llu)\n" : "refuse FAILED\n",
                (unsigned long long) TableBlockByteOrder, (unsigned long long) recorded );
    }
    else
    {
        printf( "FAIL unknown verb %s\n", verb );
        failures++;
    }
    free( allocation );
    return failures == 0 ? 0 : 1;
}
