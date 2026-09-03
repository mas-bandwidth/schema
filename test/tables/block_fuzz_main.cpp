/*
    The BLOCK FORM's FORGERY FUZZER, C++ side (docs/SPEC-TABLES.md §19.2, §19.5).

    The hand-written battery in block_main.cpp is ten forgeries, one per fact
    BlockOpen checks. This is the standing gate beside it: valid blocks from the
    generated builder, mutated by the mutators below, and one oracle over every
    mutant.

      REFUSE, or OPEN and be WHOLE. A mutant either makes BlockOpen return
      false — reading no byte outside the extent the caller passed, which the
      sanitized twin is what proves — or it opens, and then every row of every
      array is addressable inside that extent, every pitch is this build's own,
      every count is inside its declared maximum, and a full walk that READS
      every byte of every row leaves the region untouched.

    The oracle re-derives its bounds from the DESCRIPTORS and from the triples
    in the instance, never from BlockOpen's own arithmetic, which is what makes
    the two negative controls (the Makefile's tables-block-fuzz-*-negative-
    control) able to go red at all.

    Every mutant is a pure function of ( seed, unit, vector, pass, index ), so a
    failure prints the one command that re-runs it alone.

    Prints OK and exits 0 — no test framework, exit code is the verdict.
*/

#include "RenderBlock.h"
#include "PaddedBlock.h"
#include "FrameBlock.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <cstdarg>

// ---------------------------------------------------------------------------
// the verdict
// ---------------------------------------------------------------------------

static int failures = 0;
static volatile uint64_t sink = 0; // the walk's reads, kept from being folded away

// where the walk reads TO. Sized to the largest storage any unit here declares,
// so one memcpy per array can carry the whole of it.
static uint8_t * walk_scratch = NULL;
static size_t walk_scratch_bytes = 0;

struct Site
{
    const char * unit;
    int vector;
    const char * pass;
    int64_t index;
    char description[512];
};

static Site site;
static uint64_t run_seed = 0;

static void describe( const char * format, ... )
{
    va_list args;
    va_start( args, format );
    vsnprintf( site.description, sizeof( site.description ), format, args );
    va_end( args );
}

// The mutation in hand, and the one command that re-runs it alone. A find is
// useless without this: a mutant is a pure function of its site, so the site is
// the whole reproduction.
static void report_site( const char * what )
{
    fprintf( stderr, "\nFAILED: %s\n", what );
    fprintf( stderr, "  unit      %s\n", site.unit );
    fprintf( stderr, "  vector    %d\n", site.vector );
    fprintf( stderr, "  pass      %s\n", site.pass );
    fprintf( stderr, "  index     %lld\n", (long long) site.index );
    fprintf( stderr, "  mutation  %s\n", site.description );
    fprintf( stderr, "  re-run    SEED=%llu ./build/schema_test_block_fuzz --only %s:%d:%s:%lld\n\n",
             (unsigned long long) run_seed, site.unit, site.vector, site.pass, (long long) site.index );
    fflush( stderr );
}

// A defect the ORACLE saw: report and STOP. A fuzzer that keeps going after the
// first find reports the same class N times and minimizes none of them.
static void defect( const char * what )
{
    report_site( what );
    failures++;
    exit( 1 );
}

// A defect the SANITIZER saw. Its report goes to stderr and the process dies
// inside BlockOpen, so nothing above ever runs — the death callback is what
// carries the mutation out with it.
#if defined( __has_feature )
    #if __has_feature( address_sanitizer )
        #define BLOCK_FUZZ_SANITIZED 1
    #endif
#endif
#if defined( __SANITIZE_ADDRESS__ )
    #define BLOCK_FUZZ_SANITIZED 1
#endif

#if defined( BLOCK_FUZZ_SANITIZED )
extern "C" void __sanitizer_set_death_callback( void ( *callback )() );
static void on_sanitizer_death()
{
    report_site( "a sanitizer stopped the run inside BlockOpen or the walk — its report is above" );
}
#endif

static void install_death_callback()
{
#if defined( BLOCK_FUZZ_SANITIZED )
    __sanitizer_set_death_callback( on_sanitizer_death );
#endif
}

// ---------------------------------------------------------------------------
// the seeded generator: splitmix64, so a mutant is a pure function of its site
// ---------------------------------------------------------------------------

struct Rng
{
    uint64_t state;

    uint64_t next()
    {
        state += 0x9e3779b97f4a7c15ull;
        uint64_t z = state;
        z = ( z ^ ( z >> 30 ) ) * 0xbf58476d1ce4e5b9ull;
        z = ( z ^ ( z >> 27 ) ) * 0x94d049bb133111ebull;
        return z ^ ( z >> 31 );
    }

    uint64_t below( uint64_t n ) { return n == 0 ? 0 : next() % n; }
};

static uint64_t mix( uint64_t a, uint64_t b )
{
    uint64_t z = a + 0x9e3779b97f4a7c15ull * ( b + 1 );
    z = ( z ^ ( z >> 30 ) ) * 0xbf58476d1ce4e5b9ull;
    z = ( z ^ ( z >> 27 ) ) * 0x94d049bb133111ebull;
    return z ^ ( z >> 31 );
}

// ---------------------------------------------------------------------------
// the buffer: EXACTLY the bytes the caller claims, 64-byte aligned
// ---------------------------------------------------------------------------
//
// posix_memalign under the address sanitizer puts its redzone immediately after
// the requested size, so "no byte read outside [ base, base + bytes )" is
// checked to the byte by the sanitized twin rather than to the page.

