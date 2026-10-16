// THE C++ TABLES SOAK (docs/SPEC-TABLES.md "What allocates, and what never
// does"; docs/PORTING.md I9, issue #416's C++ half).
//
// test/tables/main.cpp's allocation audit is ONE pass: Load, the indexed and
// iterated reads, Measure and Save on the richest root, with a counting global
// operator new around them. A pass run once cannot see a path that retains a
// little per call, nor an allocation on a path that pass does not take. This
// runs the same instrument over a LOOP, for as long as the caller asks, over
// the corpus the C soak (test/c-tables/soak_main.c) reads: every root the wire
// surface pins, exact and evolution seams alike, through Load, the reads,
// Measure, Save and the text form.
//
// THE GATE IS THE ALLOCATION COUNT. It is read at every sample, and after the
// warmup pass it must not move, ever: a matched new/delete pair inside the
// loop leaves the heap exactly where it was, so only a COUNT says the paths
// allocate nothing. The live-heap drift is reported and gated beside it as the
// weaker instrument: it is the one that sees retention, and it is blind to a
// pair. `make tables-cpp-soak-negative-control` builds this file with
// -DSCHEMA_SOAK_PLANT_PAIR, one matched new/delete pair per iteration, and
// requires the count to go red while the drift line stays flat.
//
// It runs SOAK_SECONDS of real time, so it is a FUNCTIONAL test reached by a
// make target (`make tables-cpp-soak`), never a unit test.
//
//   ./build/schema_test_tables_soak <seconds>

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <atomic>
#include <chrono>
#include <new>

#include <fcntl.h>
#include <unistd.h>

#if defined( __GLIBC__ )
#include <malloc.h>
#elif defined( __APPLE__ )
#include <malloc/malloc.h>
#endif

#include "TablesTable.h"
#include "KeyedTable.h"
#include "NestedTable.h"
#include "WideTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"

// ---- INSTRUMENT ONE: the counting global operator new ----
//
// The counter test/tables/main.cpp defines for the one-shot audit, carried
// over every replaceable form, so an array, nothrow or over-aligned new is
// counted as well as the plain one. Counting never stops: the gate is a
// difference between two reads of it, never a window this file opens and
// closes.

static std::atomic<long long> soak_news( 0 );

static void * soak_new( size_t bytes )
{
    soak_news.fetch_add( 1, std::memory_order_relaxed );
    void * p = malloc( bytes != 0 ? bytes : 1 );
    if ( p == NULL ) { abort(); }
    return p;
}

static void * soak_new_aligned( size_t bytes, std::align_val_t align )
{
    soak_news.fetch_add( 1, std::memory_order_relaxed );
    void * p = NULL;
    const size_t a = (size_t) align < sizeof( void * ) ? sizeof( void * ) : (size_t) align;
    if ( posix_memalign( &p, a, bytes != 0 ? bytes : 1 ) != 0 ) { abort(); }
    return p;
}

void * operator new( size_t bytes ) { return soak_new( bytes ); }
void * operator new[]( size_t bytes ) { return soak_new( bytes ); }
void * operator new( size_t bytes, const std::nothrow_t & ) noexcept { return soak_new( bytes ); }
void * operator new[]( size_t bytes, const std::nothrow_t & ) noexcept { return soak_new( bytes ); }
void * operator new( size_t bytes, std::align_val_t align ) { return soak_new_aligned( bytes, align ); }
void * operator new[]( size_t bytes, std::align_val_t align ) { return soak_new_aligned( bytes, align ); }
void operator delete( void * p ) noexcept { free( p ); }
void operator delete[]( void * p ) noexcept { free( p ); }
void operator delete( void * p, size_t ) noexcept { free( p ); }
void operator delete[]( void * p, size_t ) noexcept { free( p ); }
void operator delete( void * p, std::align_val_t ) noexcept { free( p ); }
void operator delete[]( void * p, std::align_val_t ) noexcept { free( p ); }
void operator delete( void * p, size_t, std::align_val_t ) noexcept { free( p ); }
void operator delete[]( void * p, size_t, std::align_val_t ) noexcept { free( p ); }

