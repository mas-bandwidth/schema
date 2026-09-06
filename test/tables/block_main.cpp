/*
    The BLOCK FORM's C++ leg (docs/SPEC-TABLES.md §19, §19.5).

    This is the PRODUCER half of the two-language gate. It:

      * lays a block out from the counts and checks every start and the used
        extent against §19.1's worked table, to the digit;
      * fills every array from N workers over DISJOINT index ranges and proves
        the result byte-identical to a serial fill of the same data;
      * proves the fill path allocated nothing — the runtime half of §19.1's
        conformance refuser, beside the Makefile's source-level gate;
      * proves the storage was allocated exactly ONCE, at build time, through
        the CALLER'S allocator;
      * checks every refusal Begin and BlockOpen make;
      * reads the block back through the block DESCRIPTORS as well as through
        the generated accessors, because §19.2 offers both and both must land
        the same values;
      * and pins the block's bytes as a golden the C# consumer opens.

    Prints OK and exits 0 — no test framework, exit code is the verdict.
*/

#include "RenderBlock.h"
#include "PaddedBlock.h"

#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <thread>
#include <vector>

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

// ---- the allocation counter: the fill path's refuser, at runtime ----
//
// §19.1 states the multi-threaded fill as an OBLIGATION on the implementation:
// the generated fill path — Begin, the array accessors and the row storage
// they hand back — contains no allocation, no lock and no atomic. The
// Makefile's block-fill-refuser gate reads the generated SOURCE; this is the
// other half, and it watches the RUNNING program: every global operator new is
// counted, and the count may not move across Begin, the accessors and the fill.
//
// The block's STORAGE is a different question and is counted separately: it is
// allocated once, at build time, through the caller's own allocator.

static std::atomic<long> global_allocations( 0 );
static std::atomic<long> allocator_calls( 0 );

void * operator new( size_t bytes )
{
    global_allocations.fetch_add( 1, std::memory_order_relaxed );
    void * p = malloc( bytes );
    if ( p == NULL ) { abort(); }
    return p;
}

void * operator new[]( size_t bytes ) { return operator new( bytes ); }
void operator delete( void * p ) noexcept { free( p ); }
void operator delete[]( void * p ) noexcept { free( p ); }
void operator delete( void * p, size_t ) noexcept { free( p ); }
void operator delete[]( void * p, size_t ) noexcept { free( p ); }

static void * counting_alloc( void * context, int64_t bytes )
{
    (void) context;
    allocator_calls.fetch_add( 1, std::memory_order_relaxed );
    return malloc( (size_t) bytes );
}

static void counting_free( void * context, void * pointer )
{
    (void) context;
    free( pointer );
}

static TableBlockAllocator counting_allocator()
{
    TableBlockAllocator allocator;
    allocator.alloc = counting_alloc;
    allocator.free = counting_free;
    allocator.context = NULL;
    return allocator;
}

// ---- the values, generated from the row index alone ----
//
// Every field of every row is a function of its array and its index, so the C#
// consumer reproduces the same values from the same two numbers and the
// comparison is over VALUES rather than over a blob both sides copied. The
// arithmetic is exact in binary floating point on purpose: a mismatch is a
// layout defect, never a rounding one.

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

static void fill_camera( RenderCamera & r, int i )
{
    r.position = vec3_for( 1, i );
    r.rotation = quat_for( 1, i );
    r.camera_id = (uint32_t) ( i * 7 + 1 );
    r.camera_type = (uint32_t) ( i % 4 );
    r.target_object_id = (uint32_t) ( i * 13 + 2 );
    r.fov = 0.5f * i + 60.0f;
}

static void fill_ship( RenderShip & r, int i )
{
    r.position = vec3_for( 2, i );
    r.rotation = quat_for( 2, i );
    r.flags = (uint64_t) ( i % 16 );
    r.object_id = (uint32_t) ( i * 3 + 11 );
    r.target_object_id = (uint32_t) ( i * 5 + 7 );
    r.thrust = 0.25f * i;
    r.object_sequence = (uint8_t) ( i % 251 );
    r.ship_type = (ShipType) ( i % 4 );
    r.team = (Team) ( i % 5 );
    r.has_target_lock = ( i % 2 ) == 0;
    r.predicted_explode = ( i % 3 ) == 0;
}

static void fill_turret( RenderTurret & r, int i )
{
    r.rotation = quat_for( 3, i );
    r.flags = (uint64_t) ( i * 17 );
    r.object_id = (uint32_t) ( i * 2 + 1 );
    r.parent_object_id = (uint32_t) ( i / 3 );
    r.turret_index = (uint32_t) ( i % 8 );
    r.target_object_id = (uint32_t) ( i * 11 );
    r.object_sequence = (uint8_t) ( i % 253 );
    r.team = (Team) ( i % 5 );
    r.has_target_lock = ( i % 5 ) == 0;
}

