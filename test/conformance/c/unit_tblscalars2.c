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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( SimState, sim_state )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblscalars2, SimState )
};

const ConformanceStrict * conformance_stricts_tblscalars2( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