// ---- INSTRUMENT TWO: the C allocator's own entry points, where they can be
// interposed ----
//
// operator new is not the only door. The generated headers reach the heap
// through `schema_allocate`, which is calloc unless the program defines it
// (the pointer arena and the cook), and a counter on operator new cannot see
// a calloc. On glibc the four entry points are defined here, in front of the
// library's, and forward to the real allocator by the name that does not come
// back through them. Elsewhere the count is operator new's alone, and the
// report says so.

#if defined( __GLIBC__ )
#define SCHEMA_SOAK_COUNTS_MALLOC 1
extern "C" void * __libc_malloc( size_t );
extern "C" void * __libc_calloc( size_t, size_t );
extern "C" void * __libc_realloc( void *, size_t );
extern "C" void __libc_free( void * );

static std::atomic<long long> soak_mallocs( 0 );

extern "C" void * malloc( size_t n ) noexcept
{
    soak_mallocs.fetch_add( 1, std::memory_order_relaxed );
    return __libc_malloc( n );
}
extern "C" void * calloc( size_t n, size_t m ) noexcept
{
    soak_mallocs.fetch_add( 1, std::memory_order_relaxed );
    return __libc_calloc( n, m );
}
extern "C" void * realloc( void * p, size_t n ) noexcept
{
    soak_mallocs.fetch_add( 1, std::memory_order_relaxed );
    return __libc_realloc( p, n );
}
extern "C" void free( void * p ) noexcept
{
    soak_mallocs.fetch_add( 1, std::memory_order_relaxed );
    __libc_free( p );
}
#else
#define SCHEMA_SOAK_COUNTS_MALLOC 0
#endif

static long long malloc_calls()
{
#if SCHEMA_SOAK_COUNTS_MALLOC
    return soak_mallocs.load( std::memory_order_relaxed );
#else
    return 0;
#endif
}

// ---- INSTRUMENT THREE: drift, the weaker one ----
//
// The allocator's live bytes, from its own statistics, and the resident set
// beside it. Neither allocates: mallinfo2 and the zone statistics read the
// allocator's books, and the resident set is read with open/read rather than
// stdio, whose first fopen would be an allocation in the measured section.

static long long live_heap_bytes()
{
#if defined( __GLIBC__ )
    struct mallinfo2 info = mallinfo2();
    return (long long) info.uordblks;
#elif defined( __APPLE__ )
    malloc_statistics_t stats;
    malloc_zone_statistics( malloc_default_zone(), &stats );
    return (long long) stats.size_in_use;
#else
    return -1;
#endif
}

static long long resident_kib()
{
#if defined( __linux__ )
    char text[128];
    const int fd = open( "/proc/self/statm", O_RDONLY );
    if ( fd < 0 ) { return -1; }
    const ssize_t n = read( fd, text, sizeof( text ) - 1 );
    close( fd );
    if ( n <= 0 ) { return -1; }
    text[n] = 0;
    const char * p = strchr( text, ' ' ); // statm: size resident shared ...
    if ( p == NULL ) { return -1; }
    return atoll( p + 1 ) * ( sysconf( _SC_PAGESIZE ) / 1024 );
#else
    return -1;
#endif
}

// ---- the reads ----
//
// The indexed and iterated reads of each root, folded into one checksum. The
// sum is compared to the one taken before the clock, every iteration, so a
// read that went wrong is a failure rather than a number nobody looks at, and
// the compiler cannot drop reads whose result is compared.

static uint64_t fold( uint64_t h, uint64_t x ) { return ( h ^ x ) * 0x100000001b3ull; }
static uint64_t fold_f( uint64_t h, float f )
{
    uint32_t bits;
    memcpy( (void *) &bits, &f, sizeof( bits ) );
    return fold( h, bits );
}

static uint64_t read_weapon( uint64_t h, const tabledemo::WeaponConfig & w )
{
    h = fold_f( h, w.damage );
    h = fold_f( h, w.speed );
    h = fold( h, (uint64_t) w.penetration );
    h = fold( h, w.channel );
    h = fold( h, w.homing );
    return fold( h, (uint64_t) w.effect.type );
}