struct Region
{
    uint8_t * allocation;
    uint8_t * base; // allocation + lead: what Open is handed

    bool create( int64_t claim, int lead )
    {
        allocation = NULL;
        base = NULL;
        size_t want = (size_t) ( claim + lead );
        if ( want == 0 ) want = 1; // a zero-length request may legally return NULL
        void * p = NULL;
        if ( posix_memalign( &p, 64, want ) != 0 || p == NULL )
            return false;
        allocation = (uint8_t *) p;
        base = allocation + lead;
        return true;
    }

    void destroy() { free( allocation ); allocation = NULL; base = NULL; }
};

// ---------------------------------------------------------------------------
// the ORACLE, over the descriptors (docs/SPEC-TABLES.md §8, §19.2)
// ---------------------------------------------------------------------------
//
// Templated on the unit's own TableBlockInfo, because the block primitives ride
// in every package's namespace behind their own include guard: blockdemo's
// descriptor type and blockhome's are structurally identical and distinct.

// Once per unit, seed-independent: the descriptors' own layout is self
// consistent, so a per-mutant failure is about the INSTANCE and never about the
// generated tables.
template <typename Info>
static void check_descriptors( const Info * info, int depth )
{
    if ( depth > 8 )
        defect( "the block descriptors nest deeper than the fixed class can" );
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const auto & f = info->fields[i];
        if ( (uint64_t) f.offset + (uint64_t) f.size > (uint64_t) info->size )
            defect( "a block descriptor's field leaves the record it describes" );
        if ( f.out_of_line )
        {
            if ( f.size != 16 || f.offset_of_offset != f.offset || f.count_offset != f.offset + 8 || f.stride_offset != f.offset + 12 )
                defect( "an out-of-line array's triple does not sit at 0/8/12 of its field (docs/SPEC-TABLES.md §2.7)" );
            const Info * element = f.element();
            if ( element == NULL || element->size != f.stride )
                defect( "an out-of-line array's pitch is not its element's sizeof (docs/SPEC-TABLES.md §2.7)" );
            check_descriptors( element, depth + 1 );
        }
        else if ( f.element != NULL )
        {
            check_descriptors( f.element(), depth + 1 );
        }
    }
}

// The per-mutant oracle. `maxima` are the DECLARED maxima, read off the
// generated constants, in the order the out-of-line arrays are declared.
template <typename Info>
static void walk_opened( const Info * info, const uint8_t * base, int64_t bytes, int64_t reported,
                         const int64_t * maxima, int num_maxima )
{
    if ( reported < (int64_t) info->size || reported > bytes )
        defect( "an opened block reports a used extent outside [ the projection, the bytes the caller passed ]" );
    if ( (int64_t) info->size > bytes )
        defect( "an opened block's projection does not fit the extent the caller passed" );

    int array = 0;
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const auto & f = info->fields[i];
        if ( !f.out_of_line )
            continue;
        if ( array >= num_maxima )
            defect( "the unit's maxima table is shorter than its out-of-line arrays" );

        uint64_t offset_of = 0;
        uint32_t count = 0;
        uint32_t stride = 0;
        memcpy( &offset_of, base + f.offset_of_offset, 8 );
        memcpy( &count, base + f.count_offset, 4 );
        memcpy( &stride, base + f.stride_offset, 4 );

        const Info * element = f.element();

        // the layout contract, in the instance (§19.3): the pitch a consumer
        // indexes with must be this build's own, or the two sides are reading
        // different records at the same offsets
        if ( stride != f.stride )
            defect( "an opened block carries a pitch that is not this build's own (docs/SPEC-TABLES.md §19.3)" );
        if ( element == NULL || element->size != stride )
            defect( "an opened block's row descriptor disagrees with the pitch it opened at" );

        // the declared maximum (§19.2's tenth forgery): a consumer that sizes
        // anything by the maximum overflows on a count the maximum does not bound
        if ( (int64_t) count > maxima[array] )
            defect( "an opened block carries a count past its DECLARED MAXIMUM" );

        // the start: past the projection, and aligned for the element
        if ( offset_of < (uint64_t) info->size )
            defect( "an opened block's array starts inside the projection" );
        uint64_t start_alignment = 64;
        if ( element->align > start_alignment )
            start_alignment = element->align;
        if ( ( offset_of % start_alignment ) != 0 )
            defect( "an opened block's array does not start aligned for its element (docs/SPEC-TABLES.md §19.1)" );

        // the extent, computed without ever overflowing
        if ( stride != 0 && (uint64_t) count > ( UINT64_MAX - offset_of ) / stride )
            defect( "an opened block's array extent does not fit in 64 bits" );
        const uint64_t end = offset_of + (uint64_t) count * (uint64_t) stride;
        if ( end > (uint64_t) bytes )
            defect( "an opened block's rows leave the extent the caller passed" );
        if ( end > (uint64_t) reported )
            defect( "an opened block's rows leave the used extent it reported" );

        // THE WHOLE WALK: every byte of every row, actually read. Checked first
        // and read second, so the oracle names the defect and the sanitizer
        // confirms independently that nothing above was modelled wrong. The
        // read is one memcpy per array because the sanitizer range-checks a
        // memcpy whole, which is the same proof a byte loop gives at a fraction
        // of the gate's budget.
        if ( count > 0 )
        {
            const size_t span = (size_t) ( (uint64_t) count * (uint64_t) stride );
            if ( span > walk_scratch_bytes )
                defect( "an opened block's array is larger than any storage this build can size" );
            memcpy( walk_scratch, base + offset_of, span );
            sink += walk_scratch[0] + walk_scratch[span - 1];
        }
        array++;
    }
    if ( array != num_maxima )
        defect( "the unit's maxima table does not match its out-of-line arrays" );
}

