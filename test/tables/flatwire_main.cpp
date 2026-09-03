// THE CROSS-IMPLEMENTATION LOCK for the FLAT NODE TABLE (docs/SPEC-TABLES.md
// §3.1), in the direction a golden cannot check on its own: the compiler's
// engine (internal/tablewire) and this generated backend are two independent
// implementations of one wire, and each has to read what the other wrote.
//
//   reload <in> <out>   read a wire this build did not write, and write it back
//   write <out>         write a graph with a SHARED NODE, a chain and a tree
//
// The Makefile leg drives both directions and compares bytes; a difference in
// either is a difference in the wire, and neither side is allowed to be the
// only reader of its own output.
#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <new>

#include "GraphTable.h"

static void set_string( char * storage, int32_t & length, const char * text )
{
    length = (int32_t) strlen( text );
    memcpy( storage, text, (size_t) length + 1 );
}

static int reload( const char * in, const char * out )
{
    FILE * f = fopen( in, "rb" );
    if ( f == NULL ) { printf( "cannot open %s\n", in ); return 1; }
    fseek( f, 0, SEEK_END );
    long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * wire = (uint8_t *) malloc( (size_t) size );
    if ( fread( wire, 1, (size_t) size, f ) != (size_t) size ) { printf( "short read\n" ); return 1; }
    fclose( f );

    int64_t attribution = 0;
    int64_t need = graphdemo::SceneLoadMeasure( wire, size, &attribution );
    uint8_t * region = (uint8_t *) malloc( (size_t) need );
    graphdemo::TableReport report;
    const graphdemo::Scene * root = graphdemo::SceneLoad( region, need, wire, size, &report );
    if ( root == NULL )
    {
        printf( "FAILED: the tool's wire did not load\n" );
        return 1;
    }
    if ( report.malformed || report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 )
    {
        printf( "FAILED: the tool's wire did not read clean — unknown %d, kind_mismatch %d, clamped %d, malformed %d\n",
                report.unknown, report.kind_mismatch, report.clamped, (int) report.malformed );
        return 1;
    }

    int64_t back = graphdemo::SceneMeasure( root );
    uint8_t * again = (uint8_t *) malloc( (size_t) back );
    if ( graphdemo::SceneSave( root, again, back ) != back )
    {
        printf( "FAILED: measure and save disagree on the reloaded graph\n" );
        return 1;
    }
    FILE * o = fopen( out, "wb" );
    if ( o == NULL ) { printf( "cannot write %s\n", out ); return 1; }
    fwrite( again, 1, (size_t) back, o );
    fclose( o );
    printf( "reloaded %s: %ld bytes in, %lld bytes out, %lld region + %lld attribution\n",
            in, size, (long long) back, (long long) ( need - attribution ), (long long) attribution );
    free( again );
    free( region );
    free( wire );
    return 0;
}

// A graph with every shape the numbering has to get right: a SHARED node named
// from two places, a chain, a tree, a variable table nested by value, and a
// null in a pointer-shaped slot.
static int write_graph( const char * out )
{
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    set_string( root->name, root->name_length, "flat" );
    root->version = 3;

    graphdemo::TableSlot<graphdemo::ListNode> a = builder.Alloc<graphdemo::ListNode>();
    graphdemo::TableSlot<graphdemo::ListNode> b = builder.Alloc<graphdemo::ListNode>();
    a->value = 10;
    set_string( a->name, a->name_length, "a" );
    b->value = 20;
    a->next = b;
    root->head = a;
    root->alias = b; // the SAME node named twice: one index, one record

    graphdemo::TableSlot<graphdemo::TreeNode> top = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> left = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> shared = builder.Alloc<graphdemo::TreeNode>();
    set_string( top->label, top->label_length, "top" );
    set_string( left->label, left->label_length, "left" );
    set_string( shared->label, shared->label_length, "shared" );
    top->left = left;
    top->right = shared;
    left->left = shared; // a DIAMOND: it closes on a node already numbered
    root->tree = top;

    root->ground.depth = 2;
    root->ground.head = b; // and the shared node again, from a by-value nesting

    int64_t need = graphdemo::SceneMeasure( builder );
    if ( need <= 0 ) { printf( "FAILED: measure refused the graph\n" ); return 1; }
    uint8_t * wire = (uint8_t *) malloc( (size_t) need );
    if ( graphdemo::SceneSave( builder, wire, need ) != need )
    {
        printf( "FAILED: measure and save disagree\n" );
        return 1;
    }
    FILE * o = fopen( out, "wb" );
    if ( o == NULL ) { printf( "cannot write %s\n", out ); return 1; }
    fwrite( wire, 1, (size_t) need, o );
    fclose( o );
    printf( "wrote %s: %lld bytes, 5 nodes for a graph of 8 references\n", out, (long long) need );
    free( wire );
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc >= 4 && strcmp( argv[1], "reload" ) == 0 ) { return reload( argv[2], argv[3] ); }
    if ( argc >= 3 && strcmp( argv[1], "write" ) == 0 ) { return write_graph( argv[2] ); }
    printf( "usage: %s reload <in.wire> <out.wire> | write <out.wire>\n", argv[0] );
    return 2;
}