static uint64_t read_loadout( uint64_t h, const tabledemo::LoadoutConfig & v )
{
    h = fold( h, (uint64_t) v.grade );
    for ( int32_t i = 0; i < v.grades_count; i++ ) { h = fold( h, (uint64_t) v.grades[i] ); }
    for ( const tabledemo::Grade g : v.podium ) { h = fold( h, (uint64_t) g ); }
    h = fold( h, (uint64_t) v.perks );
    h = read_weapon( h, v.primary );
    for ( const tabledemo::WeaponConfig & w : v.backups ) { h = read_weapon( h, w ); }
    for ( int32_t i = 0; i < v.attachments_count; i++ )
    {
        h = fold( h, (uint64_t) v.attachments[i].slot );
        h = fold_f( h, v.attachments[i].power );
    }
    if ( v.attachments_count > 0 ) { h = fold( h, (uint64_t) v.attachments[v.attachments_count - 1].slot ); }
    return h;
}

static uint64_t read_profile( uint64_t h, const tabledemo::ProfileConfig & v )
{
    for ( int32_t i = 0; i < v.name_length; i++ ) { h = fold( h, (uint8_t) v.name[i] ); }
    for ( int32_t i = 0; i < v.icon_length; i++ ) { h = fold( h, v.icon[i] ); }
    h = fold( h, v.experience );
    h = fold( h, (uint64_t) v.tilt );
    h = fold( h, (uint64_t) v.heading );
    h = fold( h, (uint64_t) v.timestamp );
    h = fold( h, v.badge );
    h = fold( h, v.port );
    h = fold( h, v.epoch );
    for ( const float r : v.ratings ) { h = fold_f( h, r ); }
    h = fold( h, v.has_loadout );
    return v.has_loadout ? read_loadout( h, v.loadout ) : h;
}

static uint64_t read_root_config( uint64_t h, const tabledemo::RootConfig & v )
{
    for ( int32_t i = 0; i < v.version_note_length; i++ ) { h = fold( h, (uint8_t) v.version_note[i] ); }
    for ( int32_t i = 0; i < v.weapons_count; i++ ) { h = read_weapon( h, v.weapons[i] ); }
    for ( int32_t i = 0; i < v.profiles_count; i++ ) { h = read_profile( h, v.profiles[i] ); }
    // and by INDEX, the last of each: the path a caller that knows its row takes
    if ( v.weapons_count > 0 ) { h = fold( h, v.weapons[v.weapons_count - 1].channel ); }
    if ( v.profiles_count > 0 ) { h = fold( h, v.profiles[v.profiles_count - 1].experience ); }
    return h;
}

static uint64_t read_root( const tabledemo::RootConfig & v ) { return read_root_config( 1, v ); }
static uint64_t read_profile_root( const tabledemo::ProfileConfig & v ) { return read_profile( 1, v ); }
static uint64_t read_loadout_root( const tabledemo::LoadoutConfig & v ) { return read_loadout( 1, v ); }
static uint64_t read_archive( const tabledemo::ArchiveConfig & v ) { return fold( read_root_config( 1, v.root ), (uint64_t) v.count ); }

static uint64_t read_wide( const tabledemo::WideBlob & v )
{
    uint64_t h = 1;
    for ( int32_t i = 0; i < v.label_length; i++ ) { h = fold( h, (uint8_t) v.label[i] ); }
    for ( int32_t i = 0; i < v.payload_length; i++ ) { h = fold( h, v.payload[i] ); }
    for ( int32_t i = 0; i < v.samples_count; i++ ) { h = fold( h, v.samples[i] ); }
    if ( v.samples_count > 0 ) { h = fold( h, v.samples[v.samples_count / 2] ); }
    return h;
}

