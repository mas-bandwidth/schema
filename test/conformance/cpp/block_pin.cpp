// THE BLOCK PIN WRITER (block_walk.h): the C++ conformance driver's block walk
// and nothing else, so `make pins-conformance-block` compiles the two units a
// block names (blockdemo and cooktorture) and not the forty the whole driver
// links. It answers exactly what `harness pin -surface block-dump` asks of it:
//
//   conformance-cpp-block <manifest> block-dump <outdir>
//   conformance-cpp-block emit-block-forgeries
//
// and refuses every other surface by name. build/conformance-cpp compiles the
// same header and byte-compares these pins on every conformance run, so a walk
// this binary wrote differently has nowhere to hide.

#include "block_walk.h"

int main( int argc, char ** argv )
{
    if ( argc == 2 && strcmp( argv[1], "emit-block-forgeries" ) == 0 ) return emit_block_forgeries();
    if ( argc != 4 )
    {
        fprintf( stderr, "usage: %s <manifest> block-dump <outdir>\n       %s emit-block-forgeries\n", argv[0], argv[0] );
        return 2;
    }
    const std::string surface = argv[2];
    if ( surface != "block-dump" )
    {
        fprintf( stderr, "conformance-cpp-block: answers block-dump and emit-block-forgeries only, not %s: the whole driver is build/conformance-cpp\n", surface.c_str() );
        return 2;
    }
    if ( !read_manifest( argv[1] ) ) return 1;
    return surface_block_dump( argv[3] );
}
