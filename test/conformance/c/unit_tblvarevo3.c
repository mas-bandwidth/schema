#include "VarEvo3Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblvarevo3
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLVAREVO3
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( EvoDoc, evo_doc )
SCHEMA_CONFORMANCE_CODEC( EvoMsg, evo_msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblvarevo3, EvoDoc ),
    SCHEMA_CONFORMANCE_ROW( tblvarevo3, EvoMsg )
};
const ConformanceCodec * conformance_codecs_tblvarevo3( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