// ---------------------------------------------------------------------------
// the units
// ---------------------------------------------------------------------------
//
// One entry per block-form table under the fuzzer: how to build a valid block
// for a count vector, and how to open one and walk it. The three cover the
// block corpus — the LARGEST PROJECTION (RenderFrame, nine triples), the
// padding and inline-storage unit (PaddedFrame), and the block home's row with
// two bounded arrays nested by value (PartFrame).

// A named slot in the projection: what the enumerated pass overwrites.
struct Slot
{
    const char * name;
    uint32_t offset;
    int width;      // 8 for the prologue words and offset_of, 4 for count and stride
    int64_t maximum; // the semantic maximum for this slot, or -1
};

static const int MaxSlots = 64;

struct Unit
{
    const char * name;
    int64_t max_bytes;
    int num_vectors;
    int64_t projection_size;
    int num_arrays;
    int64_t maxima[16];

    // Lay a valid block out over `storage` for vector `v` and copy its used
    // extent into `out`; returns the extent, or -1.
    int64_t ( *seed_block )( int v, uint8_t * out );

    // Open the mutant and, if it opened, run the oracle over it. Returns true
    // when BlockOpen accepted.
    bool ( *open_and_walk )( void * base, int64_t bytes, const Unit & unit );

    Slot slots[MaxSlots];
    int num_slots;
};

// ---- the slot table, built once per unit from the descriptors ----

template <typename Info>
static void build_slots( Unit & unit, const Info * info )
{
    unit.num_slots = 0;
    Slot * s = unit.slots;
    s[unit.num_slots++] = Slot{ "magic", 0, 8, -1 };
    s[unit.num_slots++] = Slot{ "build_version", 8, 8, -1 };
    s[unit.num_slots++] = Slot{ "byte_order", 16, 8, 2 };
    int array = 0;
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const auto & f = info->fields[i];
        if ( !f.out_of_line )
            continue;
        if ( unit.num_slots + 3 > MaxSlots )
            defect( "the slot table is too small for this unit's arrays" );
        s[unit.num_slots++] = Slot{ f.name, f.offset_of_offset, 8, (int64_t) unit.max_bytes };
        s[unit.num_slots++] = Slot{ f.name, f.count_offset, 4, unit.maxima[array] };
        s[unit.num_slots++] = Slot{ f.name, f.stride_offset, 4, (int64_t) f.stride };
        array++;
    }
}

// ---- RenderFrame: the largest projection (docs/SPEC-TABLES.md §19.1's worked table) ----

static blockdemo::TableBlockAllocator plain_allocator()
{
    return blockdemo::TableBlockDefaultAllocator();
}

static int64_t render_seed_block( int v, uint8_t * out )
{
    using namespace blockdemo;
    RenderFrameCounts c;
    switch ( v )
    {
        case 0: break;                                                        // every count zero
        case 1: c.cameras = 1; c.ships = 1; c.turrets = 1; c.missiles = 1;
                c.dynamic_props = 1; c.static_props = 1; c.cosmetic_props = 1;
                c.lasers = 1; c.explosions = 1; break;                        // one row apiece
        case 2: c.cameras = 1; c.ships = 300; c.turrets = 900; c.missiles = 120;
                c.dynamic_props = 40; c.static_props = 5000; c.cosmetic_props = 800;
                c.lasers = 200; c.explosions = 60; break;                     // §19.1's worked frame
        case 3: c.cameras = 0; c.ships = 7; c.turrets = 0; c.missiles = 33;
                c.dynamic_props = 0; c.static_props = 0; c.cosmetic_props = 11;
                c.lasers = 0; c.explosions = 1; break;                        // mixed, with empties
        case 4: c.cameras = RenderFrameBlock::CamerasMax; c.ships = RenderFrameBlock::ShipsMax;
                c.turrets = RenderFrameBlock::TurretsMax; c.missiles = RenderFrameBlock::MissilesMax;
                c.dynamic_props = RenderFrameBlock::DynamicPropsMax; c.static_props = RenderFrameBlock::StaticPropsMax;
                c.cosmetic_props = RenderFrameBlock::CosmeticPropsMax; c.lasers = RenderFrameBlock::LasersMax;
                c.explosions = RenderFrameBlock::ExplosionsMax; break;        // every count at its maximum
        default: return -1;
    }
    RenderFrameBlockStorage storage;
    if ( !storage.Create( plain_allocator() ) )
        return -1;
    memset( storage.base, 0x5a, (size_t) RenderFrameBlockMaxBytes ); // the tail is unspecified; pin it for reproducibility
    RenderFrameBlock block;
    if ( !RenderFrameBlockBegin( block, storage, c ) )
    {
        storage.Destroy();
        return -1;
    }
    block.projection->version = RenderVersion;
    const int64_t bytes = RenderFrameBlockBytes( block );
    memcpy( out, storage.base, (size_t) bytes );
    storage.Destroy();
    return bytes;
}

