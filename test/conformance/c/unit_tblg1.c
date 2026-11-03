#include "G1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblg1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLG1
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Guarded,guarded)
const ConformanceCodec * conformance_codecs_tblg1(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblg1,Guarded)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Guarded, guarded )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblg1, Guarded )
};

const ConformanceStrict * conformance_stricts_tblg1( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
