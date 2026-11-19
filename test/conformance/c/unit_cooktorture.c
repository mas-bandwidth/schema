#include "CookTortureTable.h"
#include "CookTortureBlock.h"
#define SCHEMA_CONFORMANCE_PACKAGE cooktorture
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER COOKTORTURE
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( TortureCook, torture_cook )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( cooktorture, TortureCook )
};
const ConformanceCodec * conformance_codecs_cooktorture( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

int conformance_torture_block_open_ex( void * data, int64_t bytes, int * reason, const uint8_t ** out_base, const void ** out_type )
{
    cooktorture_torture_frame_block_t block;
    if ( !cooktorture_torture_frame_block_open_ex( &block, data, bytes, reason ) ) return 0;
    if ( out_base != NULL ) *out_base = block.base;
    if ( out_type != NULL ) *out_type = cooktorture_torture_frame_block_type();
    return 1;
}