static bool render_open_and_walk( void * base, int64_t bytes, const Unit & unit )
{
    using namespace blockdemo;
    RenderFrameBlock block;
    if ( !RenderFrameBlockOpen( block, base, bytes ) )
    {
        if ( block.base != NULL || block.projection != NULL || block.bytes != 0 )
            defect( "a refused block points at something rather than at nothing (docs/SPEC-TABLES.md §19.2)" );
        return false;
    }
    if ( block.base != (uint8_t *) base )
        defect( "an opened block points somewhere other than at the base the caller passed" );
    walk_opened( RenderFrameBlock::Type(), block.base, bytes, block.bytes, unit.maxima, unit.num_arrays );
    // and once more through the GENERATED accessors, because §19.2 offers both
    uint64_t accumulator = 0;
    for ( const RenderCamera & row : RenderFrameCameras( block ) ) accumulator += (uint64_t) row.camera_id;
    for ( const RenderShip & row : RenderFrameShips( block ) ) accumulator += (uint64_t) row.object_id;
    for ( const RenderTurret & row : RenderFrameTurrets( block ) ) accumulator += (uint64_t) row.object_id;
    for ( const RenderMissile & row : RenderFrameMissiles( block ) ) accumulator += (uint64_t) row.object_id;
    for ( const RenderDynamicProp & row : RenderFrameDynamicProps( block ) ) accumulator += (uint64_t) row.object_id;
    for ( const RenderStaticProp & row : RenderFrameStaticProps( block ) ) accumulator += (uint64_t) row.static_prop_id;
    for ( const RenderCosmeticProp & row : RenderFrameCosmeticProps( block ) ) accumulator += (uint64_t) row.cosmetic_prop_id;
    for ( const RenderLaser & row : RenderFrameLasers( block ) ) accumulator += (uint64_t) row.laser_id;
    for ( const RenderExplosion & row : RenderFrameExplosions( block ) ) accumulator += (uint64_t) row.explosion_id;
    sink += accumulator;
    return true;
}

// ---- PaddedFrame: interior padding and the inline storage classes ----

static int64_t padded_seed_block( int v, uint8_t * out )
{
    using namespace blockdemo;
    PaddedFrameCounts c;
    switch ( v )
    {
        case 0: break;
        case 1: c.rows = 1; break;
        case 2: c.rows = 7; break;
        case 3: c.rows = PaddedFrameBlock::RowsMax; break;
        default: return -1;
    }
    PaddedFrameBlockStorage storage;
    if ( !storage.Create( plain_allocator() ) )
        return -1;
    memset( storage.base, 0x5a, (size_t) PaddedFrameBlockMaxBytes );
    PaddedFrameBlock block;
    if ( !PaddedFrameBlockBegin( block, storage, c ) )
    {
        storage.Destroy();
        return -1;
    }
    const int64_t bytes = PaddedFrameBlockBytes( block );
    memcpy( out, storage.base, (size_t) bytes );
    storage.Destroy();
    return bytes;
}

static bool padded_open_and_walk( void * base, int64_t bytes, const Unit & unit )
{
    using namespace blockdemo;
    PaddedFrameBlock block;
    if ( !PaddedFrameBlockOpen( block, base, bytes ) )
    {
        if ( block.base != NULL || block.projection != NULL || block.bytes != 0 )
            defect( "a refused block points at something rather than at nothing (docs/SPEC-TABLES.md §19.2)" );
        return false;
    }
    if ( block.base != (uint8_t *) base )
        defect( "an opened block points somewhere other than at the base the caller passed" );
    walk_opened( PaddedFrameBlock::Type(), block.base, bytes, block.bytes, unit.maxima, unit.num_arrays );
    uint64_t accumulator = 0;
    for ( const PaddedRow & row : PaddedFrameRows( block ) ) accumulator += (uint64_t) row.id + row.tag;
    sink += accumulator;
    return true;
}

// ---- PartFrame: the block home, with two bounded arrays nested by value ----

static int64_t part_seed_block( int v, uint8_t * out )
{
    using namespace blockhome;
    PartFrameCounts c;
    switch ( v )
    {
        case 0: break;
        case 1: c.parts = 1; break;
        case 2: c.parts = 5; break;
        case 3: c.parts = PartFrameBlock::PartsMax; break;
        default: return -1;
    }
    PartFrameBlockStorage storage;
    if ( !storage.Create( TableBlockDefaultAllocator() ) )
        return -1;
    memset( storage.base, 0x5a, (size_t) PartFrameBlockMaxBytes );
    PartFrameBlock block;
    if ( !PartFrameBlockBegin( block, storage, c ) )
    {
        storage.Destroy();
        return -1;
    }
    const int64_t bytes = PartFrameBlockBytes( block );
    memcpy( out, storage.base, (size_t) bytes );
    storage.Destroy();
    return bytes;
}

static bool part_open_and_walk( void * base, int64_t bytes, const Unit & unit )
{
    using namespace blockhome;
    PartFrameBlock block;
    if ( !PartFrameBlockOpen( block, base, bytes ) )
    {
        if ( block.base != NULL || block.projection != NULL || block.bytes != 0 )
            defect( "a refused block points at something rather than at nothing (docs/SPEC-TABLES.md §19.2)" );
        return false;
    }
    if ( block.base != (uint8_t *) base )
        defect( "an opened block points somewhere other than at the base the caller passed" );
    walk_opened( PartFrameBlock::Type(), block.base, bytes, block.bytes, unit.maxima, unit.num_arrays );
    uint64_t accumulator = 0;
    for ( const PartRow & row : PartFrameParts( block ) ) accumulator += (uint64_t) row.part_id + row.slot;
    sink += accumulator;
    return true;
}

static Unit units[3];

