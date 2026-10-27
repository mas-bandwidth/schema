/* the tblp1 unit's codecs (test/tables/P1.schema). */
#include "P1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblp1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLP1
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Chain, chain )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblp1, Chain )
};

const ConformanceCodec * conformance_codecs_tblp1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
