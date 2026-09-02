// The CROSS-ENDIAN COOK driver (SPEC-TABLES.md §7).
//
// "Endianness is part of the COOK, not of `Open`." A cook is produced in the
// byte order of the build it is cooked for, and the magic is the byte-order
// check: a file whose recorded order is not this build's is simply not this
// build's file, and refuses. Every other test in this tree runs both halves in
// ONE process, so the rule that a cook does not cross a byte order is the one
// part of §7 no single-process test can reach — it needs two builds and a file
// between them.
//
// This driver is one of those builds. The Makefile's tables-big-endian leg
// runs it four ways: the host writes a cook the big-endian target must refuse,
// the target writes one the host must refuse, and each opens its own.
//
//   cook-endian write  <path>   cook a scene and write it
//   cook-endian accept <path>   Open must succeed and the scene must read back
//   cook-endian refuse <path>   Open must return NULL, and by BYTE ORDER
//
// The refusal case proves three things and not just the first: that Open
// returned NULL rather than pointing at garbage, that the file's magic is this
// build's magic byte-reversed (so byte order is what refused it, not some
// other mismatch), and that the reader still works afterwards — a clean
// refusal leaves the caller able to fall back, which is the whole point of
// returning NULL.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "GraphTable.h"

static int failures = 0;

#define CHECK( cond )                                                    \
    do {                                                                 \
        if ( !( cond ) ) {                                               \
            printf( "FAIL %s:%d %s\n", __FILE__, __LINE__, #cond );      \
            failures++;                                                  \
        }                                                                \
    } while ( 0 )

static void set_string( char * dest, int32_t & length, const char * s )
{
    length = (int32_t) strlen( s );
    memcpy( dest, s, length + 1 );
}

// A scene with the shapes a cooked region is made of: a pointer chain, an
// aliased node, a variable table nested by value, and a bounded array of them.
static void build_scene( graphdemo::SceneBuilder & builder )
{
    graphdemo::Scene * root = builder.GetRoot();
    set_string( root->name, root->name_length, "cooked" );
    root->version = 7;
    root->meta.build = 42;
    set_string( root->meta.tag, root->meta.tag_length, "m" );

    graphdemo::TableSlot<graphdemo::Settings> settings = builder.Alloc<graphdemo::Settings>();
    settings->quality = 4;
    set_string( settings->label, settings->label_length, "high" );
    root->settings = settings;

    graphdemo::TableSlot<graphdemo::ListNode> n0 = builder.Alloc<graphdemo::ListNode>();
    graphdemo::TableSlot<graphdemo::ListNode> n1 = builder.Alloc<graphdemo::ListNode>();
    n0->value = 10; set_string( n0->name, n0->name_length, "a" );
    n1->value = 20; set_string( n1->name, n1->name_length, "b" );
    n0->next = n1;
    root->head = n0;
    root->alias = n1; // two references, one node

    root->ground.depth = 3;
    root->layers_count = 2;
    root->layers[0].depth = 1;
    root->layers[1].depth = 2;
    root->layers[1].head = n1;
}

static void check_scene( const graphdemo::Scene * scene )
{
    CHECK( scene != NULL );
    if ( scene == NULL ) return;
    CHECK( strcmp( scene->name, "cooked" ) == 0 );
    CHECK( scene->version == 7 );
    CHECK( scene->meta.build == 42 );
    const graphdemo::ListNode * a = graphdemo::ListNodeAt( scene->head );
    CHECK( a != NULL && a->value == 10 && strcmp( a->name, "a" ) == 0 );
    const graphdemo::ListNode * b = graphdemo::ListNodeAt( a->next );
    CHECK( b != NULL && b->value == 20 );
    CHECK( graphdemo::ListNodeAt( b->next ) == NULL );
    // a node named twice packs as two nodes — the packed form is a tree, as
    // the wire is (SPEC-TABLES.md §3) — so the alias is a distinct node with
    // the same value, and that is what a reader must find on either byte order
    const graphdemo::ListNode * aliased = graphdemo::ListNodeAt( scene->alias );
    CHECK( aliased != NULL && aliased != b && aliased->value == 20 );
    const graphdemo::Settings * settings = graphdemo::SettingsAt( scene->settings );
    CHECK( settings != NULL && settings->quality == 4 );
    CHECK( scene->layers_count == 2 && scene->layers[0].depth == 1 );
    const graphdemo::ListNode * layered = graphdemo::ListNodeAt( scene->layers[1].head );
    CHECK( layered != NULL && layered != b && layered->value == 20 );
}

// cook_scene returns a malloc'd cooked file, or NULL. malloc's alignment is
// already past the region's (kTableAlign is 8), which is the same reason Open
// can point at an mmap'd base.
static uint8_t * cook_scene( int64_t * bytes )
{
    static graphdemo::SceneBuilder builder;
    build_scene( builder );
    if ( !builder.Lock() ) { printf( "FAIL Lock refused\n" ); failures++; return NULL; }
    int64_t need = graphdemo::SceneCookMeasure( builder );
    uint8_t * cooked = (uint8_t *) malloc( (size_t) need );
    if ( cooked == NULL ) { printf( "FAIL out of memory\n" ); failures++; return NULL; }
    if ( graphdemo::SceneCook( builder, cooked, need ) != need )
    {
        printf( "FAIL Cook wrote the wrong length\n" );
        failures++;
        free( cooked );
        return NULL;
    }
    *bytes = need;
    return cooked;
}

static uint8_t * read_file( const char * path, int64_t * bytes )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { printf( "FAIL cannot read %s\n", path ); failures++; return NULL; }
    fseek( f, 0, SEEK_END );
    long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * buffer = (uint8_t *) malloc( (size_t) size );
    size_t got = fread( buffer, 1, (size_t) size, f );
    fclose( f );
    if ( (long) got != size ) { printf( "FAIL short read of %s\n", path ); failures++; free( buffer ); return NULL; }
    *bytes = (int64_t) size;
    return buffer;
}