static void build_units()
{
    using namespace blockdemo;

    Unit & render = units[0];
    render.name = "render";
    render.max_bytes = RenderFrameBlockMaxBytes;
    render.num_vectors = 5;
    render.projection_size = (int64_t) sizeof( RenderFrameBlock::Projection );
    render.num_arrays = 9;
    const int64_t render_maxima[9] = {
        RenderFrameBlock::CamerasMax, RenderFrameBlock::ShipsMax, RenderFrameBlock::TurretsMax,
        RenderFrameBlock::MissilesMax, RenderFrameBlock::DynamicPropsMax, RenderFrameBlock::StaticPropsMax,
        RenderFrameBlock::CosmeticPropsMax, RenderFrameBlock::LasersMax, RenderFrameBlock::ExplosionsMax };
    memcpy( render.maxima, render_maxima, sizeof( render_maxima ) );
    render.seed_block = render_seed_block;
    render.open_and_walk = render_open_and_walk;
    build_slots( render, RenderFrameBlock::Type() );

    Unit & padded = units[1];
    padded.name = "padded";
    padded.max_bytes = PaddedFrameBlockMaxBytes;
    padded.num_vectors = 4;
    padded.projection_size = (int64_t) sizeof( PaddedFrameBlock::Projection );
    padded.num_arrays = 1;
    padded.maxima[0] = PaddedFrameBlock::RowsMax;
    padded.seed_block = padded_seed_block;
    padded.open_and_walk = padded_open_and_walk;
    build_slots( padded, PaddedFrameBlock::Type() );

    Unit & part = units[2];
    part.name = "part";
    part.max_bytes = blockhome::PartFrameBlockMaxBytes;
    part.num_vectors = 4;
    part.projection_size = (int64_t) sizeof( blockhome::PartFrameBlock::Projection );
    part.num_arrays = 1;
    part.maxima[0] = blockhome::PartFrameBlock::PartsMax;
    part.seed_block = part_seed_block;
    part.open_and_walk = part_open_and_walk;
    build_slots( part, blockhome::PartFrameBlock::Type() );
}

// ---------------------------------------------------------------------------
// the mutators
// ---------------------------------------------------------------------------

// The boundary values every field overwrite draws from, plus the three derived
// from the slot's own semantic maximum. Negative-as-unsigned is the last three.
static const uint64_t boundaries[] = {
    0ull, 1ull, 2ull,
    0x7fffffffull, 0x80000000ull, 0xffffffffull,
    0x100000000ull,
    0x7fffffffffffffffull, 0x8000000000000000ull, 0xffffffffffffffffull,
    0xfffffffffffffffeull, 0xfffffffe00000000ull,
    // and the same extremes rounded DOWN to a block's 64-byte start alignment,
    // because an offset_of that is not 64-aligned is refused before it reaches
    // the arithmetic and never exercises it
    0x4000000000000000ull, 0x7fffffffffffffc0ull, 0x7fffffffffffff80ull,
    0x8000000000000040ull, 0xffffffffffffffc0ull,
};
static const int num_boundaries = (int) ( sizeof( boundaries ) / sizeof( boundaries[0] ) );

static void write_word( uint8_t * buffer, int64_t buffer_bytes, uint64_t offset, int width, uint64_t value )
{
    if ( (int64_t) offset + width > buffer_bytes )
        return;
    for ( int i = 0; i < width; i++ )
        buffer[offset + i] = (uint8_t) ( value >> ( 8 * i ) ); // little-endian, the order this build writes
}

// the one enumerated overwrite the deterministic pass runs, and the one the
// random pass draws: value index v in [0, num_boundaries + 3)
static uint64_t boundary_value( const Slot & slot, int v )
{
    if ( v < num_boundaries )
        return boundaries[v];
    if ( slot.maximum < 0 )
        return boundaries[( v - num_boundaries ) % num_boundaries];
    switch ( v - num_boundaries )
    {
        case 0: return (uint64_t) slot.maximum - 1;
        case 1: return (uint64_t) slot.maximum;
        default: return (uint64_t) slot.maximum + 1;
    }
}
static const int num_values = num_boundaries + 3;

enum MutationKind
{
    K_NONE = 0,
    K_FLIP,
    K_WORD,
    K_SWAP_TRIPLES,
    K_OVERLAP,
    K_IN_PROJECTION,
    K_BYTE_ORDER,
    K_KINDS
};