static void fill_missile( RenderMissile & r, int i )
{
    r.position = vec3_for( 4, i );
    r.rotation = quat_for( 4, i );
    r.flags = (uint64_t) ( i * 19 );
    r.object_id = (uint32_t) ( i * 23 );
    r.object_sequence = (uint8_t) ( i % 249 );
    r.missile_type = (MissileType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

static void fill_dynamic_prop( RenderDynamicProp & r, int i )
{
    r.position = vec3_for( 5, i );
    r.rotation = quat_for( 5, i );
    r.flags = (uint64_t) ( i * 29 );
    r.object_id = (uint32_t) ( i * 31 );
    r.object_sequence = (uint8_t) ( i % 247 );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_static_prop( RenderStaticProp & r, int i )
{
    r.position = vec3_for( 6, i );
    r.rotation = quat_for( 6, i );
    r.scale = 0.5 + 0.25 * ( i % 7 );
    r.flags = (uint64_t) ( i * 37 );
    r.static_prop_id = (uint32_t) ( i * 41 );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_cosmetic_prop( RenderCosmeticProp & r, int i )
{
    r.position = vec3_for( 7, i );
    r.rotation = quat_for( 7, i );
    r.scale = 0.25 + 0.125 * ( i % 5 );
    r.flags = (uint64_t) ( i * 43 );
    r.cosmetic_prop_id = (uint32_t) ( i * 47 );
    r.prop_sequence = (uint8_t) ( i % 241 );
    r.prop_type = (PropType) ( i % 4 );
    r.team = (Team) ( i % 5 );
}

static void fill_laser( RenderLaser & r, int i )
{
    r.start = vec3_for( 8, i );
    r.finish = vec3_for( 9, i );
    r.t = 0.125 * ( i % 8 );
    r.laser_id = (uint32_t) ( i * 53 );
    r.laser_type = (LaserType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

static void fill_explosion( RenderExplosion & r, int i )
{
    r.position = vec3_for( 10, i );
    r.rotation = quat_for( 10, i );
    r.t = 0.0625 * ( i % 16 );
    r.explosion_id = (uint32_t) ( i * 59 );
    r.parent_object_id = (uint32_t) ( i * 61 );
    r.explosion_type = (ExplosionType) ( i % 3 );
    r.team = (Team) ( i % 5 );
}

// §19.1's own frame, chosen by the page to be legible rather than measured
static RenderFrameCounts worked_counts()
{
    RenderFrameCounts counts = {};
    counts.cameras = 1;
    counts.ships = 300;
    counts.turrets = 900;
    counts.missiles = 120;
    counts.dynamic_props = 40;
    counts.static_props = 5000;
    counts.cosmetic_props = 800;
    counts.lasers = 200;
    counts.explosions = 60;
    return counts;
}

// A small frame for the pinned golden: the two-language gate is about BYTES
// and OFFSETS, not about volume, and a half-megabyte golden in the tree buys
// nothing a two-kilobyte one does not.
static RenderFrameCounts golden_counts()
{
    RenderFrameCounts counts = {};
    counts.cameras = 1;
    counts.ships = 5;
    counts.turrets = 7;
    counts.missiles = 3;
    counts.dynamic_props = 2;
    counts.static_props = 4;
    counts.cosmetic_props = 3;
    counts.lasers = 6;
    counts.explosions = 2;
    return counts;
}

// One worker's share of one array: a contiguous run of indices, owned outright
// (§19.1's contract is ownership, exactly as §6.4's is).
template <typename Row>
static void fill_share( Row * rows, int count, int worker, int workers, void ( *fn )( Row &, int ) )
{
    const int per = ( count + workers - 1 ) / workers;
    int begin = worker * per;
    int end = begin + per;
    if ( begin > count ) { begin = count; }
    if ( end > count ) { end = count; }
    for ( int i = begin; i < end; i++ ) { fn( rows[i], i ); }
}

// The whole fill, as one worker of `workers` sees it. Serial is this with one
// worker, which is what makes the byte-identity claim a claim about the same
// code rather than about two hand-written twins.
static void fill_block( RenderFrameBlock & block, const RenderFrameCounts & counts, int worker, int workers )
{
    fill_share<RenderCamera>( RenderFrameCameras( block ), counts.cameras, worker, workers, fill_camera );
    fill_share<RenderShip>( RenderFrameShips( block ), counts.ships, worker, workers, fill_ship );
    fill_share<RenderTurret>( RenderFrameTurrets( block ), counts.turrets, worker, workers, fill_turret );
    fill_share<RenderMissile>( RenderFrameMissiles( block ), counts.missiles, worker, workers, fill_missile );
    fill_share<RenderDynamicProp>( RenderFrameDynamicProps( block ), counts.dynamic_props, worker, workers, fill_dynamic_prop );
    fill_share<RenderStaticProp>( RenderFrameStaticProps( block ), counts.static_props, worker, workers, fill_static_prop );
    fill_share<RenderCosmeticProp>( RenderFrameCosmeticProps( block ), counts.cosmetic_props, worker, workers, fill_cosmetic_prop );
    fill_share<RenderLaser>( RenderFrameLasers( block ), counts.lasers, worker, workers, fill_laser );
    fill_share<RenderExplosion>( RenderFrameExplosions( block ), counts.explosions, worker, workers, fill_explosion );
}

// ---- the pinned golden (the two-language gate's other half) ----

static void pin_block_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    if ( std::getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( f == NULL )
        {
            printf( "FAILED: cannot write %s\n", path );
            failures++;
            return;
        }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        return;
    }
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAILED: missing block golden %s (run: make update-goldens)\n", path );
        failures++;
        return;
    }
    static uint8_t expected[1u << 20];
    size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( expected, data, (size_t) n ) != 0 )
    {
        printf( "FAILED: block golden %s: %lld bytes written, %lld pinned\n",
                name, (long long) bytes, (long long) n );
        failures++;
    }
}

// ---- the reflective read (§19.2), in C++ ----
//
// The descriptors carry the projection offset of every field and the offsets
// of the three members inside each triple. A consumer holding them reads the
// facts out of an instance and points at rows with no knowledge of the
// spelling that produced any of it — which is the mechanism that retires a
// hand-kept mirror. This walk is the C++ half of the "runs twice" rule §19.5
// states for the C# side.
static void reflective_walk( const RenderFrameBlock & block, const RenderFrameCounts & counts )
{
    const TableBlockInfo * info = RenderFrameBlock::Type();
    check( info != NULL && strcmp( info->name, "RenderFrame" ) == 0, "the block descriptor names its table" );
    check( info->build_version == BuildVersion, "the block descriptor carries the unit's build version" );
    check( info->size == sizeof( RenderFrameBlock::Projection ), "the block descriptor size is the projection's own sizeof" );

    const int32_t expected_counts[9] = {
        counts.cameras, counts.ships, counts.turrets, counts.missiles, counts.dynamic_props,
        counts.static_props, counts.cosmetic_props, counts.lasers, counts.explosions,
    };
    int out_of_line = 0;
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const TableBlockFieldInfo & f = info->fields[i];
        if ( !f.out_of_line )
        {
            continue;
        }
        // read the triple out of the INSTANCE, at the offsets the descriptor
        // gives, and point at the rows — no generated struct in sight
        const uint8_t * base = (const uint8_t *) block.projection;
        uint64_t offset_of = 0;
        uint32_t count = 0, stride = 0;
        memcpy( &offset_of, base + f.offset_of_offset, 8 );
        memcpy( &count, base + f.count_offset, 4 );
        memcpy( &stride, base + f.stride_offset, 4 );
        check( (int32_t) count == expected_counts[out_of_line], "the descriptor's count is the count the producer wrote" );
        check( stride == f.stride, "the instance's pitch is this build's own, in a block this build laid out" );
        check( f.element != NULL && f.element() != NULL, "an out-of-line array's descriptor names its element's descriptor" );
        check( offset_of >= info->size, "an out-of-line array starts past the projection" );

        // and DESCEND: the element column carries the row's own layout, so a
        // walker reads a row's fields with no generated struct in hand. This is
        // the whole mechanism §19.2 rests on — the mirror died because the
        // layout became data.
        if ( strcmp( f.name, "ships" ) == 0 )
        {
            const TableBlockInfo * row = f.element();
            check( strcmp( row->name, "RenderShip" ) == 0, "the ships array's element descriptor names RenderShip" );
            check( row->size == stride, "the row descriptor's size is the pitch the instance carries" );
            const TableBlockFieldInfo * object_id = NULL;
            const TableBlockFieldInfo * position = NULL;
            for ( int j = 0; j < row->num_fields; j++ )
            {
                if ( strcmp( row->fields[j].name, "object_id" ) == 0 ) { object_id = &row->fields[j]; }
                if ( strcmp( row->fields[j].name, "position" ) == 0 ) { position = &row->fields[j]; }
            }
            check( object_id != NULL && position != NULL, "the row descriptor names the row's own fields" );
            if ( object_id != NULL && position != NULL && count > 0 )
            {
                const uint8_t * rows = (const uint8_t *) block.base + offset_of;
                int mismatches = 0;
                for ( uint32_t r = 0; r < count; r++ )
                {
                    uint32_t reflected = 0;
                    memcpy( &reflected, rows + (size_t) r * stride + object_id->offset, 4 );
                    if ( reflected != RenderFrameShips( block )[r].object_id ) { mismatches++; }
                }
                check( mismatches == 0, "the reflective read lands the same values as the generated accessor (§19.2's two ways)" );

                // one level further down: a nested record's own layout
                const TableBlockInfo * vec = position->element();
                check( vec != NULL && strcmp( vec->name, "RenderVector3" ) == 0, "a nested record's descriptor is reached through the same column" );
                double x = 0.0;
                memcpy( &x, rows + position->offset + vec->fields[0].offset, 8 );
                check( x == RenderFrameShips( block )[0].position.x, "and its fields read at their own offsets" );
            }
        }
        out_of_line++;
    }
    check( out_of_line == 9, "the block descriptor carries all nine out-of-line arrays" );
}

int main( int argc, char ** argv )
{
    // --race is the NEGATIVE CONTROL for the ThreadSanitizer leg, and nothing
    // else uses it: every worker fills the WHOLE of every array instead of its
    // own disjoint share, so the workers write the same rows and TSan must say
    // so. Without it a green TSan run proves only that the leg ran.
    bool race = false;
    for ( int i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--race" ) == 0 ) { race = true; }
    }

    // ---- gate: the layout, from the counts, to the digit (§19.1) ----

    const RenderFrameCounts counts = worked_counts();

    RenderFrameBlockStorage storage;
    check( storage.Create( counting_allocator() ), "the block storage allocates, through the caller's allocator" );
    check( allocator_calls.load() == 1, "the block form allocates exactly ONCE, at build time, through the caller's pair" );
    check( ( (uintptr_t) storage.base % 64 ) == 0, "the block's base is 64-byte aligned" );

    // A caller that needs a byte-stable artifact zeroes the storage once
    // (§19.1) — the form does not, because zeroing megabytes per frame is
    // exactly the cost it exists to avoid. This leg pins bytes, so it zeroes.
    memset( storage.base, 0, (size_t) RenderFrameBlockMaxBytes );

    // The fill path is measured SERIALLY, on purpose: the parallelism lives in
    // the caller's loop and std::thread allocates on its own account, so a
    // counter wrapped around the threads would measure the test harness. What
    // §19.1 obliges is that Begin, the accessors and the rows they hand back
    // allocate nothing — and that is exactly what runs between these two reads.
    const long allocations_before = global_allocations.load();

    RenderFrameBlock block;
    check( RenderFrameBlockBegin( block, storage, counts ), "Begin lays the block out from the counts" );

    check( sizeof( RenderFrameBlock::Projection ) == 176,
           "the projection is 176 bytes: the 24-byte prologue, a uint64 and nine triples" );

    const uint64_t want_starts[9] = { 192, 320, 26752, 84352, 92992, 95872, 495872, 559872, 572672 };
    const uint64_t got_starts[9] = {
        block.projection->cameras.offset_of, block.projection->ships.offset_of,
        block.projection->turrets.offset_of, block.projection->missiles.offset_of,
        block.projection->dynamic_props.offset_of, block.projection->static_props.offset_of,
        block.projection->cosmetic_props.offset_of, block.projection->lasers.offset_of,
        block.projection->explosions.offset_of,
    };
    for ( int i = 0; i < 9; i++ )
    {
        if ( got_starts[i] != want_starts[i] )
        {
            printf( "FAILED: array %d starts at %llu, want %llu (docs/SPEC-TABLES.md §19.1's worked table)\n",
                    i, (unsigned long long) got_starts[i], (unsigned long long) want_starts[i] );
            failures++;
        }
    }
    check( RenderFrameBlockBytes( block ) == 577472, "the used extent is 577,472 (docs/SPEC-TABLES.md §19.1)" );
    check( RenderFrameBlockMaxBytes == 7879488, "the storage is 7,879,488 bytes: every array at its declared maximum" );

    block.projection->version = RenderVersion;
    fill_block( block, counts, 0, 1 ); // the SERIAL fill: one worker of one

    check( global_allocations.load() == allocations_before,
           "the fill path allocated nothing: Begin, the accessors and the rows they hand back are allocation-free (§19.1)" );

    reflective_walk( block, counts );

    // ---- gate: a REAL multi-threaded fill, byte-identical to the serial one ----
    //
    // N workers fill DISJOINT index ranges of every array with no lock, no
    // atomic and no per-row synchronisation, over their own storage, and the
    // two extents are compared byte for byte. Under the sanitizer leg a race
    // in the fill is what the leg exists to find.
    {
        RenderFrameBlockStorage wide_storage;
        check( wide_storage.Create( counting_allocator() ), "the wide fill's storage allocates" );
        memset( wide_storage.base, 0, (size_t) RenderFrameBlockMaxBytes );
        RenderFrameBlock wide;
        check( RenderFrameBlockBegin( wide, wide_storage, counts ), "Begin lays the wide fill's block out identically" );
        wide.projection->version = RenderVersion;

        const int num_workers = 4;
        std::atomic<int> started( 0 );
        std::vector<std::thread> workers;
        workers.reserve( num_workers );
        for ( int w = 0; w < num_workers; w++ )
        {
            workers.push_back( std::thread( [&wide, &counts, w, race, &started]() {
                if ( race )
                {
                    // THE CONTROL: every worker writes ONE row, after a start
                    // barrier, many times. Overlapping RANGES would be a race
                    // too, but whether a sanitizer observes one then depends on
                    // how the machine happened to schedule four threads over
                    // seven thousand rows — and a control that depends on that
                    // is not a control. Contending on one address, from threads
                    // that start together, is the same defect made certain.
                    started.fetch_add( 1, std::memory_order_relaxed );
                    while ( started.load( std::memory_order_relaxed ) < num_workers ) {}
                    RenderShip * ships = RenderFrameShips( wide );
                    for ( int i = 0; i < 20000; i++ ) { fill_ship( ships[0], w ); }
                    return;
                }
                // disjoint index ranges, by ownership
                fill_block( wide, counts, w, num_workers );
            } ) );
        }
        for ( size_t i = 0; i < workers.size(); i++ ) { workers[i].join(); }

        const int64_t bytes = RenderFrameBlockBytes( block );
        check( RenderFrameBlockBytes( wide ) == bytes, "the two blocks have the same used extent" );
        check( memcmp( storage.base, wide_storage.base, (size_t) bytes ) == 0,
               "the multi-threaded fill is BYTE-IDENTICAL to the serial fill (docs/SPEC-TABLES.md §19.5)" );
        wide_storage.Destroy();
    }

    // ---- gate: the accessors agree with the values, both ways round ----

    {
        int mismatches = 0;
        RenderShip * ships = RenderFrameShips( block );
        for ( int i = 0; i < counts.ships; i++ )
        {
            // FIELD BY FIELD, not memcmp: a generated table struct is an
            // aggregate with member initialisers, so a value-initialised twin
            // carries UNSPECIFIED padding and a whole-row compare would be
            // comparing bytes nobody defines. What the fill wrote is the
            // fields; what the block's padding holds is §19.1's business, and
            // the golden pins it separately.
            RenderShip want = {};
            fill_ship( want, i );
            const RenderShip & got = ships[i];
            const bool same = got.position.x == want.position.x && got.position.y == want.position.y &&
                              got.position.z == want.position.z && got.rotation.x == want.rotation.x &&
                              got.rotation.y == want.rotation.y && got.rotation.z == want.rotation.z &&
                              got.rotation.w == want.rotation.w && got.flags == want.flags &&
                              got.object_id == want.object_id && got.target_object_id == want.target_object_id &&
                              got.thrust == want.thrust && got.object_sequence == want.object_sequence &&
                              got.ship_type == want.ship_type && got.team == want.team &&
                              got.has_target_lock == want.has_target_lock &&
                              got.predicted_explode == want.predicted_explode;
            if ( !same ) { mismatches++; }
        }
        check( mismatches == 0, "every ship row holds what the fill wrote, at the pitch the instance gives" );

        // the ITERATING accessor steps at the instance's pitch; the CONTIGUOUS
        // one is available because the pitch IS sizeof (§2.7)
        int seen = 0;
        for ( const RenderShip & ship : RenderFrameShips( block ) ) { seen += (int) ( ship.object_id != 0 ); }
        check( seen == counts.ships, "the iterating accessor yields count rows" );
        check( RenderFrameShipsSpan( block ).size() == counts.ships, "the contiguous view carries the same count" );
    }

    // ---- gate: Begin's refusal NAMES the array, its count and its maximum ----

    {
        RenderFrameCounts over = counts;
        over.ships = 4097; // one past the declared maximum
        RenderFrameBlock refused;
        TableBlockRefusal refusal;
        check( !RenderFrameBlockBegin( refused, storage, over, &refusal ), "Begin refuses a count past its maximum" );
        check( refusal.array != NULL && strcmp( refusal.array, "ships" ) == 0, "the refusal names the array" );
        check( refusal.count == 4097 && refusal.maximum == 4096, "the refusal names the count and the maximum" );

        RenderFrameCounts negative = counts;
        negative.lasers = -1;
        check( !RenderFrameBlockBegin( refused, storage, negative, &refusal ), "Begin refuses a negative count" );
        check( refusal.array != NULL && strcmp( refusal.array, "lasers" ) == 0, "the refusal names the array again" );
    }

    // the refusal left the block untouched, so the good one still opens
    check( RenderFrameBlockBegin( block, storage, counts ), "Begin re-lays the good block" );
    block.projection->version = RenderVersion;
    fill_block( block, counts, 0, 1 );

    // ---- gate: THE FORGERY BATTERY (§19.2's WHOLE check) ----
    //
    // Eleven forgeries, each a single word of an otherwise valid block. A
    // reader who wrote this battery independently found that nine of ten
    // refused and the tenth — a count past the DECLARED MAXIMUM — opened, on
    // both backends: Begin refuses it on the producer side and Open did not
    // refuse it here, so a consumer sizing anything by the maximum would have
    // overflowed. It is the tenth row below now. The eleventh came from the
    // FORGERY FUZZER beside this battery (test/tables/block_fuzz_main.cpp) and
    // is the minimized case of the defect it found.
    //
    // The battery runs under the sanitized twin as well, where a forgery that
    // got past the check would read outside the block and the leg would say so.
    {
        const int64_t bytes = RenderFrameBlockBytes( block );
        RenderFrameBlock forged;
        int refused = 0;
        const int forgeries = 11;

        // 1. a foreign magic
        {
            const uint64_t saved = block.projection->magic;
            block.projection->magic = 0;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->magic = saved;
        }
        // 2. a block of the other byte order, identified by a byte-swapped magic
        {
            const uint64_t saved = block.projection->magic;
            block.projection->magic = table_block_byteswap64( saved );
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->magic = saved;
        }
        // 3. a build this one does not match
        {
            const uint64_t saved = block.projection->build_version;
            block.projection->build_version = saved ^ 1;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->build_version = saved;
        }
        // 4. the other byte order, recorded in the prologue's own word
        {
            const uint64_t saved = block.projection->byte_order;
            block.projection->byte_order = saved == 1 ? 2 : 1;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->byte_order = saved;
        }
        // 5. an array start that is not aligned for its element
        {
            const uint64_t saved = block.projection->ships.offset_of;
            block.projection->ships.offset_of = saved + 1;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->ships.offset_of = saved;
        }
        // 6. an array start inside the projection
        {
            const uint64_t saved = block.projection->ships.offset_of;
            block.projection->ships.offset_of = 0;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->ships.offset_of = saved;
        }
        // 7. an array that leaves the block
        {
            const uint64_t saved = block.projection->ships.offset_of;
            block.projection->ships.offset_of = (uint64_t) RenderFrameBlockMaxBytes;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->ships.offset_of = saved;
        }
        // 8. a pitch that is not this build's own
        {
            const uint32_t saved = block.projection->ships.stride;
            block.projection->ships.stride = saved + 8;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->ships.stride = saved;
        }
        // 9. a count whose rows leave the extent the caller passed. It is
        //    UNDER the declared maximum on purpose, so the extent check is
        //    what refuses it and not row 10's.
        {
            const uint32_t saved = block.projection->ships.count;
            block.projection->ships.count = 4000;
            refused += !RenderFrameBlockOpen( forged, storage.base, 200000 );
            block.projection->ships.count = saved;
        }
        // 10. a count past the DECLARED MAXIMUM, inside a roomy extent — the
        //     one the reader found open. `bytes` here is the whole storage, so
        //     nothing about the extent refuses it: only the maximum does.
        {
            const uint32_t saved = block.projection->ships.count;
            block.projection->ships.count = (uint32_t) ( RenderFrameBlock::ShipsMax + 904 );
            refused += !RenderFrameBlockOpen( forged, storage.base, RenderFrameBlockMaxBytes );
            block.projection->ships.count = saved;
        }

        // 11. an offset_of that is 64-ALIGNED and just under 2^63 — the one the
        //     FORGERY FUZZER found (test/tables/block_fuzz_main.cpp). It is
        //     positive as a signed 64-bit integer and aligned for its element,
        //     so it passed both start checks, and the extent arithmetic then
        //     added one row's pitch to it in int64_t: the addition the check
        //     after it was supposed to catch WAS the overflow, which is
        //     undefined behaviour rather than a refusal. Every term of that
        //     arithmetic is unsigned and bounded before it is added now.
        {
            const uint64_t saved = block.projection->cameras.offset_of;
            block.projection->cameras.offset_of = 0x7fffffffffffffc0ull;
            refused += !RenderFrameBlockOpen( forged, storage.base, bytes );
            block.projection->cameras.offset_of = saved;
        }

        if ( refused != forgeries )
        {
            printf( "FAILED: the forgery battery refused %d of %d (docs/SPEC-TABLES.md §19.2's WHOLE check)\n", refused, forgeries );
            failures++;
        }
        check( forged.base == NULL, "a refused forgery points at nothing, rather than at rows it cannot read" );
        check( RenderFrameBlockOpen( forged, storage.base, bytes ), "and the restored block opens again" );
    }

    // ---- gate: BlockOpen's WHOLE check (§19.2) ----

    {
        const int64_t bytes = RenderFrameBlockBytes( block );
        RenderFrameBlock opened;
        check( RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen accepts a block this build wrote" );
        check( opened.bytes == bytes, "BlockOpen reports the used extent" );
        check( opened.projection->version == RenderVersion, "the consumer reads the table's own declared field" );

        // A REFUSAL NAMES ITSELF beside the false (docs/SPEC-TABLES.md §7,
        // §19.2), and a MATCH writes nothing: the caller's own value stands,
        // which is what makes the successful open cost nothing. The four
        // clauses below are the ones no forgery of an IMAGE can reach, because
        // each is about the buffer the CALLER passed and not about the block.
        TableRefuseReason reason = ok;
        check( RenderFrameBlockOpen( opened, storage.base, bytes, &reason ) && reason == ok,
               "a match leaves the caller's own reason untouched" );

        reason = ok;
        check( !RenderFrameBlockOpen( opened, NULL, bytes, &reason ) && reason == unaligned_base,
               "BlockOpen refuses a null base, which is the CALLER's own buffer and not a block" );
        reason = ok;
        check( !RenderFrameBlockOpen( opened, storage.base, 8, &reason ) && reason == truncated,
               "BlockOpen refuses a length shorter than the projection: there is no prologue to read" );
        reason = ok;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes - 64, &reason ) && reason == bad_layout,
               "BlockOpen refuses a length whose arrays no longer fit: an extent clause, not a truncation" );
        // THE USED EXTENT'S OWN PADDING, and it is the `truncated` reading no
        // forgery of an IMAGE can reach (§19.2): the caller's bytes cover every
        // array and stop INSIDE the rounding the used extent takes to 64. The
        // arrays are read BEFORE the used extent, because the used extent is
        // derived from them, so this is `truncated` and not `bad_layout`. A
        // block of its own is laid out for it, with counts whose greatest end
        // is not a multiple of 64 — the corpus frame's is.
        {
            RenderFrameBlockStorage pad_storage;
            check( pad_storage.Create( counting_allocator() ), "the padding reading's storage allocates" );
            RenderFrameCounts pad_counts = {};
            pad_counts.cameras = 1;
            // the LAST array is what the used extent is taken from, and its
            // pitch is 80: one row leaves 48 bytes of padding to stop inside
            pad_counts.explosions = 1;
            RenderFrameBlock pad_block;
            check( RenderFrameBlockBegin( pad_block, pad_storage, pad_counts ), "Begin lays the padding reading's block out" );
            const int64_t pad_bytes = RenderFrameBlockBytes( pad_block );
            const TableBlockTriple * const pad_triples[] = {
                &pad_block.projection->cameras, &pad_block.projection->ships,
                &pad_block.projection->turrets, &pad_block.projection->missiles,
                &pad_block.projection->dynamic_props, &pad_block.projection->static_props,
                &pad_block.projection->cosmetic_props, &pad_block.projection->lasers,
                &pad_block.projection->explosions };
            int64_t pad_used = (int64_t) sizeof( RenderFrameBlock::Projection );
            for ( size_t t = 0; t < sizeof( pad_triples ) / sizeof( pad_triples[0] ); t++ )
            {
                const int64_t end = (int64_t) pad_triples[t]->offset_of
                                  + (int64_t) pad_triples[t]->count * (int64_t) pad_triples[t]->stride;
                if ( end > pad_used ) { pad_used = end; }
            }
            check( pad_bytes > pad_used, "the used extent is NOT 64-aligned, which is what this reading needs" );
            RenderFrameBlock pad_open;
            TableRefuseReason pad_reason = ok;
            check( !RenderFrameBlockOpen( pad_open, pad_storage.base, pad_bytes - 1, &pad_reason ),
                   "BlockOpen refuses bytes that stop inside the used extent's own padding" );
            check( pad_reason == truncated, "and it NAMES truncated, not bad_layout: every array fits the caller's bytes" );
            pad_reason = ok;
            check( RenderFrameBlockOpen( pad_open, pad_storage.base, pad_bytes, &pad_reason ) && pad_reason == ok,
                   "and one byte more opens: the padding is the whole of the difference" );
            pad_storage.Destroy();
        }

        // THE BASE'S ALIGNMENT IS THE LAST CLAUSE (§7, §19.2), so the image
        // has to be WHOLE and at an unaligned address for it to be reached:
        // shifting the pointer into the image would fail the magic first, and
        // that is the ordering this check is about.
        {
            uint8_t * raw = (uint8_t *) malloc( (size_t) bytes + 128 );
            uint8_t * lead = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 ) + 1;
            memcpy( lead, storage.base, (size_t) bytes );
            reason = ok;
            check( !RenderFrameBlockOpen( opened, lead, bytes, &reason ) && reason == unaligned_base,
                   "BlockOpen refuses an unaligned base, LAST, because it is the one clause that reads nothing out of the block" );
            free( raw );
        }

        // each prologue word in turn, restored after
        const uint64_t magic = block.projection->magic;
        block.projection->magic = 0;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses a foreign magic" );
        block.projection->magic = table_block_byteswap64( magic );
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses a block of the other byte order" );
        block.projection->magic = magic;

        const uint64_t version = block.projection->build_version;
        block.projection->build_version = version ^ 1;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ),
               "BlockOpen refuses a block from a build this one does not match — there is ONE entry point, and a mismatch is a refusal" );
        block.projection->build_version = version;

        const uint64_t order = block.projection->byte_order;
        block.projection->byte_order = order == 1 ? 2 : 1;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses the other byte order in the prologue" );
        block.projection->byte_order = order;

        const uint64_t start = block.projection->ships.offset_of;
        block.projection->ships.offset_of = start + 1;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses an array start that is not aligned for its element" );
        block.projection->ships.offset_of = (uint64_t) RenderFrameBlockMaxBytes;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses an array that leaves the block" );
        block.projection->ships.offset_of = start;

        const uint32_t stride = block.projection->ships.stride;
        block.projection->ships.stride = stride + 8;
        check( !RenderFrameBlockOpen( opened, storage.base, bytes ), "BlockOpen refuses a pitch that is not this build's own" );
        block.projection->ships.stride = stride;

        check( RenderFrameBlockOpen( opened, storage.base, bytes ), "and the restored block opens again" );
    }

    // ---- gate: the two-language golden ----

    {
        RenderFrameBlockStorage golden_storage;
        check( golden_storage.Create( counting_allocator() ), "the golden's storage allocates" );
        memset( golden_storage.base, 0, (size_t) RenderFrameBlockMaxBytes );
        RenderFrameBlock golden;
        const RenderFrameCounts small = golden_counts();
        check( RenderFrameBlockBegin( golden, golden_storage, small ), "Begin lays the golden frame out" );
        golden.projection->version = RenderVersion;
        fill_block( golden, small, 0, 1 );
        pin_block_golden( "block_render", golden_storage.base, RenderFrameBlockBytes( golden ) );
        printf( "block golden: %lld bytes, build version 0x%016llx\n",
                (long long) RenderFrameBlockBytes( golden ), (unsigned long long) BuildVersion );
        golden_storage.Destroy();
    }

    storage.Destroy();

    // ---- gate: PADDING and inline storage (§19.3, §19.5) ----
    //
    // Render.schema is declared largest-alignment-first and has zero interior
    // padding; Padded.schema declares the opposite on purpose, so the C# side's
    // GENERATED PADDING FIELDS are exercised rather than assumed — and so the
    // negative control that deletes them has something to go red on. It also
    // carries the inline storage classes the block form leaves exactly where
    // they are: a fixed [N]T, an enum-keyed [E]T, a string, bytes, and an
    // optional's presence companion.
    {
        PaddedFrameBlockStorage padded_storage;
        check( padded_storage.Create( counting_allocator() ), "the padded frame's storage allocates" );
        memset( padded_storage.base, 0, (size_t) PaddedFrameBlockMaxBytes );
        PaddedFrameCounts padded_counts = {};
        padded_counts.rows = 4;
        PaddedFrameBlock padded;
        check( PaddedFrameBlockBegin( padded, padded_storage, padded_counts ), "Begin lays the padded frame out" );
        padded.projection->marker = 7;
        padded.projection->stamp = 0x0123456789abcdefull;
        memcpy( padded.projection->blob, "twelve bytes", 12 );
        padded.projection->blob_length = 12;
        PaddedRow * rows = PaddedFrameRows( padded );
        for ( int i = 0; i < padded_counts.rows; i++ )
        {
            rows[i].tag = (uint8_t) ( 10 + i );
            rows[i].value = 0.5 * i + 100.0;
            rows[i].flag = ( i % 2 ) == 0;
            rows[i].id = (uint32_t) ( i * 1000 + 3 );
            snprintf( rows[i].label, sizeof( rows[i].label ), "row-%d", i );
            rows[i].label_length = (int32_t) strlen( rows[i].label );
            for ( int s = 0; s < 4; s++ ) { rows[i].slots[s] = (uint16_t) ( i * 4 + s ); }
            // by KEY, never by storage index: the accessor is the only place
            // the shift appears (docs/SPEC-TABLES.md §2.4)
            for ( int t = 1; t <= 4; t++ ) { rows[i].teams[ (Team) t ] = (uint8_t) ( i + t ); }
            rows[i].counter = i * 9;
            rows[i].counter_present = ( i % 2 ) == 1;
        }
        pin_block_golden( "block_padded", padded_storage.base, PaddedFrameBlockBytes( padded ) );
        printf( "padded golden: %lld bytes, row sizeof %zu, projection sizeof %zu\n",
                (long long) PaddedFrameBlockBytes( padded ), sizeof( PaddedRow ),
                sizeof( PaddedFrameBlock::Projection ) );
        padded_storage.Destroy();
    }

    printf( failures == 0 ? "OK\n" : "FAILED\n" );
    return failures == 0 ? 0 : 1;
}