static uint64_t read_keyed( const tabledemo::KeyedConfig & v )
{
    uint64_t h = 1;
    for ( auto [ team, config ] : v.teams )
    {
        h = fold( h, (uint64_t) team );
        h = fold( h, (uint64_t) config.spawn_count );
        for ( int32_t i = 0; i < config.banner_length; i++ ) { h = fold( h, (uint8_t) config.banner[i] ); }
    }
    for ( auto [ hull, config ] : v.hulls )
    {
        h = fold( h, (uint64_t) hull );
        h = fold_f( h, config.health );
        h = fold_f( h, config.mass );
        for ( auto [ weapon, turret ] : config.turrets )
        {
            h = fold( h, (uint64_t) weapon );
            h = fold_f( h, turret.damage );
            h = fold_f( h, turret.cooldown );
            h = fold( h, turret.gunner_present );
        }
    }
    // and by KEY, through the refusing accessor
    h = fold( h, (uint64_t) v.teams[tabledemo::Team::Blue].spawn_count );
    h = fold_f( h, v.hulls[tabledemo::Hull::Freighter].mass );
    for ( const int32_t s : v.scores.per_team ) { h = fold( h, (uint64_t) s ); }
    return h;
}

template <typename Cfg>
static uint64_t read_cfg_common( const Cfg & v )
{
    uint64_t h = fold( 1, (uint64_t) v.mode );
    for ( int32_t i = 0; i < v.items_count; i++ ) { h = fold( h, (uint64_t) v.items[i] ); }
    for ( int32_t i = 0; i < v.grades_count; i++ ) { h = fold( h, (uint64_t) v.grades[i] ); }
    for ( int32_t i = 0; i < v.slots_count; i++ ) { h = fold( h, (uint64_t) v.slots[i] ); }
    for ( const auto g : v.podium ) { h = fold( h, (uint64_t) g ); }
    for ( const int32_t t : v.tally ) { h = fold( h, (uint64_t) t ); }
    h = fold( h, (uint64_t) v.grade );
    h = fold( h, (uint64_t) v.effect.type );
    for ( auto [ slot, cell ] : v.bank )
    {
        h = fold( h, (uint64_t) slot );
        h = fold( h, (uint64_t) cell.power );
        for ( int32_t i = 0; i < cell.label_length; i++ ) { h = fold( h, (uint8_t) cell.label[i] ); }
    }
    for ( auto [ slot, token ] : v.tokens ) { h = fold( fold( h, (uint64_t) slot ), (uint64_t) token ); }
    for ( auto [ slot, rank ] : v.ranks ) { h = fold( fold( h, (uint64_t) slot ), (uint64_t) rank ); }
    if ( v.items_count > 0 ) { h = fold( h, (uint64_t) v.items[v.items_count - 1] ); }
    return h;
}

static uint64_t read_v1( const tblv1::Cfg & v )
{
    uint64_t h = fold( read_cfg_common( v ), (uint64_t) v.a );
    h = fold_f( h, v.b );
    for ( int32_t i = 0; i < v.name_length; i++ ) { h = fold( h, (uint8_t) v.name[i] ); }
    return h;
}

static uint64_t read_v2( const tblv2::Cfg & v )
{
    uint64_t h = fold_f( read_cfg_common( v ), v.a );
    h = fold( h, v.c );
    for ( int32_t i = 0; i < v.title_length; i++ ) { h = fold( h, (uint8_t) v.title[i] ); }
    for ( auto [ grade, entry ] : v.ledger ) { h = fold( fold( h, (uint64_t) grade ), (uint64_t) entry ); }
    return h;
}

template <typename Chain>
static uint64_t read_chain_common( const Chain & v )
{
    uint64_t h = 1;
    for ( int32_t i = 0; i < v.name_length; i++ ) { h = fold( h, (uint8_t) v.name[i] ); }
    h = fold( h, (uint64_t) v.link.value );
    for ( int32_t i = 0; i < v.link.tag_length; i++ ) { h = fold( h, (uint8_t) v.link.tag[i] ); }
    return h;
}

static uint64_t read_p1( const tblp1::Chain & v ) { return read_chain_common( v ); }
static uint64_t read_p3( const tblp3::Chain & v ) { return fold( read_chain_common( v ), v.link_present ); }

// ---- one root, erased ----
//
// Every operation is a plain function pointer to one instantiation below: no
// std::function, which is itself allowed to allocate, and no virtual call.
// Each root has one value in static storage (WideBlob is 350 KB); two cases on
// the same root share it, one after the other.

