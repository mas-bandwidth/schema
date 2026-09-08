/* The wide scalar and fixed-point corpus, under the same erased C surface. */
#include "ScalarsTable.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( SimState, sim_state )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( scalars, SimState )
};
const ConformanceCodec * conformance_codecs_scalars( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
