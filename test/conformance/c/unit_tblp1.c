/* the tblp1 unit's codecs (test/tables/P1.schema). */
#include "P1Table.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Chain )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblp1, Chain )
};

const ConformanceCodec * conformance_codecs_tblp1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