struct Ops
{
    bool ( *load )( const uint8_t * wire, int64_t bytes );
    uint64_t ( *read )();
    int64_t ( *measure )();
    int64_t ( *save )( uint8_t * buffer, int64_t capacity );
    int64_t ( *text_measure )();
    int64_t ( *text )( char * buffer, int64_t capacity );
    bool ( *from_text )( const char * text, int64_t bytes );
};

template <typename T, typename Report,
          bool ( *Load )( T &, const uint8_t *, int64_t, Report * ),
          uint64_t ( *Read )( const T & ),
          int64_t ( *Measure )( const T & ),
          int64_t ( *Save )( const T &, uint8_t *, int64_t ),
          int64_t ( *TextMeasure )( const T & ),
          int64_t ( *Text )( const T &, char *, int64_t ),
          bool ( *FromText )( T &, const char *, int64_t, Report * )>
struct Codec
{
    static T value;
    static bool load( const uint8_t * wire, int64_t bytes )
    {
        Report report{};
        return Load( value, wire, bytes, &report ) && report.malformed == 0;
    }
    static uint64_t read() { return Read( value ); }
    static int64_t measure() { return Measure( value ); }
    static int64_t save( uint8_t * buffer, int64_t capacity ) { return Save( value, buffer, capacity ); }
    static int64_t text_measure() { return TextMeasure( value ); }
    static int64_t text( char * buffer, int64_t capacity ) { return Text( value, buffer, capacity ); }
    static bool from_text( const char * json, int64_t bytes )
    {
        Report report{};
        return FromText( value, json, bytes, &report ) && report.malformed == 0;
    }
    static constexpr Ops ops = { load, read, measure, save, text_measure, text, from_text };
};

template <typename T, typename Report,
          bool ( *Load )( T &, const uint8_t *, int64_t, Report * ),
          uint64_t ( *Read )( const T & ),
          int64_t ( *Measure )( const T & ),
          int64_t ( *Save )( const T &, uint8_t *, int64_t ),
          int64_t ( *TextMeasure )( const T & ),
          int64_t ( *Text )( const T &, char *, int64_t ),
          bool ( *FromText )( T &, const char *, int64_t, Report * )>
T Codec<T, Report, Load, Read, Measure, Save, TextMeasure, Text, FromText>::value;

#define SOAK_CODEC( ns, Root, read )                                                         \
    &Codec<ns::Root, ns::TableReport, ns::Root##Load, read, ns::Root##Measure, ns::Root##Save, \
           ns::Root##ToJsonMeasure, ns::Root##ToJson, ns::Root##FromJson>::ops

struct Case
{
    const char * unit;
    const char * root;
    const char * wire;
    // an EXACT case re-saves to the bytes it was loaded from; an EVOLUTION
    // SEAM is read by a type that did not write it, so what comes back out is
    // this reader's own encoding
    bool exact;
    const Ops * ops;
};