// The random mutator. `buffer` holds the first `copied` bytes of the seed
// block, and nothing here writes past that: bytes the caller claims but the
// seed block did not fill are garbage already.
static void mutate_random( Rng & rng, const Unit & unit, uint8_t * buffer, int64_t copied )
{
    const int kind = (int) rng.below( K_KINDS );
    const int64_t projection = unit.projection_size;
    switch ( kind )
    {
        case K_NONE:
            describe( "no mutation: the valid block itself" );
            return;

        case K_FLIP:
        {
            const int flips = 1 + (int) rng.below( 8 );
            int written = snprintf( site.description, sizeof( site.description ), "byte flips at" );
            for ( int i = 0; i < flips; i++ )
            {
                // three quarters land in the prologue and the triples, where a
                // flip decides something; the rest anywhere in the extent
                int64_t limit = ( rng.below( 4 ) != 0 && copied > projection ) ? projection : copied;
                if ( limit <= 0 )
                    break;
                const int64_t at = (int64_t) rng.below( (uint64_t) limit );
                buffer[at] ^= (uint8_t) ( 1u << rng.below( 8 ) );
                if ( written < (int) sizeof( site.description ) - 32 )
                    written += snprintf( site.description + written, sizeof( site.description ) - written, " %lld", (long long) at );
            }
            return;
        }

        case K_WORD:
        {
            const int which = (int) rng.below( (uint64_t) unit.num_slots + 4 );
            uint64_t offset;
            int width;
            uint64_t value;
            const char * name;
            if ( which < unit.num_slots )
            {
                const Slot & slot = unit.slots[which];
                name = slot.name;
                offset = slot.offset;
                // 1, 2, 4 or 8 bytes, so a partial overwrite of a word is a case too
                static const int widths[4] = { 1, 2, 4, 8 };
                width = widths[rng.below( 4 )];
                if ( offset + width > (uint64_t) projection )
                    width = slot.width;
                value = boundary_value( slot, (int) rng.below( num_values ) );
            }
            else
            {
                name = "anywhere in the projection";
                offset = rng.below( (uint64_t) projection );
                static const int widths[4] = { 1, 2, 4, 8 };
                width = widths[rng.below( 4 )];
                if ( offset + width > (uint64_t) projection )
                    offset = (uint64_t) projection - width;
                value = boundaries[rng.below( num_boundaries )];
            }
            write_word( buffer, copied, offset, width, value );
            describe( "%d-bit overwrite of %s at %llu with 0x%llx", width * 8, name,
                      (unsigned long long) offset, (unsigned long long) value );
            return;
        }

        case K_SWAP_TRIPLES:
        {
            if ( unit.num_arrays < 2 || copied < projection )
            {
                describe( "no mutation: this unit has fewer than two triples to swap" );
                return;
            }
            const int a = (int) rng.below( (uint64_t) unit.num_arrays );
            int b = (int) rng.below( (uint64_t) unit.num_arrays - 1 );
            if ( b >= a ) b++;
            const uint64_t offset_a = unit.slots[3 + 3 * a].offset;
            const uint64_t offset_b = unit.slots[3 + 3 * b].offset;
            uint8_t swap[16];
            memcpy( swap, buffer + offset_a, 16 );
            memcpy( buffer + offset_a, buffer + offset_b, 16 );
            memcpy( buffer + offset_b, swap, 16 );
            describe( "the triples of arrays %d and %d swapped whole", a, b );
            return;
        }

        case K_OVERLAP:
        {
            if ( unit.num_arrays < 2 || copied < projection )
            {
                describe( "no mutation: this unit has fewer than two arrays to overlap" );
                return;
            }
            const int a = (int) rng.below( (uint64_t) unit.num_arrays );
            int b = (int) rng.below( (uint64_t) unit.num_arrays - 1 );
            if ( b >= a ) b++;
            const uint64_t offset_a = unit.slots[3 + 3 * a].offset;
            const uint64_t offset_b = unit.slots[3 + 3 * b].offset;
            memcpy( buffer + offset_a, buffer + offset_b, 8 ); // a starts where b starts
            describe( "array %d's rows moved on top of array %d's", a, b );
            return;
        }

        case K_IN_PROJECTION:
        {
            const int a = (int) rng.below( (uint64_t) unit.num_arrays );
            const uint64_t offset = unit.slots[3 + 3 * a].offset;
            const uint64_t value = rng.below( (uint64_t) projection );
            write_word( buffer, copied, offset, 8, value );
            describe( "array %d's offset_of moved inside the projection, to %llu", a, (unsigned long long) value );
            return;
        }

        case K_BYTE_ORDER:
        {
            static const uint64_t orders[4] = { 1, 2, 0, 0x0100000000000000ull };
            const uint64_t value = orders[rng.below( 4 )];
            write_word( buffer, copied, 16, 8, value );
            describe( "the byte order word forged to %llu with the data unswapped", (unsigned long long) value );
            return;
        }

        default:
            describe( "no mutation" );
            return;
    }
}

// ---------------------------------------------------------------------------
// the runner
// ---------------------------------------------------------------------------

struct Options
{
    uint64_t seed;
    int64_t random_mutants; // per unit
    const char * only_unit;
    int only_vector;
    const char * only_pass;
    int64_t only_index;
    const char * dump_directory;
};

static Options options;
static int64_t mutants_run = 0;
static int64_t mutants_opened = 0;

static bool selected( const char * unit, int vector, const char * pass, int64_t index )
{
    if ( options.only_unit == NULL )
        return true;
    return strcmp( options.only_unit, unit ) == 0 && options.only_vector == vector
        && strcmp( options.only_pass, pass ) == 0 && options.only_index == index;
}

// One mutant, end to end: an exact-sized region, the seed block copied in, the
// mutation applied, Open, and the oracle.
static void run_one( const Unit & unit, int vector, const char * pass, int64_t index,
                     const uint8_t * seed_block, int64_t extent,
                     int64_t claim, int lead, bool random_mutation, uint64_t rng_seed,
                     const char * fixed_description )
{
    if ( !selected( unit.name, vector, pass, index ) )
        return;

    site.unit = unit.name;
    site.vector = vector;
    site.pass = pass;
    site.index = index;

    Region region;
    if ( !region.create( claim, lead ) )
    {
        printf( "FAILED: the fuzzer could not allocate a %lld-byte region\n", (long long) claim );
        exit( 1 );
    }
    const int64_t copied = claim < extent ? claim : extent;
    if ( copied > 0 )
        memcpy( region.base, seed_block, (size_t) copied );
    // extension with GARBAGE: the bytes past the seed block are not zeros
    if ( claim > copied )
    {
        Rng garbage;
        garbage.state = mix( rng_seed, 0xda7a );
        for ( int64_t i = copied; i < claim; i++ )
            region.base[i] = (uint8_t) garbage.next();
    }

    if ( fixed_description != NULL )
        snprintf( site.description, sizeof( site.description ), "%s", fixed_description );
    else
        site.description[0] = 0;

    if ( random_mutation )
    {
        Rng rng;
        rng.state = rng_seed;
        mutate_random( rng, unit, region.base, copied );
    }

    if ( claim != extent || lead != 0 )
    {
        char tail[128];
        snprintf( tail, sizeof( tail ), "%s [ %lld bytes claimed of a %lld-byte block, base + %d ]",
                  site.description[0] ? ";" : "", (long long) claim, (long long) extent, lead );
        const size_t used = strlen( site.description );
        snprintf( site.description + used, sizeof( site.description ) - used, "%s", tail );
    }

    mutants_run++;
    if ( unit.open_and_walk( region.base, claim, unit ) )
        mutants_opened++;
    region.destroy();
}

