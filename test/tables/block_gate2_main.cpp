/*
    GATE 2's C++ half (SPEC-TABLES.md §12.1): the per-frame WRITE, generated
    form against the hand-written scatter it replaces.

    THE BAR, in the owner's words: the generated form must be the SAME SPEED,
    or "not significantly slower", than the hand-written scatter. A regression
    is a defect to explain or close, not a trade to license.

    WHAT THE TWO ARMS ARE. Both write the same nine strided sections of the same
    generated row structs into one 64-byte-aligned extent, serially, from the
    same values. They differ in exactly one thing, which is the thing the form
    changes:

      * the HAND arm computes the nine section offsets in nine hand-written
        lines, stamps a hand-declared header of nine (offset, count, stride)
        triples, and casts the base plus each offset to the row type — the
        shape this form replaces, transcribed;
      * the GENERATED arm calls Begin, which walks the same nine pitches, and
        takes each array's typed base from the generated accessor.

    THE GOLDEN GATE COMES FIRST (the estate's bench rule): the two arms' bytes
    are compared before any clock starts, and a mismatch REFUSES to bench.
    Timing something that does not agree measures nothing.

    Arms are interleaved in one sitting and reported as medians, because a
    batch-shaped row swings between byte-identical binaries and pairing is the
    only thing that removes the sitting from the comparison.
*/

#include "RenderBlock.h"

#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

using namespace blockdemo;

// ---- the hand-written scatter this form replaces ----
//
// A transcription of the shape the dogfood holds by hand today: a header of
// nine (offset, count, stride) triples, nine lines of offset arithmetic, and
// nine casts. The ROW types are the generated ones on both arms, because they
// are generated in the hand-written version too — what is hand-kept there is
// the LAYOUT and the header, and that is what this arm keeps by hand.

struct HandSection
{
    uint64_t offset;
    uint32_t count;
    uint32_t stride;
};

struct HandHeader
{
    uint64_t version;
    HandSection cameras;
    HandSection ships;
    HandSection turrets;
    HandSection missiles;
    HandSection dynamic_props;
    HandSection static_props;
    HandSection cosmetic_props;
    HandSection lasers;
    HandSection explosions;
};

static uint64_t hand_align( uint64_t offset )
{
    return ( offset + 63 ) & ~(uint64_t) 63;
}

static HandSection hand_section( uint64_t offset, int count, size_t stride )
{
    HandSection section;
    section.offset = offset;
    section.count = (uint32_t) count;
    section.stride = (uint32_t) stride;
    return section;
}

// ---- the values, one function of the row index, shared by both arms ----

static RenderVector3 vec3_for( int salt, int i )
{
    RenderVector3 v;
    v.x = 1.5 * i + salt;
    v.y = 2.25 * i - salt;
    v.z = 0.5 * i + 2 * salt;
    return v;
}

static RenderQuaternion quat_for( int salt, int i )
{
    RenderQuaternion q;
    q.x = 0.25 * i + salt;
    q.y = 0.125 * i;
    q.z = 0.0625 * i - salt;
    q.w = 1.0 - 0.5 * ( i % 3 );
    return q;
}

static void fill_camera( RenderCamera & r, int i, int seed )
{
    r.position = vec3_for( 1, i );
    r.rotation = quat_for( 1, i );
    r.camera_id = (uint32_t) ( i * 7 + 1 + seed );
    r.camera_type = (uint32_t) ( i % 4 );
    r.target_object_id = (uint32_t) ( i * 13 + 2 );
    r.fov = 0.5f * i + 60.0f;
}

static void fill_ship( RenderShip & r, int i, int seed )
{
    r.position = vec3_for( 2, i );
    r.rotation = quat_for( 2, i );
    r.flags = (uint64_t) ( i % 16 );
    r.object_id = (uint32_t) ( i * 3 + 11 + seed );
    r.target_object_id = (uint32_t) ( i * 5 + 7 );
    r.thrust = 0.25f * i;
    r.object_sequence = (uint8_t) ( i % 251 );
    r.ship_type = (ShipType) ( i % 4 );
    r.team = (Team) ( i % 5 );
    r.has_target_lock = ( i % 2 ) == 0;
    r.predicted_explode = ( i % 3 ) == 0;
}