// the corpus the C soak reads, in the same order: every instance the wire
// surface pins, then the four evolution seams
static const Case cases[] = {
    { "tabledemo", "RootConfig", "testdata/wire/tables/root_full.bin", true, SOAK_CODEC( tabledemo, RootConfig, read_root ) },
    { "tabledemo", "RootConfig", "testdata/wire/tables/root_default.bin", true, SOAK_CODEC( tabledemo, RootConfig, read_root ) },
    { "tabledemo", "ProfileConfig", "testdata/wire/tables/profile_elide.bin", true, SOAK_CODEC( tabledemo, ProfileConfig, read_profile_root ) },
    { "tabledemo", "LoadoutConfig", "testdata/wire/tables/loadout_full.bin", true, SOAK_CODEC( tabledemo, LoadoutConfig, read_loadout_root ) },
    { "tabledemo", "WideBlob", "testdata/wire/tables/wide_blob.bin", true, SOAK_CODEC( tabledemo, WideBlob, read_wide ) },
    { "tabledemo", "ArchiveConfig", "testdata/wire/tables/archive.bin", true, SOAK_CODEC( tabledemo, ArchiveConfig, read_archive ) },
    { "tabledemo", "KeyedConfig", "testdata/wire/tables/keyed_config.bin", true, SOAK_CODEC( tabledemo, KeyedConfig, read_keyed ) },
    { "tabledemo", "KeyedConfig", "testdata/wire/tables/keyed_default.bin", true, SOAK_CODEC( tabledemo, KeyedConfig, read_keyed ) },
    { "tblv1", "Cfg", "testdata/wire/tables/v1_cfg.bin", true, SOAK_CODEC( tblv1, Cfg, read_v1 ) },
    { "tblv1", "Cfg", "testdata/wire/tables/v1_seams.bin", true, SOAK_CODEC( tblv1, Cfg, read_v1 ) },
    { "tblv2", "Cfg", "testdata/wire/tables/v2_cfg.bin", true, SOAK_CODEC( tblv2, Cfg, read_v2 ) },
    { "tblv2", "Cfg", "testdata/wire/tables/v2_seams.bin", true, SOAK_CODEC( tblv2, Cfg, read_v2 ) },
    { "tblp1", "Chain", "testdata/wire/tables/chain_value.bin", true, SOAK_CODEC( tblp1, Chain, read_p1 ) },
    { "tblp3", "Chain", "testdata/wire/tables/chain_optional.bin", true, SOAK_CODEC( tblp3, Chain, read_p3 ) },
    // the EVOLUTION SEAMS: bytes read by a type that did not write them
    { "tblv2", "Cfg", "testdata/wire/tables/v1_cfg.bin", false, SOAK_CODEC( tblv2, Cfg, read_v2 ) },
    { "tblv1", "Cfg", "testdata/wire/tables/v2_seams.bin", false, SOAK_CODEC( tblv1, Cfg, read_v1 ) },
    { "tblp3", "Chain", "testdata/wire/tables/chain_value.bin", false, SOAK_CODEC( tblp3, Chain, read_p3 ) },
    { "tblp1", "Chain", "testdata/wire/tables/chain_optional.bin", false, SOAK_CODEC( tblp1, Chain, read_p1 ) },
};

static constexpr size_t kCases = sizeof( cases ) / sizeof( cases[0] );

// EVERY BUFFER IS SIZED ONCE, BEFORE THE CLOCK, and it is caller-owned: the
// contract is that the generated code allocates none of them, so a harness
// that allocated per iteration would be measuring itself. These are static
// arrays, sized for the corpus, so the harness owns no heap at all.
static constexpr int64_t kWireCapacity = 1 << 20;
static uint8_t wires[kCases][kWireCapacity];
static uint8_t saved[kCases][kWireCapacity];
static char texts[kCases][kWireCapacity];

struct Loaded
{
    int64_t wire_bytes = 0;
    int64_t save_bytes = 0;
    int64_t text_bytes = 0;
    uint64_t checksum = 0;
};

static Loaded loaded[kCases];

static bool load_file( const char * path, uint8_t * into, int64_t capacity, int64_t & bytes )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) { return false; }
    const size_t n = fread( into, 1, (size_t) capacity, file );
    const bool whole = feof( file ) != 0;
    fclose( file );
    bytes = (int64_t) n;
    return whole;
}

// One full pass over the corpus: Load, the reads, Measure, Save, and the text
// out and back in. Returns the index of the first case that went wrong, or -1.
static int pass( const char ** why )
{
    for ( size_t i = 0; i < kCases; i++ )
    {
        const Ops & ops = *cases[i].ops;
        Loaded & l = loaded[i];
        if ( !ops.load( wires[i], l.wire_bytes ) ) { *why = "did not load"; return (int) i; }
        if ( ops.read() != l.checksum ) { *why = "read back a different checksum"; return (int) i; }
        if ( ops.measure() != l.save_bytes ) { *why = "measured a different size"; return (int) i; }
        if ( ops.save( saved[i], kWireCapacity ) != l.save_bytes ) { *why = "saved a different size"; return (int) i; }
        if ( ops.text( texts[i], kWireCapacity ) != l.text_bytes ) { *why = "wrote a text of a different length"; return (int) i; }
        if ( !ops.from_text( texts[i], l.text_bytes ) ) { *why = "did not read its own text back"; return (int) i; }
    }
    return -1;
}

