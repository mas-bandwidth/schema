/* The wide scalar and fixed-point corpus, under the same erased C surface. */
#include "ScalarsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE scalardemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER SCALARDEMO
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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( SimState, sim_state )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( scalars, SimState )
};

const ConformanceStrict * conformance_stricts_scalars( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
