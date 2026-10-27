#include "Scalars2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE scalardemo2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER SCALARDEMO2
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( SimState, sim_state )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblscalars2, SimState )
};
const ConformanceCodec * conformance_codecs_tblscalars2( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