static void run_unit( Unit & unit, int unit_index )
{
    uint8_t * seed_block = (uint8_t *) malloc( (size_t) unit.max_bytes );
    if ( seed_block == NULL )
    {
        printf( "FAILED: out of memory for %s's seed block\n", unit.name );
        exit( 1 );
    }

    for ( int vector = 0; vector < unit.num_vectors; vector++ )
    {
        const int64_t extent = unit.seed_block( vector, seed_block );
        if ( extent <= 0 )
        {
            printf( "FAILED: %s could not build a valid block for count vector %d\n", unit.name, vector );
            exit( 1 );
        }

        if ( options.dump_directory != NULL )
        {
            char path[512];
            snprintf( path, sizeof( path ), "%s/%s_v%d.bin", options.dump_directory, unit.name, vector );
            FILE * f = fopen( path, "wb" );
            if ( f == NULL )
            {
                printf( "FAILED: cannot write the seed block %s\n", path );
                exit( 1 );
            }
            fwrite( seed_block, 1, (size_t) extent, f );
            fclose( f );
            continue;
        }

        // pass "valid": the unmutated block opens, so a green run is not a
        // fuzzer that refuses everything
        {
            site.unit = unit.name; site.vector = vector; site.pass = "valid"; site.index = 0;
            Region region;
            region.create( extent, 0 );
            memcpy( region.base, seed_block, (size_t) extent );
            describe( "the valid block, unmutated" );
            mutants_run++;
            if ( !unit.open_and_walk( region.base, extent, unit ) )
                defect( "the VALID block this build's own builder wrote did not open" );
            mutants_opened++;
            region.destroy();
        }

        // pass "trunc": every length in [ 0, extent + 64 ]. Exhaustive where the
        // sum of the copies stays sane; sampled beyond, because the sum is
        // quadratic in the extent and a 7.5 MiB block would spend the whole
        // budget on memcpy.
        {
            const int64_t exhaustive_limit = 8192;
            if ( extent <= exhaustive_limit )
            {
                for ( int64_t claim = 0; claim <= extent + 64; claim++ )
                    run_one( unit, vector, "trunc", claim, seed_block, extent, claim, 0, false, 0,
                             "truncated or extended, otherwise untouched" );
            }
            else
            {
                // the boundaries that decide something, plus a spread
                int64_t index = 0;
                const int64_t interesting[] = {
                    0, 1, 8, unit.projection_size - 1, unit.projection_size, unit.projection_size + 1,
                    extent - 65, extent - 64, extent - 63, extent - 1, extent, extent + 1, extent + 63, extent + 64 };
                for ( size_t i = 0; i < sizeof( interesting ) / sizeof( interesting[0] ); i++ )
                {
                    const int64_t claim = interesting[i];
                    if ( claim >= 0 && claim <= extent + 64 )
                        run_one( unit, vector, "trunc", index, seed_block, extent, claim, 0, false, 0,
                                 "truncated or extended, otherwise untouched" );
                    index++;
                }
                const int64_t samples = extent > ( 1 << 20 ) ? 64 : 256;
                for ( int64_t k = 0; k < samples; k++, index++ )
                {
                    Rng rng; rng.state = mix( options.seed, mix( vector, k ) );
                    const int64_t claim = (int64_t) rng.below( (uint64_t) extent + 65 );
                    run_one( unit, vector, "trunc", index, seed_block, extent, claim, 0, false, 0,
                             "truncated or extended, otherwise untouched" );
                }
            }
        }

        // pass "lead": the caller's buffer at base + 1 .. base + 63
        for ( int lead = 1; lead < 64; lead++ )
            run_one( unit, vector, "lead", lead, seed_block, extent, extent, lead, false, 0,
                     "an unaligned base" );

        // pass "slot": every named slot x every width x every boundary value.
        // Seed-independent and exhaustive — this is where the offset_of, count
        // and stride boundaries actually get their coverage.
        //
        // The region is allocated ONCE at exactly the extent and the PROJECTION
        // is restored between mutants: a slot overwrite touches nothing else,
        // and re-copying a 7.5 MiB block per mutant would spend the whole gate
        // budget on memcpy without covering one more case.
        {
            Region region;
            if ( !region.create( extent, 0 ) )
            {
                printf( "FAILED: the fuzzer could not allocate a %lld-byte region\n", (long long) extent );
                exit( 1 );
            }
            memcpy( region.base, seed_block, (size_t) extent );
            int64_t index = 0;
            static const int widths[4] = { 1, 2, 4, 8 };
            for ( int s = 0; s < unit.num_slots; s++ )
            {
                for ( int w = 0; w < 4; w++ )
                {
                    if ( unit.slots[s].offset + widths[w] > (uint64_t) unit.projection_size )
                        continue;
                    for ( int v = 0; v < num_values; v++, index++ )
                    {
                        if ( !selected( unit.name, vector, "slot", index ) )
                            continue;
                        site.unit = unit.name; site.vector = vector; site.pass = "slot"; site.index = index;
                        memcpy( region.base, seed_block, (size_t) unit.projection_size );
                        const uint64_t value = boundary_value( unit.slots[s], v );
                        write_word( region.base, extent, unit.slots[s].offset, widths[w], value );
                        describe( "%d-bit overwrite of %s at %llu with 0x%llx", widths[w] * 8,
                                  unit.slots[s].name, (unsigned long long) unit.slots[s].offset,
                                  (unsigned long long) value );
                        mutants_run++;
                        if ( unit.open_and_walk( region.base, extent, unit ) )
                            mutants_opened++;
                    }
                }
            }
            region.destroy();
        }

        // pass "random": the seeded mutators, over lengths and leads too. The
        // max-count vector takes an eighth of the budget, because its extent is
        // megabytes and the memcpy is the whole cost.
        {
            int64_t budget = options.random_mutants / unit.num_vectors;
            if ( extent > ( 1 << 20 ) )
                budget /= 8;
            for ( int64_t k = 0; k < budget; k++ )
            {
                const uint64_t rng_seed = mix( mix( options.seed, mix( (uint64_t) unit_index, (uint64_t) vector ) ), k );
                Rng axes; axes.state = mix( rng_seed, 0x5eed );
                int64_t claim = extent;
                if ( axes.below( 4 ) == 0 )
                    claim = (int64_t) axes.below( (uint64_t) extent + 65 );
                int lead = 0;
                if ( axes.below( 8 ) == 0 )
                    lead = 1 + (int) axes.below( 63 );
                run_one( unit, vector, "random", k, seed_block, extent, claim, lead, true, rng_seed, NULL );
            }
        }
    }

    free( seed_block );
}