static void fill_turret( RenderTurret & r, int i, int seed )
{
    r.rotation = quat_for( 3, i );
    r.flags = (uint64_t) ( i * 17 );
    r.object_id = (uint32_t) ( i * 2 + 1 + seed );
    r.parent_object_id = (uint32_t) ( i / 3 );
    r.turret_index = (uint32_t) ( i % 8 );
    r.target_object_id = (uint32_t) ( i * 11 );
    r.object_sequence = (uint8_t) ( i % 253 );
    r.team = (Team) ( i % 5 );
    r.has_target_lock = ( i % 5 ) == 0;
}

static void fill_missile( RenderMissile & r, int i, int seed )
{
    r.position = vec3_for( 4, i );
    r.rotation = quat_for( 4, i );
    r.flags = (uint64_t) ( i * 19 );
    r.object_id = (uint32_t) ( i * 23 + seed );
    r.object_sequence = (uint8_t) ( i % 249 );
    r.missile_type = (MissileType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

static void fill_dynamic_prop( RenderDynamicProp & r, int i, int seed )
{
    r.position = vec3_for( 5, i );
    r.rotation = quat_for( 5, i );
    r.flags = (uint64_t) ( i * 29 );
    r.object_id = (uint32_t) ( i * 31 + seed );
    r.object_sequence = (uint8_t) ( i % 247 );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_static_prop( RenderStaticProp & r, int i, int seed )
{
    r.position = vec3_for( 6, i );
    r.rotation = quat_for( 6, i );
    r.scale = 0.5 + 0.25 * ( i % 7 );
    r.flags = (uint64_t) ( i * 37 );
    r.static_prop_id = (uint32_t) ( i * 41 + seed );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_cosmetic_prop( RenderCosmeticProp & r, int i, int seed )
{
    r.position = vec3_for( 7, i );
    r.rotation = quat_for( 7, i );
    r.scale = 0.25 + 0.125 * ( i % 5 );
    r.flags = (uint64_t) ( i * 43 );
    r.cosmetic_prop_id = (uint32_t) ( i * 47 + seed );
    r.prop_sequence = (uint8_t) ( i % 241 );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_laser( RenderLaser & r, int i, int seed )
{
    r.start = vec3_for( 8, i );
    r.finish = vec3_for( 9, i );
    r.t = 0.125 * ( i % 8 );
    r.laser_id = (uint32_t) ( i * 53 + seed );
    r.laser_type = (LaserType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

static void fill_explosion( RenderExplosion & r, int i, int seed )
{
    r.position = vec3_for( 10, i );
    r.rotation = quat_for( 10, i );
    r.t = 0.0625 * ( i % 16 );
    r.explosion_id = (uint32_t) ( i * 59 + seed );
    r.parent_object_id = (uint32_t) ( i * 61 );
    r.explosion_type = (ExplosionType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

// the frame, from §19.1's worked table
static const int kCameras = 1;
static const int kShips = 300;
static const int kTurrets = 900;
static const int kMissiles = 120;
static const int kDynamicProps = 40;
static const int kStaticProps = 5000;
static const int kCosmeticProps = 800;
static const int kLasers = 200;
static const int kExplosions = 60;

// The SEED varies per frame, so no frame's stores are redundant with the last
// one's. Without it a compiler proves the whole loop dead and BOTH arms
// measure nothing — the first sitting of this harness reported an 18x
// difference that was entirely one arm being eliminated and the other not.
template <typename Row>
static void fill_rows( Row * rows, int count, int seed, void ( *fn )( Row &, int, int ) )
{
    for ( int i = 0; i < count; i++ ) { fn( rows[i], i, seed ); }
}

// ---- arm one: the hand-written scatter ----

// A cheap digest over one row of each section, so the SINK depends on the row
// STORES and not only on the extent. The 18x artifact this harness found once
// was exactly a dead-store elimination, and a sink that takes the extent alone
// cannot see one: an arm whose rows were all removed would still return the
// same number. Reading four rows costs nothing against 7,421 written.
static uint64_t row_digest( const uint8_t * base, uint64_t offset, size_t stride, int count )
{
    if ( count <= 0 ) { return 0; }
    uint64_t h = 0;
    const int probes[4] = { 0, count / 3, count / 2, count - 1 };
    for ( int p = 0; p < 4; p++ )
    {
        uint64_t word = 0;
        memcpy( &word, base + offset + (size_t) probes[p] * stride, 8 );
        h = h * 1099511628211ull + word;
    }
    return h;
}

static uint64_t hand_frame( uint8_t * base, int seed, uint64_t * extent = NULL )
{
    const uint64_t camera_offset = hand_align( sizeof( HandHeader ) );
    const uint64_t ship_offset = hand_align( camera_offset + kCameras * sizeof( RenderCamera ) );
    const uint64_t turret_offset = hand_align( ship_offset + kShips * sizeof( RenderShip ) );
    const uint64_t missile_offset = hand_align( turret_offset + kTurrets * sizeof( RenderTurret ) );
    const uint64_t dynamic_offset = hand_align( missile_offset + kMissiles * sizeof( RenderMissile ) );
    const uint64_t static_offset = hand_align( dynamic_offset + kDynamicProps * sizeof( RenderDynamicProp ) );
    const uint64_t cosmetic_offset = hand_align( static_offset + kStaticProps * sizeof( RenderStaticProp ) );
    const uint64_t laser_offset = hand_align( cosmetic_offset + kCosmeticProps * sizeof( RenderCosmeticProp ) );
    const uint64_t explosion_offset = hand_align( laser_offset + kLasers * sizeof( RenderLaser ) );
    const uint64_t end_offset = explosion_offset + kExplosions * sizeof( RenderExplosion );

    fill_rows( (RenderCamera *) ( base + camera_offset ), kCameras, seed, fill_camera );
    fill_rows( (RenderShip *) ( base + ship_offset ), kShips, seed, fill_ship );
    fill_rows( (RenderTurret *) ( base + turret_offset ), kTurrets, seed, fill_turret );
    fill_rows( (RenderMissile *) ( base + missile_offset ), kMissiles, seed, fill_missile );
    fill_rows( (RenderDynamicProp *) ( base + dynamic_offset ), kDynamicProps, seed, fill_dynamic_prop );
    fill_rows( (RenderStaticProp *) ( base + static_offset ), kStaticProps, seed, fill_static_prop );
    fill_rows( (RenderCosmeticProp *) ( base + cosmetic_offset ), kCosmeticProps, seed, fill_cosmetic_prop );
    fill_rows( (RenderLaser *) ( base + laser_offset ), kLasers, seed, fill_laser );
    fill_rows( (RenderExplosion *) ( base + explosion_offset ), kExplosions, seed, fill_explosion );

    HandHeader * header = (HandHeader *) base;
    header->version = (uint64_t) seed;
    header->cameras = hand_section( camera_offset, kCameras, sizeof( RenderCamera ) );
    header->ships = hand_section( ship_offset, kShips, sizeof( RenderShip ) );
    header->turrets = hand_section( turret_offset, kTurrets, sizeof( RenderTurret ) );
    header->missiles = hand_section( missile_offset, kMissiles, sizeof( RenderMissile ) );
    header->dynamic_props = hand_section( dynamic_offset, kDynamicProps, sizeof( RenderDynamicProp ) );
    header->static_props = hand_section( static_offset, kStaticProps, sizeof( RenderStaticProp ) );
    header->cosmetic_props = hand_section( cosmetic_offset, kCosmeticProps, sizeof( RenderCosmeticProp ) );
    header->lasers = hand_section( laser_offset, kLasers, sizeof( RenderLaser ) );
    header->explosions = hand_section( explosion_offset, kExplosions, sizeof( RenderExplosion ) );
    // the extent AND the rows, so a dead-store elimination cannot hide. The
    // EXTENT itself goes out by reference, because the golden gate opens with
    // it and a digest mixed into the return value is not a length.
    if ( extent ) { *extent = hand_align( end_offset ); }
    return hand_align( end_offset )
         + row_digest( base, ship_offset, sizeof( RenderShip ), kShips )
         + row_digest( base, static_offset, sizeof( RenderStaticProp ), kStaticProps );
}

// ---- arm two: the generated block ----

static uint64_t generated_frame( RenderFrameBlockStorage & storage, int seed, uint64_t * extent = NULL )
{
    RenderFrameCounts counts = {};
    counts.cameras = kCameras;
    counts.ships = kShips;
    counts.turrets = kTurrets;
    counts.missiles = kMissiles;
    counts.dynamic_props = kDynamicProps;
    counts.static_props = kStaticProps;
    counts.cosmetic_props = kCosmeticProps;
    counts.lasers = kLasers;
    counts.explosions = kExplosions;

    RenderFrameBlock block;
    if ( !RenderFrameBlockBegin( block, storage, counts ) ) { return 0; }
    block.projection->version = (uint64_t) seed;

    fill_rows( (RenderCamera *) RenderFrameCameras( block ), kCameras, seed, fill_camera );
    fill_rows( (RenderShip *) RenderFrameShips( block ), kShips, seed, fill_ship );
    fill_rows( (RenderTurret *) RenderFrameTurrets( block ), kTurrets, seed, fill_turret );
    fill_rows( (RenderMissile *) RenderFrameMissiles( block ), kMissiles, seed, fill_missile );
    fill_rows( (RenderDynamicProp *) RenderFrameDynamicProps( block ), kDynamicProps, seed, fill_dynamic_prop );
    fill_rows( (RenderStaticProp *) RenderFrameStaticProps( block ), kStaticProps, seed, fill_static_prop );
    fill_rows( (RenderCosmeticProp *) RenderFrameCosmeticProps( block ), kCosmeticProps, seed, fill_cosmetic_prop );
    fill_rows( (RenderLaser *) RenderFrameLasers( block ), kLasers, seed, fill_laser );
    fill_rows( (RenderExplosion *) RenderFrameExplosions( block ), kExplosions, seed, fill_explosion );

    if ( extent ) { *extent = (uint64_t) RenderFrameBlockBytes( block ); }
    return (uint64_t) RenderFrameBlockBytes( block )
         + row_digest( block.base, block.projection->ships.offset_of, sizeof( RenderShip ), kShips )
         + row_digest( block.base, block.projection->static_props.offset_of, sizeof( RenderStaticProp ), kStaticProps );
}

static double median( std::vector<double> & samples )
{
    std::vector<double> sorted = samples;
    for ( size_t i = 1; i < sorted.size(); i++ )
    {
        double v = sorted[i];
        size_t j = i;
        while ( j > 0 && sorted[j - 1] > v ) { sorted[j] = sorted[j - 1]; j--; }
        sorted[j] = v;
    }
    return sorted[sorted.size() / 2];
}

int main( int argc, char ** argv )
{
    // --smoke runs the CORRECTNESS half whole and does not enforce the timing
    // band. The band is a paired same-sitting measurement and a shared CI
    // runner has no quiet window, so a nightly leg that enforced it would be
    // reporting the runner's mood; what a nightly leg CAN prove is that this
    // gate still builds, still agrees byte for byte with the hand-written
    // mirror across every section, and still writes the frame the C# half
    // reads. The numbers print either way, unjudged in smoke.
    bool smoke = false;
    for ( int i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--smoke" ) == 0 ) { smoke = true; }
    }

    RenderFrameBlockStorage generated_storage;
    RenderFrameBlockStorage hand_storage;
    if ( !generated_storage.Create( TableBlockDefaultAllocator() ) ||
         !hand_storage.Create( TableBlockDefaultAllocator() ) )
    {
        printf( "FAILED: the storage did not allocate\n" );
        return 1;
    }
    memset( generated_storage.base, 0, (size_t) RenderFrameBlockMaxBytes );
    memset( hand_storage.base, 0, (size_t) RenderFrameBlockMaxBytes );

    // THE GOLDEN GATE, FIRST: the two arms must agree on every byte of every
    // row before any clock starts. A runner that does not agree REFUSES to
    // bench — timing something that disagrees measures nothing.
    uint64_t hand_extent = 0, generated_extent = 0;
    const uint64_t hand_bytes = hand_frame( hand_storage.base, 0, &hand_extent );
    const uint64_t generated_bytes = generated_frame( generated_storage, 0, &generated_extent );
    if ( hand_extent != generated_extent || hand_bytes != generated_bytes )
    {
        printf( "REFUSING TO BENCH: the two arms disagree on the extent or the rows (%llu hand, %llu generated)\n",
                (unsigned long long) hand_bytes, (unsigned long long) generated_bytes );
        return 1;
    }
    // The headers differ by the block's generated PROLOGUE — 24 bytes the hand
    // form does not have and does not need — so the ROWS are what is compared,
    // section by section, which is the comparison that means anything.
    {
        const HandHeader * header = (const HandHeader *) hand_storage.base;
        const HandSection * sections = &header->cameras;
        RenderFrameBlock block;
        if ( !RenderFrameBlockOpen( block, generated_storage.base, (int64_t) generated_extent ) )
        {
            printf( "REFUSING TO BENCH: the generated block does not open\n" );
            return 1;
        }
        const TableBlockTriple * triples = &block.projection->cameras;
        for ( int i = 0; i < 9; i++ )
        {
            if ( sections[i].offset != triples[i].offset_of || sections[i].count != triples[i].count ||
                 sections[i].stride != triples[i].stride )
            {
                printf( "REFUSING TO BENCH: section %d disagrees (hand %llu/%u/%u, generated %llu/%u/%u)\n", i,
                        (unsigned long long) sections[i].offset, sections[i].count, sections[i].stride,
                        (unsigned long long) triples[i].offset_of, triples[i].count, triples[i].stride );
                return 1;
            }
            const size_t extent = (size_t) sections[i].count * sections[i].stride;
            if ( memcmp( hand_storage.base + sections[i].offset, generated_storage.base + triples[i].offset_of, extent ) != 0 )
            {
                printf( "REFUSING TO BENCH: section %d's rows differ\n", i );
                return 1;
            }
        }
    }

    // The C# half of this gate reads THE SAME representative frame, so the
    // producer writes it out here rather than a half-megabyte golden going
    // into the tree: the pinned small block already proves the bytes, and this
    // one exists to be read fast.
    {
        FILE * f = fopen( "build/block_gate2.bin", "wb" );
        if ( f == NULL )
        {
            printf( "REFUSING TO BENCH: cannot write build/block_gate2.bin\n" );
            return 1;
        }
        fwrite( generated_storage.base, 1, (size_t) generated_extent, f );
        fclose( f );
    }

    // Arms INTERLEAVED in one sitting, medians paired.
    const int warmup = smoke ? 2 : 50;
    const int samples = smoke ? 3 : 15;
    const int frames = smoke ? 2 : 20; // per sample, so one measurement is many frames

    volatile uint64_t sink = 0;
    for ( int i = 0; i < warmup; i++ )
    {
        sink += hand_frame( hand_storage.base, i );
        sink += generated_frame( generated_storage, i );
    }

    std::vector<double> hand_us, generated_us;
    for ( int s = 0; s < samples; s++ )
    {
        {
            const auto start = std::chrono::steady_clock::now();
            for ( int f = 0; f < frames; f++ ) { sink += hand_frame( hand_storage.base, s * frames + f ); }
            const auto end = std::chrono::steady_clock::now();
            hand_us.push_back( std::chrono::duration<double, std::micro>( end - start ).count() / frames );
        }
        {
            const auto start = std::chrono::steady_clock::now();
            for ( int f = 0; f < frames; f++ ) { sink += generated_frame( generated_storage, s * frames + f ); }
            const auto end = std::chrono::steady_clock::now();
            generated_us.push_back( std::chrono::duration<double, std::micro>( end - start ).count() / frames );
        }
    }

    const double hand = median( hand_us );
    const double generated = median( generated_us );
    const double ratio = generated / hand;

    printf( "gate 2, the per-frame C++ WRITE (SPEC-TABLES.md §12.1)\n" );
    printf( "  hand-written scatter : %8.2f us/frame (median of %d)\n", hand, samples );
    printf( "  generated block form : %8.2f us/frame (median of %d)\n", generated, samples );
    printf( "  ratio (generated/hand): %.3f\n", ratio );

    // THE BAR: the same speed, or not significantly slower. The band is the
    // one the estate's own bench rules already use for a paired same-sitting
    // comparison — a batch-shaped row swings between byte-identical binaries,
    // so anything inside it is noise and anything outside it is a defect to
    // explain or close, never a trade to license.
    const double band = 1.05;
    if ( smoke )
    {
        printf( "GATE 2 (C++ write) SMOKE: correctness held, the band NOT enforced — a shared runner has no quiet window\n" );
        generated_storage.Destroy();
        hand_storage.Destroy();
        return 0;
    }
    if ( ratio > band )
    {
        printf( "GATE 2 FAILED: the generated form is %.1f%% slower than the hand-written scatter, past the %.0f%% band\n",
                ( ratio - 1.0 ) * 100.0, ( band - 1.0 ) * 100.0 );
        generated_storage.Destroy();
        hand_storage.Destroy();
        return 1;
    }
    printf( "GATE 2 (C++ write): the generated form is the same speed, or not significantly slower\n" );

    generated_storage.Destroy();
    hand_storage.Destroy();
    printf( "OK (%llu)\n", (unsigned long long) ( sink & 1 ) );
    return 0;
}