// this build's own magic bytes, in the order it writes them — the four bytes
// every comparison below is against
static void local_magic( uint8_t out[4] )
{
    uint32_t magic = graphdemo::kTableCookedMagic;
    memcpy( out, &magic, 4 );
}

static int do_write( const char * path )
{
    int64_t bytes = 0;
    uint8_t * cooked = cook_scene( &bytes );
    if ( cooked == NULL ) return 1;
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { printf( "FAIL cannot write %s\n", path ); free( cooked ); return 1; }
    fwrite( cooked, 1, (size_t) bytes, f );
    fclose( f );
    uint8_t magic[4];
    local_magic( magic );
    printf( "cook-endian write: %lld bytes, magic %02x %02x %02x %02x\n",
            (long long) bytes, magic[0], magic[1], magic[2], magic[3] );
    free( cooked );
    return 0;
}

static int do_accept( const char * path )
{
    int64_t bytes = 0;
    uint8_t * file = read_file( path, &bytes );
    if ( file == NULL ) return 1;
    const graphdemo::Scene * scene = graphdemo::SceneOpen( file, bytes );
    CHECK( scene != NULL );
    CHECK( (const uint8_t *) scene == file + graphdemo::kTableCookedHeaderBytes ); // pointed at, not copied
    check_scene( scene );
    free( file );
    if ( failures > 0 ) { printf( "cook-endian accept: %d failure(s)\n", failures ); return 1; }
    printf( "cook-endian accept: %s opened, in this build's byte order\n", path );
    return 0;
}

static int do_refuse( const char * path )
{
    int64_t bytes = 0;
    uint8_t * file = read_file( path, &bytes );
    if ( file == NULL ) return 1;

    // the file is a cook, and its magic is this build's magic REVERSED: the
    // two builds agree about the constant and disagree about byte order, so
    // byte order is the only thing that can refuse it
    uint8_t mine[4];
    local_magic( mine );
    CHECK( bytes >= graphdemo::kTableCookedHeaderBytes );
    for ( int i = 0; i < 4; i++ ) { CHECK( file[i] == mine[3 - i] ); }

    // and it refuses: NULL, not a pointer into bytes this build cannot read
    CHECK( graphdemo::SceneOpen( file, bytes ) == NULL );
    free( file );

    // the refusal is CLEAN — a caller that fell back would find the reader
    // exactly as it was, so a cook this build wrote still opens
    int64_t own_bytes = 0;
    uint8_t * own = cook_scene( &own_bytes );
    if ( own != NULL )
    {
        const graphdemo::Scene * scene = graphdemo::SceneOpen( own, own_bytes );
        CHECK( scene != NULL );
        check_scene( scene );
        free( own );
    }

    if ( failures > 0 ) { printf( "cook-endian refuse: %d failure(s)\n", failures ); return 1; }
    printf( "cook-endian refuse: %s refused by byte order, cleanly\n", path );
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc != 3 )
    {
        printf( "usage: cook-endian <write|accept|refuse> <path>\n" );
        return 2;
    }
    if ( strcmp( argv[1], "write" ) == 0 )  return do_write( argv[2] );
    if ( strcmp( argv[1], "accept" ) == 0 ) return do_accept( argv[2] );
    if ( strcmp( argv[1], "refuse" ) == 0 ) return do_refuse( argv[2] );
    printf( "unknown mode %s\n", argv[1] );
    return 2;
}
