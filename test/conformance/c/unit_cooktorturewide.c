#include "CookTortureWideTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE cooktorturewide
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER COOKTORTUREWIDE
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( TortureWideCook, torture_wide_cook )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( cooktorturewide, TortureWideCook )
};
const ConformanceCodec * conformance_codecs_cooktorturewide( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