// One iteration of the soak's loop body. The warmup runs exactly this, so
// whatever the first iteration costs is charged before the baseline is read.
static int iterate( const char ** why )
{
#if defined( SCHEMA_SOAK_PLANT_PAIR )
    {
        // the negative control's sabotage: one matched pair, invisible to a
        // live-byte sample. Storing through a volatile pointer is what stops
        // the compiler eliding a new/delete pair outright, which the language
        // allows it to — a control the compiler removed proves nothing.
        static int * volatile planted = NULL;
        planted = new int( 7 );
        delete planted;
        planted = NULL;
    }
#endif
    return pass( why );
}

int main( int argc, char ** argv )
{
    const double seconds = argc > 1 ? strtod( argv[1], NULL ) : 20.0;

    // ---- setup: read the corpus, size every buffer, and hold each exact case
    // to its own bytes before anything is timed ----
    for ( size_t i = 0; i < kCases; i++ )
    {
        const Case & c = cases[i];
        Loaded & l = loaded[i];
        if ( !load_file( c.wire, wires[i], kWireCapacity, l.wire_bytes ) )
        {
            fprintf( stderr, "c++ tables soak: cannot read %s\n", c.wire );
            return 1;
        }
        if ( !c.ops->load( wires[i], l.wire_bytes ) )
        {
            fprintf( stderr, "c++ tables soak: %s.%s does not load %s\n", c.unit, c.root, c.wire );
            return 1;
        }
        l.checksum = c.ops->read();
        l.save_bytes = c.ops->measure();
        if ( l.save_bytes <= 0 || l.save_bytes > kWireCapacity ||
             c.ops->save( saved[i], kWireCapacity ) != l.save_bytes )
        {
            fprintf( stderr, "c++ tables soak: %s.%s does not measure and save %s\n", c.unit, c.root, c.wire );
            return 1;
        }
        // THE GOLDEN GATE: an exact case comes back byte for byte, so the loop
        // below is soaking a codec that reproduces the corpus
        if ( c.exact && ( l.save_bytes != l.wire_bytes || memcmp( saved[i], wires[i], (size_t) l.wire_bytes ) != 0 ) )
        {
            fprintf( stderr, "c++ tables soak: %s does not re-save to its own bytes — refusing to soak a codec "
                             "that does not reproduce the corpus\n", c.wire );
            return 1;
        }
        l.text_bytes = c.ops->text_measure();
        if ( l.text_bytes <= 0 || l.text_bytes > kWireCapacity || c.ops->text( texts[i], kWireCapacity ) != l.text_bytes )
        {
            fprintf( stderr, "c++ tables soak: %s.%s does not write its text for %s\n", c.unit, c.root, c.wire );
            return 1;
        }
    }

    // stdio's first write allocates its buffer: done here, before the warmup
    printf( "c++ tables soak: %zu cases, %.0f s, the allocation count read at every sample%s\n", kCases, seconds,
#if defined( SCHEMA_SOAK_PLANT_PAIR )
            " — PLANTED: one matched new/delete pair per iteration"
#else
            ""
#endif
    );
    fflush( stdout );

    const long long news_at_start = soak_news.load();
    const long long mallocs_at_start = malloc_calls();

    // ---- the warmup: one iteration of the loop body, untimed and ungated, so
    // a lazily built table — or the allocator's own per-thread cache filling
    // on a first allocation — is charged to it rather than to the loop ----
    const char * why = "";
    int bad = iterate( &why );
    if ( bad >= 0 )
    {
        fprintf( stderr, "SOAK FAILED: %s %s on the warmup pass\n", cases[bad].wire, why );
        return 1;
    }

    // the drift instruments are read FIRST and the counts LAST, so nothing
    // this harness does lands between the baseline and the loop
    const long long rss_warm = resident_kib();
    const long long heap_warm = live_heap_bytes();
    const long long news_warm = soak_news.load();
    const long long mallocs_warm = malloc_calls();

    // ---- the soak ----
    const auto start = std::chrono::steady_clock::now();
    unsigned long long iterations = 0, samples = 0;
    unsigned long long moved_sample = 0, moved_iteration = 0;
    long long moved_news = 0, moved_mallocs = 0;
    for ( ;; )
    {
        bad = iterate( &why );
        if ( bad >= 0 )
        {
            fprintf( stderr, "SOAK FAILED: %s %s after %llu iterations\n", cases[bad].wire, why, iterations );
            return 1;
        }
        iterations++;
        if ( ( iterations & 0xf ) != 0 ) { continue; } // a sample every 16 passes

        // ---- a SAMPLE: the count is read, and it must be where the warmup
        // left it ----
        samples++;
        const long long news_now = soak_news.load() - news_warm;
        const long long mallocs_now = malloc_calls() - mallocs_warm;
        if ( news_now != 0 || mallocs_now != 0 )
        {
            moved_sample = samples;
            moved_iteration = iterations;
            moved_news = news_now;
            moved_mallocs = mallocs_now;
            break;
        }
        const std::chrono::duration<double> elapsed = std::chrono::steady_clock::now() - start;
        if ( elapsed.count() >= seconds ) { break; }
    }
    const long long news_end = soak_news.load();
    const long long mallocs_end = malloc_calls();
    const long long heap_end = live_heap_bytes();
    const long long rss_end = resident_kib();
    const std::chrono::duration<double> elapsed = std::chrono::steady_clock::now() - start;

    // ---- the report: the count, then drift, then the verdict ----
    printf( "c++ tables soak: %llu iterations over %zu cases in %.1f s, %llu samples\n", iterations, kCases,
            elapsed.count(), samples );
    printf( "c++ tables soak: operator new calls: %lld in the warmup, %lld after it\n", news_warm - news_at_start,
            news_end - news_warm );
#if SCHEMA_SOAK_COUNTS_MALLOC
    printf( "c++ tables soak: allocator calls (malloc/calloc/realloc/free): %lld in the warmup, %lld after it\n",
            mallocs_warm - mallocs_at_start, mallocs_end - mallocs_warm );
#else
    (void) mallocs_at_start;
    (void) mallocs_end;
    printf( "c++ tables soak: allocator calls: unmeasured on this platform (no interposition); operator new is the count\n" );
#endif
    bool drift_flat = true;
    if ( heap_warm < 0 )
    {
        printf( "c++ tables soak: live heap: this platform exposes no allocator statistics, drift unmeasured\n" );
    }
    else
    {
        drift_flat = heap_end == heap_warm;
        printf( "c++ tables soak: live heap %lld bytes after the warmup, %lld at the end — drift %s\n", heap_warm,
                heap_end, drift_flat ? "flat" : "MOVED" );
    }
    if ( rss_warm >= 0 )
    {
        printf( "c++ tables soak: resident %lld KiB after the warmup, %lld KiB at the end (reported, not gated)\n",
                rss_warm, rss_end );
    }
    fflush( stdout );

    int status = 0;
    if ( moved_sample != 0 )
    {
        fprintf( stderr, "SOAK FAILED: the allocation count moved after the warmup — %lld operator new and %lld "
                         "allocator call(s) by sample %llu (iteration %llu). Load, the reads, Measure, Save and "
                         "the text paths allocate NOTHING, and a live-heap sample cannot see a matched pair\n",
                 moved_news, moved_mallocs, moved_sample, moved_iteration );
        status = 1;
    }
    if ( !drift_flat )
    {
        fprintf( stderr, "SOAK FAILED: live heap moved from %lld to %lld bytes over %llu iterations — this is retention\n",
                 heap_warm, heap_end, iterations );
        status = 1;
    }
    if ( samples == 0 && status == 0 )
    {
        fprintf( stderr, "SOAK FAILED: no sample was taken — the run was too short to gate anything\n" );
        status = 1;
    }
    if ( status == 0 )
    {
        printf( "c++ tables soak: allocation count flat after the warmup at all %llu samples, drift flat — the "
                "read and write paths allocate nothing\n", samples );
    }
    return status;
}