int main( int argc, char ** argv )
{
    options.seed = 0x5c8ea11deull;
    options.random_mutants = 24000;
    options.only_unit = NULL;
    options.only_vector = 0;
    options.only_pass = NULL;
    options.only_index = 0;
    options.dump_directory = NULL;

    if ( const char * e = getenv( "SEED" ) )
        if ( *e != 0 ) options.seed = strtoull( e, NULL, 0 );
    if ( const char * e = getenv( "N" ) )
        if ( *e != 0 ) options.random_mutants = strtoll( e, NULL, 0 );

    static char only[256];
    for ( int i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--seed" ) == 0 && i + 1 < argc )
            options.seed = strtoull( argv[++i], NULL, 0 );
        else if ( strcmp( argv[i], "--mutants" ) == 0 && i + 1 < argc )
            options.random_mutants = strtoll( argv[++i], NULL, 0 );
        else if ( strcmp( argv[i], "--dump" ) == 0 && i + 1 < argc )
            options.dump_directory = argv[++i];
        else if ( strcmp( argv[i], "--only" ) == 0 && i + 1 < argc )
        {
            snprintf( only, sizeof( only ), "%s", argv[++i] );
            char * unit = only;
            char * vector = strchr( unit, ':' );
            if ( vector == NULL ) { printf( "FAILED: --only wants unit:vector:pass:index\n" ); return 1; }
            *vector++ = 0;
            char * pass = strchr( vector, ':' );
            if ( pass == NULL ) { printf( "FAILED: --only wants unit:vector:pass:index\n" ); return 1; }
            *pass++ = 0;
            char * index = strchr( pass, ':' );
            if ( index == NULL ) { printf( "FAILED: --only wants unit:vector:pass:index\n" ); return 1; }
            *index++ = 0;
            options.only_unit = unit;
            options.only_vector = (int) strtol( vector, NULL, 10 );
            options.only_pass = pass;
            options.only_index = strtoll( index, NULL, 10 );
        }
        else
        {
            printf( "FAILED: unknown argument %s\n", argv[i] );
            return 1;
        }
    }

    run_seed = options.seed;
    install_death_callback();

    walk_scratch_bytes = (size_t) blockdemo::RenderFrameBlockMaxBytes;
    walk_scratch = (uint8_t *) malloc( walk_scratch_bytes );
    if ( walk_scratch == NULL )
    {
        printf( "FAILED: out of memory for the walk's destination\n" );
        return 1;
    }

    site.unit = "startup";
    site.pass = "startup";
    describe( "no mutation" );

    build_units();

    // the descriptors' own layout, once: a per-mutant failure is then about the
    // instance and never about the generated tables
    check_descriptors( blockdemo::RenderFrameBlock::Type(), 0 );
    check_descriptors( blockdemo::PaddedFrameBlock::Type(), 0 );
    check_descriptors( blockhome::PartFrameBlock::Type(), 0 );

    if ( options.dump_directory == NULL )
        printf( "block forgery fuzzer: SEED=%llu N=%lld (per unit, across its count vectors)\n",
                (unsigned long long) options.seed, (long long) options.random_mutants );

    for ( int i = 0; i < 3; i++ )
        run_unit( units[i], i );

    if ( options.dump_directory != NULL )
    {
        printf( "block forgery fuzzer: seed blocks written to %s\n", options.dump_directory );
        return 0;
    }

    if ( failures != 0 )
        return 1;

    printf( "block forgery fuzzer: %lld mutants over 3 units, %lld opened and walked whole, none escaped the extent (docs/SPEC-TABLES.md §19.2, §19.5)\n",
            (long long) mutants_run, (long long) mutants_opened );
    printf( "OK\n" );
    return 0;
}
